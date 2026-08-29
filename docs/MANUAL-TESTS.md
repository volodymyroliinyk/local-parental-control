# Manual behavior test checklist

Use this checklist before a release or after changes to session detection,
screen locking, usage accounting, configuration, installation, or the panel
indicator. These tests describe observable behavior; they do not replace the
automated test suite.

## How to use this checklist

- Run the tests on a disposable Ubuntu installation or virtual machine with
  `systemd`, AppArmor, and a supported graphical desktop.
- Use a non-administrator account as the controlled user and a separate
  administrator account for configuration and `lpctl` commands.
- Back up `/etc/local-parental-control/config.json` and
  `/var/lib/local-parental-control/usage.json` before testing. Some cases reset
  counters, change the clock, stop the service, or intentionally use invalid
  configuration.
- Keep test limits short, for example 2–5 minutes. Never edit the state file
  while the service is running.
- For time-boundary tests, use the timezone configured in `timezone`. The
  allowed interval is `[allowed_from, allowed_until)`: its start is inclusive
  and its end is exclusive.
- Record `Pass`, `Fail`, `Blocked`, or `N/A` in the Result column. Put evidence,
  deviations, and issue links in Notes.

Recommended evidence commands:

```bash
sudo lpctl status
sudo systemctl status local-parental-control.service
sudo journalctl -u local-parental-control.service -b
loginctl list-sessions
loginctl show-session SESSION --property=Active --property=LockedHint --property=Type
```

## Test environment

| Field | Value |
|---|---|
| Tester and date | |
| Commit or version | |
| Distribution and version | |
| Desktop and display protocol (Wayland/X11) | |
| Installation type (source/Debian package) | |
| Controlled username and UID | |
| Administrator username | |
| Configured timezone | |
| Poll interval / termination grace | |
| Tested native applications and executable paths | |
| Tested Snap applications and executable paths | |

## Installation and service lifecycle

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| INS-01 | Fresh source installation | Build as the administrator, run the source installer as root, configure a real controlled user, validate, and start the service. | Installation succeeds; daemon, CLI, indicator autostart entry, unit, documentation, and AppArmor profile are installed; the service becomes active after valid configuration. | | |
| INS-02 | Build attempted as root | Run the documented source build through `sudo`. | Build refuses to produce installer inputs and explains that it must run as the administrator account. | | |
| INS-03 | Installer input permissions | Make a built binary group- or other-writable and run the installer. | Installation is rejected without replacing the installed service. | | |
| INS-04 | Debian package installation with valid configuration | Install the package, replace the example configuration with valid data, validate, enable, and start the service. | Package installs and the service starts with the configured rules. | | |
| INS-05 | Debian package installation with invalid example user | Install where the example user does not exist. | Package remains installed, but the service stays disabled or inactive until configuration is corrected and validated. | | |
| INS-06 | Boot startup | Enable the service, reboot, and inspect service state before logging in as the child. | Service starts at boot and creates its administrative and status sockets. | | |
| INS-07 | Normal restart | Note current counters, restart the service, wait one poll, and inspect status. | Service returns to active state and preserves same-day device, continuous-use, break, and application counters. | | |
| INS-08 | Graceful stop | Stop the service after accumulating usage, then inspect the state file and restart. | Latest state is saved; restart restores it. Time while stopped is not added. | | |
| INS-09 | Invalid startup configuration | Stop the service, introduce an invalid field, and start it. | Daemon exits with status 2 and systemd does not enter an automatic restart loop. | | |
| INS-10 | Startup recovery | Correct the invalid configuration, run `lpctl validate`, and restart the service. | Validation succeeds and the service starts normally. | | |
| INS-11 | AppArmor active | Check `aa-status`, exercise status, reload, process scanning, application termination, and screen locking. | Profile is enforcing and normal operations succeed without relevant AppArmor denials. | | |
| INS-12 | Uninstall preserving data | Run the source uninstall or remove (not purge) the package. | Binaries and service integration are removed; configuration and usage history remain. | | |
| INS-13 | Package purge | Purge the Debian package in a disposable environment. | Package-owned files and saved usage state are removed according to package behavior. | | |

