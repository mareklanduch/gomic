//! The right-click menu: what it contains, and showing it.
//!
//! [`build`] describes the menu as plain data, which keeps its contents
//! testable. On Windows and macOS it is shown as a native menu, which may
//! extend beyond the small meter window; elsewhere it falls back to a menu
//! drawn inside the window.

use crate::capture::{Device, Devices, Source};
use crate::config::Config;

/// What choosing a menu entry does.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Command {
    /// Meter this device; `None` follows the system default.
    Pick(Source, Option<Device>),
    ToggleShown(Source),
    ToggleTopmost,
    ResetClip,
    Quit,
}

/// One line of the menu.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Entry {
    Item { label: String, command: Command },
    Check { label: String, checked: bool, command: Command },
    Submenu { label: String, entries: Vec<Entry> },
    Separator,
}

/// What the menu needs to know about one source.
#[derive(Debug, Clone, Default)]
pub struct SourceState {
    pub devices: Devices,
    /// The chosen device; `None` follows the system default.
    pub selected: Option<Device>,
}

/// Describes the whole menu. `sources` is indexed by [`Source::index`].
/// Without output support only the input picker is offered, and there is
/// nothing to show or hide.
pub fn build(config: &Config, sources: &[SourceState; 2], output_supported: bool) -> Vec<Entry> {
    let picker = |source: Source, label: &str| Entry::Submenu {
        label: label.to_owned(),
        entries: device_entries(source, &sources[source.index()]),
    };
    let mut entries = vec![picker(Source::Input, "Input")];
    if output_supported {
        entries.push(picker(Source::Output, "Output"));
        entries.push(Entry::Separator);
        for (source, label) in [(Source::Input, "Show input meter"), (Source::Output, "Show output meter")] {
            entries.push(Entry::Check {
                label: label.to_owned(),
                checked: config.shown(source),
                command: Command::ToggleShown(source),
            });
        }
    }
    entries.extend([
        Entry::Separator,
        Entry::Check {
            label: "Always on top".to_owned(),
            checked: config.topmost,
            command: Command::ToggleTopmost,
        },
        Entry::Item { label: "Reset clip lights (double-click)".to_owned(), command: Command::ResetClip },
        Entry::Separator,
        Entry::Item { label: "Quit".to_owned(), command: Command::Quit },
    ]);
    entries
}

/// The device picker for one source: the system default first, then every
/// connected device. A chosen device that is not connected stays listed, so
/// the choice remains visible.
fn device_entries(source: Source, state: &SourceState) -> Vec<Entry> {
    let selected_id = state.selected.as_ref().map(|device| device.id.as_str());
    let mut entries = vec![Entry::Check {
        label: match &state.devices.default_name {
            Some(name) => format!("Default ({name})"),
            None => "Default".to_owned(),
        },
        checked: selected_id.is_none(),
        command: Command::Pick(source, None),
    }];
    if !state.devices.all.is_empty() || state.selected.is_some() {
        entries.push(Entry::Separator);
    }
    entries.extend(state.devices.all.iter().map(|device| Entry::Check {
        label: device.name.clone(),
        checked: selected_id == Some(device.id.as_str()),
        command: Command::Pick(source, Some(device.clone())),
    }));
    if let Some(selected) = &state.selected
        && !state.devices.all.iter().any(|device| device.id == selected.id)
    {
        let name = if selected.name.is_empty() { "Chosen device" } else { &selected.name };
        entries.push(Entry::Check {
            label: format!("{name} (not connected)"),
            checked: true,
            command: Command::Pick(source, Some(selected.clone())),
        });
    }
    entries
}

#[cfg(any(target_os = "windows", target_os = "macos"))]
pub use native::show;

#[cfg(not(any(target_os = "windows", target_os = "macos")))]
pub use fallback::show;

/// A native popup menu, via `muda`.
#[cfg(any(target_os = "windows", target_os = "macos"))]
mod native {
    use muda::{
        CheckMenuItem, ContextMenu, IsMenuItem, Menu, MenuEvent, MenuItem, PredefinedMenuItem, Submenu,
    };
    use raw_window_handle::{HasWindowHandle, RawWindowHandle};

    use super::{Command, Entry};

    /// Shows the menu at the pointer when `response` is right-clicked, and
    /// returns what was chosen. Blocks while the menu is open.
    pub fn show(
        response: &eframe::egui::Response,
        frame: &eframe::Frame,
        entries: impl FnOnce() -> Vec<Entry>,
    ) -> Option<Command> {
        if !response.secondary_clicked() {
            return None;
        }
        let menu = Menu::new();
        let mut commands = Vec::new();
        for entry in entries() {
            // Appending only fails for item kinds a popup cannot hold.
            let _ = menu.append(to_item(entry, &mut commands).as_ref());
        }

        // Anything left over belongs to a menu that is already gone.
        while MenuEvent::receiver().try_recv().is_ok() {}

        let handle = frame.window_handle().ok()?;
        match handle.as_raw() {
            #[cfg(target_os = "windows")]
            RawWindowHandle::Win32(window) => {
                // SAFETY: the handle comes from the live window this frame draws to.
                unsafe { menu.show_context_menu_for_hwnd(window.hwnd.get(), None) };
            }
            #[cfg(target_os = "macos")]
            RawWindowHandle::AppKit(window) => {
                // SAFETY: the pointer is the live NSView this frame draws to.
                unsafe { menu.show_context_menu_for_nsview(window.ns_view.as_ptr().cast_const(), None) };
            }
            _ => return None,
        }

        let chosen = MenuEvent::receiver().try_recv().ok()?;
        let index: usize = chosen.id().as_ref().parse().ok()?;
        commands.into_iter().nth(index)
    }

