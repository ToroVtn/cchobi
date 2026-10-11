package main

// Windows, on Win32: what x11.go is on Linux. Each thing of hers is a layered window (per-pixel
// alpha, no frame, always on top, never takes focus). Also where the other windows are, and
// focusing one, which on Linux the desktop's file does.

import (
	"image"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32  = syscall.NewLazyDLL("user32.dll")
	gdi32   = syscall.NewLazyDLL("gdi32.dll")
	dwmapi  = syscall.NewLazyDLL("dwmapi.dll")
	kernel  = syscall.NewLazyDLL("kernel32.dll")
	procs   sync.Map
	memDC   uintptr // what flush draws from
	wins    = map[uintptr]*Win{}
	class   = utf16("cchobi")
	enumMu  sync.Mutex // the Enum* callbacks fill these, one enumeration at a time
	enumed  []uintptr
	mons    []monitorInfo
	enumWin = syscall.NewCallback(func(h, _ uintptr) uintptr { enumed = append(enumed, h); return 1 })
	enumMon = syscall.NewCallback(func(h, _, _, _ uintptr) uintptr {
		mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
		call(user32, "GetMonitorInfoW", h, uintptr(unsafe.Pointer(&mi)))
		mons = append(mons, mi)
		return 1
	})
)

// call: a Win32 function by name, 0 if this Windows doesn't have it.
func call(dll *syscall.LazyDLL, name string, args ...uintptr) uintptr {
	p, ok := procs.Load(dll.Name + name)
	if !ok {
		p, _ = procs.LoadOrStore(dll.Name+name, dll.NewProc(name))
	}
	proc := p.(*syscall.LazyProc)
	if proc.Find() != nil {
		return 0
	}
	r, _, _ := proc.Call(args...)
	return r
}

func utf16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

type rect32 struct{ l, t, r, b int32 }

func (r rect32) rect() rect { return rect{int(r.l), int(r.t), int(r.r - r.l), int(r.b - r.t)} }

type monitorInfo struct {
	size          uint32
	monitor, work rect32
	flags         uint32
}

func openDisplay() error {
	call(user32, "SetProcessDpiAwarenessContext", ^uintptr(3)) // per-monitor v2: real pixels, no blur
	inst := call(kernel, "GetModuleHandleW", 0)
	wc := struct {
		size, style                         uint32
		proc                                uintptr
		clsExtra, wndExtra                  int32
		inst, icon, cursor, bg, menu, class uintptr
		iconSm                              uintptr
	}{proc: syscall.NewCallback(wndProc), inst: inst, cursor: call(user32, "LoadCursorW", 0, 32512), // IDC_ARROW
		class: uintptr(unsafe.Pointer(class))}
	wc.size = uint32(unsafe.Sizeof(wc))
	if call(user32, "RegisterClassExW", uintptr(unsafe.Pointer(&wc))) == 0 {
		return syscall.GetLastError()
	}
	memDC = call(gdi32, "CreateCompatibleDC", 0)
	return nil
}

// Win is one window: a picture (img, drawn into by its owner, then shown with flush) and what
// the mouse does to it. Only the parts in its shape catch the mouse; the rest is click-through.
type Win struct {
	hwnd       uintptr
	x, y, w, h int
	shown      bool
	img        *image.RGBA // premultiplied
	shape      []rect      // nil: all of it
	mask       []bool      // shape, pixel by pixel (made by flush)
	dib        uintptr     // the bitmap flush copies img into, and its pixels
	bits       unsafe.Pointer
	dibW, dibH int
	tracking   bool // asked to hear when the pointer leaves
	press      func(button, rx, ry, x, y int)
	release    func(button, rx, ry int)
	motion     func(rx, ry, x, y int)
	leave      func()
}

func newWin(w, h int) *Win {
	const exStyle = 0x80000 | 0x80 | 0x8 | 0x08000000 // layered, tool window (not on the taskbar), topmost, no activate
	hwnd := call(user32, "CreateWindowExW", exStyle, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(class)),
		0x80000000, 0, 0, uintptr(w), uintptr(h), 0, 0, call(kernel, "GetModuleHandleW", 0), 0) // WS_POPUP
	win := &Win{hwnd: hwnd, w: w, h: h, img: image.NewRGBA(image.Rect(0, 0, w, h))}
	wins[hwnd] = win
	return win
}

func (w *Win) resize(width, height int) {
	if width == w.w && height == w.h {
		return
	}
	w.w, w.h, w.mask = width, height, nil
	w.img = image.NewRGBA(image.Rect(0, 0, width, height))
}

const swpNoSize, swpNoMove, swpNoZOrder, swpNoActivate = 0x1, 0x2, 0x4, 0x10

