// cchobi: a Claude Code desktop pet. A black cat who walks around while Claude works, and sits
// and meows when it's done or needs you. When nothing's going on she lives her own life: naps on
// her cushion, loafs, grooms, sunbathes, perches on top of windows, rubs against their sides,
// bothers her plant.
//
//	cchobi        run the pet (the SessionStart hook also starts it)
//	cchobi hook   what Claude Code hooks call: forwards the hook's JSON to the pet
//
// Mouse: click her to pet her (that also dismisses her message), drag to carry her, hover and she
// rubs against the pointer, right-click for a treat. Click her speech bubble to jump to that
// session's tab; right-click the bubble to read more of the message.
//
// Build: go build (sprites: python3 sprites.py, after changing a drawing). Settings is a separate
// little window (settings.py) that only runs while it's open.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fyne.io/systray"
)

var (
	home = os.Getenv("HOME")
	here = func() string {
		exe, _ := os.Executable()
		exe, _ = filepath.EvalSymlinks(exe)
		return filepath.Dir(exe)
	}()
	runtime_ = envOr("XDG_RUNTIME_DIR", "/tmp")
	sock     = runtime_ + "/cchobi.sock"
	data     = home + "/.cchobi"    // her sounds; change them from Settings, or by hand
	stateF   = data + "/state.json" // where her things are, her plant's state, which sounds are off
	defaults = here + "/sounds"     // her default sounds: copied into ~/.cchobi the first time
	kinds    = []string{"meows", "purrs", "water"}
	// Each sound is also kept at a few speeds (faster is also higher), made once with ffmpeg into
	// <kind>/variants/, so she doesn't sound the same every time. Meows play as they are.
	speeds = map[string][]float64{"purrs": {0.95, 1.0, 1.05}, "water": {0.9, 0.95, 1.0, 1.06, 1.12}}
)

// purrFade: seconds each purr variant fades in and out (on an S-curve), so one purr can crossfade
// softly into the next.
const purrFade = 1.2

// fadeFor: how long a purr this long fades in and out. Short ones fade for a third of it at most.
func fadeFor(seconds float64) float64 { return min(purrFade, seconds/3) }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(s), 64) }

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		hook()
		return
	}
	if c, err := net.Dial("unix", sock); err == nil { // already running? then leave it be
		c.Close()
		return
	}
	debug.SetGCPercent(50) // she's tiny: keep the heap close to what's actually in use
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cchobi:", err)
		os.Exit(1)
	}
}

// hook runs on every Claude Code hook, so it stays tiny and never fails loudly.
func hook() {
	var event map[string]any
	if json.NewDecoder(os.Stdin).Decode(&event) != nil {
		return
	}
	event["vscode"] = os.Getenv("VSCODE_PID") != "" || os.Getenv("TERM_PROGRAM") == "vscode"
	line, _ := json.Marshal(event)
	c, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
	if err != nil {
		if event["hook_event_name"] == "SessionStart" { // pet isn't running yet: start it
			exe, _ := os.Executable()
			cmd := exec.Command(exe)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			cmd.Start()
		}
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(500 * time.Millisecond))
	c.Write(append(line, '\n'))
}

// ---- the main loop: everything that touches her happens on it, one thing at a time ----

var calls = make(chan func(), 256)

// later runs f on the main loop after d.
func later(d time.Duration, f func()) { time.AfterFunc(d, func() { calls <- f }) }

