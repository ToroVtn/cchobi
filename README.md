# cchobi

A desktop pet for Claude Code: a little black tuxedo cat, drawn in pixel art in the style of the game *Replaced*.

- **She lives her own life**: wanders around your screen, loafs, grooms, sunbathes, perches on top of windows, rubs against their sides, and bothers her plant. When Claude isn't working she also naps on her cushion.
- **When Claude finishes or needs you**, she stops, meows and shows what it said in a speech bubble.

She uses about 25 MB of memory, and runs on Linux and Windows.

## Install

**Linux**, in a terminal:

```sh
curl -fsSL https://github.com/ToroVtn/cchobi/releases/latest/download/install.sh | sh
```

**Windows:** download `cchobi-setup.exe` from the [latest release](https://github.com/ToroVtn/cchobi/releases/latest) and double-click it. It isn't code-signed, so Windows may say it's from an unknown publisher: *More info*, then *Run anyway*. Or, in PowerShell:

```powershell
irm https://github.com/ToroVtn/cchobi/releases/latest/download/install.ps1 | iex
```

**With Go**, anywhere: `go install github.com/ToroVtn/cchobi@latest && cchobi install`

Each of these:
- puts her in `~/.local/share/cchobi` (Windows: `%LOCALAPPDATA%\cchobi`)
- adds her to the app menu (Windows: the Start menu, and Settings > Apps, where you can uninstall her)
- hooks her up to Claude Code in `~/.claude/settings.json`, keeping the rest of it (the old file is saved as `settings.json.bak`)
- on Linux, installs what plays her sounds (`paplay`, `pactl`) if it's missing, with your package manager
- starts her. From then on, starting a Claude Code session also starts her.

Run the same again to update her.

### Uninstall

| Installed with | Uninstall with |
|---|---|
| the Linux script | `~/.local/share/cchobi/cchobi uninstall` |
| Windows | Settings > Apps > cchobi > Uninstall, or in PowerShell: `& "$env:LOCALAPPDATA\cchobi\cchobi.exe" uninstall` |
| Go | `cchobi uninstall` (it also removes itself from `~/go/bin`) |

That stops her, takes her hooks out of Claude Code's settings (leaving the rest), and removes her and her menu entry. Her sounds and state in `~/.cchobi` stay; delete that folder too to forget her completely.

## What she does

| You… | She… |
|---|---|
| click her | purrs, and hearts float up. This also dismisses her message. |
| drag her | hangs from your hand by her belly |
| hover near her | rubs her cheek against the pointer |
| right-click her | gets a treat, then licks her lips |
| click her speech bubble | takes you to the session that called: focuses its window and, in VS Code, opens its Claude tab |
| right-click the bubble | shows more of the message: Claude's last reply |

### Her cushion

She sleeps on her cushion, which squishes when she lands. You can drag it anywhere.

### Her plant

- **Watering:** click the plant (left or right) to water it. A watering can pours for as long as its sound plays. You can also drag the plant around.
- **Drying:** about every 4 hours (± 30 min, at random) the plant checks on itself:
  - If it wasn't watered in the 2 hours before the check, its oldest green leaf dries.
  - If it was watered, a lost leaf grows back.
  - So one watering isn't enough: water it every 2 hours or so to keep it healthy.
- **Falling:** dry leaves fall off 1–4 hours after drying. Half of them land in the pot.
- **Picking:** you can pull a dry leaf off by dragging it. Let go and it glides down, and it can land in the pot.
- **Eating:** she eats dry leaves from the plant, the pot or the floor.

The plant keeps aging while she isn't running. Its state, like where her things are, is saved in `~/.cchobi/state.json`.

### Tray icon

- **Hover** to see when the plant was last watered (💧) and roughly when it next checks itself (🍂).
- **Click** for a card with the same information.
- **Right-click** for the menu: Give her a treat, Water the plant, Throw away dry leaves, Settings…, Quit.

## Sounds

Her sounds live in `~/.cchobi/`. The first time she runs, she puts her default sounds there (they're built into her).

| Folder | Plays |
|---|---|
| `meows/` | when she needs you (one at random) |
| `purrs/` | while she's petted or rubbing against the pointer |
| `water/` | when the plant is watered |

**Settings…** in the tray menu opens her sounds panel:
- switch each sound on or off
- listen to it (▶)
- remove it (✕). Removed sounds go to `~/.cchobi/removed/`, so nothing is lost.
- **Open folder** to add new ones: drop them into the right folder and they show up in the panel. A `.wav` just works. Anything else (an `.mp3`, a video: only the audio is kept) is converted if you have ffmpeg.
- **Reset** to her default sounds

**Variations:** so purrs and watering don't sound the same every time, each one is also kept at a few speeds (faster is also higher). These copies are made once into `variants/` inside each folder, and remade when you change a sound.

**Cleaning recordings:** `tools/clean_meows.py` cleans recordings of your own cat: it removes background noise and voices, keeping only the meow. `python3 tools/clean_meows.py recordings/*.wav` writes the results to `~/.cchobi/meows/`. It needs numpy.

## Building from source

### Requirements

| What | Needed for |
|---|---|
| Linux with X11, or Wayland with XWayland; or Windows 10 (1803) or later | Showing her |
| One of the desktops below | Knowing where windows and panels are (to perch on and rub against), and focusing a session's window. Elsewhere (GNOME, niri, Hyprland for now) she still works, just without those. |
| &nbsp;&nbsp;KDE Plasma 6 | Scripts run in KWin over D-Bus, read back with `journalctl` |
| &nbsp;&nbsp;sway | `swaymsg` |
| &nbsp;&nbsp;any X11 window manager that speaks EWMH (Xfce, Openbox, i3...) | Nothing extra |
| &nbsp;&nbsp;Windows | Nothing extra |
| `paplay` and `pactl` (PulseAudio or PipeWire) | Her sounds, on Linux. The installer installs them if they're missing. |
| Go | Building her |
| ffmpeg (optional) | Sounds you add that aren't `.wav` |
| fontconfig (optional) | Finding your KDE fixed-width font (without it: Go Mono) |
| Python 3 | Drawing the sprites (only when changing them) |

### Install from a checkout

`./install.sh` (Windows: `install.cmd`) builds her and runs `cchobi install`, as above.

### Build by hand

```sh
python3 sprites.py   # redraws assets/sprites.png + sprites.json (only needed after changing a drawing)
go build -ldflags="-s -w" -o cchobi .
go test              # checks the plant's rules, her reach, reading sway, the installer's hook editing, sound variants
```

On Windows, build it without a console window:

```sh
go build -ldflags="-s -w -H=windowsgui" -o cchobi.exe .
```

Everything she needs is built into the binary: the sprite sheet and her default sounds.

### Hook her up to Claude Code by hand

The installer does this for you.

Add the hook to `~/.claude/settings.json` for each of these events: `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `Notification`, `Stop`, `SessionEnd`. Here it is for one:

```json
"hooks": {
  "SessionStart": [
    { "hooks": [ { "type": "command", "command": "/path/to/cchobi hook", "timeout": 5 } ] }
  ],
  "UserPromptSubmit": [ ...same... ],
  ...
}
```

`SessionStart` starts her if she isn't running. You can also start her from the app menu, or by hand with `./cchobi`. She shows up as `cchobi` in top/btop.

## How it fits together

| File | What it is |
|---|---|
| `main.go` | Starting up, the hook, saving state, sounds, the tray |
| `settings.go` | The Settings panel (her sounds), drawn like the status card |
| `audio.go` | Reading and writing `.wav` files, and making a sound's faster and slower variants |
| `cat.go` | Her behaviour each tick (50 ms), her things (cushion, plant, leaves, watering can, bin), her speech bubble, the status card |
| `garden.go` | The plant's rules, with no drawing. `garden_test.go` checks them; `cat_test.go` checks which window edges she can reach. |
| `x11.go` | Her windows, directly on X11, with no toolkit. Each thing of hers is a borderless see-through window that only catches the mouse where something is drawn. The only file that draws on screen. |
| `draw.go` | The sprite sheet, boxes and text, drawn into plain images |
| `desktop.go` | Screens, and picking which desktop she asks where windows are: `kwin.go` (KDE), `sway.go`, `ewmh.go` (any X11 window manager). `desktop_test.go` checks the sway parsing. |
| `win32_windows.go` | `x11.go` and the desktop's file in one, for Windows: her windows (layered, per-pixel alpha), the screens, and finding and focusing other windows |
| `os_unix.go`, `os_windows.go` | Playing sounds (paplay; on Windows, waveOut), starting detached, picking the desktop, and installing (where, the menu entry, what to install), per OS |
| `install.go` | `cchobi install` / `uninstall`, which every install route runs: copying her into place, the menu entry, the Claude Code hooks. `install_test.go` checks the hook editing keeps everything else. A binary named `cchobi-setup` installs itself when started. |
| `install.sh`, `install.ps1`, `install.cmd` | The install scripts: download the right build (or build from a checkout) and run `cchobi install` |
| `.github/workflows/release.yml` | On a version tag: tests, builds for Linux (amd64, arm64) and Windows, publishes the release |
| `sprites.py` | Draws every frame procedurally and exports the sprite sheet. Each frame is built from shapes, then shaded with rim light and dithering. |
| `tools/clean_meows.py` | The meow cleaner |

The hook sends each event's JSON to her over a Unix socket at `$XDG_RUNTIME_DIR/cchobi.sock` (on Windows, `cchobi.sock` in `%TEMP%`).

### Known limits

- She finds a session's window by the project folder's name in its title. Two windows on the same folder, or a terminal that doesn't show the folder, get the first match.
- Interrupting Claude with Esc doesn't fire a hook, so a session she hasn't heard from in 5 minutes stops counting as working.
- On Windows, drawn pixels outside her clickable shape (her hearts, say) catch the mouse.
- The Settings panel has no scrolling: with a great many sounds, it can be taller than the screen.
- She looks at where windows are now and then, not constantly, so she can end up on an edge that has just moved.

## License

MIT, see [LICENSE](LICENSE). Some of the sounds are by other people, under their own licenses: see [CREDITS.md](CREDITS.md).
