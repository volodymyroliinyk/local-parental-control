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
sudo apt install apparmor
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

The installer verifies that both binaries are regular files owned by the
administrator who invoked sudo and that group or other users cannot modify
them. It then installs the binaries, systemd unit, AppArmor profile, and
documentation.

Edit the installed configuration:

```bash
sudoedit /etc/local-parental-control/config.json
sudo lpctl validate
sudo systemctl enable --now local-parental-control.service
```

The configuration file must be owned by `root:root` with mode `0600`. Its
directory must be root-owned and must not be writable by group or other users.
Configured executables must resolve to native ELF, root-owned executable files
that are not writable by group or other users.

For every controlled user, set `daily_device_minutes` and an allowed local-time
window with `allowed_from` and `allowed_until` in `HH:MM` format. The end time
is exclusive. `continuous_use_minutes` and `break_minutes` configure mandatory
breaks and default to 60 and 10 minutes when omitted. Application rules may be empty when only device-wide controls
are needed. See the configuration section in the README for validation rules.

## Install the Debian package

Build the package without sudo:

```bash
LPC_VERSION=0.1.0 ./scripts/build-deb.sh
sudo apt install ./dist/local-parental-control_0.1.0_amd64.deb
```

If the example user does not exist or its configuration is otherwise invalid,
the package remains installed but the service stays disabled. Edit the
configuration, validate it, and start the service as shown above.

## Find executable paths

Start the application, find its process ID, and inspect the kernel-resolved
executable path:

```bash
pgrep -a vlc
readlink -f /proc/PID/exe
```

Replace `PID` with the numeric process ID. Add every distinct executable used
by the application to the same rule. Do not add wrapper scripts, `.desktop`
files, or a shared runtime such as Java unless every program using that runtime
should share one limit.

## Administration

Administrative commands require root:

```bash
sudo lpctl validate
sudo lpctl discover firefox
sudo lpctl status
sudo lpctl reload
sudo lpctl reset child
sudo lpctl reset child vlc
```

After editing the configuration, run `validate` and then `reload`. A failed
reload leaves the previous configuration active.

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
afterward with `sudo lpctl status`. If the existing configuration is invalid
for the new version, the updater installs the new files but leaves the running
daemon untouched and exits with status 2. Use `sudo lpctl discover KEYWORD` to
find supported executable paths. Add only entries marked `supported`; entries
marked `unsupported launcher`, including Snap launchers, are informational and
must not be added. If no supported result exists, install a native ELF package
or choose another native application. Then correct the configuration and run:

```bash
sudo lpctl validate
sudo systemctl restart local-parental-control.service
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

Do not weaken file permissions, disable AppArmor, or grant additional systemd
capabilities to work around an error without first identifying which access is
required.
