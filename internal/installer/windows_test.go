//go:build windows

package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPowerShellQuote(t *testing.T) {
	if got := psQuote("a'b"); got != "'a''b'" {
		t.Fatal(got)
	}
}
func TestWindowsPaths(t *testing.T) {
	if !filepath.IsAbs(BinaryPath) || !strings.HasSuffix(BinaryPath, ".exe") {
		t.Fatal(BinaryPath)
	}
	if !strings.Contains(clientTokenPath(`C:\Users\Example`), `AppData\Local`) {
		t.Fatal("unexpected client token path")
	}
}
