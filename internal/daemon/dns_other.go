//go:build !darwin && !windows

package daemon

func flushDNS() error {
	return nil
}
