package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/bia-platform/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventInput struct {
	OrganizationID     string
	PRTGInstanceID     string
	PRTGSensorID       string
	EventType          string
	State              string
	OccurredAt         time.Time
	RawPayload         any
	DeviceName         string
	SensorName         string
	OnlyIfStateChanged bool
}

type Result struct {
	Inserted bool
	EventID  uuid.UUID
}

func StoreEvent(ctx context.Context, pool *pgxpool.Pool, in EventInput) (Result, error) {
	orgID, err := uuid.Parse(in.OrganizationID)
	if err != nil {
		return Result{}, fmt.Errorf("invalid organization_id")
	}
	instanceID, err := uuid.Parse(in.PRTGInstanceID)
	if err != nil {
		return Result{}, fmt.Errorf("invalid prtg_instance_id")
	}
	if in.EventType == "" {
		in.EventType = "state_change"
	}
	if in.State == "" {
		in.State = "unknown"
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = time.Now().UTC()
	}
	if in.OnlyIfStateChanged {
		var previous string
		err := pool.QueryRow(ctx, `select coalesce(last_known_state,'unknown') from prtg_sensors where organization_id=$1 and prtg_instance_id=$2 and prtg_sensor_id=$3`, orgID, instanceID, in.PRTGSensorID).Scan(&previous)
		if err == nil && previous == in.State {
			return Result{Inserted: false}, nil
		}
	}
	fingerprint := Fingerprint(in.PRTGInstanceID, in.PRTGSensorID, in.OccurredAt, in.EventType)
	raw, err := json.Marshal(in.RawPayload)
	if err != nil {
		return Result{}, err
	}
	var sensorUUID uuid.UUID
	err = pool.QueryRow(ctx, `insert into prtg_sensors(organization_id,prtg_instance_id,prtg_sensor_id,device_name,sensor_name,last_known_state,updated_at)
		values($1,$2,$3,$4,$5,$6,now())
		on conflict(prtg_instance_id,prtg_sensor_id) do update set device_name=coalesce(excluded.device_name,prtg_sensors.device_name),sensor_name=coalesce(excluded.sensor_name,prtg_sensors.sensor_name),last_known_state=excluded.last_known_state,updated_at=now()
		returning id`, orgID, instanceID, in.PRTGSensorID, nullable(in.DeviceName), nullable(in.SensorName), in.State).Scan(&sensorUUID)
	if err != nil {
		return Result{}, err
	}
	var id uuid.UUID
	err = pool.QueryRow(ctx, `insert into technical_events(organization_id,prtg_sensor_id,event_type,state,occurred_at,fingerprint,raw_payload)
		values($1,$2,$3,$4,$5,$6,$7) on conflict(fingerprint,occurred_at) do nothing returning id`, orgID, sensorUUID, in.EventType, in.State, in.OccurredAt.UTC(), fingerprint, raw).Scan(&id)
	if err != nil {
		if db.IsNoRows(err) {
			return Result{Inserted: false}, nil
		}
		return Result{}, err
	}
	return Result{Inserted: true, EventID: id}, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
