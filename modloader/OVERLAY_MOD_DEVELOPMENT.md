# Overlay Mod Development Guide

This document records the verified contract discovered while building and
testing Prime Clock on a Denon DJ Prime 4+ (`JC11S`, Engine OS 5.3.0). It is the
reference for future overlay mods. New overlay mods must use this contract;
they must not add mod-specific code to the Modloader, runtime API or Engine
hook.

## Architecture and ownership

There are three layers:

1. The device adapter injects one generic `Loader` into the existing Engine QML
   tree. This is firmware/model-specific infrastructure and is installed once.
2. `OverlayHost.qml` is the generic trusted host. It asks the loopback runtime
   for all enabled overlays, dynamically loads their catalog-declared QML files,
   mediates private settings, and suppresses overlays on secondary displays.
3. A mod package contains its own UI and behavior. It receives no bearer token,
   does not patch Engine, and does not know how the host was injected.

Adding or updating a normal overlay mod changes only the package and signed
catalog. A Modloader change is justified only when the generic contract itself
needs a new, reusable capability.

## Verified JC11S implementation

- Engine resource: compressed `EmbeddedMain.qml` in the registered QRC tree.
- Stable anchor on Engine OS 5.3.0: the nonessential `PageCacheInfo` block.
- The hook intercepts QRC registration, decompresses only that resource,
  replaces the exact anchor with the generic host `Loader`, removes comments
  only to stay inside the original compressed QRC slot, recompresses it, and
  updates the QRC length/timestamp before registration.
- Original Engine binaries and firmware partitions are never modified. Startup
  wrappers and bind mounts originate in `/data/prime-unlimited`.
- Engine QML disk cache is disabled/cleared when the adapter changes.
- The host source must use an explicit `file:` URL. An absolute string such as
  `/data/O` is resolved by this Engine build as `qrc:/data/O` and will fail.
- Engine creates more than one main-window context. The host must load package
  overlays only when `planckQuickView.isMainScreen` is true, otherwise duplicate
  overlays compete for input and settings.
- The UI interposer must never be registered as an ALSA configuration hook.
  Doing so prevents Engine from opening `/dev/snd/seq` and disables the physical
  surface. The only persistent hook path is the Engine preload wrapper.

## Package contract

Catalog schema v2 is authoritative. An overlay record needs:

```json
{
  "id": "example-jc11s",
  "version": "1.0.0",
  "models": ["JC11S"],
  "firmware": ["5.3.0"],
  "architecture": "armv7l",
  "abi": "gnueabihf",
  "entrypoint": "example/start.sh",
  "overlay": "example/ui/Example.qml",
  "approval": {"maintainer": "Prime Unlimited", "devices": ["JC11S"]}
}
```

The ZIP paths must exactly match `entrypoint` and `overlay`. All staged package
files are normalized to private mode `0600`. The current manual launcher accepts
only a small `#!/bin/sh` entrypoint; an overlay-only mod should make it inert and
exit successfully. Enable/Disable controls visibility through registry state;
Launch must not create another overlay instance.

Use separate catalog identities/records when device packages or compatibility
differ. Never publish guessed firmware, architecture, ABI or Qt values. Build
success, package validation, Modloader compatibility and real hardware testing
are four separate statuses.

## QML overlay contract

The root component must be an `Item` filling the Loader:

```qml
Item {
    anchors.fill: parent
    signal positionSaveRequested(real x, real y)
    function restorePosition(savedX, savedY) { /* clamp then restore */ }
}
```

Only visible controls should contain `MouseArea`s; the full-screen root must not
swallow deck gestures. The trusted host recognizes the optional signal/function
pair and persists integer coordinates via the authenticated runtime API under
`/data/prime-unlimited/settings/<mod-id>.json`. Package QML never receives the
authorization token.

For dragging a moving item, calculate touch deltas in a stationary coordinate
space:

```qml
var point = gesture.mapToItem(overlayRoot, mouse.x, mouse.y)
panel.x = clampX(startPanelX + point.x - startPointerX)
panel.y = clampY(startPanelY + point.y - startPointerY)
```

