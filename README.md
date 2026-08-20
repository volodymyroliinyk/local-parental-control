# Local Parental Control

Local Parental Control is a service for Ubuntu and other Linux systems. A root daemon measures how long configured users run selected applications and terminates an application after its daily allowance is exhausted. The service has no graphical interface and does not require a network connection or an online account.

This version controls native processes only.

## How it works

- The daemon starts at boot through `systemd` and scans `/proc` at a configurable interval.
- Processes are attributed by numeric UID and matched against the resolved `/proc/PID/exe` path.
- An application accrues wall-clock time once while one or more matching processes run.
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
  "termination_grace_seconds": 3,
  "users": {
    "child": {
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
- Put all real executables used by an application in the same rule. Wrapper scripts and `.desktop` files are not executable identities.
- Use `readlink -f /proc/PID/exe` while an application runs to discover its actual executable.
- A limit is 1–1440 minutes. The polling interval and termination grace period are 1–60 seconds.
- Unknown JSON fields are rejected to catch misspelled settings.

Snap and Flatpak applications may run through shared launchers or sandbox-specific paths. Confirm their real `/proc/PID/exe` values before adding them. Do not put a shared executable such as `/usr/bin/java` in a rule unless all programs using it should share that limit.

## Install

Review the scripts and example configuration first, then run:

```bash
sudo ./scripts/install.sh
```

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
sudo ./scripts/update.sh
```

To uninstall binaries and the service while preserving configuration and usage history:

```bash
sudo ./scripts/uninstall.sh
```

## Development

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

To build a native Debian/Ubuntu package:

```bash
LPC_VERSION=0.1.0 ./scripts/build-deb.sh
sudo apt install ./dist/local-parental-control_0.1.0_amd64.deb
```

The package installs the daemon and CLI in `/usr/sbin`, the systemd unit in
`/lib/systemd/system`, and a dpkg-managed configuration in
`/etc/local-parental-control/config.json`. If the example username does not
exist, installation succeeds but the service remains disabled until the
configuration validates. Package upgrades preserve the administrator's config.

## Security model and limitations

The controlled account must not have sudo/root access. Root can always stop or alter this service. Process polling means a newly launched over-limit application can run for up to one polling interval. Executables copied to a different path do not match the original rule, so the child account should not have access to terminals, interpreters, alternative launchers, package installation, AppImage execution, Wine, or virtual machines if those are realistic bypasses. Those OS-level restrictions are administrator policy and are deliberately not applied automatically by this project.

Changing the wall clock can affect the daily reset. The supplied service cannot change the system clock (`ProtectClock=true`), but the administrator must also ensure the child account lacks permission to do so.

## License

This project is licensed under the [MIT License](LICENSE).
