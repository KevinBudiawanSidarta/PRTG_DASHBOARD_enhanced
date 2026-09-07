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

	"github.com/example/bia-platform/internal/analytics"
	"github.com/example/bia-platform/internal/audit"
	"github.com/example/bia-platform/internal/auth"
	"github.com/example/bia-platform/internal/incident"
	"github.com/example/bia-platform/internal/ingestion"
	"github.com/example/bia-platform/internal/platform/db"
	"github.com/example/bia-platform/internal/prtg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
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
	base := `select i.id,i.status,i.severity,i.started_at,i.ended_at,coalesce(i.duration_seconds,extract(epoch from (now()-i.started_at))::int),s.device_name,s.sensor_name,coalesce(bs.name,''),coalesce(bs.criticality,'P4'),coalesce(ic.total_impact,0),coalesce(im.version,'') from incidents i join prtg_sensors s on s.id=i.prtg_sensor_id left join lateral (select b.id,b.name,b.criticality from business_services b join service_sensor_mapping m on m.business_service_id=b.id where m.organization_id=$1 and m.prtg_sensor_id=i.prtg_sensor_id order by m.dependency_weight desc limit 1) bs on true left join lateral (select c.total_impact,c.impact_model_id from impact_calculations c where c.organization_id=$1 and c.incident_id=i.id order by c.calculated_at desc limit 1) ic on true left join impact_models im on im.id=ic.impact_model_id where i.organization_id=$1`
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
		if err := rows.Scan(&id, &status, &severity, &started, &ended, &dur, &device, &sensor, &service, &criticality, &impact, &model); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		m := map[string]any{"id": id, "status": status, "severity": severity, "started_at": started, "ended_at": ended, "duration_seconds": dur, "sensor": map[string]any{"device": device, "name": sensor}, "service": map[string]any{"name": service, "criticality": criticality}, "total_impact": impact, "model_version": model}
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
	err = a.pool.QueryRow(r.Context(), `select i.status,i.severity,s.device_name,s.sensor_name,i.started_at,i.ended_at,coalesce(i.duration_seconds,extract(epoch from(now()-i.started_at))::int),bs.id,coalesce(bs.name,''),coalesce(bs.criticality,'P4') from incidents i join prtg_sensors s on s.id=i.prtg_sensor_id left join lateral(select b.id,b.name,b.criticality from business_services b join service_sensor_mapping m on m.business_service_id=b.id where m.organization_id=$1 and m.prtg_sensor_id=i.prtg_sensor_id order by m.dependency_weight desc limit 1) bs on true where i.organization_id=$1 and i.id=$2`, org, id).Scan(&status, &severity, &device, &sensor, &started, &ended, &dur, &serviceID, &service, &criticality)
	if err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "incident not found"})
		return
	}
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
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
	jsonOut(w, 200, map[string]any{"id": id, "status": status, "severity": severity, "started_at": started, "ended_at": ended, "duration_seconds": dur, "sensor": map[string]any{"device": device, "name": sensor}, "service": map[string]any{"name": service, "criticality": criticality}, "impact": impact})
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
	err = a.pool.QueryRow(r.Context(), `select status from incidents where organization_id=$1 and id=$2`, org, id).Scan(&current)
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
	_, err = a.pool.Exec(r.Context(), `update incidents set status=$1 where organization_id=$2 and id=$3`, string(to), org, id)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_, _ = a.pool.Exec(r.Context(), `insert into incident_events(organization_id,incident_id,event_type,actor) values($1,$2,$3,$4)`, org, id, strings.ToLower(string(to)), claims.Role)
	_ = audit.Record(r.Context(), a.pool, org, claims.Role, "incident."+action, "incident", &id, map[string]any{"status": current}, map[string]any{"status": to})
	jsonOut(w, 200, map[string]any{"id": id, "status": to})
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
	rows, err := a.pool.Query(r.Context(), `select id,name,criticality from business_services where organization_id=$1 order by criticality,name`, org)
	if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, criticality string
		if err := rows.Scan(&id, &name, &criticality); err != nil {
			jsonOut(w, 500, map[string]any{"error": err.Error()})
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "criticality": criticality})
	}
	jsonOut(w, 200, map[string]any{"items": out})
}

type serviceInput struct {
	Name        string `json:"name"`
	Criticality string `json:"criticality"`
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
	if in.Name == "" || in.Criticality <= "" || !map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true}[in.Criticality] {
		jsonOut(w, 400, map[string]any{"error": "name and criticality P1-P4 are required"})
		return
	}
	var id uuid.UUID
	err := a.pool.QueryRow(r.Context(), `insert into business_services(organization_id,name,criticality) values($1,$2,$3) returning id`, org, in.Name, in.Criticality).Scan(&id)
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
	var oldName, oldCrit string
	if err = a.pool.QueryRow(r.Context(), `select name,criticality from business_services where organization_id=$1 and id=$2`, org, id).Scan(&oldName, &oldCrit); err == pgx.ErrNoRows {
		jsonOut(w, 404, map[string]any{"error": "service not found"})
		return
	} else if err != nil {
		jsonOut(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_, err = a.pool.Exec(r.Context(), `update business_services set name=$1,criticality=$2 where organization_id=$3 and id=$4`, in.Name, in.Criticality, org, id)
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
