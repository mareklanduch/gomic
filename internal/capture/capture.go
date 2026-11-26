//go:build windows || darwin

// Package capture reads audio from the default input (or, on Windows, output)
// device and feeds its peaks into a meter.Accumulator. Each platform file
// supplies Capturer.Run.
package capture

import (
	"sync"
	"sync/atomic"

	"github.com/mareklanduch/gomic/internal/meter"
)

// Source selects which default device is metered.
type Source int32

const (
	Input  Source = iota // default recording device (microphone, instrument)
	Output               // default playback device (Windows only)
)

// Capturer keeps a capture session running and follows default-device changes.
type Capturer struct {
	Acc *meter.Accumulator

	source atomic.Int32

	mu   sync.Mutex
	name string
	err  error
}

// New returns a capturer for the given source. Call Run on its own goroutine.
func New(acc *meter.Accumulator, s Source) *Capturer {
	c := &Capturer{Acc: acc}
	c.source.Store(int32(s))
	return c
}

// SetSource switches between input and output; the running session restarts.
func (c *Capturer) SetSource(s Source) { c.source.Store(int32(s)) }

// Source returns the currently selected source.
func (c *Capturer) Source() Source { return Source(c.source.Load()) }

// Status returns the device being metered, or the reason nothing is.
func (c *Capturer) Status() (name string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.name, c.err
}

func (c *Capturer) setStatus(name string, err error) {
	c.mu.Lock()
	c.name, c.err = name, err
	c.mu.Unlock()
}
