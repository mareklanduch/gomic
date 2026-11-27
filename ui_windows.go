//go:build windows

package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/mareklanduch/gomic/internal/capture"
	"github.com/mareklanduch/gomic/internal/meter"
)

// Layout in 96-dpi pixels; scaled by the system DPI at startup.
const (
	winW         = 292
	pad          = 8
	labelW       = 14
	barH, barGap = 16, 6
	groupGap     = 10 // between the input pair and the output pair
	clipW        = 16
	valueW       = 44
	scaleH       = 12
	floorDB      = -60.0 // left edge of the bars
	warnDB       = -12.0 // green below, yellow above
	hotDB        = -3.0  // red above
	frameMS      = 33
)

var scaleMarks = []float64{-48, -36, -24, -12, -6, 0}

type ui struct {
	*meters
	hwnd  uintptr
	scale float64

	font, smallFont uintptr
}

var app *ui

func runWindow(m *meters) {
	runtime.LockOSThread()
	procSetProcessDpiAwarenessContext.Call(dpiAwarenessSystemAware)
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}

	app = &ui{meters: m, scale: float64(dpi) / 96}
	cfg := m.cfg
	app.font = app.makeFont(13, "Segoe UI")
	app.smallFont = app.makeFont(10, "Segoe UI")

	hinst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	className := utf16("gomicMeter")
	wc := wndClassEx{
		style: csDblClks, wndProc: syscall.NewCallback(wndProc),
		instance: hinst, cursor: cursor, className: className,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	w, h := app.px(winW), app.px(winHeight(len(m.shown())))
	x, y := app.startPosition(w)
	exStyle := uintptr(0)
	if cfg.Topmost {
		exStyle = wsExTopmost
	}
	app.hwnd, _, _ = procCreateWindowExW.Call(exStyle,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16("gomic"))),
		wsPopup|wsVisible, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, hinst, 0)
	if app.hwnd == 0 {
		panic("gomic: could not create window")
	}
	procSetTimer.Call(app.hwnd, 1, frameMS, 0)

	var next msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&next)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&next)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&next)))
	}
}

func wndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	u := app
	switch message {
	case wmTimer:
		u.tick(time.Now())
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmPaint:
		u.paint(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmNCHitTest:
		return htCaption // drag from anywhere
	case wmNCLButtonDblClk:
		u.resetClip()
		return 0
	case wmNCRButtonUp:
		u.menu(hwnd)
		return 0
	case wmExitSizeMove:
		u.save(hwnd)
		return 0
	case wmDestroy:
		u.save(hwnd)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wparam, lparam)
	return r
}

func (u *ui) paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var rc rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := rc.right, rc.bottom

	// Draw off-screen, then copy in one go, so the bars do not flicker.
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer func() {
		procSelectObject.Call(mem, old)
		procDeleteObject.Call(bmp)
		procDeleteDC.Call(mem)
	}()
	procSetBkMode.Call(mem, bkTransparent)

	fill(mem, rect{0, 0, w, h}, colBackground)

	barX := u.px(pad + labelW)
	barW := w - barX - u.px(pad+valueW+4+clipW+4)
	groupTop := pad
	for _, src := range u.shown() {
		_, err := u.capt[src].Status()
		d := &u.disp[src]
		labels := [2]string{"1", "2"}
		if src == capture.Output {
			labels = [2]string{"L", "R"}
		}
		for ch := 0; ch < 2; ch++ {
			top := u.px(groupTop + ch*(barH+barGap))
			bottom := top + u.px(barH)

			text(mem, u.font, colText, labels[ch], rect{u.px(pad), top, barX, bottom}, dtLeft)

			track := rect{barX, top, barX + barW, bottom}
			fill(mem, track, colTrack)
			switch {
			case err == nil:
				u.drawLevel(mem, track, d.bars[ch])
			case ch == 0:
				// Nothing to meter: say why where the level would be.
				text(mem, u.smallFont, colClipOn, err.Error(),
					rect{track.left + u.px(4), top, track.right - u.px(4), bottom}, dtLeft|dtEndEllipsis)
			}

			clipBox := rect{track.right + u.px(4), top, track.right + u.px(4+clipW), bottom}
			if d.clip[ch] {
				fill(mem, clipBox, colClipOn)
			} else {
				fill(mem, clipBox, colClipOff)
			}

			value := "-∞"
			if hold := d.bars[ch].Hold; err == nil && hold > floorDB {
				value = fmt.Sprintf("%.1f", hold)
			}
			text(mem, u.font, colText, value, rect{clipBox.right + u.px(4), top, w - u.px(pad), bottom}, dtRight)
		}
		groupTop += groupH + groupGap
	}

	scaleTop := u.px(groupTop - groupGap + 2)
	for _, db := range scaleMarks {
		x := barX + dbToX(db, barW)
		label := fmt.Sprintf("%.0f", db)
		text(mem, u.smallFont, colDim, label, rect{x - u.px(30), scaleTop, x, scaleTop + u.px(scaleH)}, dtRight)
	}
	procBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, srcCopy)
}

