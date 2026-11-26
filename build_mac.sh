#!/bin/sh
# Builds gomic.app on macOS. Needs Go and the Xcode command line tools
# (xcode-select --install). Run from this folder: sh build_mac.sh
#
# Optional environment:
#   VERSION=1.2.3   version written into the app bundle (default 0.0.0)
#   UNIVERSAL=1     build for both Apple silicon and Intel Macs
set -e

APP=gomic.app
VERSION=${VERSION:-0.0.0}

if [ "${UNIVERSAL:-}" = 1 ]; then
    CGO_ENABLED=1 GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o gomic-arm64 .
    CGO_ENABLED=1 GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o gomic-amd64 .
    lipo -create -output gomic gomic-arm64 gomic-amd64
    rm gomic-arm64 gomic-amd64
else
    go build -o gomic .
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS"
mv gomic "$APP/Contents/MacOS/gomic"

# macOS refuses microphone access to an app without NSMicrophoneUsageDescription.
cat > "$APP/Contents/Info.plist" <<EOF
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
    <string>11.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>NSMicrophoneUsageDescription</key>
    <string>gomic reads your audio input to show its level and warn when it clips.</string>
</dict>
</plist>
EOF

# Ad-hoc signature so macOS remembers the microphone permission for this build.
codesign --force --sign - "$APP"

echo "Built $APP. Start it with: open $APP"
