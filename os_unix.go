//go:build unix

package main

// What's done differently on Linux (and other Unixes) than on Windows.

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// detach: the started process gets its own session, so it outlives the terminal that started it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// playing: a sound being played, by paplay.
type playing = os.Process

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

func desktop() (func() (windows, areas []rect), func(project string, vscode bool) bool) {
	switch {
	case os.Getenv("SWAYSOCK") != "":
		return swayLayout, swayFocus
	case strings.Contains(os.Getenv("XDG_CURRENT_DESKTOP"), "KDE") || os.Getenv("KDE_FULL_SESSION") != "":
		return kwinLayout, kwinFocus
	case os.Getenv("WAYLAND_DISPLAY") == "": // plain X11: any window manager that speaks EWMH
		return ewmhLayout, ewmhFocus
	}
	// another Wayland desktop: it hides other apps' windows from her
	return func() ([]rect, []rect) { return nil, nil }, func(string, bool) bool { return false }
}

// trayIcon: the tray takes a PNG as it is.
func trayIcon(png []byte) []byte { return png }

// opener: what opens a folder in the file manager.
const opener = "xdg-open"

// ---- installing ----

func dataHome() string {
	return cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local/share"))
}

func installDir() string { return filepath.Join(dataHome(), "cchobi") }

const iconExt = ".png"

func launcher() string { return filepath.Join(dataHome(), "applications", "cchobi.desktop") }

func addLauncher(exe, icon string) error {
	os.MkdirAll(filepath.Dir(launcher()), 0o755)
	return os.WriteFile(launcher(), []byte(fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=cchobi
Comment=A desktop pet for Claude Code
Exec=%q
Icon=%s
Categories=Utility;
Terminal=false
`, exe, icon)), 0o644)
}

func removeLauncher() { os.Remove(launcher()) }

func removeDir(dir string) { os.RemoveAll(dir) }

// requirements: the programs she runs that the installer can't install, and what for.
func requirements() [][2]string {
	switch {
	case os.Getenv("SWAYSOCK") != "":
		return [][2]string{{"swaymsg", "knowing where windows are, on sway"}}
	case strings.Contains(os.Getenv("XDG_CURRENT_DESKTOP"), "KDE"):
		return [][2]string{{"journalctl", "knowing where windows are, on KDE"}}
	}
	return nil
}

// showSaid: in a terminal, it's been said already.
func showSaid(failed bool) {}

// installDeps installs what plays her sounds, if it's missing, with whichever package manager
// there is (asking for sudo).
func installDeps() {
	var missing []string
	for _, p := range []string{"paplay", "pactl"} {
		if _, err := exec.LookPath(p); err != nil {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return
	}
	for _, m := range [][]string{
		{"pacman", "-S", "--needed", "--noconfirm", "libpulse"},
		{"apt-get", "install", "-y", "pulseaudio-utils"},
		{"dnf", "install", "-y", "pulseaudio-utils"},
		{"zypper", "--non-interactive", "install", "pulseaudio-utils"},
		{"apk", "add", "pulseaudio-utils"},
	} {
		if _, err := exec.LookPath(m[0]); err != nil {
			continue
		}
		if os.Geteuid() != 0 {
			m = append([]string{"sudo"}, m...)
		}
		say("Installing %s, which plays her sounds: %s", strings.Join(missing, " and "), strings.Join(m, " "))
		cmd := exec.Command(m[0], m[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			say("That didn't work (%v): she'll be quiet until %s is installed.", err, strings.Join(missing, " and "))
		}
		return
	}
	say("Missing %s, which plays her sounds: install it with your package manager (it's usually in pulseaudio-utils).",
		strings.Join(missing, " and "))
}

func freeConsole() {}