func run() error {
	if err := openX(); err != nil {
		return err
	}
	sheet, err := loadSheet()
	if err != nil {
		return fmt.Errorf("sprites: %w", err)
	}
	if err := loadFont(); err != nil {
		return fmt.Errorf("font: %w", err)
	}
	for _, k := range kinds { // first time here: start her off with the default sounds
		if _, err := os.Stat(data + "/" + k); err != nil {
			os.MkdirAll(data+"/"+k, 0o755)
			files, _ := filepath.Glob(defaults + "/" + k + "/*")
			for _, f := range files {
				if b, err := os.ReadFile(f); err == nil {
					os.WriteFile(data+"/"+k+"/"+filepath.Base(f), b, 0o644)
				}
			}
		}
	}
	var state struct {
		Cushion *[2]int             `json:"cushion"`
		Plant   *[2]int             `json:"plant"`
		Garden  *Garden             `json:"garden"`
		Muted   map[string][]string `json:"muted"`
	}
	if b, err := os.ReadFile(stateF); err == nil {
		json.Unmarshal(b, &state)
	}
	if state.Muted == nil {
		state.Muted = map[string][]string{}
	}
	pet := newPet(sheet, newGarden(state.Garden, len(sheet.PlantLeaves), sheet.DryOrder, unixNow()), state.Muted)
	pet.windows, workAreas = layout()
	m := monitors()[0]
	g := workArea(m.x+m.w/2, m.y+m.h/2)
	put := func(w *Win, saved *[2]int, x, y int) {
		if saved != nil {
			if _, ok := screenAt(saved[0], saved[1]); ok { // that screen may be gone
				x, y = saved[0], saved[1]
			}
		}
		w.move(x, y)
	}
	c, p := pet.cushion.Win, pet.plant.Win
	put(c, state.Cushion, g.x+g.w-c.w-40, g.y+g.h-c.h-10)
	put(p, state.Plant, c.x-p.w-30, c.y+c.h-p.h)
	pet.start()
	pet.save()

	os.Remove(sock) // left over from a run that was killed (main already checked nobody's answering on it)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer os.Remove(sock)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				lines := bufio.NewScanner(conn)
				lines.Buffer(nil, 4<<20)
				for lines.Scan() {
					var e map[string]any
					if json.Unmarshal(lines.Bytes(), &e) == nil {
						calls <- func() { pet.handle(e) }
					}
				}
			}()
		}
	}()

	// not as onReady: systray runs that on a goroutine racing its own D-Bus setup, and an icon set
	// mid-setup is silently dropped (Plasma then shows an empty 0x0 pixmap)
	trayStart, trayEnd := systray.RunWithExternalLoop(nil, nil)
	systray.SetOnTapped(func() { calls <- pet.card.popup })
	trayStart()
	defer trayEnd()
	tray(pet)
	go scanSounds(func(s *Sounds) { calls <- func() { pet.sounds = s } })
	later(10*time.Second, debug.FreeOSMemory) // what loading her took, back to the system once she's settled

	events := make(chan func())
	go func() {
		for {
			ev, err := X.WaitForEvent()
			if ev == nil && err == nil {
				events <- func() { panic("lost the X connection") }
				return
			}
			if ev != nil {
				events <- func() { dispatch(ev) }
			}
		}
	}()
	tick := time.NewTicker(tickMS * time.Millisecond)
	for {
		select {
		case f := <-events:
			f()
		case f := <-calls:
			if f == nil {
				return nil
			}
			f()
		case <-tick.C:
			pet.step()
		}
	}
}

func tray(pet *Pet) {
	icon, _ := pngOf(pet.sheet.get("sit"), 2)
	systray.SetIcon(icon)
	systray.SetTitle("cchobi")
	systray.SetTooltip("cchobi")
	items := []struct {
		title string
		do    func()
	}{{"Give her a treat", pet.treat}, {"Water the plant", pet.water}, {"Throw away dry leaves", pet.throwAway},
		{"", nil}, {"Settings…", openSettings}, {"Quit", func() { calls <- nil }}}
	for _, it := range items {
		if it.do == nil {
			systray.AddSeparator()
			continue
		}
		item, do := systray.AddMenuItem(it.title, ""), it.do
		go func() {
			for range item.ClickedCh {
				calls <- do
			}
		}()
	}
}

var settingsProc *os.Process

// openSettings: the sounds window, a separate small program that only runs while it's open.
func openSettings() {
	if settingsProc != nil {
		return
	}
	cmd := exec.Command("python3", here+"/settings.py")
	if cmd.Start() == nil {
		settingsProc = cmd.Process
		go func() { cmd.Wait(); calls <- func() { settingsProc = nil } }()
	}
}

func unixNow() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// ---- sounds ----

type Sound struct {
	file    string
	seconds float64
	name    string // the file it came from, as Settings lists it
}

// Sounds: what she can play, each kind with its variants.
type Sounds map[string][]Sound

