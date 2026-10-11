package main

// Screens and windows, whatever the platform. Where the other windows are and focusing one comes
// from whichever desktop she's on (desktop() in os_unix.go or os_windows.go picks); monitors, from
// the platform's file.

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

// layout: the open windows, bottom to top (for her to perch on and rub against), and each
// screen's usable area (nil if the desktop can't say).
// focusWindow brings up the window whose title has project in it (VS Code's, if vscode), and
// says whether it's now the active one.
var layout, focusWindow = desktop()

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// pickWindow: of the windows (their titles and classes), the one to focus for a session in
// project: a VS Code one if vscode, otherwise anything but; failing that, any with project in its
// title. -1 if none.
func pickWindow(titles, classes []string, project string, vscode bool) int {
	first := -1
	for i, t := range titles {
		if !strings.Contains(t, project) {
			continue
		}
		if strings.Contains(strings.ToLower(classes[i]), "code") == vscode {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

// workAreas: each screen's usable area (minus its panels), as layout reports it.
var workAreas []rect

// workArea: the usable part of the screen a point is on (or nearest to: on a panel, say).
func workArea(x, y int) rect {
	best, bestD := rect{}, -1
	for _, a := range workAreas {
		dx, dy := max(a.x-x, 0, x-(a.x+a.w-1)), max(a.y-y, 0, y-(a.y+a.h-1))
		if d := dx*dx + dy*dy; bestD < 0 || d < bestD {
			best, bestD = a, d
		}
	}
	if bestD >= 0 {
		return best
	}
	g, _ := screenAt(x, y)
	g.h -= 60 // layout didn't say: guess a 60 px panel at the bottom
	return g
}

// screenAt: the screen a point is on, if any.
func screenAt(x, y int) (rect, bool) {
	for _, m := range monitors() {
		if m.contains(x, y) {
			return m, true
		}
	}
	return monitors()[0], false
}

// urlScheme: vscode:// for Microsoft's build, code-oss:// for the open-source one, and so on.
func urlScheme() string {
	for _, p := range []string{"/usr/lib/code/product.json", "/usr/share/code/resources/app/product.json",
		"/opt/visual-studio-code/resources/app/product.json"} {
		var product struct{ UrlProtocol string }
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &product) == nil && product.UrlProtocol != "" {
			return product.UrlProtocol
		}
	}
	return "vscode"
}

// focus brings up the window of the session that called, and in VS Code its Claude tab.
// ponytail: matches the window by project folder name in its title; two windows on the same
// folder (or a terminal that doesn't show the folder) pick the first match.
func focus(p *Pending) {
	if focusWindow(p.project, p.vscode) && p.vscode && p.session != "" {
		// The Claude extension opens a session's tab from a URL. It lands in the VS Code window
		// focused last, so only once that's this one: in another, it would start a new session.
		url := urlScheme() + "://anthropic.claude-code/open?session=" + p.session
		time.Sleep(100 * time.Millisecond) // VS Code hears it's been focused
		exec.Command("code", "--open-url", url).Start()
	}
}