func (w *Win) move(x, y int) {
	if x == w.x && y == w.y {
		return
	}
	w.x, w.y = x, y
	call(user32, "SetWindowPos", w.hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func (w *Win) show() {
	if !w.shown {
		w.shown = true
		w.flush()                             // a layered window shows nothing until it has a picture
		call(user32, "ShowWindow", w.hwnd, 4) // SW_SHOWNOACTIVATE
	}
}

func (w *Win) hide() {
	if w.shown {
		w.shown = false
		call(user32, "ShowWindow", w.hwnd, 0) // SW_HIDE
	}
}

func (w *Win) raise() {
	call(user32, "SetWindowPos", w.hwnd, ^uintptr(0), 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate) // HWND_TOPMOST
}

func (w *Win) destroy() {
	delete(wins, w.hwnd)
	call(user32, "DestroyWindow", w.hwnd)
	call(gdi32, "DeleteObject", w.dib)
}

// setShape: which rectangles of the window catch the mouse (they may overlap).
func (w *Win) setShape(rects []rect) { w.shape, w.mask = rects, nil }

// flush sends the picture to the screen. Windows sends the mouse through a layered window's
// see-through pixels, so the ones in its shape get an alpha of 1 (too faint to see) to catch it.
// ponytail: drawn pixels outside the shape catch the mouse too (on X they don't); hit-testing in
// WM_NCHITTEST can't fix that for other apps' windows, so only if it bothers anyone.
func (w *Win) flush() {
	if !w.shown {
		return
	}
	if w.dib == 0 || w.dibW != w.w || w.dibH != w.h {
		call(gdi32, "DeleteObject", w.dib)
		bmi := struct {
			size          uint32
			width, height int32
			planes, bpp   uint16
			rest          [6]uint32
		}{size: 40, width: int32(w.w), height: -int32(w.h), planes: 1, bpp: 32} // top-down, 32-bit BGRA
		w.dib = call(gdi32, "CreateDIBSection", 0, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&w.bits)), 0, 0)
		w.dibW, w.dibH = w.w, w.h
	}
	if w.mask == nil {
		w.mask = make([]bool, w.w*w.h)
		for _, r := range w.shape {
			for y := max(r.y, 0); y < min(r.y+r.h, w.h); y++ {
				for x := max(r.x, 0); x < min(r.x+r.w, w.w); x++ {
					w.mask[y*w.w+x] = true
				}
			}
		}
		if w.shape == nil {
			for i := range w.mask {
				w.mask[i] = true
			}
		}
	}
	if w.bits == nil {
		return
	}
	dst, src := unsafe.Slice((*byte)(w.bits), w.w*w.h*4), w.img.Pix
	for i := 0; i < len(src); i += 4 { // RGBA -> BGRA
		dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], src[i+3]
		if dst[i+3] == 0 && w.mask[i/4] {
			dst[i+3] = 1
		}
	}
	call(gdi32, "SelectObject", memDC, w.dib)
	pos, size, zero := [2]int32{int32(w.x), int32(w.y)}, [2]int32{int32(w.w), int32(w.h)}, [2]int32{}
	blend := [4]byte{0, 0, 255, 1} // AC_SRC_OVER, by each pixel's alpha (AC_SRC_ALPHA)
	call(user32, "UpdateLayeredWindow", w.hwnd, 0, uintptr(unsafe.Pointer(&pos)), uintptr(unsafe.Pointer(&size)),
		memDC, uintptr(unsafe.Pointer(&zero)), 0, uintptr(unsafe.Pointer(&blend)), 2) // ULW_ALPHA
}

// buttons: which button each button message is about, in X's numbers (1 left, 2 middle, 3 right).
var buttons = map[uintptr]int{0x201: 1, 0x202: 1, 0x207: 2, 0x208: 2, 0x204: 3, 0x205: 3}

// wndProc hands a window's mouse messages to it. It runs from DispatchMessage, so on the main loop.
func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	w := wins[hwnd]
	x, y := int(int16(lparam)), int(int16(lparam>>16))
	button := buttons[msg]
	switch {
	case w == nil:
	case msg == 0x21: // WM_MOUSEACTIVATE: clicking her doesn't take the focus
		return 3 // MA_NOACTIVATE
	case msg == 0x201 || msg == 0x204 || msg == 0x207: // a button down
		call(user32, "SetCapture", hwnd) // keep hearing the mouse while dragging, like X does
		if w.press != nil {
			w.press(button, w.x+x, w.y+y, x, y)
		}
		return 0
	case msg == 0x202 || msg == 0x205 || msg == 0x208: // a button up
		call(user32, "ReleaseCapture")
		if w.release != nil {
			w.release(button, w.x+x, w.y+y)
		}
		return 0
	case msg == 0x200: // WM_MOUSEMOVE
		if !w.tracking {
			tme := struct {
				size, flags uint32
				hwnd        uintptr
				hover       uint32
			}{flags: 2, hwnd: hwnd} // TME_LEAVE
			tme.size = uint32(unsafe.Sizeof(tme))
			w.tracking = call(user32, "TrackMouseEvent", uintptr(unsafe.Pointer(&tme))) != 0
		}
		if w.motion != nil {
			w.motion(w.x+x, w.y+y, x, y)
		}
		return 0
	case msg == 0x2A3: // WM_MOUSELEAVE
		w.tracking = false
		if w.leave != nil {
			w.leave()
		}
		return 0
	}
	return call(user32, "DefWindowProcW", hwnd, msg, wparam, lparam)
}

