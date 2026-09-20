$ErrorActionPreference = 'Stop'
$repo = 'chensunlai/Ritalin'
$version = if ($env:RITALIN_VERSION) { $env:RITALIN_VERSION } else { 'latest' }
$installDir = if ($env:RITALIN_INSTALL_DIR) { $env:RITALIN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\codex-ritalin' }
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
try { $cpu = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant() }
catch { $cpu = "$env:PROCESSOR_ARCHITECTURE".ToLowerInvariant() }
$arch = switch ($cpu) { 'x64' { 'amd64' }; 'amd64' { 'amd64' }; 'arm64' { 'arm64' }; 'x86' { '386' }; default { throw "Unsupported architecture: $cpu" } }
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ('ritalin-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
    if ($version -eq 'latest') {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'Ritalin-installer' }
        $version = $release.tag_name
    }
    if ($version -notmatch '^[A-Za-z0-9._-]+$') { throw 'Invalid release version' }
    $asset = "codex-ritalin_windows_$arch.zip"
    $base = "https://github.com/$repo/releases/download/$version"
    Write-Host "Downloading Ritalin $version (windows/$arch)..."
    $zip = Join-Path $tempDir $asset
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $zip
    Expand-Archive -Path $zip -DestinationPath (Join-Path $tempDir 'unpack')
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item -Force (Join-Path $tempDir 'unpack\codex-ritalin.exe') (Join-Path $installDir 'codex-ritalin.exe')
    $userPath = [string][Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $installDir) {
        [Environment]::SetEnvironmentVariable('Path', (($userPath.TrimEnd(';') + ';' + $installDir).TrimStart(';')), 'User')
    }
    if (($env:Path -split ';') -notcontains $installDir) { $env:Path += ";$installDir" }
    Write-Host "Installed: $installDir\codex-ritalin.exe"
    Write-Host 'Start: codex-ritalin dosing'
} finally {
    if (Test-Path -LiteralPath $tempDir) { Remove-Item -LiteralPath $tempDir -Recurse -Force }
}
