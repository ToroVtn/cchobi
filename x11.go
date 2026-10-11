//go:build unix

package main

// Windows, straight on X11 (XWayland under Plasma): no toolkit. Each thing of hers on the
// desktop is a borderless see-through window the window manager leaves alone. This file is all
// that talks to X: another platform provides openDisplay, Win, windowEvents and monitors
// (win32_windows.go).

import (
	"image"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"
)

var (
	X      *xgb.Conn
	root   xproto.Window
	visual xproto.Visualid // 32-bit colour with alpha, so windows can be see-through
	cmap   xproto.Colormap
	gc     xproto.Gcontext
	wins   = map[xproto.Window]*Win{}
)

func openDisplay() error {
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
func (w *Win) setShape(rects []rect) {
	xr := make([]xproto.Rectangle, len(rects))
	for i, r := range rects {
		xr[i] = xproto.Rectangle{X: int16(r.x), Y: int16(r.y), Width: uint16(r.w), Height: uint16(r.h)}
	}
	shape.Rectangles(X, shape.SoSet, shape.SkInput, xproto.ClipOrderingUnsorted, w.id, 0, 0, xr)
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

// windowEvents: X's events, each as a call that hands it to its window, for the main loop to run.
func windowEvents() <-chan func() {
	events := make(chan func())
	go func() {
		for {
			ev, err := X.WaitForEvent()
			if ev == nil && err == nil {
				events <- func() { panic("lost the X connection") }
				return
			}
			if ev != nil {
				events <- func() { dispatch(ev) }
			}
		}
	}()
	return events
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
