# Prime Clock catalog status

The current published JC11S entry is `prime-clock-v0.1.1-jc11s.json`. It pins
release asset `762426166`, size `2647` bytes and SHA-256
`a01b6787824802d08c6ea27c3e17cb397a540467b3c0d7ad531591dfb925de96`.
The adjacent signature is made by trust-root key `prime-unlimited-2026-1`.

The JC11S catalog record uses the immutable GitHub release asset ID rather than
the replaceable release filename URL. The runtime permits only GitHub API asset
redirects to GitHub's release-asset host and still verifies exact size and
SHA-256 before staging.

JP24 is intentionally absent from the trusted catalog. The repository confirms
the RK3588/AArch64 toolchain target `aarch64-linux-gnu.2.17`, but not the device's
Modloader runtime ABI identity, firmware string, QtQuick version or working
overlay adapter. Publishing guessed compatibility would violate fail-closed
device selection. Its package is built and structurally validated by CI and can
be cataloged under the separate ID `prime-clock-jp24` when those values are read
from real hardware.
