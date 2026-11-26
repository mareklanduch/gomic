#!/bin/sh
# Builds gomic.app on macOS. Needs Go and the Xcode command line tools
# (xcode-select --install). Run from this folder: sh build_mac.sh
set -e

APP=gomic.app

go build -o gomic .

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS"
mv gomic "$APP/Contents/MacOS/gomic"

# macOS refuses microphone access to an app without NSMicrophoneUsageDescription.
cat > "$APP/Contents/Info.plist" <<'EOF'
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
    <string>0.1</string>
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
