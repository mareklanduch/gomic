# gomic

A small always-on-top stereo peak meter with latching clip lights, for Windows and macOS.

It meters the default input device (microphone, audio interface) so you can see at a glance whether your signal is too quiet or clipping. On Windows it can also meter the default output device (whatever is playing).

## Features

- Two peak bars (green below -12 dBFS, yellow up to -3 dBFS, red above) with a peak-hold marker and a numeric readout.
- Clip lights that stay lit until you reset them, so a clip you did not see is not a clip you missed.
- Follows the system default device when it changes.
- Borderless window you can drag from anywhere; remembers its position and the always-on-top setting.
- No installer and no runtime dependencies. The Windows build is pure Go.

## Using it

| Action | Result |
| --- | --- |
| Drag anywhere | Move the window |
| Double-click | Reset the clip lights |
| Right-click | Menu: source (Windows), always on top, reset, quit |

Command-line flags:

| Flag | Meaning |
| --- | --- |
| `-source input` or `-source output` | Meter this source, overriding the saved choice (`output` is Windows only) |
| `-probe 5s` | Print levels to the console for this long instead of opening the window |

Settings are stored in `gomic/config.json` under the per-user configuration directory (`%AppData%` on Windows, `~/Library/Application Support` on macOS).

## Building

You need [Go](https://go.dev/dl/) 1.27 or newer.

### Windows (64-bit)

```powershell
go build -ldflags "-H=windowsgui" -o gomic.exe .
```

`-H=windowsgui` stops a console window from opening next to the meter. Leave it out if you want to see `-probe` output:

```powershell
go build -o gomic.exe .
```

### macOS

Needs the Xcode command line tools (`xcode-select --install`).

```sh
sh build_mac.sh
open gomic.app
```

The script wraps the binary in an app bundle and ad-hoc signs it, which macOS requires before it grants microphone access. Until you allow that access, macOS delivers silence rather than an error.

## Tests

```sh
go test ./...
```

## Layout

| Path | Contents |
| --- | --- |
| `main.go`, `config.go`, `display.go` | Entry point, saved settings, and the meter state shared by both window implementations |
| `ui_windows.go` | Win32 window, drawn with GDI |
| `ui_darwin.go`, `ui_darwin.m`, `ui_darwin.h` | Cocoa window (cgo) |
| `internal/capture` | Audio capture: WASAPI on Windows, [malgo](https://github.com/gen2brain/malgo) (miniaudio) on macOS |
| `internal/meter` | Platform-independent peak scanning and bar rise/fall logic |

## Install with Go

```sh
go install github.com/mareklanduch/gomic@latest
```

On Windows, add `-ldflags "-H=windowsgui"` to avoid the console window. On macOS, prefer `build_mac.sh`: a bare binary has no app bundle, so it cannot get microphone access.

## License

[MIT](LICENSE)
