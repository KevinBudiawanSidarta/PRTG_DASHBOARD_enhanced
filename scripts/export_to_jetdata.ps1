# ==============================================================================
# BIA Platform -> JETData.AI (DCMG) Exporter & Custom UI Deployer
# ==============================================================================
param(
    [string]$JetHost = $(if ($env:JET_HOST) { $env:JET_HOST } else { "https://indo1.jetdata.ai/JET" }),
    [string]$Project = $(if ($env:JET_PROJECT) { $env:JET_PROJECT } else { "DCMG" }),
    [string]$Username = $(if ($env:JET_USERNAME) { $env:JET_USERNAME } else { "kevin" }),
    [string]$Password = $env:JET_PASSWORD
)

$ErrorActionPreference = "Stop"

if (-not $Password) {
    . (Join-Path $PSScriptRoot "_env.ps1")
    $Password = $env:JET_PASSWORD
}
if (-not $Password) {
    Write-Error "JET_PASSWORD not set. Add JET_PASSWORD=... to .env, export `$env:JET_PASSWORD, or pass -Password."
    exit 1
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  BIA PLATFORM -> JETDATA EXPORTER ($Project)" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Authenticate with JETData API V3
Write-Host "`n[1/5] Authenticating to $JetHost (Project: $Project, User: $Username)..." -ForegroundColor Yellow
$authJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" `
  -F "method=authenticate" `
  -F "project=$Project" `
  -F "username=$Username" `
  -F "password=$Password"

$authRes = $authJson | ConvertFrom-Json
if ($authRes.status -ne "success" -or -not $authRes.session_key) {
    Write-Error "Authentication failed: $($authRes.message)"
    exit 1
}

$sessionKey = $authRes.session_key
Write-Host "  -> Auth SUCCESS! Logged in as $($authRes.user.first_name) $($authRes.user.last_name) ($($authRes.user.user_type))" -ForegroundColor Green
Write-Host "  -> Session Key: $sessionKey" -ForegroundColor Gray

# Helper to call JET API V3
function Invoke-JetApi {
    param(
        [string]$Method,
        [hashtable]$Params = @{}
    )
    
    $args = @(
        "-s", "-X", "POST", "$JetHost/api_v3/api.php",
        "-F", "method=$Method",
        "-F", "project=$Project",
        "-F", "session_key=$sessionKey"
    )
    
    foreach ($k in $Params.Keys) {
        $v = $Params[$k]
        if ($null -ne $v) {
            $args += "-F"
            $args += "$k=$v"
        }
    }
    
    $raw = & curl.exe @args
    return $raw | ConvertFrom-Json
}

# 2. List Existing Forms
Write-Host "`n[2/5] Checking existing forms in project $Project..." -ForegroundColor Yellow
$formsListRes = Invoke-JetApi -Method "getFormsList"
$existingForms = @{}
if ($formsListRes.status -eq "success" -and $formsListRes.forms) {
    foreach ($f in $formsListRes.forms) {
        $existingForms[$f.form_name] = $f
    }
}

# Helper to create form if not exists
function Ensure-Form {
    param(
        [string]$FormName,
        [string]$FormType = "form",
        [string]$GroupName = "BIA Platform"
    )
    
    if ($existingForms.ContainsKey($FormName)) {
        $id = $existingForms[$FormName].id_form
        Write-Host "  [+] Form '$FormName' already exists (ID: $id)" -ForegroundColor DarkGreen
        return $id
    }
    
    Write-Host "  [*] Creating form '$FormName' ($FormType)..." -ForegroundColor Yellow
    $createRes = Invoke-JetApi -Method "createForm" -Params @{
        "form_name" = $FormName
        "form_type" = $FormType
        "group_name" = $GroupName
    }
    
    if ($createRes.status -eq "success") {
        $id = $createRes.id_form
        Write-Host "  [+] Form '$FormName' created successfully (ID: $id)" -ForegroundColor Green
        return $id
    } else {
        Write-Error "Failed to create form '$FormName': $($createRes.message)"
    }
}

# Helper to ensure fields exist
function Ensure-Field {
    param(
        [string]$FormId,
        [string]$FieldName,
        [string]$FieldLabel,
        [string]$FieldType = "text",
        [string]$DropdownData = ""
    )
    
    $fieldsRes = Invoke-JetApi -Method "getFieldMappings" -Params @{ "id_form" = $FormId }
    if ($fieldsRes.status -eq "success" -and $fieldsRes.fields) {
        $found = $fieldsRes.fields | Where-Object { $_.field_name -eq $FieldName }
        if ($found) {
            return
        }
    }
    
    $params = @{
        "id_form" = $FormId
        "field_name" = $FieldName
        "field_label" = $FieldLabel
        "field_type" = $FieldType
    }
    if ($DropdownData) {
        $params["dropdowndata"] = $DropdownData
    }
    
    $res = Invoke-JetApi -Method "createFieldMapping" -Params $params
    if ($res.status -eq "success") {
        Write-Host "    -> Field '$FieldName' ($FieldType) added to form $FormId" -ForegroundColor Gray
    }
}

# 3. Create all BIA Data Forms
Write-Host "`n[3/5] Setting up BIA Data Forms & Schema..." -ForegroundColor Yellow

# Form 1: BIA Services
$formServicesId = Ensure-Form -FormName "BIA Services"
Ensure-Field -FormId $formServicesId -FieldName "Service_Name" -FieldLabel "Service Name" -FieldType "text"
Ensure-Field -FormId $formServicesId -FieldName "Criticality" -FieldLabel "Criticality Tier" -FieldType "dropdownprepopulated" -DropdownData "P1,P2,P3,P4"
Ensure-Field -FormId $formServicesId -FieldName "Owner_Name" -FieldLabel "Owner Team" -FieldType "text"
Ensure-Field -FormId $formServicesId -FieldName "Affected_Users" -FieldLabel "Affected Users" -FieldType "numeric"
Ensure-Field -FormId $formServicesId -FieldName "SLA_Target" -FieldLabel "SLA Target (%)" -FieldType "numeric"
Ensure-Field -FormId $formServicesId -FieldName "RTO_Minutes" -FieldLabel "RTO (Minutes)" -FieldType "numeric"
Ensure-Field -FormId $formServicesId -FieldName "RPO_Minutes" -FieldLabel "RPO (Minutes)" -FieldType "numeric"
Ensure-Field -FormId $formServicesId -FieldName "Business_Value_Per_Hour" -FieldLabel "Business Value / Hour (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formServicesId -FieldName "Description" -FieldLabel "Description" -FieldType "textarea"

# Form 2: BIA Financial Profiles
$formFinancialId = Ensure-Form -FormName "BIA Financial Profiles"
Ensure-Field -FormId $formFinancialId -FieldName "Service_Name" -FieldLabel "Service Name" -FieldType "text"
Ensure-Field -FormId $formFinancialId -FieldName "Hourly_Revenue" -FieldLabel "Hourly Revenue (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Transactions_Per_Hour" -FieldLabel "Transactions / Hour" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Avg_Transaction_Value" -FieldLabel "Avg Transaction Value (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Service_Dependency" -FieldLabel "Service Dependency (0-1)" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Loss_Probability" -FieldLabel "Loss Probability (0-1)" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Operational_Cost_Per_Hour" -FieldLabel "Operational Cost / Hour (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Penalty_Fixed" -FieldLabel "SLA Penalty Fixed (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Recovery_Fixed" -FieldLabel "Recovery Cost Fixed (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Affected_Employees" -FieldLabel "Affected Employees" -FieldType "numeric"
Ensure-Field -FormId $formFinancialId -FieldName "Avg_Employee_Cost_Per_Hour" -FieldLabel "Avg Employee Cost / Hour" -FieldType "numeric"

# Form 3: BIA Sensors & Mappings
$formSensorsId = Ensure-Form -FormName "BIA Sensors and Mappings"
Ensure-Field -FormId $formSensorsId -FieldName "PRTG_Sensor_ID" -FieldLabel "PRTG Sensor ID" -FieldType "text"
Ensure-Field -FormId $formSensorsId -FieldName "Device_Name" -FieldLabel "Device Name" -FieldType "text"
Ensure-Field -FormId $formSensorsId -FieldName "Sensor_Name" -FieldLabel "Sensor Name" -FieldType "text"
Ensure-Field -FormId $formSensorsId -FieldName "Service_Name" -FieldLabel "Mapped Service" -FieldType "text"
Ensure-Field -FormId $formSensorsId -FieldName "Dependency_Weight" -FieldLabel "Dependency Weight (0-1)" -FieldType "numeric"
Ensure-Field -FormId $formSensorsId -FieldName "Last_Known_State" -FieldLabel "Status" -FieldType "dropdownprepopulated" -DropdownData "up,down,warning,paused,unknown"
Ensure-Field -FormId $formSensorsId -FieldName "Uptime_Pct" -FieldLabel "Uptime (%)" -FieldType "numeric"
Ensure-Field -FormId $formSensorsId -FieldName "Last_Check" -FieldLabel "Last Checked" -FieldType "text"

# Form 4: BIA Incidents & Impacts
$formIncidentsId = Ensure-Form -FormName "BIA Incidents and Impacts"
Ensure-Field -FormId $formIncidentsId -FieldName "Incident_Code" -FieldLabel "Incident ID" -FieldType "text"
Ensure-Field -FormId $formIncidentsId -FieldName "Service_Name" -FieldLabel "Impacted Service" -FieldType "text"
Ensure-Field -FormId $formIncidentsId -FieldName "Device_Name" -FieldLabel "Device" -FieldType "text"
Ensure-Field -FormId $formIncidentsId -FieldName "Sensor_Name" -FieldLabel "Sensor" -FieldType "text"
Ensure-Field -FormId $formIncidentsId -FieldName "Severity" -FieldLabel "Severity" -FieldType "dropdownprepopulated" -DropdownData "CRITICAL,HIGH,MEDIUM,LOW"
Ensure-Field -FormId $formIncidentsId -FieldName "Status" -FieldLabel "Status" -FieldType "dropdownprepopulated" -DropdownData "OPEN,ACKNOWLEDGED,RESOLVED,CLOSED"
Ensure-Field -FormId $formIncidentsId -FieldName "Started_At" -FieldLabel "Started At" -FieldType "text"
Ensure-Field -FormId $formIncidentsId -FieldName "Duration_Seconds" -FieldLabel "Duration (Sec)" -FieldType "numeric"
Ensure-Field -FormId $formIncidentsId -FieldName "Total_Impact" -FieldLabel "Total Financial Impact (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formIncidentsId -FieldName "Direct_Loss" -FieldLabel "Direct Revenue Loss (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formIncidentsId -FieldName "Operational_Loss" -FieldLabel "Operational Loss (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formIncidentsId -FieldName "Penalty_Loss" -FieldLabel "SLA Penalty Loss (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formIncidentsId -FieldName "Recovery_Loss" -FieldLabel "Recovery Cost (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formIncidentsId -FieldName "AI_Recommendation" -FieldLabel "AI Impact Recommendation" -FieldType "textarea"

# Form 5: BIA Knowledge Base
$formKbId = Ensure-Form -FormName "BIA Knowledge Base"
Ensure-Field -FormId $formKbId -FieldName "Title" -FieldLabel "SOP Title" -FieldType "text"
Ensure-Field -FormId $formKbId -FieldName "Device_Pattern" -FieldLabel "Device Pattern" -FieldType "text"
Ensure-Field -FormId $formKbId -FieldName "Sensor_Pattern" -FieldLabel "Sensor Pattern" -FieldType "text"
Ensure-Field -FormId $formKbId -FieldName "Service_Category" -FieldLabel "Category" -FieldType "dropdownprepopulated" -DropdownData "server,network,database,storage,application"
Ensure-Field -FormId $formKbId -FieldName "Priority" -FieldLabel "Priority" -FieldType "dropdownprepopulated" -DropdownData "P1,P2,P3"
Ensure-Field -FormId $formKbId -FieldName "Hourly_Loss_Estimate" -FieldLabel "Hourly Loss Estimate (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formKbId -FieldName "SLA_Penalty_Per_Hour" -FieldLabel "SLA Penalty / Hour (IDR)" -FieldType "numeric"
Ensure-Field -FormId $formKbId -FieldName "Affected_Users_Estimate" -FieldLabel "Affected Users Estimate" -FieldType "numeric"
Ensure-Field -FormId $formKbId -FieldName "Recovery_Time_Estimate_Minutes" -FieldLabel "Recovery Time Estimate (Min)" -FieldType "numeric"
Ensure-Field -FormId $formKbId -FieldName "Affected_Processes" -FieldLabel "Affected Processes" -FieldType "text"
Ensure-Field -FormId $formKbId -FieldName "Description" -FieldLabel "Problem Description" -FieldType "textarea"
Ensure-Field -FormId $formKbId -FieldName "Recovery_Procedure" -FieldLabel "Recovery SOP Procedure" -FieldType "textarea"

Write-Host "`n  -> All 5 BIA Data Forms verified and configured!" -ForegroundColor Green

# 4. Seed Data into JETData
Write-Host "`n[4/5] Seeding data into JETData tables..." -ForegroundColor Yellow

# Helper to check record count in form
function Get-RecordCount {
    param([string]$FormId)
    $res = Invoke-JetApi -Method "getRecords" -Params @{ "id_form" = $FormId; "limit" = 1 }
    if ($res.status -eq "success" -and $null -ne $res.total) {
        return [int]$res.total
    }
    return 0
}

# Seed Services
$srvCount = Get-RecordCount -FormId $formServicesId
if ($srvCount -eq 0) {
    Write-Host "  [*] Seeding BIA Services..." -ForegroundColor Yellow
    $servicesData = @(
        @{
            "Service_Name" = "Customer Transaction API"
            "Criticality" = "P1"
            "Owner_Name" = "Payment & Core Banking"
            "Affected_Users" = 3200
            "SLA_Target" = 99.99
            "RTO_Minutes" = 10
            "RPO_Minutes" = 5
            "Business_Value_Per_Hour" = 150000000
            "Description" = "Transaction routing, payment gateways, and checkout API processing"
        },
        @{
            "Service_Name" = "ERP Production"
            "Criticality" = "P1"
            "Owner_Name" = "IT Operations & Enterprise Apps"
            "Affected_Users" = 450
            "SLA_Target" = 99.90
            "RTO_Minutes" = 30
            "RPO_Minutes" = 15
            "Business_Value_Per_Hour" = 50000000
            "Description" = "Critical ERP backend powering inventory, financial billing, and branch invoicing"
        },
        @{
            "Service_Name" = "Customer Portal"
            "Criticality" = "P1"
            "Owner_Name" = "Digital Channels"
            "Affected_Users" = 1800
            "SLA_Target" = 99.95
            "RTO_Minutes" = 15
            "RPO_Minutes" = 5
            "Business_Value_Per_Hour" = 90000000
            "Description" = "External web and mobile customer facing portal for account access and ordering"
        },
        @{
            "Service_Name" = "Warehouse System"
            "Criticality" = "P2"
            "Owner_Name" = "Supply Chain & Logistics"
            "Affected_Users" = 120
            "SLA_Target" = 99.50
            "RTO_Minutes" = 60
            "RPO_Minutes" = 30
            "Business_Value_Per_Hour" = 20000000
            "Description" = "Barcode scanning, order fulfillment, and logistics dispatch in warehouse"
        },
        @{
            "Service_Name" = "Office Network & Corporate Services"
            "Criticality" = "P3"
            "Owner_Name" = "IT Infrastructure"
            "Affected_Users" = 280
            "SLA_Target" = 99.00
            "RTO_Minutes" = 120
            "RPO_Minutes" = 60
            "Business_Value_Per_Hour" = 8000000
            "Description" = "Internal office Wi-Fi, LAN switches, and corporate intranet"
        }
    )
    foreach ($item in $servicesData) {
        $p = @{ "id_form" = $formServicesId }
        foreach ($k in $item.Keys) { $p[$k] = $item[$k] }
        Invoke-JetApi -Method "createRecord" -Params $p | Out-Null
    }
    Write-Host "    -> 5 Services seeded" -ForegroundColor Green
} else {
    Write-Host "  [+] BIA Services already has $srvCount records" -ForegroundColor DarkGreen
}

# Seed Financial Profiles
$finCount = Get-RecordCount -FormId $formFinancialId
if ($finCount -eq 0) {
    Write-Host "  [*] Seeding Financial Profiles..." -ForegroundColor Yellow
    $finData = @(
        @{
            "Service_Name" = "Customer Transaction API"
            "Hourly_Revenue" = 150000000
            "Transactions_Per_Hour" = 1500
            "Avg_Transaction_Value" = 100000
            "Service_Dependency" = 0.98
            "Loss_Probability" = 0.90
            "Operational_Cost_Per_Hour" = 8000000
            "Penalty_Fixed" = 50000000
            "Recovery_Fixed" = 20000000
            "Affected_Employees" = 40
            "Avg_Employee_Cost_Per_Hour" = 120000
        },
        @{
            "Service_Name" = "ERP Production"
            "Hourly_Revenue" = 50000000
            "Transactions_Per_Hour" = 200
            "Avg_Transaction_Value" = 250000
            "Service_Dependency" = 0.90
            "Loss_Probability" = 0.80
            "Operational_Cost_Per_Hour" = 3000000
            "Penalty_Fixed" = 10000000
            "Recovery_Fixed" = 5000000
            "Affected_Employees" = 300
            "Avg_Employee_Cost_Per_Hour" = 75000
        },
        @{
            "Service_Name" = "Customer Portal"
            "Hourly_Revenue" = 90000000
            "Transactions_Per_Hour" = 800
            "Avg_Transaction_Value" = 112500
            "Service_Dependency" = 0.95
            "Loss_Probability" = 0.85
            "Operational_Cost_Per_Hour" = 5000000
            "Penalty_Fixed" = 25000000
            "Recovery_Fixed" = 10000000
            "Affected_Employees" = 50
            "Avg_Employee_Cost_Per_Hour" = 100000
        },
        @{
            "Service_Name" = "Warehouse System"
            "Hourly_Revenue" = 20000000
            "Transactions_Per_Hour" = 150
            "Avg_Transaction_Value" = 133333
            "Service_Dependency" = 0.80
            "Loss_Probability" = 0.70
            "Operational_Cost_Per_Hour" = 2000000
            "Penalty_Fixed" = 5000000
            "Recovery_Fixed" = 2000000
            "Affected_Employees" = 120
            "Avg_Employee_Cost_Per_Hour" = 50000
        },
        @{
            "Service_Name" = "Office Network & Corporate Services"
            "Hourly_Revenue" = 8000000
            "Transactions_Per_Hour" = 50
            "Avg_Transaction_Value" = 160000
            "Service_Dependency" = 0.60
            "Loss_Probability" = 0.50
            "Operational_Cost_Per_Hour" = 1000000
            "Penalty_Fixed" = 0
            "Recovery_Fixed" = 1000000
            "Affected_Employees" = 280
            "Avg_Employee_Cost_Per_Hour" = 60000
        }
    )
    foreach ($item in $finData) {
        $p = @{ "id_form" = $formFinancialId }
        foreach ($k in $item.Keys) { $p[$k] = $item[$k] }
        Invoke-JetApi -Method "createRecord" -Params $p | Out-Null
    }
    Write-Host "    -> 5 Financial Profiles seeded" -ForegroundColor Green
} else {
    Write-Host "  [+] BIA Financial Profiles already has $finCount records" -ForegroundColor DarkGreen
}

# Seed Knowledge Base
$kbCount = Get-RecordCount -FormId $formKbId
if ($kbCount -eq 0) {
    Write-Host "  [*] Seeding Knowledge Base from knowledge_base.json..." -ForegroundColor Yellow
    $kbFile = Join-Path (Split-Path $PSScriptRoot -Parent) "database\seeds\knowledge_base.json"
    if (Test-Path $kbFile) {
        $kbJson = Get-Content $kbFile -Raw | ConvertFrom-Json
        foreach ($k in $kbJson) {
            $procs = ""
            if ($k.affected_processes) { $procs = ($k.affected_processes -join ", ") }
            $p = @{
                "id_form" = $formKbId
                "Title" = "$($k.service_category.ToUpper()): $($k.device_pattern) $($k.sensor_pattern)".Trim()
                "Device_Pattern" = $k.device_pattern
                "Sensor_Pattern" = $k.sensor_pattern
                "Service_Category" = $k.service_category
                "Priority" = $k.priority
                "Hourly_Loss_Estimate" = $k.hourly_loss_estimate
                "SLA_Penalty_Per_Hour" = $k.sla_penalty_per_hour
                "Affected_Users_Estimate" = $k.affected_users_estimate
                "Recovery_Time_Estimate_Minutes" = $k.recovery_time_estimate_minutes
                "Affected_Processes" = $procs
                "Description" = $k.description
                "Recovery_Procedure" = $k.recovery_procedure
            }
            Invoke-JetApi -Method "createRecord" -Params $p | Out-Null
        }
        Write-Host "    -> $($kbJson.Count) Knowledge Base items seeded" -ForegroundColor Green
    }
} else {
    Write-Host "  [+] BIA Knowledge Base already has $kbCount records" -ForegroundColor DarkGreen
}

# Seed Sensors & Mappings
$sensorCount = Get-RecordCount -FormId $formSensorsId
if ($sensorCount -eq 0) {
    Write-Host "  [*] Seeding Sensors and Mappings..." -ForegroundColor Yellow
    $sensorsSeed = @(
        @{ "PRTG_Sensor_ID" = "2114"; "Device_Name" = "Switch Edge Core | 192.168.10.2"; "Sensor_Name" = "Ping v2"; "Service_Name" = "Customer Transaction API"; "Dependency_Weight" = 0.95; "Last_Known_State" = "up"; "Uptime_Pct" = 99.98; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "2150"; "Device_Name" = "Switch Edge Core | 192.168.10.2"; "Sensor_Name" = "HTTP v2"; "Service_Name" = "Customer Portal"; "Dependency_Weight" = 0.90; "Last_Known_State" = "up"; "Uptime_Pct" = 99.95; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "2151"; "Device_Name" = "Switch Edge Core | 192.168.10.2"; "Sensor_Name" = "HTTPS v2"; "Service_Name" = "Customer Portal"; "Dependency_Weight" = 0.95; "Last_Known_State" = "up"; "Uptime_Pct" = 99.92; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "2122"; "Device_Name" = "Switch Edge Core | 192.168.10.2"; "Sensor_Name" = "SNMP Uptime v2"; "Service_Name" = "ERP Production"; "Dependency_Weight" = 0.85; "Last_Known_State" = "up"; "Uptime_Pct" = 100.00; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "2137"; "Device_Name" = "Switch Edge Core | 192.168.10.2"; "Sensor_Name" = "SSL Certificate Sensor"; "Service_Name" = "Customer Transaction API"; "Dependency_Weight" = 0.80; "Last_Known_State" = "up"; "Uptime_Pct" = 100.00; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "1001"; "Device_Name" = "SRV-DNS"; "Sensor_Name" = "DNS Query Latency"; "Service_Name" = "ERP Production"; "Dependency_Weight" = 0.95; "Last_Known_State" = "down"; "Uptime_Pct" = 97.40; "Last_Check" = "2m ago" },
        @{ "PRTG_Sensor_ID" = "1002"; "Device_Name" = "Mikrotik 192.168.10.1"; "Sensor_Name" = "SNMP CPU Load"; "Service_Name" = "Office Network & Corporate Services"; "Dependency_Weight" = 0.80; "Last_Known_State" = "up"; "Uptime_Pct" = 99.85; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "1003"; "Device_Name" = "Router Indihome"; "Sensor_Name" = "WAN Gateway Ping"; "Service_Name" = "Customer Portal"; "Dependency_Weight" = 0.90; "Last_Known_State" = "up"; "Uptime_Pct" = 99.10; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "1004"; "Device_Name" = "Database Cluster 01"; "Sensor_Name" = "PostgreSQL Query Time"; "Service_Name" = "Customer Transaction API"; "Dependency_Weight" = 0.98; "Last_Known_State" = "up"; "Uptime_Pct" = 99.99; "Last_Check" = "Just now" },
        @{ "PRTG_Sensor_ID" = "1005"; "Device_Name" = "Warehouse Storage NAS"; "Sensor_Name" = "Disk I/O Latency"; "Service_Name" = "Warehouse System"; "Dependency_Weight" = 0.85; "Last_Known_State" = "up"; "Uptime_Pct" = 99.60; "Last_Check" = "Just now" }
    )
    foreach ($item in $sensorsSeed) {
        $p = @{ "id_form" = $formSensorsId }
        foreach ($k in $item.Keys) { $p[$k] = $item[$k] }
        Invoke-JetApi -Method "createRecord" -Params $p | Out-Null
    }
    Write-Host "    -> 10 Sensors and Mappings seeded" -ForegroundColor Green
} else {
    Write-Host "  [+] BIA Sensors and Mappings already has $sensorCount records" -ForegroundColor DarkGreen
}

# Seed Incidents
$incCount = Get-RecordCount -FormId $formIncidentsId
if ($incCount -eq 0) {
    Write-Host "  [*] Seeding Incidents and Impacts..." -ForegroundColor Yellow
    $incSeed = @(
        @{
            "Incident_Code" = "INC-2026-0891"
            "Service_Name" = "ERP Production"
            "Device_Name" = "SRV-DNS"
            "Sensor_Name" = "DNS Query Latency"
            "Severity" = "CRITICAL"
            "Status" = "OPEN"
            "Started_At" = (Get-Date).AddMinutes(-42).ToString("yyyy-MM-dd HH:mm:ss")
            "Duration_Seconds" = 2520
            "Total_Impact" = 53500000
            "Direct_Loss" = 35000000
            "Operational_Loss" = 3500000
            "Penalty_Loss" = 10000000
            "Recovery_Loss" = 5000000
            "AI_Recommendation" = "Restart systemd-resolved and failover DHCP DNS immediately to secondary provider to restore ERP branch connectivity."
        },
        @{
            "Incident_Code" = "INC-2026-0888"
            "Service_Name" = "Warehouse System"
            "Device_Name" = "Warehouse Storage NAS"
            "Sensor_Name" = "Disk I/O Latency"
            "Severity" = "MEDIUM"
            "Status" = "RESOLVED"
            "Started_At" = (Get-Date).AddHours(-6).ToString("yyyy-MM-dd HH:mm:ss")
            "Duration_Seconds" = 1200
            "Total_Impact" = 8666000
            "Direct_Loss" = 6666000
            "Operational_Loss" = 1000000
            "Penalty_Loss" = 0
            "Recovery_Loss" = 1000000
            "AI_Recommendation" = "Resolved: Background RAID scrubber priority reconfigured to off-peak hours."
        }
    )
    foreach ($item in $incSeed) {
        $p = @{ "id_form" = $formIncidentsId }
        foreach ($k in $item.Keys) { $p[$k] = $item[$k] }
        Invoke-JetApi -Method "createRecord" -Params $p | Out-Null
    }
    Write-Host "    -> 2 Incidents seeded" -ForegroundColor Green
} else {
    Write-Host "  [+] BIA Incidents already has $incCount records" -ForegroundColor DarkGreen
}

Write-Host "`nSummary of BIA Data Forms in JETData:" -ForegroundColor Cyan
Write-Host "  1. BIA Services:              ID $formServicesId" -ForegroundColor White
Write-Host "  2. BIA Financial Profiles:    ID $formFinancialId" -ForegroundColor White
Write-Host "  3. BIA Sensors and Mappings:  ID $formSensorsId" -ForegroundColor White
Write-Host "  4. BIA Incidents and Impacts: ID $formIncidentsId" -ForegroundColor White
Write-Host "  5. BIA Knowledge Base:        ID $formKbId" -ForegroundColor White

# Store the Form IDs in a json manifest for the custom UI builder
$manifest = @{
    "JetHost" = $JetHost
    "Project" = $Project
    "FormServices" = $formServicesId
    "FormFinancial" = $formFinancialId
    "FormSensors" = $formSensorsId
    "FormIncidents" = $formIncidentsId
    "FormKb" = $formKbId
}
$manifest | ConvertTo-Json | Set-Content -Path (Join-Path $PSScriptRoot "jet_manifest.json")
Write-Host "  -> Manifest written to jet_manifest.json" -ForegroundColor Green
