# Splash Manager

The source-built Splash Manager uploads PNG images to the active Prime Unlimited
splash at `/data/prime-unlimited/ui/splash/SplashScreen.png` through the trusted
runtime capability broker. It runs as a sandboxed systemd DynamicUser service.

## Device use after approval

Install the signed catalog package and activate it in Modloader. Tap **WEB
ACCESS** to display the HTTPS address, six-digit pairing code and certificate fingerprint.
Open the address on a computer/phone on the same network, compare the device
certificate fingerprint before accepting the self-signed certificate, then
enter the code. It expires after two minutes, is single-use and locks after
five incorrect attempts. The long session token is exchanged automatically.
No SSH tunnel is required for this managed mode.

Use a **1280 × 800 px (16:10) PNG** for the splash screen.
Uploads are decoded as PNG, limited to 10 MiB and 16 megapixels, and committed
atomically. Reboot the device to see the new splash. Disable stops the service;
uninstall removes its executable and credentials while preserving the splash.
Safe mode/quarantine also stop it. The service never receives the Modloader
API mutation token or direct write access to the data partition.

**Hardware approval remains false.** The runtime with service support must be
installed first, and the package needs a signed compatible service declaration.
JP24 identity is `JP24 / aarch64 / gnu`; firmware approval remains empty until
the mod is verified on hardware. Build success does not grant catalog approval.
See `modloader/SERVICE_MOD_DEVELOPMENT.md` for the systemd and security contract.

## Build and package

Go is required. Each package build cross-compiles the native server from source,
with VCS stamping disabled. Its platform profile includes a candidate service
binary path and digest; only the signed catalog can authorize that declaration.

```sh
python3 scripts/package.py --target jc11s --output dist/
python3 scripts/package.py --target jp24 --output dist/
```

`src/server/build.sh` refreshes both committed native build copies. The shared
release workflow builds packages twice and compares them for reproducibility.

## Manual development fallback

Direct root launch through `start.sh start` retains loopback HTTP on port 8081
and a private access token at
`/data/prime-unlimited/private/splash-manager.token`. This fallback is separate
from managed deployment and requires an SSH tunnel. The restricted shell
launcher intentionally rejects the manual entrypoint; service-declared mods
use the trusted native service adapter instead.
