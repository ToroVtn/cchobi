package main

// Windows, drawing and text, straight on X11 (XWayland under Plasma): no toolkit. Each thing of
// hers on the desktop is a borderless see-through window the window manager leaves alone, drawn
// from the sprite sheet that sprites.py exports.

import (
	"bytes"
	"embed"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	X      *xgb.Conn
	root   xproto.Window
	visual xproto.Visualid // 32-bit colour with alpha, so windows can be see-through
	cmap   xproto.Colormap
	gc     xproto.Gcontext
	wins   = map[xproto.Window]*Win{}
)

func openX() error {
	var err error
	if X, err = xgb.NewConn(); err != nil {
		return err
	}
	if err = shape.Init(X); err != nil {
		return err
	}
	if err = randr.Init(X); err != nil {
		return err
	}
	screen := xproto.Setup(X).DefaultScreen(X)
	root = screen.Root
	for _, d := range screen.AllowedDepths {
		for _, v := range d.Visuals {
			if d.Depth == 32 && v.Class == xproto.VisualClassTrueColor && visual == 0 {
				visual = v.VisualId
			}
		}
	}
	cmap, _ = xproto.NewColormapId(X)
	xproto.CreateColormap(X, xproto.ColormapAllocNone, cmap, root, visual)
	pix, _ := xproto.NewPixmapId(X)
	xproto.CreatePixmap(X, 32, pix, xproto.Drawable(root), 1, 1)
	gc, _ = xproto.NewGcontextId(X)
	return xproto.CreateGCChecked(X, gc, xproto.Drawable(pix), 0, nil).Check()
}

// Win is one window: a picture (img, drawn into by its owner, then shown with flush) and what
// the mouse does to it. Only the parts in its shape catch the mouse; the rest is click-through.
type Win struct {
	id         xproto.Window
	x, y, w, h int
	shown      bool
	img        *image.RGBA // premultiplied, like X wants it
	upload     []byte
	press      func(button, rx, ry, x, y int)
	release    func(button, rx, ry int)
	motion     func(rx, ry, x, y int)
	leave      func()
}

func newWin(w, h int) *Win {
	id, _ := xproto.NewWindowId(X)
	events := uint32(xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease | xproto.EventMaskPointerMotion |
		xproto.EventMaskLeaveWindow | xproto.EventMaskExposure)
	// override-redirect: the window manager won't move, group, focus or decorate it
	xproto.CreateWindow(X, 32, id, root, 0, 0, uint16(w), uint16(h), 0, xproto.WindowClassInputOutput, visual,
		xproto.CwBackPixel|xproto.CwBorderPixel|xproto.CwOverrideRedirect|xproto.CwEventMask|xproto.CwColormap,
		[]uint32{0, 0, 1, events, uint32(cmap)})
	name := []byte("cchobi\x00cchobi\x00")
	xproto.ChangeProperty(X, xproto.PropModeReplace, id, xproto.AtomWmClass, xproto.AtomString, 8, uint32(len(name)), name)
	win := &Win{id: id, w: w, h: h, img: image.NewRGBA(image.Rect(0, 0, w, h))}
	wins[id] = win
	return win
}

func (w *Win) resize(width, height int) {
	if width == w.w && height == w.h {
		return
	}
	w.w, w.h = width, height
	w.img = image.NewRGBA(image.Rect(0, 0, width, height))
	xproto.ConfigureWindow(X, w.id, xproto.ConfigWindowWidth|xproto.ConfigWindowHeight, []uint32{uint32(width), uint32(height)})
}

func (w *Win) move(x, y int) {
	if x == w.x && y == w.y {
		return
	}
	w.x, w.y = x, y
	xproto.ConfigureWindow(X, w.id, xproto.ConfigWindowX|xproto.ConfigWindowY, []uint32{uint32(int32(x)), uint32(int32(y))})
}

func (w *Win) show() {
	if !w.shown {
		w.shown = true
		xproto.MapWindow(X, w.id)
		w.flush()
	}
}

func (w *Win) hide() {
	if w.shown {
		w.shown = false
		xproto.UnmapWindow(X, w.id)
	}
}

func (w *Win) raise() {
	xproto.ConfigureWindow(X, w.id, xproto.ConfigWindowStackMode, []uint32{xproto.StackModeAbove})
}

func (w *Win) destroy() {
	delete(wins, w.id)
	xproto.DestroyWindow(X, w.id)
}

