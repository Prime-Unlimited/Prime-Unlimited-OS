# Device core

The core contains immutable profiles, platform-aware read-only USB diagnostics, profile matching, Ed25519 key handling, pinned-host SSH verification, package integrity checks, strict mod manifests, and secret redaction. On Windows, JC11S uses SetupAPI and WinUSB directly to enumerate interfaces, validate `FF/42/03`, discover bulk pipes, and query the Fastboot protocol version. Other platforms retain the external read-only Fastboot probe. The JP24 diagnostic remains separate and treats VID:PID `15e4:d014` only as unverified transport evidence.

`usb_discovery.py` provides normalized hardware records and independent verification states. `windows_usb_discovery.py` normalizes dynamic SetupAPI results. The macOS backend parses IORegistry plist output and uses structured `system_profiler` JSON only as fallback, allowing JP24 discovery when profiler output is empty.

`fastboot_protocol.py` intentionally exposes only `getvar` and `reboot`. `windows_fastboot.py` contains the platform-independent transport and injectable API boundary; `windows_winusb_native.py` is imported only on Windows and owns overlapped native handles with bounded pipe timeouts, cancellation, buffer lifetime protection, and deterministic cleanup.

Both transport writers fail closed. Enabling either one requires positive device and firmware identity, licensed pinned packages, tested backup/recovery, explicit confirmation, real progress/cancellation support, and hardware end-to-end validation.
