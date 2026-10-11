# cchobi

A desktop pet for Claude Code: a little black tuxedo cat, drawn in pixel art in the style of the game *Replaced*.

- **She lives her own life**: wanders around your screen, loafs, grooms, sunbathes, perches on top of windows, rubs against their sides, and bothers her plant. When Claude isn't working she also naps on her cushion.
- **When Claude finishes or needs you**, she stops, meows and shows what it said in a speech bubble.

She uses about 25 MB of memory.

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

Her sounds live in `~/.cchobi/`. The first time she runs, she copies the defaults there from `sounds/`.

| Folder | Plays |
|---|---|
| `meows/` | when she needs you (one at random) |
| `purrs/` | while she's petted or rubbing against the pointer |
| `water/` | when the plant is watered |

**Settings…** in the tray menu lets you:
- switch sounds on or off
- preview them (▶)
- remove them (✕). Removed sounds go to `~/.cchobi/removed/`, so nothing is lost.
- add new ones. Videos are fine; only the audio is kept.
- reset to the defaults

You can also just drop `.wav` files into the folders. In `meows/`, other formats are converted for you.

**Variations:** so purrs and watering don't sound the same every time, each one is also kept at a few speeds (faster is also higher). These copies are made once with ffmpeg into `variants/` inside each folder, and remade when you change a sound.

**Cleaning recordings:** `tools/clean_meows.py` cleans recordings of your own cat: it removes background noise and voices, keeping only the meow. `python3 tools/clean_meows.py recordings/*.wav` writes the results to `~/.cchobi/meows/`. It needs numpy.

## Setup

### Requirements

| What | Needed for |
|---|---|
| Linux with KDE Plasma 6 (Wayland, with XWayland) | Asking KWin, through scripts run with `qdbus6` and read back with `journalctl`, which window to focus, where windows are, and where the panels are |
| Go | Building her |
| Python 3 | Drawing the sprites (only when changing them) |
| PySide6 | The Settings window |
| ffmpeg | Sounds |
| `paplay` (PulseAudio or PipeWire) | Sounds |
| fontconfig | Finding your KDE fixed-width font |

### Build

```sh
python3 sprites.py   # redraws assets/sprites.png + sprites.json (only needed after changing a drawing)
go build -ldflags="-s -w" -o cchobi .
go test              # checks the plant's rules
```

The sprite sheet is built into the binary. `settings.py` and `sounds/` must sit next to it, so to run her elsewhere, copy those three.

### Hook her up to Claude Code

Add the hook to `~/.claude/settings.json` for each of these events: `SessionStart`, `UserPromptSubmit`, `PostToolUse`, `Notification`, `Stop`, `SessionEnd`. Here it is for one:

```json
"hooks": {
  "SessionStart": [
    { "hooks": [ { "type": "command", "command": "/stuff/cchobi/cchobi hook", "timeout": 5 } ] }
  ],
  "UserPromptSubmit": [ ...same... ],
  ...
}
```

`SessionStart` starts her if she isn't running. You can also start her by hand with `./cchobi`. She shows up as `cchobi` in top/btop.

## How it fits together

| File | What it is |
|---|---|
| `main.go` | Starting up, the hook, saving state, sounds, KWin (focusing windows; where windows and panels are), the tray |
| `cat.go` | Her behaviour each tick (50 ms), her things (cushion, plant, leaves, watering can, bin), her speech bubble, the status card |
| `garden.go` | The plant's rules, with no drawing. `garden_test.go` checks them; `cat_test.go` checks which window edges she can reach. |
| `x11.go` | Windows, drawing and text directly on X11, with no toolkit. Each thing of hers is a borderless see-through window that only catches the mouse where something is drawn. |
| `sprites.py` | Draws every frame procedurally and exports the sprite sheet. Each frame is built from shapes, then shaded with rim light and dithering. |
| `settings.py` | The Settings window. A separate program that runs only while it's open and tells her about changes over her socket. |
| `tools/clean_meows.py` | The meow cleaner |

The hook sends each event's JSON to her over a Unix socket at `$XDG_RUNTIME_DIR/cchobi.sock`.

### Known limits

- She finds a session's window by the project folder's name in its title. Two windows on the same folder, or a terminal that doesn't show the folder, get the first match.
- Interrupting Claude with Esc doesn't fire a hook, so a session she hasn't heard from in 5 minutes stops counting as working.
- She looks at where windows are now and then, not constantly, so she can end up on an edge that has just moved.

## License

MIT, see [LICENSE](LICENSE). Some of the sounds are by other people, under their own licenses: see [CREDITS.md](CREDITS.md).