// windowEvents: a call, every 15 ms, that runs the messages waiting for her windows (and the
// tray's, which lives on this same thread). Windows only hands a thread's messages to that
// thread, and the main loop can't wait on them and its channels at once.
// ponytail: polling; MsgWaitForMultipleObjects with an event the channels set, if the wakeups show.
func windowEvents() <-chan func() {
	events := make(chan func())
	go func() {
		for range time.Tick(15 * time.Millisecond) {
			events <- pumpMessages
		}
	}()
	return events
}

func pumpMessages() {
	var m struct {
		hwnd           uintptr
		message        uint32
		wparam, lparam uintptr
		time           uint32
		pt             [2]int32
		private        uint32
	}
	for call(user32, "PeekMessageW", uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1) != 0 { // PM_REMOVE
		call(user32, "TranslateMessage", uintptr(unsafe.Pointer(&m)))
		call(user32, "DispatchMessageW", uintptr(unsafe.Pointer(&m)))
	}
}

// monitorInfos: each screen and its work area (minus the taskbar), the primary one first.
func monitorInfos() []monitorInfo {
	enumMu.Lock()
	defer enumMu.Unlock()
	mons = nil
	call(user32, "EnumDisplayMonitors", 0, 0, enumMon, 0)
	out := mons
	for i, m := range out {
		if m.flags&1 != 0 { // MONITORINFOF_PRIMARY
			out[0], out[i] = out[i], out[0]
		}
	}
	return out
}

// monitors: the screens, the primary one first.
func monitors() (out []rect) {
	for _, m := range monitorInfos() {
		out = append(out, m.monitor.rect())
	}
	if len(out) == 0 {
		out = []rect{{0, 0, 1920, 1080}}
	}
	return out
}

// ---- the other windows ----

// topLevel: the windows an app would show in Alt-Tab, top to bottom: no tool windows, owned
// dialogs or the desktop itself.
func topLevel() (out []uintptr) {
	enumMu.Lock()
	enumed = nil
	call(user32, "EnumWindows", enumWin, 0)
	all := enumed
	enumMu.Unlock()
	shell := call(user32, "GetShellWindow")
	for _, h := range all {
		ex := call(user32, "GetWindowLongPtrW", h, ^uintptr(19))                                            // GWL_EXSTYLE
		if h != shell && call(user32, "IsWindowVisible", h) != 0 && call(user32, "GetWindow", h, 4) == 0 && // no owner
			ex&0x80 == 0 && call(user32, "GetWindowTextLengthW", h) > 0 {
			out = append(out, h)
		}
	}
	return out
}

// cloaked: hidden by the system (on another virtual desktop, or a suspended store app).
func cloaked(h uintptr) bool {
	var c uint32
	return call(dwmapi, "DwmGetWindowAttribute", h, 14, uintptr(unsafe.Pointer(&c)), 4) == 0 && c != 0 // DWMWA_CLOAKED
}

func winLayout() (windows, areas []rect) {
	top := topLevel()
	for i := len(top) - 1; i >= 0; i-- { // bottom to top
		h := top[i]
		if call(user32, "IsIconic", h) != 0 || cloaked(h) {
			continue
		}
		var r rect32                                                                           // the frame as drawn, without the invisible borders for resizing
		if call(dwmapi, "DwmGetWindowAttribute", h, 9, uintptr(unsafe.Pointer(&r)), 16) != 0 { // DWMWA_EXTENDED_FRAME_BOUNDS
			call(user32, "GetWindowRect", h, uintptr(unsafe.Pointer(&r)))
		}
		windows = append(windows, r.rect())
	}
	for _, m := range monitorInfos() {
		areas = append(areas, m.work.rect())
	}
	return windows, areas
}

func winFocus(project string, vscode bool) bool {
	top := topLevel()
	titles, exes := make([]string, len(top)), make([]string, len(top))
	for i, h := range top {
		buf := make([]uint16, 512)
		call(user32, "GetWindowTextW", h, uintptr(unsafe.Pointer(&buf[0])), 512)
		titles[i], exes[i] = syscall.UTF16ToString(buf), exeOf(h)
	}
	i := pickWindow(titles, exes, project, vscode)
	if i < 0 {
		return false
	}
	h := top[i]
	if call(user32, "IsIconic", h) != 0 {
		call(user32, "ShowWindow", h, 9) // SW_RESTORE
	}
	// allowed: she just got the click that asked for it
	call(user32, "SetForegroundWindow", h)
	for range 20 {
		if call(user32, "GetForegroundWindow") == h {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// exeOf: the program a window belongs to (Code.exe, say), which is what tells VS Code apart: its
// window class is Chromium's.
func exeOf(h uintptr) string {
	var pid uint32
	call(user32, "GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&pid)))
	p := call(kernel, "OpenProcess", 0x1000, 0, uintptr(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if p == 0 {
		return ""
	}
	defer call(kernel, "CloseHandle", p)
	buf, n := make([]uint16, 1024), uint32(1024)
	call(kernel, "QueryFullProcessImageNameW", p, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:n])))
}
