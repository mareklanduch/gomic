//! Audio capture: one background thread per source keeps a stream open on
//! the chosen device and feeds its peaks to the meter.
//!
//! Capturing an *output* device records what is playing through it
//! (loopback). That needs WASAPI on Windows or macOS 14.6 or newer.

use std::fmt;
use std::sync::{Arc, Mutex, MutexGuard};
use std::thread;
use std::time::{Duration, Instant};

use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use cpal::{ErrorKind, FromSample, SampleFormat, SizedSample};

use crate::meter::{Accumulator, Peaks};

/// Whether this platform can meter what is playing through an output device.
pub const OUTPUT_SUPPORTED: bool = cfg!(any(target_os = "windows", target_os = "macos"));

/// Which side of the sound system a capturer meters.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Source {
    /// Recording devices (microphone, instrument).
    Input,
    /// Playback devices.
    Output,
}

impl Source {
    /// Both sources, in the order their meters are stacked.
    pub const ALL: [Self; 2] = [Self::Input, Self::Output];

    /// Position in arrays that hold one value per source.
    pub const fn index(self) -> usize {
        self as usize
    }
}

impl fmt::Display for Source {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.pad(match self {
            Self::Input => "input",
            Self::Output => "output",
        })
    }
}

/// One audio device, identified by the stable id the audio backend gives it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Device {
    pub id: String,
    pub name: String,
}

/// What a capturer is doing right now.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub enum Status {
    /// Not metering: the meter is hidden or capture has not started yet.
    #[default]
    Idle,
    /// Metering the named device.
    Metering(String),
    /// Nothing is being metered, for this reason.
    Failed(String),
}

/// The connected devices on one side, as of about a second ago.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Devices {
    pub all: Vec<Device>,
    /// Name of the current system default, if there is one.
    pub default_name: Option<String>,
}

#[derive(Debug, Default)]
struct State {
    /// The chosen device; `None` follows the system default.
    want: Option<Device>,
    enabled: bool,
    devices: Devices,
    status: Status,
}

#[derive(Debug)]
struct Shared {
    source: Source,
    peaks: Accumulator,
    state: Mutex<State>,
}

impl Shared {
    fn state(&self) -> MutexGuard<'_, State> {
        // The state stays consistent even if a holder panicked.
        self.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
    }

    fn set_status(&self, status: Status) {
        self.state().status = status;
    }
}

/// Keeps a capture session running for one source. It meters the selected
/// device while enabled, and keeps the list of connected devices fresh
/// either way. Cloning is cheap and refers to the same capturer.
#[derive(Debug, Clone)]
pub struct Capturer(Arc<Shared>);

impl Capturer {
    /// Starts the capture thread for `source`.
    pub fn spawn(source: Source, want: Option<Device>, enabled: bool) -> Self {
        let shared = Arc::new(Shared {
            source,
            peaks: Accumulator::default(),
            state: Mutex::new(State { want, enabled, ..State::default() }),
        });
        let worker = Arc::clone(&shared);
        thread::Builder::new()
            .name(format!("capture-{source}"))
            .spawn(move || run(&worker))
            .expect("the system can start a thread");
        Self(shared)
    }

    /// Switches to another device; the running session restarts.
    /// `None` follows the system default.
    pub fn select(&self, device: Option<Device>) {
        self.0.state().want = device;
    }

    /// The device chosen with [`Capturer::select`].
    pub fn selected(&self) -> Option<Device> {
        self.0.state().want.clone()
    }

    /// Starts or stops metering. A stopped capturer holds no device open.
    pub fn set_enabled(&self, enabled: bool) {
        self.0.state().enabled = enabled;
    }

    /// The connected devices and the current default.
    pub fn devices(&self) -> Devices {
        self.0.state().devices.clone()
    }

    /// The device being metered, or the reason nothing is.
    pub fn status(&self) -> Status {
        self.0.state().status.clone()
    }

