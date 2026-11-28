//! The meter window: a small borderless, draggable window drawn with egui.

use std::path::PathBuf;
use std::time::{Duration, Instant};

use eframe::egui::{
    self, Align2, Color32, FontId, PointerButton, Pos2, Rect, Sense, Vec2, ViewportBuilder, ViewportCommand,
    WindowLevel,
};

use crate::capture::{Capturer, OUTPUT_SUPPORTED, Source, Status};
use crate::config::Config;
use crate::menu::{self, Command, SourceState};
use crate::meter::Display;

// Layout, in points.
const WIDTH: f32 = 292.0;
const PAD: f32 = 8.0;
const LABEL_W: f32 = 14.0;
const BAR_H: f32 = 16.0;
const BAR_GAP: f32 = 6.0;
/// Between the input pair and the output pair.
const GROUP_GAP: f32 = 10.0;
const CLIP_W: f32 = 16.0;
const VALUE_W: f32 = 44.0;
const SCALE_H: f32 = 12.0;
/// One meter: two bars and the gap between them.
const GROUP_H: f32 = 2.0 * BAR_H + BAR_GAP;

/// Left edge of the bars.
const FLOOR_DB: f64 = -60.0;
/// Green below, yellow above.
const WARN_DB: f64 = -12.0;
/// Red above.
const HOT_DB: f64 = -3.0;
const SCALE_MARKS: [f64; 6] = [-48.0, -36.0, -24.0, -12.0, -6.0, 0.0];

const FRAME: Duration = Duration::from_millis(33);
/// How long the window must rest before its new position is saved.
const SETTLE: Duration = Duration::from_millis(500);

const BACKGROUND: Color32 = Color32::from_rgb(0x1b, 0x1b, 0x1d);
const TRACK: Color32 = Color32::from_rgb(0x30, 0x30, 0x34);
const GREEN: Color32 = Color32::from_rgb(0x3c, 0xcf, 0x5a);
const YELLOW: Color32 = Color32::from_rgb(0xe6, 0xc2, 0x29);
const RED: Color32 = Color32::from_rgb(0xe5, 0x48, 0x4d);
const HOLD: Color32 = Color32::from_rgb(0xf2, 0xf2, 0xf2);
const CLIP_OFF: Color32 = Color32::from_rgb(0x4a, 0x22, 0x22);
const CLIP_ON: Color32 = Color32::from_rgb(0xff, 0x3b, 0x30);
const TEXT: Color32 = Color32::from_rgb(0xd0, 0xd0, 0xd4);
const DIM: Color32 = Color32::from_rgb(0x8a, 0x8a, 0x90);

/// The window size with `meters` meters visible.
fn window_size(meters: usize) -> Vec2 {
    let n = meters.max(1) as f32;
    Vec2::new(WIDTH, PAD + n * GROUP_H + (n - 1.0) * GROUP_GAP + 2.0 + SCALE_H + PAD)
}

/// Maps `FLOOR_DB..=0` dBFS onto `0..=width`.
fn db_to_x(db: f64, width: f32) -> f32 {
    let fraction = ((db - FLOOR_DB) / -FLOOR_DB).clamp(0.0, 1.0);
    (fraction * f64::from(width)) as f32
}

/// How the window should be created for `config`.
pub fn viewport(config: &Config) -> ViewportBuilder {
    let mut viewport = ViewportBuilder::default()
        .with_title("gomic")
        .with_inner_size(window_size(config.shown_sources().count()))
        .with_decorations(false)
        .with_resizable(false)
        .with_window_level(if config.topmost { WindowLevel::AlwaysOnTop } else { WindowLevel::Normal });
    if config.has_pos {
        viewport = viewport.with_position([config.x as f32, config.y as f32]);
    }
    viewport
}

/// The running meter window.
pub struct App {
    config: Config,
    config_path: PathBuf,
    /// One per source, indexed by [`Source::index`].
    capturers: [Capturer; 2],
    displays: [Display; 2],
    /// Where the window was last seen, and since when it has been there.
    resting_at: Option<(Pos2, Instant)>,
    /// Whether a first-run window has been moved to its starting corner.
    placed: bool,
}

impl App {
    pub fn new(config: Config, config_path: PathBuf, capturers: [Capturer; 2]) -> Self {
        let displays = [Display::new(Instant::now()); 2];
        Self { config, config_path, capturers, displays, resting_at: None, placed: false }
    }

    fn save_config(&self) {
        // Losing a saved setting is not worth interrupting the user for.
        let _ = self.config.save(&self.config_path);
    }

