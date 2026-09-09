# ==============================================================================
# Removes the Scheduled Tasks installed by install_autostart.ps1. Does not
# stop already-running processes - use Task Manager or close the windows.
# ==============================================================================
$ErrorActionPreference = "SilentlyContinue"
Unregister-ScheduledTask -TaskName "BIA Platform - Autostart" -Confirm:$false
Unregister-ScheduledTask -TaskName "BIA Platform - JET Sync" -Confirm:$false
Write-Host "Removed 'BIA Platform - Autostart' and 'BIA Platform - JET Sync' scheduled tasks." -ForegroundColor Green
