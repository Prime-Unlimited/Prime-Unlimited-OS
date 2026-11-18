# Verified JC11S manager-UI integration status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Public QML template: `/data/prime-unlimited/ui/ModLoader.template.qml`
  - contains an authorization marker, never a token
  - mode and owner: `0644 root:root`
- Generated QML client: `/data/prime-unlimited/ui/ModLoader.qml`
  - mode and owner: `0600 root:root`
  - no digest is recorded because it would be derived from the device secret
- Runtime binary: 6,815,928-byte static ARM EABI5 executable
  - SHA-256: `c88638493a4f4cbacc78f4e933d78eba65c91b987c44e59cf8d72bec71a248c2`
- Pre-milestone backup:
  `/data/prime-unlimited/backups/ui-manager-v1/ModLoader.qml.pre-manager-ui`

## Implemented UI contract

The install button now executes cache, stage, inspection and install in strict
order and stops on the first error. Installed mods expose atomic enable,
disable and confirmed uninstall actions. Enabled mods expose manual
dependency-ordered launch. Quarantined mods show their failure count and an
explicit recovery action. A global safety indicator controls safe mode, and an
activity tab renders persisted lifecycle transactions.

At API startup the runtime reads the existing `0600 root:root` token, replaces
exactly one marker in the validated public template and atomically publishes a
`0600 root:root` QML runtime copy. Missing or duplicate markers fail closed.
The token is never requested from an HTTP endpoint or placed in UI text, logs,
Git or this document.

## Device verification

The deployed file matched the repository digest and expected ownership. The
public installed, transaction and safety endpoints returned valid empty state.
Using the same authenticated request shape as the UI, safe mode was enabled,
observed as persistent, disabled and observed false again. Engine retained PID
698 throughout and was not restarted.

The owner then performed the physical touchscreen acceptance: the tile opened,
showed the compatible `Catalog Smoke Test v0.0.0`, and rendered the install
button. Installed and activity tabs displayed their correct empty states. Safe
mode was toggled on and off through the UI, both before and after automatic
private-QML generation, proving authenticated mutation delivery. The smoke
entry was deliberately not installed.

Static UI contract tests verify every endpoint, action and the ordered
installation pipeline. The final safety state is off; API and Engine are active.
