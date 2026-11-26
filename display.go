package main

import (
	"time"

	"github.com/mareklanduch/gomic/internal/meter"
)

// display is what the window shows: two bars and two latching clip lights.
// It is shared by every platform's window code.
type display struct {
	acc      *meter.Accumulator
	bars     [2]meter.Bar
	clip     [2]bool // stays lit until resetClip
	lastTick time.Time
}

func newDisplay(acc *meter.Accumulator, now time.Time) display {
	return display{acc: acc, bars: [2]meter.Bar{meter.NewBar(), meter.NewBar()}, lastTick: now}
}

// tick pulls the peaks captured since the previous tick into the bars.
func (d *display) tick(now time.Time) {
	dt := now.Sub(d.lastTick)
	d.lastTick = now
	peak, clipped := d.acc.Take()
	for ch := range d.bars {
		d.bars[ch].Update(meter.DB(peak[ch]), dt)
		d.clip[ch] = d.clip[ch] || clipped[ch]
	}
}

func (d *display) resetClip() { d.clip = [2]bool{} }
