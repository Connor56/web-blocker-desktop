//go:build darwin

package daemon

import (
	"errors"
	"fmt"
	"os/exec"
)

func flushDNS() error {
	var errs []error
	if output, err := exec.Command("/usr/bin/dscacheutil", "-flushcache").CombinedOutput(); err != nil {
		errs = append(errs, fmt.Errorf("dscacheutil: %w (%s)", err, output))
	}
	if output, err := exec.Command("/usr/bin/killall", "-HUP", "mDNSResponder").CombinedOutput(); err != nil {
		errs = append(errs, fmt.Errorf("mDNSResponder: %w (%s)", err, output))
	}
	return errors.Join(errs...)
}
