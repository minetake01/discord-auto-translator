param(
    [string]$ConfigFile = "deploy/deploy.json",
    [string]$EnvFile,
    [switch]$Bootstrap,
    [switch]$UploadEnv,
    [switch]$UploadDb,
    [switch]$SkipTests
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
        EnvFile      = if ([string]::IsNullOrWhiteSpace($raw.envFile)) { ".env" } else { [string]$raw.envFile }
    }
}

function Read-DotEnv {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $values = @{}
    foreach ($rawLine in Get-Content -LiteralPath $Path) {
        $line = $rawLine.Trim()
        if ($line -eq "" -or $line.StartsWith("#")) {
            continue
        }
        $parts = $line -split "=", 2
        if ($parts.Count -ne 2) {
            continue
        }
        $key = $parts[0].Trim()
        $value = $parts[1].Trim().Trim('"').Trim("'")
        if (-not $values.ContainsKey($key)) {
            $values[$key] = $value
        }
    }
    return $values
}

function Get-HttpListenPort {
    param(
        [Parameter(Mandatory = $true)]
        [string]$HttpAddr
    )

    if ($HttpAddr -match ':(\d+)$') {
        return $Matches[1]
    }
    throw "HTTP_ADDR must include a port (e.g. :8080); got: $HttpAddr"
}

function Get-PublicBaseURLHost {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PublicBaseURL
    )

    try {
        $uri = [Uri]$PublicBaseURL
    } catch {
        throw "PUBLIC_BASE_URL is invalid: $PublicBaseURL"
    }
    if ($uri.Scheme -notin @("http", "https")) {
        throw "PUBLIC_BASE_URL must use http or https: $PublicBaseURL"
    }
    if ([string]::IsNullOrWhiteSpace($uri.Host)) {
        throw "PUBLIC_BASE_URL must include a host: $PublicBaseURL"
    }
    return $uri.Host
}

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $RepoRoot

$configPath = if ([System.IO.Path]::IsPathRooted($ConfigFile)) {
    $ConfigFile
} else {
    Join-Path $RepoRoot $ConfigFile
}
$config = Read-DeployConfig $configPath

$resolvedEnvFile = if (-not [string]::IsNullOrWhiteSpace($EnvFile)) {
    $EnvFile
} else {
    $config.EnvFile
}
$envPath = if ([System.IO.Path]::IsPathRooted($resolvedEnvFile)) {
    $resolvedEnvFile
} else {
    Join-Path $RepoRoot $resolvedEnvFile
}
$envBaseName = Split-Path -Leaf $envPath

$publicBaseUrl = ""
$httpAddr = ":8080"

if ($Bootstrap -or $UploadEnv) {
    Assert-FileExists $envPath
    $envVars = Read-DotEnv $envPath
    $publicBaseUrl = $envVars["PUBLIC_BASE_URL"]
    if ($envVars.ContainsKey("HTTP_ADDR") -and -not [string]::IsNullOrWhiteSpace($envVars["HTTP_ADDR"])) {
        $httpAddr = $envVars["HTTP_ADDR"]
    }
}

function Invoke-GcloudSsh {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RemoteCommand
    )

    Invoke-Checked "gcloud" "compute", "ssh", $config.InstanceName, "--zone", $config.Zone, "--command", $RemoteCommand
}

$BinaryName = "discord-auto-translator-linux-amd64"
$BinaryPath = Join-Path $RepoRoot $BinaryName

if (-not $SkipTests) {
    Invoke-Checked "go" "test", "./..."
}

Write-Host "==> Building linux/amd64 binary"
$oldGoos = $env:GOOS
$oldGoarch = $env:GOARCH
$oldCgo = $env:CGO_ENABLED
try {
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    Invoke-Checked "go" "build", "-o", $BinaryPath, "./cmd/discord-auto-translator"
}
finally {
    $env:GOOS = $oldGoos
    $env:GOARCH = $oldGoarch
    $env:CGO_ENABLED = $oldCgo
}

