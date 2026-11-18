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

This port is build- and integration-ready but must be marked hardware-verified
only after the service, health endpoint, UI and reboot behavior have been
checked on a real JP24 device.
