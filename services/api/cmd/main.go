package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/bia-platform/internal/aiagent"
	"github.com/example/bia-platform/internal/analytics"
	"github.com/example/bia-platform/internal/audit"
	"github.com/example/bia-platform/internal/auth"
	"github.com/example/bia-platform/internal/financial"
	"github.com/example/bia-platform/internal/incident"
	"github.com/example/bia-platform/internal/ingestion"
	"github.com/example/bia-platform/internal/platform/db"
	"github.com/example/bia-platform/internal/prtg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type app struct{ pool *pgxpool.Pool }

func main() {
	ctx := context.Background()
	p, err := db.NewPool(ctx)
	if err != nil {
		panic(err)
	}
	defer p.Close()
	a := &app{p}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, map[string]any{"status": "ok"}) })
	mux.Handle("GET /api/v1/dashboard/summary", auth.WithOrg(http.HandlerFunc(a.summary)))
	mux.Handle("GET /api/v1/incidents", auth.WithOrg(http.HandlerFunc(a.incidents)))
	mux.Handle("GET /api/v1/incidents/", auth.WithOrg(http.HandlerFunc(a.incidentByID)))
	mux.Handle("POST /api/v1/incidents/", auth.WithOrg(http.HandlerFunc(a.incidentAction)))
	mux.Handle("GET /api/v1/technical-events", auth.WithOrg(http.HandlerFunc(a.events)))
	mux.Handle("GET /api/v1/services", auth.WithOrg(http.HandlerFunc(a.services)))
	mux.Handle("POST /api/v1/services", auth.WithOrg(http.HandlerFunc(a.createService)))
	mux.Handle("GET /api/v1/services/{id}/mappings", auth.WithOrg(http.HandlerFunc(a.serviceMappings)))
	mux.Handle("PUT /api/v1/services/{id}/mappings", auth.WithOrg(http.HandlerFunc(a.replaceServiceMappings)))
	mux.Handle("PUT /api/v1/services/{id}", auth.WithOrg(http.HandlerFunc(a.updateService)))
	mux.Handle("DELETE /api/v1/services/{id}", auth.WithOrg(http.HandlerFunc(a.deleteService)))
	mux.Handle("GET /api/v1/sensors", auth.WithOrg(http.HandlerFunc(a.sensors)))
	mux.Handle("GET /api/v1/financial-profiles", auth.WithOrg(http.HandlerFunc(a.financialProfiles)))
	mux.Handle("POST /api/v1/financial-profiles", auth.WithOrg(http.HandlerFunc(a.createFinancialProfile)))
	mux.Handle("PUT /api/v1/financial-profiles/", auth.WithOrg(http.HandlerFunc(a.updateFinancialProfile)))
	mux.Handle("DELETE /api/v1/financial-profiles/", auth.WithOrg(http.HandlerFunc(a.deleteFinancialProfile)))
	mux.Handle("GET /api/v1/knowledge-base", auth.WithOrg(http.HandlerFunc(a.listKnowledgeBase)))
	mux.Handle("POST /api/v1/knowledge-base", auth.WithOrg(http.HandlerFunc(a.createKnowledgeBase)))
	mux.Handle("PUT /api/v1/knowledge-base/", auth.WithOrg(http.HandlerFunc(a.updateKnowledgeBase)))
	mux.Handle("DELETE /api/v1/knowledge-base/", auth.WithOrg(http.HandlerFunc(a.deleteKnowledgeBase)))
	mux.Handle("POST /api/v1/ai/analyze", auth.WithOrg(http.HandlerFunc(a.aiAnalyze)))
	mux.Handle("POST /api/v1/ai/chat", auth.WithOrg(http.HandlerFunc(a.aiChat)))
	// ── BIA Endpoints ──
	mux.Handle("GET /api/v1/bia/executive-summary", auth.WithOrg(http.HandlerFunc(a.biaExecutiveSummary)))
	mux.Handle("GET /api/v1/bia/service-impact", auth.WithOrg(http.HandlerFunc(a.biaServiceImpact)))
	mux.Handle("GET /api/v1/bia/incident-priority", auth.WithOrg(http.HandlerFunc(a.biaIncidentPriority)))
	mux.Handle("GET /api/v1/bia/incident-correlation", auth.WithOrg(http.HandlerFunc(a.biaIncidentCorrelation)))
	mux.Handle("GET /api/v1/bia/sla-analysis", auth.WithOrg(http.HandlerFunc(a.biaSLAAnalysis)))
	mux.Handle("GET /api/v1/bia/financial-impact", auth.WithOrg(http.HandlerFunc(a.biaFinancialImpact)))
	mux.Handle("GET /api/v1/business-processes", auth.WithOrg(http.HandlerFunc(a.listBusinessProcesses)))
	mux.Handle("PUT /api/v1/business-processes/{id}", auth.WithOrg(http.HandlerFunc(a.updateBusinessProcess)))
	mux.Handle("GET /api/v1/impact-matrix", auth.WithOrg(http.HandlerFunc(a.listImpactMatrix)))
	mux.Handle("POST /api/v1/impact-matrix", auth.WithOrg(http.HandlerFunc(a.createImpactMatrix)))
	mux.Handle("PUT /api/v1/impact-matrix/", auth.WithOrg(http.HandlerFunc(a.updateImpactMatrix)))
	mux.Handle("DELETE /api/v1/impact-matrix/", auth.WithOrg(http.HandlerFunc(a.deleteImpactMatrix)))
	mux.HandleFunc("POST /internal/prtg/events", a.prtgWebhook)
	srv := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: withJSON(withCORS(mux)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	fmt.Printf("API listening on %s\n", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Organization-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func orgID(r *http.Request) string { c, _ := auth.ClaimsFromContext(r.Context()); return c.OrgID }

func (a *app) summary(w http.ResponseWriter, r *http.Request) {
	s, err := analytics.SummaryFor(r.Context(), a.pool, orgID(r))
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	jsonOut(w, 200, s)
}

func (a *app) incidents(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	limit := 25
	if x, _ := strconv.Atoi(r.URL.Query().Get("limit")); x > 0 && x <= 100 {
		limit = x
	}
	cursor := r.URL.Query().Get("cursor")
	var rows pgx.Rows
	var err error
	base := `select i.id,i.status,i.severity,i.started_at,i.ended_at,coalesce(i.duration_seconds,extract(epoch from (now()-i.started_at))::int),s.device_name,s.sensor_name,coalesce(bs.name,''),coalesce(bs.criticality,'P4'),coalesce(ic.total_impact,0),coalesce(im.version,''),i.correlation_group_id,(select count(*) from incidents i2 where i2.organization_id=$1 and i2.correlation_group_id=i.correlation_group_id) from incidents i join prtg_sensors s on s.id=i.prtg_sensor_id left join lateral (select b.id,b.name,b.criticality from business_services b join service_sensor_mapping m on m.business_service_id=b.id where m.organization_id=$1 and m.prtg_sensor_id=i.prtg_sensor_id order by m.dependency_weight desc limit 1) bs on true left join lateral (select c.total_impact,c.impact_model_id from impact_calculations c where c.organization_id=$1 and c.incident_id=i.id order by c.calculated_at desc limit 1) ic on true left join impact_models im on im.id=ic.impact_model_id where i.organization_id=$1`
	args := []any{org}
	if cursor != "" {
		t, id, ok := decodeCursor(cursor)
		if !ok {
			jsonOut(w, 400, map[string]any{"error": "invalid cursor"})
			return
		}
		base += ` and (i.started_at < $2 or (i.started_at=$2 and i.id < $3))`
		args = append(args, t, id)
	}
	base += fmt.Sprintf(` order by i.started_at desc,i.id desc limit $%d`, len(args)+1)
	args = append(args, limit+1)
	rows, err = a.pool.Query(r.Context(), base, args...)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	var lastTime time.Time
	var lastID uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var status, severity string
		var started, ended *time.Time
		var dur int
		var device, sensor, service, criticality, model string
		var impact float64
		var correlationGroup *uuid.UUID
		var groupSize int
		if err := rows.Scan(&id, &status, &severity, &started, &ended, &dur, &device, &sensor, &service, &criticality, &impact, &model, &correlationGroup, &groupSize); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		m := map[string]any{"id": id, "status": status, "severity": severity, "started_at": started, "ended_at": ended, "duration_seconds": dur, "sensor": map[string]any{"device": device, "name": sensor}, "service": map[string]any{"name": service, "criticality": criticality}, "total_impact": impact, "model_version": model, "correlation_group_id": correlationGroup, "correlated_incident_count": groupSize}
		items = append(items, m)
		if len(items) == limit+1 {
			lastTime = *started
			lastID = id
			items = items[:limit]
			break
		} else {
			lastTime = *started
			lastID = id
		}
	}
	next := ""
	if len(items) == limit {
		next = encodeCursor(lastTime, lastID)
	}
	jsonOut(w, 200, map[string]any{"items": items, "next_cursor": next})
}
func encodeCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}
func decodeCursor(s string) (time.Time, uuid.UUID, bool) {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return time.Time{}, uuid.Nil, false
	}
	p := strings.Split(string(b), "|")
	if len(p) != 2 {
		return time.Time{}, uuid.Nil, false
	}
	t, e := time.Parse(time.RFC3339Nano, p[0])
	if e != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, e := uuid.Parse(p[1])
	return t, id, e == nil
}

