# JP24 Project Status

**Reviewed:** 2026-11-18  
**Repository:** [Prime-Unlimited/Prime-Unlimited-OS](https://github.com/Prime-Unlimited/Prime-Unlimited-OS)  
**Scope:** GitHub state visible at review time. Hardware claims below are attributed to the associated PR or owner report.

## Current direction

The JP24 (PRIME 4 G2) bootstrap is no longer only a local image experiment. PR #5 records a successful real-device root SSH bootstrap. Follow-up PRs #12 and #13, both merged, add querying the real `/data` partition size and live write progress/ETA in the desktop installer. PR #14 builds on an already reachable JP24 and ports the Control Center QML resource hook to aarch64.

The owner separately reported on 2026-11-16 that Claude work established durable SSH using a reverse shell. That exact implementation has not been identified in the repository or reviewed here; preserve it as owner-reported until its source or sanitized notes are committed.

## Recent pull request changes

- [PR #12](https://github.com/Prime-Unlimited/Prime-Unlimited-OS/pull/12) — merged. Replaces fixed-size assumptions with a device-reported `/data` size query and an image rebuild/reflash flow. Its description reports a real JP24 check of about 20.8 GB and `df -h /data` at 10.1 GB after rebuild/reflash. The flow uses a 50% sizing margin.
- [PR #13](https://github.com/Prime-Unlimited/Prime-Unlimited-OS/pull/13) — merged. Adds live write percentage and ETA, scales the timeout with image size, and retains the buffered admin-prompt fallback.
- [PR #14](https://github.com/Prime-Unlimited/Prime-Unlimited-OS/pull/14) — open, non-draft, mergeable at review time. Ports the QML resource hook to JP24/aarch64; adds a diagnostic-only default, fixes 64-bit page alignment, tile clipping, and icon aspect ratio. Its description reports live JP24 verification of the diagnostic hook and real Control Center tile patch.
  - Follow-up still needed: no persistent JP24 activation/install chain or aarch64 runtime API backend yet.
  - The PR reports that restarting `engine.service` kills the ALSA-hook-forked `sshd`; a full reboot was needed to regain SSH. Account for this during future activation work.
  - No PR reviews, conversation comments, or workflow runs were returned by the GitHub connector during this review. This does not establish that no external checks exist.

## Open issues

- [Issue #3](https://github.com/Prime-Unlimited/Prime-Unlimited-OS/issues/3) — macOS JP24 USB probe misses the device. The report requests an IORegistry fallback while keeping discovery transport-only and unable to authorize writes.
- [Issue #11](https://github.com/Prime-Unlimited/Prime-Unlimited-OS/issues/11) — JC11S Wi-Fi AutoConnect needs persistent `/var/lib/connman`. Issue body reports the directory fix confirmed on JC11S.

## Active branch map

GitHub listed these branches at review time:

`main`, `dev/project-foundation`, `ci/windows-macos-installer-build`, `feat/data-partition-query-rebuild`, `feat/desktop-installer-foundation`, `feat/integrate-jc11s-jp24`, `feat/jc11s-enginex-verified-bootstrap`, `feat/jc11s-native-windows-fastboot`, `feat/jc11s-offline-ssh-poc`, `feat/jc11s-prime-native-installer`, `feat/jp24-bootstrap-module`, `feat/jp24-ssh-bootstrap-integration`, `feat/jp24-ui-hook-port`, `feat/jp24-write-progress-eta`, `feat/prime4-g2-ssh-bootstrap`, `feat/prime-unlimited-mod-loader`, `feat/ssh-bootstrap-both-devices`, `fix/isolated-usb-backends-jc11s-jp24`, and `fix/jc11s-hardware-verified-ssh-bootstrap`.

## Suggested next work

1. Review PR #14's patch and decide whether its UI-hook scope is ready to merge into `feat/prime-unlimited-mod-loader`.
2. Track an explicit JP24 activation/persistence design that survives Engine restarts and documents how SSH is re-established.
3. Import the owner's reverse-shell implementation on a dedicated branch, after removing secrets, and document its exact startup, reconnect, and repeatability evidence.
4. Keep destructive `/data` writes behind the existing identity/evidence checks and explicit confirmation flow.