// setShape: which rectangles of the window catch the mouse (they may overlap).
func (w *Win) setShape(rects []xproto.Rectangle) {
	shape.Rectangles(X, shape.SoSet, shape.SkInput, xproto.ClipOrderingUnsorted, w.id, 0, 0, rects)
}

// flush sends the picture to the screen, in strips that fit in an X request.
func (w *Win) flush() {
	if !w.shown {
		return
	}
	pix := w.img.Pix
	if len(w.upload) != len(pix) {
		w.upload = make([]byte, len(pix))
	}
	for i := 0; i < len(pix); i += 4 { // RGBA -> X's BGRA
		w.upload[i], w.upload[i+1], w.upload[i+2], w.upload[i+3] = pix[i+2], pix[i+1], pix[i], pix[i+3]
	}
	stride, rows := w.w*4, max(1, 60000/(w.w*4))
	for y := 0; y < w.h; y += rows {
		n := min(rows, w.h-y)
		xproto.PutImage(X, xproto.ImageFormatZPixmap, xproto.Drawable(w.id), gc, uint16(w.w), uint16(n),
			0, int16(y), 0, 32, w.upload[y*stride:(y+n)*stride])
	}
}

// dispatch hands an X event to the window it's for.
func dispatch(ev xgb.Event) {
	switch e := ev.(type) {
	case xproto.ButtonPressEvent:
		if w := wins[e.Event]; w != nil && w.press != nil {
			w.press(int(e.Detail), int(e.RootX), int(e.RootY), int(e.EventX), int(e.EventY))
		}
	case xproto.ButtonReleaseEvent:
		if w := wins[e.Event]; w != nil && w.release != nil {
			w.release(int(e.Detail), int(e.RootX), int(e.RootY))
		}
	case xproto.MotionNotifyEvent:
		if w := wins[e.Event]; w != nil && w.motion != nil {
			w.motion(int(e.RootX), int(e.RootY), int(e.EventX), int(e.EventY))
		}
	case xproto.LeaveNotifyEvent:
		if w := wins[e.Event]; w != nil && w.leave != nil {
			w.leave()
		}
	case xproto.ExposeEvent:
		if w := wins[e.Window]; w != nil && e.Count == 0 {
			w.flush()
		}
	}
}

// ---- screens ----

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// monitors: the screens, the primary one first.
func monitors() []rect {
	var out []rect
	if reply, err := randr.GetMonitors(X, root, true).Reply(); err == nil {
		for _, m := range reply.Monitors {
			r := rect{int(m.X), int(m.Y), int(m.Width), int(m.Height)}
			if m.Primary {
				out = append([]rect{r}, out...)
			} else {
				out = append(out, r)
			}
		}
	}
	if len(out) == 0 {
		s := xproto.Setup(X).DefaultScreen(X)
		out = []rect{{0, 0, int(s.WidthInPixels), int(s.HeightInPixels)}}
	}
	return out
}

// workAreas: each screen's usable area (minus its panels), as KWin reports it.
var workAreas []rect

// workArea: the usable part of the screen a point is on (or nearest to: on a panel, say).
func workArea(x, y int) rect {
	best, bestD := rect{}, -1
	for _, a := range workAreas {
		dx, dy := max(a.x-x, 0, x-(a.x+a.w-1)), max(a.y-y, 0, y-(a.y+a.h-1))
		if d := dx*dx + dy*dy; bestD < 0 || d < bestD {
			best, bestD = a, d
		}
	}
	if bestD >= 0 {
		return best
	}
	g, _ := screenAt(x, y)
	g.h -= 60 // KWin didn't answer: guess a 60 px panel at the bottom
	return g
}

// screenAt: the screen a point is on, if any.
func screenAt(x, y int) (rect, bool) {
	for _, m := range monitors() {
		if m.contains(x, y) {
			return m, true
		}
	}
	return monitors()[0], false
}

// ---- the sprite sheet ----

