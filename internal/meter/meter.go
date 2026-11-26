// Package meter holds the platform-independent parts of the level meter:
// scanning raw sample buffers for peaks, and the bar's rise/fall behavior.
package meter

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

const (
	// ClipThreshold is the normalized magnitude at or above which a sample
	// counts as clipped (about -0.009 dBFS). Slightly below 1.0 so that
	// converters and limiters that stop a hair short of full scale still count.
	ClipThreshold = 0.999

	// MinDB is the floor used for silence.
	MinDB = -90.0

	// DecayDBPerSec is how fast the bar falls once the signal drops.
	DecayDBPerSec = 24.0

	// HoldTime is how long the peak-hold marker stays at its highest value.
	HoldTime = 1500 * time.Millisecond
)

// Format describes interleaved little-endian PCM as delivered by the device.
type Format struct {
	Channels       int
	BytesPerSample int  // 2, 3 or 4
	Float          bool // 32-bit IEEE float when true
}

// Scan returns the peak magnitude (1.0 = full scale) and clip flag for the
// first two channels of data. Mono input is reported on both channels.
func Scan(data []byte, f Format) (peak [2]float32, clipped [2]bool) {
	if f.Channels < 1 || f.BytesPerSample < 1 {
		return
	}
	frame := f.Channels * f.BytesPerSample
	n := min(f.Channels, 2)
	for off := 0; off+frame <= len(data); off += frame {
		for ch := 0; ch < n; ch++ {
			v := sample(data[off+ch*f.BytesPerSample:], f)
			if v < 0 {
				v = -v
			}
			if v > peak[ch] {
				peak[ch] = v
			}
		}
	}
	if f.Channels == 1 {
		peak[1] = peak[0]
	}
	for ch := range peak {
		clipped[ch] = peak[ch] >= ClipThreshold
	}
	return
}

func sample(b []byte, f Format) float32 {
	switch {
	case f.Float && f.BytesPerSample == 4:
		return math.Float32frombits(binary.LittleEndian.Uint32(b))
	case f.BytesPerSample == 2:
		return float32(int16(binary.LittleEndian.Uint16(b))) / (1 << 15)
	case f.BytesPerSample == 3:
		v := int32(b[0]) | int32(b[1])<<8 | int32(int8(b[2]))<<16
		return float32(v) / (1 << 23)
	case f.BytesPerSample == 4:
		return float32(float64(int32(binary.LittleEndian.Uint32(b))) / (1 << 31))
	}
	return 0
}

// DB converts a linear magnitude to decibels relative to full scale.
func DB(linear float32) float64 {
	if linear <= 0 {
		return MinDB
	}
	return math.Max(MinDB, 20*math.Log10(float64(linear)))
}

// Accumulator carries peaks from the audio thread to the UI thread.
type Accumulator struct {
	mu      sync.Mutex
	peak    [2]float32
	clipped [2]bool
}

// Add merges one scanned buffer into the running maximum.
func (a *Accumulator) Add(peak [2]float32, clipped [2]bool) {
	a.mu.Lock()
	for ch := range peak {
		a.peak[ch] = max(a.peak[ch], peak[ch])
		a.clipped[ch] = a.clipped[ch] || clipped[ch]
	}
	a.mu.Unlock()
}

// Take returns everything seen since the previous Take and resets.
func (a *Accumulator) Take() (peak [2]float32, clipped [2]bool) {
	a.mu.Lock()
	peak, clipped = a.peak, a.clipped
	a.peak, a.clipped = [2]float32{}, [2]bool{}
	a.mu.Unlock()
	return
}

// Bar is the displayed state of one channel: a level that rises instantly
// and falls slowly, plus a peak-hold marker.
type Bar struct {
	Level   float64 // dBFS
	Hold    float64 // dBFS
	holdAge time.Duration
}

// NewBar returns a silent bar.
func NewBar() Bar { return Bar{Level: MinDB, Hold: MinDB} }

// Update feeds the newest peak (in dBFS) and the time since the last update.
func (b *Bar) Update(db float64, dt time.Duration) {
	fallen := b.Level - DecayDBPerSec*dt.Seconds()
	b.Level = math.Max(MinDB, math.Max(db, fallen))
	if b.Level >= b.Hold {
		b.Hold, b.holdAge = b.Level, 0
		return
	}
	b.holdAge += dt
	if b.holdAge > HoldTime {
		b.Hold, b.holdAge = b.Level, 0
	}
}
