param(
    [string]$InstanceName = "discord-translate-bot",
    [string]$Zone = "us-central1-a",
    [string]$AppDir = "/opt/discord-auto-translator",
    [string]$ServiceName = "discord-auto-translator",
    [string]$Domain = "discord-translator.minetake.net",
    [string]$PublicBaseUrl = "https://discord-translator.minetake.net",
    [string]$RemoteUser = "minet",
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

function Invoke-GcloudSsh {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RemoteCommand
    )

    Invoke-Checked "gcloud" "compute", "ssh", $InstanceName, "--zone", $Zone, "--command", $RemoteCommand
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

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $RepoRoot

$BinaryName = "discord-auto-translator-linux-amd64"
$BinaryPath = Join-Path $RepoRoot $BinaryName

Assert-FileExists ".env"

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
    $bootstrapCommand = @"
sudo apt-get update
sudo apt-get install -y caddy
sudo mkdir -p '$AppDir'
sudo chown '${RemoteUser}:${RemoteUser}' '$AppDir'
sudo bash -lc "cat >/etc/systemd/system/$ServiceName.service <<'EOF'
[Unit]
Description=Discord Auto Translator
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$AppDir
EnvironmentFile=$AppDir/.env
ExecStart=$AppDir/$BinaryName
Restart=always
RestartSec=5
User=$RemoteUser
Group=$RemoteUser

[Install]
WantedBy=multi-user.target
EOF"
sudo bash -lc "cat >/etc/caddy/Caddyfile <<'EOF'
{
    auto_https disable_redirects
}

http://$Domain {
    reverse_proxy 127.0.0.1:8080
}

https://$Domain {
    reverse_proxy 127.0.0.1:8080
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
    $FilesToUpload += (Join-Path $RepoRoot ".env")
}
if ($UploadDb) {
    Assert-FileExists "translator.db"
    $FilesToUpload += (Join-Path $RepoRoot "translator.db")
}

$RemoteStagingDir = "/tmp/discord-auto-translator-deploy-$RemoteUser"
Invoke-GcloudSsh "rm -rf '$RemoteStagingDir' && mkdir -p '$RemoteStagingDir'"

$scpArgs = @("compute", "scp") + $FilesToUpload + @("${InstanceName}:${RemoteStagingDir}/", "--zone", $Zone)
Invoke-Checked "gcloud" @scpArgs

$remoteDeployCommand = @"
set -eu
sudo mkdir -p '$AppDir'
sudo install -m 755 '$RemoteStagingDir/$BinaryName' '$AppDir/$BinaryName'
if [ -f '$RemoteStagingDir/.env' ]; then
    sudo install -m 600 '$RemoteStagingDir/.env' '$AppDir/.env'
fi
if [ -f '$RemoteStagingDir/translator.db' ]; then
    sudo install -m 600 '$RemoteStagingDir/translator.db' '$AppDir/translator.db'
fi
sudo chown -R '${RemoteUser}:${RemoteUser}' '$AppDir'
if [ -f '$AppDir/.env' ]; then
    python3 - <<'PY'
from pathlib import Path

path = Path("$AppDir/.env")
text = path.read_text() if path.exists() else ""
entries = []
seen = set()
for raw in text.splitlines():
    line = raw.strip()
    if not line or line.startswith("#") or "=" not in line:
        entries.append(raw)
        continue
    key = line.split("=", 1)[0]
    if key in {"HTTP_ADDR", "PUBLIC_BASE_URL"}:
        if key in seen:
            continue
    seen.add(key)
    entries.append(raw)

if not any(line.strip().startswith("HTTP_ADDR=") for line in entries):
    entries.append("HTTP_ADDR=:8080")
if not any(line.strip().startswith("PUBLIC_BASE_URL=") for line in entries):
    entries.append("PUBLIC_BASE_URL=$PublicBaseUrl")

path.write_text("\n".join(entries).rstrip() + "\n")
PY
    chmod 600 '$AppDir/.env'
fi
sudo chown -R '${RemoteUser}:${RemoteUser}' '$AppDir'
rm -rf '$RemoteStagingDir'
sudo systemctl restart '$ServiceName'
sudo systemctl --no-pager --full status '$ServiceName'
"@
Invoke-GcloudSsh $remoteDeployCommand

Write-Host ""
Write-Host "Deployment complete: $PublicBaseUrl"
