# Runs Go commands in Docker (Go is not installed on the host).
#
#   .\scripts\go.ps1 test ./...                    # unit tests only (DB tests skip)
#   .\scripts\go.ps1 --db test -v ./internal/...   # with Postgres + MongoDB from docker compose (RLS applies)
#   .\scripts\go.ps1 mod tidy
#   .\scripts\go.ps1 vet ./...
#
# A plain script (no param block) on purpose: every argument, including Go flags like -v,
# is passed through untouched instead of being parsed by PowerShell.
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$goArgs = @($args)
$dockerArgs = @(
    'run', '--rm',
    '-v', "${root}\backend:/src",
    '-v', 'mscs-gomodcache:/go/pkg/mod',
    '-v', 'mscs-gobuildcache:/root/.cache/go-build',
    '-w', '/src'
)

if ($goArgs.Count -gt 0 -and $goArgs[0] -eq '--db') {
    $goArgs = @($goArgs | Select-Object -Skip 1)

    # Read .env into a hashtable (KEY=VALUE lines only).
    $vars = @{}
    Get-Content (Join-Path $root '.env') | Where-Object { $_ -match '^\s*([A-Z_]+)=(.*)$' } |
        ForEach-Object { $vars[$Matches[1]] = $Matches[2] }

    # Join the compose network and log in as the restricted app role, so RLS applies.
    # Passed by name only (-e DATABASE_URL), so the password never appears in the process list.
    $env:DATABASE_URL = "postgres://mscs_app:$($vars.APP_DB_PASSWORD)@postgres:5432/$($vars.POSTGRES_DB)?sslmode=disable"
    $env:MONGO_URI = "mongodb://$($vars.MONGO_USER):$($vars.MONGO_PASSWORD)@mongo:27017/?authSource=admin"
    $dockerArgs += @('--network', 'mscs_default', '-e', 'DATABASE_URL', '-e', 'MONGO_URI')
}

try {
    # Go prints progress (e.g. "go: downloading ...") to stderr; in Windows PowerShell 5.1 that
    # must not count as a script error. The exit code below is what reports failure.
    $ErrorActionPreference = 'Continue'
    # Full Debian-based image (not alpine): it includes the C toolchain that `go test -race` needs.
    docker @dockerArgs golang:1.26 go @goArgs
    exit $LASTEXITCODE
}
finally {
    Remove-Item Env:DATABASE_URL, Env:MONGO_URI -ErrorAction SilentlyContinue
}