func (a *app) incidentByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/incidents/")
	idStr = strings.TrimSuffix(idStr, "/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid id"})
		return
	}
	org := orgID(r)
	var status, severity, device, sensor string
	var started, ended *time.Time
	var dur int
	var serviceID *uuid.UUID
	var service, criticality string
	var correlationGroup *uuid.UUID
	err = a.pool.QueryRow(r.Context(), `select i.status,i.severity,s.device_name,s.sensor_name,i.started_at,i.ended_at,coalesce(i.duration_seconds,extract(epoch from(now()-i.started_at))::int),bs.id,coalesce(bs.name,''),coalesce(bs.criticality,'P4'),i.correlation_group_id from incidents i join prtg_sensors s on s.id=i.prtg_sensor_id left join lateral(select b.id,b.name,b.criticality from business_services b join service_sensor_mapping m on m.business_service_id=b.id where m.organization_id=$1 and m.prtg_sensor_id=i.prtg_sensor_id order by m.dependency_weight desc limit 1) bs on true where i.organization_id=$1 and i.id=$2`, org, id).Scan(&status, &severity, &device, &sensor, &started, &ended, &dur, &serviceID, &service, &criticality, &correlationGroup)
	if err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "incident not found"})
		return
	}
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	var correlated []map[string]any
	if correlationGroup != nil {
		crows, err := a.pool.Query(r.Context(), `select i2.id,i2.severity,i2.status,s2.device_name,s2.sensor_name,coalesce(bs2.name,'') from incidents i2 join prtg_sensors s2 on s2.id=i2.prtg_sensor_id left join lateral(select b.name from business_services b join service_sensor_mapping m on m.business_service_id=b.id where m.organization_id=$1 and m.prtg_sensor_id=i2.prtg_sensor_id order by m.dependency_weight desc limit 1) bs2 on true where i2.organization_id=$1 and i2.correlation_group_id=$2 and i2.id!=$3 order by i2.started_at`, org, correlationGroup, id)
		if err == nil {
			for crows.Next() {
				var cid uuid.UUID
				var csev, cstatus, cdev, csensor, csvc string
				if crows.Scan(&cid, &csev, &cstatus, &cdev, &csensor, &csvc) == nil {
					correlated = append(correlated, map[string]any{"id": cid, "severity": csev, "status": cstatus, "sensor": map[string]any{"device": cdev, "name": csensor}, "service": csvc})
				}
			}
			crows.Close()
		}
	}
	if correlated == nil {
		correlated = []map[string]any{}
	}
	var model, breakdownJSON, snapshotJSON string
	var total, confidence float64
	var calcID *uuid.UUID
	_ = serviceID
	err = a.pool.QueryRow(r.Context(), `select c.id,im.version,c.result_breakdown,c.input_snapshot,c.total_impact,coalesce(c.confidence,0) from impact_calculations c join impact_models im on im.id=c.impact_model_id where c.organization_id=$1 and c.incident_id=$2 order by c.calculated_at desc limit 1`, org, id).Scan(&calcID, &model, &breakdownJSON, &snapshotJSON, &total, &confidence)
	_ = calcID
	impact := map[string]any{"model_version": model, "breakdown": json.RawMessage(breakdownJSON), "total_impact": total, "confidence": confidence, "input_snapshot": json.RawMessage(snapshotJSON)}
	if err == pgx.ErrNoRows {
		impact = nil
	}
	jsonOut(w, 200, map[string]any{"id": id, "status": status, "severity": severity, "started_at": started, "ended_at": ended, "duration_seconds": dur, "sensor": map[string]any{"device": device, "name": sensor}, "service": map[string]any{"name": service, "criticality": criticality}, "impact": impact, "correlation_group_id": correlationGroup, "correlated_incidents": correlated})
}

func (a *app) incidentAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		jsonOut(w, 404, map[string]any{"error": "not found"})
		return
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid id"})
		return
	}
	action := parts[4]
	claims, _ := auth.ClaimsFromContext(r.Context())
	org := claims.OrgID
	var current string
	var startedAt time.Time
	var sensorID uuid.UUID
	err = a.pool.QueryRow(r.Context(), `select status, started_at, prtg_sensor_id from incidents where organization_id=$1 and id=$2`, org, id).Scan(&current, &startedAt, &sensorID)
	if err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "incident not found"})
		return
	}
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	var to incident.Status
	switch action {
	case "ack":
		to = incident.Acknowledged
	case "close":
		to = incident.Closed
	default:
		jsonOut(w, 404, map[string]any{"error": "unknown action"})
		return
	}
	if !incident.CanTransition(incident.Status(current), to) {
		jsonOut(w, 409, map[string]any{"error": fmt.Sprintf("invalid transition %s -> %s", current, to)})
		return
	}
	if to == incident.Closed {
		endedAt := time.Now().UTC()
		dur := int64(endedAt.Sub(startedAt).Seconds())
		if dur < 0 {
			dur = 0
		}
		_, err = a.pool.Exec(r.Context(), `update incidents set status=$1, ended_at=$2, duration_seconds=$3 where organization_id=$4 and id=$5`, string(to), endedAt, dur, org, id)
		if err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		a.calculateManualImpact(r.Context(), org, id, sensorID, dur, endedAt)
	} else {
		_, err = a.pool.Exec(r.Context(), `update incidents set status=$1 where organization_id=$2 and id=$3`, string(to), org, id)
		if err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
	}
	_, _ = a.pool.Exec(r.Context(), `insert into incident_events(organization_id,incident_id,event_type,actor) values($1,$2,$3,$4)`, org, id, strings.ToLower(string(to)), claims.Role)
	_ = audit.Record(r.Context(), a.pool, org, claims.Role, "incident."+action, "incident", &id, map[string]any{"status": current}, map[string]any{"status": to})
	jsonOut(w, 200, map[string]any{"id": id, "status": to})
}

func (a *app) calculateManualImpact(ctx context.Context, org string, incID uuid.UUID, sensorID uuid.UUID, dur int64, ended time.Time) {
	var serviceID *uuid.UUID
	var dependency decimal.Decimal
	_ = a.pool.QueryRow(ctx, `select business_service_id,dependency_weight from service_sensor_mapping where organization_id=$1 and prtg_sensor_id=$2 order by dependency_weight desc limit 1`, org, sensorID).Scan(&serviceID, &dependency)
	var hourly, txh, atv, cost, pExp, rCost decimal.Decimal
	q := `select hourly_revenue,coalesce(transactions_per_hour,0),coalesce(avg_transaction_value,0),service_dependency,loss_probability,operational_cost_per_hour,coalesce(penalty_config->>'fixed', '0')::numeric,coalesce(recovery_config->>'fixed','0')::numeric from financial_profiles where organization_id=$1 and ($2::uuid is null and business_service_id is null or business_service_id=$2) and valid_from <= $3 and (valid_to is null or $3 < valid_to) order by business_service_id nulls last, valid_from desc limit 1`
	var dep, lp decimal.Decimal
	if err := a.pool.QueryRow(ctx, q, org, serviceID, ended).Scan(&hourly, &txh, &atv, &dep, &lp, &cost, &pExp, &rCost); err != nil {
		dep = decimal.NewFromFloat(dependency.InexactFloat64())
		lp = decimal.NewFromInt(1)
	}
	model := financial.Model{
		Version: "2026.1",
		Components: []financial.Component{
			{Key: "revenue_loss", Expr: "hourly_revenue * (duration_seconds/3600) * service_dependency * loss_probability"},
			{Key: "operational_cost", Expr: "operational_cost_per_hour * (duration_seconds/3600)"},
			{Key: "penalty_exposure", Expr: "penalty_exposure"},
			{Key: "recovery_cost", Expr: "recovery_cost"},
		},
		TotalExpr: "revenue_loss + operational_cost + penalty_exposure + recovery_cost",
	}
	in := financial.InputSnapshot{
		DurationSeconds:        dur,
		HourlyRevenue:          hourly,
		TransactionsPerHour:    txh,
		AvgTransactionValue:    atv,
		ServiceDependency:      dep,
		LossProbability:        lp,
		OperationalCostPerHour: cost,
		PenaltyExposure:        pExp,
		RecoveryCost:           rCost,
	}
	res, err := financial.Calculate(model, in)
	if err != nil {
		fmt.Printf("manual financial calculation failed: %s\n", err.Error())
		return
	}
	var modelID uuid.UUID
	if err = a.pool.QueryRow(ctx, `select id from impact_models where version=$1`, model.Version).Scan(&modelID); err != nil {
		return
	}
	snap, _ := json.Marshal(in)
	breakdown, _ := json.Marshal(res.Breakdown)
	_, _ = a.pool.Exec(ctx, `insert into impact_calculations(organization_id,incident_id,impact_model_id,input_snapshot,result_breakdown,total_impact,confidence) values($1,$2,$3,$4,$5,$6,$7) on conflict(incident_id,impact_model_id) do update set input_snapshot=excluded.input_snapshot, result_breakdown=excluded.result_breakdown, total_impact=excluded.total_impact, confidence=excluded.confidence`, org, incID, modelID, snap, breakdown, res.Total, 0.9)
}

func (a *app) events(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	limit := 50
	if x, _ := strconv.Atoi(r.URL.Query().Get("limit")); x > 0 && x <= 200 {
		limit = x
	}
	rows, err := a.pool.Query(r.Context(), `select e.id,e.event_type,e.state,e.occurred_at,s.device_name,s.sensor_name from technical_events e join prtg_sensors s on s.id=e.prtg_sensor_id where e.organization_id=$1 order by e.occurred_at desc limit $2`, org, limit)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var typ, state, device, sensor string
		var at time.Time
		if err := rows.Scan(&id, &typ, &state, &at, &device, &sensor); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		out = append(out, map[string]any{"id": id, "event_type": typ, "state": state, "occurred_at": at, "device": device, "sensor": sensor})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}
func (a *app) services(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	rows, err := a.pool.Query(r.Context(), `select id,name,criticality,coalesce(owner_name,''),coalesce(affected_users,0),coalesce(sla_target_pct,99.9),coalesce(rto_minutes,60),coalesce(rpo_minutes,30),coalesce(description,''),coalesce(business_value_per_hour,0) from business_services where organization_id=$1 order by criticality,name`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, criticality, owner, desc string
		var users, rto, rpo int
		var sla, bvph float64
		if err := rows.Scan(&id, &name, &criticality, &owner, &users, &sla, &rto, &rpo, &desc, &bvph); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "criticality": criticality, "owner_name": owner, "affected_users": users, "sla_target_pct": sla, "rto_minutes": rto, "rpo_minutes": rpo, "description": desc, "business_value_per_hour": bvph})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

type serviceInput struct {
	Name                 string  `json:"name"`
	Criticality          string  `json:"criticality"`
	OwnerName            string  `json:"owner_name"`
	AffectedUsers        int     `json:"affected_users"`
	SLATargetPct         float64 `json:"sla_target_pct"`
	RTOMinutes           int     `json:"rto_minutes"`
	RPOMinutes           int     `json:"rpo_minutes"`
	Description          string  `json:"description"`
	BusinessValuePerHour float64 `json:"business_value_per_hour"`
}
type mappingInput struct {
	SensorID         string  `json:"sensor_id"`
	DependencyWeight float64 `json:"dependency_weight"`
}
type financialInput struct {
	BusinessServiceID      *string `json:"business_service_id"`
	HourlyRevenue          float64 `json:"hourly_revenue"`
	TransactionsPerHour    float64 `json:"transactions_per_hour"`
	AvgTransactionValue    float64 `json:"avg_transaction_value"`
	ServiceDependency      float64 `json:"service_dependency"`
	LossProbability        float64 `json:"loss_probability"`
	OperationalCostPerHour float64 `json:"operational_cost_per_hour"`
	PenaltyFixed           float64 `json:"penalty_fixed"`
	RecoveryFixed          float64 `json:"recovery_fixed"`
	ValidFrom              string  `json:"valid_from"`
}

