# JP24 native runtime API

This directory ports the shared Mod Loader runtime service to the Prime 4 G2
(`JP24`) aarch64 Linux target. The service source remains shared with the
JC11S implementation; only the cross-build and device identity differ.

Build on a host with Go installed:

```sh
./build.sh
```

The result is
`build/prime-unlimited-runtime-api`, a static `linux/arm64` binary. The JP24
UI-hook installer requires that file and installs it together with `start.sh`.
At boot, `activate-jp24.sh` starts it as the transient, restart-on-failure
`prime-unlimited-modloader-api.service` bound only to `127.0.0.1:8098`.

Runtime compatibility identity:

- model: `JP24`
- firmware: `5.3.0`
- architecture: `aarch64`
- ABI: `gnu`

Catalog entries must explicitly approve this exact identity. Existing JC11S
packages therefore remain incompatible by design. The first start creates the
private mutation token and renders authorized `ModLoader.qml` and OverlayHost
runtime files from their templates.

## Hardware verification

Verified on 2026-11-18 on a real Prime 4 G2 (`JP24`) running Engine OS 5.3.0.
After a complete device reboot:

- `engine.service`, `prime4-mod-sshd.service`, and
  `prime-unlimited-modloader-api.service` were all active;
- the loopback health endpoint reported `authorizationStatus: ready`;
- both launcher bind mounts were present;
- a repeated activation completed with exit status 0 without interrupting
  the active SSH session;
- the Engine UI, hardware LEDs, Mod Loader tile, and Mod Loader screen all
  operated normally.

The missing catalog reported by the health endpoint is expected until a
signed JP24 catalog and trust key are provisioned.
