# Business logic audit

Date: 2026-08-25  
Reviewed revision: `8f2728c` (`develop`)

## Scope and method

This audit reviewed configuration validation, user-to-UID resolution, process
attribution, session detection, schedule enforcement, device and application
accounting, mandatory breaks, state persistence, status reporting, reset and
reload behavior, discovery, and documented operational assumptions.

The findings below are based on code-path analysis and the existing automated
tests. `go vet ./...` and `go test -race ./...` passed during the audit. No
manual desktop, Snap-refresh, clock-change, or corrupt-state experiments were
performed. Findings that require those environments are explicitly described
as scenarios to verify.

Severity means:

- **High:** enforcement can silently target the wrong rule or stop working for
  a supported deployment.
- **Medium:** normal configuration or boundary behavior can produce materially
  surprising enforcement or accounting.
- **Low:** administrative behavior, validation, or diagnostics are inconsistent
  but do not normally defeat enforcement by themselves.

## Summary

| ID | Severity | Finding | Recommended disposition |
|---|---|---|---|
| BL-01 | High | Multiple configured usernames may resolve to the same UID, making rule selection nondeterministic. | Fix before supporting aliases, renamed accounts, or nonstandard NSS setups. |
| BL-02 | High | A Snap refresh changes executable paths and can silently disable application matching until reload/restart. | Fix or explicitly withdraw continuous Snap support. |
| BL-03 | High | Invalid or damaged usage state prevents daemon startup and leaves enforcement unavailable. | Add an explicit fail-safe recovery policy. |
| BL-04 | Medium | Poll-end sampling misattributes time around launches, exits, schedule boundaries, breaks, and midnight. | Bound accounting to known eligible intervals. |
| BL-05 | Medium | A full-day schedule cannot be represented although 1440-minute limits are valid. | Add an explicit all-day form or revise limits/documentation. |
| BL-06 | Medium | Schedule and device locking depend on finding at least one readable process for the UID. | Drive access enforcement from configured users and logind sessions. |
| BL-07 | Medium | Resetting one application cancels pending forced termination globally. | Cancel only terminations affected by the reset. |
| BL-08 | Low | Discovery labels executables as supported without applying production ownership and writability checks. | Share validation logic or change the output contract. |
| BL-09 | Low | Application IDs containing tabs or newlines pass validation despite an error contract that forbids whitespace. | Validate all Unicode/ASCII whitespace. |
| BL-10 | Low | Any local-date change resets counters, including a clock correction back to an already used date. | Define and persist a clock-rollback policy. |

## Detailed findings

### BL-01 — Duplicate numeric UID selects rules nondeterministically

Severity: **High**

Status: **Resolved on 2026-08-27.** Configuration validation, daemon startup,
and reload now reject duplicate numeric UIDs. Reload retains the active
configuration when the replacement is rejected.

Configuration is keyed by username and validates that every name exists, but it
does not require each name to resolve to a distinct numeric UID. During service
initialization and reload, `resolveUsers` stores one username per UID in a Go
map. A later assignment silently overwrites an earlier one. Iteration order over
the configuration map is not stable, so either user's rules can win after a
restart or reload.

Impact:

- device schedule, allowance, breaks, and application rules may come from the
  wrong configured username;
- one configured entry can disappear from process attribution;
- the read-only status socket can return the winning username's status to the
  shared UID while the other entry is unreachable;
- behavior may change after restart without a configuration change.

Evidence: `Config.Validate` checks names independently, while
`Service.resolveUsers` assigns `resolved[uid] = name` without collision
detection.

Recommended change:

1. Resolve every configured username during validation or service construction.
2. Reject a configuration when two names resolve to the same UID, naming both
   entries and the UID.
3. Add startup and reload regression tests, including transactional rejection
   during reload.

### BL-02 — Snap refresh creates an enforcement gap

Severity: **High**

Status: **Resolved on 2026-08-27.** Discovery now emits stable
`/snap/PACKAGE/current/...` identities. Secure loading migrates previously
discovered numeric-revision paths to that form and validates the current ELF.
Runtime matching uses the package and relative executable path while accepting
both old and new numeric revisions across a refresh.

Discovery walks `/snap/PACKAGE/current`, resolves it to a numbered revision,
and prints executable paths from that revision. Secure configuration loading
also resolves symlinks and stores the canonical revision path in the daemon's
in-memory rules. A Snap refresh installs a new revision and moves `current`.
New processes then expose a different `/proc/PID/exe` path, but the running
daemon continues matching the old revision until an administrator reloads or
restarts it.