func adminOnly(r *http.Request) error {
	c, _ := auth.ClaimsFromContext(r.Context())
	if c.Role != "admin" && c.Role != "owner" {
		return fmt.Errorf("admin role required")
	}
	return nil
}
func parsePathID(path, prefix string) (uuid.UUID, error) {
	x := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	return uuid.Parse(x)
}
func pathID(r *http.Request, fallbackPrefix string) (uuid.UUID, error) {
	if x := r.PathValue("id"); x != "" {
		return uuid.Parse(x)
	}
	return parsePathID(r.URL.Path, fallbackPrefix)
}

func (a *app) sensors(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	rows, err := a.pool.Query(r.Context(), `select id,prtg_instance_id,prtg_sensor_id,coalesce(device_name,''),coalesce(sensor_name,''),coalesce(last_known_state,'unknown') from prtg_sensors where organization_id=$1 order by device_name,sensor_name`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, inst uuid.UUID
		var pid, dev, name, state string
		if err := rows.Scan(&id, &inst, &pid, &dev, &name, &state); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		out = append(out, map[string]any{"id": id, "prtg_instance_id": inst, "prtg_sensor_id": pid, "device_name": dev, "sensor_name": name, "last_known_state": state})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

func (a *app) createService(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	org := orgID(r)
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	in.Criticality = strings.ToUpper(in.Criticality)
	if in.Name == "" || !map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true}[in.Criticality] {
		jsonOut(w, 400, map[string]any{"error": "name and criticality P1-P4 are required"})
		return
	}
	if in.SLATargetPct == 0 {
		in.SLATargetPct = 99.9
	}
	if in.RTOMinutes == 0 {
		in.RTOMinutes = 60
	}
	if in.RPOMinutes == 0 {
		in.RPOMinutes = 30
	}
	var id uuid.UUID
	err := a.pool.QueryRow(r.Context(), `insert into business_services(organization_id,name,criticality,owner_name,affected_users,sla_target_pct,rto_minutes,rpo_minutes,description,business_value_per_hour) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) returning id`,
		org, in.Name, in.Criticality, in.OwnerName, in.AffectedUsers, in.SLATargetPct, in.RTOMinutes, in.RPOMinutes, in.Description, in.BusinessValuePerHour).Scan(&id)
	if err != nil {
		jsonOut(w, 409, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "service.create", "business_service", &id, nil, map[string]any{"name": in.Name, "criticality": in.Criticality})
	jsonOut(w, 201, map[string]any{"id": id, "name": in.Name, "criticality": in.Criticality})
}

func (a *app) updateService(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/services/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid service id"})
		return
	}
	org := orgID(r)
	var in serviceInput
	if err = json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	in.Criticality = strings.ToUpper(in.Criticality)
	if in.Name == "" || !map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true}[in.Criticality] {
		jsonOut(w, 400, map[string]any{"error": "name and criticality P1-P4 are required"})
		return
	}
	if in.SLATargetPct == 0 {
		in.SLATargetPct = 99.9
	}
	if in.RTOMinutes == 0 {
		in.RTOMinutes = 60
	}
	if in.RPOMinutes == 0 {
		in.RPOMinutes = 30
	}
	var oldName, oldCrit string
	if err = a.pool.QueryRow(r.Context(), `select name,criticality from business_services where organization_id=$1 and id=$2`, org, id).Scan(&oldName, &oldCrit); err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "service not found"})
		return
	} else if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_, err = a.pool.Exec(r.Context(), `update business_services set name=$1,criticality=$2,owner_name=$3,affected_users=$4,sla_target_pct=$5,rto_minutes=$6,rpo_minutes=$7,description=$8,business_value_per_hour=$9 where organization_id=$10 and id=$11`,
		in.Name, in.Criticality, in.OwnerName, in.AffectedUsers, in.SLATargetPct, in.RTOMinutes, in.RPOMinutes, in.Description, in.BusinessValuePerHour, org, id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "service.update", "business_service", &id, map[string]any{"name": oldName, "criticality": oldCrit}, map[string]any{"name": in.Name, "criticality": in.Criticality})
	jsonOut(w, 200, map[string]any{"id": id, "name": in.Name, "criticality": in.Criticality})
}

