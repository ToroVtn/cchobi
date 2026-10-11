#!/bin/sh
# Builds cchobi and installs her for you: into ~/.local/share/cchobi, with an app menu entry,
# hooked up to Claude Code. Run it again to update her. ./install.sh uninstall takes her out.
set -e
cd "$(dirname "$0")"
if command -v go >/dev/null 2>&1; then
	go build -ldflags="-s -w" -o cchobi .
elif [ ! -x cchobi ]; then
	echo "Building her needs Go: https://go.dev/dl/" >&2
	exit 1
fi
./cchobi "${1:-install}"
