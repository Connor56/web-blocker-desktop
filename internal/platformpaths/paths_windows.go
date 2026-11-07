package platformpaths

import (
	"golang.org/x/sys/windows"
	"path/filepath"
)

func knownFolder(id *windows.KNOWNFOLDERID) string {
	path, err := windows.KnownFolderPath(id, 0)
	if err != nil {
		panic(err)
	}
	return path
}

var ApplicationDir = filepath.Join(knownFolder(windows.FOLDERID_ProgramData), "Annoying Web Blocker Desktop")
var HostsPath = filepath.Join(knownFolder(windows.FOLDERID_Windows), "System32", "drivers", "etc", "hosts")
var BinaryPath = filepath.Join(ApplicationDir, "annoying-web-blocker-desktop.exe")
