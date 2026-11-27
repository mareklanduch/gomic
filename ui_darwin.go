//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "ui_darwin.h"
*/
import "C"

import (
	"runtime"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/mareklanduch/gomic/internal/capture"
)

// Cocoa requires the event loop on the process's first thread; locking in
// init keeps main() there.
func init() { runtime.LockOSThread() }

// macOS meters the input only, so the window always shows that one meter.
type macUI struct {
	*meters
	offered []pickerEntry // the device submenu as last shown
}

var app *macUI

func runWindow(m *meters) {
	app = &macUI{meters: m}
	cfg := m.cfg
	C.gomicRun(cBool(cfg.HasPos), C.double(cfg.X), C.double(cfg.Y), cBool(cfg.Topmost))
}

// goTick is called by the window's timer about 30 times a second.
//
//export goTick
func goTick(s *C.MeterState) {
	u := app
	u.tick(time.Now())
	d := &u.disp[capture.Input]
	for ch := 0; ch < 2; ch++ {
		s.level[ch] = C.double(d.bars[ch].Level)
		s.hold[ch] = C.double(d.bars[ch].Hold)
		s.clip[ch] = cBool(d.clip[ch])
	}
	name, err := u.capt[capture.Input].Status()
	message := ""
	if err != nil {
		message = err.Error()
	}
	s.hasError = cBool(err != nil)
	setCString(s.message[:], message)
	setCString(s.device[:], name)
}

// goResetClip is called on double-click and from the menu.
//
//export goResetClip
func goResetClip() { app.resetClip() }

// goSaveState is called when the app quits or the pin setting changes.
// x and y are the window's bottom-left corner in Cocoa screen coordinates.
//
//export goSaveState
func goSaveState(x, y C.double, topmost C.int) {
	u := app
	u.cfg.X, u.cfg.Y, u.cfg.HasPos = int(x), int(y), true
	u.cfg.Topmost = topmost != 0
	_ = u.cfg.Save(u.cfgPath) // losing the saved position is not worth interrupting for
}

// goMenuDevices fills out with the input device submenu (at most max lines)
// and returns how many lines there are. Line 0 is the system default.
//
//export goMenuDevices
func goMenuDevices(out *C.DeviceItem, max C.int) C.int {
	u := app
	u.offered = u.picker(capture.Input)
	if len(u.offered) > int(max) {
		u.offered = u.offered[:max]
	}
	items := unsafe.Slice(out, int(max))
	for i, e := range u.offered {
		setCString(items[i].label[:], e.label)
		items[i].checked = cBool(e.checked)
	}
	return C.int(len(u.offered))
}

// goPickDevice is called when a line of the device submenu is chosen.
//
//export goPickDevice
func goPickDevice(i C.int) {
	u := app
	if i < 0 || int(i) >= len(u.offered) {
		return
	}
	u.pick(capture.Input, u.offered[i].device)
	_ = u.cfg.Save(u.cfgPath)
}

func cBool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// setCString copies s into dst as a NUL-terminated string, cutting it at a
// character boundary if it does not fit.
func setCString(dst []C.char, s string) {
	for len(s) > len(dst)-1 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	for i := 0; i < len(s); i++ {
		dst[i] = C.char(s[i])
	}
	dst[len(s)] = 0
}
