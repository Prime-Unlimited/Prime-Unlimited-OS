# USB Copy 0.1.4-beta.1 installation

Experimental opt-in for Prime 4 G2 / JP24, Engine OS 5.3.0, existing Prime Unlimited with working modloader/updater (r16 baseline). No other model or firmware is approved. Finish active USB Copy jobs before updating. Keep the normal library backup. The source USB and SD card are not installation targets.

## 1. Install the companion beta runtime

The signed updater installs its allowlisted runtime/UI payload, backs up the preceding runtime, and restores it if its health check fails. Engine is not restarted by this update. The companion runtime identifies as 0.2.0-r17 and enables experimental live imports. The stable updater channel remains unchanged.

In an authenticated root SSH terminal on the device, run the temporary updater against the release's immutable signed manifest:

```sh
systemd-run --unit=prime-unlimited-usb-copy-beta-updater \
  /data/prime-unlimited/bin/prime-unlimited-self-updater \
  -listen 127.0.0.1:8100 -root /data/prime-unlimited \
  -model JP24 -architecture aarch64 -abi gnu \
  -manifest-url 'https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/783087439' \
  -signature-url 'https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/783087441' \
  -trust-key /data/prime-unlimited/trust/catalog-key.json \
  -auth-token /data/prime-unlimited/auth/mutation.token
```

After the service starts:

```sh
TOKEN=$(cat /data/prime-unlimited/auth/mutation.token)
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8100/api/v1/check
curl -fsS http://127.0.0.1:8100/api/v1/status
```

Continue only if the status offers version 0.2.0 revision 17 for JP24 without an error. Do not downgrade a later installed runtime.

```sh
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8100/api/v1/update
unset TOKEN
```

A successful response is `{"ok":true}`. The temporary updater can then be stopped:

```sh
systemctl stop prime-unlimited-usb-copy-beta-updater
```

## 2. Install the mod from the signed beta catalog

Open Prime Unlimited Mod Loader and synchronize the catalog. The beta runtime uses these release-pinned catalog assets:

- Catalog: https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/783082385
- Signature: https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/783082388

Choose **USB Copy 0.1.4-beta.1**, install it, then enable it. An existing development USB Copy installation may require the normal Update action. Use the normal Mod Loader inspection/approval flow. Do not replace the device trust key; the included public key is reference material only.

## 3. Use and rollback

Insert a USB stick, choose Copy and the internal destination. The generated playlist includes only newly copied tracks. Choosing No leaves normal USB use available. Large sources can require several minutes for content checking even if only two files are new.

To leave the beta, disable USB Copy first. Restart the temporary updater command above, then call its authenticated `POST /api/v1/rollback` instead of update. This restores its backed-up runtime and stable catalog configuration. Stop the temporary updater afterward. Rollback changes the runtime/UI; it does not remove already imported music or playlists. Keep the original updater backup until the beta is accepted.

The release does not install automatically on devices. A fresh standard-r16-to-beta updater installation has not been exercised as an end-to-end hardware test; the integrated ARM64 runtime suite, package contents and signatures are verified, and native import/playback was tested on the owner's existing experimental installation.