    /// The peaks captured since the previous call.
    pub fn take_peaks(&self) -> Peaks {
        self.0.peaks.take()
    }
}

/// How often a session looks for a new choice, so the menu feels immediate.
const POLL: Duration = Duration::from_millis(50);
/// How often the device list and the system default are checked.
const REFRESH: Duration = Duration::from_secs(1);

fn run(shared: &Arc<Shared>) {
    let host = cpal::default_host();
    loop {
        refresh_devices(&host, shared);
        if !shared.state().enabled {
            shared.set_status(Status::Idle);
            thread::sleep(POLL * 5);
            continue;
        }
        if let Err(reason) = session(&host, shared) {
            shared.set_status(Status::Failed(reason));
            thread::sleep(REFRESH);
        }
    }
}

/// Meters one device until the selection, the default device, or the enabled
/// state changes (`Ok`) or something fails (`Err` with the reason to show).
fn session(host: &cpal::Host, shared: &Arc<Shared>) -> Result<(), String> {
    let source = shared.source;
    let want = shared.state().want.clone();
    let device = open_device(host, source, want.as_ref())?;
    let opened_id = device_id(&device);

    let config = match source {
        Source::Input => device.default_input_config(),
        // Opening an output device for input is how loopback is requested.
        Source::Output => device.default_output_config(),
    }
    .map_err(|err| format!("could not read the {source} format: {err}"))?;

    let failure = Arc::new(Mutex::new(None));
    let stream = build_stream(&device, &config, shared, &failure)
        .map_err(|err| format!("could not open the {source} device: {err}"))?;
    stream.play().map_err(|err| format!("could not start {source} capture: {err}"))?;
    shared.set_status(Status::Metering(device_name(&device)));

    let mut last_refresh = Instant::now();
    loop {
        thread::sleep(POLL);
        {
            let state = shared.state();
            if !state.enabled || state.want.as_ref().map(|d| &d.id) != want.as_ref().map(|d| &d.id) {
                return Ok(());
            }
        }
        if let Some(reason) = failure.lock().unwrap_or_else(|p| p.into_inner()).take() {
            return Err(reason);
        }
        if last_refresh.elapsed() >= REFRESH {
            last_refresh = Instant::now();
            let devices = refresh_devices(host, shared);
            match &want {
                Some(device) => check_connected(device, &devices.all)?,
                // Following the default: start over when it moves elsewhere.
                None => {
                    let current = default_device(host, source).as_ref().and_then(device_id);
                    if current != opened_id {
                        return Ok(());
                    }
                }
            }
        }
    }
}

fn open_device(host: &cpal::Host, source: Source, want: Option<&Device>) -> Result<cpal::Device, String> {
    let Some(want) = want else {
        return default_device(host, source).ok_or_else(|| format!("no default {source} device"));
    };
    want.id.parse().ok().and_then(|id| host.device_by_id(&id)).ok_or_else(|| not_connected(want))
}

fn default_device(host: &cpal::Host, source: Source) -> Option<cpal::Device> {
    match source {
        Source::Input => host.default_input_device(),
        Source::Output => host.default_output_device(),
    }
}

/// Publishes the connected devices for the menu and returns them.
fn refresh_devices(host: &cpal::Host, shared: &Shared) -> Devices {
    let all = match shared.source {
        Source::Input => host.input_devices().map(|devices| list(devices)),
        Source::Output => host.output_devices().map(|devices| list(devices)),
    }
    .unwrap_or_default();
    let default_name = default_device(host, shared.source).as_ref().map(device_name);
    let devices = Devices { all, default_name };
    shared.state().devices = devices.clone();
    devices
}

fn list(devices: impl Iterator<Item = cpal::Device>) -> Vec<Device> {
    devices
        .filter_map(|device| Some(Device { id: device_id(&device)?, name: device_name(&device) }))
        .collect()
}