Impact: an application presented as supported can become unlimited silently as
part of routine automatic package maintenance. The status command continues to
show the configured rule and gives no indication that none of its executable
paths belong to the current revision.

Recommended change: make a product decision before implementation. Viable
directions include:

- monitor Snap revision changes and securely re-resolve configured stable Snap
  identities;
- store a typed package/application identity and derive its approved revision
  paths at runtime;
- install a privileged refresh hook that validates and reloads rules;
- if none is acceptable, document that Snap rules require rediscovery after
  every refresh and stop describing them as continuously supported.

Regression test: simulate `current` moving from revision A to B while the
service remains running and verify that a B process is still controlled or that
an explicit actionable failure is surfaced.

### BL-03 — Corrupt state disables all enforcement

Severity: **High**

Status: **Resolved on 2026-08-27.** Invalid state now starts the daemon in a
fail-closed recovery mode that locks every configured graphical session,
exposes the condition through status and the indicator, and preserves the
original file. A root-only recovery command quarantines invalid data and uses a
durable marker so interruption cannot turn a missing file into fresh allowance.
Runtime persistence failures also block access and retry without discarding the
in-memory counters.

Missing state correctly creates a new state, but malformed JSON, unexpected
fields, unsafe metadata, excessive values, or an oversized file make
`loadState` fail. Service construction then fails and the daemon exits. Systemd
restarts exit status 1, but the persistent condition remains, producing a
restart loop while no schedule, device limit, break, or application limit is
enforced.

The strict validation is appropriate; silently treating damaged state as zero
would grant a fresh allowance. The missing piece is a defined safe recovery
mode.

Recommended change:

1. Decide the safety policy with the administrator experience in mind. A sound
   default is to preserve/quarantine the invalid file, start in a clearly
   reported blocked state, and require an explicit root recovery command.
2. Provide a command that can inspect the error and intentionally reset or
   replace state without manual JSON editing.
3. Ensure the indicator and `lpctl status` expose recovery mode.
4. Test corrupt JSON, invalid permissions, disk errors, and restart behavior.

### BL-04 — Poll-end sampling misattributes boundary time

Severity: **Medium**

Status: **Resolved on 2026-08-27.** Accounting now intersects every sampled
interval with the current local day, the configured minute-based schedule, and
persisted break deadlines before adding integer seconds. Session and process
activity must be observed at both interval endpoints, so launches, exits,
lock-state changes, scanner failures, reloads, and recovery cannot cause time
from an unknown interval to be charged. Continuous-use limits start breaks at
the exact second the threshold is reached rather than at poll completion.

Each tick computes one `delta` since the previous tick and attributes all of it
using conditions observed at the end of the interval. The delta is capped at
twice the configured polling interval, but it is not intersected with the
actual allowed-time boundary, break deadline, midnight, process lifetime, or
session lock transition.

Examples:

- a tick just after `allowed_from` can charge seconds that occurred before
  access became allowed;
- the first tick after a break deadline can charge part of the break;
- the first tick after midnight assigns the entire delta to the new day;
- a process first observed at the end of an interval receives the entire delta,
  while a process that exits before the scan can receive none of it.

At the default two-second poll this is small. At the allowed 60-second poll it
can materially distort short limits, and a delayed tick can attribute up to 120
seconds.

Recommended change:

- split elapsed time at known wall-clock boundaries (midnight, schedule start
  and end, and break deadline);
- track previous session eligibility and previously observed application
  activity, with an explicit sampling policy for process start/exit;
- consider reducing the maximum poll interval if precise minute-scale limits
  remain a product requirement;
- add table-driven tests on both sides of every boundary and document the
  maximum remaining error.

### BL-05 — No representation for all-day access

Severity: **Medium**

The configuration accepts device and application limits up to 1440 minutes,
but requires `allowed_from < allowed_until` using minute-of-day values. The
widest possible window is `00:00`–`23:59`, whose exclusive end blocks the final
minute of every day. Equal endpoints are rejected and `24:00` is not a valid
time, so an administrator cannot configure schedule-independent daily limits.

Recommended change: support an explicit all-day mode. Prefer an unambiguous
field or omitted schedule over assigning surprising meaning to equal endpoints.
Keep overnight-window support as a separate design decision.

Resolution: **Implemented.** A user may select explicit `all_day: true` without
clock-window fields. The daemon treats every second of the local day as
schedule-eligible, while daily limits and breaks continue to apply. Validation
rejects a configuration that combines all-day mode with either clock boundary.

