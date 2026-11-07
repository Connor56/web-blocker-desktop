package installer

import (
	"strings"
	"testing"
)

func TestPlistRunsDaemonAtStartup(t *testing.T) {
	plist := string(Plist())
	for _, required := range []string{Label, BinaryPath, "<string>--daemon</string>", "<key>RunAtLoad</key>", "<key>KeepAlive</key>"} {
		if !strings.Contains(plist, required) {
			t.Fatalf("plist does not contain %q", required)
		}
	}
}
