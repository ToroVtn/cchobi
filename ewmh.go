package main

// The desktop on plain X11, as any window manager that speaks EWMH tells it through properties on
// the windows: where the windows are, and focusing one.

import (
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

var (
	atoms   = map[string]xproto.Atom{}
	atomsMu sync.Mutex // layout and focusWindow run on their own goroutines
)

func atom(name string) xproto.Atom {
	atomsMu.Lock()
	defer atomsMu.Unlock()
	if a, ok := atoms[name]; ok {
		return a
	}
	r, err := xproto.InternAtom(X, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0
	}
	atoms[name] = r.Atom
	return r.Atom
}

// prop: a window's property, raw.
func prop(w xproto.Window, name string) *xproto.GetPropertyReply {
	r, err := xproto.GetProperty(X, false, w, atom(name), xproto.GetPropertyTypeAny, 0, 1<<16).Reply()
	if err != nil {
		return &xproto.GetPropertyReply{}
	}
	return r
}

// cards: a property of 32-bit numbers (or windows, or atoms).
func cards(w xproto.Window, name string) []uint32 {
	r := prop(w, name)
	if r.Format != 32 {
		return nil
	}
	out := make([]uint32, r.ValueLen)
	for i := range out {
		out[i] = xgb.Get32(r.Value[i*4:])
	}
	return out
}

func has(list []uint32, a xproto.Atom) bool {
	for _, v := range list {
		if xproto.Atom(v) == a {
			return true
		}
	}
	return false
}

// normal: a window she can perch on or focus (not a panel, menu, tooltip...).
func normal(w xproto.Window) bool {
	types := cards(w, "_NET_WM_WINDOW_TYPE")
	return len(types) == 0 || has(types, atom("_NET_WM_WINDOW_TYPE_NORMAL"))
}

func clients(name string) (out []xproto.Window) {
	for _, w := range cards(root, name) {
		out = append(out, xproto.Window(w))
	}
	return out
}

func ewmhLayout() (windows, areas []rect) {
	stack := clients("_NET_CLIENT_LIST_STACKING") // bottom to top
	if len(stack) == 0 {
		stack = clients("_NET_CLIENT_LIST") // not every window manager keeps the stacking one
	}
	desk := cards(root, "_NET_CURRENT_DESKTOP")
	for _, w := range stack {
		on := cards(w, "_NET_WM_DESKTOP")
		if !normal(w) || has(cards(w, "_NET_WM_STATE"), atom("_NET_WM_STATE_HIDDEN")) ||
			(len(on) > 0 && len(desk) > 0 && on[0] != desk[0] && on[0] != 0xFFFFFFFF) {
			continue
		}
		g, err := xproto.GetGeometry(X, xproto.Drawable(w)).Reply()
		if err != nil {
			continue
		}
		at, err := xproto.TranslateCoordinates(X, w, root, 0, 0).Reply()
		if err != nil {
			continue
		}
		r := rect{int(at.DstX), int(at.DstY), int(g.Width), int(g.Height)}
		if e := cards(w, "_NET_FRAME_EXTENTS"); len(e) == 4 { // left, right, top, bottom: its frame
			r = rect{r.x - int(e[0]), r.y - int(e[2]), r.w + int(e[0]+e[1]), r.h + int(e[2]+e[3])}
		}
		windows = append(windows, r)
	}
	// ponytail: _NET_WORKAREA is one box over all screens, so a panel on one screen shrinks them
	// all; reading each panel's _NET_WM_STRUT_PARTIAL if that's wrong somewhere.
	if wa := cards(root, "_NET_WORKAREA"); len(desk) > 0 && len(wa) >= int(desk[0])*4+4 {
		a := wa[desk[0]*4:]
		x0, y0, x1, y1 := int(int32(a[0])), int(int32(a[1])), int(int32(a[0]+a[2])), int(int32(a[1]+a[3]))
		for _, m := range monitors() {
			l, t, r, b := max(m.x, x0), max(m.y, y0), min(m.x+m.w, x1), min(m.y+m.h, y1)
			if r > l && b > t {
				areas = append(areas, rect{l, t, r - l, b - t})
			}
		}
	}
	return windows, areas
}

func ewmhFocus(project string, vscode bool) bool {
	var ws []xproto.Window
	var titles, classes []string
	for _, w := range clients("_NET_CLIENT_LIST") {
		if !normal(w) {
			continue
		}
		title := prop(w, "_NET_WM_NAME").Value
		if len(title) == 0 {
			title = prop(w, "WM_NAME").Value
		}
		ws, titles, classes = append(ws, w), append(titles, string(title)), append(classes, string(prop(w, "WM_CLASS").Value))
	}
	i := pickWindow(titles, classes, project, vscode)
	if i < 0 {
		return false
	}
	// ask the window manager to activate it, as a pager would (2): that also unminimizes it and
	// switches to its desktop
	ev := xproto.ClientMessageEvent{Format: 32, Window: ws[i], Type: atom("_NET_ACTIVE_WINDOW"),
		Data: xproto.ClientMessageDataUnionData32New([]uint32{2, 0, 0, 0, 0})}
	xproto.SendEvent(X, false, root, xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(ev.Bytes()))
	for range 20 {
		if a := cards(root, "_NET_ACTIVE_WINDOW"); len(a) > 0 && xproto.Window(a[0]) == ws[i] {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
