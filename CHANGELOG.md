# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-08-20

### Added

- Linux service for tracking daily application use by configured users.
- Application limit enforcement with `SIGTERM` and delayed `SIGKILL`.
- Persistent daily usage state with atomic file updates.
- Root-only `lpctl` commands for validation, status, reload, and reset.
- Systemd service, installation scripts, and Debian package generation.
- Configuration validation, automated tests, and project documentation.

