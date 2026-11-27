//go:build windows || darwin

// Package capture reads audio from an input or output device and feeds its
// peaks into a meter.Accumulator. Each platform file supplies Capturer.Run
// and OutputSupported.
package capture

import (
	"fmt"
	"sync"

	"github.com/mareklanduch/gomic/internal/meter"
)

// Source says which side of the sound system a capturer meters.
type Source int

const (
	Input  Source = iota // recording devices (microphone, instrument)
	Output               // playback devices (Windows only)
)

func (s Source) String() string {
	if s == Output {
		return "output"
	}
	return "input"
}

// Device is one connected device. An empty ID means "whatever the system
// default is", which the capturer keeps following as the default changes.
type Device struct {
	ID   string
	Name string
}

// Capturer keeps a capture session running for one source. It meters the
// selected device while enabled, and keeps the list of connected devices
// fresh either way.
type Capturer struct {
	Acc *meter.Accumulator
	src Source

	mu          sync.Mutex
	want        Device // ID "" follows the default
	enabled     bool
	devices     []Device
	defaultName string
	name        string
	err         error
}

// New returns a capturer for the given source. Call Run on its own goroutine.
func New(acc *meter.Accumulator, src Source, want Device, enabled bool) *Capturer {
	return &Capturer{Acc: acc, src: src, want: want, enabled: enabled}
}

// Source returns which side this capturer meters.
func (c *Capturer) Source() Source { return c.src }

// Select switches to another device; the running session restarts. The name
// is only used to say which device is missing if it is not connected.
func (c *Capturer) Select(d Device) {
	c.mu.Lock()
	c.want = d
	c.mu.Unlock()
}

// Selected returns the device chosen with Select.
func (c *Capturer) Selected() Device {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.want
}

// SetEnabled starts or stops metering. A stopped capturer holds no device open.
func (c *Capturer) SetEnabled(on bool) {
	c.mu.Lock()
	c.enabled = on
	c.mu.Unlock()
}

// Enabled reports whether the capturer is metering.
func (c *Capturer) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// Devices returns the connected devices and the name of the current default
// one, as of about a second ago.
func (c *Capturer) Devices() (devices []Device, defaultName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Device(nil), c.devices...), c.defaultName
}

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

func (c *Capturer) setDevices(devices []Device, defaultName string) {
	c.mu.Lock()
	c.devices, c.defaultName = devices, defaultName
	c.mu.Unlock()
}

// checkConnected returns an error naming the wanted device if it is a
// specific one that is not in the list of connected devices.
func checkConnected(want Device, devices []Device) error {
	if want.ID == "" {
		return nil
	}
	for _, d := range devices {
		if d.ID == want.ID {
			return nil
		}
	}
	name := want.Name
	if name == "" {
		name = "The chosen device"
	}
	return fmt.Errorf("%s is not connected", name)
}
