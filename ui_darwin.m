// The macOS meter window. Go owns all meter state; this file only draws it
// and forwards clicks. Kept in step with ui_windows.go by hand.

#import <Cocoa/Cocoa.h>
#include "ui_darwin.h"
#include "_cgo_export.h"

// Layout in points (same numbers as the Windows window).
static const CGFloat kWinW = 292, kWinH = 68;
static const CGFloat kPad = 8, kLabelW = 14, kBarH = 16, kBarGap = 6;
static const CGFloat kClipW = 16, kValueW = 44, kScaleH = 12;
static const double kFloorDB = -60.0; // left edge of the bars
static const double kWarnDB = -12.0;  // green below, yellow above
static const double kHotDB = -3.0;    // red above

static NSWindow *gWindow;
static BOOL gTopmost;

static NSColor *rgb(int r, int g, int b) {
    return [NSColor colorWithSRGBRed:r / 255.0 green:g / 255.0 blue:b / 255.0 alpha:1.0];
}

static void fillRect(NSRect r, NSColor *color) {
    [color setFill];
    NSRectFill(r);
}

// dbToX maps kFloorDB..0 dBFS onto 0..width.
static CGFloat dbToX(double db, CGFloat width) {
    double f = (db - kFloorDB) / -kFloorDB;
    if (f < 0) f = 0;
    if (f > 1) f = 1;
    return (CGFloat)(f * width);
}

static NSString *str(const char *utf8) {
    NSString *s = [NSString stringWithUTF8String:utf8];
    return s != nil ? s : @"";
}

// drawText draws one vertically centered line inside r.
static void drawText(NSString *s, NSRect r, NSFont *font, NSColor *color, NSTextAlignment align) {
    NSMutableParagraphStyle *para = [[NSMutableParagraphStyle alloc] init];
    para.alignment = align;
    para.lineBreakMode = NSLineBreakByTruncatingTail;
    NSDictionary *attrs = @{
        NSFontAttributeName : font,
        NSForegroundColorAttributeName : color,
        NSParagraphStyleAttributeName : para,
    };
    CGFloat h = [s sizeWithAttributes:attrs].height;
    NSRect line = NSMakeRect(r.origin.x, r.origin.y + (r.size.height - h) / 2, r.size.width, h);
    [s drawInRect:line withAttributes:attrs];
}

// drawLevel paints the green/yellow/red bar and the peak-hold marker.
static void drawLevel(NSRect track, double level, double hold) {
    CGFloat w = track.size.width;
    CGFloat levelX = dbToX(level, w);
    const double from[3] = {kFloorDB, kWarnDB, kHotDB};
    const double to[3] = {kWarnDB, kHotDB, 0.0};
    NSColor *colors[3] = {rgb(0x3c, 0xcf, 0x5a), rgb(0xe6, 0xc2, 0x29), rgb(0xe5, 0x48, 0x4d)};
    for (int i = 0; i < 3; i++) {
        CGFloat left = dbToX(from[i], w);
        CGFloat right = MIN(dbToX(to[i], w), levelX);
        if (right > left) {
            fillRect(NSMakeRect(track.origin.x + left, track.origin.y, right - left, track.size.height), colors[i]);
        }
    }
    if (hold > kFloorDB) {
        const CGFloat markW = 2;
        CGFloat x = MAX(dbToX(hold, w) - markW, 0);
        fillRect(NSMakeRect(track.origin.x + x, track.origin.y, markW, track.size.height), rgb(0xf2, 0xf2, 0xf2));
    }
}

static void saveState(void) {
    if (gWindow == nil) return;
    NSPoint origin = gWindow.frame.origin;
    goSaveState(origin.x, origin.y, gTopmost ? 1 : 0);
}

// A borderless window refuses keyboard focus unless told otherwise.
@interface MeterWindow : NSWindow
@end

@implementation MeterWindow
- (BOOL)canBecomeKeyWindow {
    return YES;
}
@end

