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
	"path/filepath"
	"strings"
	"time"

	"annoying-web-blocker-desktop/internal/config"
	"annoying-web-blocker-desktop/internal/hosts"
	"annoying-web-blocker-desktop/internal/web"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "AnnoyingWebBlockerDesktop"

func IsInstalled() bool { _, err := os.Stat(BinaryPath); return err == nil }
func Install() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return runAuthorized([]string{executable, "--install-root", "--source", executable, "--user-home", home, "--uid", "0", "--gid", "0"})
}
func InstallRoot(source, home string, _, _ int) error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("installation requires Administrator privileges")
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(home) {
		return errors.New("absolute source and user home paths required")
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("source must be a regular file")
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	existing, err := manager.OpenService(serviceName)
	if err == nil {
		defer existing.Close()
		if err := stopService(existing); err != nil {
			return err
		}
	} else if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return err
	}
	if err := os.MkdirAll(ApplicationDir, 0755); err != nil {
		return err
	}
	// Only administrators and SYSTEM may modify installed code or configuration.
	if err := setACL(ApplicationDir, true); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(source), filepath.Clean(BinaryPath)) {
		input, err := os.Open(source)
		if err != nil {
			return err
		}
		defer input.Close()
		temp, err := os.CreateTemp(ApplicationDir, ".install-*.exe")
		if err != nil {
			return err
		}
		name := temp.Name()
		defer os.Remove(name)
		_, copyErr := io.Copy(temp, input)
		closeErr := temp.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Rename(name, BinaryPath); err != nil {
			return err
		}
	}
	token, err := os.ReadFile(SystemTokenPath)
	if errors.Is(err, os.ErrNotExist) {
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		token = []byte(base64.RawURLEncoding.EncodeToString(random))
		file, err := os.OpenFile(SystemTokenPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if err := setACL(SystemTokenPath, false); err != nil {
			file.Close()
			return err
		}
		_, writeErr := file.Write(token)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	} else if err != nil {
		return err
	}
	if strings.TrimSpace(string(token)) == "" {
		return errors.New("service token is empty")
	}
	if err := setACL(SystemTokenPath, false); err != nil {
		return err
	}
	if err := config.Initialize(config.DefaultPath); err != nil {
		return err
	}
	client := clientTokenPath(home)
	if err := os.MkdirAll(filepath.Dir(client), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(client, token, 0600); err != nil {
		return err
	}
	service := existing
	if service == nil {
		service, err = manager.CreateService(serviceName, BinaryPath, mgr.Config{DisplayName: "Annoying Web Blocker Desktop", StartType: mgr.StartAutomatic, ErrorControl: mgr.ErrorNormal, Description: "Enforces the configured website blocking schedule."}, "--daemon")
		if err != nil {
			return err
		}
		defer service.Close()
	}
	if err := service.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}}, 86400); err != nil {
		return err
	}
	if err := service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return err
	}
	return service.Start()
}
func stopService(service *mgr.Service) error {
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	if status.State != svc.StopPending {
		if _, err := service.Control(svc.Stop); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status, err = service.Query()
		if err != nil {
			return err
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timed out stopping blocker service")
}
func Uninstall() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := runAuthorized([]string{executable, "--uninstall-root"}); err != nil {
		return err
	}
	path, err := ClientTokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func UninstallRoot() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("removal requires Administrator privileges")
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(serviceName)
	if err == nil {
		defer service.Close()
		if err := stopService(service); err != nil {
			return err
		}
	} else if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return err
	}
	changed, err := hosts.NewManager(hosts.DefaultPath).Reconcile(false, nil)
	if err != nil {
		return err
	}
	if changed {
		if output, err := exec.Command(systemCommand("ipconfig.exe"), "/flushdns").CombinedOutput(); err != nil {
			return fmt.Errorf("flush DNS: %w (%s)", err, output)
		}
	}
	if service != nil {
		if err := service.Delete(); err != nil {
			return err
		}
	}
	// Remove only known installation files, leaving any unrelated directory contents alone.
	for _, path := range []string{
		SystemTokenPath,
		config.DefaultPath,
		config.DefaultPath + ".bak",
		config.DefaultPath + ".key",
		config.LegacyPath,
		BinaryPath,
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s (run uninstall from your original downloaded executable): %w", path, err)
		}
	}
	return nil
}
func ClientTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return clientTokenPath(home), nil
}
func clientTokenPath(home string) string {
	return filepath.Join(home, "AppData", "Local", "Annoying Web Blocker Desktop", "client-token")
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
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", web.Address, 250*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("settings server unavailable: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	target := web.BaseURL + "/#token=" + strings.TrimSpace(string(token))
	return exec.Command(systemCommand("WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", "Start-Process -FilePath "+psQuote(target)).Run()
}
func systemCommand(parts ...string) string {
	return filepath.Join(append([]string{os.Getenv("SystemRoot"), "System32"}, parts...)...)
}
func setACL(path string, directory bool) error {
	sddl := "D:P(A;;FA;;;SY)(A;;FA;;;BA)"
	if directory {
		sddl = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;GRGX;;;BU)"
	}
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func psQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func runAuthorized(args []string) error {
	// Escape arguments for Windows command-line parsing, then quote as PowerShell literals.
	quoted := make([]string, len(args)-1)
	for i, arg := range args[1:] {
		quoted[i] = windows.EscapeArg(arg)
	}
	script := "$p = Start-Process -FilePath " + psQuote(args[0]) + " -ArgumentList " + psQuote(strings.Join(quoted, " ")) + " -Verb RunAs -Wait -PassThru; exit $p.ExitCode"
	output, err := exec.Command(systemCommand("WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("administrator operation failed: %w (%s)", err, output)
	}
	return nil
}