// scanSounds looks through her sound folders (in the background: it may run ffmpeg) and makes
// any missing variants. Anything that isn't a .wav in meows/ (a cropped video, say) is
// converted once, with the low rumble filtered out.
func scanSounds(done func(*Sounds)) {
	out := Sounds{}
	for _, kind := range kinds {
		dir := data + "/" + kind
		files, _ := filepath.Glob(dir + "/*")
		for _, f := range files {
			ext := strings.ToLower(filepath.Ext(f))
			wav := strings.TrimSuffix(f, filepath.Ext(f)) + ".wav"
			if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() && kind == "meows" && ext != ".wav" && !exists(wav) {
				exec.Command("ffmpeg", "-v", "error", "-i", f, "-vn", "-ac", "1", "-af", "highpass=f=200", wav).Run()
			}
		}
		wavs, _ := filepath.Glob(dir + "/*.wav")
		slices.Sort(wavs)
		keep := map[string]bool{}
		for _, f := range wavs {
			name := filepath.Base(f)
			if speeds[kind] == nil {
				out[kind] = append(out[kind], Sound{f, wavSeconds(f), name})
				continue
			}
			for _, speed := range speeds[kind] {
				tag := fmt.Sprintf("%g", speed)
				if kind == "purrs" { // the fade is in the name, so changing it remakes them
					tag += fmt.Sprintf("-fade%g", purrFade)
				}
				v := fmt.Sprintf("%s/variants/%s-%s.wav", dir, strings.TrimSuffix(name, ".wav"), tag)
				keep[v] = true
				if newer(f, v) {
					os.MkdirAll(dir+"/variants", 0o755)
					rate, seconds := wavInfo(f)
					af := fmt.Sprintf("asetrate=%.0f,aresample=%d", float64(rate)*speed, rate)
					if kind == "purrs" {
						d := fadeFor(seconds)
						af = fmt.Sprintf("afade=t=in:d=%.3f:curve=hsin,afade=t=out:st=%.3f:d=%.3f:curve=hsin,",
							d, seconds-d, d) + af
					}
					exec.Command("ffmpeg", "-v", "error", "-y", "-i", f, "-ac", "1", "-af", af, v).Run()
				}
				out[kind] = append(out[kind], Sound{v, wavSeconds(v), name})
			}
		}
		old, _ := filepath.Glob(dir + "/variants/*.wav") // variants of sounds you've removed
		for _, v := range old {
			if !keep[v] {
				os.Remove(v)
			}
		}
	}
	done(&out)
}

func exists(f string) bool { _, err := os.Stat(f); return err == nil }

// newer: src has changed since out was made (or out doesn't exist yet).
func newer(src, out string) bool {
	a, err1 := os.Stat(src)
	b, err2 := os.Stat(out)
	return err1 == nil && (err2 != nil || a.ModTime().After(b.ModTime()))
}

// wavInfo reads a .wav's header: sample rate and how long it plays.
func wavInfo(f string) (rate int, seconds float64) {
	fh, err := os.Open(f)
	if err != nil {
		return 44100, 0
	}
	defer fh.Close()
	var riff [12]byte
	if _, err := io.ReadFull(fh, riff[:]); err != nil {
		return 44100, 0
	}
	byteRate := 0
	for {
		var head [8]byte
		if _, err := io.ReadFull(fh, head[:]); err != nil {
			return
		}
		id, size := string(head[:4]), int64(binary.LittleEndian.Uint32(head[4:]))
		switch id {
		case "fmt ":
			var fmtc [16]byte
			io.ReadFull(fh, fmtc[:])
			rate = int(binary.LittleEndian.Uint32(fmtc[4:]))
			byteRate = int(binary.LittleEndian.Uint32(fmtc[8:]))
			fh.Seek(size-16+size%2, io.SeekCurrent)
		case "data":
			if byteRate > 0 {
				seconds = float64(size) / float64(byteRate)
			}
			return
		default:
			fh.Seek(size+size%2, io.SeekCurrent)
		}
	}
}

func wavSeconds(f string) float64 { _, s := wavInfo(f); return s }

// play a sound at a volume (0..1 on PulseAudio's scale, which is cubic: 0.8 is about -6 dB).
// play starts a sound and returns its player, so it can be faded out early.
func play(file string, volume float64) *os.Process {
	cmd := exec.Command("paplay", fmt.Sprintf("--volume=%d", int(volume*65536)), file)
	if cmd.Start() != nil {
		return nil
	}
	go cmd.Wait()
	return cmd.Process
}

// fadeOut turns a playing sound down over about a second, then stops it. paplay can't change
// its volume once it's playing, so this goes through pactl, finding its stream by its pid.
func fadeOut(proc *os.Process, volume float64) {
	go func() {
		defer proc.Kill()
		out, _ := exec.Command("pactl", "-f", "json", "list", "sink-inputs").Output()
		var streams []struct {
			Index      int               `json:"index"`
			Properties map[string]string `json:"properties"`
		}
		json.Unmarshal(out, &streams)
		for _, s := range streams {
			if s.Properties["application.process.id"] == strconv.Itoa(proc.Pid) {
				for i := 19; i >= 0; i-- {
					exec.Command("pactl", "set-sink-input-volume", strconv.Itoa(s.Index),
						fmt.Sprintf("%.1f%%", volume*100*float64(i)/20)).Run()
					time.Sleep(50 * time.Millisecond)
				}
			}
		}
	}()
}

