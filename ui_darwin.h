#ifndef GOMIC_UI_DARWIN_H
#define GOMIC_UI_DARWIN_H

// MeterState is filled by Go (goTick) and drawn by the Cocoa view.
typedef struct {
    double level[2]; // dBFS, current bar position
    double hold[2];  // dBFS, peak-hold marker
    int clip[2];     // 1 while the clip light is lit
    int hasError;    // 1 when nothing is being metered
    char message[200]; // why, when hasError is set (UTF-8)
    char device[200];  // device name otherwise (UTF-8)
} MeterState;

// DeviceItem is one line of the input device submenu, filled by Go
// (goMenuDevices).
typedef struct {
    char label[200]; // UTF-8
    int checked;     // 1 for the device in use
} DeviceItem;

// gomicRun opens the meter window and runs the Cocoa event loop.
// It must be called on the main thread and does not return.
void gomicRun(int hasPos, double x, double y, int topmost);

#endif
