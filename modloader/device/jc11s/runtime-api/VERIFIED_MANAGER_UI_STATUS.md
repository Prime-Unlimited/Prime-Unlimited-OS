# Verified JC11S manager-UI integration status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifact

- QML client: `/data/prime-unlimited/ui/ModLoader.qml`
  - size: 13,938 bytes
  - mode and owner: `0644 root:root`
  - SHA-256: `770e32454827ae7332d6cc35dc1878ef37b352e58d262a09589cba0ac16e0557`
- Pre-milestone backup:
  `/data/prime-unlimited/backups/ui-manager-v1/ModLoader.qml.pre-manager-ui`

## Implemented UI contract

The install button now executes cache, stage, inspection and install in strict
order and stops on the first error. Installed mods expose atomic enable,
disable and confirmed uninstall actions. Enabled mods expose manual
dependency-ordered launch. Quarantined mods show their failure count and an
explicit recovery action. A global safety indicator controls safe mode, and an
activity tab renders persisted lifecycle transactions.

The root-owned Engine process reads the existing `0600 root:root` device token
directly from its local file. It is never requested from an HTTP endpoint or
placed in UI text, logs, Git or this document.

## Device verification

The deployed file matched the repository digest and expected ownership. The
public installed, transaction and safety endpoints returned valid empty state.
Using the same authenticated request shape as the UI, safe mode was enabled,
observed as persistent, disabled and observed false again. Engine retained PID
698 throughout and was not restarted.

The owner was away during deployment, so opening the tile and visually checking
the rendered controls on the physical touchscreen remains an explicit manual
acceptance check. Static UI contract tests verify every endpoint, action and
the ordered installation pipeline; the existing catalog and touch tile remain
unchanged.
