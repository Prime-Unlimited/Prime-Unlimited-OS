# Prime 4 G2 (JP24)

Status: Device-specific Fastboot identification, partition layout, firmware compatibility, SSH payload and recovery steps still require verification.

Do not reuse JC11S images or partition assumptions for this device.

On macOS, discovery uses structured IORegistry output with structured `system_profiler` fallback. This detects the Issue #3 device even when `system_profiler SPUSBDataType` is empty. The observed `15E4:D014` identifier and `PRIME 4 G2` product string are candidate hints only; the imbupd protocol, model identity, and hardware revision remain unverified.
