# Annoying Web Blocker Desktop

A small macOS and Windows background daemon that enforces a recurring daily website block through `/etc/hosts`. It installs itself as a root `launchd` daemon on macOS or a LocalSystem Windows Service, starts at boot, checks its desired state every 30 seconds, and exposes a minimal configuration page on localhost.

## Requirements

- macOS 12 or newer, or Windows 10/11 (64-bit)
- Administrator access during installation and removal
- Go 1.23 or newer when building from source

The app has no Firebase or runtime network dependency. Building Windows support downloads the Go `golang.org/x/sys` module.

## Build

Build on the target Mac:

```sh
go build -trimpath -o annoying-web-blocker-desktop ./cmd/annoying-web-blocker-desktop
```

Cross-build from another system:

```sh
GOOS=darwin GOARCH=arm64 go build -trimpath -o annoying-web-blocker-desktop-arm64 ./cmd/annoying-web-blocker-desktop
GOOS=darwin GOARCH=amd64 go build -trimpath -o annoying-web-blocker-desktop-amd64 ./cmd/annoying-web-blocker-desktop
```

Use `arm64` for Apple silicon and `amd64` for Intel Macs.

## Install and use

Make the downloaded or built file executable, then run it:

```sh
chmod +x ./annoying-web-blocker-desktop
./annoying-web-blocker-desktop
```

On macOS, the first run displays the normal administrator dialog, installs and starts the daemon, and opens the settings page in the default browser. Later runs simply open the settings page:

```sh
./annoying-web-blocker-desktop open
```

To replace an installed version with the current executable:

```sh
./annoying-web-blocker-desktop install
```

The browser page configures:

- Exact domain names to block
- Whether each domain is enabled
- Any number of daily blocking windows for each domain

Overnight windows are supported; for example, `22:00` to `06:00`. Start and end times cannot be identical.
An enabled domain with no schedules is blocked permanently.
Adding a domain or expanding its blocked times takes effect immediately. Disabling or
deleting a domain or reducing its blocked minutes requires the 120-second moving-dot
challenge. The dot must be clicked at least once every five seconds or the full timer
restarts. Removing the final schedule instead makes the block permanent and is a tightening
change. This is enforced by the local service API, not only by the browser page.

## Windows build and installation

From PowerShell, build:

```powershell
go build -trimpath -o annoying-web-blocker-desktop.exe ./cmd/annoying-web-blocker-desktop
```

Then run with:

```powershell
.\annoying-web-blocker-desktop.exe
```

Approve the UAC prompt once. The app installs the `AnnoyingWebBlockerDesktop` service under LocalSystem, sets automatic startup and restart-on-failure, and opens the settings page. The service runs independently of the browser and your login session.

```powershell
.\annoying-web-blocker-desktop.exe open
.\annoying-web-blocker-desktop.exe install
.\annoying-web-blocker-desktop.exe uninstall
```

Keep the original executable for updates and removal; Windows can lock an installed executable while it is running. Run these commands as your ordinary user and approve elevation when prompted. Installation from an already elevated shell uses that shell's user profile for the browser token.

Windows files:

- Binary and config: `%ProgramData%\Annoying Web Blocker Desktop\`
- Service token: `ui-token` in that directory, accessible only to Administrators and SYSTEM
- Browser token: `%USERPROFILE%\AppData\Local\Annoying Web Blocker Desktop\client-token`
- Hosts file: the Windows system directory's `System32\drivers\etc\hosts`

Service files are protected against modification by ordinary users. DNS is refreshed using `ipconfig /flushdns` after changes. Windows Defender or other security software can reject hosts-file changes; failures appear in the settings page.

## Recovery and status

A malformed configuration does not prevent the settings page from opening. It shows an error and default form values so you can save a repaired configuration. Hosts-file errors are reported without terminating the daemon. If the localhost port is occupied, enforcement continues and the daemon retries the settings server every five seconds.

Settings saves and scheduled reconciliation share one lock, so overlapping saves cannot apply an older schedule after a newer one. The page checks enforcement status every five seconds; DNS-flush failures are reported and retried. Schedule changes and resume from sleep are reconciled at the next 30-second check. Blocking uses local clock time, so manually changing the system clock or timezone changes the active window.

The challenge is deliberate friction rather than tamper-proof security: an administrator
can still stop the service or edit its files. Hosts entries do not close existing network
connections or cover unlisted subdomains.

## Schedule storage

The schedule is serialized as JSON internally, then authenticated and encrypted
with AES-256-GCM in `config.bin`. Its random machine key is stored separately in
`config.bin.key`: Windows protects it with machine-scope DPAPI and an
Administrators/SYSTEM-only ACL; macOS stores it as a root-only `0600` file.
`config.bin.bak` retains the previous authenticated configuration.

Each site has an exact `domain`, an `enabled` flag, and a `schedules` array of
`start`/`end` times in local 24-hour `HH:MM` format. Existing `config.json` files
are encrypted and removed during installation. If the primary file is corrupt,
the authenticated backup is used. If protected storage becomes unavailable
while running, the daemon continues reconciling the last valid in-memory
schedule rather than treating it as an empty configuration.

## Domain limitations

The hosts file supports exact hostnames only. It cannot express wildcard domains. Blocking `example.com` does not automatically block `www.example.com`, `m.example.com`, or other subdomains; add each required hostname to the list.

Each configured domain receives both an IPv4 (`127.0.0.1`) and IPv6 (`::1`) entry. DNS caches are flushed after a change.

## Safe hosts-file behavior

The daemon owns only the section between:

```text
# BEGIN ANNOYING WEB BLOCKER DESKTOP
# END ANNOYING WEB BLOCKER DESKTOP
```

Content outside those markers is preserved. Updates use a temporary file and replacement while retaining Unix permissions and ownership on macOS, and the target access-control list through `ReplaceFileW` on Windows. Malformed or duplicate app markers cause the update to stop instead of risking unrelated content.

The daemon reconciles immediately when settings are saved and every 30 seconds thereafter. It reads the hosts file on every check, including when file metadata is unchanged, and avoids rewriting already-correct content.

## Installed files

- Daemon: `/Library/PrivilegedHelperTools/com.annoyingwebblocker.desktop`
- Launch daemon: `/Library/LaunchDaemons/com.annoyingwebblocker.desktop.plist`
- Configuration and service token: `/Library/Application Support/Annoying Web Blocker Desktop/`
- Browser token: `~/Library/Application Support/Annoying Web Blocker Desktop/client-token`

The UI listens only on `127.0.0.1:47823`. Its configuration API requires a random bearer token and validates the request host and origin.

## Uninstall

Run the same executable:

```sh
./annoying-web-blocker-desktop uninstall
```

Removal asks for administrator access, stops the daemon, removes its managed hosts section, flushes DNS, and deletes the installed service files.

If the original executable is unavailable, run the installed copy first:

```sh
sudo /Library/PrivilegedHelperTools/com.annoyingwebblocker.desktop --uninstall-root
```

## Unsigned build warning

Locally built binaries are unsigned. A downloaded unsigned binary may be blocked by Gatekeeper. For personal development, use Finder's **Open** action and explicitly approve it in macOS Privacy & Security. Public distribution should use an Apple Developer ID signature and notarization; this project does not yet provide that packaging.

## Development checks

```sh
gofmt -w .
go test ./...
go vet ./...
```

Unit and integration tests use temporary hosts files and do not install a service or change the real hosts file. Native installation, UAC/authorization, service startup, sleep/reboot behavior, and DNS effects need testing on their respective operating systems. Cross-compilation checks build compatibility but cannot verify native service integration.
