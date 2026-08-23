# Security model

The daemon runs as root because it must inspect processes belonging to other
users and signal them. The systemd unit and AppArmor profile restrict that root
process, but they do not make it equivalent to an unprivileged service.

## Assumptions

- The administrator and root account are trusted.
- The controlled account has no sudo access and cannot modify system files.
- Configuration and configured executable files remain root-controlled.
- The host kernel, systemd, and AppArmor are functioning normally.

## Protections

- Production configuration and executable metadata are validated before use.
- Usage state and the administrative socket are root-private.
- Administrative socket clients must present UID 0 through `SO_PEERCRED`.
- Process signals use pidfds to avoid numeric PID reuse races.
- Input sizes and concurrent administrative connections are limited.
- systemd removes unnecessary capabilities, address families, namespaces, and
  system calls.
- AppArmor limits filesystem access and outbound signals.
- Screen locking queries sessions using only a validated numeric UID and passes
  validated session identifiers to the fixed `/usr/bin/loginctl lock-session`
  command. Each operation has a five-second timeout.

## Limitations

Rules match the kernel-resolved executable path. A copy at another path, an
interpreter, another launcher, a container, Wine, a virtual machine, or remote
execution can bypass a rule. Snap and Flatpak applications may expose launcher
or sandbox-specific paths. Restricting those mechanisms is administrator policy
and is outside this service.

Polling allows an over-limit application or login session to run for up to one poll interval.
After the configured grace period, `SIGKILL` can cause unsaved application data
to be lost. Screen locking preserves running programs, but it depends on the
desktop environment honoring systemd-logind's lock request. Device presence is
inferred from owned processes, so lingering services and processes that remain
active behind a locked screen can continue consuming time. Changing the system
clock can affect the daily reset, allowed-hours checks, and break deadlines.

The service is not a substitute for account separation, filesystem
permissions, application sandboxing, backups, or operating-system updates.

## Reporting a security issue

Do not include usernames, configuration files, logs containing personal data,
or proof-of-concept details in a public issue. Contact the repository owner
privately when possible, describe the affected version and Ubuntu release, and
include the minimum information needed to reproduce the problem.