Do not subtract successive `mouse.x`/`mouse.y` values local to the panel being
moved. Its coordinate origin moves with it, causing drift, oscillation and jumps.
Use `pressAndHold` to enter drag mode, suppress the subsequent click, clamp both
axes after size/display changes, and persist only on release.

## Time and background behavior

Wall time is appropriate for a clock display. It is not appropriate for elapsed
time. Prime Clock polls `/api/v1/monotonic`; the Go runtime derives it from a
monotonic process clock. Stopwatch state uses an accumulated duration plus the
latest monotonic start value, so wall-clock corrections cannot change elapsed
time. Pause adds only the current interval, resume starts a new interval, reset
clears and stops, and switching UI mode does not stop the timer.

Keep timers stopped when their display context is inactive. Do not continuously
poll from secondary screens, disabled mods or Safe Mode.

## Lifecycle

The verified installation order is:

1. Load and verify detached catalog signature against the deployment trust root.
2. Reject model/firmware/architecture/ABI mismatches.
3. Download from the signed URL.
4. Verify exact Content-Length, package size and SHA-256.
5. Safely extract to non-executable staging; reject traversal, links, duplicates,
   unsafe modes, unexpected size/count and undeclared paths.
6. Inspect declared entrypoint and overlay.
7. Atomically install disabled and persist the registry transaction.
8. Enable explicitly; Safe Mode exposes no overlays.
9. Disable before uninstall. The UI performs this sequence automatically because
   the runtime correctly refuses to uninstall an enabled mod.

Disable and uninstall are dynamic and require no Engine restart. A host/adapter
upgrade may require one controlled restart outside a running set. Never restart
Engine automatically merely because a mod was enabled.

## Do

- Share common code and keep device facts in explicit platform profiles.
- Use QtQuick features actually present on the target firmware (`2.12` on the
  verified JC11S system).
- Keep package QML self-contained and use relative imports inside the package.
- Use immutable, asset-ID-based downloads; verify SHA-256 and size anyway.
- Sign the final catalog only after release bytes and URLs are final.
- Test cache, stage, inspect, disabled install, enable, launch, disable,
  re-enable, Safe Mode, uninstall, rollback and power-cycle behavior.
- After an adapter change, verify Engine stability, `/dev/snd/seq` descriptors,
  physical buttons/encoders, touchscreen behavior and absence of restart loops.
- Preserve backups under `/data/prime-unlimited/backups` before adapter changes.
- Document hardware evidence per exact model and firmware.

## Don’t

- Don’t add a Prime Clock ID, filename or behavior to generic host/runtime code.
- Don’t give untrusted package QML the Modloader bearer token.
- Don’t patch original Engine binaries or protected firmware partitions.
- Don’t install the UI preload library through ALSA hooks.
- Don’t assume one Engine window means one display context.
- Don’t use local coordinates from the item currently being dragged.
- Don’t use `Date.now()` for stopwatch elapsed time.
- Don’t write outside `/data` or let packages choose arbitrary settings paths.
- Don’t bypass signature, compatibility, staging or registry transactions for a
  production install.
- Don’t overwrite release assets after catalog signing. Publish a new version.
- Don’t call a cross-compiled or structurally valid package hardware-tested.

## Build and release checklist

- Run shared logic/UI tests and all relevant runtime tests.
- Build every matrix target twice and compare bytes for reproducibility.
- Inspect target profiles and any native ELF architecture/ABI.
- Validate QML imports/resources and the exact ZIP inventory/modes.
- Compute size and SHA-256 from final release artifacts.
- Publish immutable assets, generate per-device records, then sign the catalog
  outside CI unless an authorized secret-backed signing step exists.
- Confirm the trusted catalog signature through the same runtime verifier used on
  the device.
- Record separately: compiled, package-valid, Modloader-compatible, hardware-tested.

Prime Clock is the reference implementation in `mods/prime-clock/` and its CI
matrix is `.github/workflows/prime-clock.yml`.