// Sheet holds every sprite at 1 px per pixel (sprites.py draws them), plus the points on them
// that things line up with. Sprites face right; the app mirrors them to face left.
type Sheet struct {
	img         *image.NRGBA
	Sprites     map[string][4]int `json:"sprites"`
	Size        int               `json:"size"`
	WalkFrames  int               `json:"walk_frames"`
	HangFrames  int               `json:"hang_frames"`
	Accent      [3]uint8          `json:"accent"`
	HangGrab    [2]float64        `json:"hang_grab"`
	SleepAnchor [2]float64        `json:"sleep_anchor"`
	PerchAnchor [2]float64        `json:"perch_anchor"`
	MunchMouth  [2]float64        `json:"munch_mouth"`
	NibbleMouth [2]float64        `json:"nibble_mouth"`
	RubCheek    [2]float64        `json:"rub_cheek"`
	CushionSeat [][2]float64      `json:"cushion_seats"`
	PlantSize   [2]int            `json:"plant_size"`
	PlantBite   [2]float64        `json:"plant_bite"`
	PlantSoil   [2]float64        `json:"plant_soil"`
	PlantCheek  [2]float64        `json:"plant_cheek"`
	PlantLeaves [][4]float64      `json:"plant_leaves"`
	DryOrder    []int             `json:"dry_order"`
	PotLeaves   [][3]float64      `json:"pot_leaves"`
	PotMouth    [4]float64        `json:"pot_mouth"`
	LeafSize    int               `json:"leaf_size"`
	BinMouth    [2]float64        `json:"bin_mouth"`
	CanPour     [2]float64        `json:"can_pour"`
}

// The sprite sheet is built into the binary, so she runs without Python. Rebuild after python3 sprites.py.
//
//go:embed assets/sprites.json assets/sprites.png
var assets embed.FS

func loadSheet() (*Sheet, error) {
	s := &Sheet{}
	data, err := assets.ReadFile("assets/sprites.json")
	if err == nil {
		err = json.Unmarshal(data, s)
	}
	if err != nil {
		return nil, err
	}
	f, err := assets.Open("assets/sprites.png")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	s.img = img.(*image.NRGBA)
	return s, nil
}

func (s *Sheet) get(name string) *image.NRGBA {
	r, ok := s.Sprites[name]
	if !ok {
		panic("no sprite " + name)
	}
	return s.img.SubImage(image.Rect(r[0], r[1], r[0]+r[2], r[1]+r[3])).(*image.NRGBA)
}

// blit draws a sprite into a window's picture at (dx, dy), each pixel as a scale x scale block,
// mirrored if flip, at the given opacity (0..1).
func blit(dst *image.RGBA, src *image.NRGBA, dx, dy, scale int, flip bool, opacity float64) {
	b := src.Bounds()
	for sy := 0; sy < b.Dy(); sy++ {
		for sx := 0; sx < b.Dx(); sx++ {
			fx := sx
			if flip {
				fx = b.Dx() - 1 - sx
			}
			c := src.NRGBAAt(b.Min.X+fx, b.Min.Y+sy)
			a := uint32(float64(c.A) * opacity)
			if a == 0 {
				continue
			}
			for y := dy + sy*scale; y < dy+(sy+1)*scale; y++ {
				for x := dx + sx*scale; x < dx+(sx+1)*scale; x++ {
					if (image.Point{x, y}).In(dst.Rect) {
						over(dst, x, y, uint32(c.R), uint32(c.G), uint32(c.B), a)
					}
				}
			}
		}
	}
}

// over lays a colour (not premultiplied, 0..255) with alpha a over a pixel.
func over(dst *image.RGBA, x, y int, r, g, b, a uint32) {
	i := dst.PixOffset(x, y)
	p := dst.Pix[i : i+4 : i+4]
	k := 255 - a
	p[0] = uint8((r*a + uint32(p[0])*k) / 255)
	p[1] = uint8((g*a + uint32(p[1])*k) / 255)
	p[2] = uint8((b*a + uint32(p[2])*k) / 255)
	p[3] = uint8(a + uint32(p[3])*k/255)
}

func clear(img *image.RGBA) {
	for i := range img.Pix {
		img.Pix[i] = 0
	}
}

// shapeOf: rectangles covering a sprite's visible pixels, scaled and placed like blit would.
func shapeOf(src *image.NRGBA, dx, dy, scale int, flip bool) []xproto.Rectangle {
	var out []xproto.Rectangle
	b := src.Bounds()
	for y := 0; y < b.Dy(); y++ {
		start := -1
		for x := 0; x <= b.Dx(); x++ {
			solid := x < b.Dx() && src.NRGBAAt(b.Min.X+x, b.Min.Y+y).A > 0
			if solid && start < 0 {
				start = x
			} else if !solid && start >= 0 {
				x0 := start
				if flip {
					x0 = b.Dx() - x
				}
				out = append(out, xproto.Rectangle{X: int16(dx + x0*scale), Y: int16(dy + y*scale),
					Width: uint16((x - start) * scale), Height: uint16(scale)})
				start = -1
			}
		}
	}
	return out
}