// drawLevel paints the green/yellow/red bar and the peak-hold marker.
func (u *ui) drawLevel(hdc uintptr, track rect, b meter.Bar) {
	w := track.right - track.left
	level := track.left + dbToX(b.Level, w)
	zones := []struct {
		from, to float64
		color    uint32
	}{
		{floorDB, warnDB, colGreen},
		{warnDB, hotDB, colYellow},
		{hotDB, 0, colRed},
	}
	for _, z := range zones {
		left := track.left + dbToX(z.from, w)
		right := min(track.left+dbToX(z.to, w), level)
		if right > left {
			fill(hdc, rect{left, track.top, right, track.bottom}, z.color)
		}
	}
	if b.Hold > floorDB {
		x := track.left + dbToX(b.Hold, w)
		markW := max(int32(2), u.px(2))
		x = min(x, track.right) - markW
		fill(hdc, rect{max(x, track.left), track.top, max(x, track.left) + markW, track.bottom}, colHold)
	}
}

// dbToX maps floorDB..0 dBFS onto 0..width.
func dbToX(db float64, width int32) int32 {
	f := (db - floorDB) / -floorDB
	f = max(0, min(1, f))
	return int32(f*float64(width) + 0.5)
}

// groupH is the height of one meter: two bars and the gap between them.
const groupH = 2*barH + barGap

// winHeight is the window height, in 96-dpi pixels, with n meters visible.
func winHeight(n int) int {
	return pad + n*groupH + (n-1)*groupGap + 2 + scaleH + pad
}

// Menu command ids. Device entries are the base plus their position in the submenu.
const (
	idShowInput = iota + 1
	idShowOutput
	idTopmost
	idReset
	idQuit

	idInputDevices  = 1000
	idOutputDevices = 2000
	maxDevices      = 1000
)

func (u *ui) menu(hwnd uintptr) {
	check := func(on bool) uintptr {
		if on {
			return mfChecked
		}
		return 0
	}
	add := func(menu, flags, id uintptr, label string) {
		procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(utf16(label))))
	}
	// A device submenu; destroyed together with the menu it is attached to.
	pickers := [2][]pickerEntry{u.picker(capture.Input), u.picker(capture.Output)}
	submenu := func(src capture.Source, base uintptr) uintptr {
		sub, _, _ := procCreatePopupMenu.Call()
		for i, e := range pickers[src] {
			if i == maxDevices {
				break
			}
			if i == 1 {
				add(sub, mfSeparator, 0, "")
			}
			// "&" would otherwise underline the next letter.
			add(sub, check(e.checked), base+uintptr(i), strings.ReplaceAll(e.label, "&", "&&"))
		}
		return sub
	}

	m, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(m)
	add(m, mfPopup, submenu(capture.Input, idInputDevices), "Input")
	add(m, mfPopup, submenu(capture.Output, idOutputDevices), "Output")
	add(m, mfSeparator, 0, "")
	add(m, check(u.cfg.ShowInput), idShowInput, "Show input meter")
	add(m, check(u.cfg.ShowOutput), idShowOutput, "Show output meter")
	add(m, mfSeparator, 0, "")
	add(m, check(u.cfg.Topmost), idTopmost, "Always on top")
	add(m, 0, idReset, "Reset clip lights\tDouble-click")
	add(m, mfSeparator, 0, "")
	add(m, 0, idQuit, "Quit")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton,
		uintptr(pt.x), uintptr(pt.y), 0, hwnd, 0)

	switch {
	case cmd >= idOutputDevices && int(cmd-idOutputDevices) < len(pickers[capture.Output]):
		u.pick(capture.Output, pickers[capture.Output][cmd-idOutputDevices].device)
		u.save(hwnd)
	case cmd >= idInputDevices && int(cmd-idInputDevices) < len(pickers[capture.Input]):
		u.pick(capture.Input, pickers[capture.Input][cmd-idInputDevices].device)
		u.save(hwnd)
	case cmd == idShowInput, cmd == idShowOutput:
		src := capture.Input
		if cmd == idShowOutput {
			src = capture.Output
		}
		u.toggleShow(src)
		// The window grows or shrinks downward; its top-left corner stays put.
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(u.px(winW)), uintptr(u.px(winHeight(len(u.shown())))),
			swpNoMove|swpNoZOrder|swpNoActivate)
		u.save(hwnd)
	case cmd == idTopmost:
		u.cfg.Topmost = !u.cfg.Topmost
		after := hwndNoTopmost
		if u.cfg.Topmost {
			after = hwndTopmost
		}
		procSetWindowPos.Call(hwnd, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		u.save(hwnd)
	case cmd == idReset:
		u.resetClip()
	case cmd == idQuit:
		procDestroyWindow.Call(hwnd)
	}
}

