//go:build !darwin && !windows

package installer

import "errors"

var errUnsupported = errors.New("Annoying Web Blocker installation is supported on macOS and Windows")

func IsInstalled() bool                              { return false }
func Install() error                                 { return errUnsupported }
func InstallRoot(_ string, _ string, _, _ int) error { return errUnsupported }
func Uninstall() error                               { return errUnsupported }
func UninstallRoot() error                           { return errUnsupported }
func OpenUI() error                                  { return errUnsupported }
func ClientTokenPath() (string, error)               { return "", errUnsupported }
