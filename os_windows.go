package main

// What's done differently on Windows.

import (
	"fmt"
	"math"
	"os"
	"os/exec"
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
