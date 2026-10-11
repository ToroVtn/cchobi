package main

// What's done differently on Windows.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
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

// playing: a sound being played, by waveOut, a tenth of a second at a time, so its volume can
// still change (to fade it out) while it plays.
type playing struct {
	fading atomic.Bool
}

// play starts a sound and returns it, so it can be faded out early. The volume is on
// PulseAudio's cubic scale, like on Linux.
func play(file string, volume float64) *playing {
	samples, rate, err := readWav(file)
	if err != nil {
		return nil
	}
	p := &playing{}
	go p.run(samples, rate, volume*volume*volume)
	return p
}

// fadeOut turns it down over about a second, then stops it.
func fadeOut(p *playing, volume float64) { p.fading.Store(true) }

func (p *playing) run(samples []float64, rate int, gain float64) {
	winmm := syscall.NewLazyDLL("winmm.dll")
	format := struct { // WAVEFORMATEX: 16-bit mono PCM
		tag, channels      uint16
		rate, bytesPerSec  uint32
		align, bits, extra uint16
	}{1, 1, uint32(rate), uint32(rate * 2), 2, 16, 0}
	var h uintptr
	if call(winmm, "waveOutOpen", uintptr(unsafe.Pointer(&h)), 0xFFFFFFFF, uintptr(unsafe.Pointer(&format)), 0, 0, 0) != 0 {
		return // WAVE_MAPPER: whatever the default output is
	}
	defer call(winmm, "waveOutClose", h)
	defer call(winmm, "waveOutReset", h)
	type header struct { // WAVEHDR
		data             *byte
		length, recorded uint32
		user             uintptr
		flags, loops     uint32
		next, reserved   uintptr
	}
	const done, size = 1, uint32(unsafe.Sizeof(header{}))
	var queue [3]*header // what's been handed to waveOut: it holds on to them until done
	var bufs [3][]byte
	wait := func(i int) {
		if hd := queue[i]; hd != nil {
			for hd.flags&done == 0 {
				time.Sleep(10 * time.Millisecond)
			}
			call(winmm, "waveOutUnprepareHeader", h, uintptr(unsafe.Pointer(hd)), uintptr(size))
		}
	}
	chunk, fade := rate/10, 10 // a tenth of a second; fading takes ten of them
	for i, at := 0, 0; at < len(samples); i, at = i+1, at+chunk {
		if p.fading.Load() {
			if fade--; fade <= 0 {
				return
			}
			gain *= float64(fade) / float64(fade+1) // straight down to 0 over the ten
		}
		part := slices.Clone(samples[at:min(at+chunk, len(samples))])
		for j := range part {
			part[j] *= gain
		}
		slot := i % len(queue)
		wait(slot)
		bufs[slot] = pcm16(part)
		queue[slot] = &header{data: &bufs[slot][0], length: uint32(len(bufs[slot]))}
		call(winmm, "waveOutPrepareHeader", h, uintptr(unsafe.Pointer(queue[slot])), uintptr(size))
		call(winmm, "waveOutWrite", h, uintptr(unsafe.Pointer(queue[slot])), uintptr(size))
	}
	for i := range queue {
		wait(i)
	}
}

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

// opener: what opens a folder in the file manager.
const opener = "explorer"

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
	if err := hidden(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)).Run(); err != nil {
		return err
	}
	if _, err := os.Stat(launcher()); err != nil { // PowerShell can say it went fine when it didn't
		return err
	}
	// and in Settings > Apps, to uninstall her from there
	for _, v := range [][3]string{{"DisplayName", "REG_SZ", "cchobi"}, {"DisplayIcon", "REG_SZ", icon},
		{"UninstallString", "REG_SZ", fmt.Sprintf("%q uninstall", exe)}, {"InstallLocation", "REG_SZ", filepath.Dir(exe)},
		{"Publisher", "REG_SZ", "ToroVtn"}, {"NoModify", "REG_DWORD", "1"}, {"NoRepair", "REG_DWORD", "1"}} {
		hidden(exec.Command("reg", "add", uninstallKey, "/v", v[0], "/t", v[1], "/d", v[2], "/f")).Run()
	}
	return nil
}

const uninstallKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\cchobi`

func removeLauncher() {
	os.Remove(launcher())
	hidden(exec.Command("reg", "delete", uninstallKey, "/f")).Run()
}

// hidden: no console window flashing up for it (she has none of her own to lend it).
func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	return cmd
}

// showSaid: double-clicked (cchobi-setup.exe, or uninstalling from Settings > Apps), there's no
// terminal that showed what install or uninstall said: a message box does.
func showSaid(failed bool) {
	if _, err := os.Stdout.Stat(); err == nil {
		return
	}
	icon := uintptr(0x40) // MB_ICONINFORMATION
	if failed {
		icon = 0x10 // MB_ICONERROR
	}
	text, _ := syscall.UTF16PtrFromString(said.String())
	call(user32, "MessageBoxW", 0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(class)), icon)
}

// installDeps: nothing to install, on Windows.
func installDeps() {}

// removeLater removes a file or folder. A running program can't delete itself on Windows, so if
// that's what it is (or holds), a detached cmd deletes it once this one has exited.
func removeLater(path string) {
	if os.RemoveAll(path) != nil {
		cmd := exec.Command("cmd")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNoWindow,
			CmdLine: fmt.Sprintf(`cmd /c "ping -n 3 127.0.0.1 >nul & rmdir /s /q "%[1]s" 2>nul & del /f /q "%[1]s" 2>nul"`, path)}
		cmd.Start()
	}
}

// requirements: the programs she runs, and what for.
func requirements() [][2]string { return nil }

// freeConsole: built as a console program (go install does that), she'd keep a console window
// open while she runs.
func freeConsole() { call(kernel, "FreeConsole") }
