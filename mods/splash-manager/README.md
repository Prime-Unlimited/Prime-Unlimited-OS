# Splash Manager

Splash Manager runs a small web UI on port `8081` for replacing the active
Prime Unlimited splash image. The upload server is implemented in auditable Go
source under `src/server/` and built for both supported device architectures.

## Modloader integration status

This is a manual device-test package, not a catalog-approved Modloader service.
The current launcher runs `/bin/sh -s` as UID/GID 65534, with a five-second
limit, in a separate working directory. It does not pass a package root or
lifecycle arguments. Installed files are private `0600`; the sandbox cannot
execute packaged native binaries or write the protected splash image.
Enable/Disable updates registry state only; it does not start or stop services.
Uninstall therefore cannot safely clean up a running standalone daemon.

The entrypoint explicitly rejects the restricted Modloader launch context.
A generic trusted service adapter must supervise start/stop, verify package
hashes before execution, grant only the required splash write capability,
and stop services on disable, uninstall, quarantine and safe mode. It must
also own token provisioning and expose authorized access without giving the
mod the runtime API bearer token. Hardware approval remains false.

JP24 runtime identity is `JP24 / aarch64 / gnu`; firmware approval remains
empty pending on-device verification of this mod.

## Storage and access

Uploaded PNGs are validated and atomically saved to:

```
/data/prime-unlimited/ui/splash/SplashScreen.png
```

The server creates a random 256-bit access token on first launch and stores it
with owner-only permissions at:

```
/data/prime-unlimited/private/splash-manager.token
```

The server listens on loopback only. Create an SSH tunnel to reach the page
from your Mac; JP24 commonly uses port `2222`:

```sh
ssh -p 2222 -N -L 8081:127.0.0.1:8081 root@<deck-ip>
```

Then open `http://127.0.0.1:8081`. Retrieve the token over authenticated SSH
and enter it in the page:

```sh
ssh -p 2222 root@<deck-ip> 'cat /data/prime-unlimited/private/splash-manager.token'
```

The `/upload` and `/current.png` endpoints require the token in an HTTP Bearer
authorization header. Because the browser connection is carried inside SSH,
the token is encrypted in transit. Uploads are limited to 10 MiB, decoded as
PNG, and limited to 16 megapixels. The server logs to
`/data/prime-unlimited/logs/splash-manager.log`.

## Build and package

Build both static server binaries from source:

```sh
cd src/server && ./build.sh
```

Create a target package (Go is required; each invocation cross-compiles from
source with VCS stamping disabled, rather than trusting committed binaries):

```sh
python3 scripts/package.py --target jc11s --output dist/
python3 scripts/package.py --target jp24 --output dist/
```
