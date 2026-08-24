# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- A quiet read-only panel indicator now starts automatically for configured desktop users and shows their remaining device, break, and application time without notifications.

- The lpctl discover command returns configuration-ready ELF paths from native packages and installed Snap packages.

- Configurable mandatory screen breaks with a 60-minute use and 10-minute break default.

- Per-user daily device allowances and allowed login hours, enforced through systemd-logind screen locking.

- Separate installation, development, and security documentation for administrators and contributors.

- Release script for verification, artifact builds, Git tags, branch updates, and GitHub Releases.

### Security

- Systemd capability, syscall, network, and namespace restrictions and an enforced AppArmor profile reduce daemon access to the host.

- Process signaling now uses pidfds; production configuration, executable paths, state files, and administrative socket peers receive stricter validation.

### Changed

- Device and application usage now pauses while the controlled user's graphical session is locked or inactive, resumes after unlock, and excludes time while the computer or daemon is stopped.

- Application discovery now prints only executable paths that can be copied directly into configuration, resolving Snap launchers to real package ELF files.

- The default termination grace period is 15 seconds to give applications more time to exit cleanly.

- Installation and update scripts require binaries built without root privileges and verify their ownership and permissions before installation.

### Fixed

- AppArmor now permits the daemon to read discovered Snap ELF files during configuration validation and allows the procfs reads required for process and socket operation.

- Source updates are now transactional when configuration is incompatible: only the new lpctl is installed, while the daemon, service, confinement, configuration, state, and running process remain unchanged until validation succeeds.

- Empty application rules now report how to recover by adding a supported native executable or removing the rule, instead of implying that configuration was cached.

- The systemd service no longer enters an automatic restart loop when startup configuration is invalid; it waits for an administrator to correct and validate the file.

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
