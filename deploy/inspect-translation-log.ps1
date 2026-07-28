param(
    [string]$ConfigFile = "deploy/deploy.json",
    [string]$OutFile = "translation-debug.log",
    [switch]$Remote,
    [switch]$Errors,
    [switch]$Detail,
    [string]$GuildId = "",
    [string]$MessageId = "",
    [int]$Limit = 50,
    [string[]]$InspectArgs = @()
)

$ErrorActionPreference = "Stop"

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Command,
        [Parameter(ValueFromRemainingArguments = $true)]
        [string[]]$Arguments
    )

    Write-Host "==> $Command $($Arguments -join ' ')"
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code ${LASTEXITCODE}: $Command $($Arguments -join ' ')"
    }
}

function Assert-FileExists {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        throw "Required file not found: $Path"
    }
}

function Read-DeployConfig {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    Assert-FileExists $Path
    $raw = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    foreach ($key in @("instanceName", "zone", "remoteUser")) {
        if ([string]::IsNullOrWhiteSpace($raw.$key)) {
            throw "$key is required in $Path"
        }
    }
    return [PSCustomObject]@{
        InstanceName = [string]$raw.instanceName
        Zone         = [string]$raw.zone
        RemoteUser   = [string]$raw.remoteUser
        AppDir       = if ([string]::IsNullOrWhiteSpace($raw.appDir)) { "/opt/discord-auto-translator" } else { [string]$raw.appDir }
        ServiceName  = if ([string]::IsNullOrWhiteSpace($raw.serviceName)) { "discord-auto-translator" } else { [string]$raw.serviceName }
    }
}

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $RepoRoot

$configPath = if ([System.IO.Path]::IsPathRooted($ConfigFile)) {
    $ConfigFile
} else {
    Join-Path $RepoRoot $ConfigFile
}
$config = Read-DeployConfig $configPath

$logPath = if ([System.IO.Path]::IsPathRooted($OutFile)) {
    $OutFile
} else {
    Join-Path $RepoRoot $OutFile
}

$sshTarget = "$($config.RemoteUser)@$($config.InstanceName)"
$remoteLog = "$($config.AppDir)/translation-debug.log"

if ($Remote) {
    $checkCommand = @"
set -euo pipefail
cd '$($config.AppDir)'
if [ ! -f translation-debug.log ] && [ ! -f translation-debug.log.1 ]; then
  echo 'missing translation-debug.log on GCE' >&2
  if grep -q '^TRANSLATION_DEBUG_LOG_PATH=' .env; then
    echo 'TRANSLATION_DEBUG_LOG_PATH is set, but no log file exists yet (trigger a translation first).' >&2
  else
    echo 'TRANSLATION_DEBUG_LOG_PATH is not set in $($config.AppDir)/.env' >&2
    echo 'Set it in local .env and deploy with: .\deploy\deploy-gce.ps1 -UploadEnv' >&2
  fi
  exit 1
fi
if [ -f translation-debug.log ]; then
  echo 'HAS_ACTIVE=1'
else
  echo 'HAS_ACTIVE=0'
fi
if [ -f translation-debug.log.1 ]; then
  echo 'HAS_ROTATED=1'
else
  echo 'HAS_ROTATED=0'
fi
"@
    Write-Host "==> gcloud compute ssh $sshTarget --zone $($config.Zone) --command <check remote log>"
    $checkOutput = & gcloud compute ssh $sshTarget --zone $config.Zone --command $checkCommand
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code ${LASTEXITCODE}: gcloud compute ssh (remote log check)"
    }
    $checkText = ($checkOutput | Out-String)
    $hasActive = $checkText -match 'HAS_ACTIVE=1'
    $hasRotated = $checkText -match 'HAS_ROTATED=1'

    if (Test-Path -LiteralPath $logPath) {
        Remove-Item -LiteralPath $logPath -Force
    }
    if (Test-Path -LiteralPath "$logPath.1") {
        Remove-Item -LiteralPath "$logPath.1" -Force
    }

    if ($hasActive) {
        Invoke-Checked "gcloud" "compute", "scp", "${sshTarget}:${remoteLog}", $logPath, "--zone", $config.Zone
    }
    if ($hasRotated) {
        Invoke-Checked "gcloud" "compute", "scp", "${sshTarget}:${remoteLog}.1", "$logPath.1", "--zone", $config.Zone
    }
    if (-not $hasActive -and -not $hasRotated) {
        throw "Remote log check passed but neither active nor rotated file was reported"
    }
}

$goArgs = @("run", "./cmd/inspect-translation-log", "--path", $logPath, "--limit", "$Limit")
if ($Errors) { $goArgs += "--errors" }
if ($Detail) { $goArgs += "--detail" }
if (-not [string]::IsNullOrWhiteSpace($GuildId)) {
    $goArgs += @("--guild-id", $GuildId)
}
if (-not [string]::IsNullOrWhiteSpace($MessageId)) {
    $goArgs += @("--message-id", $MessageId)
}
if ($InspectArgs.Count -gt 0) {
    $goArgs += $InspectArgs
}

Invoke-Checked "go" @goArgs