if ($Bootstrap) {
    if ([string]::IsNullOrWhiteSpace($publicBaseUrl)) {
        throw "PUBLIC_BASE_URL must be set in $resolvedEnvFile for -Bootstrap (required for Caddy reverse proxy)"
    }
    $caddyDomain = Get-PublicBaseURLHost $publicBaseUrl
    $proxyPort = Get-HttpListenPort $httpAddr

    $bootstrapCommand = @"
sudo apt-get update
sudo apt-get install -y caddy
sudo mkdir -p '$($config.AppDir)'
sudo chown '$($config.RemoteUser):$($config.RemoteUser)' '$($config.AppDir)'
sudo bash -lc "cat >/etc/systemd/system/$($config.ServiceName).service <<'EOF'
[Unit]
Description=Discord Auto Translator
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$($config.AppDir)
EnvironmentFile=$($config.AppDir)/.env
ExecStart=$($config.AppDir)/$BinaryName
Restart=always
RestartSec=5
User=$($config.RemoteUser)
Group=$($config.RemoteUser)

[Install]
WantedBy=multi-user.target
EOF"
sudo bash -lc "cat >/etc/caddy/Caddyfile <<'EOF'
{
    auto_https disable_redirects
}

http://$caddyDomain {
    reverse_proxy 127.0.0.1:$proxyPort
}

https://$caddyDomain {
    reverse_proxy 127.0.0.1:$proxyPort
}
EOF"
sudo caddy fmt --overwrite /etc/caddy/Caddyfile
sudo systemctl daemon-reload
sudo systemctl enable caddy
sudo systemctl reload caddy || sudo systemctl restart caddy
"@
    Invoke-GcloudSsh $bootstrapCommand
}

$FilesToUpload = @($BinaryPath)
if ($UploadEnv) {
    $FilesToUpload += $envPath
}
if ($UploadDb) {
    Assert-FileExists "translator.db"
    $FilesToUpload += (Join-Path $RepoRoot "translator.db")
}

$RemoteStagingDir = "/tmp/discord-auto-translator-deploy-$($config.InstanceName)"
Invoke-GcloudSsh "rm -rf '$RemoteStagingDir' && mkdir -p '$RemoteStagingDir'"

$scpArgs = @("compute", "scp") + $FilesToUpload + @("$($config.InstanceName):${RemoteStagingDir}/", "--zone", $config.Zone)
Invoke-Checked "gcloud" @scpArgs

$remoteDeployCommand = @"
set -eu
chmod 700 '$RemoteStagingDir/$BinaryName'
if [ -f '$RemoteStagingDir/$envBaseName' ]; then
    '$RemoteStagingDir/$BinaryName' --env-file '$RemoteStagingDir/$envBaseName' --bedrock-prewarm
else
    '$RemoteStagingDir/$BinaryName' --env-file '$($config.AppDir)/.env' --bedrock-prewarm
fi
sudo mkdir -p '$($config.AppDir)'
sudo install -m 755 '$RemoteStagingDir/$BinaryName' '$($config.AppDir)/$BinaryName'
if [ -f '$RemoteStagingDir/$envBaseName' ]; then
    sudo install -m 600 '$RemoteStagingDir/$envBaseName' '$($config.AppDir)/.env'
fi
if [ -f '$RemoteStagingDir/translator.db' ]; then
    sudo install -m 600 '$RemoteStagingDir/translator.db' '$($config.AppDir)/translator.db'
fi
sudo chown -R '$($config.RemoteUser):$($config.RemoteUser)' '$($config.AppDir)'
rm -rf '$RemoteStagingDir'
sudo systemctl restart '$($config.ServiceName)'
sudo systemctl --no-pager --full status '$($config.ServiceName)'
"@
Invoke-GcloudSsh $remoteDeployCommand

Write-Host ""
if ([string]::IsNullOrWhiteSpace($publicBaseUrl)) {
    Write-Host "Deployment complete."
} else {
    Write-Host "Deployment complete: $publicBaseUrl"
}
