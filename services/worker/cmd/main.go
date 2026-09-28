package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/example/bia-platform/internal/financial"
	"github.com/example/bia-platform/internal/incident"
	"github.com/example/bia-platform/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type worker struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func main() {
	ctx := context.Background()
	pool, err := db.NewPool(ctx)
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	w := &worker{pool: pool, log: slog.New(slog.NewJSONHandler(os.Stdout, nil))}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		panic(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "listen new_technical_event"); err != nil {
		panic(err)
	}
	w.log.Info("worker listening", "component", "worker")
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			w.log.Error("listen error", "error", err)
			time.Sleep(time.Second)
			continue
		}
		var id string
		_, _ = fmt.Sscanf(n.Payload, "%s", &id)
		id = stringBeforePipe(n.Payload)
		w.processEvent(ctx, id)
	}
}
func stringBeforePipe(s string) string {
	for i, c := range s {
		if c == '|' {
			return s[:i]
		}
	}
	return s
}
func (w *worker) processEvent(ctx context.Context, eventID string) {
	var org, sensorID, eventType, state string
	var occurred time.Time
	err := w.pool.QueryRow(ctx, `select organization_id,prtg_sensor_id,event_type,state,occurred_at from technical_events where id=$1`, eventID).Scan(&org, &sensorID, &eventType, &state, &occurred)
	if err != nil {
		return
	}
	var incID uuid.UUID
	var status, severity string
	var started time.Time
	err = w.pool.QueryRow(ctx, `select id,status,severity,started_at from incidents where organization_id=$1 and prtg_sensor_id=$2 and status in ('OPEN','ACKNOWLEDGED') order by started_at desc limit 1`, org, sensorID).Scan(&incID, &status, &severity, &started)
	if err == pgx.ErrNoRows && state == "down" {
		dep := w.serviceDependency(ctx, org, sensorID)
		sev := incident.SeverityFor(dep, 0)
		if err = w.pool.QueryRow(ctx, `insert into incidents(organization_id,prtg_sensor_id,status,severity,started_at) values($1,$2,'OPEN',$3,$4) returning id`, org, sensorID, string(sev), occurred).Scan(&incID); err == nil {
			_, _ = w.pool.Exec(ctx, `insert into incident_events(organization_id,incident_id,event_type,actor,metadata) values($1,$2,'created','system',$3)`, org, incID, `{"source":"collector"}`)
			w.correlateIncident(ctx, org, incID, occurred)
		}
	}
	if err == nil && state == "up" && status == string(incident.Acknowledged) {
		dur := incident.Duration(started, occurred)
		dep := w.serviceDependency(ctx, org, sensorID)
		sev := incident.SeverityFor(dep, float64(dur))
		_, _ = w.pool.Exec(ctx, `update incidents set status='RESOLVED',severity=$1,ended_at=$2,duration_seconds=$3 where id=$4`, string(sev), occurred, dur, incID)
		_, _ = w.pool.Exec(ctx, `insert into incident_events(organization_id,incident_id,event_type,actor,metadata) values($1,$2,'resolved','system',$3)`, org, incID, fmt.Sprintf(`{"duration_seconds":%d}`, dur))
		w.calculateImpact(ctx, org, incID, sensorID, dur, occurred)
	}
	if err == nil && state == "down" {
		dur := incident.Duration(started, occurred)
		dep := w.serviceDependency(ctx, org, sensorID)
		sev := incident.SeverityFor(dep, float64(dur))
		_, _ = w.pool.Exec(ctx, `update incidents set severity=$1 where id=$2`, string(sev), incID)
	}
}

