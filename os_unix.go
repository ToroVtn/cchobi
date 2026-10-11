//go:build unix

package main

// What's done differently on Linux (and other Unixes) than on Windows.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// detach: the started process gets its own session, so it outlives the terminal that started it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

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

const python = "python3"
