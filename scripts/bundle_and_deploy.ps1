# ==============================================================================
# Copy the custom_ui iframe wrapper to dist/ and deploy it to JETData.AI.
#
# apps/custom-ui/index.html is a single self-contained file (a thin iframe
# wrapper embedding the live dashboard - see the JETData custom UI section in
# README.md) so there is no CSS/JS to inline anymore; this is just a copy +
# push step.
# ==============================================================================
param(
    [string]$JetHost = "https://indo1.jetdata.ai/JET",
    [string]$Project = "DCMG",
    [string]$Username = "kevin",
    [string]$Password = $env:JET_PASSWORD,
    [string]$FormId = "140"
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

$rootDir = Split-Path $PSScriptRoot -Parent
$customUiDir = Join-Path $rootDir "apps\custom-ui"
$htmlPath = Join-Path $customUiDir "index.html"
$distDir = Join-Path $rootDir "dist"
New-Item -ItemType Directory -Force -Path $distDir | Out-Null
$distBundlePath = Join-Path $distDir "bia_custom_ui.html"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  PREPARING CUSTOM UI" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

if (-not (Test-Path $htmlPath)) {
    Write-Error "Source file missing: $htmlPath"
    exit 1
}

Copy-Item -Path $htmlPath -Destination $distBundlePath -Force
$bundleSize = (Get-Item $distBundlePath).Length

Write-Host "  -> Copied $htmlPath to $distBundlePath ($bundleSize bytes)" -ForegroundColor Green

Write-Host "`n==========================================================" -ForegroundColor Cyan
Write-Host "  DEPLOYING BUNDLE TO JETDATA ($Project, Form: $FormId)" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# Authenticate
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

# Push payload
Write-Host "  [*] Uploading custom_UI to form $FormId..." -ForegroundColor Yellow
$uploadJson = curl.exe -s -X POST "$JetHost/api_v3/api.php" `
  -F "method=updateCustomForm" `
  -F "project=$Project" `
  -F "session_key=$sessionKey" `
  -F "id_form=$FormId" `
  -F "custom_UI=<$distBundlePath"

$uploadRes = $uploadJson | ConvertFrom-Json
if ($uploadRes.status -eq "success") {
    Write-Host "  [+] Form $FormId updated successfully in JETData!" -ForegroundColor Green
} else {
    Write-Host "  [-] Upload response: $uploadJson" -ForegroundColor Red
}