// correlateIncident groups a newly-created incident with any other
// currently-open incidents that started within incident.CorrelationWindow
// of it — a temporal heuristic for likely shared-root-cause "incident
// storms" (e.g. one switch failing takes many dependent sensors down at
// once), since the platform has no real network topology graph.
func (w *worker) correlateIncident(ctx context.Context, org string, incID uuid.UUID, occurred time.Time) {
	windowStart := occurred.Add(-incident.CorrelationWindow)
	windowEnd := occurred.Add(incident.CorrelationWindow)
	rows, err := w.pool.Query(ctx, `
		select id, correlation_group_id from incidents
		where organization_id=$1 and id!=$2 and status in ('OPEN','ACKNOWLEDGED')
		and started_at between $3 and $4`, org, incID, windowStart, windowEnd)
	if err != nil {
		w.log.Error("correlation lookup failed", "organization_id", org, "component", "worker", "error", err)
		return
	}
	var groupID uuid.UUID
	var toBackfill []uuid.UUID
	for rows.Next() {
		var otherID uuid.UUID
		var existingGroup *uuid.UUID
		if err := rows.Scan(&otherID, &existingGroup); err != nil {
			continue
		}
		if existingGroup != nil {
			groupID = *existingGroup
		} else {
			toBackfill = append(toBackfill, otherID)
		}
	}
	rows.Close()
	if groupID == uuid.Nil && len(toBackfill) == 0 {
		return // no other incidents nearby in time - stays uncorrelated
	}
	if groupID == uuid.Nil {
		groupID = uuid.New()
	}
	_, _ = w.pool.Exec(ctx, `update incidents set correlation_group_id=$1 where id=$2`, groupID, incID)
	for _, id := range toBackfill {
		_, _ = w.pool.Exec(ctx, `update incidents set correlation_group_id=$1 where id=$2`, groupID, id)
	}
}

func (w *worker) serviceDependency(ctx context.Context, org, sensor string) float64 {
	var x float64
	_ = w.pool.QueryRow(ctx, `select coalesce(max(dependency_weight),1) from service_sensor_mapping where organization_id=$1 and prtg_sensor_id=$2`, org, sensor).Scan(&x)
	if x <= 0 || math.IsNaN(x) {
		return 1
	}
	return x
}
func (w *worker) calculateImpact(ctx context.Context, org string, incID uuid.UUID, sensor string, dur int64, ended time.Time) {
	var serviceID *uuid.UUID
	var dependency decimal.Decimal
	_ = w.pool.QueryRow(ctx, `select business_service_id,dependency_weight from service_sensor_mapping where organization_id=$1 and prtg_sensor_id=$2 order by dependency_weight desc limit 1`, org, sensor).Scan(&serviceID, &dependency)
	var hourly, txh, atv, cost, pExp, rCost decimal.Decimal
	q := `select hourly_revenue,coalesce(transactions_per_hour,0),coalesce(avg_transaction_value,0),service_dependency,loss_probability,operational_cost_per_hour,coalesce(penalty_config->>'fixed', '0')::numeric,coalesce(recovery_config->>'fixed','0')::numeric from financial_profiles where organization_id=$1 and ($2::uuid is null and business_service_id is null or business_service_id=$2) and valid_from <= $3 and (valid_to is null or $3 < valid_to) order by business_service_id nulls last, valid_from desc limit 1`
	var dep, lp decimal.Decimal
	if err := w.pool.QueryRow(ctx, q, org, serviceID, ended).Scan(&hourly, &txh, &atv, &dep, &lp, &cost, &pExp, &rCost); err != nil {
		dep = decimal.NewFromFloat(dependency.InexactFloat64())
		lp = decimal.NewFromInt(1)
	}
	model := financial.Model{Version: "2026.1", Components: []financial.Component{{Key: "revenue_loss", Expr: "hourly_revenue * (duration_seconds/3600) * service_dependency * loss_probability"}, {Key: "operational_cost", Expr: "operational_cost_per_hour * (duration_seconds/3600)"}, {Key: "penalty_exposure", Expr: "penalty_exposure"}, {Key: "recovery_cost", Expr: "recovery_cost"}}, TotalExpr: "revenue_loss + operational_cost + penalty_exposure + recovery_cost"}
	in := financial.InputSnapshot{DurationSeconds: dur, HourlyRevenue: hourly, TransactionsPerHour: txh, AvgTransactionValue: atv, ServiceDependency: dep, LossProbability: lp, OperationalCostPerHour: cost, PenaltyExposure: pExp, RecoveryCost: rCost}
	res, err := financial.Calculate(model, in)
	if err != nil {
		w.log.Error("financial calculation failed", "organization_id", org, "component", "worker", "error", err)
		return
	}
	var modelID uuid.UUID
	if err = w.pool.QueryRow(ctx, `select id from impact_models where version=$1`, model.Version).Scan(&modelID); err != nil {
		return
	}
	snap, _ := json.Marshal(in)
	breakdown, _ := json.Marshal(res.Breakdown)
	_, _ = w.pool.Exec(ctx, `insert into impact_calculations(organization_id,incident_id,impact_model_id,input_snapshot,result_breakdown,total_impact,confidence) values($1,$2,$3,$4,$5,$6,$7) on conflict(incident_id,impact_model_id) do nothing`, org, incID, modelID, snap, breakdown, res.Total, 0.9)
}
