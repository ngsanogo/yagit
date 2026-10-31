@echo off
rem do.cmd <task> [args] - a shortcut for `mise run <task>`; `do.cmd` alone lists the tasks.
rem `do.cmd test go` runs the `test:go` task. The background stack (up/down) is Unix-only: use `do.cmd dev`.
setlocal
cd /d "%~dp0"
where mise >nul 2>&1
if errorlevel 1 (
  echo error: mise is yagit's only prerequisite: winget install jdx.mise 1>&2
  exit /b 1
)
mise install --quiet || exit /b 1
if "%~1"=="" goto list
if "%~1"=="help" goto list
set "task=%~1"
shift
if "%task%"=="test" if not "%~1"=="" (set "task=test:%~1" & shift)
set "args="
:collect
if "%~1"=="" goto run
set "args=%args% %1"
shift
goto collect
:run
mise run %task%%args%
exit /b %errorlevel%
:list
mise tasks
