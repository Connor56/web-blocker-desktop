package installer

import (
	"annoying-web-blocker-desktop/internal/platformpaths"
	"path/filepath"
)

const (
	Label = "com.annoyingwebblocker.desktop"

	PlistPath = "/Library/LaunchDaemons/" + Label + ".plist"

	ClientTokenName = "Annoying Web Blocker Desktop/client-token"
)

var BinaryPath = platformpaths.BinaryPath
var ApplicationDir = platformpaths.ApplicationDir
var SystemTokenPath = filepath.Join(ApplicationDir, "ui-token")

func Plist() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + Label + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + BinaryPath + `</string>
    <string>--daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
</dict>
</plist>
`)
}
