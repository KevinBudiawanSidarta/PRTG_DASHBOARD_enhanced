// jetsync mirrors the dashboard's data into JETData.AI so the JET app
// "PRTG BIA Dashboard" can show it from anywhere, with JETData as an
// off-site copy. The flow is one way and outbound only:
//
//	PRTG -> collector/worker -> dashboard API -> jetsync -> JETData forms
//
// It reads the dashboard's own API (so JET shows exactly the numbers the
// dashboard shows) and upserts one JET row per sensor / incident / business
// process, keyed by its dashboard id.
//
//	jetsync -setup [-ui path]   create the JET forms, fields and the custom UI page
//	jetsync [-once]             sync every JETSYNC_INTERVAL_SEC (default POLL_INTERVAL_SEC)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/bia-platform/internal/jetdata"
)

const (
	appGroup       = "PRTG BIA Dashboard" // sidebar group = the JET app
	formSummary    = "PRTG BIA Summary"
	formSensors    = "PRTG BIA Sensors"
	formIncidents  = "PRTG BIA Incidents"
	formProcesses  = "PRTG BIA Business Processes"
	formServices   = "PRTG BIA Service Impact"
	formOverview   = "PRTG BIA Overview" // custom UI page
	incidentWindow = 100                 // most recent incidents mirrored
)

type formSpec struct {
	Name   string
	Key    string
	Fields []jetdata.Field
}

func f(name, label, typ string) jetdata.Field {
	return jetdata.Field{Name: name, Label: label, Type: typ}
}

var auditFields = []jetdata.Field{
	f("Created_By", "Created By", "userInsert"),
	f("Last_Updated_by", "Last Updated by", "userUpdate"),
	f("Time_Created_Server", "Time Created Server", "timestampserver"),
	f("Time_Updated_Server", "Time Updated Server", "lastupdatedserver"),
}

