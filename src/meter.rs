//! The platform-independent parts of the level meter: scanning sample
//! buffers for peaks, and the bar's rise/fall behavior.

use std::sync::Mutex;
use std::time::{Duration, Instant};

/// The normalized magnitude at or above which a sample counts as clipped
/// (about -0.009 dBFS). Slightly below 1.0 so that converters and limiters
/// that stop a hair short of full scale still count.
pub const CLIP_THRESHOLD: f32 = 0.999;

/// The floor used for silence.
pub const MIN_DB: f64 = -90.0;

/// How fast the bar falls once the signal drops.
pub const DECAY_DB_PER_SEC: f64 = 24.0;

/// How long the peak-hold marker stays at its highest value.
pub const HOLD_TIME: Duration = Duration::from_millis(1500);

/// The loudest sample seen on each of the two metered channels.
#[derive(Debug, Clone, Copy, Default, PartialEq)]
pub struct Peaks {
    /// Peak magnitude per channel; 1.0 is full scale.
    pub level: [f32; 2],
    /// Whether the channel reached [`CLIP_THRESHOLD`].
    pub clipped: [bool; 2],
}

impl Peaks {
    /// Scans interleaved samples for the peaks of the first two channels.
    /// Mono input is reported on both channels.
    pub fn scan(samples: impl IntoIterator<Item = f32>, channels: usize) -> Self {
        let mut peaks = Self::default();
        if channels == 0 {
            return peaks;
        }
        for (i, sample) in samples.into_iter().enumerate() {
            let channel = i % channels;
            if channel < 2 {
                peaks.level[channel] = peaks.level[channel].max(sample.abs());
            }
        }
        if channels == 1 {
            peaks.level[1] = peaks.level[0];
        }
        peaks.clipped = peaks.level.map(|level| level >= CLIP_THRESHOLD);
        peaks
    }

    fn merge(&mut self, other: Self) {
        for channel in 0..2 {
            self.level[channel] = self.level[channel].max(other.level[channel]);
            self.clipped[channel] |= other.clipped[channel];
        }
    }
}

/// Converts a linear magnitude to decibels relative to full scale.
pub fn db(linear: f32) -> f64 {
    if linear <= 0.0 {
        return MIN_DB;
    }
    (20.0 * f64::from(linear).log10()).max(MIN_DB)
}

/// Carries peaks from the audio thread to the UI thread.
#[derive(Debug, Default)]
pub struct Accumulator(Mutex<Peaks>);

impl Accumulator {
    /// Merges one scanned buffer into the running maximum.
    pub fn add(&self, peaks: Peaks) {
        self.lock().merge(peaks);
    }

    /// Returns everything seen since the previous `take` and resets.
    pub fn take(&self) -> Peaks {
        std::mem::take(&mut *self.lock())
    }

    fn lock(&self) -> std::sync::MutexGuard<'_, Peaks> {
        // A panic while holding the lock cannot leave the peaks half-updated
        // in a way that matters, so a poisoned lock is still usable.
        self.0.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
    }
}

/// The displayed state of one channel: a level that rises instantly and
/// falls slowly, plus a peak-hold marker.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Bar {
    /// Current bar position, in dBFS.
    pub level: f64,
    /// Peak-hold marker, in dBFS.
    pub hold: f64,
    hold_age: Duration,
}

impl Default for Bar {
    /// A silent bar.
    fn default() -> Self {
        Self { level: MIN_DB, hold: MIN_DB, hold_age: Duration::ZERO }
    }
}

impl Bar {
    /// Feeds the newest peak (in dBFS) and the time since the last update.
    pub fn update(&mut self, db: f64, dt: Duration) {
        let fallen = self.level - DECAY_DB_PER_SEC * dt.as_secs_f64();
        self.level = db.max(fallen).max(MIN_DB);
        if self.level >= self.hold {
            self.hold = self.level;
            self.hold_age = Duration::ZERO;
            return;
        }
        self.hold_age += dt;
        if self.hold_age > HOLD_TIME {
            self.hold = self.level;
            self.hold_age = Duration::ZERO;
        }
    }
}

/// What one meter shows: two bars and two latching clip lights.
#[derive(Debug, Clone, Copy)]
pub struct Display {
    pub bars: [Bar; 2],
    /// Stays lit until [`Display::reset_clip`].
    pub clip: [bool; 2],
    last_tick: Instant,
}

