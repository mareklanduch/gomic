//! Settings remembered between runs, stored as JSON in the per-user
//! configuration directory.

use std::fs;
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use directories::BaseDirs;
use serde::{Deserialize, Serialize};

use crate::capture::{Device, Source};

/// What the meter remembers between runs.
///
/// The file has the same name, location and field names as the one written
/// by the Go version of gomic, so window position and visibility carry over.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Config {
    pub x: i32,
    pub y: i32,
    pub has_pos: bool,
    pub topmost: bool,

    /// Which meters are visible. At least one always is.
    pub show_input: bool,
    pub show_output: bool,

    /// The chosen devices. An empty id follows the system default; the name
    /// is kept so a device that is not connected can still be named.
    pub input_device: String,
    pub input_device_name: String,
    pub output_device: String,
    pub output_device_name: String,
}

impl Default for Config {
    /// Used on first run and when the saved file is unusable.
    fn default() -> Self {
        Self {
            x: 0,
            y: 0,
            has_pos: false,
            topmost: true,
            show_input: true,
            show_output: false,
            input_device: String::new(),
            input_device_name: String::new(),
            output_device: String::new(),
            output_device_name: String::new(),
        }
    }
}

/// Fields that only matter for reading files written by older versions.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Legacy {
    /// Before both meters could be shown together there was a single source.
    source: Option<String>,
    show_input: Option<bool>,
    show_output: Option<bool>,
}

impl Config {
    /// The per-user settings file location.
    pub fn path() -> PathBuf {
        let dir = BaseDirs::new().map_or_else(|| PathBuf::from("."), |dirs| dirs.config_dir().to_owned());
        dir.join("gomic").join("config.json")
    }

    /// Reads `path`, falling back to defaults on any problem.
    pub fn load(path: &Path) -> Self {
        fs::read_to_string(path).ok().and_then(|text| Self::parse(&text)).unwrap_or_default()
    }

    fn parse(text: &str) -> Option<Self> {
        let mut config: Self = serde_json::from_str(text).ok()?;
        let legacy: Legacy = serde_json::from_str(text).ok()?;
        if legacy.show_input.is_none() && legacy.show_output.is_none() {
            let output = legacy.source.as_deref() == Some("output");
            config.show_input = !output;
            config.show_output = output;
        }
        if !config.show_input && !config.show_output {
            config.show_input = true;
        }
        Some(config)
    }

    /// Writes the config, creating the folder if needed.
    pub fn save(&self, path: &Path) -> Result<()> {
        if let Some(dir) = path.parent() {
            fs::create_dir_all(dir).with_context(|| format!("could not create {}", dir.display()))?;
        }
        let text = serde_json::to_string_pretty(self)?;
        fs::write(path, text).with_context(|| format!("could not write {}", path.display()))
    }

    /// Whether the meter for `source` is visible.
    pub fn shown(&self, source: Source) -> bool {
        match source {
            Source::Input => self.show_input,
            Source::Output => self.show_output,
        }
    }

