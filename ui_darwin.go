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

	"github.com/mareklanduch/gomic/internal/capture"
	"github.com/mareklanduch/gomic/internal/meter"
)

// Cocoa requires the event loop on the process's first thread; locking in
// init keeps main() there.
func init() { runtime.LockOSThread() }

type macUI struct {
	display
	capt    *capture.Capturer
	cfg     Config
	cfgPath string
}

var app *macUI

func runWindow(acc *meter.Accumulator, capt *capture.Capturer, cfg Config, cfgPath string) {
	app = &macUI{display: newDisplay(acc, time.Now()), capt: capt, cfg: cfg, cfgPath: cfgPath}
	C.gomicRun(cBool(cfg.HasPos), C.double(cfg.X), C.double(cfg.Y), cBool(cfg.Topmost))
}

// goTick is called by the window's timer about 30 times a second.
//
//export goTick
func goTick(s *C.MeterState) {
	u := app
	u.tick(time.Now())
	for ch := 0; ch < 2; ch++ {
		s.level[ch] = C.double(u.bars[ch].Level)
		s.hold[ch] = C.double(u.bars[ch].Hold)
		s.clip[ch] = cBool(u.clip[ch])
	}
	name, err := u.capt.Status()
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
	u.cfg.Source = "input"
	_ = u.cfg.Save(u.cfgPath) // losing the saved position is not worth interrupting for
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
