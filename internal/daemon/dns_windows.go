package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func flushDNS() error {
	output, err := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "ipconfig.exe"), "/flushdns").CombinedOutput()
	if err != nil {
		return fmt.Errorf("flush DNS: %w (%s)", err, output)
	}
	return nil
}