impl Display {
    /// A silent display whose clock starts at `now`.
    pub fn new(now: Instant) -> Self {
        Self { bars: [Bar::default(); 2], clip: [false; 2], last_tick: now }
    }

    /// Feeds the peaks captured since the previous tick into the bars.
    pub fn tick(&mut self, now: Instant, peaks: Peaks) {
        let dt = now.saturating_duration_since(self.last_tick);
        self.last_tick = now;
        for channel in 0..2 {
            self.bars[channel].update(db(peaks.level[channel]), dt);
            self.clip[channel] |= peaks.clipped[channel];
        }
    }

    /// Turns the clip lights off.
    pub fn reset_clip(&mut self) {
        self.clip = [false; 2];
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn close(a: f64, b: f64) -> bool {
        (a - b).abs() < 1e-9
    }

    #[test]
    fn scan_finds_the_peak_of_each_stereo_channel() {
        let peaks = Peaks::scan([0.1, -0.5, -0.3, 0.2], 2);
        assert_eq!(peaks.level, [0.3, 0.5]);
        assert_eq!(peaks.clipped, [false, false]);
    }

    #[test]
    fn scan_reports_mono_on_both_channels() {
        let peaks = Peaks::scan([0.25, -0.75], 1);
        assert_eq!(peaks.level, [0.75, 0.75]);
    }

    #[test]
    fn scan_ignores_channels_beyond_the_first_two() {
        let peaks = Peaks::scan([0.1, 0.2, 0.9, 0.9], 4);
        assert_eq!(peaks.level, [0.1, 0.2]);
    }

    #[test]
    fn scan_flags_full_scale_as_clipped_per_channel() {
        let peaks = Peaks::scan([1.0, 0.5, -0.9995, 0.998], 2);
        assert_eq!(peaks.clipped, [true, false]);
    }

    #[test]
    fn scan_of_no_channels_is_silence() {
        assert_eq!(Peaks::scan([1.0, 1.0], 0), Peaks::default());
    }

    #[test]
    fn db_maps_full_scale_to_zero_and_silence_to_the_floor() {
        assert!(close(db(1.0), 0.0));
        assert!(close(db(0.5), -6.020_599_913_279_624));
        assert!(close(db(0.0), MIN_DB));
        assert!(close(db(1e-9), MIN_DB));
    }

    #[test]
    fn accumulator_keeps_the_maximum_until_taken() {
        let acc = Accumulator::default();
        acc.add(Peaks { level: [0.2, 0.9], clipped: [false, false] });
        acc.add(Peaks { level: [1.0, 0.1], clipped: [true, false] });

        assert_eq!(acc.take(), Peaks { level: [1.0, 0.9], clipped: [true, false] });
        assert_eq!(acc.take(), Peaks::default());
    }

    #[test]
    fn bar_rises_instantly_and_falls_at_the_decay_rate() {
        let mut bar = Bar::default();
        bar.update(-6.0, Duration::from_millis(33));
        assert!(close(bar.level, -6.0));

        bar.update(MIN_DB, Duration::from_secs(1));
        assert!(close(bar.level, -6.0 - DECAY_DB_PER_SEC));
    }

    #[test]
    fn bar_hold_stays_at_the_peak_then_drops_to_the_level() {
        let mut bar = Bar::default();
        bar.update(-3.0, Duration::from_millis(33));
        bar.update(MIN_DB, Duration::from_secs(1));
        assert!(close(bar.hold, -3.0));

        bar.update(MIN_DB, Duration::from_secs(1));
        assert!(close(bar.hold, bar.level));
    }

    #[test]
    fn display_keeps_clip_lit_after_the_signal_drops_until_reset() {
        let start = Instant::now();
        let mut display = Display::new(start);
        display.tick(start + Duration::from_millis(33), Peaks { level: [1.0, 0.1], clipped: [true, false] });
        display.tick(start + Duration::from_secs(5), Peaks::default());
        assert_eq!(display.clip, [true, false]);

        display.reset_clip();
        assert_eq!(display.clip, [false, false]);
    }

    #[test]
    fn display_bar_falls_by_the_time_between_ticks() {
        let start = Instant::now();
        let mut display = Display::new(start);
        display.tick(start, Peaks { level: [1.0, 1.0], clipped: [false, false] });
        display.tick(start + Duration::from_secs(1), Peaks::default());
        assert!(close(display.bars[0].level, -DECAY_DB_PER_SEC));
    }
}
