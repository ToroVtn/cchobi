package main

// What's done differently on Windows.

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	detachedProcess = 0x00000008 // DETACHED_PROCESS, which syscall doesn't name
	createNoWindow  = 0x08000000 // CREATE_NO_WINDOW: no console window flashing up
)

// detach: the started process gets no console and its own process group, so it outlives the
// terminal that started it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
}

// play starts a sound and returns its player, so it can be stopped early. The volume is on
// PulseAudio's cubic scale, like on Linux; ffplay's is linear, 0..100. ffplay comes with ffmpeg,
// which she needs anyway.
func play(file string, volume float64) *os.Process {
	cmd := exec.Command("ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet",
		"-volume", fmt.Sprint(int(math.Round(math.Pow(volume, 3)*100))), file)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if cmd.Start() != nil {
		return nil
	}
	go cmd.Wait()
	return cmd.Process
}

// fadeOut stops a playing sound.
// ponytail: cuts it off, as ffplay can't change its volume once it's playing; an in-process
// player (oto, say) if the cut is audible.
func fadeOut(proc *os.Process, volume float64) { proc.Kill() }

func desktop() (func() (windows, areas []rect), func(project string, vscode bool) bool) {
	return winLayout, winFocus
}

// trayIcon: the tray wants an .ico, which can hold a PNG as it is behind a 22-byte header.
func trayIcon(png []byte) []byte {
	w, h := binary.BigEndian.Uint32(png[16:]), binary.BigEndian.Uint32(png[20:]) // from the PNG's IHDR
	ico := []byte{0, 0, 1, 0, 1, 0, byte(w), byte(h), 0, 0, 1, 0, 32, 0}         // byte(256) is 0, which means 256
	ico = binary.LittleEndian.AppendUint32(ico, uint32(len(png)))
	ico = binary.LittleEndian.AppendUint32(ico, 22)
	return append(ico, png...)
}

const python = "python" // what the installer from python.org puts on PATH

// ---- installing ----

func installDir() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "cchobi") }

const iconExt = ".ico"

func launcher() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\cchobi.lnk`)
}

// addLauncher: a Start menu shortcut, made through PowerShell (a .lnk is a COM object's file).
func addLauncher(exe, icon string) error {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := fmt.Sprintf(`$s = (New-Object -ComObject WScript.Shell).CreateShortcut(%s); $s.TargetPath = %s; `+
		`$s.IconLocation = %s; $s.Description = 'A desktop pet for Claude Code'; $s.Save()`, q(launcher()), q(exe), q(icon))
	if err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run(); err != nil {
		return err
	}
	_, err := os.Stat(launcher()) // PowerShell can say it went fine when it didn't
	return err
}

func removeLauncher() { os.Remove(launcher()) }

// removeDir: a running program can't delete itself on Windows, so if that's what's in dir, a
// detached cmd deletes it once this one has exited.
func removeDir(dir string) {
	if os.RemoveAll(dir) != nil {
		cmd := exec.Command("cmd", "/c", "ping -n 3 127.0.0.1 >nul & rmdir /s /q", dir)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNoWindow}
		cmd.Start()
	}
}

// requirements: the programs she runs, and what for.
func requirements() [][2]string {
	return [][2]string{{"ffmpeg", "her sounds"}, {"ffplay", "playing her sounds"}, {python, "the Settings window"}}
}
