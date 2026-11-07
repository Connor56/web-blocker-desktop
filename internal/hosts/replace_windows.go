package hosts

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// ReplaceFileW preserves the target DACL, unlike renaming a newly created file.
func replaceFile(source, destination string) error {
	dst, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	src, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFileW.Call(uintptr(unsafe.Pointer(dst)), uintptr(unsafe.Pointer(src)), 0, 0, 0, 0)
	if result == 0 {
		return callErr
	}
	return nil
}