@interface MeterView : NSView
@end

@implementation MeterView {
    MeterState _state;
}

- (BOOL)isFlipped {
    return YES; // origin at the top left, like the Windows layout
}

- (BOOL)isOpaque {
    return YES;
}

- (BOOL)acceptsFirstMouse:(NSEvent *)event {
    return YES;
}

- (void)tick:(NSTimer *)timer {
    goTick(&_state);
    [self setNeedsDisplay:YES];
}

// Double-click resets the clip lights; any other press drags the window.
- (void)mouseDown:(NSEvent *)event {
    if (event.clickCount >= 2) {
        goResetClip();
        return;
    }
    [self.window performWindowDragWithEvent:event];
}

- (NSMenu *)menuForEvent:(NSEvent *)event {
    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"gomic"];

    // No action, so AppKit shows it grayed out: it is a label.
    [menu addItemWithTitle:str(_state.hasError ? _state.message : _state.device) action:NULL keyEquivalent:@""];
    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *top = [menu addItemWithTitle:@"Always on top" action:@selector(toggleTopmost:) keyEquivalent:@""];
    top.target = self;
    top.state = gTopmost ? NSControlStateValueOn : NSControlStateValueOff;

    NSMenuItem *reset = [menu addItemWithTitle:@"Reset clip lights (double-click)" action:@selector(resetClip:) keyEquivalent:@""];
    reset.target = self;

    [menu addItem:[NSMenuItem separatorItem]];
    NSMenuItem *quit = [menu addItemWithTitle:@"Quit gomic" action:@selector(terminate:) keyEquivalent:@""];
    quit.target = NSApp;
    return menu;
}

- (void)toggleTopmost:(id)sender {
    gTopmost = !gTopmost;
    self.window.level = gTopmost ? NSFloatingWindowLevel : NSNormalWindowLevel;
    saveState();
}

- (void)resetClip:(id)sender {
    goResetClip();
}

- (void)drawRect:(NSRect)dirtyRect {
    NSRect bounds = self.bounds;
    CGFloat w = bounds.size.width;
    fillRect(bounds, rgb(0x1b, 0x1b, 0x1d));

    NSFont *font = [NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightRegular];
    NSFont *smallFont = [NSFont systemFontOfSize:9];
    NSColor *textColor = rgb(0xd0, 0xd0, 0xd4);
    NSColor *dimColor = rgb(0x8a, 0x8a, 0x90);
    NSColor *clipOn = rgb(0xff, 0x3b, 0x30);
    NSColor *clipOff = rgb(0x4a, 0x22, 0x22);

    CGFloat barX = kPad + kLabelW;
    CGFloat barW = w - barX - (kPad + kValueW + 4 + kClipW + 4);

    for (int ch = 0; ch < 2; ch++) {
        CGFloat top = kPad + ch * (kBarH + kBarGap);

        drawText(ch == 0 ? @"1" : @"2", NSMakeRect(kPad, top, kLabelW, kBarH), font, textColor, NSTextAlignmentLeft);

        NSRect track = NSMakeRect(barX, top, barW, kBarH);
        fillRect(track, rgb(0x30, 0x30, 0x34));
        if (!_state.hasError) {
            drawLevel(track, _state.level[ch], _state.hold[ch]);
        }

        NSRect clipBox = NSMakeRect(NSMaxX(track) + 4, top, kClipW, kBarH);
        fillRect(clipBox, _state.clip[ch] ? clipOn : clipOff);

        NSString *value = @"-∞";
        if (!_state.hasError && _state.hold[ch] > kFloorDB) {
            value = [NSString stringWithFormat:@"%.1f", _state.hold[ch]];
        }
        NSRect valueRect = NSMakeRect(NSMaxX(clipBox) + 4, top, w - kPad - (NSMaxX(clipBox) + 4), kBarH);
        drawText(value, valueRect, font, textColor, NSTextAlignmentRight);
    }

    CGFloat scaleTop = kPad + 2 * kBarH + kBarGap + 2;
    if (_state.hasError) {
        drawText(str(_state.message), NSMakeRect(barX, scaleTop, w - barX - kPad, kScaleH), smallFont, clipOn, NSTextAlignmentLeft);
    } else {
        const double marks[6] = {-48, -36, -24, -12, -6, 0};
        for (int i = 0; i < 6; i++) {
            CGFloat x = barX + dbToX(marks[i], barW);
            NSString *label = [NSString stringWithFormat:@"%.0f", marks[i]];
            drawText(label, NSMakeRect(x - 30, scaleTop, 30, kScaleH), smallFont, dimColor, NSTextAlignmentRight);
        }
    }
}