func (u *ui) save(hwnd uintptr) {
	var rc rect
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r != 0 {
		u.cfg.X, u.cfg.Y, u.cfg.HasPos = int(rc.left), int(rc.top), true
	}
	_ = u.cfg.Save(u.cfgPath) // losing the saved position is not worth interrupting for
}

// startPosition restores the saved position if it is still on a screen,
// otherwise uses the top-right corner of the primary screen.
func (u *ui) startPosition(w int32) (x, y int32) {
	if u.cfg.HasPos {
		// POINT is passed by value: x in the low half, y in the high half.
		pt := uintptr(uint32(int32(u.cfg.X))) | uintptr(uint32(int32(u.cfg.Y)))<<32
		if mon, _, _ := procMonitorFromPoint.Call(pt, monitorDefaultToNull); mon != 0 {
			return int32(u.cfg.X), int32(u.cfg.Y)
		}
	}
	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	return int32(screenW) - w - u.px(24), u.px(24)
}

func (u *ui) px(v int) int32 { return int32(float64(v)*u.scale + 0.5) }

func (u *ui) makeFont(size int, face string) uintptr {
	f, _, _ := procCreateFontW.Call(uintptr(-int(u.px(size))), 0, 0, 0, fwNormal, 0, 0, 0,
		defaultCharset, 0, 0, cleartypeQuality, 0, uintptr(unsafe.Pointer(utf16(face))))
	return f
}

func fill(hdc uintptr, r rect, color uint32) {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
	procDeleteObject.Call(brush)
}

func text(hdc, font uintptr, color uint32, s string, r rect, align uintptr) {
	old, _, _ := procSelectObject.Call(hdc, font)
	procSetTextColor.Call(hdc, uintptr(color))
	u := syscall.StringToUTF16(s)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1),
		uintptr(unsafe.Pointer(&r)), align|dtSingleLine|dtVCenter|dtNoPrefix|dtNoClip)
	procSelectObject.Call(hdc, old)
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

var (
	colBackground = rgb(0x1b, 0x1b, 0x1d)
	colTrack      = rgb(0x30, 0x30, 0x34)
	colGreen      = rgb(0x3c, 0xcf, 0x5a)
	colYellow     = rgb(0xe6, 0xc2, 0x29)
	colRed        = rgb(0xe5, 0x48, 0x4d)
	colHold       = rgb(0xf2, 0xf2, 0xf2)
	colClipOff    = rgb(0x4a, 0x22, 0x22)
	colClipOn     = rgb(0xff, 0x3b, 0x30)
	colText       = rgb(0xd0, 0xd0, 0xd4)
	colDim        = rgb(0x8a, 0x8a, 0x90)
)

// ---- Win32 plumbing ----

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	pt      point
	private uint32
}

type wndClassEx struct {
	size, style            uint32
	wndProc                uintptr
	clsExtra, wndExtra     int32
	instance, icon, cursor uintptr
	background             uintptr
	menuName, className    *uint16
	iconSm                 uintptr
}

type paintStruct struct {
	hdc         uintptr
	erase       int32
	paint       rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}

const (
	wmDestroy         = 0x0002
	wmPaint           = 0x000F
	wmEraseBkgnd      = 0x0014
	wmNCHitTest       = 0x0084
	wmNCLButtonDblClk = 0x00A3
	wmNCRButtonUp     = 0x00A5
	wmTimer           = 0x0113
	wmExitSizeMove    = 0x0232

	wsPopup     = 0x80000000
	wsVisible   = 0x10000000
	wsExTopmost = 0x00000008
	csDblClks   = 0x0008
	htCaption   = 2
	idcArrow    = 32512
	smCxScreen  = 0

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	mfGrayed       = 0x0001
	mfChecked      = 0x0008
	mfPopup        = 0x0010
	mfSeparator    = 0x0800
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	dtLeft        = 0x0000
	dtRight       = 0x0002
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtNoClip      = 0x0100
	dtNoPrefix    = 0x0800
	dtEndEllipsis = 0x8000

	bkTransparent        = 1
	srcCopy              = 0x00CC0020
	fwNormal             = 400
	defaultCharset       = 1
	cleartypeQuality     = 5
	monitorDefaultToNull = 0
)

var (
	hwndTopmost             = ^uintptr(0)     // (HWND)-1
	hwndNoTopmost           = ^uintptr(0) - 1 // (HWND)-2
	dpiAwarenessSystemAware = ^uintptr(0) - 1 // DPI_AWARENESS_CONTEXT_SYSTEM_AWARE (-2)
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procSetTimer                      = user32.NewProc("SetTimer")
	procInvalidateRect                = user32.NewProc("InvalidateRect")
	procBeginPaint                    = user32.NewProc("BeginPaint")
	procEndPaint                      = user32.NewProc("EndPaint")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procFillRect                      = user32.NewProc("FillRect")
	procDrawTextW                     = user32.NewProc("DrawTextW")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procMonitorFromPoint              = user32.NewProc("MonitorFromPoint")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procCreateFontW            = gdi32.NewProc("CreateFontW")
)
