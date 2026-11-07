package hosts

import (
	"annoying-web-blocker-desktop/internal/platformpaths"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	beginMarker = "# BEGIN ANNOYING WEB BLOCKER DESKTOP"
	endMarker   = "# END ANNOYING WEB BLOCKER DESKTOP"
)

var DefaultPath = platformpaths.HostsPath

type Manager struct {
	Path string

	mu sync.Mutex
}

func NewManager(path string) *Manager {
	return &Manager{Path: path}
}

func Render(domains []string) []byte {
	var b strings.Builder
	b.WriteString(beginMarker)
	b.WriteByte('\n')
	for _, domain := range domains {
		fmt.Fprintf(&b, "127.0.0.1 %s\n::1 %s\n", domain, domain)
	}
	b.WriteString(endMarker)
	b.WriteByte('\n')
	return []byte(b.String())
}

func Transform(original []byte, active bool, domains []string) ([]byte, bool, error) {
	start, end, found, err := managedRange(original)
	if err != nil {
		return nil, false, err
	}

	if !active {
		if !found {
			return original, false, nil
		}
		result := appendCopy(original[:start], original[end:])
		return result, true, nil
	}

	block := append([]byte{'\n'}, Render(domains)...)
	if found {
		result := appendCopy(original[:start], block)
		result = append(result, original[end:]...)
		if bytes.Equal(result, original) {
			return original, false, nil
		}
		return result, true, nil
	}

	result := appendCopy(original, block)
	return result, true, nil
}

func (m *Manager) Reconcile(active bool, domains []string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, err := os.Stat(m.Path)
	if err != nil {
		return false, fmt.Errorf("stat hosts file: %w", err)
	}
	original, err := os.ReadFile(m.Path)
	if err != nil {
		return false, fmt.Errorf("read hosts file: %w", err)
	}
	updated, changed, err := Transform(original, active, domains)
	if err != nil {
		return false, err
	}
	if changed {
		if err := atomicReplace(m.Path, updated, info); err != nil {
			return false, err
		}
		info, err = os.Stat(m.Path)
		if err != nil {
			return false, fmt.Errorf("stat updated hosts file: %w", err)
		}
	}
	return changed, nil
}

func managedRange(data []byte) (start, end int, found bool, err error) {
	type marker struct {
		start, end int
		isBegin    bool
	}
	var markers []marker
	for pos := 0; pos < len(data); {
		lineStart := pos
		newline := bytes.IndexByte(data[pos:], '\n')
		lineEnd := len(data)
		next := len(data)
		if newline >= 0 {
			lineEnd = pos + newline
			next = lineEnd + 1
		}
		line := bytes.TrimSuffix(data[lineStart:lineEnd], []byte{'\r'})
		switch string(line) {
		case beginMarker:
			markers = append(markers, marker{start: lineStart, end: next, isBegin: true})
		case endMarker:
			markers = append(markers, marker{start: lineStart, end: next})
		}
		pos = next
	}
	if len(markers) == 0 {
		return 0, 0, false, nil
	}
	if len(markers) != 2 || !markers[0].isBegin || markers[1].isBegin {
		return 0, 0, false, errors.New("hosts file has malformed or duplicate Annoying Web Blocker markers")
	}
	start = markers[0].start
	if start > 0 && data[start-1] == '\n' {
		start--
	}
	return start, markers[1].end, true, nil
}

func atomicReplace(path string, data []byte, original os.FileInfo) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".annoying-web-blocker-hosts-*")
	if err != nil {
		return fmt.Errorf("create temporary hosts file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if err := temp.Chmod(original.Mode().Perm()); err != nil {
		temp.Close()
		return fmt.Errorf("preserve hosts permissions: %w", err)
	}
	if err := preserveOwner(tempName, original); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary hosts file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary hosts file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary hosts file: %w", err)
	}
	if err := replaceFile(tempName, path); err != nil {
		return fmt.Errorf("replace hosts file: %w", err)
	}
	return nil
}

func appendCopy(parts ...[]byte) []byte {
	size := 0
	for _, part := range parts {
		size += len(part)
	}
	result := make([]byte, 0, size)
	for _, part := range parts {
		result = append(result, part...)
	}
	return result
}
