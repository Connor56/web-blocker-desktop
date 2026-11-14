//go:build !windows

package config

import "os"

func protectKey(key []byte) ([]byte, error) {
	return append([]byte(nil), key...), nil
}

func unprotectKey(protected []byte) ([]byte, error) {
	return append([]byte(nil), protected...), nil
}

func secureKeyFile(path string) error {
	return os.Chmod(path, 0600)
}
