# SMB OS Desktop installer for Windows.
#
#   irm https://github.com/wayneColt/smb-os-desktop/releases/latest/download/install.ps1 | iex
#
# Downloads the Windows build, checks its SHA-256 against the release's
# SHA256SUMS.txt, installs it to %LOCALAPPDATA%\SMB-OS-Desktop and starts it.
# Nothing is installed if the checksum does not match. Files downloaded this
# way are not marked as coming from the internet, so SmartScreen does not
# prompt; the checksum is what vouches for the file.
#
# Environment (all optional):
#   AIOS_BASE_URL     download from here instead of the latest GitHub release
#   AIOS_VERSION      install this version (e.g. 0.1.0) instead of the latest
#   AIOS_INSTALL_DIR  install into this folder
#   AIOS_NO_START=1   install only; don't start it
#
# Works in Windows PowerShell 5.1 and PowerShell 7. Errors are thrown rather
# than calling exit, so a failed `irm | iex` does not close the window.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
} catch { }

$repo = 'wayneColt/smb-os-desktop'
if ($env:AIOS_BASE_URL) {
    $base = $env:AIOS_BASE_URL.TrimEnd('/')
} elseif ($env:AIOS_VERSION) {
    $base = "https://github.com/$repo/releases/download/v$($env:AIOS_VERSION.TrimStart('v'))"
} else {
    $base = "https://github.com/$repo/releases/latest/download"
}

# Only a windows/amd64 build is published; Windows on ARM runs it under
# x64 emulation.
$platform = 'windows_amd64'
if ($env:AIOS_INSTALL_DIR) {
    $dest = $env:AIOS_INSTALL_DIR
} else {
    $dest = Join-Path $env:LOCALAPPDATA 'SMB-OS-Desktop'
}
$exe = Join-Path $dest 'aios.exe'

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('smb-os-desktop-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Downloading the release checksums from $base ..."
    $sums = Join-Path $tmp 'SHA256SUMS.txt'
    Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS.txt" -OutFile $sums
    $pattern = '^[0-9a-f]{64}  smb-os-desktop_[0-9A-Za-z.+-]+_' + $platform + '\.zip$'
    $lines = @(Get-Content -LiteralPath $sums | Where-Object { $_ -cmatch $pattern })
    if ($lines.Count -ne 1) {
        throw "install: SHA256SUMS.txt does not list exactly one $platform build."
    }
    $want, $zip = $lines[0] -split '  ', 2

    Write-Host "Downloading $zip ..."
    $zipPath = Join-Path $tmp $zip
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$zip" -OutFile $zipPath
    $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $zipPath).Hash.ToLowerInvariant()
    if ($got -ne $want) {
        throw "install: checksum mismatch for ${zip}: expected $want, got $got. Nothing was installed."
    }
    Write-Host "Checksum verified: $got"

    $unpacked = Join-Path $tmp 'unpacked'
    Expand-Archive -LiteralPath $zipPath -DestinationPath $unpacked -Force
    $newExe = Join-Path $unpacked 'aios.exe'
    if (-not (Test-Path -LiteralPath $newExe)) {
        throw 'install: the download does not contain aios.exe.'
    }

    $running = @(Get-Process -Name 'aios' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe })
    if ($running.Count -gt 0) {
        throw "install: SMB OS Desktop is running from $dest. Stop it (Ctrl+C in its window) and run the installer again."
    }
    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    Copy-Item -Force -LiteralPath $newExe -Destination $exe
    $readme = Join-Path $unpacked 'README.txt'
    if (Test-Path -LiteralPath $readme) {
        Copy-Item -Force -LiteralPath $readme -Destination (Join-Path $dest 'README.txt')
    }
} finally {
    Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction SilentlyContinue
}

$version = & $exe --version
Write-Host "Installed $version to $dest"
if ($env:AIOS_NO_START -eq '1') {
    Write-Host "Start it any time by double-clicking $exe"
    return
}
Write-Host 'Starting SMB OS Desktop in a new window. Press Ctrl+C there to stop it.'
if ($args.Count -gt 0) {
    Start-Process -FilePath $exe -WorkingDirectory $dest -ArgumentList $args
} else {
    Start-Process -FilePath $exe -WorkingDirectory $dest
}
