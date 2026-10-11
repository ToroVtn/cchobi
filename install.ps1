# Installs cchobi, a desktop pet for Claude Code, on Windows. In PowerShell:
#
#   irm https://github.com/ToroVtn/cchobi/releases/latest/download/install.ps1 | iex
#
# (Or download cchobi-setup.exe from the same release and double-click it: it's the same thing.)
# It puts her in %LOCALAPPDATA%\cchobi, adds her to the Start menu and to Settings > Apps (where
# she can be uninstalled), hooks her up to Claude Code, and starts her. Run it again to update her.
$ErrorActionPreference = 'Stop'
$setup = Join-Path $env:TEMP 'cchobi-setup.exe'
Invoke-WebRequest -UseBasicParsing 'https://github.com/ToroVtn/cchobi/releases/latest/download/cchobi-setup.exe' -OutFile $setup
& $setup install | Out-Host # piping her output is what shows it here (she has no console of her own)
Remove-Item $setup