var specs = []formSpec{
	{Name: formSummary, Key: "Snapshot", Fields: []jetdata.Field{
		f("Snapshot", "Snapshot", "text"),
		f("Synced_At", "Synced At (UTC)", "text"),
		f("Source", "Source", "text"),
		f("Sensors_Total", "Sensors Total", "numeric"),
		f("Sensors_Up", "Sensors Up", "numeric"),
		f("Sensors_Down", "Sensors Down", "numeric"),
		f("Sensors_Warning", "Sensors Warning", "numeric"),
		f("Sensors_Unknown", "Sensors Unknown", "numeric"),
		f("Open_Incidents", "Open Incidents", "numeric"),
		f("Critical_Open", "Critical Open Incidents", "numeric"),
		f("Impact_30d", "Impact 30 Days (IDR)", "currency"),
		f("Events_24h", "Events 24h", "numeric"),
		f("BP_Total", "Business Processes", "numeric"),
		f("BP_Down", "Business Processes Down", "numeric"),
		f("BP_Degraded", "Business Processes Degraded", "numeric"),
		f("BP_Loss_Per_Hour", "Running Loss per Hour (IDR)", "currency"),
		f("BP_Loss_MTD", "Business Process Loss MTD (IDR)", "currency"),
		f("BP_Availability_Pct", "Average Availability %", "numeric"),
		f("Service_Loss_MTD", "Service Loss MTD (IDR)", "currency"),
	}},
	{Name: formSensors, Key: "PRTG_Sensor_ID", Fields: []jetdata.Field{
		f("PRTG_Sensor_ID", "PRTG Sensor ID", "text"),
		f("Device", "Device", "text"),
		f("Sensor", "Sensor", "text"),
		f("Status", "Status", "text"),
	}},
	{Name: formIncidents, Key: "Incident_ID", Fields: []jetdata.Field{
		f("Incident_ID", "Incident ID", "text"),
		f("Severity", "Severity", "text"),
		f("Status", "Status", "text"),
		f("Service", "Business Service", "text"),
		f("Criticality", "Criticality", "text"),
		f("Device", "Device", "text"),
		f("Sensor", "Sensor", "text"),
		f("Started_At", "Started At (UTC)", "text"),
		f("Ended_At", "Ended At (UTC)", "text"),
		f("Duration_Min", "Duration (min)", "numeric"),
		f("Impact_IDR", "Financial Impact (IDR)", "currency"),
		f("Correlated", "Correlated Incidents", "numeric"),
	}},
	{Name: formProcesses, Key: "PRTG_Sensor_ID", Fields: []jetdata.Field{
		f("PRTG_Sensor_ID", "PRTG Sensor ID", "text"),
		f("Process_Name", "Business Process", "text"),
		f("State", "State", "text"),
		f("Service", "Business Service", "text"),
		f("Criticality", "Criticality", "text"),
		f("Current_Since", "Current State Since (UTC)", "text"),
		f("Loss_Per_Hour", "Running Loss per Hour (IDR)", "currency"),
		f("Current_Loss", "Loss This Disruption (IDR)", "currency"),
		f("Availability_Pct", "Availability MTD %", "numeric"),
		f("SLA_Target_Pct", "SLA Target %", "numeric"),
		f("SLA_Status", "SLA Status", "text"),
		f("Downtime_Min", "Downtime MTD (min)", "numeric"),
		f("Degraded_Min", "Degraded MTD (min)", "numeric"),
		f("Outages", "Disruptions MTD", "numeric"),
		f("Loss_MTD", "Estimated Loss MTD (IDR)", "currency"),
		f("PRTG_Uptime_Pct", "PRTG Uptime %", "numeric"),
		f("Problem_Components", "Problem Components", "text"),
		f("Warnings", "Warnings", "textarea"),
		f("Components_JSON", "Components (JSON)", "textarea"),
		f("Episodes_JSON", "Recent Disruptions (JSON)", "textarea"),
		// Loss rates and the month-to-date breakdown, as computed by the
		// dashboard, so the JET page can show and simulate the same numbers.
		f("Revenue_Per_Hour", "Revenue Loss per Hour (IDR)", "currency"),
		f("Productivity_Per_Hour", "Productivity Loss per Hour (IDR)", "currency"),
		f("Operational_Per_Hour", "Operational Cost per Hour (IDR)", "currency"),
		f("Degraded_Impact_Pct", "Impact While Degraded %", "numeric"),
		f("Recovery_Fixed", "Recovery Cost per Outage (IDR)", "currency"),
		f("Penalty_Fixed", "SLA Penalty (IDR)", "currency"),
		f("RTO_Min", "RTO (min)", "numeric"),
		f("RTO_Remaining_Min", "RTO Remaining (min)", "text"),
		f("Loss_Revenue_MTD", "Revenue Loss MTD (IDR)", "currency"),
		f("Loss_Productivity_MTD", "Productivity Loss MTD (IDR)", "currency"),
		f("Loss_Operational_MTD", "Operational Loss MTD (IDR)", "currency"),
		f("Loss_Recovery_MTD", "Recovery Cost MTD (IDR)", "currency"),
		f("Loss_Penalty_MTD", "SLA Penalty MTD (IDR)", "currency"),
	}},
	// Keyed by service id: the dashboard allows two services with one name.
	{Name: formServices, Key: "Service_ID", Fields: []jetdata.Field{
		f("Service_ID", "Service ID", "text"),
		f("Service_Name", "Business Service", "text"),
		f("Criticality", "Criticality", "text"),
		f("Downtime_Min", "Downtime MTD (min)", "numeric"),
		f("Revenue_Loss", "Revenue Loss (IDR)", "currency"),
		f("Business_Value_Loss", "Business Value Loss (IDR)", "currency"),
		f("Productivity_Loss", "Productivity Loss (IDR)", "currency"),
		f("Operational_Loss", "Operational Cost (IDR)", "currency"),
		f("SLA_Penalty", "SLA Penalty (IDR)", "currency"),
		f("Total_Loss", "Total Estimated Loss (IDR)", "currency"),
		f("Affected_Employees", "Affected Employees", "numeric"),
		f("SLA_Target_Pct", "SLA Target %", "numeric"),
		f("Availability_Pct", "Availability MTD %", "numeric"),
		f("SLA_Remaining_Min", "SLA Allowance Remaining (min)", "numeric"),
		f("SLA_Status", "SLA Status", "text"),
		f("Incidents_MTD", "Incidents MTD", "numeric"),
	}},
}