func uniform(a, b float64) float64 { return a + rand.Float64()*(b-a) }

// ---- finding and focusing windows, through KWin ----

// kwin runs a KWin script. It lives inside the compositor, so unlike an app on Wayland it can see
// and focus any window.
func kwin(source, name string) {
	js := runtime_ + "/" + name + ".js"
	os.WriteFile(js, []byte(source), 0o644)
	q := func(args ...string) string {
		out, _ := exec.Command("qdbus6", append([]string{"org.kde.KWin"}, args...)...).Output()
		return strings.TrimSpace(string(out))
	}
	q("/Scripting", "org.kde.kwin.Scripting.unloadScript", name)
	id := q("/Scripting", "org.kde.kwin.Scripting.loadScript", js, name)
	q("/Scripting/Script"+id, "org.kde.kwin.Script.run")
	q("/Scripting", "org.kde.kwin.Scripting.unloadScript", name)
}

const focusJS = `
const all = workspace.windowList().filter(w => w.normalWindow && w.caption.includes(%s));
const code = all.filter(w => /code/i.test(w.resourceClass));
const pick = (%t ? code : all.filter(w => !code.includes(w)))[0] || all[0];
if (pick) { pick.minimized = false; workspace.activeWindow = pick; }
`

const layoutJS = `
const windows = workspace.stackingOrder
    .filter(w => w.normalWindow && !w.minimized && w.desktops.includes(workspace.currentDesktop))
    .map(w => [w.frameGeometry.x, w.frameGeometry.y, w.frameGeometry.width, w.frameGeometry.height]);
const areas = workspace.screens.map(s => {
    const a = workspace.clientArea(KWin.MaximizeArea, s, workspace.currentDesktop);
    return [a.x, a.y, a.width, a.height];
});
print("%s" + JSON.stringify({windows: windows, areas: areas}));
`

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
	project, _ := json.Marshal(p.project)
	kwin(fmt.Sprintf(focusJS, project, p.vscode), "cchobi-focus")
	if p.vscode && p.session != "" {
		// The Claude extension opens a session's tab from a URL; it lands in the window just focused.
		url := urlScheme() + "://anthropic.claude-code/open?session=" + p.session
		time.Sleep(300 * time.Millisecond)
		exec.Command("code", "--open-url", url).Start()
	}
}

// layout asks KWin where the open windows are, bottom to top (for her to perch on and rub
// against), and each screen's usable area: the screen minus its panels, which XWayland can't
// tell her.
func layout() (windows, areas []rect) {
	tag := fmt.Sprintf("cchobi-%x ", rand.Uint32())
	kwin(fmt.Sprintf(layoutJS, tag), "cchobi-layout")
	for range 10 { // a KWin script can only answer by printing to the journal
		time.Sleep(200 * time.Millisecond)
		out, _ := exec.Command("journalctl", "--user", "--since", "-1min", "-o", "cat", "--no-pager", "--grep", tag).Output()
		for _, line := range strings.Split(string(out), "\n") {
			if _, after, ok := strings.Cut(line, tag); ok {
				var got struct{ Windows, Areas [][4]float64 } // KWin's geometry can be fractional
				json.Unmarshal([]byte(after), &got)
				toRects := func(in [][4]float64) (out []rect) {
					for _, r := range in {
						out = append(out, rect{int(r[0]), int(r[1]), int(r[2]), int(r[3])})
					}
					return out
				}
				return toRects(got.Windows), toRects(got.Areas)
			}
		}
	}
	return nil, nil
}

// lastReply: the last thing Claude said or tried to do in a session, read from its transcript.
func lastReply(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if end, err := f.Seek(0, io.SeekEnd); err == nil {
		f.Seek(max(0, end-400_000), io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Type    string
			Message struct {
				Content []struct {
					Type, Text, Name string
					Input            json.RawMessage
				}
			}
		}
		if json.Unmarshal([]byte(lines[i]), &entry) != nil || entry.Type != "assistant" {
			continue
		}
		var parts []string
		for _, block := range entry.Message.Content {
			switch block.Type {
			case "text":
				parts = append(parts, block.Text)
			case "tool_use":
				parts = append(parts, fmt.Sprintf("[%s] %s", block.Name, cut(string(block.Input), 500)))
			}
		}
		if len(parts) > 0 {
			return cut(strings.Join(parts, "\n"), 1500)
		}
	}
	return ""
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