    /// Converts an entry, giving each command its position in `commands` as id.
    fn to_item(entry: Entry, commands: &mut Vec<Command>) -> Box<dyn IsMenuItem> {
        let mut id = |command| {
            commands.push(command);
            (commands.len() - 1).to_string()
        };
        match entry {
            Entry::Item { label, command } => {
                Box::new(MenuItem::with_id(id(command), text(&label), true, None))
            }
            Entry::Check { label, checked, command } => {
                Box::new(CheckMenuItem::with_id(id(command), text(&label), true, checked, None))
            }
            Entry::Submenu { label, entries } => {
                let submenu = Submenu::new(text(&label), true);
                for entry in entries {
                    let _ = submenu.append(to_item(entry, commands).as_ref());
                }
                Box::new(submenu)
            }
            Entry::Separator => Box::new(PredefinedMenuItem::separator()),
        }
    }

    /// Menu text treats "&" as "underline the next letter"; device names mean it literally.
    fn text(label: &str) -> String {
        label.replace('&', "&&")
    }
}

/// A menu drawn by egui inside the window, for platforms without a native one.
#[cfg(not(any(target_os = "windows", target_os = "macos")))]
mod fallback {
    use super::{Command, Entry};

    /// Shows the menu while `response` has it open, and returns what was chosen.
    pub fn show(
        response: &eframe::egui::Response,
        _frame: &eframe::Frame,
        entries: impl FnOnce() -> Vec<Entry>,
    ) -> Option<Command> {
        let mut chosen = None;
        response.context_menu(|ui| add(ui, entries(), &mut chosen));
        chosen
    }

    fn add(ui: &mut eframe::egui::Ui, entries: Vec<Entry>, chosen: &mut Option<Command>) {
        for entry in entries {
            match entry {
                Entry::Item { label, command } => {
                    if ui.button(label).clicked() {
                        *chosen = Some(command);
                        ui.close();
                    }
                }
                Entry::Check { label, mut checked, command } => {
                    if ui.checkbox(&mut checked, label).clicked() {
                        *chosen = Some(command);
                        ui.close();
                    }
                }
                Entry::Submenu { label, entries } => {
                    ui.menu_button(label, |ui| add(ui, entries, chosen));
                }
                Entry::Separator => {
                    ui.separator();
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn device(id: &str, name: &str) -> Device {
        Device { id: id.into(), name: name.into() }
    }

    fn state(all: Vec<Device>, default_name: Option<&str>, selected: Option<Device>) -> SourceState {
        SourceState { devices: Devices { all, default_name: default_name.map(Into::into) }, selected }
    }

    /// Labels of the checked entries.
    fn checked(entries: &[Entry]) -> Vec<&str> {
        entries
            .iter()
            .filter_map(|entry| match entry {
                Entry::Check { label, checked: true, .. } => Some(label.as_str()),
                _ => None,
            })
            .collect()
    }

    fn labels(entries: &[Entry]) -> Vec<&str> {
        entries
            .iter()
            .map(|entry| match entry {
                Entry::Item { label, .. } | Entry::Check { label, .. } | Entry::Submenu { label, .. } => {
                    label
                }
                Entry::Separator => "-",
            })
            .collect()
    }

    #[test]
    fn picker_lists_the_default_first_and_checks_it() {
        let mics = vec![device("a", "USB Mic"), device("b", "Webcam")];
        let entries = device_entries(Source::Input, &state(mics, Some("USB Mic"), None));

        assert_eq!(labels(&entries), ["Default (USB Mic)", "-", "USB Mic", "Webcam"]);
        assert_eq!(checked(&entries), ["Default (USB Mic)"]);
    }

    #[test]
    fn picker_checks_the_chosen_device_and_picking_it_carries_the_device() {
        let mics = vec![device("a", "USB Mic"), device("b", "Webcam")];
        let entries = device_entries(Source::Input, &state(mics.clone(), None, Some(mics[1].clone())));

        assert_eq!(checked(&entries), ["Webcam"]);
        assert!(entries.contains(&Entry::Check {
            label: "Webcam".into(),
            checked: true,
            command: Command::Pick(Source::Input, Some(mics[1].clone())),
        }));
    }

    #[test]
    fn picker_keeps_a_chosen_device_that_is_not_connected_visible() {
        let gone = device("z", "Interface");
        let entries = device_entries(Source::Output, &state(vec![device("a", "Speakers")], None, Some(gone)));

        assert_eq!(labels(&entries), ["Default", "-", "Speakers", "Interface (not connected)"]);
        assert_eq!(checked(&entries), ["Interface (not connected)"]);
    }

    #[test]
    fn picker_with_no_devices_offers_only_the_default() {
        let entries = device_entries(Source::Input, &state(vec![], None, None));
        assert_eq!(labels(&entries), ["Default"]);
    }

    #[test]
    fn menu_is_short_with_the_devices_tucked_into_submenus() {
        let config = Config { show_output: true, ..Config::default() };
        let entries = build(&config, &Default::default(), true);

        assert_eq!(
            labels(&entries),
            [
                "Input",
                "Output",
                "-",
                "Show input meter",
                "Show output meter",
                "-",
                "Always on top",
                "Reset clip lights (double-click)",
                "-",
                "Quit",
            ]
        );
        assert_eq!(checked(&entries), ["Show input meter", "Show output meter", "Always on top"]);
    }

    #[test]
    fn menu_without_output_support_offers_only_the_input_picker() {
        let entries = build(&Config::default(), &Default::default(), false);
        assert_eq!(
            labels(&entries),
            ["Input", "-", "Always on top", "Reset clip lights (double-click)", "-", "Quit"]
        );
    }
}