func main() {
	setup := flag.Bool("setup", false, "create the JET forms, fields and custom UI page, then exit")
	uiPath := flag.String("ui", "apps/custom-ui/prtg-bia-dashboard.html", "custom UI HTML pushed by -setup")
	once := flag.Bool("once", false, "run a single sync and exit")
	dryRun := flag.Bool("dry-run", false, "compute and log the writes a sync would make without writing to JET (implies -once)")
	dump := flag.String("dump", "", "with -dry-run, also write the rows a sync would send to this JSON file")
	flag.Parse()
	if *dryRun {
		*once = true
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()
	jet, err := jetdata.NewClient()
	if err != nil {
		log.Error("jetsync config", "error", err)
		os.Exit(1)
	}

	if *setup {
		if err := runSetup(ctx, jet, *uiPath, log); err != nil {
			log.Error("jetsync setup failed", "error", err)
			os.Exit(1)
		}
		return
	}

	s := &syncer{jet: jet, log: log, api: apiBase(), org: os.Getenv("ORGANIZATION_ID"), http: &http.Client{Timeout: 60 * time.Second}, dryRun: *dryRun}
	interval := intervalSec()
	log.Info("jetsync started", "component", "jetsync", "jet_host", jet.Host, "api", s.api, "interval_sec", interval, "dry_run", *dryRun)
	run := func() {
		start := time.Now()
		if err := s.syncAll(ctx); err != nil {
			log.Error("jetsync cycle failed", "component", "jetsync", "error", err)
			return
		}
		log.Info("jetsync cycle complete", "component", "jetsync", "duration_ms", time.Since(start).Milliseconds())
	}
	if *dump != "" {
		defer func() {
			b, err := json.MarshalIndent(map[string]any{"form_ids": s.ids, "rows": s.dumped}, "", " ")
			if err == nil {
				err = os.WriteFile(*dump, b, 0o600)
			}
			if err != nil {
				log.Error("jetsync dump failed", "error", err)
			}
		}()
	}
	run()
	if *once {
		return
	}
	t := time.NewTicker(time.Duration(interval) * time.Second)
	defer t.Stop()
	for range t.C {
		run()
	}
}

func apiBase() string {
	if v := strings.TrimRight(os.Getenv("JETSYNC_API_URL"), "/"); v != "" {
		return v
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return "http://127.0.0.1:" + port
}

func intervalSec() int {
	for _, k := range []string{"JETSYNC_INTERVAL_SEC", "POLL_INTERVAL_SEC"} {
		if v, _ := strconv.Atoi(os.Getenv(k)); v > 0 {
			return v
		}
	}
	return 600
}

// ─── Setup ──────────────────────────────────────────────────────────────────

// runSetup is idempotent: it creates only the forms and fields that are
// missing (matched by name) and (re)deploys the custom UI page.
func runSetup(ctx context.Context, jet *jetdata.Client, uiPath string, log *slog.Logger) error {
	forms, err := jet.Forms(ctx)
	if err != nil {
		return err
	}
	ids := map[string]int{}
	for _, spec := range specs {
		id := findForm(forms, spec.Name, "form")
		if id == 0 {
			if id, err = jet.CreateForm(ctx, spec.Name, "form", appGroup); err != nil {
				return fmt.Errorf("create form %q: %w", spec.Name, err)
			}
			log.Info("created form", "id_form", id, "form_name", spec.Name)
		}
		ids[spec.Name] = id
		have, err := jet.FieldNames(ctx, id)
		if err != nil {
			return err
		}
		all := append(append([]jetdata.Field{}, spec.Fields...), auditFields...)
		for i, fld := range all {
			if have[fld.Name] {
				continue
			}
			fld.Order = i + 1
			if err := jet.CreateField(ctx, id, fld); err != nil {
				return fmt.Errorf("form %d %q field %s: %w", id, spec.Name, fld.Name, err)
			}
		}
		log.Info("form ready", "id_form", id, "form_name", spec.Name)
	}

	raw, err := os.ReadFile(uiPath)
	if err != nil {
		return fmt.Errorf("read custom UI: %w", err)
	}
	html := strings.NewReplacer(
		"{{JET_HOST}}", jet.Host,
		"{{FORM_SUMMARY}}", strconv.Itoa(ids[formSummary]),
		"{{FORM_SENSORS}}", strconv.Itoa(ids[formSensors]),
		"{{FORM_INCIDENTS}}", strconv.Itoa(ids[formIncidents]),
		"{{FORM_PROCESSES}}", strconv.Itoa(ids[formProcesses]),
		"{{FORM_SERVICES}}", strconv.Itoa(ids[formServices]),
	).Replace(string(raw))
	uiID := findForm(forms, formOverview, "custom")
	if uiID == 0 {
		if uiID, err = jet.CreateCustomForm(ctx, formOverview, appGroup, html); err != nil {
			return fmt.Errorf("create custom form: %w", err)
		}
		log.Info("created custom form", "id_form", uiID, "form_name", formOverview)
	} else if err := jet.UpdateCustomForm(ctx, uiID, html); err != nil {
		return fmt.Errorf("update custom form: %w", err)
	}
	stored, err := jet.CustomFormHTML(ctx, uiID)
	if err != nil {
		return fmt.Errorf("verify custom form: %w", err)
	}
	if stored != html {
		return fmt.Errorf("custom form %d stored %d bytes, expected %d", uiID, len(stored), len(html))
	}
	if forms, err = jet.Forms(ctx); err != nil {
		return err
	}
	for _, fm := range forms {
		if fm.ID == uiID && fm.Type != "custom" {
			return fmt.Errorf("form %d is %q, not custom; JET would show a record list", uiID, fm.Type)
		}
	}
	log.Info("custom UI deployed and verified", "id_form", uiID, "form_name", formOverview, "bytes", len(html))
	return nil
}

func findForm(forms []jetdata.Form, name, typ string) int {
	for _, fm := range forms {
		if strings.TrimSpace(fm.Name) == name && fm.Type == typ {
			return fm.ID
		}
	}
	return 0
}

// ─── Sync ───────────────────────────────────────────────────────────────────

type syncer struct {
	jet    *jetdata.Client
	log    *slog.Logger
	api    string
	org    string
	http   *http.Client
	ids    map[string]int
	dryRun bool
	dumped map[string][]map[string]string
}

func (s *syncer) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Organization-ID", s.org)
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard API %s: HTTP %d: %.200s", path, resp.StatusCode, string(b))
	}
	return json.Unmarshal(b, out)
}