    fn apply(&mut self, command: Command, ctx: &egui::Context) {
        match command {
            Command::Pick(source, device) => {
                self.config.set_device(source, device.as_ref());
                self.capturers[source.index()].select(device);
                self.displays[source.index()].reset_clip();
            }
            Command::ToggleShown(source) => {
                let was_shown = Source::ALL.map(|source| self.config.shown(source));
                self.config.toggle_shown(source);
                for source in Source::ALL {
                    let shown = self.config.shown(source);
                    if shown && !was_shown[source.index()] {
                        // Start from silence, not from stale levels.
                        self.displays[source.index()] = Display::new(Instant::now());
                    }
                    self.capturers[source.index()].set_enabled(shown);
                }
                let size = window_size(self.config.shown_sources().count());
                ctx.send_viewport_cmd(ViewportCommand::InnerSize(size));
            }
            Command::ToggleTopmost => {
                self.config.topmost = !self.config.topmost;
                let level = if self.config.topmost { WindowLevel::AlwaysOnTop } else { WindowLevel::Normal };
                ctx.send_viewport_cmd(ViewportCommand::WindowLevel(level));
            }
            Command::ResetClip => self.reset_clip(),
            Command::Quit => ctx.send_viewport_cmd(ViewportCommand::Close),
        }
        self.save_config();
    }

    fn reset_clip(&mut self) {
        for display in &mut self.displays {
            display.reset_clip();
        }
    }

    /// Without a saved position, starts in the top-right corner of the screen.
    fn place_on_first_run(&mut self, ctx: &egui::Context) {
        if self.config.has_pos || self.placed {
            return;
        }
        if let Some(monitor) = ctx.input(|input| input.viewport().monitor_size) {
            const MARGIN: f32 = 24.0;
            let corner = Pos2::new(monitor.x - WIDTH - MARGIN, MARGIN);
            ctx.send_viewport_cmd(ViewportCommand::OuterPosition(corner));
            self.placed = true;
        }
    }

    /// Remembers where the window is once it has stopped moving.
    fn track_position(&mut self, ctx: &egui::Context) {
        let Some(position) = ctx.input(|input| input.viewport().outer_rect).map(|rect| rect.min) else {
            return;
        };
        let now = Instant::now();
        match self.resting_at {
            Some((at, since)) if at == position => {
                let saved = Pos2::new(self.config.x as f32, self.config.y as f32);
                if now.duration_since(since) >= SETTLE && (!self.config.has_pos || saved != position.round())
                {
                    self.remember_position(position);
                    self.save_config();
                }
            }
            _ => self.resting_at = Some((position, now)),
        }
    }

    fn remember_position(&mut self, position: Pos2) {
        self.config.x = position.x.round() as i32;
        self.config.y = position.y.round() as i32;
        self.config.has_pos = true;
    }

    fn menu_entries(&self) -> Vec<menu::Entry> {
        let sources = Source::ALL.map(|source| {
            let capturer = &self.capturers[source.index()];
            SourceState { devices: capturer.devices(), selected: capturer.selected() }
        });
        menu::build(&self.config, &sources, OUTPUT_SUPPORTED)
    }

