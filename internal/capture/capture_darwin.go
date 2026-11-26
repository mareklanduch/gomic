//go:build darwin

package capture

import (
	"errors"
	"fmt"
	"time"

	"github.com/gen2brain/malgo"

	"github.com/mareklanduch/gomic/internal/meter"
)

// Run captures the default input device forever, reopening it if it stops.
// macOS asks for microphone permission the first time; until it is granted
// the system delivers silence rather than an error.
func (c *Capturer) Run() {
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
		if err := c.session(ctx); err != nil {
			c.setStatus("", err)
		}
		time.Sleep(time.Second)
	}
}

// session meters the default input until the source changes (returns nil)
// or the device stops (returns the error).
func (c *Capturer) session(ctx *malgo.AllocatedContext) error {
	if c.Source() != Input {
		return errors.New("output metering is not available on macOS")
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 0 // keep the device's own channel count; no downmixing
	cfg.SampleRate = 0       // keep the device's own rate

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
		c.setStatus(defaultInputName(ctx), nil)
		time.Sleep(time.Second)
		if c.Source() != Input {
			return nil
		}
		if !dev.IsStarted() {
			return errors.New("input device stopped")
		}
	}
}

func defaultInputName(ctx *malgo.AllocatedContext) string {
	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return "Default input"
	}
	for i := range infos {
		if infos[i].IsDefault != 0 {
			return infos[i].Name()
		}
	}
	return "Default input"
}
