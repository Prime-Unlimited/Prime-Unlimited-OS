# Revision-aware USB discovery

Prime-Unlimited OS separates broad discovery from compatibility authorization:

1. **Discovered** — the operating system exposes a USB device.
2. **Candidate** — observed identifiers or product/manufacturer text suggest JC11S or JP24.
3. **Transport detected** — interface descriptors and endpoints suggest Fastboot or another device transport.
4. **Protocol verified** — a safe read-only protocol exchange succeeds.
5. **Model verified** — an independent, documented model identity check succeeds.
6. **Revision verified** — the hardware revision is in a validated compatibility allowlist.
7. **Installation authorized** — every safety, firmware, package, backup, and recovery gate passes.

The current implementation can reach protocol verification for JC11S Fastboot on Windows. It cannot independently verify the model or revision and never authorizes installation. JP24 discovery does not verify the proprietary imbupd protocol.

## Windows

The Windows backend dynamically enumerates registered device-interface classes with SetupAPI. It groups interface paths by physical device instance and records hardware IDs, driver/service, manufacturer, product, location, and available serial data. Known VID/PID values and product strings are candidate hints.

JC11S transport discovery considers accessible WinUSB interfaces broadly. It opens an interface without sending a command, validates `FF/42/03`, and discovers exactly one bulk IN and OUT endpoint. Only after that non-destructive evidence exists does it send `getvar:version`. Unrelated WinUSB interfaces receive no protocol command. Interface GUIDs, paths, and serials are never permanent device-family identities.

## macOS and Issue #3

Issue #3 showed a connected Prime 4 G2 in IORegistry while `system_profiler SPUSBDataType` returned no output. The macOS backend now reads structured plist output from `ioreg` (`IOUSBHostDevice`, then the IOUSB plane) and parses `idVendor` and `idProduct` independently. Structured JSON from `system_profiler` is a fallback rather than the sole discovery mechanism.

The observed JP24 identifier `15E4:D014` is a hint, not a guarantee. A device with changed identifiers but a Prime 4 G2 product string is displayed as a possible new revision. Its transport, model, revision, and compatibility remain unverified, and installation stays blocked.

## Revision handling

Observed identifiers:

| Family | Observed USB hint | Observed transport evidence |
|---|---|---|
| JC11S / Prime 4+ | `15E4:D007` | WinUSB `FF/42/03`, bulk pipes, Fastboot `OKAY0.4` |
| JP24 / Prime 4 G2 | `15E4:D014` | USB device visible; imbupd protocol not implemented |

Future revisions may change any of these values. Discovery therefore keeps unfamiliar hardware visible and records unknown fields as unknown. Product text can establish candidacy only. A successful Fastboot response verifies the protocol only. Neither authorizes a write.

When several relevant candidates are connected, the wizard requires an explicit selection and reruns diagnostics for that physical device. It never silently chooses a candidate or switches the manually selected product family.
