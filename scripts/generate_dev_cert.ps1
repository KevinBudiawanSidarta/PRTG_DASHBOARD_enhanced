# ==============================================================================
# Generates a self-signed HTTPS certificate for the local dashboard.
#
# Needed so the JET custom UI (which runs on https://indo1.jetdata.ai) can
# embed the local dashboard in an iframe without the browser blocking it as
# mixed content — that requires the dashboard itself to be served over https.
#
# Uses openssl directly instead of Next.js's built-in `--experimental-https`
# (which uses mkcert and tries to install a local CA, prompting for admin
# elevation). This script needs no elevation — the browser will still show a
# one-time "not secure" warning for the self-signed cert, which is expected;
# click through it once per browser profile.
# ==============================================================================
param(
    [string]$OutDir = (Join-Path (Split-Path $PSScriptRoot -Parent) ".certs"),
    [string]$IPAddress = ""
)

if (-not (Get-Command openssl -ErrorAction SilentlyContinue)) {
    Write-Error "openssl not found on PATH. It ships with Git for Windows (Git Bash) - install that, or run 'npm run dev' (http only, no JET iframe embedding) instead."
    exit 1
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$keyPath = Join-Path $OutDir "localhost-key.pem"
$certPath = Join-Path $OutDir "localhost.pem"

if ((Test-Path $keyPath) -and (Test-Path $certPath)) {
    Write-Host "Certificate already exists at $OutDir - delete both files first to regenerate (needed if you're adding -IPAddress to an existing cert)." -ForegroundColor Yellow
    exit 0
}

$san = "DNS:localhost,IP:127.0.0.1"
if ($IPAddress) { $san += ",IP:$IPAddress" }

$env:MSYS_NO_PATHCONV = "1"
& openssl req -x509 -newkey rsa:2048 -keyout $keyPath -out $certPath -days 3650 -nodes `
    -subj "/CN=localhost" -addext "subjectAltName=$san"

if ($LASTEXITCODE -ne 0) {
    Write-Error "openssl failed to generate the certificate."
    exit 1
}

Write-Host "`nCertificate generated at $OutDir (covers: $san)" -ForegroundColor Green
Write-Host "Now run: cd apps\web && npm run dev:https" -ForegroundColor Cyan
$testHost = if ($IPAddress) { $IPAddress } else { "localhost" }
Write-Host "Then open https://${testHost}:3000 once in your browser and accept the self-signed cert warning." -ForegroundColor Cyan
