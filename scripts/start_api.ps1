$content = Get-Content .env
foreach ($line in $content) {
    if ($line -notmatch '^\s*#' -and $line -match '^([^=]+)=(.*)$') {
        $n = $Matches[1].Trim()
        $v = $Matches[2].Trim()
        [System.Environment]::SetEnvironmentVariable($n, $v, 'Process')
    }
}
go run ./services/api/cmd
