@echo off
rem Builds cchobi and installs her for you: into %LOCALAPPDATA%\cchobi, with a Start menu entry,
rem hooked up to Claude Code. Run it again to update her. "install.cmd uninstall" takes her out.
cd /d "%~dp0"
where go >nul 2>nul && go build -ldflags="-s -w -H=windowsgui" -o cchobi.exe .
if not exist cchobi.exe (
	echo Building her needs Go: https://go.dev/dl/
	pause
	exit /b 1
)
set action=%1
if "%action%"=="" set action=install
rem She has no console of her own (windowsgui): piping her output is what shows it, and waits for her.
cchobi.exe %action% 2>&1 | more
pause
