# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Separate installation, development, and security documentation for administrators and contributors.

- Release script for verification, artifact builds, Git tags, branch updates, and GitHub Releases.

### Security

- Systemd capability, syscall, network, and namespace restrictions and an enforced AppArmor profile reduce daemon access to the host.

- Process signaling now uses pidfds; production configuration, executable paths, state files, and administrative socket peers receive stricter validation.

### Changed

- The default termination grace period is 15 seconds to give applications more time to exit cleanly.

- Installation and update scripts require binaries built without root privileges and verify their ownership and permissions before installation.

## [0.1.0] - 2026-08-20

### Added

- Linux service for tracking daily application use by configured users.
- Application limit enforcement with `SIGTERM` and delayed `SIGKILL`.
- Persistent daily usage state with atomic file updates.
- Root-only `lpctl` commands for validation, status, reload, and reset.
- Systemd service, installation scripts, and Debian package generation.
- Configuration validation, automated tests, and project documentation.
