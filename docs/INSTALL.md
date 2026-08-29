# Installation and administration

This guide applies to Ubuntu 24.04 and comparable Linux systems that use
systemd, AppArmor, and `/proc`.

## Before installation

The controlled user must be a normal local account without sudo access. This
service does not secure an administrator account and does not prevent a user
from running an application under a different executable path.

Install the required system packages:

```bash
sudo apt update
sudo apt install apparmor gnome-shell-extension-appindicator
```

Install a native Go 1.26 or newer toolchain when building from source and
confirm it with `go version`. The Debian package does not require Go after it
has been built.

## Install from source

Build as the administrator account. Do not run the build with sudo:

```bash
./scripts/build.sh
sudo ./scripts/install.sh
```

The installer verifies that all binaries are regular files owned by the
administrator who invoked sudo and that group or other users cannot modify
them. It then installs the daemon, CLI, child indicator, XDG autostart entry,
systemd unit, AppArmor profile, and documentation.

Discover application paths and edit the installed configuration:

```bash
sudo lpctl discover firefox
sudoedit /etc/local-parental-control/config.json
sudo lpctl validate
sudo systemctl enable --now local-parental-control.service
```

The configuration file must be owned by `root:root` with mode `0600`. Its
directory must be root-owned and must not be writable by group or other users.
Configured executables must resolve to ELF, root-owned executable files
that are not writable by group or other users.

For every controlled user, set `daily_device_minutes` and either `all_day: true`
or an allowed local-time window with `allowed_from` and `allowed_until` in
`HH:MM` format. All-day mode and clock-window fields are mutually exclusive.
The window end time is exclusive. `continuous_use_minutes` and `break_minutes` configure mandatory
breaks and default to 60 and 10 minutes when omitted. Device and application
usage is counted only while the user's graphical session is active and
unlocked. Locking the screen pauses usage; unlocking it resumes the saved
counters until the next local calendar day. Shutdown time is not counted.
Application rules may be empty when only device-wide controls are needed. See
the configuration section in the README for validation rules.

## Install the Debian package

Build the package without sudo:

```bash
LPC_VERSION=0.1.0 ./scripts/build-deb.sh
sudo apt install ./dist/local-parental-control_0.1.0_amd64.deb
```

If the example user does not exist or its configuration is otherwise invalid,
the package remains installed but the service stays disabled. Edit the
configuration, validate it, and start the service as shown above.

## Child indicator

After a successful installation or update, reboot or sign out and sign in to
the controlled account. A small time indicator then appears in the desktop
panel automatically. It shows remaining device time directly in the panel;
hovering over it shows the break and configured application allowances.

The indicator is intentionally quiet: it sends no notifications, plays no
sounds, and opens no windows. It has no administration controls. The daemon
identifies the local connection by numeric UID and returns only that user's
status. The indicator exits silently for users absent from the configuration.

On Ubuntu GNOME, the panel requires `gnome-shell-extension-appindicator`,
which is installed by the prerequisite command above and recommended by the
Debian package. If the indicator is absent, first sign out and back in, then
confirm that this package and the service are active.

## Add or change an application

Let `lpctl` find configuration-ready ELF paths, copy all relevant results into
one application rule, validate, and reload:

```bash
sudo lpctl discover firefox
sudoedit /etc/local-parental-control/config.json
sudo lpctl validate
sudo lpctl reload
```

Every printed `supported` path can be copied into `executables`. Discovery
resolves Snap launchers to stable real-ELF identities under
`/snap/PACKAGE/current`; never use `/snap/bin/*` manually. These identities
continue matching after Snap refreshes. Add every returned executable used by
the application to the same rule.

## Administration

Administrative commands require root:

```bash
sudo lpctl validate
sudo lpctl discover firefox
sudo lpctl status
sudo lpctl reload
sudo lpctl recover-state
sudo lpctl reset child
sudo lpctl reset child vlc
```

After editing the configuration, run `validate` and then `reload`. This applies
valid changes without restarting systemd. A failed reload leaves the previous
configuration active.

`State: RECOVERY REQUIRED` means usage data could not be trusted or persisted.
The daemon remains running and keeps configured graphical sessions locked.
Inspect `journalctl`, correct any disk or directory-permission problem, then run
`sudo lpctl recover-state`. Invalid data is retained as a dated
`usage.json.invalid-*` file before fresh counters are created. Do not delete the
`.recovery` marker manually; it prevents an interrupted recovery from granting
fresh allowance on restart.

Inspect service health and logs with:

```bash
sudo systemctl status local-parental-control.service
sudo journalctl -u local-parental-control.service
sudo aa-status
```

## Update or uninstall

For a source installation, rebuild without sudo before updating:

```bash
git pull --ff-only
./scripts/build.sh
sudo ./scripts/update.sh
```

The update preserves configuration and usage state. Confirm service health
afterward with `sudo lpctl status`. The updated indicator starts on the next
login; no child-account command is needed. If the existing configuration is
invalid for the new version, the updater installs only the new `lpctl`, leaves
the installed and running daemon and its service files unchanged, and exits
with status 2. Use `sudo lpctl discover KEYWORD` to get configuration-ready paths,
correct the configuration, and run:

```bash
sudo lpctl validate
sudo ./scripts/update.sh
```

To remove a source installation while preserving configuration and usage
history:

```bash
sudo ./scripts/uninstall.sh
```

For a Debian installation, use `sudo apt remove local-parental-control`.
Purging the package also removes its saved usage state.

## Recovery

If the service does not start:

1. Run `sudo lpctl validate` and correct the reported configuration error.
2. Check `sudo journalctl -u local-parental-control.service -b`.
3. Check AppArmor denials with `sudo journalctl -k -g apparmor`.
4. Run `sudo systemctl restart local-parental-control.service` after correcting
   the problem.

The daemon exits with status 2 when its startup configuration is invalid.
Systemd does not automatically restart it for that status, because retrying
cannot repair the configuration. After correcting and validating the file,
restart the service manually.

Do not weaken file permissions, disable AppArmor, or grant additional systemd
capabilities to work around an error without first identifying which access is
required.
