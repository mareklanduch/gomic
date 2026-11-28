//! gomic is a small always-on-top stereo peak meter with latching clip
//! lights for an input device and an output device of your choice.

// A release build on Windows is a GUI program: no console window opens next
// to the meter.
#![cfg_attr(all(target_os = "windows", not(debug_assertions)), windows_subsystem = "windows")]

mod app;
mod capture;
mod config;
mod menu;
mod meter;

use std::thread;
use std::time::{Duration, Instant};

use anyhow::{Result, anyhow, bail};
use clap::{Parser, ValueEnum};

use crate::capture::{Capturer, OUTPUT_SUPPORTED, Source, Status};
use crate::config::Config;
use crate::meter::db;

/// Release builds are stamped with the release tag; other builds use the
/// version in Cargo.toml.
const VERSION: &str = match option_env!("GOMIC_VERSION") {
    Some(version) => version,
    None => env!("CARGO_PKG_VERSION"),
};

/// A small always-on-top stereo peak meter with latching clip lights.
#[derive(Debug, Parser)]
#[command(version = VERSION, about)]
struct Args {
    /// Which meters to show, overriding the saved choice.
    #[arg(long, value_enum)]
    source: Option<Show>,

    /// Print levels to the console for this long (for example "5s") instead
    /// of opening the window.
    #[arg(long, value_name = "DURATION", value_parser = humantime::parse_duration)]
    probe: Option<Duration>,

    /// Forget the saved window position and start in the default place.
    #[arg(long)]
    reset_position: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum)]
enum Show {
    Input,
    Output,
    Both,
}

fn main() -> Result<()> {
    attach_parent_console();
    let args = Args::parse();

    let path = Config::path();
    let mut config = Config::load(&path);
    if let Some(show) = args.source {
        config.show_input = show != Show::Output;
        config.show_output = show != Show::Input;
    }
    if !OUTPUT_SUPPORTED {
        if config.show_output && args.source.is_some() {
            bail!("output metering is not available on this system");
        }
        config.show_input = true;
        config.show_output = false;
    }
    if args.reset_position {
        config.has_pos = false;
    }

    let capturers =
        Source::ALL.map(|source| Capturer::spawn(source, config.device(source), config.shown(source)));

    if let Some(duration) = args.probe {
        probe(&config, &capturers, duration);
        return Ok(());
    }

    let options = eframe::NativeOptions { viewport: app::viewport(&config), ..Default::default() };
    eframe::run_native(
        "gomic",
        options,
        Box::new(move |_| Ok(Box::new(app::App::new(config, path, capturers)))),
    )
    .map_err(|err| anyhow!("could not open the window: {err}"))
}

/// Prints what the meters would show, for checking capture without the window.
fn probe(config: &Config, capturers: &[Capturer; 2], duration: Duration) {
    let end = Instant::now() + duration;
    while Instant::now() < end {
        thread::sleep(Duration::from_millis(250));
        for source in config.shown_sources() {
            let capturer = &capturers[source.index()];
            match capturer.status() {
                Status::Idle => println!("{source:<6} starting"),
                Status::Failed(reason) => println!("{source:<6} error: {reason}"),
                Status::Metering(name) => {
                    let peaks = capturer.take_peaks();
                    let clip = |clipped| if clipped { "CLIP" } else { "" };
                    println!(
                        "{source:<6} {name:<40} L {:6.1} dB {:<4}  R {:6.1} dB {:<4}",
                        db(peaks.level[0]),
                        clip(peaks.clipped[0]),
                        db(peaks.level[1]),
                        clip(peaks.clipped[1]),
                    );
                }
            }
        }
    }
}

/// A Windows GUI program has no console of its own. When it was started from
/// a terminal, borrow that one so `--help` and `--probe` have somewhere to print.
fn attach_parent_console() {
    #[cfg(target_os = "windows")]
    {
        use windows_sys::Win32::System::Console::{ATTACH_PARENT_PROCESS, AttachConsole};
        // SAFETY: the call takes no pointers; it fails harmlessly when there
        // is no parent console or one is already attached.
        unsafe { AttachConsole(ATTACH_PARENT_PROCESS) };
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn command_line_is_well_formed() {
        use clap::CommandFactory;
        Args::command().debug_assert();
    }

    #[test]
    fn probe_accepts_a_human_duration() {
        let args = Args::try_parse_from(["gomic", "--source", "both", "--probe", "5s"]).unwrap();
        assert_eq!(args.source, Some(Show::Both));
        assert_eq!(args.probe, Some(Duration::from_secs(5)));
    }
}
