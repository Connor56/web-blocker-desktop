//go:build unix

package hosts

import (
	"fmt"
	"os"
	"syscall"
)

func preserveOwner(path string, original os.FileInfo) error {
	stat, ok := original.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := os.Chown(path, int(stat.Uid), int(stat.Gid)); err != nil {
		return fmt.Errorf("preserve hosts ownership: %w", err)
	}
	return nil
}
