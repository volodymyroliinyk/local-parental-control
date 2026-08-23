# Local Parental Control

Local Parental Control is a service for Ubuntu and other Linux systems. A root daemon limits a configured user's total daily device time and login hours, and measures how long selected applications run. The service has no graphical interface and does not require a network connection or an online account.

This version controls native processes only.

Detailed documentation:

- [Installation and administration](docs/INSTALL.md)
- [Development guide](docs/DEVELOPMENT.md)
- [Security model](docs/SECURITY.md)

## How it works

- The daemon starts at boot through `systemd` and scans `/proc` at a configurable interval.
- Processes are attributed by numeric UID and matched against the resolved `/proc/PID/exe` path.
- An application accrues wall-clock time once while one or more matching processes run.
- A user's device allowance accrues once while any process owned by that user is present, regardless of the number of processes.
- Outside the allowed hours, after the daily device allowance, or during a mandatory break, the daemon asks `systemd-logind` to lock the controlled user's graphical sessions.
- At the limit, every matching process receives `SIGTERM`; processes still present after the grace period receive `SIGKILL`.
- Usage is stored in `/var/lib/local-parental-control/usage.json` with atomic writes and resets on the next poll after local midnight.
- Administrative commands use a root-only Unix socket. The child account cannot reset counters or reload configuration.

The service controls configured applications; it is not a general application allowlist. This avoids breaking GNOME, D-Bus, PipeWire, portals, and other essential desktop-session processes.

## Requirements

- Ubuntu or a comparable Linux distribution with `systemd` and `/proc`
- Go 1.26 or newer to build
- root access for installation and administration
- a non-administrator account for the controlled user

## Configuration

Copy and edit [`example/config.json`](example/config.json). The service reads `/etc/local-parental-control/config.json`. This file is owned by `root:root` with mode `0600`.

```json
{
  "timezone": "America/Toronto",
  "poll_interval_seconds": 2,
  "termination_grace_seconds": 15,
  "users": {
    "child": {
      "daily_device_minutes": 180,
      "continuous_use_minutes": 60,
      "break_minutes": 10,
      "allowed_from": "11:00",
      "allowed_until": "20:00",
      "applications": [
        {
          "id": "vlc",
          "name": "VLC media player",
          "executables": ["/usr/bin/vlc"],
          "daily_minutes": 60
        }
      ]
    }
  }
}
```

Important details:

- Usernames must already exist when configuration is validated or loaded.
- Each executable path must be absolute and unique within a user's rules.
- Executables are resolved through symlinks when the production configuration
  is loaded. The resolved file must be owned by root, executable, regular, and
  not writable by group or other users.
- Put all real executables used by an application in the same rule. Wrapper scripts and `.desktop` files are not executable identities.
- Use `readlink -f /proc/PID/exe` while an application runs to discover its actual executable.
- `daily_device_minutes` is required and must be 1–1440 minutes.
- `continuous_use_minutes` and `break_minutes` are 1–1440 minutes. When omitted, they default to 60 and 10 respectively.
- `allowed_from` and `allowed_until` are required 24-hour `HH:MM` values. The start is inclusive, the end is exclusive, and the start must be earlier than the end; overnight windows are not supported.
- Application rules are optional. An application limit is 1–1440 minutes. The polling interval and termination grace period are 1–60 seconds.
- Unknown JSON fields are rejected to catch misspelled settings.

When a limit is reached, the daemon sends `SIGTERM` first and waits for the
configured grace period. It then uses `SIGKILL` if the same process is still
running. Keep enough grace time for applications to save their data; the
default is 15 seconds.

Device limits are enforced at the next polling iteration with
`loginctl lock-session`. The daemon resolves the graphical sessions belonging
to the configured numeric UID and repeatedly requests a lock while access is
blocked. Programs remain running and unsaved work is retained. The desktop
environment must support systemd-logind's lock request; otherwise enforcement
is not possible and the daemon records an error. `lpctl reset USER`
resets both the device and application counters; resetting one application does
not reset device time.

