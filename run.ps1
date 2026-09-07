# ============================================================
# run.ps1 — Load .env dan jalankan semua BIA Platform services
# Usage: .\run.ps1
# ============================================================

# 1. Load .env
Write-Host "Loading .env..." -ForegroundColor Cyan
Get-Content .env | Where-Object { $_ -notmatch '^\s*#' -and $_ -match '=' } | ForEach-Object {
    $name, $value = $_ -split '=', 2
    $name = $name.Trim()
    $value = $value.Trim()
    if ($name -ne '') {
        [System.Environment]::SetEnvironmentVariable($name, $value, 'Process')
        Write-Host "  SET $name" -ForegroundColor DarkGray
    }
}

Write-Host ""
Write-Host "Starting BIA Platform services..." -ForegroundColor Green
Write-Host "-----------------------------------"

$envBlock = (Get-Content .env | Where-Object { $_ -notmatch '^\s*#' -and $_ -match '=' } | ForEach-Object {
    $n, $v = $_ -split '=', 2
    "`$env:$($n.Trim()) = `"$($v.Trim())`""
}) -join "`n"

$services = @(
    @{ Name = "API";       Cmd = "go run ./services/api/cmd" },
    @{ Name = "Collector"; Cmd = "go run ./services/collector/cmd" },
    @{ Name = "Worker";    Cmd = "go run ./services/worker/cmd" },
    @{ Name = "Scheduler"; Cmd = "go run ./services/scheduler/cmd" }
)

foreach ($svc in $services) {
    $script = "Set-Location '$PWD'`n$envBlock`n$($svc.Cmd)"
    $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($script))
    Start-Process powershell -ArgumentList "-NoExit", "-EncodedCommand", $encoded -WindowStyle Normal
    Write-Host "  Started: $($svc.Name)" -ForegroundColor Yellow
    Start-Sleep -Milliseconds 500
}

Write-Host ""
Write-Host "-----------------------------------"
Write-Host "Semua service sudah berjalan di window terpisah." -ForegroundColor Green
Write-Host "Frontend: cd apps/web && npm install && npm run dev" -ForegroundColor Cyan
Write-Host "Dashboard: http://localhost:3000" -ForegroundColor Magenta
