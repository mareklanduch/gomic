//go:build darwin

package capture

import (
	"errors"
	"fmt"
	"time"

	"github.com/gen2brain/malgo"

	"github.com/mareklanduch/gomic/internal/meter"
)

// OutputSupported reports whether this platform can meter playback devices.
// macOS offers no loopback capture of what is playing.
const OutputSupported = false

// Run captures the selected input device forever, reopening it if it stops.
// macOS asks for microphone permission the first time; until it is granted
// the system delivers silence rather than an error.
func (c *Capturer) Run() {
	if c.src != Input {
		c.setStatus("", errors.New("output metering is not available on macOS"))
		return
	}
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		c.setStatus("", fmt.Errorf("could not start audio: %w", err))
		return
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	for {
		infos := c.refreshDevices(ctx)
		if !c.Enabled() {
			c.setStatus("", nil)
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if err := c.session(ctx, infos); err != nil {
			c.setStatus("", err)
			time.Sleep(time.Second)
		}
	}
}

// refreshDevices publishes the connected inputs for the menu and returns
// them as miniaudio describes them.
func (c *Capturer) refreshDevices(ctx *malgo.AllocatedContext) []malgo.DeviceInfo {
	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		infos = nil
	}
	devices := make([]Device, 0, len(infos))
	defaultName := ""
	for i := range infos {
		devices = append(devices, Device{ID: infos[i].ID.String(), Name: infos[i].Name()})
		if infos[i].IsDefault != 0 {
			defaultName = infos[i].Name()
		}
	}
	c.setDevices(devices, defaultName)
	return infos
}

// session meters one input until the selection or the enabled state changes
// (returns nil) or the device stops (returns the error).
func (c *Capturer) session(ctx *malgo.AllocatedContext, infos []malgo.DeviceInfo) error {
	want := c.Selected()

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 0 // keep the device's own channel count; no downmixing
	cfg.SampleRate = 0       // keep the device's own rate

	name := ""
	if want.ID != "" {
		found := false
		for i := range infos {
			if infos[i].ID.String() == want.ID {
				cfg.Capture.DeviceID = infos[i].ID.Pointer()
				name = infos[i].Name()
				found = true
				break
			}
		}
		if !found {
			return checkConnected(want, nil)
		}
	}

	callbacks := malgo.DeviceCallbacks{
		Data: func(_, in []byte, frames uint32) {
			if frames == 0 || len(in) == 0 {
				return
			}
			const bytesPerSample = 4 // 32-bit float
			channels := len(in) / (int(frames) * bytesPerSample)
			c.Acc.Add(meter.Scan(in, meter.Format{Channels: channels, BytesPerSample: bytesPerSample, Float: true}))
		},
	}

	dev, err := malgo.InitDevice(ctx.Context, cfg, callbacks)
	if err != nil {
		return fmt.Errorf("no usable input device: %w", err)
	}
	defer dev.Uninit()

	if err := dev.Start(); err != nil {
		return fmt.Errorf("could not start input: %w", err)
	}

	for {
		current := c.refreshDevices(ctx)
		connected := want.ID == ""
		if want.ID == "" {
			name = "Default input"
		}
		for i := range current {
			if want.ID == "" && current[i].IsDefault != 0 {
				name = current[i].Name()
			}
			if current[i].ID.String() == want.ID {
				connected = true
			}
		}
		if !connected {
			return checkConnected(want, nil)
		}
		c.setStatus(name, nil)

		// Look for a new choice often, so the menu feels immediate.
		for i := 0; i < 10; i++ {
			time.Sleep(100 * time.Millisecond)
			if !c.Enabled() || c.Selected().ID != want.ID {
				return nil
			}
		}
		if !dev.IsStarted() {
			return errors.New("input device stopped")
		}
	}
}