fn device_id(device: &cpal::Device) -> Option<String> {
    device.id().ok().map(|id| id.to_string())
}

fn device_name(device: &cpal::Device) -> String {
    device
        .description()
        .map_or_else(|_| "Unknown device".to_owned(), |description| description.name().to_owned())
}

/// Fails with a message naming `want` if it is not among `connected`.
fn check_connected(want: &Device, connected: &[Device]) -> Result<(), String> {
    if connected.iter().any(|device| device.id == want.id) { Ok(()) } else { Err(not_connected(want)) }
}

fn not_connected(want: &Device) -> String {
    let name = if want.name.is_empty() { "The chosen device" } else { &want.name };
    format!("{name} is not connected")
}

type Failure = Arc<Mutex<Option<String>>>;

/// Opens a capture stream in the device's own sample format.
fn build_stream(
    device: &cpal::Device,
    config: &cpal::SupportedStreamConfig,
    shared: &Arc<Shared>,
    failure: &Failure,
) -> Result<cpal::Stream, cpal::Error> {
    macro_rules! typed {
        ($sample:ty) => {
            build_typed_stream::<$sample>(device, config.config(), shared, failure)
        };
    }
    match config.sample_format() {
        SampleFormat::I8 => typed!(i8),
        SampleFormat::I16 => typed!(i16),
        SampleFormat::I24 => typed!(cpal::I24),
        SampleFormat::I32 => typed!(i32),
        SampleFormat::I64 => typed!(i64),
        SampleFormat::U8 => typed!(u8),
        SampleFormat::U16 => typed!(u16),
        SampleFormat::U24 => typed!(cpal::U24),
        SampleFormat::U32 => typed!(u32),
        SampleFormat::U64 => typed!(u64),
        SampleFormat::F32 => typed!(f32),
        SampleFormat::F64 => typed!(f64),
        other => Err(cpal::Error::with_message(
            ErrorKind::UnsupportedConfig,
            format!("unsupported sample format {other}"),
        )),
    }
}

fn build_typed_stream<T>(
    device: &cpal::Device,
    config: cpal::StreamConfig,
    shared: &Arc<Shared>,
    failure: &Failure,
) -> Result<cpal::Stream, cpal::Error>
where
    T: SizedSample,
    f32: FromSample<T>,
{
    let channels = usize::from(config.channels);
    let shared = Arc::clone(shared);
    let failure = Arc::clone(failure);
    device.build_input_stream(
        config,
        move |data: &[T], _: &cpal::InputCallbackInfo| {
            let samples = data.iter().map(|&sample| f32::from_sample_(sample));
            shared.peaks.add(Peaks::scan(samples, channels));
        },
        move |err| match err.kind() {
            // A late buffer or a refused priority boost does not stop the stream.
            ErrorKind::Xrun | ErrorKind::RealtimeDenied => {}
            _ => *failure.lock().unwrap_or_else(|p| p.into_inner()) = Some(err.to_string()),
        },
        None,
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn device(id: &str, name: &str) -> Device {
        Device { id: id.into(), name: name.into() }
    }

    #[test]
    fn a_connected_device_passes_the_check() {
        let connected = [device("a", "USB Mic"), device("b", "Webcam")];
        assert_eq!(check_connected(&connected[1], &connected), Ok(()));
    }

    #[test]
    fn a_missing_device_is_named_in_the_error() {
        let err = check_connected(&device("z", "Interface"), &[device("a", "USB Mic")]);
        assert_eq!(err, Err("Interface is not connected".to_owned()));
    }

    #[test]
    fn a_missing_device_without_a_name_still_gets_a_message() {
        assert_eq!(not_connected(&device("z", "")), "The chosen device is not connected");
    }

    #[test]
    fn sources_index_their_arrays_in_stacking_order() {
        assert_eq!(Source::ALL.map(Source::index), [0, 1]);
        assert_eq!(Source::Output.to_string(), "output");
    }
}
