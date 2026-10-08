#requires -Version 7.0
[CmdletBinding()]
param(
    [string]$NodePath = 'node',
    [switch]$Preview
)

$ErrorActionPreference = 'Stop'
$repoPath = Split-Path -Path $PSScriptRoot -Parent
$frontPath = Join-Path $repoPath 'frontend'
$logPath = Join-Path ([IO.Path]::GetTempPath()) 'sub2api-multibrand-verification'
New-Item -ItemType Directory -Path $logPath -Force | Out-Null

function Invoke-Native {
    param([string]$Executable, [string[]]$Arguments)
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Executable failed with exit code $LASTEXITCODE"
    }
}

function Ensure-TestContainer {
    param([string]$Name, [string[]]$CreateArguments)
    $result = @(& docker container ls --all --filter "name=^/$Name$" --format '{{.Names}}')
    if ($LASTEXITCODE -ne 0) { throw 'Could not list local test containers' }
    if ($result -contains $Name) {
        $label = & docker inspect --format '{{index .Config.Labels "codex.task"}}' $Name
        if ($LASTEXITCODE -ne 0) { throw "Could not inspect $Name" }
        if ($label -ne 'multibrand') { throw "Refusing to reuse an unrelated container: $Name" }
        Invoke-Native 'docker' @('start', $Name)
    } else {
        Invoke-Native 'docker' (@('run', '--detach', '--name', $Name, '--label', 'codex.task=multibrand') + $CreateArguments)
    }
}

Ensure-TestContainer 'sub2api-multibrand-db' @(
    '-p', '127.0.0.1:25432:5432',
    '-e', 'POSTGRES_PASSWORD=multibrand_local_test',
    '-e', 'POSTGRES_DB=multibrand_test', 'postgres:18-bookworm'
)
Ensure-TestContainer 'sub2api-multibrand-redis' @(
    '-p', '127.0.0.1:26379:6379', 'redis:8-alpine'
)
$ready = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    & docker exec sub2api-multibrand-db pg_isready -U postgres -d multibrand_test | Out-Null
    $readyExit = $LASTEXITCODE
    if ($readyExit -eq 0) { $ready = $true; break }
    if ($readyExit -ne 1 -and $readyExit -ne 2) { throw "PostgreSQL readiness failed: $readyExit" }
    Start-Sleep -Seconds 1
}
if (-not $ready) { throw 'Local test PostgreSQL did not become ready' }
$redisReady = $false
$redisPing = @()
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    $redisPing = @(& docker exec sub2api-multibrand-redis redis-cli ping 2>&1)
    $redisExit = $LASTEXITCODE
    if ($redisExit -eq 0 -and (($redisPing -join "`n").Trim() -eq 'PONG')) {
        $redisReady = $true
        break
    }
    Start-Sleep -Seconds 1
}
if (-not $redisReady) {
    throw "Local test Redis did not become ready (exit $redisExit): $(($redisPing -join ' ').Trim())"
}

$dockerGo = @(
    'run', '--rm', '-v', "${repoPath}:/workspace",
    '-v', 'sub2api-review-gomod:/go/pkg/mod',
    '-v', 'sub2api-review-gobuild:/root/.cache/go-build',
    '-e', 'MULTIBRAND_TEST_DSN=host=host.docker.internal port=25432 user=postgres password=multibrand_local_test dbname=multibrand_test sslmode=disable',
    '-e', 'MULTIBRAND_TEST_REDIS=host.docker.internal:26379',
    '-w', '/workspace/backend'
)

Push-Location -LiteralPath $frontPath
try {
    foreach ($relative in @('node_modules\vue-tsc\bin\vue-tsc.js', 'node_modules\vitest\vitest.mjs', 'node_modules\vite\bin\vite.js')) {
        if (-not (Test-Path -LiteralPath (Join-Path $frontPath $relative))) {
            throw 'Install the frontend dependencies from the frozen pnpm lockfile first.'
        }
    }
    Invoke-Native $NodePath @((Join-Path $frontPath 'node_modules\vue-tsc\bin\vue-tsc.js'), '--noEmit')
    Invoke-Native $NodePath @(
        (Join-Path $frontPath 'node_modules\vitest\vitest.mjs'), 'run',
        'src/stores/brand.test.ts', 'src/utils/brandMarkdown.test.ts'
    )
    Invoke-Native $NodePath @((Join-Path $frontPath 'node_modules\vite\bin\vite.js'), 'build')
} finally {
    Pop-Location
}

$unitArgs = $dockerGo + @(
    'golang:1.27-bookworm', 'go', 'test', '-p', '1', '-tags', 'unit,embed',
    './internal/brand', './internal/config', './internal/server/...',
    './internal/repository', './internal/handler/...', './internal/service',
    './internal/web', './internal/setup', '-json', '-timeout', '10m'
)
$unitLog = Join-Path $logPath 'unit.jsonl'
& docker @unitArgs | Out-File -LiteralPath $unitLog -Encoding utf8
$unitExit = $LASTEXITCODE
if ($unitExit -ne 0) {
    Get-Content -LiteralPath $unitLog | ForEach-Object {
        try { $event = $_ | ConvertFrom-Json } catch { return }
        if ($event.Action -eq 'fail') { Write-Output "$($event.Package): $($event.Test)" }
    }
    throw "Unit tests failed; inspect $unitLog"
}

Invoke-Native 'docker' ($dockerGo + @(
    'golang:1.27-bookworm', 'go', 'test', '-tags', 'multibrand,embed',
    './internal/repository', './internal/server/middleware',
    '-run', '^TestMultiBrand', '-v', '-count=1', '-timeout', '3m'
))
Invoke-Native 'docker' ($dockerGo + @(
    'golang:1.27-bookworm', 'go', 'build', '-tags', 'embed',
    '-o', '/tmp/sub2api-multibrand', './cmd/server'
))

Write-Output "Multi-brand checks passed. Unit log: $unitLog"
if ($Preview) {
    Write-Output 'Preview hosts: llmp.localhost, mues.localhost, aisi.localhost, codes.localhost, in.localhost; port 28080.'
    Write-Output 'Disposable fixture login: http-shared@multibrand.test / NewPassword!'
    Write-Output 'Stop with Invoke-WebRequest -Uri http://127.0.0.1:28080/__preview/stop -Method Post'
    Invoke-Native 'docker' (@(
        'run', '--rm', '--name', 'sub2api-multibrand-preview', '--label', 'codex.task=multibrand',
        '-p', '127.0.0.1:28080:28080',
        '-e', 'MULTIBRAND_PREVIEW_LISTEN=0.0.0.0:28080'
    ) + $dockerGo[2..($dockerGo.Length - 1)] + @(
        'golang:1.27-bookworm', 'go', 'test', '-tags', 'multibrand,embed',
        './internal/repository', '-run', '^TestMultiBrandHTTP$', '-v', '-count=1', '-timeout', '30m'
    ))
}