func (a *app) deleteService(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/services/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid service id"})
		return
	}
	org := orgID(r)
	var name string
	if err = a.pool.QueryRow(r.Context(), `select name from business_services where organization_id=$1 and id=$2`, org, id).Scan(&name); err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "service not found"})
		return
	}
	_, err = a.pool.Exec(r.Context(), `delete from service_sensor_mapping where organization_id=$1 and business_service_id=$2`, org, id)
	if err == nil {
		_, err = a.pool.Exec(r.Context(), `delete from business_processes where organization_id=$1 and business_service_id=$2`, org, id)
	}
	if err == nil {
		_, err = a.pool.Exec(r.Context(), `delete from business_services where organization_id=$1 and id=$2`, org, id)
	}
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "service.delete", "business_service", &id, map[string]any{"name": name}, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) serviceMappings(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "/api/v1/services/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid service id"})
		return
	}
	org := orgID(r)
	rows, err := a.pool.Query(r.Context(), `select m.prtg_sensor_id,m.dependency_weight,s.prtg_sensor_id,s.device_name,s.sensor_name,s.last_known_state from service_sensor_mapping m join prtg_sensors s on s.id=m.prtg_sensor_id where m.organization_id=$1 and m.business_service_id=$2 order by s.device_name,s.sensor_name`, org, id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var sid uuid.UUID
		var wgt float64
		var pid, dev, name, state string
		if err := rows.Scan(&sid, &wgt, &pid, &dev, &name, &state); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		out = append(out, map[string]any{"sensor_id": sid, "prtg_sensor_id": pid, "device_name": dev, "sensor_name": name, "last_known_state": state, "dependency_weight": wgt})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

func (a *app) replaceServiceMappings(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/services/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid service id"})
		return
	}
	org := orgID(r)
	var in struct {
		Mappings []mappingInput `json:"mappings"`
	}
	if err = json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	tx, err := a.pool.Begin(r.Context())
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer tx.Rollback(r.Context())
	var exists int
	if err = tx.QueryRow(r.Context(), `select 1 from business_services where id=$1 and organization_id=$2`, id, org).Scan(&exists); err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "service not found"})
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from service_sensor_mapping where organization_id=$1 and business_service_id=$2`, org, id); err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	for _, m := range in.Mappings {
		if m.DependencyWeight <= 0 || m.DependencyWeight > 1 {
			jsonOut(w, 400, map[string]any{"error": "dependency_weight must be >0 and <=1"})
			return
		}
		sid, e := uuid.Parse(m.SensorID)
		if e != nil {
			jsonOut(w, 400, map[string]any{"error": "invalid sensor_id"})
			return
		}
		if _, e = tx.Exec(r.Context(), `insert into service_sensor_mapping(organization_id,business_service_id,prtg_sensor_id,dependency_weight) select $1,$2,id,$4 from prtg_sensors where organization_id=$1 and id=$3 on conflict (business_service_id,prtg_sensor_id) do update set dependency_weight=excluded.dependency_weight`, org, id, sid, m.DependencyWeight); e != nil {
			jsonOut(w, 400, map[string]any{"error": e.Error()})
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "service.mapping.replace", "business_service", &id, nil, map[string]any{"mapping_count": len(in.Mappings)})
	jsonOut(w, 200, map[string]any{"service_id": id, "mapping_count": len(in.Mappings)})
}

func (a *app) financialProfiles(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	rows, err := a.pool.Query(r.Context(), `select f.id,f.business_service_id,coalesce(b.name,'Organization Default'),f.hourly_revenue,coalesce(f.transactions_per_hour,0),coalesce(f.avg_transaction_value,0),f.service_dependency,f.loss_probability,f.operational_cost_per_hour,coalesce(f.penalty_config->>'fixed','0')::numeric,coalesce(f.recovery_config->>'fixed','0')::numeric,f.valid_from,f.valid_to from financial_profiles f left join business_services b on b.id=f.business_service_id where f.organization_id=$1 order by f.valid_to is null desc,f.valid_from desc`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var bs *uuid.UUID
		var name string
		var hr, txh, atv, dep, lp, cost, p, r float64
		var vf time.Time
		var vtn *time.Time
		if err := rows.Scan(&id, &bs, &name, &hr, &txh, &atv, &dep, &lp, &cost, &p, &r, &vf, &vtn); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		var validTo any
		if vtn != nil {
			validTo = *vtn
		}
		out = append(out, map[string]any{"id": id, "business_service_id": bs, "service_name": name, "hourly_revenue": hr, "transactions_per_hour": txh, "avg_transaction_value": atv, "service_dependency": dep, "loss_probability": lp, "operational_cost_per_hour": cost, "penalty_fixed": p, "recovery_fixed": r, "valid_from": vf, "valid_to": validTo, "active": vtn == nil})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

func validateFinancial(in financialInput) error {
	if in.HourlyRevenue < 0 || in.TransactionsPerHour < 0 || in.AvgTransactionValue < 0 || in.OperationalCostPerHour < 0 || in.PenaltyFixed < 0 || in.RecoveryFixed < 0 {
		return fmt.Errorf("financial values cannot be negative")
	}
	if in.ServiceDependency <= 0 || in.ServiceDependency > 1 {
		return fmt.Errorf("service_dependency must be >0 and <=1")
	}
	if in.LossProbability < 0 || in.LossProbability > 1 {
		return fmt.Errorf("loss_probability must be between 0 and 1")
	}
	return nil
}

func (a *app) createFinancialProfile(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	org := orgID(r)
	var in financialInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	if err := validateFinancial(in); err != nil {
		jsonOut(w, 400, map[string]any{"error": err.Error()})
		return
	}
	var bs any = nil
	if in.BusinessServiceID != nil && *in.BusinessServiceID != "" {
		id, e := uuid.Parse(*in.BusinessServiceID)
		if e != nil {
			jsonOut(w, 400, map[string]any{"error": "invalid business_service_id"})
			return
		}
		bs = id
	}
	validFrom := time.Now().UTC()
	if in.ValidFrom != "" {
		if t, e := time.Parse(time.RFC3339, in.ValidFrom); e == nil {
			validFrom = t
		} else {
			jsonOut(w, 400, map[string]any{"error": "valid_from must be RFC3339"})
			return
		}
	}
	// Close the currently active profile for the same scope before creating a new version.
	_, _ = a.pool.Exec(r.Context(), `update financial_profiles set valid_to=$1 where organization_id=$2 and valid_to is null and ((business_service_id is null and $3::uuid is null) or business_service_id=$3) and valid_from <= $1`, validFrom, org, bs)
	var id uuid.UUID
	err := a.pool.QueryRow(r.Context(), `insert into financial_profiles(organization_id,business_service_id,hourly_revenue,transactions_per_hour,avg_transaction_value,service_dependency,loss_probability,operational_cost_per_hour,penalty_config,recovery_config,valid_from) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) returning id`, org, bs, in.HourlyRevenue, in.TransactionsPerHour, in.AvgTransactionValue, in.ServiceDependency, in.LossProbability, in.OperationalCostPerHour, fmt.Sprintf(`{"fixed":%g}`, in.PenaltyFixed), fmt.Sprintf(`{"fixed":%g}`, in.RecoveryFixed), validFrom).Scan(&id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "financial_profile.create", "financial_profile", &id, nil, in)
	jsonOut(w, 201, map[string]any{"id": id, "valid_from": validFrom})
}

func (a *app) updateFinancialProfile(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/financial-profiles/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid financial profile id"})
		return
	}
	org := orgID(r)
	var in financialInput
	if err = json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	if err = validateFinancial(in); err != nil {
		jsonOut(w, 400, map[string]any{"error": err.Error()})
		return
	}
	var bs *uuid.UUID
	var oldBS *uuid.UUID
	if in.BusinessServiceID != nil && *in.BusinessServiceID != "" {
		x, e := uuid.Parse(*in.BusinessServiceID)
		if e != nil {
			jsonOut(w, 400, map[string]any{"error": "invalid business_service_id"})
			return
		}
		bs = &x
	}
	var exists bool
	err = a.pool.QueryRow(r.Context(), `select exists(select 1 from financial_profiles where id=$1 and organization_id=$2)`, id, org).Scan(&exists)
	if err != nil || !exists {
		jsonOut(w, 404, map[string]any{"error": "financial profile not found"})
		return
	}
	var from time.Time
	if in.ValidFrom != "" {
		from, err = time.Parse(time.RFC3339, in.ValidFrom)
		if err != nil {
			jsonOut(w, 400, map[string]any{"error": "valid_from must be RFC3339"})
			return
		}
	} else {
		from = time.Now().UTC()
	}
	_ = a.pool.QueryRow(r.Context(), `select business_service_id from financial_profiles where id=$1 and organization_id=$2`, id, org).Scan(&oldBS)
	_, _ = a.pool.Exec(r.Context(), `update financial_profiles set valid_to=$1 where id=$2 and organization_id=$3 and valid_to is null`, from, id, org)
	var newID uuid.UUID
	err = a.pool.QueryRow(r.Context(), `insert into financial_profiles(organization_id,business_service_id,hourly_revenue,transactions_per_hour,avg_transaction_value,service_dependency,loss_probability,operational_cost_per_hour,penalty_config,recovery_config,valid_from) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) returning id`, org, bs, in.HourlyRevenue, in.TransactionsPerHour, in.AvgTransactionValue, in.ServiceDependency, in.LossProbability, in.OperationalCostPerHour, fmt.Sprintf(`{"fixed":%g}`, in.PenaltyFixed), fmt.Sprintf(`{"fixed":%g}`, in.RecoveryFixed), from).Scan(&newID)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "financial_profile.update", "financial_profile", &id, map[string]any{"business_service_id": oldBS}, map[string]any{"replaced_by": newID})
	jsonOut(w, 200, map[string]any{"id": newID, "replaced_profile_id": id, "valid_from": from})
}

func (a *app) deleteFinancialProfile(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/financial-profiles/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid financial profile id"})
		return
	}
	org := orgID(r)
	now := time.Now().UTC()
	cmd, err := a.pool.Exec(r.Context(), `update financial_profiles set valid_to=$1 where id=$2 and organization_id=$3 and valid_to is null`, now, id, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if cmd.RowsAffected() == 0 {
		jsonOut(w, 404, map[string]any{"error": "active financial profile not found"})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "financial_profile.delete", "financial_profile", &id, map[string]any{"active": true}, map[string]any{"valid_to": now})
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) prtgWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("PRTG_WEBHOOK_SECRET")
	if secret != "" && r.Header.Get("X-PRTG-Webhook-Secret") != secret && r.URL.Query().Get("token") != secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ct := r.Header.Get("Content-Type")
	var payload map[string]any = map[string]any{}
	if strings.Contains(ct, "application/json") {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", 400)
			return
		}
		for k, vs := range r.Form {
			if len(vs) > 0 {
				payload[k] = vs[0]
			}
		}
	}
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := payload[k].(string); ok && v != "" {
				return v
			}
			if v, ok := payload[strings.ToLower(k)]; ok {
				if x, ok := v.(string); ok && x != "" {
					return x
				}
			}
		}
		return ""
	}
	instance := get("prtg_instance_id", "instance_id", "instance")
	sensor := get("sensorid", "sensor_id", "prtg_sensor_id")
	org := get("organization_id", "org_id")
	status := get("status", "state")
	device := get("device", "server")
	name := get("sensor", "name")
	dt := get("datetime", "occurred_at", "timestamp")
	if instance == "" || sensor == "" {
		http.Error(w, "prtg_instance_id and sensorid are required", 400)
		return
	}
	var instOrg string
	if err := a.pool.QueryRow(r.Context(), `select organization_id from prtg_instances where id=$1`, instance).Scan(&instOrg); err != nil {
		http.Error(w, "unknown prtg instance", 404)
		return
	}
	if org == "" {
		org = instOrg
	} else if org != instOrg {
		http.Error(w, "organization does not match PRTG instance", 403)
		return
	}
	occurred := time.Now().UTC()
	if dt != "" {
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
			if x, e := time.Parse(layout, dt); e == nil {
				occurred = x
				break
			}
		}
	}
	state := prtg.NormalizeState(status)
	res, err := ingestion.StoreEvent(r.Context(), a.pool, ingestion.EventInput{OrganizationID: org, PRTGInstanceID: instance, PRTGSensorID: sensor, EventType: "notification", State: state, OccurredAt: occurred, RawPayload: payload, DeviceName: device, SensorName: name})
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	jsonOut(w, 200, map[string]any{"accepted": true, "inserted": res.Inserted, "event_id": res.EventID})
}

// ─── Knowledge Base CRUD ────────────────────────────────────────────────────

type kbInput struct {
	DevicePattern               string   `json:"device_pattern"`
	SensorPattern               string   `json:"sensor_pattern"`
	ServiceCategory             string   `json:"service_category"`
	Description                 string   `json:"description"`
	HourlyLossEstimate          float64  `json:"hourly_loss_estimate"`
	AffectedUsersEstimate       int      `json:"affected_users_estimate"`
	AffectedProcesses           []string `json:"affected_processes"`
	SLAPenaltyPerHour           float64  `json:"sla_penalty_per_hour"`
	RecoveryTimeEstimateMinutes int      `json:"recovery_time_estimate_minutes"`
	RecoveryProcedure           string   `json:"recovery_procedure"`
	Priority                    string   `json:"priority"`
	IsActive                    bool     `json:"is_active"`
}

func (a *app) listKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	rows, err := a.pool.Query(r.Context(), `SELECT id,device_pattern,sensor_pattern,service_category,description,hourly_loss_estimate,affected_users_estimate,affected_processes,sla_penalty_per_hour,recovery_time_estimate_minutes,recovery_procedure,priority,is_active,created_at,updated_at FROM knowledge_base WHERE organization_id=$1 ORDER BY priority,device_pattern`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var devicePattern, sensorPattern, serviceCategory, description, priority string
		var recoveryProcedure *string
		var hourlyLoss, slaPenalty float64
		var affectedUsers, recoveryMinutes int
		var affectedProcesses []string
		var isActive bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &devicePattern, &sensorPattern, &serviceCategory, &description, &hourlyLoss, &affectedUsers, &affectedProcesses, &slaPenalty, &recoveryMinutes, &recoveryProcedure, &priority, &isActive, &createdAt, &updatedAt); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		rp := ""
		if recoveryProcedure != nil {
			rp = *recoveryProcedure
		}
		if affectedProcesses == nil {
			affectedProcesses = []string{}
		}
		out = append(out, map[string]any{
			"id": id, "device_pattern": devicePattern, "sensor_pattern": sensorPattern,
			"service_category": serviceCategory, "description": description,
			"hourly_loss_estimate": hourlyLoss, "affected_users_estimate": affectedUsers,
			"affected_processes": affectedProcesses, "sla_penalty_per_hour": slaPenalty,
			"recovery_time_estimate_minutes": recoveryMinutes, "recovery_procedure": rp,
			"priority": priority, "is_active": isActive,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

func (a *app) createKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	org := orgID(r)
	var in kbInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	if in.DevicePattern == "" || in.Description == "" {
		jsonOut(w, 400, map[string]any{"error": "device_pattern and description are required"})
		return
	}
	cats := map[string]bool{"network": true, "server": true, "application": true, "database": true, "storage": true, "security": true}
	if !cats[in.ServiceCategory] {
		in.ServiceCategory = "network"
	}
	pris := map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true}
	if !pris[in.Priority] {
		in.Priority = "P2"
	}
	if in.RecoveryTimeEstimateMinutes <= 0 {
		in.RecoveryTimeEstimateMinutes = 60
	}
	if in.AffectedProcesses == nil {
		in.AffectedProcesses = []string{}
	}
	var id uuid.UUID
	err := a.pool.QueryRow(r.Context(), `INSERT INTO knowledge_base(organization_id,device_pattern,sensor_pattern,service_category,description,hourly_loss_estimate,affected_users_estimate,affected_processes,sla_penalty_per_hour,recovery_time_estimate_minutes,recovery_procedure,priority,is_active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
		org, in.DevicePattern, in.SensorPattern, in.ServiceCategory, in.Description,
		in.HourlyLossEstimate, in.AffectedUsersEstimate, in.AffectedProcesses,
		in.SLAPenaltyPerHour, in.RecoveryTimeEstimateMinutes, in.RecoveryProcedure,
		in.Priority, true).Scan(&id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "knowledge_base.create", "knowledge_base", &id, nil, in)
	jsonOut(w, 201, map[string]any{"id": id})
}

