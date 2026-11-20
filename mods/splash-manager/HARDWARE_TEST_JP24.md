# JP24 service hardware test — 2026-11-20

Source: `5b3e56e2e5786dc66bf2bde7241a9bd88556f3b8`, Splash Manager 1.1.0.
Device identity: JP24, aarch64, Engine firmware contract 5.3.0, ABI gnu.
Device systemd: 255 (255.21), unified cgroups.

## Test isolation

A temporary runtime unit used a separately signed local development catalog and
local test trust file. Production catalog/trust files, runtime executable and
start script were retained. Bind-mounted private copies of registry, activation,
safety and installed-package state isolated lifecycle changes. The device UI
was backed up and temporarily replaced to expose WEB ACCESS. A 30-minute
systemd rollback timer restores the original runtime and UI. The local signing
private key was never copied to the device or repository.

The package was transferred into the verified test cache over SSH. GitHub asset
download and official catalog publication were not exercised by this test.

## Observed on hardware

- Stage and inspect: HTTP 200; installation and activation: HTTP 202.
- Actual systemd sandbox: DynamicUser=yes, ProtectSystem=strict,
  NoNewPrivileges=yes; process UID 62432 and kernel NoNewPrivs=1.
- HTTPS certificate fingerprint matched the fingerprint returned through the
  authenticated management path.
- Authenticated browser login: HTTP 204.
- Unauthorized upload: HTTP 401; malformed PNG: HTTP 400.
- Valid PNG upload and preview: HTTP 200.
- Existing 80,283-byte splash was uploaded unchanged. Before/after SHA-256:
  `40da40f5f9928f8232c44780d4ba39eb82b4e9b9c80ed556ae035ee763424f52`.
- Disable stopped the actual service. Uninstall, reinstall and re-enable
  completed successfully through the runtime API.
- Safe Mode stopped the service; leaving Safe Mode started it again.
- Final HTTPS login worked with the newly issued token after lifecycle tests.
- Engine, SSH and runtime API remained active.

The initial staging attempt rejected files whose transfer preserved the Mac
UID. The previous runtime was restored automatically; the repeated deployment
set root ownership on the test files and passed staging. This was a deployment
fixture correction, not a relaxation of ownership checks.

## Remaining release gates

No reboot or visual boot-splash verification was performed. Human interaction
with the on-device WEB ACCESS dialog and browser UI still needs confirmation.
JC11S is untested. Both committed hardwareVerified flags remain false, and no
production catalog approval/signature was generated. These results establish
JP24 service/API operation, not an official release decision.
