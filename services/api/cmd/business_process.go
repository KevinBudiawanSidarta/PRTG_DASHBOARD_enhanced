package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/example/bia-platform/internal/audit"
	"github.com/example/bia-platform/internal/bizprocess"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ─── Business Process Impact (PRTG Business Process sensors) ─────────────────
//
// The collector mirrors PRTG Business Process sensors (state, channel
// definitions, per-scan history). These handlers turn that into business
// impact: current loss rate, outage episodes with root-cause channels, SLA
// position and estimated loss, using the linked business service's
// financial profile.

type bpRow struct {
	ID             uuid.UUID
	PRTGSensorID   string
	SensorUUID     *uuid.UUID
	Name           string
	Device, Group  string
	State, Message string
	ScanInterval   int
	UptimePct      *float64
	DowntimePct    *float64
	StatsSince     *time.Time
	ServiceID      *uuid.UUID
	DegradedPct    float64
	DefStatus      string
	HistoryUntil   *time.Time
	LastSynced     *time.Time
	RemovedAt      *time.Time
}

type bpChannelRow struct {
	ID       int
	Name     string
	State    string
	Warn     *float64
	Err      *float64
	Members  []bizprocess.Member
}

type bpService struct {
	ID            uuid.UUID
	Name          string
	Criticality   string
	Owner         string
	AffectedUsers int
	SLATarget     float64
	RTOMinutes    int
}

type bpProfile struct {
	HourlyRevenue, Dependency, LossProbability, Operational, Penalty, Recovery, EmployeeCost float64
	Employees                                                                                int
}

