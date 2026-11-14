//go:build darwin

package installer

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"annoying-web-blocker-desktop/internal/config"
	"annoying-web-blocker-desktop/internal/hosts"
	"annoying-web-blocker-desktop/internal/web"
)

func IsInstalled() bool {
	_, binaryErr := os.Stat(BinaryPath)
	_, plistErr := os.Stat(PlistPath)
	return binaryErr == nil && plistErr == nil
}

func Install() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	current, err := user.Current()
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(current.Uid)
	if err != nil {
		return fmt.Errorf("parse user ID: %w", err)
	}
	gid, err := strconv.Atoi(current.Gid)
	if err != nil {
		return fmt.Errorf("parse group ID: %w", err)
	}
	args := []string{
		executable, "--install-root",
		"--source", executable,
		"--user-home", current.HomeDir,
		"--uid", strconv.Itoa(uid),
		"--gid", strconv.Itoa(gid),
	}
	return runAuthorized(args)
}

func InstallRoot(source, userHome string, uid, gid int) error {
	if os.Geteuid() != 0 {
		return errors.New("root installation must run as root")
	}
	if source == "" || !filepath.IsAbs(source) || userHome == "" || !filepath.IsAbs(userHome) || uid < 0 || gid < 0 {
		return errors.New("invalid installation arguments")
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect source executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("source executable is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(BinaryPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(ApplicationDir, 0755); err != nil {
		return err
	}
	if err := copyExecutable(source, BinaryPath); err != nil {
		return err
	}

	token, err := loadOrCreateToken()
	if err != nil {
		return err
	}
	if err := config.Initialize(config.DefaultPath); err != nil {
		return fmt.Errorf("initialize configuration: %w", err)
	}
	if err := writeClientToken(userHome, token, uid, gid); err != nil {
		return err
	}
	if err := writeRootFile(PlistPath, Plist(), 0644); err != nil {
		return err
	}

	_ = exec.Command("/bin/launchctl", "bootout", "system/"+Label).Run()
	if output, err := exec.Command("/bin/launchctl", "bootstrap", "system", PlistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("bootstrap launch daemon: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	if output, err := exec.Command("/bin/launchctl", "kickstart", "-k", "system/"+Label).CombinedOutput(); err != nil {
		return fmt.Errorf("start launch daemon: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func Uninstall() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := runAuthorized([]string{executable, "--uninstall-root"}); err != nil {
		return err
	}
	if path, err := ClientTokenPath(); err == nil {
		_ = os.Remove(path)
	}
	return nil
}

func UninstallRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("root uninstall must run as root")
	}
	_ = exec.Command("/bin/launchctl", "bootout", "system/"+Label).Run()
	manager := hosts.NewManager(hosts.DefaultPath)
	changed, err := manager.Reconcile(false, nil)
	if err != nil {
		return fmt.Errorf("remove managed hosts entries: %w", err)
	}
	if changed {
		_ = exec.Command("/usr/bin/dscacheutil", "-flushcache").Run()
		_ = exec.Command("/usr/bin/killall", "-HUP", "mDNSResponder").Run()
	}
	var errs []error
	for _, path := range []string{PlistPath, BinaryPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := os.RemoveAll(ApplicationDir); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func OpenUI() error {
	path, err := ClientTokenPath()
	if err != nil {
		return err
	}
	token, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read client token (try reinstalling): %w", err)
	}
	target := web.BaseURL + "/#token=" + strings.TrimSpace(string(token))
	if err := waitForUI(5 * time.Second); err != nil {
		return err
	}
	if err := exec.Command("/usr/bin/open", target).Run(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}

func waitForUI(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		connection, err := net.DialTimeout("tcp", web.Address, 250*time.Millisecond)
		if err == nil {
			connection.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon UI did not start at %s: %w", web.Address, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func ClientTokenPath() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(current.HomeDir, "Library", "Application Support", ClientTokenName), nil
}

func runAuthorized(args []string) error {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	command := strings.Join(quoted, " ")
	script := `do shell script "` + appleScriptQuote(command) + `" with administrator privileges`
	output, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("administrator authorization failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".annoying-web-blocker-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := io.Copy(temp, input); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0755); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chown(0, 0); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, destination); err != nil {
		return fmt.Errorf("install executable: %w", err)
	}
	return nil
}

func loadOrCreateToken() (string, error) {
	if data, err := os.ReadFile(SystemTokenPath); err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	if err := writeRootFile(SystemTokenPath, []byte(token+"\n"), 0600); err != nil {
		return "", err
	}
	return token, nil
}

func writeClientToken(home, token string, uid, gid int) error {
	dir := filepath.Join(home, "Library", "Application Support", "Annoying Web Blocker Desktop")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "Application Support", ClientTokenName)
	if err := writeRootFile(path, []byte(token+"\n"), 0600); err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}

func writeRootFile(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".annoying-web-blocker-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chown(0, 0); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func appleScriptQuote(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}
