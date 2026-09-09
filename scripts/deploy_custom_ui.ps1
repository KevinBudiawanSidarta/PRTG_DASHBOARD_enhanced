# ==============================================================================
# Deploy BIA Custom UI to JETData.AI (DCMG)
# ==============================================================================
param(
    [string]$JetHost = "https://indo1.jetdata.ai/JET",
    [string]$Project = "DCMG",
    [string]$Username = "kevin",
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
Write-Host "  DEPLOYING BIA CUSTOM UI TO JETDATA ($Project)" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Authenticate
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
Write-Host "  -> Auth SUCCESS! (Session: $sessionKey)" -ForegroundColor Green

# 2. Check or Create Custom Form
$formsJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" `
  -F "method=getFormsList" `
  -F "project=$Project" `
  -F "session_key=$sessionKey"

$formsRes = $formsJson | ConvertFrom-Json
$formName = "BIA Platform Dashboard"
$customForm = $formsRes.forms | Where-Object { $_.form_name -eq $formName }

$formId = $null
if ($customForm) {
    $formId = $customForm.id_form
    Write-Host "  [+] Existing Custom Form '$formName' found (ID: $formId)" -ForegroundColor DarkGreen
    
    # Ensure form_type is custom
    if ($customForm.form_type -ne "custom") {
        Write-Host "  [*] Updating form_type to 'custom'..." -ForegroundColor Yellow
        curl.exe -s -X POST "$JetHost/api_v3/api.php" `
          -F "method=updateForm" `
          -F "project=$Project" `
          -F "session_key=$sessionKey" `
          -F "id_form=$formId" `
          -F "form_type=custom" | Out-Null
    }
} else {
    Write-Host "  [*] Creating new custom form '$formName'..." -ForegroundColor Yellow
    $createJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" `
      -F "method=createForm" `
      -F "project=$Project" `
      -F "session_key=$sessionKey" `
      -F "form_name=$formName" `
      -F "form_type=custom" `
      -F "group_name=BIA Platform"
    
    $createRes = $createJson | ConvertFrom-Json
    if ($createRes.status -eq "success") {
        $formId = $createRes.id_form
        Write-Host "  [+] Custom Form '$formName' created (ID: $formId)" -ForegroundColor Green
    } else {
        Write-Error "Failed to create custom form: $($createRes.message)"
        exit 1
    }
}

# 3. Push HTML payload via updateCustomForm
$htmlFile = Join-Path (Split-Path $PSScriptRoot -Parent) "dist\bia_custom_ui.html"
if (-not (Test-Path $htmlFile)) {
    Write-Error "HTML file not found: $htmlFile"
    exit 1
}

$fileSize = (Get-Item $htmlFile).Length
Write-Host "  [*] Pushing custom_UI payload ($fileSize bytes)..." -ForegroundColor Yellow

$uploadJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" `
  -F "method=updateCustomForm" `
  -F "project=$Project" `
  -F "session_key=$sessionKey" `
  -F "id_form=$formId" `
  -F "custom_UI=<$htmlFile"

Write-Host "  Upload response: $uploadJson" -ForegroundColor Gray
$uploadRes = $uploadJson | ConvertFrom-Json

if ($uploadRes.status -ne "success") {
    # If updateCustomForm fails or form is new, try createCustomForm
    Write-Host "  [*] Trying createCustomForm..." -ForegroundColor Yellow
    $uploadJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" `
      -F "method=createCustomForm" `
      -F "project=$Project" `
      -F "session_key=$sessionKey" `
      -F "id_form=$formId" `
      -F "custom_UI=<$htmlFile"
    Write-Host "  Upload response: $uploadJson" -ForegroundColor Gray
    $uploadRes = $uploadJson | ConvertFrom-Json
}

# 4. Verify deployment with getCustomForm
Write-Host "`n[4/4] Verifying Custom Form deployment in JETData..." -ForegroundColor Yellow
$verifyJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" `
  -F "method=getCustomForm" `
  -F "project=$Project" `
  -F "session_key=$sessionKey" `
  -F "id_form=$formId"

$verifyRes = $verifyJson | ConvertFrom-Json
if ($verifyRes.status -eq "success" -and $verifyRes.data.custom_UI) {
    $storedLen = $verifyRes.data.custom_UI.Length
    Write-Host "  [+] DEPLOYMENT VERIFIED! Stored custom_UI: $storedLen chars" -ForegroundColor Green
    Write-Host "`n==========================================================" -ForegroundColor Cyan
    Write-Host "  BIA PLATFORM DEPLOYED TO JETDATA!" -ForegroundColor Green
    Write-Host "  Project:    $Project" -ForegroundColor White
    Write-Host "  Form Name:  $formName" -ForegroundColor White
    Write-Host "  Form ID:    $formId" -ForegroundColor White
    Write-Host "  URL:        $JetHost" -ForegroundColor Yellow
    Write-Host "==========================================================" -ForegroundColor Cyan
} else {
    Write-Host "  [-] Warning: Could not verify custom form: $($verifyRes.message)" -ForegroundColor Red
}
