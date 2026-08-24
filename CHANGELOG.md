# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The lpctl discover command finds matching native executables and identifies installed Snap and Flatpak launchers as unsupported.

- Configurable mandatory screen breaks with a 60-minute use and 10-minute break default.

- Per-user daily device allowances and allowed login hours, enforced through systemd-logind screen locking.

- Separate installation, development, and security documentation for administrators and contributors.

- Release script for verification, artifact builds, Git tags, branch updates, and GitHub Releases.

### Security

- Systemd capability, syscall, network, and namespace restrictions and an enforced AppArmor profile reduce daemon access to the host.

- Process signaling now uses pidfds; production configuration, executable paths, state files, and administrative socket peers receive stricter validation.

### Changed

- The default termination grace period is 15 seconds to give applications more time to exit cleanly.

- Installation and update scripts require binaries built without root privileges and verify their ownership and permissions before installation.

### Fixed

- Application discovery now fails clearly when it finds only unsupported launchers and explains that Snap and Flatpak launcher paths must not be added to configuration.

- Source updates now install the new discovery command without restarting the daemon when an existing configuration is incompatible, allowing administrators to discover executable paths and repair the configuration first.

- Updates now validate existing configuration with the newly built lpctl, avoiding false unknown-field failures from an older installed CLI.

- Production validation now rejects scripts and the shared Snap launcher, which cannot be matched reliably through /proc/PID/exe.

- Builds now produce installer-safe binary permissions, AppArmor permits the process inspection required for executable attribution, and installation waits for the control socket to become ready.

## [0.1.0] - 2026-08-20

### Added

- Linux service for tracking daily application use by configured users.
- Application limit enforcement with `SIGTERM` and delayed `SIGKILL`.
- Persistent daily usage state with atomic file updates.
- Root-only `lpctl` commands for validation, status, reload, and reset.
- Systemd service, installation scripts, and Debian package generation.
- Configuration validation, automated tests, and project documentation.