func (a *app) updateKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/knowledge-base/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid id"})
		return
	}
	org := orgID(r)
	var in kbInput
	if err = json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	if in.DevicePattern == "" || in.Description == "" {
		jsonOut(w, 400, map[string]any{"error": "device_pattern and description are required"})
		return
	}
	cats := map[string]bool{"network": true, "server": true, "application": true, "database": true, "storage": true, "security": true}
	if !cats[in.ServiceCategory] {
		in.ServiceCategory = "network"
	}
	pris := map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true}
	if !pris[in.Priority] {
		in.Priority = "P2"
	}
	if in.RecoveryTimeEstimateMinutes <= 0 {
		in.RecoveryTimeEstimateMinutes = 60
	}
	if in.AffectedProcesses == nil {
		in.AffectedProcesses = []string{}
	}
	cmd, err := a.pool.Exec(r.Context(), `UPDATE knowledge_base SET device_pattern=$1,sensor_pattern=$2,service_category=$3,description=$4,hourly_loss_estimate=$5,affected_users_estimate=$6,affected_processes=$7,sla_penalty_per_hour=$8,recovery_time_estimate_minutes=$9,recovery_procedure=$10,priority=$11,is_active=$12,updated_at=now() WHERE organization_id=$13 AND id=$14`,
		in.DevicePattern, in.SensorPattern, in.ServiceCategory, in.Description,
		in.HourlyLossEstimate, in.AffectedUsersEstimate, in.AffectedProcesses,
		in.SLAPenaltyPerHour, in.RecoveryTimeEstimateMinutes, in.RecoveryProcedure,
		in.Priority, in.IsActive, org, id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if cmd.RowsAffected() == 0 {
		jsonOut(w, 404, map[string]any{"error": "knowledge base entry not found"})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "knowledge_base.update", "knowledge_base", &id, nil, in)
	jsonOut(w, 200, map[string]any{"id": id})
}

