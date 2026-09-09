# ==============================================================================
# Sync LIVE BIA Platform data (Postgres, via the running API) into JETData.AI.
#
# Unlike export_to_jetdata.ps1 (which seeds fixed demo/sample data once), this
# script pulls whatever is currently in the database through the API and
# replaces the contents of the 5 BIA forms in JET with it. Safe to re-run any
# time — each form is fully cleared and rewritten from the live source, so
# there is no possibility of accumulating duplicates.
#
# Requires the 5 BIA forms to already exist in JET (run export_to_jetdata.ps1
# once first if they don't) and the BIA API to be running and reachable.
# ==============================================================================
param(
    [string]$JetHost = $(if ($env:JET_HOST) { $env:JET_HOST } else { "https://indo1.jetdata.ai/JET" }),
    [string]$Project = $(if ($env:JET_PROJECT) { $env:JET_PROJECT } else { "DCMG" }),
    [string]$Username = $(if ($env:JET_USERNAME) { $env:JET_USERNAME } else { "kevin" }),
    [string]$Password = $env:JET_PASSWORD,
    [string]$ApiUrl = $(if ($env:API_URL) { $env:API_URL } else { "http://localhost:8080" }),
    [string]$OrganizationId = $env:ORGANIZATION_ID,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

if (-not $Password -or -not $OrganizationId) {
    . (Join-Path $PSScriptRoot "_env.ps1")
    if (-not $Password) { $Password = $env:JET_PASSWORD }
    if (-not $OrganizationId) { $OrganizationId = $env:ORGANIZATION_ID }
}
if (-not $Password) {
    Write-Error "JET_PASSWORD not set. Add JET_PASSWORD=... to .env, export `$env:JET_PASSWORD, or pass -Password."
    exit 1
}
if (-not $OrganizationId) {
    Write-Error "ORGANIZATION_ID not set. Add ORGANIZATION_ID=... to .env or pass -OrganizationId."
    exit 1
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  BIA PLATFORM LIVE DATA -> JETDATA SYNC ($Project)" -ForegroundColor Cyan
if ($DryRun) { Write-Host "  (DRY RUN - nothing will be written to JET)" -ForegroundColor Yellow }
Write-Host "==========================================================" -ForegroundColor Cyan

# ── BIA API helper ───────────────────────────────────────────────────────────
function Get-ApiData {
    param([string]$Path)
    $headers = @{ "X-Organization-ID" = $OrganizationId }
    return Invoke-RestMethod -Uri "$ApiUrl$Path" -Headers $headers -Method Get
}

# ── JET API helper (same pattern as export_to_jetdata.ps1) ─────────────────
function Invoke-JetApi {
    param([string]$Method, [hashtable]$Params = @{})
    $args = @("-s", "-X", "POST", "$JetHost/api_v3/api.php",
              "-F", "method=$Method", "-F", "project=$Project", "-F", "session_key=$script:sessionKey")
    foreach ($k in $Params.Keys) {
        $v = $Params[$k]
        if ($null -ne $v -and $v -ne "") {
            $args += "-F"
            $args += "$k=$v"
        }
    }
    $raw = & curl.exe @args
    return $raw | ConvertFrom-Json
}

# Wipe every existing record in a form, then insert a fresh set.
function Sync-JetForm {
    param([string]$FormId, [string]$FormLabel, [array]$Records)

    Write-Host "`n[$FormLabel] (form $FormId)" -ForegroundColor Yellow
    Write-Host "  -> $($Records.Count) live record(s) to sync" -ForegroundColor Gray

    if ($DryRun) {
        Write-Host "  -> DRY RUN: would clear form $FormId and insert $($Records.Count) record(s)" -ForegroundColor DarkYellow
        return
    }

    # 1. List existing rows
    $existing = Invoke-JetApi -Method "getRecords" -Params @{ "id_form" = $FormId; "limit" = 1000 }
    $existingRows = @()
    if ($existing.status -eq "success" -and $existing.data) { $existingRows = $existing.data }

    # 2. Delete them all
    $deleted = 0
    foreach ($row in $existingRows) {
        $res = Invoke-JetApi -Method "deleteRecord" -Params @{ "id_form" = $FormId; "id_row" = $row.id_row }
        if ($res.status -eq "success") { $deleted++ }
    }
    Write-Host "  -> Cleared $deleted existing record(s)" -ForegroundColor Gray

    # 3. Insert fresh records
    $created = 0
    foreach ($rec in $Records) {
        $p = @{ "id_form" = $FormId }
        foreach ($k in $rec.Keys) { $p[$k] = $rec[$k] }
        $res = Invoke-JetApi -Method "createRecord" -Params $p
        if ($res.status -eq "success") { $created++ } else { Write-Host "    ! failed: $($res.message)" -ForegroundColor Red }
    }
    Write-Host "  -> Inserted $created record(s)" -ForegroundColor Green
}

# 1. Authenticate to JET
Write-Host "`n[1/3] Authenticating to $JetHost (Project: $Project, User: $Username)..." -ForegroundColor Yellow
$authJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" -F "method=authenticate" -F "project=$Project" -F "username=$Username" -F "password=$Password"
$authRes = $authJson | ConvertFrom-Json
if ($authRes.status -ne "success" -or -not $authRes.session_key) {
    Write-Error "JET authentication failed: $($authRes.message)"
    exit 1
}
$sessionKey = $authRes.session_key
Write-Host "  -> Auth SUCCESS! Logged in as $($authRes.user.first_name) $($authRes.user.last_name)" -ForegroundColor Green

# 2. Verify the BIA forms already exist (created by export_to_jetdata.ps1)
Write-Host "`n[2/3] Locating BIA forms in project $Project..." -ForegroundColor Yellow
$formsListRes = Invoke-JetApi -Method "getFormsList"
$forms = @{}
if ($formsListRes.status -eq "success" -and $formsListRes.forms) {
    foreach ($f in $formsListRes.forms) { $forms[$f.form_name] = $f.id_form }
}
$required = "BIA Services", "BIA Financial Profiles", "BIA Sensors and Mappings", "BIA Incidents and Impacts", "BIA Knowledge Base"
$missing = $required | Where-Object { -not $forms.ContainsKey($_) }
if ($missing) {
    Write-Error "Missing form(s) in JET: $($missing -join ', '). Run scripts\export_to_jetdata.ps1 once first to create them."
    exit 1
}
Write-Host "  -> All 5 BIA forms found" -ForegroundColor Green

# 3. Pull live data from the BIA API and sync each form
Write-Host "`n[3/3] Pulling live data from $ApiUrl and syncing to JET..." -ForegroundColor Yellow

# --- BIA Services ---
# Skip incomplete/junk service records (blank description AND zero business
# value) rather than mirroring them into JET — e.g. accidental empty entries
# created via the Service Mapping tab's "+ New Service" form.
$allServicesRaw = (Get-ApiData "/api/v1/services").items
$services = $allServicesRaw | Where-Object { $_.description -or $_.business_value_per_hour -gt 0 }
$skipped = $allServicesRaw.Count - $services.Count
if ($skipped -gt 0) {
    Write-Host "  -> Skipping $skipped incomplete/junk service record(s) (blank description, zero business value)" -ForegroundColor DarkYellow
}
$serviceRecords = $services | ForEach-Object {
    @{
        "Service_Name" = $_.name
        "Criticality" = $_.criticality
        "Owner_Name" = $_.owner_name
        "Affected_Users" = $_.affected_users
        "SLA_Target" = $_.sla_target_pct
        "RTO_Minutes" = $_.rto_minutes
        "RPO_Minutes" = $_.rpo_minutes
        "Business_Value_Per_Hour" = $_.business_value_per_hour
        "Description" = $_.description
    }
}
Sync-JetForm -FormId $forms["BIA Services"] -FormLabel "BIA Services" -Records $serviceRecords

# --- BIA Financial Profiles (active versions only) ---
$profiles = (Get-ApiData "/api/v1/financial-profiles").items | Where-Object { $_.active }
$profileRecords = $profiles | ForEach-Object {
    @{
        "Service_Name" = $_.service_name
        "Hourly_Revenue" = $_.hourly_revenue
        "Transactions_Per_Hour" = $_.transactions_per_hour
        "Avg_Transaction_Value" = $_.avg_transaction_value
        "Service_Dependency" = $_.service_dependency
        "Loss_Probability" = $_.loss_probability
        "Operational_Cost_Per_Hour" = $_.operational_cost_per_hour
        "Penalty_Fixed" = $_.penalty_fixed
        "Recovery_Fixed" = $_.recovery_fixed
    }
}
Sync-JetForm -FormId $forms["BIA Financial Profiles"] -FormLabel "BIA Financial Profiles" -Records $profileRecords

# --- BIA Sensors and Mappings (only sensors mapped to a business service) ---
$allSensors = (Get-ApiData "/api/v1/sensors").items
$sensorById = @{}
foreach ($s in $allSensors) { $sensorById[$s.id] = $s }

$mappingRecords = @()
foreach ($svc in $services) {
    $mappings = (Get-ApiData "/api/v1/services/$($svc.id)/mappings").items
    foreach ($m in $mappings) {
        $sensor = $sensorById[$m.sensor_id]
        if (-not $sensor) { continue }
        $mappingRecords += @{
            "PRTG_Sensor_ID" = $sensor.prtg_sensor_id
            "Device_Name" = $sensor.device_name
            "Sensor_Name" = $sensor.sensor_name
            "Service_Name" = $svc.name
            "Dependency_Weight" = $m.dependency_weight
            "Last_Known_State" = $sensor.last_known_state
        }
    }
}
Sync-JetForm -FormId $forms["BIA Sensors and Mappings"] -FormLabel "BIA Sensors and Mappings" -Records $mappingRecords

# --- BIA Incidents and Impacts (most recent 50) ---
$incidents = (Get-ApiData "/api/v1/incidents?limit=50").items
$incidentRecords = $incidents | ForEach-Object {
    $inc = $_
    $detail = $null
    try { $detail = Get-ApiData "/api/v1/incidents/$($inc.id)" } catch {}
    $breakdown = if ($detail -and $detail.impact) { $detail.impact.breakdown } else { $null }
    @{
        "Incident_Code" = $inc.id
        "Service_Name" = $inc.service.name
        "Device_Name" = $inc.sensor.device
        "Sensor_Name" = $inc.sensor.name
        "Severity" = $inc.severity
        "Status" = $inc.status
        "Started_At" = $inc.started_at
        "Duration_Seconds" = $inc.duration_seconds
        "Total_Impact" = $inc.total_impact
        "Direct_Loss" = if ($breakdown) { $breakdown.revenue_loss } else { 0 }
        "Operational_Loss" = if ($breakdown) { $breakdown.operational_cost } else { 0 }
        "Penalty_Loss" = if ($breakdown) { $breakdown.penalty_exposure } else { 0 }
        "Recovery_Loss" = if ($breakdown) { $breakdown.recovery_cost } else { 0 }
    }
}
Sync-JetForm -FormId $forms["BIA Incidents and Impacts"] -FormLabel "BIA Incidents and Impacts" -Records $incidentRecords

# --- BIA Knowledge Base ---
$kbEntries = (Get-ApiData "/api/v1/knowledge-base").items
$kbRecords = $kbEntries | ForEach-Object {
    @{
        "Title" = "$($_.service_category.ToUpper()): $($_.device_pattern) $($_.sensor_pattern)".Trim()
        "Device_Pattern" = $_.device_pattern
        "Sensor_Pattern" = $_.sensor_pattern
        "Service_Category" = $_.service_category
        "Priority" = $_.priority
        "Hourly_Loss_Estimate" = $_.hourly_loss_estimate
        "SLA_Penalty_Per_Hour" = $_.sla_penalty_per_hour
        "Affected_Users_Estimate" = $_.affected_users_estimate
        "Recovery_Time_Estimate_Minutes" = $_.recovery_time_estimate_minutes
        "Affected_Processes" = ($_.affected_processes -join ", ")
        "Description" = $_.description
        "Recovery_Procedure" = $_.recovery_procedure
    }
}
Sync-JetForm -FormId $forms["BIA Knowledge Base"] -FormLabel "BIA Knowledge Base" -Records $kbRecords

Write-Host "`n==========================================================" -ForegroundColor Cyan
Write-Host "  SYNC COMPLETE" -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Cyan
