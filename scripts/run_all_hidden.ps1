# ==============================================================================
# Starts every BIA Platform process (API, collector, worker, scheduler, web
# dashboard over HTTPS) fully in the background - no visible terminal windows.
#
# Meant to be triggered automatically by a Scheduled Task at logon (see
# install_autostart.ps1), but can also be run manually. Safe to re-run: it
# frees the ports first, so it won't pile up duplicate processes.
#
# Logs go to logs/<name>.log / .err next to this script's parent folder.
# ==============================================================================
$ErrorActionPreference = "Continue"
$rootDir = Split-Path $PSScriptRoot -Parent
$logDir = Join-Path $rootDir "logs"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

# Load .env into this process so every child process inherits it.
. (Join-Path $PSScriptRoot "_env.ps1")
$envFile = Join-Path $rootDir ".env"
if (Test-Path $envFile) {
    Get-Content $envFile | Where-Object { $_ -notmatch '^\s*#' -and $_ -match '^[^=]+=' } | ForEach-Object {
        $n, $v = $_ -split '=', 2
        [System.Environment]::SetEnvironmentVariable($n.Trim(), $v.Trim(), 'Process')
    }
}

# Free a port if something is already listening on it (stale process from a
# previous run, crashed session, etc.) so this script is safely re-runnable.
function Clear-Port {
    param([int]$Port)
    Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue | ForEach-Object {
        Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue
    }
}
Clear-Port -Port 8080  # API
Clear-Port -Port 3000  # web dashboard
Start-Sleep -Milliseconds 500

function Start-Bg {
    param([string]$Name, [string]$WorkDir, [string]$Command)
    $out = Join-Path $logDir "$Name.log"
    $err = Join-Path $logDir "$Name.err.log"
    Start-Process -FilePath "powershell.exe" `
        -ArgumentList @("-NoProfile", "-WindowStyle", "Hidden", "-Command", $Command) `
        -WorkingDirectory $WorkDir `
        -WindowStyle Hidden `
        -RedirectStandardOutput $out `
        -RedirectStandardError $err
}

Start-Bg -Name "api"       -WorkDir $rootDir -Command "go run ./services/api/cmd"
Start-Bg -Name "collector" -WorkDir $rootDir -Command "go run ./services/collector/cmd"
Start-Bg -Name "worker"    -WorkDir $rootDir -Command "go run ./services/worker/cmd"
Start-Bg -Name "scheduler" -WorkDir $rootDir -Command "go run ./services/scheduler/cmd"

# Give the API a moment to bind its port before the web app's first fetch.
Start-Sleep -Seconds 3

# PORT in .env is for the Go API (8080) - Next.js's dev server also honors
# $env:PORT when no -p flag is given, so it must be overridden here or the
# web app tries to bind 8080 too and collides with the API.
Start-Bg -Name "web" -WorkDir (Join-Path $rootDir "apps\web") -Command "`$env:PORT='3000'; npm run dev:https"

# Log a timestamped marker so repeated runs are easy to tell apart in the logs.
"[$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] run_all_hidden.ps1 started all services" |
    Out-File -FilePath (Join-Path $logDir "run_all.log") -Append