// grown: the shape plus a d-pixel margin, so the pointer counts as near her a little early.
func grown(rects []xproto.Rectangle, d int16) []xproto.Rectangle {
	var out []xproto.Rectangle
	for _, dx := range []int16{-d, 0, d} {
		for _, dy := range []int16{-d, 0, d} {
			for _, r := range rects {
				out = append(out, xproto.Rectangle{X: r.X + dx, Y: r.Y + dy, Width: r.Width, Height: r.Height})
			}
		}
	}
	return out
}

// ---- boxes and text: dark glass with an amber hairline, like a terminal ----

var (
	glass  = color.NRGBA{14, 12, 18, 235}
	accent color.NRGBA // amber, from the sprite sheet
)

func fill(img *image.RGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	for y := max(y0, 0); y < min(y1, img.Rect.Dy()); y++ {
		for x := max(x0, 0); x < min(x1, img.Rect.Dx()); x++ {
			over(img, x, y, uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A))
		}
	}
}

func box(img *image.RGBA, x0, y0, x1, y1 int, line color.NRGBA) {
	fill(img, x0+1, y0+1, x1-1, y1-1, glass)
	fill(img, x0, y0, x1, y0+1, line)
	fill(img, x0, y1-1, x1, y1, line)
	fill(img, x0, y0+1, x0+1, y1-1, line)
	fill(img, x1-1, y0+1, x1, y1-1, line)
}

// tail: the bubble's little triangle pointing down at her, from its bottom edge at y.
func tail(img *image.RGBA, cx, y int, line color.NRGBA) {
	for k := 0; k <= 10; k++ {
		half := 6 * (10 - k) / 10
		fill(img, cx-half, y+k, cx+half+1, y+k+1, glass)
		fill(img, cx-half, y+k, cx-half+1, y+k+1, line)
		fill(img, cx+half, y+k, cx+half+1, y+k+1, line)
	}
	fill(img, cx-5, y-1, cx+6, y, glass) // opens it onto the box
}

var face font.Face

// loadFont: your KDE fixed-width font (what Qt used for her bubble), via fontconfig.
func loadFont() error {
	family, size := "monospace", 11.0
	if data, err := os.ReadFile(os.Getenv("HOME") + "/.config/kdeglobals"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(line, "fixed="); ok {
				parts := strings.Split(v, ",")
				family = parts[0]
				if len(parts) > 1 {
					if n, err := parseFloat(parts[1]); err == nil && n > 0 {
						size = n
					}
				}
			}
		}
	}
	out, err := exec.Command("fc-match", "-f", "%{file}", family).Output()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(string(out))
	if err != nil {
		return err
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return err
	}
	face, err = opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 96, Hinting: font.HintingFull})
	return err
}

func lineHeight() int { return face.Metrics().Height.Ceil() }

func textWidth(s string) int { return font.MeasureString(face, s).Ceil() }

// wrap breaks text into lines no wider than width, at spaces where it can.
func wrap(text string, width int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Split(para, " ") {
			try := word
			if line != "" {
				try = line + " " + word
			}
			if textWidth(try) <= width {
				line = try
				continue
			}
			if line != "" {
				lines = append(lines, line)
			}
			for textWidth(word) > width { // a word too long for a line: break it anywhere
				r := []rune(word)
				n := len(r)
				for n > 1 && textWidth(string(r[:n])) > width {
					n--
				}
				lines = append(lines, string(r[:n]))
				word = string(r[n:])
			}
			line = word
		}
		lines = append(lines, line)
	}
	return lines
}

func drawText(img *image.RGBA, lines []string, x, y int, c color.NRGBA) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face}
	ascent := face.Metrics().Ascent.Ceil()
	for i, line := range lines {
		d.Dot = fixed.P(x, y+ascent+i*lineHeight())
		d.DrawString(line)
	}
}

// pngOf: a sprite as a PNG file's bytes, scaled up (for the tray icon).
func pngOf(src *image.NRGBA, scale int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, src.Rect.Dx()*scale, src.Rect.Dy()*scale))
	blit(img, src, 0, 0, scale, false, 1)
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}
