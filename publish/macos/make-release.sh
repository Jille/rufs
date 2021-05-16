#!/bin/bash

# Clean up
if [ -d "rufs.app" ]; then
	rm -rf rufs.app
fi
if [ -f "rufs.dmg" ]; then
	rm rufs.dmg
fi
if [ -d "rufs.iconset" ]; then
	rm -rf rufs.iconset
fi
if [ -d "tempdir" ]; then
	rm -rf tempdir
fi

# Create icons
mkdir -p rufs.iconset
sips -z 16 16 rufs-macos.png --out rufs.iconset/icon_16x16.png
sips -z 32 32 rufs-macos.png --out rufs.iconset/icon_16x16@2x.png
sips -z 32 32 rufs-macos.png --out rufs.iconset/icon_32x32.png
sips -z 64 64 rufs-macos.png --out rufs.iconset/icon_32x32@2x.png
sips -z 128 128 rufs-macos.png --out rufs.iconset/icon_128x128.png
sips -z 256 256 rufs-macos.png --out rufs.iconset/icon_128x128@2x.png
sips -z 256 256 rufs-macos.png --out rufs.iconset/icon_256x256.png
sips -z 512 512 rufs-macos.png --out rufs.iconset/icon_256x256@2x.png
sips -z 512 512 rufs-macos.png --out rufs.iconset/icon_512x512.png
cp rufs-macos.png rufs.iconset/icon_512x512@2x.png
iconutil -c icns -o rufs.icns rufs.iconset
rm -rf rufs.iconset

# Create app
mkdir -p rufs.app/Contents/MacOS
GOOS=darwin GOARCH=amd64 go build -o rufs.app/Contents/MacOS ../../client
cp rufs.sh rufs.app/Contents/MacOS

cat <<EOF >rufs.app/Contents/Info.plist
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleGetInfoString</key>
  <string>RUFS</string>
  <key>CFBundleExecutable</key>
  <string>rufs.sh</string>
  <key>CFBundleIdentifier</key>
  <string>com.github.sgielen.rufs</string>
  <key>CFBundleName</key>
  <string>RUFS</string>
  <key>CFBundleIconFile</key>
  <string>rufs.icns</string>
  <key>CFBundleShortVersionString</key>
  <string>0.01</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>IFMajorVersion</key>
  <integer>0</integer>
  <key>IFMinorVersion</key>
  <integer>1</integer>
</dict>
</plist>
EOF

mkdir -p rufs.app/Contents/Resources
cp rufs.icns rufs.app/Contents/Resources

# Create dmg
mkdir tempdir
cp -R rufs.app tempdir
create-dmg/create-dmg \
  --volname "RUFS" \
  --volicon "rufs.icns" \
  --background "background.png" \
  --window-pos 200 120 \
  --window-size 800 425 \
  --icon-size 100 \
  --icon "rufs.app" 250 225 \
  --hide-extension "rufs.app" \
  --app-drop-link 550 225 \
  rufs.dmg \
  "tempdir"
rm -rf tempdir
