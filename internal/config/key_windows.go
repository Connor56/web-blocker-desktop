//go:build windows

package config

import (
	"annoying-web-blocker-desktop/internal/platformpaths"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protectKey(key []byte) ([]byte, error) {
	input := bytesBlob(key)
	var output windows.DataBlob
	if err := windows.CryptProtectData(
		&input, nil, nil, 0, nil, windows.CRYPTPROTECT_LOCAL_MACHINE, &output,
	); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func unprotectKey(protected []byte) ([]byte, error) {
	input := bytesBlob(protected)
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, 0, &output); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func bytesBlob(value []byte) windows.DataBlob {
	if len(value) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
}

func secureKeyFile(path string) error {
	if !strings.EqualFold(filepath.Clean(filepath.Dir(path)), filepath.Clean(platformpaths.ApplicationDir)) {
		return nil
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
}