func (s *syncer) formIDs(ctx context.Context) error {
	if s.ids != nil {
		return nil
	}
	forms, err := s.jet.Forms(ctx)
	if err != nil {
		return err
	}
	ids := map[string]int{}
	for _, spec := range specs {
		id := findForm(forms, spec.Name, "form")
		if id == 0 {
			return fmt.Errorf("JET form %q not found; run jetsync -setup first", spec.Name)
		}
		ids[spec.Name] = id
	}
	s.ids = ids
	return nil
}

type apiSensor struct {
	PRTGSensorID string `json:"prtg_sensor_id"`
	Device       string `json:"device_name"`
	Sensor       string `json:"sensor_name"`
	State        string `json:"last_known_state"`
}

type apiIncident struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Severity   string  `json:"severity"`
	StartedAt  string  `json:"started_at"`
	EndedAt    *string `json:"ended_at"`
	Duration   float64 `json:"duration_seconds"`
	Impact     float64 `json:"total_impact"`
	Correlated int     `json:"correlated_incident_count"`
	Sensor     struct {
		Device string `json:"device"`
		Name   string `json:"name"`
	} `json:"sensor"`
	Service struct {
		Name        string `json:"name"`
		Criticality string `json:"criticality"`
	} `json:"service"`
}

type apiLoss struct {
	Revenue      float64 `json:"revenue"`
	Productivity float64 `json:"productivity"`
	Operational  float64 `json:"operational"`
	Recovery     float64 `json:"recovery"`
	Penalty      float64 `json:"penalty"`
	Total        float64 `json:"total"`
}