    fn paint(&self, painter: &egui::Painter, area: Rect) {
        let font = FontId::proportional(13.0);
        let small = FontId::proportional(10.0);
        painter.rect_filled(area, 0.0, BACKGROUND);

        let bar_left = area.left() + PAD + LABEL_W;
        let bar_w = area.right() - bar_left - (PAD + VALUE_W + 4.0 + CLIP_W + 4.0);
        let mut group_top = area.top() + PAD;
        for source in self.config.shown_sources() {
            let display = &self.displays[source.index()];
            let failure = match self.capturers[source.index()].status() {
                Status::Failed(reason) => Some(reason),
                Status::Idle | Status::Metering(_) => None,
            };
            let labels = match source {
                Source::Input => ["1", "2"],
                Source::Output => ["L", "R"],
            };
            for (channel, label) in labels.into_iter().enumerate() {
                let top = group_top + channel as f32 * (BAR_H + BAR_GAP);
                let middle = top + BAR_H / 2.0;
                painter.text(
                    Pos2::new(area.left() + PAD, middle),
                    Align2::LEFT_CENTER,
                    label,
                    font.clone(),
                    TEXT,
                );

                let track = Rect::from_min_size(Pos2::new(bar_left, top), Vec2::new(bar_w, BAR_H));
                painter.rect_filled(track, 0.0, TRACK);
                match &failure {
                    None => {
                        paint_level(painter, track, display.bars[channel].level, display.bars[channel].hold)
                    }
                    // Nothing to meter: say why where the level would be.
                    Some(reason) if channel == 0 => {
                        painter.with_clip_rect(track.shrink2(Vec2::new(4.0, 0.0))).text(
                            Pos2::new(track.left() + 4.0, middle),
                            Align2::LEFT_CENTER,
                            reason,
                            small.clone(),
                            CLIP_ON,
                        );
                    }
                    Some(_) => {}
                }

                let clip_box =
                    Rect::from_min_size(Pos2::new(track.right() + 4.0, top), Vec2::new(CLIP_W, BAR_H));
                painter.rect_filled(clip_box, 0.0, if display.clip[channel] { CLIP_ON } else { CLIP_OFF });

                let hold = display.bars[channel].hold;
                let value = if failure.is_none() && hold > FLOOR_DB {
                    format!("{hold:.1}")
                } else {
                    "-∞".to_owned()
                };
                painter.text(
                    Pos2::new(area.right() - PAD, middle),
                    Align2::RIGHT_CENTER,
                    value,
                    font.clone(),
                    TEXT,
                );
            }
            group_top += GROUP_H + GROUP_GAP;
        }

        let scale_middle = group_top - GROUP_GAP + 2.0 + SCALE_H / 2.0;
        for db in SCALE_MARKS {
            painter.text(
                Pos2::new(bar_left + db_to_x(db, bar_w), scale_middle),
                Align2::RIGHT_CENTER,
                format!("{db:.0}"),
                small.clone(),
                DIM,
            );
        }
    }
}

/// Paints the green/yellow/red bar and the peak-hold marker.
fn paint_level(painter: &egui::Painter, track: Rect, level: f64, hold: f64) {
    let width = track.width();
    let level_x = track.left() + db_to_x(level, width);
    for (from, to, color) in [(FLOOR_DB, WARN_DB, GREEN), (WARN_DB, HOT_DB, YELLOW), (HOT_DB, 0.0, RED)] {
        let left = track.left() + db_to_x(from, width);
        let right = (track.left() + db_to_x(to, width)).min(level_x);
        if right > left {
            painter.rect_filled(Rect::from_x_y_ranges(left..=right, track.y_range()), 0.0, color);
        }
    }
    if hold > FLOOR_DB {
        const MARK_W: f32 = 2.0;
        let left = (track.left() + db_to_x(hold, width) - MARK_W).max(track.left());
        painter.rect_filled(Rect::from_x_y_ranges(left..=left + MARK_W, track.y_range()), 0.0, HOLD);
    }
}

impl eframe::App for App {
    fn ui(&mut self, ui: &mut egui::Ui, frame: &mut eframe::Frame) {
        let now = Instant::now();
        for source in Source::ALL {
            let peaks = self.capturers[source.index()].take_peaks();
            self.displays[source.index()].tick(now, peaks);
        }

        let area = ui.max_rect();
        self.paint(ui.painter(), area);

        // Drag from anywhere, double-click to reset, right-click for the menu.
        let response = ui.interact(area, egui::Id::unique("meter"), Sense::click_and_drag());
        if response.drag_started_by(PointerButton::Primary) {
            ui.ctx().send_viewport_cmd(ViewportCommand::StartDrag);
        }
        if response.double_clicked() {
            self.reset_clip();
        }
        if let Some(command) = menu::show(&response, frame, || self.menu_entries()) {
            self.apply(command, ui.ctx());
        }

        self.place_on_first_run(ui.ctx());
        self.track_position(ui.ctx());
        ui.ctx().request_repaint_after(FRAME);
    }

    fn on_exit(&mut self, _gl: Option<&eframe::glow::Context>) {
        if let Some((position, _)) = self.resting_at {
            self.remember_position(position);
        }
        self.save_config();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn one_meter_fits_the_original_window_and_two_stack() {
        assert_eq!(window_size(1), Vec2::new(292.0, 68.0));
        assert_eq!(window_size(2), Vec2::new(292.0, 116.0));
    }

    #[test]
    fn db_maps_the_floor_to_the_left_edge_and_full_scale_to_the_right() {
        assert_eq!(db_to_x(FLOOR_DB, 200.0), 0.0);
        assert_eq!(db_to_x(-30.0, 200.0), 100.0);
        assert_eq!(db_to_x(0.0, 200.0), 200.0);
        assert_eq!(db_to_x(-90.0, 200.0), 0.0);
        assert_eq!(db_to_x(6.0, 200.0), 200.0);
    }
}
