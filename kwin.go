package main

// The desktop, as KDE Plasma's KWin knows it: where the windows are, and focusing one.

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// kwinAsk runs a KWin script and returns what it prints after tag (a const it's given), or "" if
// it says nothing within two seconds. The script lives inside the compositor, so unlike an app on
// Wayland it can see and focus any window; but it can only answer by printing to the journal.
func kwinAsk(source, name string) string {
	tag := fmt.Sprintf("cchobi-%x ", rand.Uint32())
	js := runtime_ + "/" + name + ".js"
	os.WriteFile(js, []byte(fmt.Sprintf("const tag = %q;\n", tag)+source), 0o644)
	bus, err := dbus.SessionBus()
	if err != nil {
		return ""
	}
	kwin := bus.Object("org.kde.KWin", "/Scripting")
	q := func(method string, args ...any) { kwin.Call(method, 0, args...) }
	// start runs whatever's loaded and not yet running. (Not Script<id>.run: KWin hands out ids
	// that loaded scripts still have, so that can reach some other script.)
	q("org.kde.kwin.Scripting.unloadScript", name)
	q("org.kde.kwin.Scripting.loadScript", js, name)
	q("org.kde.kwin.Scripting.start")
	defer q("org.kde.kwin.Scripting.unloadScript", name) // only once it's answered: it runs a moment after start
	for range 10 {
		time.Sleep(200 * time.Millisecond)
		out, _ := exec.Command("journalctl", "--user", "--since", "-1min", "-o", "cat", "--no-pager", "--grep", tag).Output()
		for _, line := range strings.Split(string(out), "\n") {
			if _, after, ok := strings.Cut(line, tag); ok {
				return after
			}
		}
	}
	return ""
}

// focusJS says "ok" once the window is active (an X11 one only becomes so a moment later).
const focusJS = `
const all = workspace.windowList().filter(w => w.normalWindow && w.caption.includes(%s));
const code = all.filter(w => /code/i.test(w.resourceClass));
const pick = (%t ? code : all.filter(w => !code.includes(w)))[0] || all[0];
if (pick) {
    pick.minimized = false;
    workspace.activeWindow = pick;
    if (workspace.activeWindow === pick) print(tag + "ok");
    else workspace.windowActivated.connect(w => { if (w === pick) print(tag + "ok"); });
}
`

const layoutJS = `
const windows = workspace.stackingOrder
    .filter(w => w.normalWindow && !w.minimized && w.desktops.includes(workspace.currentDesktop))
    .map(w => [w.frameGeometry.x, w.frameGeometry.y, w.frameGeometry.width, w.frameGeometry.height]);
const areas = workspace.screens.map(s => {
    const a = workspace.clientArea(KWin.MaximizeArea, s, workspace.currentDesktop);
    return [a.x, a.y, a.width, a.height];
});
print(tag + JSON.stringify({windows: windows, areas: areas}));
`

func kwinFocus(project string, vscode bool) bool {
	quoted, _ := json.Marshal(project)
	return kwinAsk(fmt.Sprintf(focusJS, quoted, vscode), "cchobi-focus") == "ok"
}

// kwinLayout: KWin knows each screen's usable area (the screen minus its panels), which XWayland
// can't tell her.
func kwinLayout() (windows, areas []rect) {
	var got struct{ Windows, Areas [][4]float64 } // KWin's geometry can be fractional
	json.Unmarshal([]byte(kwinAsk(layoutJS, "cchobi-layout")), &got)
	toRects := func(in [][4]float64) (out []rect) {
		for _, r := range in {
			out = append(out, rect{int(r[0]), int(r[1]), int(r[2]), int(r[3])})
		}
		return out
	}
	return toRects(got.Windows), toRects(got.Areas)
}