type apiServiceLoss struct {
	ID            string  `json:"service_id"`
	Name          string  `json:"service_name"`
	Criticality   string  `json:"criticality"`
	DowntimeMin   float64 `json:"downtime_minutes"`
	Revenue       float64 `json:"revenue_loss"`
	BusinessValue float64 `json:"business_value_loss"`
	Productivity  float64 `json:"productivity_loss"`
	Operational   float64 `json:"operational_cost_loss"`
	Penalty       float64 `json:"sla_penalty"`
	Total         float64 `json:"total_estimated_loss"`
	Employees     int     `json:"affected_employees"`
}

type apiServiceSLA struct {
	ID           string  `json:"service_id"`
	TargetPct    float64 `json:"sla_target_pct"`
	ActualPct    float64 `json:"availability_actual_pct"`
	RemainingMin float64 `json:"remaining_allowance_min"`
	Status       string  `json:"status"`
	Incidents    int     `json:"incident_count_month"`
}

type apiProcess struct {
	PRTGSensorID string  `json:"prtg_sensor_id"`
	Name         string  `json:"name"`
	State        string  `json:"state"`
	RemovedAt    *string `json:"removed_at"`
	PRTG         *struct {
		UptimePct float64 `json:"uptime_pct"`
	} `json:"prtg"`
	Service *struct {
		Name        string `json:"name"`
		Criticality string `json:"criticality"`
	} `json:"service"`
	Current struct {
		LossPerHour  float64  `json:"loss_per_hour"`
		Since        *string  `json:"since"`
		LossSoFar    *float64 `json:"loss_so_far"`
		RTORemaining *float64 `json:"rto_remaining_seconds"`
	} `json:"current"`
	DegradedImpactPct float64 `json:"degraded_impact_pct"`
	Rates             struct {
		Revenue      float64 `json:"revenue_per_hour"`
		Productivity float64 `json:"productivity_per_hour"`
		Operational  float64 `json:"operational_per_hour"`
		Recovery     float64 `json:"recovery_fixed"`
		Penalty      float64 `json:"penalty_fixed"`
		RTOSeconds   float64 `json:"rto_seconds"`
	} `json:"rates"`
	Stats struct {
		AvailabilityPct float64 `json:"availability_pct"`
		DownSeconds     float64 `json:"down_seconds"`
		WarningSeconds  float64 `json:"warning_seconds"`
		Outages         int     `json:"outages"`
		Degradations    int     `json:"degradations"`
		Loss            apiLoss `json:"loss"`
	} `json:"stats"`
	SLA *struct {
		TargetPct float64 `json:"target_pct"`
		Status    string  `json:"status"`
	} `json:"sla"`
	Channels []struct {
		ID      int      `json:"id"`
		Name    string   `json:"name"`
		State   string   `json:"state"`
		UpPct   *float64 `json:"up_pct"`
		Members []struct {
			Name       string `json:"name"`
			Parent     string `json:"parent"`
			State      string `json:"state"`
			CountsAsUp bool   `json:"counts_as_up"`
		} `json:"members"`
	} `json:"channels"`
	Episodes []struct {
		Start    string  `json:"start"`
		Ongoing  bool    `json:"ongoing"`
		Duration float64 `json:"duration_seconds"`
		Worst    string  `json:"worst"`
		Loss     apiLoss `json:"loss"`
		Causes   []struct {
			Name string `json:"channel_name"`
		} `json:"causes"`
	} `json:"episodes"`
	Warnings []string `json:"warnings"`
}

type apiProcesses struct {
	Summary struct {
		Total           int      `json:"total"`
		Down            int      `json:"down"`
		Degraded        int      `json:"degraded"`
		LossPerHour     float64  `json:"current_loss_per_hour"`
		PeriodLoss      float64  `json:"period_loss"`
		AvgAvailability *float64 `json:"avg_availability_pct"`
	} `json:"summary"`
	Items []apiProcess `json:"items"`
}

