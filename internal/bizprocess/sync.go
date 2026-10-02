package bizprocess

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/example/bia-platform/internal/prtg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// backfillWindow is how much PRTG history is loaded for a newly found
	// process: enough for month-to-date and 30-day views.
	backfillWindow = 35 * 24 * time.Hour
	// historyChunk keeps each historic-data request to a few MB.
	historyChunk = 7 * 24 * time.Hour
	// wideOverlap pads a history request when the server's UTC offset is
	// unknown; any real offset is within ±14h.
	wideOverlap = 14 * time.Hour
)

// execer is satisfied by both *pgxpool.Pool and pgx.Tx.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Member is one object (sensor, device, group or probe) a channel summarizes.
type Member struct {
	ObjID      string `json:"objid"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Parent     string `json:"parent,omitempty"`
	Status     string `json:"status"`
	State      string `json:"state"`
	CountsAsUp bool   `json:"counts_as_up"`
}

type Syncer struct {
	Pool     *pgxpool.Pool
	Client   *prtg.Client
	Log      *slog.Logger
	Org      string
	Instance string
}

// Sync mirrors every PRTG Business Process sensor found in sensors (the
// collector's latest poll) into the business_process_* tables: current state,
// channel definitions with member status, and the state timeline from PRTG
// history. A failing process is logged and skipped so one bad sensor does not
// block the others.
func (s *Syncer) Sync(ctx context.Context, sensors []prtg.Sensor) error {
	byID := make(map[string]prtg.Sensor, len(sensors))
	var bps []prtg.Sensor
	for _, x := range sensors {
		byID[x.ID] = x
		if x.Type == prtg.TypeBusinessProcess {
			bps = append(bps, x)
		}
	}
	seen := make([]string, 0, len(bps))
	for _, bp := range bps {
		seen = append(seen, bp.ID)
	}
	if _, err := s.Pool.Exec(ctx, `update business_process_sensors set removed_at=now()
		where organization_id=$1 and prtg_instance_id=$2 and removed_at is null and not (prtg_sensor_id = any($3))`,
		s.Org, s.Instance, seen); err != nil {
		return fmt.Errorf("mark removed business processes: %w", err)
	}
	if len(bps) == 0 {
		return nil
	}

	offset, offsetErr := s.Client.ServerUTCOffset(ctx)
	if offsetErr != nil {
		s.Log.Warn("PRTG clock unavailable, using wide history window", "component", "bizprocess", "error", offsetErr)
	}
	others := &objectNames{client: s.Client}
	for _, bp := range bps {
		if err := s.syncOne(ctx, bp, byID, others, offset, offsetErr == nil); err != nil {
			s.Log.Error("business process sync failed", "component", "bizprocess", "organization_id", s.Org, "sensor_id", bp.ID, "error", err)
		}
	}
	return nil
}

func (s *Syncer) syncOne(ctx context.Context, bp prtg.Sensor, byID map[string]prtg.Sensor, others *objectNames, offset time.Duration, offsetKnown bool) error {
	details, err := s.Client.SensorDetails(ctx, bp.ID)
	if err != nil {
		s.Log.Warn("business process details unavailable", "component", "bizprocess", "sensor_id", bp.ID, "error", err)
		details = prtg.SensorDetails{Name: bp.Sensor, ParentDevice: bp.Device, StatusText: bp.Status}
	}
	channels, err := s.Client.Channels(ctx, bp.ID)
	if err != nil {
		return fmt.Errorf("channels: %w", err)
	}
	defs, defErr := s.Client.BusinessProcessDefinition(ctx, bp.ID)
	defStatus := "ok"
	if defErr != nil {
		defStatus = "unavailable"
		s.Log.Warn("business process definition unavailable", "component", "bizprocess", "sensor_id", bp.ID, "error", defErr)
	}
	interval := details.IntervalSec
	if interval <= 0 {
		interval = 60
	}
	name := details.Name
	if name == "" {
		name = bp.Sensor
	}
	state := StateFromText(details.StatusText)
	if state == Unknown {
		state = StateFromText(bp.Status)
	}

	var bpID uuid.UUID
	var serviceID *uuid.UUID
	var sensorUUID *uuid.UUID
	var syncedUntil *time.Time
	var inserted bool
	// A new process is linked to the business service with the same name, if
	// any, so PRTG's naming drives the mapping without manual setup.
	err = s.Pool.QueryRow(ctx, `
		insert into business_process_sensors (organization_id, prtg_instance_id, prtg_sensor_id, sensor_uuid, name, device_name, group_name, state, message,
			scan_interval_sec, prtg_uptime_pct, prtg_downtime_pct, prtg_stats_since, definition_status, last_synced_at, business_service_id)
		values ($1, $2, $3,
			(select id from prtg_sensors where organization_id=$1 and prtg_instance_id=$2 and prtg_sensor_id=$3),
			$4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now(),
			(select id from business_services where organization_id=$1 and lower(name)=lower($4) order by created_at limit 1))
		on conflict (prtg_instance_id, prtg_sensor_id) do update set
			sensor_uuid = coalesce(excluded.sensor_uuid, business_process_sensors.sensor_uuid),
			name = excluded.name, device_name = excluded.device_name, group_name = excluded.group_name,
			state = excluded.state, message = excluded.message, scan_interval_sec = excluded.scan_interval_sec,
			prtg_uptime_pct = coalesce(excluded.prtg_uptime_pct, business_process_sensors.prtg_uptime_pct),
			prtg_downtime_pct = coalesce(excluded.prtg_downtime_pct, business_process_sensors.prtg_downtime_pct),
			prtg_stats_since = coalesce(excluded.prtg_stats_since, business_process_sensors.prtg_stats_since),
			definition_status = excluded.definition_status, last_synced_at = now(), removed_at = null
		returning id, business_service_id, sensor_uuid, history_synced_until, (xmax = 0)`,
		s.Org, s.Instance, bp.ID, name, nullable(details.ParentDevice), nullable(details.ParentGroup), state, nullable(details.Message),
		interval, details.UptimePct, details.DowntimePct, details.StatsSince, defStatus,
	).Scan(&bpID, &serviceID, &sensorUUID, &syncedUntil, &inserted)
	if err != nil {
		return fmt.Errorf("upsert business process: %w", err)
	}
	if inserted && serviceID != nil && sensorUUID != nil {
		// Attribute the process sensor's incidents to its service everywhere
		// else in the dashboard (incident list, SLA, financial impact).
		if err := LinkSensorToService(ctx, s.Pool, s.Org, *sensorUUID, nil, serviceID); err != nil {
			s.Log.Warn("auto service mapping failed", "component", "bizprocess", "sensor_id", bp.ID, "error", err)
		}
	}

	if err := s.syncChannels(ctx, bpID, channels, defs, defErr == nil, byID, others); err != nil {
		return err
	}
	return s.syncHistory(ctx, bpID, bp.ID, syncedUntil, time.Duration(interval)*time.Second, offset, offsetKnown)
}

func (s *Syncer) syncChannels(ctx context.Context, bpID uuid.UUID, channels []prtg.Channel, defs []prtg.BusinessProcessChannel, defOK bool, byID map[string]prtg.Sensor, others *objectNames) error {
	defByName := map[string]prtg.BusinessProcessChannel{}
	for _, d := range defs {
		defByName[d.Name] = d
	}
	keep := make([]int32, 0, len(channels))
	for _, ch := range channels {
		keep = append(keep, int32(ch.ID))
		var warn, errT *float64
		var members []byte
		if d, ok := defByName[ch.Name]; ok && ch.ID != GlobalChannel {
			w, e := d.WarningThreshold, d.ErrorThreshold
			warn, errT = &w, &e
			list := make([]Member, 0, len(d.Objects))
			for _, id := range d.Objects {
				list = append(list, resolveMember(ctx, id, byID, others))
			}
			members, _ = json.Marshal(list)
		} else if defOK || ch.ID == GlobalChannel {
			members = []byte("[]")
		}
		// When the definition could not be read, keep the stored members and
		// thresholds (nil members => coalesce to existing).
		_, err := s.Pool.Exec(ctx, `
			insert into business_process_channels (organization_id, business_process_id, prtg_channel_id, name, state, warning_threshold_pct, error_threshold_pct, members, updated_at)
			values ($1, $2, $3, $4, $5, $6, $7, coalesce($8::jsonb, '[]'::jsonb), now())
			on conflict (business_process_id, prtg_channel_id) do update set
				name = excluded.name, state = excluded.state,
				warning_threshold_pct = case when $9 then excluded.warning_threshold_pct else business_process_channels.warning_threshold_pct end,
				error_threshold_pct = case when $9 then excluded.error_threshold_pct else business_process_channels.error_threshold_pct end,
				members = coalesce($8::jsonb, business_process_channels.members),
				updated_at = now()`,
			s.Org, bpID, ch.ID, ch.Name, StateFromText(ch.LastValue), warn, errT, nullableJSON(members), defOK)
		if err != nil {
			return fmt.Errorf("upsert channel %q: %w", ch.Name, err)
		}
	}
	if _, err := s.Pool.Exec(ctx, `delete from business_process_channels where business_process_id=$1 and not (prtg_channel_id = any($2))`, bpID, keep); err != nil {
		return fmt.Errorf("prune channels: %w", err)
	}
	return nil
}

// syncHistory loads PRTG scans since the last sync (or the backfill window)
// and folds them into each channel's interval timeline.
func (s *Syncer) syncHistory(ctx context.Context, bpID uuid.UUID, prtgID string, syncedUntil *time.Time, scan time.Duration, offset time.Duration, offsetKnown bool) error {
	now := time.Now().UTC()
	from := now.Add(-backfillWindow)
	if syncedUntil != nil && syncedUntil.After(from) {
		from = *syncedUntil
	}
	for chunkStart := from; chunkStart.Before(now); chunkStart = chunkStart.Add(historyChunk) {
		chunkEnd := chunkStart.Add(historyChunk)
		if chunkEnd.After(now) {
			chunkEnd = now
		}
		reqFrom, reqTo, reqOffset := chunkStart.Add(-2*time.Minute), chunkEnd.Add(2*time.Minute), offset
		if !offsetKnown {
			reqFrom, reqTo, reqOffset = chunkStart.Add(-wideOverlap), chunkEnd.Add(wideOverlap), 0
		}
		samples, err := s.Client.History(ctx, prtgID, reqFrom, reqTo, reqOffset)
		if err != nil {
			return fmt.Errorf("history %s..%s: %w", chunkStart.Format(time.RFC3339), chunkEnd.Format(time.RFC3339), err)
		}
		last, err := s.applySamples(ctx, bpID, samples, chunkStart, chunkEnd, scan)
		if err != nil {
			return err
		}
		if last.IsZero() {
			continue
		}
		if _, err := s.Pool.Exec(ctx, `update business_process_sensors set history_synced_until=$2 where id=$1`, bpID, last); err != nil {
			return fmt.Errorf("save history cursor: %w", err)
		}
	}
	return nil
}

// applySamples writes the samples in (after, until] into the timeline in one
// transaction and returns the newest sample time applied.
func (s *Syncer) applySamples(ctx context.Context, bpID uuid.UUID, samples []prtg.HistorySample, after, until time.Time, scan time.Duration) (time.Time, error) {
	sort.Slice(samples, func(i, j int) bool { return samples[i].At.Before(samples[j].At) })
	perChannel := map[int][]Sample{}
	var last time.Time
	for _, h := range samples {
		if !h.At.After(after) || h.At.After(until) {
			continue
		}
		for ch, v := range h.Values {
			if ch < 0 {
				continue
			}
			perChannel[ch] = append(perChannel[ch], Sample{At: h.At, State: StateFromValueRaw(v)})
		}
		last = h.At
	}
	if len(perChannel) == 0 {
		return time.Time{}, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)
	for ch, list := range perChannel {
		var tail *Interval
		var t Interval
		err := tx.QueryRow(ctx, `select state, started_at, ended_at from business_process_intervals
			where business_process_id=$1 and prtg_channel_id=$2 order by started_at desc limit 1`, bpID, ch).Scan(&t.State, &t.Start, &t.End)
		switch {
		case err == nil:
			t.ChannelID = ch
			tail = &t
		case err != pgx.ErrNoRows:
			return time.Time{}, fmt.Errorf("load timeline tail: %w", err)
		}
		newTail, created := ExtendIntervals(tail, ch, list, scan)
		if newTail != nil && !newTail.End.Equal(tail.End) {
			if _, err := tx.Exec(ctx, `update business_process_intervals set ended_at=$4
				where business_process_id=$1 and prtg_channel_id=$2 and started_at=$3`, bpID, ch, newTail.Start, newTail.End); err != nil {
				return time.Time{}, fmt.Errorf("extend interval: %w", err)
			}
		}
		for _, iv := range created {
			if _, err := tx.Exec(ctx, `insert into business_process_intervals (organization_id, business_process_id, prtg_channel_id, state, started_at, ended_at)
				values ($1, $2, $3, $4, $5, $6)
				on conflict (business_process_id, prtg_channel_id, started_at) do update set state=excluded.state, ended_at=excluded.ended_at`,
				s.Org, bpID, ch, iv.State, iv.Start, iv.End); err != nil {
				return time.Time{}, fmt.Errorf("insert interval: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return last, nil
}

// LinkSensorToService moves a Business Process sensor's service mapping from
// oldService to newService (either may be nil) so incidents raised on the
// process sensor are attributed to the linked business service.
func LinkSensorToService(ctx context.Context, q execer, org string, sensorUUID uuid.UUID, oldService, newService *uuid.UUID) error {
	if oldService != nil && (newService == nil || *oldService != *newService) {
		if _, err := q.Exec(ctx, `delete from service_sensor_mapping where organization_id=$1 and business_service_id=$2 and prtg_sensor_id=$3`, org, *oldService, sensorUUID); err != nil {
			return err
		}
	}
	if newService != nil {
		if _, err := q.Exec(ctx, `insert into service_sensor_mapping (organization_id, business_service_id, prtg_sensor_id, dependency_weight)
			values ($1, $2, $3, 1.0) on conflict (business_service_id, prtg_sensor_id) do nothing`, org, *newService, sensorUUID); err != nil {
			return err
		}
	}
	return nil
}

// objectNames lazily loads device and group names for members that are not
// sensors, at most once per sync.
type objectNames struct {
	client *prtg.Client
	loaded bool
	kinds  map[string]string
	infos  map[string]prtg.ObjectInfo
}

func (o *objectNames) lookup(ctx context.Context, id string) (string, prtg.ObjectInfo, bool) {
	if !o.loaded {
		o.loaded = true
		o.kinds, o.infos = map[string]string{}, map[string]prtg.ObjectInfo{}
		for _, content := range []string{"devices", "groups"} {
			m, err := o.client.ObjectNames(ctx, content)
			if err != nil {
				continue
			}
			for k, v := range m {
				o.kinds[k] = content[:len(content)-1]
				o.infos[k] = v
			}
		}
	}
	info, ok := o.infos[id]
	return o.kinds[id], info, ok
}

func resolveMember(ctx context.Context, id string, byID map[string]prtg.Sensor, others *objectNames) Member {
	m := Member{ObjID: id, Kind: "object", Name: "Objek #" + id}
	if x, ok := byID[id]; ok {
		m.Kind, m.Name, m.Parent, m.Status = "sensor", x.Sensor, x.Device, x.Status
	} else if kind, info, ok := others.lookup(ctx, id); ok {
		m.Kind, m.Name, m.Status = kind, info.Name, info.Status
	}
	m.State, m.CountsAsUp = MemberState(m.Status)
	return m
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}
