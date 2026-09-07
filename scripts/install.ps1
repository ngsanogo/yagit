# Install the latest yagit release binary for this machine.
#
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/install.ps1 | iex"
#
# Optional environment:
#   YAGIT_VERSION      release tag (default: latest)
#   YAGIT_INSTALL_DIR  directory for the launcher (default: %USERPROFILE%\.local\bin)
#   YAGIT_HOME         directory for the binary (default: %LOCALAPPDATA%\yagit)

$ErrorActionPreference = "Stop"

# Windows PowerShell 5.1 is still the shell this line is pasted into, and it
# negotiates whatever [Net.ServicePointManager]::SecurityProtocol holds — a
# list that on an unpatched machine can still exclude TLS 1.2. The releases are
# served over TLS 1.2 and above, so without this the download fails with "Could
# not create SSL/TLS secure channel", which names neither the cause nor the fix.
#
# Added to the list rather than replacing it: on PowerShell 7 the default
# already includes 1.3, and assigning Tls12 alone would quietly take it away.
[Net.ServicePointManager]::SecurityProtocol =
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$Repo = "ngsanogo/yagit"
$BaseUrl = "https://github.com/$Repo/releases"

function Get-InstallDir {
    if ($env:YAGIT_INSTALL_DIR) { return $env:YAGIT_INSTALL_DIR }
    return Join-Path $env:USERPROFILE ".local\bin"
}

function Get-YagitHome {
    if ($env:YAGIT_HOME) { return $env:YAGIT_HOME }
    $local = $env:LOCALAPPDATA
    if (-not $local) { $local = Join-Path $env:USERPROFILE "AppData\Local" }
    return Join-Path $local "yagit"
}

function Get-Target {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    switch ($arch) {
        "x64" { return "windows-amd64" }
        "arm64" { return "windows-arm64" }
        default { throw "unsupported architecture: $arch" }
    }
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -Algorithm SHA256 -Path $Path).Hash.ToLowerInvariant()
}

$installDir = Get-InstallDir
$yagitHome = Get-YagitHome
$target = Get-Target
$asset = "yagit-$target.exe"

if ($env:YAGIT_VERSION) {
    $version = $env:YAGIT_VERSION
    if (-not $version.StartsWith("v")) { $version = "v$version" }
    $downloadRoot = "$BaseUrl/download/$version"
} else {
    $version = "latest"
    $downloadRoot = "$BaseUrl/latest/download"
}

$tmpdir = Join-Path ([System.IO.Path]::GetTempPath()) ("yagit-install-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $tmpdir | Out-Null
try {
    Write-Host "Downloading yagit ($version, $target)…"

    $assetPath = Join-Path $tmpdir $asset
    $sumsPath = Join-Path $tmpdir "SHA256SUMS"
    Invoke-WebRequest -Uri "$downloadRoot/$asset" -OutFile $assetPath -UseBasicParsing
    Invoke-WebRequest -Uri "$downloadRoot/SHA256SUMS" -OutFile $sumsPath -UseBasicParsing

    $expected = $null
    foreach ($line in Get-Content $sumsPath) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and $parts[1] -eq $asset) {
            $expected = $parts[0].ToLowerInvariant()
            break
        }
    }
    if (-not $expected) { throw "SHA256SUMS has no entry for $asset" }

    $got = Get-Sha256 $assetPath
    if ($got -ne $expected) {
        throw "checksum mismatch for $asset (got $got, want $expected)"
    }

    New-Item -ItemType Directory -Force -Path $yagitHome | Out-Null
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null

    $binary = Join-Path $yagitHome "yagit.exe"
    Copy-Item -Force $assetPath $binary

    $launcher = Join-Path $installDir "yagit.cmd"
    $launcherBody = @"
@echo off
REM Launcher installed by scripts/install.ps1 — defaults a repository root and a
REM session token file so `yagit` is enough to start.
set "ROOT=%YAGIT_ROOT%"
if "%ROOT%"=="" set "ROOT=%USERPROFILE%"
set "TOKEN_FILE=%YAGIT_TOKEN_FILE%"
if "%TOKEN_FILE%"=="" set "TOKEN_FILE=%LOCALAPPDATA%\yagit\token"
if not exist "%LOCALAPPDATA%\yagit" mkdir "%LOCALAPPDATA%\yagit"
"$binary" -root "%ROOT%" -token-file "%TOKEN_FILE%" %*
"@
    # OEM, not ASCII: the launcher embeds $binary, which sits under
    # %LOCALAPPDATA% and therefore holds the account name. ASCII turns every
    # character outside it into a question mark, so an account called Renée or
    # 陈 gets a launcher pointing at a path that does not exist — and the error
    # names the mangled path rather than the encoding that mangled it.
    #
    # OEM rather than UTF-8 because cmd.exe reads a .cmd in the console code
    # page; a UTF-8 file would be mangled the other way round.
    Set-Content -Path $launcher -Value $launcherBody -Encoding Oem

    Write-Host ""
    Write-Host "Installed:"
    Write-Host "  binary   $binary"
    Write-Host "  command  $launcher"
    Write-Host ""

    $pathEntries = $env:PATH -split ';'
    if ($pathEntries -notcontains $installDir) {
        Write-Host "Add $installDir to your PATH, then run:"
        Write-Host ""
    }
    Write-Host "  yagit"
    Write-Host ""
    Write-Host "Open the URL it prints. Repositories under your profile are allowed by default;"
    Write-Host "set YAGIT_ROOT to narrow that. Requires git on PATH."
}
finally {
    Remove-Item -Recurse -Force $tmpdir -ErrorAction SilentlyContinue
}
