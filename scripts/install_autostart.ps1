# ==============================================================================
# Installs Windows Scheduled Tasks so the whole BIA Platform starts itself
# automatically at logon, and JET data stays synced automatically - no manual
# terminal commands needed ever again.
#
# Registers two tasks (both under the current user, no admin rights needed):
#   "BIA Platform - Autostart"  -> runs run_all_hidden.ps1 at every logon
#   "BIA Platform - JET Sync"   -> runs sync_live_to_jetdata.ps1 every N minutes
#
# Re-run any time to update the tasks (e.g. after changing SyncIntervalMinutes).
# Remove everything with uninstall_autostart.ps1.
# ==============================================================================
param([int]$SyncIntervalMinutes = 5)

$ErrorActionPreference = "Stop"
$rootDir = Split-Path $PSScriptRoot -Parent
$psExe = (Get-Command powershell.exe).Source

$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable `
    -ExecutionTimeLimit ([TimeSpan]::Zero)  # no timeout - these are long-running/background

# ── 1. Autostart everything at logon ─────────────────────────────────────────
$startupAction = New-ScheduledTaskAction -Execute $psExe `
    -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$rootDir\scripts\run_all_hidden.ps1`""
$startupTrigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "BIA Platform - Autostart" `
    -Action $startupAction -Trigger $startupTrigger -Settings $settings -Force | Out-Null
Write-Host "[+] Registered 'BIA Platform - Autostart' (runs at every logon)" -ForegroundColor Green

# ── 2. Periodic JET data sync ────────────────────────────────────────────────
$syncAction = New-ScheduledTaskAction -Execute $psExe `
    -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$rootDir\scripts\sync_live_to_jetdata.ps1`""
$syncTrigger = New-ScheduledTaskTrigger -Once -At (Get-Date) `
    -RepetitionInterval (New-TimeSpan -Minutes $SyncIntervalMinutes) `
    -RepetitionDuration ([TimeSpan]::MaxValue)
Register-ScheduledTask -TaskName "BIA Platform - JET Sync" `
    -Action $syncAction -Trigger $syncTrigger -Settings $settings -Force | Out-Null
Write-Host "[+] Registered 'BIA Platform - JET Sync' (every $SyncIntervalMinutes minutes)" -ForegroundColor Green

Write-Host "`nStarting services right now (no need to log off/on)..." -ForegroundColor Cyan
Start-ScheduledTask -TaskName "BIA Platform - Autostart"
Start-Sleep -Seconds 2
Start-ScheduledTask -TaskName "BIA Platform - JET Sync"

Write-Host "`nDone. From now on, everything starts automatically when you log into Windows." -ForegroundColor Green
Write-Host "View/manage these tasks in Task Scheduler under the root folder, or run:" -ForegroundColor Gray
Write-Host "  Get-ScheduledTask -TaskName 'BIA Platform*'" -ForegroundColor Gray
Write-Host "To remove this automation later: scripts\uninstall_autostart.ps1" -ForegroundColor Gray