func (a *app) deleteKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/knowledge-base/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid id"})
		return
	}
	org := orgID(r)
	cmd, err := a.pool.Exec(r.Context(), `DELETE FROM knowledge_base WHERE organization_id=$1 AND id=$2`, org, id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if cmd.RowsAffected() == 0 {
		jsonOut(w, 404, map[string]any{"error": "knowledge base entry not found"})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "knowledge_base.delete", "knowledge_base", &id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ─── AI Analysis ─────────────────────────────────────────────────────────────

func (a *app) aiAnalyze(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	var in struct {
		Query    string `json:"query"`
		SensorID string `json:"sensor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}

	// Fetch all knowledge base entries for this org
	kbRows, err := a.pool.Query(r.Context(), `SELECT id,device_pattern,sensor_pattern,service_category,description,hourly_loss_estimate,affected_users_estimate,affected_processes,sla_penalty_per_hour,recovery_time_estimate_minutes,recovery_procedure,priority FROM knowledge_base WHERE organization_id=$1 AND is_active=true ORDER BY priority`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer kbRows.Close()
	type kbEntry struct {
		ID, DevicePattern, SensorPattern, ServiceCategory, Description, Priority, RecoveryProcedure string
		HourlyLoss, SLAPenalty                                                                      float64
		AffectedUsers, RecoveryMinutes                                                              int
		AffectedProcesses                                                                           []string
	}
	var kbEntries []kbEntry
	for kbRows.Next() {
		var e kbEntry
		var recProc *string
		var procs []string
		var kid uuid.UUID
		if err := kbRows.Scan(&kid, &e.DevicePattern, &e.SensorPattern, &e.ServiceCategory, &e.Description, &e.HourlyLoss, &e.AffectedUsers, &procs, &e.SLAPenalty, &e.RecoveryMinutes, &recProc, &e.Priority); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		e.ID = kid.String()
		if recProc != nil {
			e.RecoveryProcedure = *recProc
		}
		if procs != nil {
			e.AffectedProcesses = procs
		}
		kbEntries = append(kbEntries, e)
	}

	type sensorInfo struct {
		ID, PRTGID, Device, Sensor, State string
	}
	var downSensors []sensorInfo

	if in.SensorID != "" {
		// Specific sensor requested: analyze only that sensor, regardless of its current state.
		sid, e := uuid.Parse(in.SensorID)
		if e != nil {
			jsonOut(w, 400, map[string]any{"error": "invalid sensor_id"})
			return
		}
		var dev, name, state string
		err = a.pool.QueryRow(r.Context(), `SELECT device_name,sensor_name,last_known_state FROM prtg_sensors WHERE organization_id=$1 AND id=$2`, org, sid).Scan(&dev, &name, &state)
		if err != nil {
			jsonOut(w, 404, map[string]any{"error": "sensor not found"})
			return
		}
		downSensors = []sensorInfo{{ID: sid.String(), Device: dev, Sensor: name, State: state}}
	} else {
		// Fetch all currently down sensors
		sRows, err := a.pool.Query(r.Context(), `SELECT id,prtg_sensor_id,device_name,sensor_name,last_known_state FROM prtg_sensors WHERE organization_id=$1 AND last_known_state='down' ORDER BY device_name`, org)
		if err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		defer sRows.Close()
		for sRows.Next() {
			var s sensorInfo
			var sid uuid.UUID
			if err := sRows.Scan(&sid, &s.PRTGID, &s.Device, &s.Sensor, &s.State); err != nil {
				jsonOut(w, 500, map[string]any{"error": err.Error()})
				return
			}
			s.ID = sid.String()
			downSensors = append(downSensors, s)
		}
	}

	// Match sensors against knowledge base
	type analysisResult struct {
		Sensor          map[string]any   `json:"sensor"`
		MatchedRules    []map[string]any `json:"matched_rules"`
		TotalHourlyLoss float64          `json:"total_hourly_loss"`
		TotalSLAPenalty float64          `json:"total_sla_penalty"`
		AffectedUsers   int              `json:"affected_users"`
		RecoveryMinutes int              `json:"recovery_time_minutes"`
		RiskLevel       string           `json:"risk_level"`
		Recommendations []string         `json:"recommendations"`
	}
	var results []analysisResult
	totalOrgImpact := 0.0
	for _, sensor := range downSensors {
		ar := analysisResult{
			Sensor: map[string]any{"id": sensor.ID, "device": sensor.Device, "sensor": sensor.Sensor, "state": sensor.State},
		}
		for _, kb := range kbEntries {
			deviceMatch := strings.Contains(strings.ToLower(sensor.Device), strings.ToLower(kb.DevicePattern))
			sensorMatch := kb.SensorPattern == "" || strings.Contains(strings.ToLower(sensor.Sensor), strings.ToLower(kb.SensorPattern))
			if deviceMatch && sensorMatch {
				ar.MatchedRules = append(ar.MatchedRules, map[string]any{
					"kb_id": kb.ID, "device_pattern": kb.DevicePattern, "description": kb.Description,
					"hourly_loss": kb.HourlyLoss, "sla_penalty": kb.SLAPenalty, "priority": kb.Priority,
					"affected_processes": kb.AffectedProcesses, "recovery_procedure": kb.RecoveryProcedure,
					"recovery_minutes": kb.RecoveryMinutes, "category": kb.ServiceCategory,
				})
				ar.TotalHourlyLoss += kb.HourlyLoss
				ar.TotalSLAPenalty += kb.SLAPenalty
				if kb.AffectedUsers > ar.AffectedUsers {
					ar.AffectedUsers = kb.AffectedUsers
				}
				if kb.RecoveryMinutes > ar.RecoveryMinutes {
					ar.RecoveryMinutes = kb.RecoveryMinutes
				}
			}
		}
		if ar.MatchedRules == nil {
			ar.MatchedRules = []map[string]any{}
		}
		// Determine risk level
		switch {
		case ar.TotalHourlyLoss >= 100000000:
			ar.RiskLevel = "CRITICAL"
		case ar.TotalHourlyLoss >= 50000000:
			ar.RiskLevel = "HIGH"
		case ar.TotalHourlyLoss >= 10000000:
			ar.RiskLevel = "MEDIUM"
		default:
			ar.RiskLevel = "LOW"
		}
		// Generate recommendations
		if sensor.State == "down" {
			ar.Recommendations = append(ar.Recommendations, fmt.Sprintf("URGENT: %s sedang DOWN — segera eskalasi ke tim terkait", sensor.Device))
		}
		if ar.TotalHourlyLoss > 0 {
			ar.Recommendations = append(ar.Recommendations, fmt.Sprintf("Estimasi kerugian: Rp %.0f per jam downtime", ar.TotalHourlyLoss))
		}
		if ar.RecoveryMinutes > 0 {
			ar.Recommendations = append(ar.Recommendations, fmt.Sprintf("Estimasi waktu recovery: %d menit", ar.RecoveryMinutes))
		}
		if len(ar.MatchedRules) == 0 {
			ar.Recommendations = append(ar.Recommendations, "Belum ada knowledge base entry untuk device ini — tambahkan di tab Knowledge Base")
		}
		totalOrgImpact += ar.TotalHourlyLoss
		results = append(results, ar)
	}
	if results == nil {
		results = []analysisResult{}
	}

	summary := map[string]any{
		"total_devices_affected": len(downSensors),
		"total_hourly_impact":    totalOrgImpact,
		"total_kb_rules":         len(kbEntries),
		"analysis_timestamp":     time.Now().UTC(),
	}
	jsonOut(w, 200, map[string]any{"summary": summary, "analysis": results})
}

// ─── AI Chatbot (RAG) ──────────────────────────────────────────────────────────

const aiChatSystemPreamble = `Anda adalah asisten Business Impact Analysis untuk BIA Platform, sebuah dashboard yang menghubungkan monitoring infrastruktur IT (PRTG) dengan dampak bisnis dan kerugian finansial.

Jawab pertanyaan pengguna HANYA berdasarkan data snapshot di bawah ini. Jangan mengarang angka. Kalau data yang ditanyakan tidak ada di snapshot, katakan terus terang bahwa datanya tidak tersedia saat ini.

Gaya jawaban: ringkas, langsung ke poin, gunakan format Rupiah untuk nilai uang, dan sebutkan angka konkret dari data yang tersedia. Jawab dalam bahasa yang sama dengan pertanyaan pengguna (default Bahasa Indonesia).

`

// buildRAGContext gathers a compact, current snapshot of the organization's
// BIA data (services, active incidents, financial profiles, knowledge base)
// to ground the chatbot's answers in real numbers instead of letting the
// model guess.
func (a *app) buildRAGContext(ctx context.Context, org string) (string, error) {
	var b strings.Builder

	b.WriteString("=== Business Services ===\n")
	rows, err := a.pool.Query(ctx, `select name, criticality, coalesce(business_value_per_hour,0), coalesce(sla_target_pct,99.9), coalesce(affected_users,0) from business_services where organization_id=$1 order by criticality, name limit 8`, org)
	if err != nil {
		return "", err
	}
	svcCount := 0
	for rows.Next() {
		var name, crit string
		var bvph, sla float64
		var users int
		if err := rows.Scan(&name, &crit, &bvph, &sla, &users); err != nil {
			rows.Close()
			return "", err
		}
		fmt.Fprintf(&b, "- %s (Prioritas %s, SLA target %.2f%%, %d pengguna terdampak, nilai bisnis Rp%.0f/jam)\n", name, crit, sla, users, bvph)
		svcCount++
	}
	rows.Close()
	if svcCount == 0 {
		b.WriteString("(belum ada business service terdaftar)\n")
	}

	var totalOpenIncidents int
	if err := a.pool.QueryRow(ctx, `select count(*) from incidents where organization_id=$1 and status in ('OPEN','ACKNOWLEDGED')`, org).Scan(&totalOpenIncidents); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\n=== Insiden Aktif (OPEN/ACKNOWLEDGED) — total %d, 5 teratas ditampilkan ===\n", totalOpenIncidents)
	rows2, err := a.pool.Query(ctx, `
		select coalesce(bs.name,'Unmapped'), s.device_name, s.sensor_name, i.severity, i.status,
			coalesce(i.duration_seconds, extract(epoch from (now()-i.started_at))::int),
			coalesce(ic.total_impact,0)
		from incidents i
		join prtg_sensors s on s.id=i.prtg_sensor_id
		left join lateral (
			select b2.name from business_services b2
			join service_sensor_mapping m on m.business_service_id=b2.id
			where m.organization_id=$1 and m.prtg_sensor_id=i.prtg_sensor_id
			order by m.dependency_weight desc limit 1
		) bs on true
		left join lateral (
			select c.total_impact from impact_calculations c
			where c.organization_id=$1 and c.incident_id=i.id
			order by c.calculated_at desc limit 1
		) ic on true
		where i.organization_id=$1 and i.status in ('OPEN','ACKNOWLEDGED')
		order by (i.severity='CRITICAL') desc, i.started_at desc limit 5`, org)
	if err != nil {
		return "", err
	}
	incCount := 0
	for rows2.Next() {
		var svc, dev, sensor, sev, status string
		var dur int
		var impact float64
		if err := rows2.Scan(&svc, &dev, &sensor, &sev, &status, &dur, &impact); err != nil {
			rows2.Close()
			return "", err
		}
		fmt.Fprintf(&b, "- [%s] %s — sensor %s @ %s, status %s, berlangsung %dm, estimasi dampak Rp%.0f\n", sev, svc, sensor, dev, status, dur/60, impact)
		incCount++
	}
	rows2.Close()
	if incCount == 0 {
		b.WriteString("(tidak ada insiden aktif saat ini — semua layanan normal)\n")
	}

	b.WriteString("\n=== Business Process (sensor Business Process PRTG) ===\n")
	rowsBP, err := a.pool.Query(ctx, `
		select bp.name, bp.state, coalesce(bs.name,'belum terhubung'), coalesce(bp.prtg_uptime_pct,0)::float8,
			coalesce((select string_agg(c.name||' '||c.state, ', ') from business_process_channels c
				where c.business_process_id=bp.id and c.prtg_channel_id<>0 and c.state<>'up'),'')
		from business_process_sensors bp left join business_services bs on bs.id=bp.business_service_id
		where bp.organization_id=$1 and bp.removed_at is null order by bp.name limit 8`, org)
	if err != nil {
		return "", err
	}
	bpCount := 0
	for rowsBP.Next() {
		var name, state, svc, bad string
		var uptime float64
		if err := rowsBP.Scan(&name, &state, &svc, &uptime, &bad); err != nil {
			rowsBP.Close()
			return "", err
		}
		fmt.Fprintf(&b, "- %s: status %s, service %s, uptime PRTG %.2f%%", name, state, svc, uptime)
		if bad != "" {
			fmt.Fprintf(&b, ", komponen bermasalah: %s", bad)
		}
		b.WriteString("\n")
		bpCount++
	}
	rowsBP.Close()
	if bpCount == 0 {
		b.WriteString("(belum ada sensor Business Process dari PRTG)\n")
	}

	b.WriteString("\n=== Financial Profiles Aktif ===\n")
	rows3, err := a.pool.Query(ctx, `select coalesce(b2.name,'Organization Default'), f.hourly_revenue, f.service_dependency, f.loss_probability from financial_profiles f left join business_services b2 on b2.id=f.business_service_id where f.organization_id=$1 and f.valid_to is null limit 8`, org)
	if err != nil {
		return "", err
	}
	finCount := 0
	for rows3.Next() {
		var name string
		var hr, dep, lp float64
		if err := rows3.Scan(&name, &hr, &dep, &lp); err != nil {
			rows3.Close()
			return "", err
		}
		fmt.Fprintf(&b, "- %s: revenue Rp%.0f/jam, dependency %.2f, loss probability %.0f%%\n", name, hr, dep, lp*100)
		finCount++
	}
	rows3.Close()
	if finCount == 0 {
		b.WriteString("(belum ada financial profile aktif)\n")
	}

	b.WriteString("\n=== Knowledge Base (Prioritas Tinggi Dulu, ringkas) ===\n")
	rows4, err := a.pool.Query(ctx, `select device_pattern, service_category, priority, hourly_loss_estimate, left(description, 70) from knowledge_base where organization_id=$1 and is_active=true order by priority limit 5`, org)
	if err != nil {
		return "", err
	}
	kbCount := 0
	for rows4.Next() {
		var dp, cat, pr, desc string
		var loss float64
		if err := rows4.Scan(&dp, &cat, &pr, &loss, &desc); err != nil {
			rows4.Close()
			return "", err
		}
		fmt.Fprintf(&b, "- [%s/%s] %s: Rp%.0f/jam — %s\n", pr, cat, dp, loss, desc)
		kbCount++
	}
	rows4.Close()
	if kbCount == 0 {
		b.WriteString("(belum ada knowledge base entry)\n")
	}

	return b.String(), nil
}

func (a *app) aiChat(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	var in struct {
		Message string            `json:"message"`
		History []aiagent.Message `json:"history"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(in.Message) == "" {
		jsonOut(w, 400, map[string]any{"error": "message is required"})
		return
	}

	client := aiagent.NewClient()
	if !client.Configured() {
		jsonOut(w, 503, map[string]any{"error": "AI agent belum dikonfigurasi di server (AI_AGENT_BASE_URL/AI_AGENT_API_KEY)"})
		return
	}

	ragContext, err := a.buildRAGContext(r.Context(), org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}

	messages := []aiagent.Message{{Role: "system", Content: aiChatSystemPreamble + ragContext}}
	// Cap conversation history so the prompt stays bounded.
	if len(in.History) > 12 {
		in.History = in.History[len(in.History)-12:]
	}
	messages = append(messages, in.History...)
	messages = append(messages, aiagent.Message{Role: "user", Content: in.Message})

	reply, err := client.Chat(r.Context(), messages)
	if err != nil {
		jsonOut(w, 502, map[string]any{"error": err.Error()})
		return
	}
	jsonOut(w, 200, map[string]any{"reply": reply})
}

// ─── BIA: Executive Summary ───────────────────────────────────────────────────

func (a *app) biaExecutiveSummary(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Total & critical services
	var totalServices, criticalImpacted int
	var estimatedUsersAffected int
	rows, err := a.pool.Query(r.Context(), `
		SELECT bs.id, bs.criticality, coalesce(bs.affected_users,0),
			(SELECT count(*) FROM service_sensor_mapping m
			 JOIN prtg_sensors ps ON ps.id=m.prtg_sensor_id
			 WHERE m.organization_id=$1 AND m.business_service_id=bs.id
			 AND ps.last_known_state IN ('down','warning')) as degraded_sensors,
			(SELECT count(*) FROM service_sensor_mapping m2 WHERE m2.organization_id=$1 AND m2.business_service_id=bs.id) as total_sensors
		FROM business_services bs WHERE bs.organization_id=$1`, org)
	type svcRow struct {
		id                     uuid.UUID
		crit                   string
		users, degraded, total int
	}
	var svcs []svcRow
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s svcRow
			if rows.Scan(&s.id, &s.crit, &s.users, &s.degraded, &s.total) == nil {
				svcs = append(svcs, s)
			}
		}
	}
	for _, s := range svcs {
		totalServices++
		if s.degraded > 0 {
			criticalImpacted++
			estimatedUsersAffected += s.users
		}
	}
	servicesNormal := totalServices - criticalImpacted

	// SLA compliance: average availability this month across all services
	var slaCompliance float64 = 100.0
	monthMinutes := now.Sub(monthStart).Minutes()
	if monthMinutes > 0 && len(svcs) > 0 {
		slaRows, _ := a.pool.Query(r.Context(), `
			SELECT bs.id, coalesce(sum(coalesce(i.duration_seconds,extract(epoch from (now()-i.started_at))::int)),0)/60.0 as downtime_min
			FROM business_services bs
			LEFT JOIN service_sensor_mapping m ON m.business_service_id=bs.id AND m.organization_id=$1
			LEFT JOIN incidents i ON i.prtg_sensor_id=m.prtg_sensor_id AND i.organization_id=$1
				AND i.started_at >= $2 AND i.status != 'CLOSED'
			WHERE bs.organization_id=$1
			GROUP BY bs.id`, org, monthStart)
		if slaRows != nil {
			defer slaRows.Close()
			totalAvail := 0.0
			count := 0
			for slaRows.Next() {
				var sid uuid.UUID
				var dtMin float64
				if slaRows.Scan(&sid, &dtMin) == nil {
					avail := (monthMinutes - dtMin) / monthMinutes * 100
					if avail > 100 {
						avail = 100
					}
					totalAvail += avail
					count++
				}
			}
			if count > 0 {
				slaCompliance = totalAvail / float64(count)
			}
		}
	}

	// Financial exposure this month (real-time aggregation from impact_calculations or active downtime × hourly business value)
	var financialExposure float64
	a.pool.QueryRow(r.Context(), `
		SELECT coalesce(
			NULLIF((SELECT sum(ic.total_impact) FROM impact_calculations ic JOIN incidents i ON i.id=ic.incident_id WHERE ic.organization_id=$1 AND i.started_at>=$2), 0),
			(
				SELECT coalesce(sum(
					(coalesce(i.duration_seconds, extract(epoch from (now()-i.started_at))::int) / 3600.0) *
					coalesce(bs.business_value_per_hour, fp.hourly_revenue, 15000000)
				), 0)
				FROM incidents i
				LEFT JOIN LATERAL (
					SELECT b.business_value_per_hour, b.id
					FROM business_services b
					JOIN service_sensor_mapping m ON m.business_service_id=b.id
					WHERE m.organization_id=$1 AND m.prtg_sensor_id=i.prtg_sensor_id
					ORDER BY m.dependency_weight DESC LIMIT 1
				) bs ON true
				LEFT JOIN financial_profiles fp ON fp.business_service_id=bs.id AND fp.organization_id=$1 AND fp.valid_to IS NULL
				WHERE i.organization_id=$1 AND i.started_at>=$2 AND i.status!='CLOSED'
			),
			0
		)`, org, monthStart).Scan(&financialExposure)

	// Open critical incidents
	var openCritical int
	a.pool.QueryRow(r.Context(), `SELECT count(*) FROM incidents WHERE organization_id=$1 AND severity='CRITICAL' AND status IN ('OPEN','ACKNOWLEDGED')`, org).Scan(&openCritical)

	jsonOut(w, 200, map[string]any{
		"services_normal":            servicesNormal,
		"services_total":             totalServices,
		"critical_services_impacted": criticalImpacted,
		"estimated_users_affected":   estimatedUsersAffected,
		"sla_compliance_pct":         slaCompliance,
		"financial_exposure":         financialExposure,
		"open_critical_incidents":    openCritical,
	})
}

// ─── BIA: Service Impact ──────────────────────────────────────────────────────

func (a *app) biaServiceImpact(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	rows, err := a.pool.Query(r.Context(), `
		SELECT
			bs.id, bs.name, bs.criticality,
			coalesce(bs.owner_name,''),
			coalesce(bs.affected_users,0),
			coalesce(bs.sla_target_pct,99.9),
			coalesce(bs.description,''),
			coalesce(bs.business_value_per_hour,0),
			count(m.prtg_sensor_id) as total_sensors,
			count(ps.id) FILTER (WHERE ps.last_known_state='down') as sensors_down,
			count(ps.id) FILTER (WHERE ps.last_known_state='warning') as sensors_warning,
			coalesce(SUM(coalesce(i.duration_seconds,extract(epoch from (now()-i.started_at))::int)) FILTER (WHERE i.id IS NOT NULL AND i.started_at>=$2),0)::numeric/60.0 as downtime_min_month
		FROM business_services bs
		LEFT JOIN service_sensor_mapping m ON m.business_service_id=bs.id AND m.organization_id=$1
		LEFT JOIN prtg_sensors ps ON ps.id=m.prtg_sensor_id
		LEFT JOIN incidents i ON i.prtg_sensor_id=m.prtg_sensor_id AND i.organization_id=$1 AND i.started_at>=$2 AND i.status!='CLOSED'
		WHERE bs.organization_id=$1
		GROUP BY bs.id, bs.name, bs.criticality, bs.owner_name, bs.affected_users, bs.sla_target_pct, bs.description, bs.business_value_per_hour
		ORDER BY bs.criticality, bs.name`, org, monthStart)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()

	monthMinutes := now.Sub(monthStart).Minutes()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, crit, owner, desc string
		var users, totalSensors, sensorsDown, sensorsWarning int
		var slaTarget, bvph, downtimeMin float64
		if err := rows.Scan(&id, &name, &crit, &owner, &users, &slaTarget, &desc, &bvph, &totalSensors, &sensorsDown, &sensorsWarning, &downtimeMin); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		status := "normal"
		if sensorsDown > 0 {
			status = "down"
		} else if sensorsWarning > 0 {
			status = "degraded"
		}
		var availPct float64 = 100.0
		if monthMinutes > 0 {
			availPct = (monthMinutes - downtimeMin) / monthMinutes * 100
			if availPct > 100 {
				availPct = 100
			}
		}
		impact := ""
		switch crit {
		case "P1":
			impact = "Critical revenue & operations impacted"
		case "P2":
			impact = "Significant business process disruption"
		case "P3":
			impact = "Minor operational impact"
		default:
			impact = "Low business impact"
		}
		if status != "normal" && desc != "" {
			impact = desc
		}
		out = append(out, map[string]any{
			"service_id": id, "service_name": name, "status": status, "priority": crit,
			"owner": owner, "affected_users": users, "sla_target_pct": slaTarget,
			"availability_pct": availPct, "business_impact": impact,
			"business_value_per_hour": bvph,
			"sensor_count":            totalSensors, "sensors_down": sensorsDown, "sensors_warning": sensorsWarning,
			"downtime_minutes_month": downtimeMin,
		})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

// ─── BIA: Incident Priority Scoring ──────────────────────────────────────────

func (a *app) biaIncidentPriority(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	rows, err := a.pool.Query(r.Context(), `
		SELECT
			i.id, i.status, i.severity, i.started_at,
			coalesce(i.duration_seconds, extract(epoch from (now()-i.started_at))::int) as dur,
			s.device_name, s.sensor_name,
			coalesce(bs.name,'Unmapped'), coalesce(bs.criticality,'P4'),
			coalesce(bs.affected_users,0), coalesce(bs.business_value_per_hour,0),
			coalesce(ic.total_impact,0)
		FROM incidents i
		JOIN prtg_sensors s ON s.id=i.prtg_sensor_id
		LEFT JOIN LATERAL (
			SELECT b.name,b.criticality,b.affected_users,b.business_value_per_hour
			FROM business_services b
			JOIN service_sensor_mapping m ON m.business_service_id=b.id
			WHERE m.organization_id=$1 AND m.prtg_sensor_id=i.prtg_sensor_id
			ORDER BY m.dependency_weight DESC LIMIT 1
		) bs ON true
		LEFT JOIN LATERAL (
			SELECT c.total_impact FROM impact_calculations c
			WHERE c.organization_id=$1 AND c.incident_id=i.id
			ORDER BY c.calculated_at DESC LIMIT 1
		) ic ON true
		WHERE i.organization_id=$1 AND i.status IN ('OPEN','ACKNOWLEDGED')
		ORDER BY i.started_at DESC LIMIT 50`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var status, severity, device, sensor, svcName, crit string
		var started time.Time
		var dur, users int
		var bvph, impact float64
		if err := rows.Scan(&id, &status, &severity, &started, &dur, &device, &sensor, &svcName, &crit, &users, &bvph, &impact); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		if impact <= 0 {
			rate := bvph
			if rate <= 0 {
				switch crit {
				case "P1":
					rate = 50000000
				case "P2":
					rate = 20000000
				case "P3":
					rate = 8000000
				default:
					rate = 3000000
				}
			}
			impact = (float64(dur) / 3600.0) * rate
		}
		// Score factors (1-5)
		critScore := map[string]int{"P1": 5, "P2": 4, "P3": 3, "P4": 2}[crit]
		if critScore == 0 {
			critScore = 1
		}
		userScore := 1
		switch {
		case users >= 500:
			userScore = 5
		case users >= 200:
			userScore = 4
		case users >= 50:
			userScore = 3
		case users >= 10:
			userScore = 2
		}
		finScore := 1
		switch {
		case impact >= 500000000:
			finScore = 5
		case impact >= 100000000:
			finScore = 4
		case impact >= 50000000:
			finScore = 3
		case impact >= 10000000:
			finScore = 2
		}
		urgScore := 1
		switch severity {
		case "CRITICAL":
			urgScore = 5
		case "MAJOR":
			urgScore = 4
		case "MINOR":
			urgScore = 3
		case "WARNING":
			urgScore = 2
		}
		totalScore := critScore * userScore * finScore * urgScore
		priority := "P4"
		switch {
		case totalScore >= 300:
			priority = "P1"
		case totalScore >= 150:
			priority = "P2"
		case totalScore >= 50:
			priority = "P3"
		}
		out = append(out, map[string]any{
			"incident_id": id, "status": status, "severity": severity, "started_at": started,
			"duration_seconds": dur, "device": device, "sensor": sensor,
			"service_name": svcName, "criticality": crit,
			"affected_users": users, "financial_impact": impact,
			"score_criticality": critScore, "score_user_impact": userScore,
			"score_financial": finScore, "score_urgency": urgScore,
			"priority_score": totalScore, "recovery_priority": priority,
		})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

// ─── BIA: Incident Correlation (root-cause / temporal clustering) ────────────

// biaIncidentCorrelation returns incidents grouped by correlation_group_id
// (incidents that started within incident.CorrelationWindow of each other -
// see services/worker) for groups with more than one member, i.e. likely
// "incident storms" sharing a root cause.
func (a *app) biaIncidentCorrelation(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	groupRows, err := a.pool.Query(r.Context(), `
		select i.correlation_group_id, count(*), min(i.started_at), max(i.started_at),
			sum(coalesce(ic.total_impact,0)),
			count(*) filter (where i.status in ('OPEN','ACKNOWLEDGED'))
		from incidents i
		left join lateral (
			select c.total_impact from impact_calculations c
			where c.organization_id=$1 and c.incident_id=i.id
			order by c.calculated_at desc limit 1
		) ic on true
		where i.organization_id=$1 and i.correlation_group_id is not null
		group by i.correlation_group_id
		having count(*) > 1
		order by min(i.started_at) desc
		limit 20`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	type group struct {
		ID                        uuid.UUID
		Count, OpenCount          int
		FirstStarted, LastStarted time.Time
		TotalImpact               float64
	}
	var groups []group
	for groupRows.Next() {
		var g group
		if err := groupRows.Scan(&g.ID, &g.Count, &g.FirstStarted, &g.LastStarted, &g.TotalImpact, &g.OpenCount); err != nil {
			groupRows.Close()
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		groups = append(groups, g)
	}
	groupRows.Close()

	out := []map[string]any{}
	for _, g := range groups {
		memberRows, err := a.pool.Query(r.Context(), `
			select i.id, i.severity, i.status, s.device_name, s.sensor_name, coalesce(bs.name,'Unmapped')
			from incidents i
			join prtg_sensors s on s.id=i.prtg_sensor_id
			left join lateral (
				select b.name from business_services b
				join service_sensor_mapping m on m.business_service_id=b.id
				where m.organization_id=$1 and m.prtg_sensor_id=i.prtg_sensor_id
				order by m.dependency_weight desc limit 1
			) bs on true
			where i.organization_id=$1 and i.correlation_group_id=$2
			order by i.started_at`, org, g.ID)
		if err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		members := []map[string]any{}
		for memberRows.Next() {
			var id uuid.UUID
			var sev, status, dev, sensor, svc string
			if err := memberRows.Scan(&id, &sev, &status, &dev, &sensor, &svc); err != nil {
				memberRows.Close()
				jsonOut(w, 500, map[string]any{"error": err.Error()})
				return
			}
			members = append(members, map[string]any{"incident_id": id, "severity": sev, "status": status, "device": dev, "sensor": sensor, "service_name": svc})
		}
		memberRows.Close()
		out = append(out, map[string]any{
			"correlation_group_id": g.ID,
			"incident_count":       g.Count,
			"open_incident_count":  g.OpenCount,
			"first_started_at":     g.FirstStarted,
			"last_started_at":      g.LastStarted,
			"window_seconds":       int(g.LastStarted.Sub(g.FirstStarted).Seconds()),
			"total_impact":         g.TotalImpact,
			"incidents":            members,
		})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

// ─── BIA: SLA & Downtime Analysis ─────────────────────────────────────────────

func (a *app) biaSLAAnalysis(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthMinutes := now.Sub(monthStart).Minutes()

	rows, err := a.pool.Query(r.Context(), `
		SELECT
			bs.name, coalesce(bs.sla_target_pct,99.9), coalesce(bs.rto_minutes,60), coalesce(bs.rpo_minutes,30),
			coalesce(sum(coalesce(i.duration_seconds,extract(epoch from (now()-i.started_at))::int)) FILTER (WHERE i.id IS NOT NULL AND i.started_at>=$2),0)::float/60.0 as downtime_min,
			coalesce(avg(coalesce(i.duration_seconds,0)) FILTER (WHERE i.ended_at IS NOT NULL AND i.started_at>=$2),0)::float/60.0 as avg_mttr_min,
			count(i.id) FILTER (WHERE i.started_at>=$2) as incident_count_month
		FROM business_services bs
		LEFT JOIN service_sensor_mapping m ON m.business_service_id=bs.id AND m.organization_id=$1
		LEFT JOIN incidents i ON i.prtg_sensor_id=m.prtg_sensor_id AND i.organization_id=$1 AND i.status!='CLOSED'
		WHERE bs.organization_id=$1
		GROUP BY bs.id, bs.name, bs.sla_target_pct, bs.rto_minutes, bs.rpo_minutes
		ORDER BY bs.criticality, bs.name`, org, monthStart)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name string
		var slaTarget, rto, rpo float64
		var downtimeMin, mttrMin float64
		var incidentCount int
		if err := rows.Scan(&name, &slaTarget, &rto, &rpo, &downtimeMin, &mttrMin, &incidentCount); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		availPct := 100.0
		if monthMinutes > 0 {
			availPct = (monthMinutes - downtimeMin) / monthMinutes * 100
			if availPct > 100 {
				availPct = 100
			}
		}
		allowedDowntime := (100.0 - slaTarget) / 100.0 * monthMinutes
		remaining := allowedDowntime - downtimeMin
		slaStatus := "healthy"
		if availPct < slaTarget {
			slaStatus = "breached"
		} else if remaining < 30 {
			slaStatus = "at_risk"
		}
		// MTBF: if incidents > 0, estimate MTBF = (month minutes - downtime) / incidents
		mtbfHours := 0.0
		if incidentCount > 0 {
			mtbfHours = (monthMinutes - downtimeMin) / float64(incidentCount) / 60.0
		}
		out = append(out, map[string]any{
			"service_name":            name,
			"sla_target_pct":          slaTarget,
			"availability_actual_pct": availPct,
			"downtime_minutes_month":  downtimeMin,
			"remaining_allowance_min": remaining,
			"allowed_downtime_min":    allowedDowntime,
			"mttr_minutes":            mttrMin,
			"mtbf_hours":              mtbfHours,
			"incident_count_month":    incidentCount,
			"rto_minutes":             rto,
			"rpo_minutes":             rpo,
			"status":                  slaStatus,
		})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

// ─── BIA: Financial Impact Estimation ─────────────────────────────────────────

func (a *app) biaFinancialImpact(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	rows, err := a.pool.Query(r.Context(), `
		SELECT
			bs.name, bs.criticality, coalesce(bs.business_value_per_hour,0),
			coalesce(fp.hourly_revenue,0), coalesce(fp.service_dependency,1), coalesce(fp.loss_probability,1),
			coalesce(fp.operational_cost_per_hour,0), coalesce(fp.penalty_config->>'fixed','0')::numeric,
			coalesce(fp.affected_employees,0), coalesce(fp.avg_employee_cost_per_hour,0),
			coalesce(sum(coalesce(i.duration_seconds,extract(epoch from (now()-i.started_at))::int)) FILTER (WHERE i.id IS NOT NULL AND i.started_at>=$2),0)::float/60.0 as downtime_min
		FROM business_services bs
		LEFT JOIN financial_profiles fp ON fp.business_service_id=bs.id AND fp.organization_id=$1 AND fp.valid_to IS NULL
		LEFT JOIN service_sensor_mapping m ON m.business_service_id=bs.id AND m.organization_id=$1
		LEFT JOIN incidents i ON i.prtg_sensor_id=m.prtg_sensor_id AND i.organization_id=$1 AND i.status!='CLOSED'
		WHERE bs.organization_id=$1
		GROUP BY bs.id, bs.name, bs.criticality, bs.business_value_per_hour, fp.hourly_revenue, fp.service_dependency, fp.loss_probability, fp.operational_cost_per_hour, fp.penalty_config, fp.affected_employees, fp.avg_employee_cost_per_hour
		ORDER BY bs.criticality, bs.name`, org, monthStart)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name, crit string
		var bvph, hrRev, svcDep, lossPb, opCost, penalty, empCostPh float64
		var employees int
		var downtimeMin float64
		if err := rows.Scan(&name, &crit, &bvph, &hrRev, &svcDep, &lossPb, &opCost, &penalty, &employees, &empCostPh, &downtimeMin); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		downtimeHours := downtimeMin / 60.0
		// Revenue loss: hourly_revenue × downtime_hours × service_dependency × loss_probability
		revLoss := hrRev * downtimeHours * svcDep * lossPb
		// Business value loss: business_value_per_hour × downtime_hours × impact%
		bizLoss := bvph * downtimeHours
		// Productivity loss: employees × downtime_hours × avg_cost_per_hour
		prodLoss := float64(employees) * downtimeHours * empCostPh
		// Operational cost
		opLoss := opCost * downtimeHours
		// SLA penalties (fixed per incident, simplified)
		totalEstimate := revLoss + bizLoss + prodLoss + opLoss + penalty
		out = append(out, map[string]any{
			"service_name":               name,
			"criticality":                crit,
			"downtime_minutes":           downtimeMin,
			"downtime_hours":             downtimeHours,
			"revenue_loss":               revLoss,
			"business_value_loss":        bizLoss,
			"productivity_loss":          prodLoss,
			"operational_cost_loss":      opLoss,
			"sla_penalty":                penalty,
			"total_estimated_loss":       totalEstimate,
			"affected_employees":         employees,
			"avg_employee_cost_per_hour": empCostPh,
		})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

// ─── Impact Matrix CRUD ───────────────────────────────────────────────────────

type impactMatrixInput struct {
	TechnicalCondition string   `json:"technical_condition"`
	OperationalImpact  string   `json:"operational_impact"`
	BusinessImpact     string   `json:"business_impact"`
	AffectedServices   []string `json:"affected_services"`
	Severity           string   `json:"severity"`
	SortOrder          int      `json:"sort_order"`
	IsActive           bool     `json:"is_active"`
}

func (a *app) listImpactMatrix(w http.ResponseWriter, r *http.Request) {
	org := orgID(r)
	rows, err := a.pool.Query(r.Context(), `SELECT id,technical_condition,operational_impact,business_impact,coalesce(affected_services,'{}'),severity,sort_order,is_active,created_at,updated_at FROM impact_matrix_entries WHERE organization_id=$1 ORDER BY sort_order,technical_condition`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var tc, oi, bi, sev string
		var svcs []string
		var sortOrder int
		var isActive bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &tc, &oi, &bi, &svcs, &sev, &sortOrder, &isActive, &createdAt, &updatedAt); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		if svcs == nil {
			svcs = []string{}
		}
		out = append(out, map[string]any{
			"id": id, "technical_condition": tc, "operational_impact": oi, "business_impact": bi,
			"affected_services": svcs, "severity": sev, "sort_order": sortOrder,
			"is_active": isActive, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

func (a *app) createImpactMatrix(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	org := orgID(r)
	var in impactMatrixInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	if in.TechnicalCondition == "" || in.BusinessImpact == "" {
		jsonOut(w, 400, map[string]any{"error": "technical_condition and business_impact are required"})
		return
	}
	sevs := map[string]bool{"INFO": true, "WARNING": true, "MINOR": true, "MAJOR": true, "CRITICAL": true}
	if !sevs[in.Severity] {
		in.Severity = "MAJOR"
	}
	if in.AffectedServices == nil {
		in.AffectedServices = []string{}
	}
	var id uuid.UUID
	err := a.pool.QueryRow(r.Context(), `INSERT INTO impact_matrix_entries(organization_id,technical_condition,operational_impact,business_impact,affected_services,severity,sort_order,is_active) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		org, in.TechnicalCondition, in.OperationalImpact, in.BusinessImpact, in.AffectedServices, in.Severity, in.SortOrder, true).Scan(&id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "impact_matrix.create", "impact_matrix_entries", &id, nil, in)
	jsonOut(w, 201, map[string]any{"id": id})
}

func (a *app) updateImpactMatrix(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/impact-matrix/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid id"})
		return
	}
	org := orgID(r)
	var in impactMatrixInput
	if err = json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	if in.TechnicalCondition == "" || in.BusinessImpact == "" {
		jsonOut(w, 400, map[string]any{"error": "technical_condition and business_impact are required"})
		return
	}
	sevs := map[string]bool{"INFO": true, "WARNING": true, "MINOR": true, "MAJOR": true, "CRITICAL": true}
	if !sevs[in.Severity] {
		in.Severity = "MAJOR"
	}
	if in.AffectedServices == nil {
		in.AffectedServices = []string{}
	}
	cmd, err := a.pool.Exec(r.Context(), `UPDATE impact_matrix_entries SET technical_condition=$1,operational_impact=$2,business_impact=$3,affected_services=$4,severity=$5,sort_order=$6,is_active=$7 WHERE organization_id=$8 AND id=$9`,
		in.TechnicalCondition, in.OperationalImpact, in.BusinessImpact, in.AffectedServices, in.Severity, in.SortOrder, in.IsActive, org, id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if cmd.RowsAffected() == 0 {
		jsonOut(w, 404, map[string]any{"error": "entry not found"})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "impact_matrix.update", "impact_matrix_entries", &id, nil, in)
	jsonOut(w, 200, map[string]any{"id": id})
}

func (a *app) deleteImpactMatrix(w http.ResponseWriter, r *http.Request) {
	if err := adminOnly(r); err != nil {
		jsonOut(w, 403, map[string]any{"error": err.Error()})
		return
	}
	id, err := pathID(r, "/api/v1/impact-matrix/")
	if err != nil {
		jsonOut(w, 400, map[string]any{"error": "invalid id"})
		return
	}
	org := orgID(r)
	cmd, err := a.pool.Exec(r.Context(), `DELETE FROM impact_matrix_entries WHERE organization_id=$1 AND id=$2`, org, id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if cmd.RowsAffected() == 0 {
		jsonOut(w, 404, map[string]any{"error": "entry not found"})
		return
	}
	_ = audit.Record(r.Context(), a.pool, org, "admin", "impact_matrix.delete", "impact_matrix_entries", &id, nil, nil)
	w.WriteHeader(http.StatusNoContent)
}
