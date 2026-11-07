//go:build windows

package hosts

import "os"

func preserveOwner(_ string, _ os.FileInfo) error {
	return nil
}
