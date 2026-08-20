# Agent instructions

These instructions apply to the entire repository.

## Communication and autonomy

- Communicate with the user in Ukrainian or English. Do not use Russian.
- Prefer Ukrainian unless the user writes in English or asks for English.
- Work autonomously when the requested change is clear. Inspect the repository, make reasonable low-risk assumptions, implement the change, and verify it without asking for confirmation at every step.
- Ask a question only when a missing decision would materially change behavior, security, compatibility, or data, or when an irreversible/external action requires approval.
- Do not repeat long plans, file listings, or command output. Progress updates and final reports should state only decisions, material changes, verification results, and blockers.
- Do not claim that an action succeeded unless it was verified.

## Project scope

- This is a local application-time control service for Linux. Website and URL blocking are not part of the current scope.
- The root daemon scans `/proc`, attributes processes by UID, matches resolved executable paths, records daily use, and terminates applications at configured limits.
- `lpctl` administers the daemon through a root-only Unix socket.
- Configuration: `/etc/local-parental-control/config.json`.
- State: `/var/lib/local-parental-control/usage.json`.
- Admin socket: `/run/local-parental-control/control.sock`.
- `task.md` is the current product request. `chatgpt_conversation.md` is historical background; do not reread it unless a requirement is unclear.

## Implementation rules

- Use Go 1.26 or newer and the standard library unless a dependency has a clear, documented benefit.
- Keep packages small and responsibilities separated under `cmd/` and `internal/`.
- Preserve these behavior and security properties:
  - reject unknown and invalid configuration fields;
  - identify processes using numeric UID and `/proc/PID/exe`, not process names;
  - count one application once even when it has several processes;
  - persist state atomically with restrictive permissions;
  - keep administrative operations inaccessible to the controlled account;
  - account for PID reuse before sending delayed signals;
  - reject an invalid reload while retaining the active configuration;
  - preserve configuration and state during updates.
- Do not implement a general process allowlist without an explicit design change; killing unlisted session processes can break the desktop.
- Avoid unrelated refactors and preserve user changes in a dirty worktree.
- Use Conventional Commits for commit messages (`feat:`, `fix:`, `docs:`,
  `test:`, `build:`, `chore:`). Add user-visible changes to the `Unreleased`
  section of `CHANGELOG.md` with `scripts/update-changelog.sh`.

## Documentation

- Write documentation in plain, professional language.
- Describe observable behavior, commands, limitations, and recovery steps.
- Avoid marketing language, unsupported security claims, and vague adjectives.
- Keep `README.md`, the example configuration, CLI help, service paths, installation scripts, and package metadata consistent.
- The project is licensed under MIT. Keep `LICENSE` and Debian copyright metadata consistent.

## Build and verification

Use the repository scripts instead of inventing new build commands:

```bash
./scripts/build.sh
LPC_VERSION=0.1.0 ./scripts/build-deb.sh
```

Before reporting a code change as complete, run the relevant checks:

```bash
gofmt -w cmd internal
go vet ./...
go test -race ./...
git diff --check
```

For shell or packaging changes, also run:

```bash
bash -n scripts/*.sh packaging/*.sh packaging/debian/postinst packaging/debian/prerm packaging/debian/postrm
dpkg-deb --info dist/local-parental-control_0.1.0_amd64.deb
dpkg-deb --contents dist/local-parental-control_0.1.0_amd64.deb
```

- Build Go binaries with `CGO_ENABLED=0` so release artifacts remain statically linked.
- `bin/` and `dist/` are generated outputs. Do not commit binaries or `.deb` files; publish them as release artifacts.
- Do not install the service, alter `/etc`, `/usr`, or systemd state unless the user explicitly asks for installation or system changes.

## Change completion

- Verify changes in proportion to their risk.
- Report which user-visible behavior changed, which checks passed, and any limitation or manual step that remains.
- Use clickable repository file links when useful, but do not enumerate every touched file.
