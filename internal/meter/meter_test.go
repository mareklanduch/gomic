package meter

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func floatFrames(samples ...float32) []byte {
	b := make([]byte, 0, len(samples)*4)
	for _, s := range samples {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(s))
	}
	return b
}

func int16Frames(samples ...int16) []byte {
	b := make([]byte, 0, len(samples)*2)
	for _, s := range samples {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
	}
	return b
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestScanFloatStereoReportsPeakPerChannel(t *testing.T) {
	// frames: (L, R)
	data := floatFrames(0.1, -0.5, -0.25, 0.2)
	peak, clipped := Scan(data, Format{Channels: 2, BytesPerSample: 4, Float: true})
	if !near(peak[0], 0.25) || !near(peak[1], 0.5) {
		t.Fatalf("peak = %v, want [0.25 0.5]", peak)
	}
	if clipped[0] || clipped[1] {
		t.Fatalf("clipped = %v, want none", clipped)
	}
}

func TestScanFlagsOnlyTheChannelThatReachesFullScale(t *testing.T) {
	data := floatFrames(0.3, 1.0, 0.3, 0.2)
	_, clipped := Scan(data, Format{Channels: 2, BytesPerSample: 4, Float: true})
	if clipped[0] || !clipped[1] {
		t.Fatalf("clipped = %v, want [false true]", clipped)
	}
}

func TestScanDoesNotFlagALoudButLegalPeak(t *testing.T) {
	// -0.1 dBFS, a common mastering ceiling
	data := floatFrames(0.9886, -0.9886)
	_, clipped := Scan(data, Format{Channels: 2, BytesPerSample: 4, Float: true})
	if clipped[0] || clipped[1] {
		t.Fatalf("clipped = %v, want none", clipped)
	}
}

func TestScanInt16FullScaleNegativeClips(t *testing.T) {
	data := int16Frames(-32768, 16384)
	peak, clipped := Scan(data, Format{Channels: 2, BytesPerSample: 2})
	if !clipped[0] || clipped[1] {
		t.Fatalf("clipped = %v, want [true false]", clipped)
	}
	if !near(peak[1], 0.5) {
		t.Fatalf("peak[1] = %v, want 0.5", peak[1])
	}
}

func TestScanInt24(t *testing.T) {
	// 0x400000 = half scale, little endian 3 bytes; mono
	data := []byte{0x00, 0x00, 0x40}
	peak, _ := Scan(data, Format{Channels: 1, BytesPerSample: 3})
	if !near(peak[0], 0.5) {
		t.Fatalf("peak[0] = %v, want 0.5", peak[0])
	}
}

func TestScanInt32(t *testing.T) {
	data := binary.LittleEndian.AppendUint32(nil, uint32(1<<30))
	peak, _ := Scan(data, Format{Channels: 1, BytesPerSample: 4})
	if !near(peak[0], 0.5) {
		t.Fatalf("peak[0] = %v, want 0.5", peak[0])
	}
}

func TestScanMonoFillsBothChannels(t *testing.T) {
	data := floatFrames(0.4, -0.7)
	peak, _ := Scan(data, Format{Channels: 1, BytesPerSample: 4, Float: true})
	if !near(peak[0], 0.7) || !near(peak[1], 0.7) {
		t.Fatalf("peak = %v, want [0.7 0.7]", peak)
	}
}

func TestScanIgnoresChannelsBeyondTheFirstTwo(t *testing.T) {
	// 4 channels, one frame; channels 3 and 4 are loud
	data := floatFrames(0.1, 0.2, 1.0, 1.0)
	peak, clipped := Scan(data, Format{Channels: 4, BytesPerSample: 4, Float: true})
	if !near(peak[0], 0.1) || !near(peak[1], 0.2) || clipped[0] || clipped[1] {
		t.Fatalf("peak = %v clipped = %v", peak, clipped)
	}
}

func TestScanIgnoresTrailingPartialFrame(t *testing.T) {
	data := append(floatFrames(0.1, 0.2), 0xFF, 0xFF)
	peak, _ := Scan(data, Format{Channels: 2, BytesPerSample: 4, Float: true})
	if !near(peak[0], 0.1) || !near(peak[1], 0.2) {
		t.Fatalf("peak = %v", peak)
	}
}

func TestDBConvertsLinearToDecibels(t *testing.T) {
	if got := DB(1); math.Abs(got) > 1e-9 {
		t.Fatalf("DB(1) = %v, want 0", got)
	}
	if got := DB(0.5); math.Abs(got-(-6.0206)) > 1e-3 {
		t.Fatalf("DB(0.5) = %v, want -6.02", got)
	}
	if got := DB(0); got != MinDB {
		t.Fatalf("DB(0) = %v, want MinDB", got)
	}
}

func TestAccumulatorTakeReturnsMaxSinceLastTakeAndResets(t *testing.T) {
	var a Accumulator
	a.Add([2]float32{0.2, 0.9}, [2]bool{false, false})
	a.Add([2]float32{0.6, 0.1}, [2]bool{true, false})
	peak, clipped := a.Take()
	if !near(peak[0], 0.6) || !near(peak[1], 0.9) || !clipped[0] || clipped[1] {
		t.Fatalf("first take: peak = %v clipped = %v", peak, clipped)
	}
	peak, clipped = a.Take()
	if peak != [2]float32{} || clipped != [2]bool{} {
		t.Fatalf("second take: peak = %v clipped = %v, want zero", peak, clipped)
	}
}

func TestBarJumpsUpImmediately(t *testing.T) {
	b := NewBar()
	b.Update(DB(0.5), 30*time.Millisecond)
	if math.Abs(b.Level-DB(0.5)) > 1e-9 {
		t.Fatalf("Level = %v, want %v", b.Level, DB(0.5))
	}
}

func TestBarFallsAtDecayRateWhenSignalDrops(t *testing.T) {
	b := NewBar()
	b.Update(-6, 0)
	b.Update(MinDB, 500*time.Millisecond)
	want := -6 - DecayDBPerSec*0.5
	if math.Abs(b.Level-want) > 1e-9 {
		t.Fatalf("Level = %v, want %v", b.Level, want)
	}
}

func TestBarNeverFallsBelowMinDB(t *testing.T) {
	b := NewBar()
	b.Update(-6, 0)
	b.Update(MinDB, time.Hour)
	if b.Level != MinDB {
		t.Fatalf("Level = %v, want MinDB", b.Level)
	}
}

func TestBarHoldKeepsHighestPeakForHoldTime(t *testing.T) {
	b := NewBar()
	b.Update(-3, 0)
	b.Update(-40, HoldTime-10*time.Millisecond)
	if b.Hold != -3 {
		t.Fatalf("Hold = %v, want -3 while holding", b.Hold)
	}
}

func TestBarHoldDropsToLevelAfterHoldTime(t *testing.T) {
	b := NewBar()
	b.Update(-3, 0)
	b.Update(-40, HoldTime+10*time.Millisecond)
	if b.Hold != b.Level {
		t.Fatalf("Hold = %v, want it to drop to Level %v", b.Hold, b.Level)
	}
}
