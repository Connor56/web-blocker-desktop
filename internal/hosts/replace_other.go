//go:build !windows

package hosts

import "os"

func replaceFile(source, destination string) error { return os.Rename(source, destination) }
