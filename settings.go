package main

// Settings: her sounds panel, from the tray, drawn like her status card. Each sound in her
// folders (~/.cchobi/<kind>) with a switch, ▶ to listen and ✕ to remove it (into
// ~/.cchobi/removed, so nothing's lost); Open folder, where new sounds go (anything ffmpeg can
// read; a .wav needs nothing); Reset to her default sounds.

import (
	"cmp"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var kindTitles = map[string]string{"meows": "Meows", "purrs": "Purrs", "water": "Watering the plant"}

type control struct {
	r  rect
	do func()
}

type Settings struct {
	*Win
	pet      *Pet
	controls []control
	listing  string  // what was in her folders when last drawn, to notice sounds dropped in
	checked  float64 // when that was last looked at
}

func newSettings(p *Pet) *Settings {
	s := &Settings{Win: newWin(1, 1), pet: p}
	s.press = func(button, _, _, x, y int) {
		for _, c := range s.controls {
			if button == 1 && c.r.contains(x, y) {
				c.do()
				return
			}
		}
	}
	return s
}

func (s *Settings) popup() {
	s.draw()
	m := monitors()[0]
	g := workArea(m.x+m.w/2, m.y+m.h/2)
	s.move(g.x+g.w-s.w-12, g.y+g.h-s.h-12)
	s.show()
	s.raise()
	s.rescan()
}

// step, while it's open: once a second, has anything been dropped into (or taken out of) her folders?
func (s *Settings) step() {
	if now := mono(); now-s.checked > 1 {
		s.checked = now
		if s.list() != s.listing {
			s.rescan() // makes a dropped-in video's .wav, which shows up a moment later
			s.redraw()
		}
	}
}

func (s *Settings) rescan() {
	go scanSounds(func(snd *Sounds) { calls <- func() { s.pet.sounds = snd } })
}

// redraw keeps its bottom right corner where it is as it grows or shrinks.
func (s *Settings) redraw() {
	right, bottom := s.x+s.w, s.y+s.h
	s.draw()
	s.move(right-s.w, bottom-s.h)
}

// wavs: the sounds in a folder, by name, meow2 before meow10.
func wavs(kind string) []string {
	files, _ := filepath.Glob(filepath.Join(data, kind, "*.wav"))
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	slices.SortFunc(names, naturally)
	return names
}

var digits = regexp.MustCompile(`\d+|\D+`)

func naturally(a, b string) int {
	pa, pb := digits.FindAllString(a, -1), digits.FindAllString(b, -1)
	for i := range min(len(pa), len(pb)) {
		if pa[i] == pb[i] {
			continue
		}
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea == nil && eb == nil && na != nb {
			return cmp.Compare(na, nb)
		}
		return strings.Compare(pa[i], pb[i])
	}
	return len(pa) - len(pb)
}

func (s *Settings) list() string {
	var all []string
	for _, k := range kinds {
		all = append(all, wavs(k)...)
	}
	return strings.Join(all, "|")
}

func (s *Settings) draw() {
	lh, pad, w := lineHeight(), 12, 300
	row := lh + 8
	h := pad + row + pad // the buttons
	for _, k := range kinds {
		h += row*(1+len(wavs(k))) + 6
	}
	s.resize(w, h)
	clear(s.img)
	box(s.img, 0, 0, w, h, accent)
	faint := color.NRGBA{accent.R, accent.G, accent.B, 90}
	s.controls = nil
	y := pad
	for _, kind := range kinds {
		drawText(s.img, []string{kindTitles[kind]}, pad, y+(row-lh)/2, accent)
		fill(s.img, pad, y+row-3, w-pad, y+row-2, faint)
		y += row
		for _, name := range wavs(kind) {
			on := !slices.Contains(s.pet.muted[kind], name)
			cy := y + (row-12)/2
			box(s.img, pad, cy, pad+12, cy+12, accent)
			if on {
				fill(s.img, pad+3, cy+3, pad+9, cy+9, accent)
			}
			drawText(s.img, []string{strings.TrimSuffix(name, ".wav")}, pad+20, y+(row-lh)/2, accent)
			drawPlay(s.img, w-pad-40, cy)
			drawCross(s.img, w-pad-12, cy)
			kind, name, f := kind, name, filepath.Join(data, kind, name)
			s.controls = append(s.controls,
				control{rect{pad, y, w - pad - 52 - pad, row}, func() { s.toggle(kind, name) }},
				control{rect{w - pad - 46, y, 24, row}, func() { play(f, 1) }},
				control{rect{w - pad - 18, y, 24, row}, func() { s.remove(kind, name) }})
			y += row
		}
		y += 6
	}
	x := pad
	for _, b := range []struct {
		label string
		do    func()
	}{{"Open folder", func() { exec.Command(opener, data).Start() }}, {"Reset", s.reset}, {"Close", s.hide}} {
		bw := textWidth(b.label) + 16
		box(s.img, x, y, x+bw, y+row, accent)
		drawText(s.img, []string{b.label}, x+8, y+(row-lh)/2, accent)
		s.controls = append(s.controls, control{rect{x, y, bw, row}, b.do})
		x += bw + 8
	}
	s.listing = s.list()
	s.flush()
}

// drawPlay draws ▶ in a 12-pixel square.
func drawPlay(img *image.RGBA, x, y int) {
	for i := range 10 {
		fill(img, x+1+i, y+1+i/2, x+2+i, y+11-i/2, accent)
	}
}

// drawCross draws ✕ in a 12-pixel square.
func drawCross(img *image.RGBA, x, y int) {
	for i := range 10 {
		fill(img, x+1+i, y+1+i, x+3+i, y+3+i, accent)
		fill(img, x+9-i, y+1+i, x+11-i, y+3+i, accent)
	}
}

func (s *Settings) toggle(kind, name string) {
	p := s.pet
	if i := slices.Index(p.muted[kind], name); i >= 0 {
		p.muted[kind] = slices.Delete(p.muted[kind], i, i+1)
	} else {
		if p.muted == nil {
			p.muted = map[string][]string{}
		}
		p.muted[kind] = append(p.muted[kind], name)
	}
	p.save()
	s.redraw()
}

// remove moves a sound into ~/.cchobi/removed/<kind>/.
func (s *Settings) remove(kind, name string) {
	s.moveOut(kind, name)
	s.pet.save()
	s.rescan()
	s.redraw()
}

func (s *Settings) moveOut(kind, name string) {
	bin := filepath.Join(data, "removed", kind)
	os.MkdirAll(bin, 0o755)
	os.Remove(filepath.Join(bin, name)) // Windows won't rename over a file
	os.Rename(filepath.Join(data, kind, name), filepath.Join(bin, name))
	if i := slices.Index(s.pet.muted[kind], name); i >= 0 {
		s.pet.muted[kind] = slices.Delete(s.pet.muted[kind], i, i+1)
	}
}

// reset: back to exactly her default sounds, all switched on. Anything else is moved out, like ✕.
func (s *Settings) reset() {
	for _, kind := range kinds {
		defaults, _ := defaultSounds.ReadDir("sounds/" + kind)
		for _, name := range wavs(kind) {
			if !slices.ContainsFunc(defaults, func(d os.DirEntry) bool { return d.Name() == name }) {
				s.moveOut(kind, name)
			}
		}
		copyDefaults(kind)
	}
	s.pet.muted = map[string][]string{}
	s.pet.save()
	s.rescan()
	s.redraw()
}