## Configuration validation and reload

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| CFG-01 | Valid complete configuration | Validate a configuration containing device, break, schedule, and multiple application rules. | `lpctl validate` succeeds and reports the correct user and application counts. | | |
| CFG-02 | Optional defaults | Omit timezone, poll interval, termination grace, continuous-use, and break values, then validate and exercise the service. | Configuration is accepted and documented defaults are applied. | | |
| CFG-03 | Unknown field | Add a misspelled or unknown field at each object level. | Validation fails and identifies the unknown field. | | |
| CFG-04 | Malformed or trailing JSON | Test malformed JSON, two JSON values, and non-whitespace trailing data. | Every variant is rejected. | | |
| CFG-05 | Oversized configuration | Validate a configuration larger than 1 MiB. | Validation fails because the size limit is exceeded. | | |
| CFG-06 | Missing user | Configure a username that does not exist locally. | Validation fails and names the missing user. | | |
| CFG-07 | Empty users map | Validate with no configured users. | Validation fails. | | |
| CFG-08 | Numeric ranges | Test 0, negative, valid boundary values, and values above 1440 for all minute limits; test 0/default, 1, 60, and 61 for poll and grace seconds. | Only documented defaults and inclusive valid ranges are accepted. | | |
| CFG-09 | Allowed-time format | Test missing values, `H:MM`, invalid hours/minutes, and valid `HH:MM` values. | Only exact 24-hour `HH:MM` values are accepted. | | |
| CFG-10 | Invalid or overnight interval | Test equal start/end and a start later than the end. | Validation rejects both; overnight windows are unsupported. | | |
| CFG-11 | Empty application list | Configure a user with no application rules. | Configuration is accepted and device-wide controls still operate. | | |
| CFG-12 | Application identity fields | Test blank/duplicate IDs, whitespace or slash in an ID, and a blank name. | Each invalid rule is rejected with a useful error. | | |
| CFG-13 | Executable list and path | Test an empty list, relative path, nonexistent path, and duplicate path within one user. | Each invalid configuration is rejected. | | |
| CFG-14 | Executable security | Test a script, directory, non-executable file, non-root-owned file, and group- or other-writable ELF file. | Production validation rejects every unsafe or unsupported entry. | | |
| CFG-15 | Symlink resolution | Configure a safe symlink to a supported ELF and validate/reload. | Validation resolves it to the real path and process matching works. | | |
| CFG-16 | Application ID characters | Validate IDs containing spaces, tabs, newlines, Unicode whitespace, `/`, and `\\`; also validate ordinary IDs using letters, numbers, dots, underscores, and hyphens. | Every whitespace or slash form is rejected; ordinary IDs are accepted. | | |
| CFG-16 | Duplicate resolved executable | Put a real path and a symlink to it in different rules for one user. | Validation rejects the duplicate resolved identity. | | |
| CFG-17 | Snap launcher rejection | Configure `/snap/bin/APP` or `/usr/bin/snap`. | Validation rejects the shared launcher and explains that a real ELF is required. | | |
| CFG-18 | Configuration ownership and mode | Test non-root ownership, non-regular file, wrong mode, and a group/other-writable parent directory. | Production validation rejects each case. | | |
| CFG-19 | Valid live reload | Change limits, names, schedule, users, or rules; validate and reload without restarting. | `lpctl reload` succeeds and subsequent status/enforcement uses the new configuration. | | |
| CFG-20 | Invalid live reload | Introduce invalid configuration and run reload. | Reload fails and the daemon continues using the entire previous valid configuration. | | |
| CFG-21 | Reload raises an application limit | Reach an application limit, then raise it and reload during the termination grace period. | Any scheduled `SIGKILL` is cancelled and the still-running process may continue under the new limit. | | |
| CFG-22 | Reload removes an application rule | Trigger termination, remove the rule, and reload during the grace period. | Scheduled forced termination is cancelled and the executable is no longer controlled by that rule. | | |
| CFG-23 | Reload changes poll interval | Reload with a different valid polling interval and observe status updates. | New iterations use the changed interval without restarting the service. | | |

