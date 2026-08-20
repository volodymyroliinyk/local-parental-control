# Development guide

## Requirements

- Go 1.26 or newer
- Linux on amd64 or arm64 for the production daemon
- Bash, systemd tools, AppArmor tools, and `dpkg-deb` for packaging checks

The project uses the Go standard library and builds with `CGO_ENABLED=0`.
Generated files under `bin/` and `dist/` are not committed.

## Repository layout

- `cmd/local-parental-control`: daemon entry point
- `cmd/lpctl`: administrative CLI
- `internal/config`: configuration parsing and production security validation
- `internal/process`: `/proc` scanning and pidfd-based signaling
- `internal/daemon`: accounting, enforcement, persistent state, and Unix socket
- `packaging`: systemd, AppArmor, and Debian package files
- `scripts`: build, installation, changelog, update, and release workflows

Keep process discovery and signaling in `internal/process`. Keep configuration
rules in `internal/config`; do not duplicate security checks in command entry
points.

## Build and test

```bash
make check
./scripts/build.sh
LPC_VERSION=0.1.0 ./scripts/build-deb.sh
```

Before committing, run:

```bash
gofmt -w cmd internal
go vet ./...
go test -race ./...
git diff --check
bash -n scripts/*.sh packaging/*.sh packaging/debian/postinst \
  packaging/debian/prerm packaging/debian/postrm
apparmor_parser -Q -T -W --cache-loc=/tmp/local-parental-control-apparmor \
  packaging/apparmor/local-parental-control
```

Inspect a built package with:

```bash
dpkg-deb --info dist/local-parental-control_0.1.0_amd64.deb
dpkg-deb --contents dist/local-parental-control_0.1.0_amd64.deb
```

Tests must not require root or modify `/etc`, `/usr`, systemd, or the active
AppArmor policy. Installation and enforcement behavior require separate manual
tests on a disposable Ubuntu system.

## Security invariants

Changes must preserve these properties:

- production configuration and executable identities are root-controlled;
- state and the administrative socket are private to root;
- administrative clients are authenticated using Unix peer credentials;
- signals use pidfds and never fall back to signaling a numeric PID;
- delayed `SIGKILL` requires the same PID, UID, start time, and executable;
- invalid reloads do not replace the active configuration;
- state writes remain atomic and durable;
- the service does not gain network access or broader Linux capabilities;
- updates preserve configuration and state.

Treat changes to `/proc` parsing, signal delivery, root scripts, systemd,
AppArmor, configuration validation, and state persistence as security-sensitive.
Add focused tests for success, malformed input, permission failures, and race
conditions.

The service is deliberately a configured-application limiter, not a process
allowlist. Do not terminate unrelated desktop-session processes.

See [SECURITY.md](SECURITY.md) for the threat model and known limitations.

## Changes and commits

Use Conventional Commits, for example:

```text
fix: prevent signaling a reused process ID
docs: clarify package installation
```

Add observable changes to the Unreleased changelog before committing:

```bash
./scripts/update-changelog.sh add security "Describe the security change"
```

Normal work is committed to `develop`. Do not commit `bin/`, `dist/`, local
task files, credentials, or test data containing personal information.

## Releases

Read `scripts/release.sh` before use and start with its dry-run mode. Do not run
a release until application enforcement, installation, upgrade, uninstall,
systemd confinement, and AppArmor behavior have been tested manually.

```bash
./scripts/release.sh 0.2.0 --dry-run
```

The non-dry release command creates commits and tags and pushes to GitHub. It
must be run only when publishing a release is explicitly intended.
