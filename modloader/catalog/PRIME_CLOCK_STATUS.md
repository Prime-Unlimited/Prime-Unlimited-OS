# Prime Clock catalog status

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