## Allowed hours and login regression cases

Use a short window around the current local time. Repeat the core cases on both
Wayland and X11 when both are supported.

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| SCH-01 | Login before allowed start | Set `allowed_from` a few minutes in the future, reach the login screen, and attempt to sign in as the controlled user. | The graphical session is locked again within one poll; running programs are not terminated. | | |
| SCH-02 | Repeated login attempts before start | Before the allowed start, unlock or sign in repeatedly. | Every attempt is locked again at subsequent polls; access is not granted early. | | |
| SCH-03 | Transition at allowed start | Remain at the locked screen across `allowed_from`, then sign in or unlock at exactly/just after that minute. | The user can enter the desktop and the daemon does not immediately relock the allowed, active session. | | |
| SCH-04 | Regression: first login during allowed time | Start or reboot the machine before the window, leave it at the login screen, and first sign in after `allowed_from` with unused allowance. | Login succeeds and remains unlocked. This specifically guards against blocking a permitted first login. | | |
| SCH-05 | Login well inside allowed time | Sign out and sign in during the window with unused allowance and no active break. | Desktop remains accessible and usage begins counting. | | |
| SCH-06 | Allowed start is inclusive | Attempt access during the exact `allowed_from` minute. | Access is allowed, subject to daily and break limits. | | |
| SCH-07 | Minute before allowed end | Use the desktop during the final allowed minute. | Access remains allowed until the configured end minute begins. | | |
| SCH-08 | Allowed end is exclusive | Stay active across `allowed_until`. | Screen locks within one poll after the end minute begins. | | |
| SCH-09 | Login after allowed end | Attempt sign-in or unlock after `allowed_until`. | Screen locks again within one poll. | | |
| SCH-10 | Running work at schedule end | Keep applications and unsaved content open across `allowed_until`. | Screen locks, but applications remain running and are not terminated solely because of the schedule. | | |
| SCH-11 | Next-day allowed window | After an out-of-hours lock, attempt access during the next valid window. | Access is allowed if no current-day limit or break blocks it. | | |
| SCH-12 | Configured timezone differs from system timezone | Choose a different valid timezone and test both boundaries. | Enforcement follows the configured timezone, not the desktop display timezone. | | |
| SCH-13 | Multiple graphical sessions for one user | Open two graphical sessions for the controlled UID and reach an out-of-hours condition. | All graphical sessions returned by logind receive lock requests. | | |
| SCH-14 | TTY/non-graphical session | Log in on a TTY during allowed time and observe counters; also trigger an out-of-hours lock. | TTY alone is not treated as an unlocked graphical session; graphical lock behavior is unaffected. | | |
| SCH-15 | Unconfigured user login | Sign in as a local user absent from configuration. | Parental-control schedule and counters do not affect that user. | | |
| SCH-16 | Explicit all-day access | Configure `all_day: true` without `allowed_from` or `allowed_until`; remain active across 23:59 and midnight. | Schedule enforcement never blocks access, the final minute remains eligible, and the new day starts with reset counters. Usage limits and breaks still apply. | | |
| SCH-17 | Conflicting all-day configuration | Configure `all_day: true` together with either clock-window field and validate. | Validation fails and identifies the mutually exclusive schedule settings. | | |
| SCH-18 | Enforcement without visible processes | Reach an out-of-hours, device-limit, or mandatory-break condition, then simulate an empty or failed `/proc` scan while a graphical session exists. | The session is still locked for the configured UID. Failed scanning is reported as degraded in `lpctl status`; counters and application termination pause until scanning recovers. | | |

