@echo off
rem  do.cmd - yagit's single entry point on Windows.
rem
rem  The counterpart of ./do, which the same comments explain at length. These
rem  two files are the only places that name the directories below; nothing in
rem  cmd/do does, because by the time it runs they already exist and the tools
rem  are already pointed at them. Change both together.

setlocal
cd /d "%~dp0"

where mise >nul 2>&1
if errorlevel 1 (
  echo error: mise was not found, and it is yagit's only prerequisite. 1>&2
  echo        Install it:  winget install jdx.mise 1>&2
  echo        Then run this command again. 1>&2
  exit /b 1
)

set "YAGIT_STATE=%CD%\.yagit"
if not exist "%YAGIT_STATE%\mise" mkdir "%YAGIT_STATE%\mise"
if not exist "%YAGIT_STATE%\mise-cache" mkdir "%YAGIT_STATE%\mise-cache"
if not exist "%YAGIT_STATE%\mise-state" mkdir "%YAGIT_STATE%\mise-state"
if not exist "%YAGIT_STATE%\go-cache" mkdir "%YAGIT_STATE%\go-cache"
if not exist "%YAGIT_STATE%\go-mod-cache" mkdir "%YAGIT_STATE%\go-mod-cache"
if not exist "%YAGIT_STATE%\npm-cache" mkdir "%YAGIT_STATE%\npm-cache"
if not exist "%YAGIT_STATE%\browsers" mkdir "%YAGIT_STATE%\browsers"

set "MISE_DATA_DIR=%YAGIT_STATE%\mise"
set "MISE_CACHE_DIR=%YAGIT_STATE%\mise-cache"
set "MISE_STATE_DIR=%YAGIT_STATE%\mise-state"
set "GOCACHE=%YAGIT_STATE%\go-cache"
set "GOMODCACHE=%YAGIT_STATE%\go-mod-cache"
set "npm_config_cache=%YAGIT_STATE%\npm-cache"
set "PLAYWRIGHT_BROWSERS_PATH=%YAGIT_STATE%\browsers"

rem  -modcacherw: Go writes the module cache read-only, and a read-only
rem  directory inside the checkout is a checkout that cannot be deleted.
set "GOFLAGS=-modcacherw %GOFLAGS%"

mise install --quiet
if errorlevel 1 exit /b 1

mise exec -- go build -o .yagit\do.exe ./cmd/do
if errorlevel 1 exit /b 1

mise exec -- .yagit\do.exe %*
exit /b %errorlevel%
