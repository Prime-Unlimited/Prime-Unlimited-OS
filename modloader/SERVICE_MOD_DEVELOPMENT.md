# Managed native service mods

A service declaration in a verified schema-v2 catalog permits one native ELF
binary and a bounded runtime capability. The first capability is `splash-image`.
Neither package metadata nor a mod script can grant service privileges.

```json
"service": {
  "binary": "example/bin/server-aarch64",
  "sha256": "<SHA-256 of the uncompressed native executable>",
  "capability": "splash-image"
}
```

## Installation and lifecycle

Installation verifies the cached ZIP and staged native digest and architecture,
then records the signed service declaration in the private registry. The package
remains disabled. Enabling starts a transient `pu-mod-<hash>.service` through
`systemd-run`; package shell scripts are not invoked. The root runtime copies
only the verified native executable to an immutable runtime directory in `/run`.

The unit uses `DynamicUser=yes`, an empty capability set, `NoNewPrivileges`,
a read-only system filesystem, private devices/tmp, bounded memory/tasks and
control-group termination. It is bound to the trusted runtime API unit. It is
never installed into a firmware partition or enabled independently at boot.
The runtime reconciles enabled services at startup and every five seconds.
Disable stops the service before committing registry changes. Uninstall stops
any residual unit before removing the disabled package. Safe mode, quarantine
and signed catalog revocation stop services and revoke their scoped credentials.
Three failed starts/crashes quarantine the service; recovery uses the existing
maintainer-authorized recovery endpoint. Any number of service mods may be
enabled together. Each keeps its own unit, tokens and TLS material.

This requires a systemd system manager supporting DynamicUser, Type=exec,
StandardInput=file, BindsTo and the listed sandbox properties (systemd 240+
with the corresponding kernel facilities). Unsupported provisioning fails
closed and returns an error. This support has not been verified on JC11S/JP24
hardware; a Linux unit test is not evidence of device/systemd compatibility.

## Credentials and file access

The runtime generates distinct 256-bit web and broker tokens. It passes them
and the device TLS key to the service as JSON on stdin using a root-private
file, never command-line arguments, environment variables or the mod API token.
The service cannot read the private registry, credential files or mutation token.

The service sends GET/POST to `http://127.0.0.1:8098/api/v1/service-splash`
with its broker token. The runtime checks the enabled installed version against
the current trusted compatible catalog and rejects safe-mode/quarantined mods.
POST requires image/png, at most 10 MiB, full PNG decoding and at most 16 MP.
It atomically writes only `/data/prime-unlimited/ui/splash/SplashScreen.png`.
The service has no general filesystem write capability.

## Browser access

The managed Splash Manager serves HTTPS on port 8443. Its web token authorizes
browser upload/preview, but cannot authorize runtime mutations or broker writes.
The authenticated `POST /api/v1/service-access` endpoint returns a six-digit, single-use pairing
code, LAN URLs and SHA-256 certificate fingerprint to the trusted Modloader UI.
Codes expire after two minutes and lock after five incorrect attempts.
The service exchanges the code at its scoped `/api/v1/service-pair` endpoint;
the browser receives the 256-bit web token automatically over HTTPS.
The WEB ACCESS dialog displays those values on the device, so SSH is not needed
for routine browser use. The browser must verify the displayed fingerprint
before accepting the self-signed certificate. Credentials are revoked at stop;
the root-private device certificate persists until explicitly rotated.

Persistent splash data and the device TLS identity are retained on uninstall.
Runtime executable copies and per-service credentials are removed on stop.
