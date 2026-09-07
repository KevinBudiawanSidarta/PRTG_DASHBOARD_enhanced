package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/example/bia-platform/internal/ingestion"
	"github.com/example/bia-platform/internal/platform/db"
	"github.com/example/bia-platform/internal/prtg"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()
	pool, err := db.NewPool(ctx)
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	org := os.Getenv("ORGANIZATION_ID")
	instance := os.Getenv("PRTG_INSTANCE_ID")
	if _, err := uuid.Parse(org); err != nil {
		panic("ORGANIZATION_ID must be a UUID")
	}
	if _, err := uuid.Parse(instance); err != nil {
		panic("PRTG_INSTANCE_ID must be a UUID")
	}
	interval := 60
	if v, _ := strconv.Atoi(os.Getenv("POLL_INTERVAL_SEC")); v > 0 {
		interval = v
	}
	client := prtg.NewClient()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	log.Info("collector started", "component", "collector", "organization_id", org, "prtg_server", client.BaseURL)
	poll := func() {
		start := time.Now()
		sensors, err := client.Sensors(ctx)
		if err != nil {
			log.Error("PRTG poll failed", "component", "collector", "organization_id", org, "error", err)
			return
		}
		for _, s := range sensors {
			state := prtg.NormalizeState(s.Status)
			_, err := ingestion.StoreEvent(ctx, pool, ingestion.EventInput{OrganizationID: org, PRTGInstanceID: instance, PRTGSensorID: s.ID, EventType: "state_change", State: state, OccurredAt: time.Now().UTC(), RawPayload: s, DeviceName: s.Device, SensorName: s.Sensor, OnlyIfStateChanged: true})
			if err != nil {
				log.Error("event ingestion failed", "component", "collector", "organization_id", org, "sensor_id", s.ID, "error", err)
			}
		}
		_, _ = pool.Exec(ctx, `update prtg_instances set last_sync_at=now(),sync_status='idle' where id=$1 and organization_id=$2`, instance, org)
		log.Info("PRTG poll complete", "component", "collector", "organization_id", org, "sensor_count", len(sensors), "latency_ms", time.Since(start).Milliseconds())
	}
	poll()
	t := time.NewTicker(time.Duration(interval) * time.Second)
	defer t.Stop()
	for range t.C {
		poll()
	}
}
