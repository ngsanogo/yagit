# Remove a yagit release install made by scripts/install.ps1.
#
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/uninstall.ps1 | iex"
#
# Optional environment (same defaults as the installer):
#   YAGIT_INSTALL_DIR  directory holding the launcher (default: %USERPROFILE%\.local\bin)
#   YAGIT_HOME         directory holding the binary (default: %LOCALAPPDATA%\yagit)

$ErrorActionPreference = "Stop"

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

function Get-TokenFile {
    if ($env:YAGIT_TOKEN_FILE) { return $env:YAGIT_TOKEN_FILE }
    $local = $env:LOCALAPPDATA
    if (-not $local) { $local = Join-Path $env:USERPROFILE "AppData\Local" }
    return Join-Path $local "yagit\token"
}

function Remove-InstallPath {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Kind
    )
    if (-not (Test-Path -LiteralPath $Path)) {
        Write-Host "  $Kind  absent ($Path)"
        return
    }
    # Remove the path itself, never what a reparse point points at. A recursive
    # wipe of YAGIT_HOME would be wrong the moment somebody pointed it at a
    # directory that held more than the installer put there.
    $item = Get-Item -LiteralPath $Path -Force
    if ($item.PSIsContainer -and -not ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        $children = @(Get-ChildItem -LiteralPath $Path -Force)
        if ($children.Count -gt 0) {
            throw "cannot remove ${Kind}: ${Path} is not empty"
        }
        Remove-Item -LiteralPath $Path -Force
    } else {
        Remove-Item -LiteralPath $Path -Force
    }
    Write-Host "  $Kind  removed ($Path)"
}

$installDir = Get-InstallDir
$yagitHome = Get-YagitHome
$tokenFile = Get-TokenFile

$launcher = Join-Path $installDir "yagit.cmd"
$binary = Join-Path $yagitHome "yagit.exe"

Write-Host "Uninstalling yagit…"
Remove-InstallPath -Path $launcher -Kind "command"
Remove-InstallPath -Path $binary -Kind "binary"

if (Test-Path -LiteralPath $yagitHome) {
    $remaining = @(Get-ChildItem -LiteralPath $yagitHome -Force)
    if ($remaining.Count -eq 0) {
        Remove-Item -LiteralPath $yagitHome -Force
        Write-Host "  home     removed ($yagitHome)"
    } else {
        Write-Host "  home     kept ($yagitHome is not empty)"
    }
}

Write-Host ""
if (Test-Path -LiteralPath $tokenFile) {
    Write-Host "Session token left at $tokenFile."
    Write-Host "Remove it by hand if you no longer want that secret on disk:"
    Write-Host ""
    Write-Host "  Remove-Item -Force `"$tokenFile`""
    Write-Host ""
} else {
    Write-Host "No session token found at $tokenFile."
    Write-Host ""
}
Write-Host "Done."