Regression tests cover the final minute, local midnight, DST dates, and status
rendering.

### BL-06 — Access enforcement is coupled to process discovery

Severity: **Medium**

The daemon builds `activeUsers` only from processes returned by the `/proc`
scanner and applies schedule, device-limit, and break locking only to that map.
A total scanner error aborts the tick before any locking. Per-process read
failures are skipped by the scanner; if no readable process remains for a
configured UID, the daemon does not query or lock that user's graphical
sessions during the iteration.

A normal graphical session usually has several readable processes, so this is
primarily a robustness gap. Nevertheless, the access rule is conceptually a
session rule and should not depend on application/process visibility. It also
increases the impact of `/proc` confinement regressions.

Recommended change:

1. Resolve and evaluate configured UIDs independently of the process scan.
2. Use logind sessions to decide whether access must be locked.
3. Keep the “any user process exists” condition only for device-time accounting
   if that remains the intended definition.
4. Continue schedule/break/device enforcement when application scanning fails,
   and report the degraded application-monitoring state.

### BL-07 — Narrow reset cancels unrelated pending kills

Severity: **Medium**

Both a full-user reset and an application-only reset replace the entire
`pendingKill` map. Therefore `lpctl reset alice browser` can cancel a pending
`SIGKILL` for another application or another user. On the next successful scan,
an exhausted unrelated rule normally receives a new `SIGTERM` and a fresh grace
period, so enforcement is delayed rather than permanently removed.

This is root-only behavior, but it violates the command's stated scope and can
matter during administrative recovery or testing.

Recommended change: retain pending entries unless their process belongs to the
reset user and, for an application reset, matches that application rule. Add a
multi-user, multi-application regression test.

### BL-08 — Discovery and production validation disagree

Severity: **Low**

`lpctl discover` labels a regular executable ELF as `supported`, but its helper
does not check root ownership or group/other writability. `lpctl validate`
correctly applies those additional production checks. Consequently discovery
can claim that a path is configuration-ready and validation can immediately
reject it, especially for executables found in a customized `PATH`.

Recommended change: share one executable-inspection routine between discovery
and secure validation, or print a distinct `candidate` result with the reason
production validation may reject it. Test user-owned and writable ELF files.

### BL-09 — Application ID whitespace rule is incomplete

Severity: **Low**

Validation rejects only a literal space and slash characters, while its error
says that all whitespace is forbidden. Tabs, newlines, and other whitespace can
therefore become state keys and appear in diagnostics or CLI output.

Recommended change: reject any `unicode.IsSpace` rune plus `/` and `\\`, then
add table-driven validation tests. Consider a conservative identifier grammar
such as ASCII letters, digits, `_`, `.`, and `-`.

### BL-10 — Clock correction can grant repeated fresh days

Severity: **Low**

Every mismatch between the persisted state date and the current configured
local date creates a new empty state. If the clock moves forward to another
date and is then corrected backward, both transitions reset usage. The README
already documents clock changes as a limitation and requires the child account
to lack clock-setting privileges, which reduces severity. NTP or administrator
correction can still trigger it unintentionally.

Recommended change: persist enough date history to distinguish a legitimate
next day from rollback, define how long rollback protection lasts, and require
an explicit administrative reset when the date is ambiguous. Add forward,
backward, timezone-change, and DST tests.

## Suggested implementation order

1. **BL-01** is small, deterministic, and prevents rules from targeting the
   wrong logical user.
2. Decide the product policy for **BL-02** and **BL-03** before further claims
   about Snap support or fail-safe enforcement.
3. Decouple session enforcement in **BL-06**, then address interval accounting
   in **BL-04**; these touch the central tick state machine and should be designed
   and tested together.
4. Implement the explicit schedule behavior in **BL-05**.
5. Fix scoped reset and validation/diagnostic inconsistencies in **BL-07**,
   **BL-08**, and **BL-09**.
6. Treat **BL-10** as a policy decision unless clock instability is observed in
   supported deployments.

## Reviewed behavior not reported as defects

The following behaviors are deliberate or already clearly documented:

- overnight allowed windows are unsupported;
- device/application time pauses while graphical sessions are locked or
  inactive;
- time while the daemon or computer is stopped is not reconstructed;
- application rules match resolved executable paths rather than process names;
- unlisted desktop processes are not terminated;
- application enforcement uses `SIGTERM`, then checks full process identity
  before delayed `SIGKILL`;
- failed reload retains the previous active configuration;
- administrative reset does not override an out-of-hours schedule;
- screen locking depends on the desktop honoring systemd-logind requests.