    /// The visible meters, top to bottom.
    pub fn shown_sources(&self) -> impl Iterator<Item = Source> + '_ {
        Source::ALL.into_iter().filter(|&source| self.shown(source))
    }

    /// Shows or hides one meter. Hiding the only visible meter shows the
    /// other one instead, so the window is never empty.
    pub fn toggle_shown(&mut self, source: Source) {
        match source {
            Source::Input => self.show_input = !self.show_input,
            Source::Output => self.show_output = !self.show_output,
        }
        if !self.show_input && !self.show_output {
            match source {
                Source::Input => self.show_output = true,
                Source::Output => self.show_input = true,
            }
        }
    }

    /// The device chosen for `source`, or `None` to follow the default.
    pub fn device(&self, source: Source) -> Option<Device> {
        let (id, name) = match source {
            Source::Input => (&self.input_device, &self.input_device_name),
            Source::Output => (&self.output_device, &self.output_device_name),
        };
        (!id.is_empty()).then(|| Device { id: id.clone(), name: name.clone() })
    }

    /// Remembers the device chosen for `source`; `None` follows the default.
    pub fn set_device(&mut self, source: Source, device: Option<&Device>) {
        let (id, name) = match source {
            Source::Input => (&mut self.input_device, &mut self.input_device_name),
            Source::Output => (&mut self.output_device, &mut self.output_device_name),
        };
        *id = device.map(|d| d.id.clone()).unwrap_or_default();
        *name = device.map(|d| d.name.clone()).unwrap_or_default();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_file(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("gomic-test-{}-{name}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        dir.join("sub").join("config.json")
    }

    #[test]
    fn survives_save_and_load() {
        let path = temp_file("roundtrip");
        let want = Config {
            x: -1200,
            y: 40,
            has_pos: true,
            topmost: false,
            show_input: true,
            show_output: true,
            input_device: "wasapi:{0.0.1}.{abc}".into(),
            input_device_name: "USB Microphone".into(),
            output_device: "wasapi:{0.0.0}.{def}".into(),
            output_device_name: "Speakers".into(),
        };
        want.save(&path).unwrap();
        assert_eq!(Config::load(&path), want);
    }

    #[test]
    fn falls_back_to_defaults_when_the_file_is_missing() {
        let config = Config::load(&temp_file("missing"));
        assert_eq!(config, Config::default());
        assert!(config.topmost && config.show_input && !config.show_output);
    }

    #[test]
    fn falls_back_to_defaults_when_the_file_is_corrupt() {
        assert_eq!(Config::parse("{not json"), None);
    }

    #[test]
    fn reads_a_file_written_by_the_go_version() {
        let config = Config::parse(
            r#"{"x": 964, "y": 24, "hasPos": true, "topmost": false,
                "showInput": true, "showOutput": true,
                "inputDevice": "", "inputDeviceName": "",
                "outputDevice": "", "outputDeviceName": ""}"#,
        )
        .unwrap();
        assert_eq!((config.x, config.y, config.has_pos, config.topmost), (964, 24, true, false));
        assert!(config.show_input && config.show_output);
    }

    #[test]
    fn shows_only_the_meter_an_older_file_had_selected() {
        for (source, want) in [("output", (false, true)), ("input", (true, false)), ("banana", (true, false))]
        {
            let config = Config::parse(&format!(r#"{{"source":"{source}","topmost":true}}"#)).unwrap();
            assert_eq!((config.show_input, config.show_output), want, "source {source}");
        }
    }

    #[test]
    fn never_hides_both_meters_on_load() {
        let config = Config::parse(r#"{"showInput":false,"showOutput":false}"#).unwrap();
        assert!(config.show_input && !config.show_output);
    }

    #[test]
    fn hiding_the_only_visible_meter_shows_the_other() {
        // (input, output) before, the meter toggled, (input, output) after.
        let cases = [
            ((true, false), Source::Output, (true, true)),
            ((true, true), Source::Input, (false, true)),
            ((true, true), Source::Output, (true, false)),
            ((true, false), Source::Input, (false, true)),
            ((false, true), Source::Output, (true, false)),
        ];
        for ((show_input, show_output), source, want) in cases {
            let mut config = Config { show_input, show_output, ..Config::default() };
            config.toggle_shown(source);
            assert_eq!((config.show_input, config.show_output), want);
        }
    }

    #[test]
    fn remembers_and_forgets_a_chosen_device() {
        let mut config = Config::default();
        let mic = Device { id: "a".into(), name: "USB Mic".into() };

        config.set_device(Source::Input, Some(&mic));
        assert_eq!(config.device(Source::Input), Some(mic));
        assert_eq!(config.device(Source::Output), None);

        config.set_device(Source::Input, None);
        assert_eq!(config.device(Source::Input), None);
    }
}
