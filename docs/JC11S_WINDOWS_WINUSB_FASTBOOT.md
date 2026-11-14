# JC11S native Windows WinUSB Fastboot — verified bench notes

Date: 2026-11-14. Device: Denon DJ PRIME 4 Plus (JC11S), connected in Fastboot bootloader mode. Evidence: interactive Windows PowerShell session reported by the tester. **This is a single-device proof of concept, not a validated production flasher.**

## Result

**Confirmed:** Native Windows Fastboot protocol communication works using Microsoft's inbox WinUSB driver and direct WinUSB API calls, without WSL2, usbipd-win, Zadig, or a driver replacement.

1. Sending ASCII `getvar:version` to bulk OUT endpoint `0x02` returned ASCII **`OKAY0.4`** from bulk IN `0x81`.
2. Sending ASCII `reboot` through the same transport was followed by the **physical device restarting**, as confirmed by the tester.
3. Google's Android SDK Platform Tools **fastboot 37.0.0-14910828** returned an **empty device list** for `fastboot devices -l` on this Windows machine. This does **not** imply a broken WinUSB driver. The precise discovery/filtering failure remains unproven.

The direct reboot observation confirms command delivery and execution. The transcript does not establish whether a final `OKAY` packet was received for reboot.

## Windows device identification

| Field | Observed |
| --- | --- |
| PnP friendly name | Denon DJ PRIME 4 Plus |
| Device instance | `USB\VID_15E4&PID_D007\A12506258617421` |
| Hardware IDs | `USB\VID_15E4&PID_D007&REV_2607`; `USB\VID_15E4&PID_D007` |
| Compatible ID | `USB\MS_COMP_WINUSB` |
| Interface class/subclass/protocol | `FF/42/03` (Fastboot-compatible) |
| Service | `WINUSB` |
| Driver INF | `winusb.inf` |
| Driver provider | Microsoft |
| Driver version | `10.0.26100.9818` |
| Bulk IN | `0x81`, type 2 |
| Bulk OUT | `0x02`, type 2 |
| Protocol version response | `OKAY0.4` |

Three enabled Windows device interface paths for this same device instance were observed, each successfully opened using `CreateFileW`, initialized using `WinUsb_Initialize`, and queried using `WinUsb_QueryInterfaceSettings` and `WinUsb_QueryPipe`:

- `{4866319a-f4d6-4374-93b9-dc2deb361ba9}`
- `{dee824ef-729b-4a0e-9c14-b7117d33a817}`
- `{a5dcbf10-6530-11d2-901f-00c04fb951ed}`

Do **not** hardcode the device instance serial or these interface GUIDs as universally applicable. Enumerate enabled WinUSB device interfaces at runtime, validate VID/PID **and** actual USB interface descriptors, and reject ambiguity. Some GUIDs are generic device-interface classes and are not unique JC11S identity proofs.

## Reproduction steps (read-only until explicit reboot)

1. Enter the device's known Fastboot bootloader mode and connect it by USB to Windows.
2. Inspect `Get-PnpDevice -PresentOnly` and `Get-PnpDeviceProperty -InstanceId <id>` for hardware IDs, `DEVPKEY_Device_Service`, and `DEVPKEY_Device_DriverInfPath`.
3. Inspect `pnputil /enum-interfaces /enabled` for matching USB interface paths.
4. Call `CreateFileW(path, GENERIC_READ | GENERIC_WRITE, FILE_SHARE_READ | FILE_SHARE_WRITE, ..., OPEN_EXISTING, FILE_FLAG_OVERLAPPED, ...)` via a correct P/Invoke signature. In PowerShell, use unsigned values `[uint32]3221225472` and `[uint32]1073741824` rather than signed `0xC0000000` literals.
5. Call `WinUsb_Initialize`, then `WinUsb_QueryInterfaceSettings(handle, 0)` and `WinUsb_QueryPipe(handle, 0, index)`; verify `FF/42/03` and exactly the expected direction/type of bulk endpoints. Do not assume endpoint IDs on other firmware.
6. Set `PIPE_TRANSFER_TIMEOUT` (policy ID 3) to 2000 ms for the selected IN and OUT pipes using `WinUsb_SetPipePolicy`.
7. `WinUsb_WritePipe` the ASCII bytes `getvar:version` to OUT `0x02` (14 bytes), then `WinUsb_ReadPipe` from IN `0x81`; observed `OKAY0.4`.
8. **Only with explicit user consent**, send ASCII `reboot` (6 bytes) to OUT `0x02`. Tester confirmed that the controller restarted.
9. Free the WinUSB interface handle and close the Windows file handle in a `finally` block. Reboot can disconnect the device before a response is read; handle removal gracefully.

The proof-of-concept used C# P/Invoke from PowerShell to call `kernel32.dll` (`CreateFile`, `CloseHandle`) and `winusb.dll` (`WinUsb_Initialize`, `WinUsb_Free`, `WinUsb_QueryInterfaceSettings`, `WinUsb_QueryPipe`, `WinUsb_SetPipePolicy`, `WinUsb_WritePipe`, `WinUsb_ReadPipe`). No custom USB driver was installed.

## Scope, implementation and safety

- Implement this as a Windows-only native WinUSB transport for **JC11S**; retain existing safe detection and a separate JP24 imbupd transport. macOS requires a separate implementation, not WinUSB.
- Separate discovery/identity validation, read-only queries, explicit reboot, and destructive operations. The `15E4:D007` VID/PID and Fastboot interface are strong transport hints but **not sufficient on their own** to authorize flashing.
- Use SetupAPI/Configuration Manager device-interface enumeration, correct resource lifetimes, bounded timeouts, cancellation/disconnect handling, and explicit protocol response parsing (`INFO`, `OKAY`, `FAIL`, `DATA`). Test multiple and missing devices.
- For now allow only safe `getvar` probes and explicitly confirmed reboot. Keep `flash`, `erase`, `download`, and other writes **disabled/fail-closed** until firmware compatibility, verified image provenance, backup/recovery, transfer sizing, integrity, and end-to-end testing are completed.
- Do not claim that Google's `fastboot.exe` supports this device natively; it did not enumerate it in the observed test. Do not assume a new driver, Zadig, WSL2, or admin elevation is required.
- Never log sensitive device identifiers or private SSH key material by default; redact diagnostics.
- Keep mandatory SSH setup and host-key verification requirements intact.

## Suggested validation matrix

- Read-only enumeration with no device, one supported device, multiple devices, and an unrelated WinUSB device.
- Distinguish JC11S from JP24; no cross-model fallback.
- Test `getvar:version` returning `OKAY0.4`; parse `FAIL`, timeouts, partial responses and disconnects.
- Explicitly approved reboot and subsequent device removal.
- Mock WinUSB API in CI; run an opt-in Windows hardware-in-the-loop test on a real JC11S.
- No flash tests until independent recovery/backup validation.

This document records **observed behavior**, not a claim that a complete installer or flashing workflow has been validated.
