#!/bin/sh
# Installs cchobi, a desktop pet for Claude Code, on Linux:
#
#   curl -fsSL https://github.com/ToroVtn/cchobi/releases/latest/download/install.sh | sh
#
# It downloads the right build for your machine and runs "cchobi install", which puts her in
# ~/.local/share/cchobi, adds her to the app menu, hooks her up to Claude Code, installs what plays
# her sounds if it's missing, and starts her. From a checkout of the source with Go, it builds her
# instead. Run it again to update her; add "uninstall" (... | sh -s uninstall) to take her out.
set -e

main() {
	action=${1:-install}
	src=$(cd "$(dirname "$0")" 2>/dev/null && pwd) || src=
	if [ -n "$src" ] && grep -qs '^module github.com/ToroVtn/cchobi$' "$src/go.mod" && command -v go >/dev/null 2>&1; then
		(cd "$src" && go build -ldflags="-s -w" -o cchobi .)
		exec "$src/cchobi" "$action"
	fi
	if [ "$(uname -s)" != Linux ]; then
		echo "This script is for Linux. On Windows, see https://github.com/ToroVtn/cchobi#install" >&2
		exit 1
	fi
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*)
		echo "No build for $(uname -m) yet. With Go: go install github.com/ToroVtn/cchobi@latest && cchobi install" >&2
		exit 1
		;;
	esac
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	url=https://github.com/ToroVtn/cchobi/releases/latest/download/cchobi-linux-$arch
	echo "Downloading $url"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$tmp/cchobi" "$url"
	else
		wget -qO "$tmp/cchobi" "$url"
	fi
	chmod +x "$tmp/cchobi"
	"$tmp/cchobi" "$action"
}

main "$@" # only once the whole script has arrived, when it's piped in