func (s *syncer) syncAll(ctx context.Context) error {
	if err := s.formIDs(ctx); err != nil {
		return err
	}
	var summary struct {
		Open   int     `json:"open_incidents"`
		Impact float64 `json:"total_impact"`
		Events int     `json:"events_24h"`
	}
	var sensors struct {
		Items []apiSensor `json:"items"`
	}
	var incidents struct {
		Items []apiIncident `json:"items"`
	}
	var procs apiProcesses
	var svcLoss struct {
		Items []apiServiceLoss `json:"items"`
	}
	var svcSLA struct {
		Items []apiServiceSLA `json:"items"`
	}
	for path, out := range map[string]any{
		"/api/v1/dashboard/summary": &summary,
		"/api/v1/sensors":           &sensors,
		fmt.Sprintf("/api/v1/incidents?limit=%d", incidentWindow): &incidents,
		"/api/v1/business-processes?period=mtd":                   &procs,
		"/api/v1/bia/financial-impact":                            &svcLoss,
		"/api/v1/bia/sla-analysis":                                &svcSLA,
	} {
		if err := s.get(ctx, path, out); err != nil {
			return err
		}
	}

	sensorRows := make([]map[string]string, 0, len(sensors.Items))
	counts := map[string]int{}
	for _, x := range sensors.Items {
		state := x.State
		if state != "up" && state != "down" && state != "warning" {
			state = "unknown"
		}
		counts[state]++
		sensorRows = append(sensorRows, map[string]string{"PRTG_Sensor_ID": x.PRTGSensorID, "Device": x.Device, "Sensor": x.Sensor, "Status": state})
	}

	incidentRows := make([]map[string]string, 0, len(incidents.Items))
	criticalOpen := 0
	for _, x := range incidents.Items {
		open := x.Status == "OPEN" || x.Status == "ACKNOWLEDGED"
		if open && x.Severity == "CRITICAL" {
			criticalOpen++
		}
		ended := ""
		if x.EndedAt != nil {
			ended = utc(*x.EndedAt)
		}
		incidentRows = append(incidentRows, map[string]string{
			"Incident_ID": x.ID, "Severity": x.Severity, "Status": x.Status,
			"Service": orDefault(x.Service.Name, "Unmapped"), "Criticality": x.Service.Criticality,
			"Device": x.Sensor.Device, "Sensor": x.Sensor.Name,
			"Started_At": utc(x.StartedAt), "Ended_At": ended,
			"Duration_Min": num(x.Duration/60, 0), "Impact_IDR": num(x.Impact, 2),
			"Correlated": strconv.Itoa(x.Correlated),
		})
	}

	processRows := []map[string]string{}
	for _, p := range procs.Items {
		if p.RemovedAt != nil {
			continue
		}
		processRows = append(processRows, processRow(p))
	}

	slaByID := map[string]apiServiceSLA{}
	for _, x := range svcSLA.Items {
		slaByID[x.ID] = x
	}
	serviceRows := []map[string]string{}
	var serviceLoss float64
	for _, x := range svcLoss.Items {
		if x.ID == "" {
			continue
		}
		serviceLoss += x.Total
		row := map[string]string{
			"Service_ID": x.ID, "Service_Name": x.Name, "Criticality": x.Criticality,
			"Downtime_Min": num(x.DowntimeMin, 0), "Revenue_Loss": num(x.Revenue, 2),
			"Business_Value_Loss": num(x.BusinessValue, 2), "Productivity_Loss": num(x.Productivity, 2),
			"Operational_Loss": num(x.Operational, 2), "SLA_Penalty": num(x.Penalty, 2),
			"Total_Loss": num(x.Total, 2), "Affected_Employees": strconv.Itoa(x.Employees),
			"SLA_Target_Pct": "", "Availability_Pct": "", "SLA_Remaining_Min": "", "SLA_Status": "", "Incidents_MTD": "",
		}
		if sla, ok := slaByID[x.ID]; ok {
			row["SLA_Target_Pct"], row["Availability_Pct"] = num(sla.TargetPct, 2), num(sla.ActualPct, 3)
			row["SLA_Remaining_Min"], row["SLA_Status"] = num(sla.RemainingMin, 0), sla.Status
			row["Incidents_MTD"] = strconv.Itoa(sla.Incidents)
		}
		serviceRows = append(serviceRows, row)
	}

	avail := ""
	if procs.Summary.AvgAvailability != nil {
		avail = num(*procs.Summary.AvgAvailability, 2)
	}
	summaryRow := map[string]string{
		"Snapshot": "current", "Synced_At": time.Now().UTC().Format(time.RFC3339), "Source": s.api,
		"Sensors_Total": strconv.Itoa(len(sensors.Items)), "Sensors_Up": strconv.Itoa(counts["up"]),
		"Sensors_Down": strconv.Itoa(counts["down"]), "Sensors_Warning": strconv.Itoa(counts["warning"]),
		"Sensors_Unknown": strconv.Itoa(counts["unknown"]),
		"Open_Incidents":  strconv.Itoa(summary.Open), "Critical_Open": strconv.Itoa(criticalOpen),
		"Impact_30d": num(summary.Impact, 2), "Events_24h": strconv.Itoa(summary.Events),
		"BP_Total": strconv.Itoa(procs.Summary.Total), "BP_Down": strconv.Itoa(procs.Summary.Down),
		"BP_Degraded":      strconv.Itoa(procs.Summary.Degraded),
		"BP_Loss_Per_Hour": num(procs.Summary.LossPerHour, 2), "BP_Loss_MTD": num(procs.Summary.PeriodLoss, 2),
		"BP_Availability_Pct": avail,
		"Service_Loss_MTD":    num(serviceLoss, 2),
	}

	// Details first, summary last: the summary's Synced_At then tells the
	// JET page when a complete snapshot landed.
	for _, step := range []struct {
		form string
		rows []map[string]string
	}{
		{formSensors, sensorRows},
		{formIncidents, incidentRows},
		{formProcesses, processRows},
		{formServices, serviceRows},
		{formSummary, []map[string]string{summaryRow}},
	} {
		if err := s.apply(ctx, step.form, step.rows); err != nil {
			return err
		}
	}
	return nil
}

