package main

import (
	"math"
	"testing"
	"time"

	"github.com/mareklanduch/gomic/internal/meter"
)

func TestDisplayTickShowsNewestPeakPerChannel(t *testing.T) {
	acc := &meter.Accumulator{}
	start := time.Unix(0, 0)
	d := newDisplay(acc, start)

	acc.Add([2]float32{0.5, 0.25}, [2]bool{})
	d.tick(start.Add(33 * time.Millisecond))

	if math.Abs(d.bars[0].Level-meter.DB(0.5)) > 1e-9 || math.Abs(d.bars[1].Level-meter.DB(0.25)) > 1e-9 {
		t.Fatalf("levels = %v, %v", d.bars[0].Level, d.bars[1].Level)
	}
}

func TestDisplayKeepsClipLitAfterSignalDrops(t *testing.T) {
	acc := &meter.Accumulator{}
	start := time.Unix(0, 0)
	d := newDisplay(acc, start)

	acc.Add([2]float32{1, 0.1}, [2]bool{true, false})
	d.tick(start.Add(33 * time.Millisecond))
	d.tick(start.Add(5 * time.Second)) // silence since

	if d.clip != [2]bool{true, false} {
		t.Fatalf("clip = %v, want [true false]", d.clip)
	}
}

func TestDisplayResetClipTurnsLightsOff(t *testing.T) {
	acc := &meter.Accumulator{}
	start := time.Unix(0, 0)
	d := newDisplay(acc, start)
	acc.Add([2]float32{1, 1}, [2]bool{true, true})
	d.tick(start.Add(33 * time.Millisecond))

	d.resetClip()

	if d.clip != [2]bool{} {
		t.Fatalf("clip = %v, want none", d.clip)
	}
}

func TestDisplayBarFallsByElapsedTimeBetweenTicks(t *testing.T) {
	acc := &meter.Accumulator{}
	start := time.Unix(0, 0)
	d := newDisplay(acc, start)
	acc.Add([2]float32{1, 1}, [2]bool{})
	d.tick(start)

	d.tick(start.Add(time.Second))

	if want := -meter.DecayDBPerSec; math.Abs(d.bars[0].Level-want) > 1e-9 {
		t.Fatalf("level = %v, want %v", d.bars[0].Level, want)
	}
}