## Device usage accounting and daily limit

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| DEV-01 | Basic device accounting | During allowed hours, keep the controlled graphical session active and unlocked. | Device time increases approximately once per elapsed second, within polling resolution. | | |
| DEV-02 | Multiple processes | Start many processes for the controlled user. | Device time still increases only once, not once per process. | | |
| DEV-03 | Multiple controlled applications | Run several configured applications simultaneously. | Device time increases once while each active application gets its own usage increment. | | |
| DEV-04 | Locked screen pauses usage | Note counters, lock the screen for several polls, then check status. | Device, continuous-use, and application counters do not increase while all graphical sessions are locked. | | |
| DEV-05 | Unlock resumes usage | Unlock during allowed hours after DEV-04. | Counters resume from their saved values without adding locked time. | | |
| DEV-06 | Inactive graphical session | Switch to another user's graphical session while leaving the controlled session unlocked but inactive. | Controlled-user usage pauses while no active, unlocked graphical session exists. | | |
| DEV-07 | No user processes | End the controlled user's processes/session and wait. | Device usage does not increase without any process attributed to that UID. | | |
| DEV-08 | Daemon downtime | Stop the daemon for several minutes, restart it, and inspect counters. | Downtime is not added to usage. | | |
| DEV-09 | Suspend or long scheduling gap | Suspend and resume, or otherwise delay polling well beyond two intervals. | The daemon does not charge the full gap; accounting is capped to avoid adding unavailable time. | | |
| DEV-10 | Clock moves backward | In a disposable environment, move the wall clock backward while active. | No negative usage is subtracted; daemon remains operational. | | |
| DEV-11 | Reach daily device limit | Use the session until `daily_device_minutes` is consumed. | Screen locks within the polling tolerance and status reports device `BLOCKED`. | | |
| DEV-12 | Repeated unlock after daily limit | Attempt to unlock several times after DEV-11. | Daemon repeatedly requests a lock; counters do not resume. | | |
| DEV-13 | Applications survive device limit | Keep configured and unconfigured applications open when the device limit is reached. | Screen locks; applications are not terminated merely because the device limit was reached. | | |
| DEV-14 | Application limit already exhausted at login | Exhaust an app limit, lock/sign out, then enter an otherwise allowed session and launch the app. | Device access remains available, but the limited app is terminated. | | |
| DEV-15 | Full user reset | Run `sudo lpctl reset USER` after device, continuous, break, and app usage exists. | All counters and the active break for that user reset; access is restored if currently within allowed hours. | | |
| DEV-16 | Reset does not override schedule | Reset all counters outside allowed hours. | Counters clear, but the session remains blocked by the schedule. | | |
| DEV-17 | Local midnight reset | Stay running across midnight in the configured timezone. | On the next poll, date and all daily counters reset; old-day usage is not carried forward. | | |
| DEV-18 | Restart after midnight | Stop before midnight and start after midnight with the prior day's state file. | Service starts with fresh counters for the new configured local date. | | |
| DEV-19 | Independent users | Configure two controlled users and accrue or exhaust usage for only one. | Each user's counters, schedule, breaks, and enforcement remain independent. | | |
| DEV-20 | Session query failure | In a disposable setup, make logind session-state lookup fail and wait several polls. | Usage pauses, service remains running, and a warning is recorded; time is not guessed. | | |
| DEV-21 | Allowed-start interval accounting | Use a long poll that begins before `allowed_from` and ends after it. | Only whole seconds after the configured start are charged. | | |
| DEV-22 | Allowed-end interval accounting | Use a long poll that begins before `allowed_until` and ends after it. | Only whole seconds before the configured end are charged, then access locks. | | |
| DEV-23 | Midnight interval accounting | Keep an eligible session active across local midnight. | Prior-day seconds are not assigned to the new day; only the post-midnight portion is charged. | | |
| DEV-21 | Desktop refuses lock request | Test on a desktop/session known not to honor logind locking, if available. | Service records a lock error and remains running; documentation limitation is observable. | | |