func processRow(p apiProcess) map[string]string {
	row := map[string]string{
		"PRTG_Sensor_ID": p.PRTGSensorID, "Process_Name": p.Name, "State": p.State,
		"Service": "", "Criticality": "", "Current_Since": "",
		"Loss_Per_Hour": num(p.Current.LossPerHour, 2), "Current_Loss": "0",
		"Availability_Pct": num(p.Stats.AvailabilityPct, 2), "SLA_Target_Pct": "", "SLA_Status": "",
		"Downtime_Min": num(p.Stats.DownSeconds/60, 0), "Degraded_Min": num(p.Stats.WarningSeconds/60, 0),
		"Outages": strconv.Itoa(p.Stats.Outages + p.Stats.Degradations), "Loss_MTD": num(p.Stats.Loss.Total, 2),
		"PRTG_Uptime_Pct": "", "Warnings": strings.Join(p.Warnings, "\n"),
		"Revenue_Per_Hour": num(p.Rates.Revenue, 2), "Productivity_Per_Hour": num(p.Rates.Productivity, 2),
		"Operational_Per_Hour": num(p.Rates.Operational, 2), "Degraded_Impact_Pct": num(p.DegradedImpactPct, 2),
		"Recovery_Fixed": num(p.Rates.Recovery, 2), "Penalty_Fixed": num(p.Rates.Penalty, 2),
		"RTO_Min": num(p.Rates.RTOSeconds/60, 0), "RTO_Remaining_Min": "",
		"Loss_Revenue_MTD": num(p.Stats.Loss.Revenue, 2), "Loss_Productivity_MTD": num(p.Stats.Loss.Productivity, 2),
		"Loss_Operational_MTD": num(p.Stats.Loss.Operational, 2), "Loss_Recovery_MTD": num(p.Stats.Loss.Recovery, 2),
		"Loss_Penalty_MTD": num(p.Stats.Loss.Penalty, 2),
	}
	if p.Current.RTORemaining != nil {
		// Signed: negative means the outage has already run past the RTO.
		row["RTO_Remaining_Min"] = num(*p.Current.RTORemaining/60, 0)
	}
	if p.Service != nil {
		row["Service"], row["Criticality"] = p.Service.Name, p.Service.Criticality
	}
	if p.Current.Since != nil {
		row["Current_Since"] = utc(*p.Current.Since)
	}
	if p.Current.LossSoFar != nil {
		row["Current_Loss"] = num(*p.Current.LossSoFar, 2)
	}
	if p.SLA != nil {
		row["SLA_Target_Pct"], row["SLA_Status"] = num(p.SLA.TargetPct, 2), p.SLA.Status
	}
	if p.PRTG != nil {
		row["PRTG_Uptime_Pct"] = num(p.PRTG.UptimePct, 2)
	}

	type member struct {
		Name   string `json:"n"`
		Parent string `json:"p,omitempty"`
		State  string `json:"s"`
		Up     bool   `json:"u"`
	}
	type component struct {
		Name    string   `json:"name"`
		State   string   `json:"state"`
		UpPct   *float64 `json:"up_pct"`
		Members []member `json:"members"`
	}
	var comps []component
	var problems []string
	for _, c := range p.Channels {
		if c.ID == 0 {
			continue
		}
		comp := component{Name: c.Name, State: c.State, UpPct: c.UpPct}
		for _, m := range c.Members {
			comp.Members = append(comp.Members, member{Name: m.Name, Parent: m.Parent, State: m.State, Up: m.CountsAsUp})
		}
		comps = append(comps, comp)
		if c.State != "up" {
			problems = append(problems, c.Name+" ("+c.State+")")
		}
	}
	b, _ := json.Marshal(comps)
	row["Components_JSON"] = string(b)
	row["Problem_Components"] = strings.Join(problems, ", ")

	type episode struct {
		Start    string  `json:"start"`
		Ongoing  bool    `json:"ongoing"`
		Duration float64 `json:"duration_s"`
		Worst    string  `json:"worst"`
		Causes   string  `json:"causes"`
		Loss     float64 `json:"loss"`
	}
	var eps []episode
	for i, e := range p.Episodes {
		if i == 5 {
			break
		}
		var causes []string
		for _, c := range e.Causes {
			causes = append(causes, c.Name)
		}
		eps = append(eps, episode{Start: utc(e.Start), Ongoing: e.Ongoing, Duration: float64(int64(e.Duration)), Worst: e.Worst, Causes: strings.Join(causes, ", "), Loss: float64(int64(e.Loss.Total))})
	}
	b, _ = json.Marshal(eps)
	row["Episodes_JSON"] = string(b)
	return row
}

