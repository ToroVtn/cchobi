package main

// Drawing, on plain images: the sprite sheet, boxes and text. Nothing here knows about the
// screen; a Win shows what's drawn into its img.

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

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

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
	ScratchWall [2]float64        `json:"scratch_wall"`
	Floor       [2]float64        `json:"floor"`
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
func shapeOf(src *image.NRGBA, dx, dy, scale int, flip bool) []rect {
	var out []rect
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
				out = append(out, rect{dx + x0*scale, dy + y*scale, (x - start) * scale, scale})
				start = -1
			}
		}
	}
	return out
}

// grown: the shape plus a d-pixel margin, so the pointer counts as near her a little early.
func grown(rects []rect, d int) []rect {
	var out []rect
	for _, dx := range []int{-d, 0, d} {
		for _, dy := range []int{-d, 0, d} {
			for _, r := range rects {
				out = append(out, rect{r.x + dx, r.y + dy, r.w, r.h})
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
	if data, err := os.ReadFile(home + "/.config/kdeglobals"); err == nil {
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
	data := gomono.TTF // no fontconfig (on Windows, say), or it found nothing: Go's own
	if out, err := exec.Command("fc-match", "-f", "%{file}", family).Output(); err == nil {
		if b, err := os.ReadFile(string(out)); err == nil {
			data = b
		}
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
