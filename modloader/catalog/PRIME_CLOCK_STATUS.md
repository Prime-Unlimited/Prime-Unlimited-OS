# Prime Clock catalog status

The current published JC11S entry is `prime-clock-v0.1.2-jc11s.json`. It pins
release asset `770107450`, size `2608` bytes and SHA-256
`2ce60eb2e999cfeee17cf1a1eab5283636e73b81ac75efe462ff64e29ee00136`.
The adjacent signature is made by trust-root key `prime-unlimited-2026-1`.

Version 0.1.2 replaces the moving-item coordinate calculation with Qt Quick's
native constrained drag target. This keeps the initial finger-to-overlay offset
stable and removes the feedback drift seen over longer drag distances.

The JC11S catalog record uses the immutable GitHub release asset ID rather than
the replaceable release filename URL. The runtime permits only GitHub API asset
redirects to GitHub's release-asset host and still verifies exact size and
SHA-256 before staging.

The signed `prime-clock-v0.1.2-jp24.json` is a hardware-test catalog, not a
production verification claim. Real JP24 hardware confirmed the exact runtime
identity (`JP24`, Engine OS `5.3.0`, `aarch64`, `gnu`), persistent runtime API,
Mod Loader tile and screen. CI reproducibly builds and validates the separate
JP24 package under ID `prime-clock-jp24`.

The JP24 overlay injection and Prime Clock rendering still require their final
live hardware test. Until the overlay is visibly exercised through install,
enable, drag/persistence, disable and uninstall, report JP24 as
Modloader-compatible and hardware-test-ready, not as a hardware-verified Prime
Clock overlay. The separate package identity prevents JC11S bytes from being
selected on JP24 and vice versa.
