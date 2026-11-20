# Splash Manager

Splash Manager runs a small web UI on port `8081` for replacing the active
Prime Unlimited splash image. The upload server is implemented in auditable Go
source under `src/server/` and built for both supported device architectures.

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

Create a target package:

```sh
python3 scripts/package.py --target jc11s --output dist/
python3 scripts/package.py --target jp24 --output dist/
```
