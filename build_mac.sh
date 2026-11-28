#!/bin/sh
# Builds gomic.app on macOS. Needs Rust (https://rustup.rs) and the Xcode
# command line tools (xcode-select --install). Run from this folder:
# sh build_mac.sh
#
# Optional environment:
#   VERSION=1.2.3   version written into the app bundle (default 0.0.0)
#   UNIVERSAL=1     build for both Apple silicon and Intel Macs
set -e

APP=gomic.app
VERSION=${VERSION:-0.0.0}
export GOMIC_VERSION="$VERSION"

if [ "${UNIVERSAL:-}" = 1 ]; then
    rustup target add aarch64-apple-darwin x86_64-apple-darwin
    cargo build --release --locked --target aarch64-apple-darwin
    cargo build --release --locked --target x86_64-apple-darwin
    lipo -create -output gomic \
        target/aarch64-apple-darwin/release/gomic \
        target/x86_64-apple-darwin/release/gomic
else
    cargo build --release --locked
    cp target/release/gomic gomic
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS"
mv gomic "$APP/Contents/MacOS/gomic"

# macOS refuses audio access to an app that does not say why it wants it:
# NSMicrophoneUsageDescription for inputs, NSAudioCaptureUsageDescription for
# metering what is playing.
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>gomic</string>
    <key>CFBundleIdentifier</key>
    <string>local.gomic</string>
    <key>CFBundleExecutable</key>
    <string>gomic</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>$VERSION</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.2</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSMicrophoneUsageDescription</key>
    <string>gomic reads your audio input to show its level and warn when it clips.</string>
    <key>NSAudioCaptureUsageDescription</key>
    <string>gomic reads what is playing to show its level and warn when it clips.</string>
</dict>
</plist>
PLIST

# Ad-hoc signature so macOS remembers the audio permissions for this build.
codesign --force --sign - "$APP"

echo "Built $APP. Start it with: open $APP"