After `continuous_use_minutes` of use, the screen remains locked for
`break_minutes`. Break progress is persisted, so restarting the daemon does not
cancel an active break. The default 60/10 schedule follows general ergonomic
guidance from the [American Academy of Pediatrics](https://www.healthychildren.org/English/health-issues/conditions/eyes/Pages/What-Too-Much-Screen-Time-Does-to-Your-Childs-Eyes.aspx)
and [CCOHS](https://www.ccohs.ca/oshanswers/ergonomics/office/stretching.html)
to leave the screen for 5–10 minutes each hour. It is separate from short
eye-rest reminders such as the 20-20-20 rule.

Snap and Flatpak applications may run through shared launchers or sandbox-specific paths. Confirm their real `/proc/PID/exe` values before adding them. Do not put a shared executable such as `/usr/bin/java` in a rule unless all programs using it should share that limit.

## Install

See the [installation guide](docs/INSTALL.md) for prerequisites,
troubleshooting, updates, and removal.

Review the scripts and example configuration first, then run:

```bash
./scripts/build.sh
sudo ./scripts/install.sh
```

The build must run as the administrator account, not as root. The installer
accepts only regular, administrator-owned binaries that are not writable by
group or other users. AppArmor must be installed and enabled.

On a new installation, the script copies the example configuration. It will not start the daemon until the configured user exists and the file validates. After editing:

```bash
sudo lpctl validate
sudo systemctl enable --now local-parental-control.service
```

Existing `/etc/local-parental-control/config.json` files are preserved during upgrades.

## Administration

```text
sudo lpctl validate
sudo lpctl status
sudo lpctl reload
sudo lpctl reset child
sudo lpctl reset child vlc
sudo systemctl status local-parental-control.service
sudo journalctl -u local-parental-control.service
```

If a new configuration is invalid, `reload` reports an error and the daemon continues using the previous rules. Editing the file alone does not activate the changes.

To update an installation made with `scripts/install.sh` while preserving the configuration and usage data:

```bash
./scripts/build.sh
sudo ./scripts/update.sh
```

To uninstall binaries and the service while preserving configuration and usage history:

```bash
sudo ./scripts/uninstall.sh
```

## Development

See the [development guide](docs/DEVELOPMENT.md) before changing process
handling, root scripts, systemd confinement, or AppArmor policy.

```bash
make check
make build
# Or directly: ./scripts/build.sh
```

Build output is placed in `bin/`. Tests do not require root.

Add user-visible changes to the `Unreleased` section of the changelog:

```bash
./scripts/update-changelog.sh add added "Describe the new behavior"
./scripts/update-changelog.sh add fixed "Describe the corrected behavior"
```

When preparing a release, move the `Unreleased` entries under a version and date:

```bash
./scripts/update-changelog.sh release 0.2.0
```

The release script verifies the repository, runs tests, builds release artifacts,
updates the changelog, updates `develop` and `main`, creates a version tag, and
publishes a GitHub Release through the GitHub CLI. Run a dry check first:

```bash
./scripts/release.sh 0.2.0 --dry-run
./scripts/release.sh 0.2.0
```

The normal release command shows its plan and requires the exact confirmation
`RELEASE 0.2.0` before it creates commits, tags, or remote changes. Use `--draft`
to create a draft GitHub Release. Do not run a release until manual application
and installation tests are complete.

To build a native Debian/Ubuntu package:

```bash
LPC_VERSION=0.1.0 ./scripts/build-deb.sh
sudo apt install ./dist/local-parental-control_0.1.0_amd64.deb
```

The package installs the daemon and CLI in `/usr/sbin`, the systemd unit in
`/lib/systemd/system`, and a dpkg-managed configuration in
`/etc/local-parental-control/config.json`. It also installs an enforcing
AppArmor profile. If the example username does not
exist, installation succeeds but the service remains disabled until the
configuration validates. Package upgrades preserve the administrator's config.

## Security model and limitations

The controlled account must not have sudo/root access. Root can always stop or alter this service. Process polling means a newly launched over-limit application can run for up to one polling interval. Executables copied to a different path do not match the original rule, so the child account should not have access to terminals, interpreters, alternative launchers, package installation, AppImage execution, Wine, or virtual machines if those are realistic bypasses. Those OS-level restrictions are administrator policy and are deliberately not applied automatically by this project.

Changing the wall clock can affect the daily reset, allowed-hours checks, and break deadlines. The supplied service cannot change the system clock (`ProtectClock=true`), but the administrator must also ensure the child account lacks permission to do so. Device presence is inferred from processes owned by the configured UID; lingering user services and processes that remain active while the screen is locked can continue consuming time.

## License

This project is licensed under the [MIT License](LICENSE).