## Continuous use and mandatory breaks

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| BRK-01 | Continuous-use accounting | Use an active unlocked graphical session within allowed hours. | Continuous-use time increases with device time. | | |
| BRK-02 | Locked time pauses continuous use | Lock the screen before the continuous limit, wait, and unlock. | Locked time is not counted; the saved continuous counter resumes. | | |
| BRK-03 | Reach continuous-use limit | Stay active until the configured continuous limit. | Screen locks, a break deadline is created, and status shows an active break. | | |
| BRK-04 | Repeated unlock during break | Attempt to unlock repeatedly before the break deadline. | Every attempt is locked again within one poll. | | |
| BRK-05 | Break duration is wall-clock time | Remain locked throughout a break. | Break expires after configured wall-clock duration; it does not require active usage. | | |
| BRK-06 | Break completion | Unlock at or just after the break deadline during allowed hours. | Access remains allowed and continuous-use accounting restarts from zero. | | |
| BRK-07 | Daemon restart during break | Restart the daemon during an active break. | Break deadline persists and access remains blocked until it expires. | | |
| BRK-08 | Machine restart during break | Reboot during an active break and attempt login before its deadline. | Persisted break remains effective until its deadline. | | |
| BRK-09 | Full reset during break | Run `sudo lpctl reset USER`, then unlock within allowed hours. | Break and continuous counter clear and access is restored. | | |
| BRK-10 | Application-only reset during break | Run `sudo lpctl reset USER APP_ID`. | Only that app counter resets; the active break and device counters remain unchanged. | | |
| BRK-11 | Daily limit reached during same tick | Set device allowance equal to or shorter than continuous allowance and consume it. | Daily device blocking takes effect; it is not incorrectly reported as a resumable break. | | |
| BRK-12 | Schedule ends during break | Start a break shortly before `allowed_until` and wait past both deadlines. | Access remains blocked by the schedule even after the break expires. | | |
| BRK-13 | Outside-hours processing clears break | Enter an out-of-hours state with a stored break, then inspect status/state through normal commands. | The daemon enforces the schedule and clears continuous/break progress on a monitoring iteration. | | |
| BRK-14 | Midnight during break | Keep a break active across configured local midnight. | Next poll resets daily state, including the prior day's break. Access then depends on the new day's schedule. | | |