// apply diffs one form against the desired rows and writes the changes.
func (s *syncer) apply(ctx context.Context, form string, rows []map[string]string) error {
	id := s.ids[form]
	key := ""
	for _, spec := range specs {
		if spec.Name == form {
			key = spec.Key
		}
	}
	existing, err := s.jet.AllRecords(ctx, id)
	if err != nil {
		return fmt.Errorf("read %q: %w", form, err)
	}
	plan := jetdata.Diff(existing, rows, key)
	if s.dryRun {
		if s.dumped == nil {
			s.dumped = map[string][]map[string]string{}
		}
		s.dumped[form] = rows
		s.log.Info("jet form dry run (nothing written)", "component", "jetsync", "id_form", id, "form_name", form,
			"rows", len(rows), "would_create", len(plan.Create), "would_update", len(plan.Update), "would_delete", len(plan.Delete))
		return nil
	}
	for i := 0; i < len(plan.Create); i += 100 {
		end := i + 100
		if end > len(plan.Create) {
			end = len(plan.Create)
		}
		if err := s.jet.CreateBatch(ctx, id, plan.Create[i:end]); err != nil {
			return fmt.Errorf("create in %q: %w", form, err)
		}
	}
	for _, u := range plan.Update {
		if err := s.jet.UpdateRecord(ctx, id, u.ID, u.Values); err != nil {
			return fmt.Errorf("update %q row %d: %w", form, u.ID, err)
		}
	}
	for _, rowID := range plan.Delete {
		if err := s.jet.DeleteRecord(ctx, id, rowID); err != nil {
			return fmt.Errorf("delete %q row %d: %w", form, rowID, err)
		}
	}
	s.log.Info("jet form synced", "component", "jetsync", "id_form", id, "form_name", form,
		"rows", len(rows), "created", len(plan.Create), "updated", len(plan.Update), "deleted", len(plan.Delete))
	return nil
}

// utc normalizes an API timestamp to RFC3339 UTC (seconds precision).
func utc(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

func num(v float64, decimals int) string { return strconv.FormatFloat(v, 'f', decimals, 64) }

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