func bpPeriod(key string, now time.Time) (string, time.Time) {
	switch key {
	case "7d":
		return key, now.Add(-7 * 24 * time.Hour)
	case "30d":
		return key, now.Add(-30 * 24 * time.Hour)
	}
	return "mtd", time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (a *app) listBusinessProcesses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org := orgID(r)
	now := time.Now().UTC()
	periodKey, from := bpPeriod(r.URL.Query().Get("period"), now)
	pollSec, _ := strconv.Atoi(env("POLL_INTERVAL_SEC", "60"))
	if pollSec <= 0 {
		pollSec = 60
	}

	bps, err := a.loadBusinessProcesses(ctx, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	services, err := a.loadBPServices(ctx, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	profiles, orgDefault, err := a.loadBPProfiles(ctx, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}

	items := []map[string]any{}
	var lastSync *time.Time
	summary := map[string]any{"total": 0, "down": 0, "degraded": 0, "unlinked": 0}
	var totalRate, totalLoss, availSum float64
	var availN, total, down, degraded, unlinked int
	for _, bp := range bps {
		if bp.LastSynced != nil && (lastSync == nil || bp.LastSynced.After(*lastSync)) {
			lastSync = bp.LastSynced
		}
		channels, err := a.loadBPChannels(ctx, bp.ID)
		if err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		timeline, err := a.loadBPIntervals(ctx, bp.ID, from)
		if err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		// Let an ongoing state keep accruing between syncs, but never assume
		// a state for data older than two polls.
		maxStale := time.Duration(2*pollSec+2*bp.ScanInterval) * time.Second
		for ch, ivs := range timeline {
			timeline[ch] = bizprocess.Clip(bizprocess.ExtendToNow(ivs, now, maxStale), from, now)
		}

		var svc *bpService
		if bp.ServiceID != nil {
			if s, ok := services[*bp.ServiceID]; ok {
				svc = &s
			}
		}
		profileSource := "none"
		var prof *bpProfile
		if svc != nil {
			if p, ok := profiles[svc.ID]; ok {
				prof, profileSource = &p, "service"
			} else if orgDefault != nil {
				prof, profileSource = orgDefault, "organization_default"
			}
		}
		rates := bizprocess.Rates{DegradedFactor: bp.DegradedPct / 100}
		if prof != nil {
			rates.RevenuePerHour = prof.HourlyRevenue * prof.Dependency * prof.LossProbability
			rates.ProductivityPerHour = float64(prof.Employees) * prof.EmployeeCost
			rates.OperationalPerHour = prof.Operational
			rates.RecoveryFixed = prof.Recovery
			rates.PenaltyFixed = prof.Penalty
		}
		if svc != nil {
			rates.RTOSeconds = float64(svc.RTOMinutes) * 60
		}

		component := map[int][]bizprocess.Interval{}
		for ch, ivs := range timeline {
			if ch != bizprocess.GlobalChannel {
				component[ch] = ivs
			}
		}
		global := timeline[bizprocess.GlobalChannel]
		ongoing := !bp.isRemoved() && (bp.State == bizprocess.Down || bp.State == bizprocess.Warning)
		episodes := bizprocess.BuildEpisodes(global, component, rates, ongoing)
		stats := bizprocess.Summarize(global, episodes)

		channelName := map[int]string{}
		for _, c := range channels {
			channelName[c.ID] = c.Name
		}

		// Current state and live loss.
		var lossPerHour float64
		switch bp.State {
		case bizprocess.Down:
			lossPerHour = rates.FullPerHour()
		case bizprocess.Warning:
			lossPerHour = rates.FullPerHour() * rates.DegradedFactor
		}
		current := map[string]any{"state": bp.State, "loss_per_hour": lossPerHour}
		if n := len(episodes); n > 0 && episodes[n-1].Ongoing {
			e := episodes[n-1]
			current["since"] = e.Start
			current["duration_seconds"] = e.Seconds()
			current["down_seconds"] = e.DownSeconds
			current["loss_so_far"] = e.Loss.Total
			if bp.State == bizprocess.Down && rates.RTOSeconds > 0 {
				current["rto_remaining_seconds"] = rates.RTOSeconds - e.DownSeconds
			}
		}

		// SLA position for the period, against the linked service's target.
		var sla map[string]any
		if svc != nil {
			known := stats.UpSeconds + stats.WarningSeconds + stats.DownSeconds
			allowed := (100 - svc.SLATarget) / 100 * known
			remaining := allowed - stats.DownSeconds
			status := "healthy"
			if known > 0 && stats.AvailabilityPct < svc.SLATarget {
				status = "breached"
			} else if remaining < allowed*0.25 {
				status = "at_risk"
			}
			sla = map[string]any{"target_pct": svc.SLATarget, "allowed_down_seconds": allowed, "remaining_seconds": remaining, "status": status}
		}

		chOut := []map[string]any{}
		warnings := []string{}
		for _, c := range channels {
			down, deg := bizprocess.ChannelSeconds(timeline[c.ID])
			upCount, paused := 0, 0
			for _, m := range c.Members {
				if m.CountsAsUp {
					upCount++
				}
				if m.State == "paused" {
					paused++
				}
			}
			var upPct any
			if len(c.Members) > 0 {
				upPct = float64(upCount) / float64(len(c.Members)) * 100
			}
			chOut = append(chOut, map[string]any{
				"id": c.ID, "name": c.Name, "state": c.State,
				"warning_threshold_pct": c.Warn, "error_threshold_pct": c.Err,
				"members": c.Members, "up_pct": upPct,
				"down_seconds": down, "degraded_seconds": deg,
			})
			if paused > 0 {
				warnings = append(warnings, fmt.Sprintf("%d objek di channel \"%s\" sedang di-pause. PRTG menghitung objek yang di-pause sebagai Down, sehingga status proses ini ikut turun walaupun tidak ada gangguan nyata. Keluarkan objek tersebut dari channel di PRTG jika memang sudah tidak dipakai.", paused, c.Name))
			}
			if c.ID != bizprocess.GlobalChannel && c.Err != nil && *c.Err <= 0 {
				warnings = append(warnings, fmt.Sprintf("Channel \"%s\" memakai batas error 0%%, sehingga channel ini tidak akan pernah berstatus Down (paling parah Warning).", c.Name))
			}
		}
		if bp.DefStatus != "ok" {
			warnings = append(warnings, "Definisi channel tidak bisa dibaca dari PRTG pada sinkronisasi terakhir; menampilkan definisi terakhir yang tersimpan.")
		}
		switch {
		case bp.ServiceID == nil:
			warnings = append(warnings, "Belum terhubung ke business service, sehingga kerugian finansial belum dihitung.")
		case profileSource == "organization_default":
			warnings = append(warnings, "Business service ini belum punya financial profile sendiri; perhitungan memakai profil default organisasi.")
		case profileSource == "none":
			warnings = append(warnings, "Tidak ada financial profile aktif untuk service ini, sehingga kerugian dihitung Rp 0.")
		}
		if bp.isRemoved() {
			warnings = append(warnings, fmt.Sprintf("Sensor ini sudah tidak ditemukan di PRTG sejak %s.", bp.RemovedAt.Format("02 Jan 2006 15:04 MST")))
		}

		epOut := []map[string]any{}
		for i := len(episodes) - 1; i >= 0 && len(epOut) < 50; i-- {
			e := episodes[i]
			causes := []map[string]any{}
			for _, c := range e.Causes {
				causes = append(causes, map[string]any{"channel_id": c.ChannelID, "channel_name": channelName[c.ChannelID], "down_seconds": c.DownSeconds, "degraded_seconds": c.DegradedSeconds})
			}
			epOut = append(epOut, map[string]any{
				"start": e.Start, "end": e.End, "ongoing": e.Ongoing, "duration_seconds": e.Seconds(),
				"down_seconds": e.DownSeconds, "degraded_seconds": e.DegradedSeconds, "worst": e.Worst,
				"causes": causes, "loss": e.Loss,
			})
		}

		var svcOut any
		if svc != nil {
			svcOut = map[string]any{"id": svc.ID, "name": svc.Name, "criticality": svc.Criticality, "owner_name": svc.Owner, "affected_users": svc.AffectedUsers, "sla_target_pct": svc.SLATarget, "rto_minutes": svc.RTOMinutes}
		}
		var prtgStats any
		if bp.UptimePct != nil {
			prtgStats = map[string]any{"uptime_pct": bp.UptimePct, "downtime_pct": bp.DowntimePct, "since": bp.StatsSince}
		}
		items = append(items, map[string]any{
			"id": bp.ID, "prtg_sensor_id": bp.PRTGSensorID, "name": bp.Name, "device_name": bp.Device, "group_name": bp.Group,
			"state": bp.State, "message": bp.Message, "last_synced_at": bp.LastSynced, "history_until": bp.HistoryUntil,
			"definition_status": bp.DefStatus, "removed_at": bp.RemovedAt, "scan_interval_sec": bp.ScanInterval,
			"prtg": prtgStats, "service": svcOut, "financial_profile_source": profileSource,
			"degraded_impact_pct": bp.DegradedPct,
			"rates": map[string]any{
				"revenue_per_hour": rates.RevenuePerHour, "productivity_per_hour": rates.ProductivityPerHour,
				"operational_per_hour": rates.OperationalPerHour, "full_per_hour": rates.FullPerHour(),
				"degraded_factor": rates.DegradedFactor, "recovery_fixed": rates.RecoveryFixed,
				"penalty_fixed": rates.PenaltyFixed, "rto_seconds": rates.RTOSeconds,
			},
			"current": current, "stats": stats, "sla": sla,
			"channels": chOut, "episodes": epOut, "warnings": warnings,
		})

		if bp.isRemoved() {
			continue
		}
		total++
		switch bp.State {
		case bizprocess.Down:
			down++
		case bizprocess.Warning:
			degraded++
		}
		if bp.ServiceID == nil {
			unlinked++
		}
		totalRate += lossPerHour
		totalLoss += stats.Loss.Total
		if stats.UpSeconds+stats.WarningSeconds+stats.DownSeconds > 0 {
			availSum += stats.AvailabilityPct
			availN++
		}
	}
	summary["total"], summary["down"], summary["degraded"], summary["unlinked"] = total, down, degraded, unlinked
	summary["current_loss_per_hour"] = totalRate
	summary["period_loss"] = totalLoss
	if availN > 0 {
		summary["avg_availability_pct"] = availSum / float64(availN)
	}

	svcList := []map[string]any{}
	for _, s := range services {
		svcList = append(svcList, map[string]any{"id": s.ID, "name": s.Name, "criticality": s.Criticality})
	}
	sort.Slice(svcList, func(i, j int) bool { return svcList[i]["name"].(string) < svcList[j]["name"].(string) })

	stale := lastSync == nil || now.Sub(*lastSync) > time.Duration(2*pollSec+120)*time.Second
	jsonOut(w, 200, map[string]any{
		"period":   map[string]any{"key": periodKey, "from": from, "to": now},
		"sync":     map[string]any{"last_synced_at": lastSync, "poll_interval_sec": pollSec, "stale": stale},
		"summary":  summary,
		"items":    items,
		"services": svcList,
	})
}

func (b bpRow) isRemoved() bool { return b.RemovedAt != nil }

func (a *app) loadBusinessProcesses(ctx context.Context, org string) ([]bpRow, error) {
	rows, err := a.pool.Query(ctx, `
		select id, prtg_sensor_id, sensor_uuid, name, coalesce(device_name,''), coalesce(group_name,''), state, coalesce(message,''),
			scan_interval_sec, prtg_uptime_pct::float8, prtg_downtime_pct::float8, prtg_stats_since, business_service_id,
			degraded_impact_pct::float8, definition_status, history_synced_until, last_synced_at, removed_at
		from business_process_sensors where organization_id=$1
		order by removed_at nulls first, name`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []bpRow{}
	for rows.Next() {
		var b bpRow
		if err := rows.Scan(&b.ID, &b.PRTGSensorID, &b.SensorUUID, &b.Name, &b.Device, &b.Group, &b.State, &b.Message,
			&b.ScanInterval, &b.UptimePct, &b.DowntimePct, &b.StatsSince, &b.ServiceID,
			&b.DegradedPct, &b.DefStatus, &b.HistoryUntil, &b.LastSynced, &b.RemovedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (a *app) loadBPChannels(ctx context.Context, bpID uuid.UUID) ([]bpChannelRow, error) {
	rows, err := a.pool.Query(ctx, `select prtg_channel_id, name, state, warning_threshold_pct::float8, error_threshold_pct::float8, members
		from business_process_channels where business_process_id=$1 order by prtg_channel_id`, bpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []bpChannelRow{}
	for rows.Next() {
		var c bpChannelRow
		var members []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.State, &c.Warn, &c.Err, &members); err != nil {
			return nil, err
		}
		c.Members = []bizprocess.Member{}
		_ = json.Unmarshal(members, &c.Members)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *app) loadBPIntervals(ctx context.Context, bpID uuid.UUID, from time.Time) (map[int][]bizprocess.Interval, error) {
	rows, err := a.pool.Query(ctx, `select prtg_channel_id, state, started_at, ended_at from business_process_intervals
		where business_process_id=$1 and ended_at >= $2 order by prtg_channel_id, started_at`, bpID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]bizprocess.Interval{}
	for rows.Next() {
		var iv bizprocess.Interval
		if err := rows.Scan(&iv.ChannelID, &iv.State, &iv.Start, &iv.End); err != nil {
			return nil, err
		}
		iv.Start, iv.End = iv.Start.UTC(), iv.End.UTC()
		out[iv.ChannelID] = append(out[iv.ChannelID], iv)
	}
	return out, rows.Err()
}

func (a *app) loadBPServices(ctx context.Context, org string) (map[uuid.UUID]bpService, error) {
	rows, err := a.pool.Query(ctx, `select id, name, criticality, coalesce(owner_name,''), coalesce(affected_users,0),
		coalesce(sla_target_pct,99.9)::float8, coalesce(rto_minutes,60) from business_services where organization_id=$1`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]bpService{}
	for rows.Next() {
		var s bpService
		if err := rows.Scan(&s.ID, &s.Name, &s.Criticality, &s.Owner, &s.AffectedUsers, &s.SLATarget, &s.RTOMinutes); err != nil {
			return nil, err
		}
		out[s.ID] = s
	}
	return out, rows.Err()
}

// loadBPProfiles returns active financial profiles per service plus the
// organization default (business_service_id null), mirroring the worker's
// fallback when a service has no profile of its own.
func (a *app) loadBPProfiles(ctx context.Context, org string) (map[uuid.UUID]bpProfile, *bpProfile, error) {
	rows, err := a.pool.Query(ctx, `select business_service_id, hourly_revenue::float8, service_dependency::float8, loss_probability::float8,
		operational_cost_per_hour::float8, coalesce(penalty_config->>'fixed','0')::float8, coalesce(recovery_config->>'fixed','0')::float8,
		coalesce(affected_employees,0), coalesce(avg_employee_cost_per_hour,0)::float8
		from financial_profiles where organization_id=$1 and valid_to is null`, org)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]bpProfile{}
	var orgDefault *bpProfile
	for rows.Next() {
		var sid *uuid.UUID
		var p bpProfile
		if err := rows.Scan(&sid, &p.HourlyRevenue, &p.Dependency, &p.LossProbability, &p.Operational, &p.Penalty, &p.Recovery, &p.Employees, &p.EmployeeCost); err != nil {
			return nil, nil, err
		}
		if sid == nil {
			pp := p
			orgDefault = &pp
		} else {
			out[*sid] = p
		}
	}
	return out, orgDefault, rows.Err()
}

type bpUpdateInput struct {
	BusinessServiceID *string  `json:"business_service_id"`
	DegradedImpactPct *float64 `json:"degraded_impact_pct"`
}

// updateBusinessProcess links a process to a business service and/or sets
// its degraded impact. Re-linking also moves the process sensor's service
// mapping so incidents on it are attributed to the new service.
func (a *app) updateBusinessProcess(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/business-processes/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid business process id"})
		return
	}
	org := orgID(r)
	var in bpUpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer tx.Rollback(ctx)

	var oldService *uuid.UUID
	var sensorUUID *uuid.UUID
	var oldPct float64
	err = tx.QueryRow(ctx, `select business_service_id, sensor_uuid, degraded_impact_pct::float8 from business_process_sensors
		where organization_id=$1 and id=$2 for update`, org, id).Scan(&oldService, &sensorUUID, &oldPct)
	if err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "business process not found"})
		return
	} else if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}

	newService := oldService
	if in.BusinessServiceID != nil {
		if *in.BusinessServiceID == "" {
			newService = nil
		} else {
			sid, err := uuid.Parse(*in.BusinessServiceID)
			if err != nil {
				jsonOut(w, 400, map[string]any{"error": "invalid business_service_id"})
				return
			}
			var exists bool
			if err := tx.QueryRow(ctx, `select exists(select 1 from business_services where organization_id=$1 and id=$2)`, org, sid).Scan(&exists); err != nil || !exists {
				jsonOut(w, 400, map[string]any{"error": "business service not found"})
				return
			}
			newService = &sid
		}
	}
	pct := oldPct
	if in.DegradedImpactPct != nil {
		if *in.DegradedImpactPct < 0 || *in.DegradedImpactPct > 100 {
			jsonOut(w, 400, map[string]any{"error": "degraded_impact_pct must be between 0 and 100"})
			return
		}
		pct = *in.DegradedImpactPct
	}
	if _, err := tx.Exec(ctx, `update business_process_sensors set business_service_id=$3, degraded_impact_pct=$4 where organization_id=$1 and id=$2`, org, id, newService, pct); err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if sensorUUID != nil {
		if err := bizprocess.LinkSensorToService(ctx, tx, org, *sensorUUID, oldService, newService); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(ctx, a.pool, org, "admin", "business_process.update", "business_process", &id,
		map[string]any{"business_service_id": oldService, "degraded_impact_pct": oldPct},
		map[string]any{"business_service_id": newService, "degraded_impact_pct": pct})
	jsonOut(w, 200, map[string]any{"id": id, "business_service_id": newService, "degraded_impact_pct": pct})
}