## Application accounting and enforcement

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| APP-01 | Matching native executable | Launch a configured ELF during allowed, active, unlocked use. | Only its application rule accrues time. | | |
| APP-02 | Unconfigured executable | Launch an executable absent from all rules. | It is not given an application counter and is not terminated by application limits. | | |
| APP-03 | Multiple processes of one app | Open several processes/windows whose resolved executable belongs to one rule. | The application accrues time once, not once per process. | | |
| APP-04 | Multiple executables in one rule | Run two different configured executable paths from the same application rule simultaneously. | The shared application rule accrues time once. | | |
| APP-05 | Two apps simultaneously | Run executables from two different rules. | Both rules accrue independently at wall-clock rate. | | |
| APP-06 | App usage while screen locked | Leave a configured app running and lock the screen. | Its application usage pauses while locked and resumes after unlock. | | |
| APP-07 | App usage outside allowed hours | Leave or launch a configured app while access is schedule-blocked. | Screen is locked and app usage does not accrue merely while the session is blocked. | | |
| APP-08 | App usage during mandatory break | Leave a configured app running during a break. | Application usage pauses for the break. | | |
| APP-09 | Reach application limit | Keep the app active until its daily limit. | Every matching process receives `SIGTERM` at or immediately after the limit within polling tolerance. | | |
| APP-10 | Graceful application exit | Use an app that exits on `SIGTERM`. | It closes before the grace deadline and is not later signalled as a reused PID. | | |
| APP-11 | Forced application exit | Use a disposable process that ignores `SIGTERM`. | The same process receives `SIGKILL` after the configured grace period. | | |
| APP-12 | Launch after application limit | Relaunch a limited app while its counter is exhausted. | It is terminated at the next poll without consuming additional app allowance. | | |
| APP-13 | Other apps after one app limit | Exhaust one rule and use another configured or unconfigured app. | Only executables belonging to the exhausted rule are terminated. | | |
| APP-14 | Application-only reset | Exhaust one app, run `sudo lpctl reset USER APP_ID`, and relaunch it. | That app is allowed again with a zero counter; device, break, and other app counters remain unchanged. | | |
| APP-15 | Reset during termination grace | Trigger `SIGTERM`, reset that app before grace expires, and keep the process alive. | Pending forced termination is cancelled. | | |
| APP-16 | Unknown application reset | Reset a nonexistent app ID. | Command fails and no counters change. | | |
| APP-17 | Scoped reset during multiple termination grace periods | Trigger pending forced terminations for two applications and two users, then reset one application. Repeat with a full-user reset. | Application reset cancels only matching pending terminations. Full-user reset cancels only that user's pending terminations. Unrelated processes keep their original deadlines. | | |
| APP-17 | Same executable path for different users | Configure the same root-owned executable for two users and use it under only one UID. | Only the process owner's rule and counter are affected. | | |
| APP-18 | Executable copied to another path | Run a copy of a configured executable from an unconfigured path in a disposable setup. | It does not match the original rule, demonstrating the documented path-identity limitation. | | |
| APP-19 | First process observation | Launch an application just before a poll and compare two subsequent status samples. | No time before first observation is charged; seconds accrue after the application is observed in consecutive samples. | | |
| APP-20 | Process exit between polls | Exit an application between samples. | The uncertain final interval is not charged, preventing time after exit from being fabricated. | | |
| APP-19 | Deleted/replaced executable while running | Replace or remove a configured executable after starting it, where safe. | Daemon remains stable; matching follows the kernel-resolved process executable identity and configuration constraints. | | |
| APP-20 | PID reuse safety | During a delayed termination test, let the original process exit and create process churn before the grace deadline. | A different process that reuses the numeric PID is not killed. | | |

## CLI, status, discovery, and authorization

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| CLI-01 | Help and version | Run `lpctl help`, `lpctl --version`, and each binary's version option where supported. | Help is accurate and installed binaries report the expected version. | | |
| CLI-02 | Argument errors | Run no command, unknown commands, and commands with missing or extra arguments. | CLI prints usage/error and exits nonzero without changing service state. | | |
| CLI-03 | Root status | Run `sudo lpctl status` with multiple users and apps. | Output is sorted predictably, shows the state date, counters/limits, schedule, break state, and `ALLOWED`/`BLOCKED` accurately. | | |
| CLI-04 | Status outside allowed hours | Check status with unused allowance outside the schedule. | Device reports `BLOCKED` because of schedule; counters remain visible. | | |
| CLI-05 | Status during break | Check status during a mandatory break. | Device reports `BLOCKED` and displays the break deadline in configured local time. | | |
| CLI-06 | Administrative command as child | Run `lpctl status`, `reload`, and `reset` without sudo from the controlled account. | Administrative access is denied or the connection is closed; no state changes. | | |
| CLI-07 | Unknown user reset | Run reset for an unconfigured username. | Command fails and no counters change. | | |
| CLI-08 | Daemon unavailable | Stop the daemon and run socket-based commands. | CLI reports that it cannot contact the daemon and exits nonzero. | | |
| CLI-09 | Native discovery | Run `sudo lpctl discover KEYWORD` for an installed native application. | Output contains configuration-ready, real ELF paths or clearly reports that none were found. | | |
| CLI-10 | Snap discovery | Discover an installed Snap application. | Output uses stable real-ELF identities under `/snap/PACKAGE/current`, never `/snap/bin/*`; supported results validate successfully. | | |
| CLI-11 | Snap refresh continuity | Configure a discovered Snap executable, start it, refresh the package to a new revision, and start it again without reloading the daemon. | Both the already-running old revision and the newly launched revision accrue against and enforce the same application limit. | | |
| CLI-11 | Discovery miss | Search for a nonexistent keyword. | Command reports no configuration-ready path and exits nonzero. | | |
| CLI-12 | Unsafe discovery candidate | Put matching user-owned and group- or other-writable ELF files in a temporary `PATH` directory and run discovery. | Neither file is printed as `supported`; every printed result passes production validation. | | |
| CLI-12 | Read-only status privacy | Sign in as each configured user and observe the indicator/status endpoint through the shipped client. | Each user receives only their own status, never another user's counters or rules. | | |
| CLI-13 | Unconfigured status client | Start the indicator as an unconfigured local user. | It exits quietly and exposes no configured-user data. | | |