@end

@interface AppDelegate : NSObject <NSApplicationDelegate>
@end

@implementation AppDelegate
- (void)applicationWillTerminate:(NSNotification *)notification {
    saveState();
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}
@end

static AppDelegate *gDelegate;

void gomicRun(int hasPos, double x, double y, int topmost) {
    @autoreleasepool {
        NSApplication *application = [NSApplication sharedApplication];
        [application setActivationPolicy:NSApplicationActivationPolicyRegular];
        gDelegate = [[AppDelegate alloc] init];
        application.delegate = gDelegate;

        // Minimal menu bar so Cmd+Q works.
        NSMenu *menuBar = [[NSMenu alloc] init];
        NSMenuItem *appItem = [[NSMenuItem alloc] init];
        [menuBar addItem:appItem];
        NSMenu *appMenu = [[NSMenu alloc] init];
        [appMenu addItemWithTitle:@"Quit gomic" action:@selector(terminate:) keyEquivalent:@"q"];
        appItem.submenu = appMenu;
        application.mainMenu = menuBar;

        // Restore the saved position if it is still on a screen; otherwise
        // start in the top-right corner of the main screen.
        NSRect frame = NSMakeRect(0, 0, kWinW, kWinH);
        BOOL placed = NO;
        if (hasPos) {
            NSPoint centre = NSMakePoint(x + kWinW / 2, y + kWinH / 2);
            for (NSScreen *screen in [NSScreen screens]) {
                if (NSPointInRect(centre, screen.frame)) {
                    frame.origin = NSMakePoint(x, y);
                    placed = YES;
                    break;
                }
            }
        }
        if (!placed) {
            NSRect visible = [NSScreen mainScreen].visibleFrame;
            frame.origin = NSMakePoint(NSMaxX(visible) - kWinW - 24, NSMaxY(visible) - kWinH - 24);
        }

        gTopmost = topmost != 0;
        gWindow = [[MeterWindow alloc] initWithContentRect:frame
                                                 styleMask:NSWindowStyleMaskBorderless
                                                   backing:NSBackingStoreBuffered
                                                     defer:NO];
        gWindow.title = @"gomic";
        gWindow.releasedWhenClosed = NO;
        gWindow.opaque = YES;
        gWindow.hasShadow = YES;
        gWindow.level = gTopmost ? NSFloatingWindowLevel : NSNormalWindowLevel;
        // Stay visible when switching Spaces and over full-screen apps.
        gWindow.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;

        MeterView *view = [[MeterView alloc] initWithFrame:NSMakeRect(0, 0, kWinW, kWinH)];
        gWindow.contentView = view;

        // Common modes keep the meter moving while a menu is open or the window is dragged.
        NSTimer *timer = [NSTimer timerWithTimeInterval:1.0 / 30.0 target:view selector:@selector(tick:) userInfo:nil repeats:YES];
        [[NSRunLoop mainRunLoop] addTimer:timer forMode:NSRunLoopCommonModes];

        [gWindow makeKeyAndOrderFront:nil];
        [application activateIgnoringOtherApps:YES];
        [application run];
    }
}
