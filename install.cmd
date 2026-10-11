@echo off
rem From a checkout of the source, with Go: builds cchobi and installs her (see install.ps1 for
rem what that does). Run it again to update her; "install.cmd uninstall" takes her out.
cd /d "%~dp0"
go build -ldflags="-s -w -H=windowsgui" -o cchobi.exe . || (pause & exit /b 1)
set action=%1
if "%action%"=="" set action=install
rem She has no console of her own (windowsgui): piping her output is what shows it, and waits for her.
cchobi.exe %action% 2>&1 | more
pause