## Panel indicator

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| UI-01 | Automatic startup | Sign out and back in as a configured graphical user after installation. | Indicator starts without per-user setup and appears in a StatusNotifier-compatible panel. | | |
| UI-02 | Quiet behavior | Observe startup, normal use, limit approach, break, and blocked states. | No notifications, sounds, or automatic windows appear. | | |
| UI-03 | Device label | Compare the panel label with `lpctl status` around whole-minute boundaries. | Label shows remaining device minutes rounded up and never below `0m`. | | |
| UI-04 | Tooltip normal state | Hover while no break is active. | Tooltip shows device time remaining, time until break, each configured app's remaining time, and the state date. | | |
| UI-05 | Tooltip break state | Hover during a break. | Tooltip says `Break in progress` and still shows device and application remaining time. | | |
| UI-06 | Indicator outside allowed hours | Observe the indicator before/after the schedule window. | Indicator remains visible and continues to show remaining counters without attention animation. | | |
| UI-07 | Live updates | Use device and application time for several update cycles. | Label and tooltip refresh approximately every five seconds. | | |
| UI-08 | Temporary daemon outage | Stop the daemon briefly while the indicator is running, then restart it. | Indicator remains quiet, retains its last display during errors, and resumes updates when status returns. | | |
| UI-09 | Daemon absent at login | Sign in while the daemon is temporarily unavailable, then start it. | Indicator waits quietly and appears/registers after status becomes available. | | |
| UI-10 | User removed on reload | Remove the signed-in user from configuration and reload. | That user's indicator exits quietly on its next status refresh. | | |
| UI-11 | Panel restart | Restart GNOME Shell/panel where safe or disable/re-enable AppIndicator support. | Running indicator re-registers with the panel and becomes visible again. | | |
| UI-12 | No administration controls | Click, right-click, scroll, and inspect any menu behavior. | Indicator does not expose reset, reload, configuration, or other administrative actions. | | |

## State persistence and failure recovery

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| STA-01 | State ownership and mode | Accrue usage and inspect the state directory/file. | Directory is root-owned mode `0700`; state file is root-owned regular mode `0600`. | | |
| STA-02 | Atomic state updates | Monitor the state directory while counters update and interrupt the service at varied moments. | Readers see a complete old or new JSON document, not a partial state file; temporary files do not accumulate. | | |
| STA-03 | Valid same-day restoration | Restart with a valid state containing device, continuous, break, and app values. | All same-day values are restored. | | |
| STA-04 | Missing state file | Stop the service, move the state file aside, and start it. | Service creates fresh current-day state on the next save. | | |
| STA-05 | Invalid state JSON | In a disposable setup while stopped, install malformed or trailing-data state and start. | Daemon starts in recovery mode, preserves the file, reports the error, and repeatedly locks configured sessions. | | |
| STA-06 | Invalid state ownership or mode | While stopped, change owner/type/mode and start the service. | Daemon enters recovery mode without reading or overwriting the unsafe file. Recovery remains blocked until directory/filesystem safety is restored. | | |
| STA-07 | Invalid state values | While stopped, test negative, over-one-day, invalid date, zero break deadline, and unknown fields. | Each invalid state produces fail-closed recovery mode. | | |
| STA-08 | Oversized state | Start with a state file larger than 4 MiB. | Daemon enters recovery mode without reading or replacing the oversized file. | | |
| STA-09 | Missing maps compatibility | Start with otherwise valid same-day state where optional maps are `null` or omitted as accepted by the decoder. | Service initializes missing maps and continues safely. | | |
| STA-10 | Failed state save | In a disposable setup, make the state directory temporarily unwritable/incompatible and use the service. | Access becomes blocked, in-memory counters are retained, and persistence automatically recovers after storage is writable again. | | |
| STA-11 | Explicit state recovery | Start in recovery mode, inspect status, then run `sudo lpctl recover-state`. | Invalid state is preserved as `usage.json.invalid-*`, a valid fresh state is written, the marker is removed, and access again follows normal rules. | | |
| STA-12 | Interrupted recovery | Leave a valid `.recovery` marker with `usage.json` absent and restart the daemon. | Recovery mode remains active; the missing file is not treated as a clean installation. | | |
| STA-13 | Recovery indicator | Observe the controlled user's panel indicator during state recovery. | Label says `BLOCKED` and tooltip says administrator recovery is required without exposing the detailed state error. | | |

