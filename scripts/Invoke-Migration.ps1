<#
.SYNOPSIS
    Windows PowerShell alternative to the Makefile migrate-* targets.

.DESCRIPTION
    Runs the golang-migrate binary via `go run` from the api/ directory.
    Requires DATABASE_URL to be set (via environment or .env at the repo root).

.PARAMETER Command
    Migration command: up | down | version | force
    Defaults to 'up'.

.PARAMETER Version
    Version number for the 'force' command.

.PARAMETER Path
    Override the migrations path (golang-migrate file:// URL).
    Defaults to file://../db/migrations (relative to api/).

.EXAMPLE
    .\scripts\Invoke-Migration.ps1
    .\scripts\Invoke-Migration.ps1 -Command down
    .\scripts\Invoke-Migration.ps1 -Command version
    .\scripts\Invoke-Migration.ps1 -Command force -Version 3
#>

param(
    [ValidateSet('up', 'down', 'version', 'force')]
    [string]$Command = 'up',

    [int]$Version,

    [string]$Path = 'file://../db/migrations'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Load .env from repo root if DATABASE_URL is not already set
if (-not $env:DATABASE_URL) {
    $envFile = Join-Path $PSScriptRoot '..' '.env'
    if (Test-Path $envFile) {
        Get-Content $envFile | Where-Object { $_ -match '^\s*DATABASE_URL\s*=' } | ForEach-Object {
            $value = ($_ -split '=', 2)[1].Trim()
            $env:DATABASE_URL = $value
        }
    }
}

if (-not $env:DATABASE_URL) {
    Write-Error 'DATABASE_URL environment variable is required. Set it in your shell or add it to .env'
    exit 1
}

$repoRoot = Split-Path $PSScriptRoot -Parent
$apiDir   = Join-Path $repoRoot 'api'

Push-Location $apiDir
try {
    $goArgs = @('run', './cmd/migrate', '-path', $Path, $Command)
    if ($Command -eq 'force') {
        if (-not $PSBoundParameters.ContainsKey('Version')) {
            Write-Error "The 'force' command requires -Version <N>"
            exit 1
        }
        $goArgs += $Version
    }
    & go @goArgs
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
finally {
    Pop-Location
}
