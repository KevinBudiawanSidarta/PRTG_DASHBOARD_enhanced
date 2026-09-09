# ==============================================================================
# Loads key=value pairs from the repo-root .env into the current process
# environment (without overwriting variables already set). Dot-source this
# from other scripts: . (Join-Path $PSScriptRoot "_env.ps1")
# ==============================================================================
$rootDir = Split-Path $PSScriptRoot -Parent
$envFile = Join-Path $rootDir ".env"

if (Test-Path $envFile) {
    Get-Content $envFile | Where-Object { $_ -notmatch '^\s*#' -and $_ -match '^[^=]+=' } | ForEach-Object {
        $n, $v = $_ -split '=', 2
        $n = $n.Trim(); $v = $v.Trim()
        if ($n -and -not [System.Environment]::GetEnvironmentVariable($n, 'Process')) {
            [System.Environment]::SetEnvironmentVariable($n, $v, 'Process')
        }
    }
}