## Update compatibility and preservation

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| UPD-01 | Valid source update | Accrue usage, save a custom config, build a newer version, and run the update script. | Binaries/service assets update; configuration and usage remain; service returns healthy with existing counters. | | |
| UPD-02 | Incompatible configuration during source update | Use configuration rejected by the new version and run update. | Only new `lpctl` is installed; daemon binary, service, confinement, config, state, and running process remain unchanged; script exits with documented recovery status. | | |
| UPD-03 | Recover incompatible source update | Use new discovery/validation to correct config and rerun update. | Update completes and preserved state/configuration are used. | | |
| UPD-04 | Debian package upgrade | Upgrade over an installed package with custom configuration and active usage. | Administrator configuration and usage state are preserved; service uses the new version after successful upgrade. | | |
| UPD-05 | Indicator after update | Complete an update while the child is signed in, then sign out/in. | Updated indicator starts on the next graphical login and reports current preserved counters. | | |

## Security and documented limitations

| ID | Test | Procedure | Expected result | Result | Notes |
|---|---|---|---|---|---|
| SEC-01 | Child cannot edit configuration | As the controlled user, try to read/write the production config and its directory. | Configuration contents cannot be read or changed. | | |
| SEC-02 | Child cannot edit state | As the controlled user, try to read/write usage state and its directory. | State cannot be read, reset, or changed. | | |
| SEC-03 | Administrative socket permissions | Inspect the control socket and attempt access as non-root. | Socket is root-private mode `0600`; peer UID checks prevent administration. | | |
| SEC-04 | Status socket is read-only | As a controlled user, send administrative-shaped input to the status socket. | No command is accepted and no state changes; response contains only that peer UID's status. | | |
| SEC-05 | Root remains administrator | As root, validate, status, reload, reset, start, and stop the service. | Authorized administrative operations work as documented. | | |
| SEC-06 | Systemd confinement sanity | Inspect unit security properties and exercise all supported behavior. | Service retains only required access/capabilities and supported operations still work. | | |
| SEC-07 | No general allowlist behavior | Start ordinary desktop/session processes not listed as applications. | They are not terminated merely because they are unlisted. | | |
| SEC-08 | Unsupported bypass is understood | Demonstrate a copied executable, interpreter, container, Wine, VM, or remote execution only in a safe test environment. | Behavior matches documented limitation; test does not treat these mechanisms as protected by the service. | | |
| SEC-09 | Controlled user has no administration path | Confirm the controlled account lacks sudo and cannot install/replace system-controlled configured executables. | Deployment assumptions hold; otherwise record the environment as unsupported/insecure. | | |

## Completion summary

| Metric | Value |
|---|---|
| Total applicable tests | |
| Passed | |
| Failed | |
| Blocked | |
| Not applicable | |
| Issues filed | |
| Release/manual-test decision | |

Sign-off:

| Role | Name | Date | Decision/notes |
|---|---|---|---|
| Tester | | | |
| Reviewer | | | |
