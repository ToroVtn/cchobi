package main

// cchobi install / uninstall: puts her (one binary: everything else is built in) where she lives, adds her to the app menu, and hooks her up to Claude Code. What's
// different per OS (where, and the menu entry) is in os_unix.go / os_windows.go.

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var hookEvents = []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Notification", "Stop", "SessionEnd"}

func claudeSettings() string {
	return filepath.Join(cmp.Or(os.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude")), "settings.json")
}

func install() error {
	dir := installDir()
	exe, _ := os.Executable()
	target := filepath.Join(dir, "cchobi"+filepath.Ext(exe)) // not cchobi-setup.exe, cchobi-linux-amd64...
	stopRunning()
	installDeps()
	if here != dir {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := copyFile(exe, target, 0o755); err != nil {
			return err
		}
	}
	sheet, err := loadSheet()
	if err != nil {
		return err
	}
	png, _ := pngOf(sheet.get("sit"), 4)
	icon := filepath.Join(dir, "icon"+iconExt)
	os.WriteFile(icon, trayIcon(png), 0o644)
	if err := addLauncher(target, icon); err != nil {
		say("Couldn't add her to the app menu: %v", err)
	}
	if err := editHooks(fmt.Sprintf("%q hook", filepath.ToSlash(target))); err != nil {
		return fmt.Errorf("hooking her up to Claude Code: %w", err)
	}
	say("Installed in %s, added to the app menu, and hooked up to Claude Code (%s).", dir, claudeSettings())
	for _, r := range requirements() {
		if _, err := exec.LookPath(r[0]); err != nil {
			say("Missing %s: needed for %s.", r[0], r[1])
		}
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		say("Optional: ffmpeg, to give her sounds that aren't .wav (an .mp3, a video...).")
	}
	cmd := exec.Command(target)
	detach(cmd)
	if cmd.Start() == nil {
		say("She's starting. From now on, starting a Claude Code session also starts her.")
	}
	return nil
}

func uninstall() error {
	stopRunning()
	if err := editHooks(""); err != nil {
		return fmt.Errorf("unhooking her from Claude Code: %w", err)
	}
	removeLauncher()
	removeLater(installDir())
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	if bin := goBin(); bin != "" && strings.EqualFold(filepath.Dir(exe), bin) {
		removeLater(exe) // what go install put there: nothing else would take it out
	}
	say("Uninstalled. Her sounds and state are still in %s; delete that too to forget her completely.", data)
	return nil
}

// goBin: where go install puts programs, if Go's here.
func goBin() string {
	out, err := exec.Command("go", "env", "GOBIN", "GOPATH").Output()
	if err != nil {
		return ""
	}
	gobin, gopath, _ := strings.Cut(string(out), "\n") // GOBIN is usually empty: its line too
	if gobin = strings.TrimSpace(gobin); gobin != "" {
		return filepath.Clean(gobin)
	}
	return filepath.Join(filepath.SplitList(strings.TrimSpace(gopath))[0], "bin")
}

// said: everything install or uninstall has said, for a message box when there's no terminal to
// show it (cchobi-setup.exe, double-clicked).
var said strings.Builder

func say(format string, a ...any) {
	s := fmt.Sprintf(format, a...) + "\n"
	fmt.Print(s)
	said.WriteString(s)
}

// stopRunning asks a running pet to quit, and waits until she has.
func stopRunning() {
	c, err := net.Dial("unix", sock)
	if err != nil {
		return
	}
	c.Write([]byte(`{"cchobi":"quit"}` + "\n"))
	c.Close()
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		if c, err := net.Dial("unix", sock); err != nil {
			return
		} else {
			c.Close()
		}
	}
}

// copyFile writes next to dst and renames it over, so a binary that's still running isn't
// overwritten in place. Windows may hold on to a just-quit binary for a moment: retry.
func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst+".new", b, mode); err != nil {
		return err
	}
	for range 20 {
		if err = os.Rename(dst+".new", dst); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}

// editHooks takes cchobi out of Claude Code's hooks, and puts command in for each event (unless
// it's ""). The old settings are kept next to it, as settings.json.bak.
func editHooks(command string) error {
	f := claudeSettings()
	old, err := os.ReadFile(f)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := withHooks(old, command)
	if err != nil {
		return err
	}
	if old != nil {
		os.WriteFile(f+".bak", old, 0o644)
	}
	os.MkdirAll(filepath.Dir(f), 0o755)
	return os.WriteFile(f, out, 0o644)
}

// withHooks: settings (JSON) with any cchobi hook ("... cchobi hook", wherever it's installed)
// replaced by command, or just taken out if command is "". Everything else is kept.
// ponytail: rewriting reorders the top-level keys alphabetically (the content's the same).
func withHooks(settings []byte, command string) ([]byte, error) {
	s := map[string]any{}
	if len(bytes.TrimSpace(settings)) > 0 {
		if err := json.Unmarshal(settings, &s); err != nil {
			return nil, fmt.Errorf("can't read it: %w", err)
		}
	}
	hooks, _ := s["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for event, v := range hooks {
		groups, _ := v.([]any)
		var kept []any
		for _, g := range groups {
			group, _ := g.(map[string]any)
			list, _ := group["hooks"].([]any)
			var others []any
			for _, h := range list {
				m, _ := h.(map[string]any)
				c, _ := m["command"].(string)
				if !(strings.Contains(c, "cchobi") && strings.HasSuffix(strings.TrimSpace(c), " hook")) {
					others = append(others, h)
				}
			}
			if len(others) > 0 || len(list) == 0 {
				group["hooks"] = others
				kept = append(kept, group)
			}
		}
		hooks[event] = kept
		if len(kept) == 0 {
			delete(hooks, event)
		}
	}
	if command != "" {
		for _, event := range hookEvents {
			groups, _ := hooks[event].([]any)
			hooks[event] = append(groups, map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 5}}})
		}
	}
	if len(hooks) > 0 {
		s["hooks"] = hooks
	} else {
		delete(s, "hooks")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false) // keep a command's < > & as they were
	enc.SetIndent("", "  ")
	err := enc.Encode(s)
	return out.Bytes(), err
}
