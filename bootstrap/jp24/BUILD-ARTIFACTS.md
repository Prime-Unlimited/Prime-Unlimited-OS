# JP24 Build Artifact Record

## Local image build (2026-11-14)

- Profile: 256 MiB ext4 image labeled `data`, built from the JP24 payload on the developer workstation.
- Local path: `bootstrap/jp24/build/prime4-g2-ssh.img` (generated output; not committed or uploaded).
- SHA-256: `669d18ef7df9930eb5f01144af43f84548d877ec09d8081fd3ab3b9b9a93294c`.
- Build inputs included the ALSA hook with constructor activation, `99-prime4-mod.conf`, `boot-activate.sh`, and the installer's Ed25519 public key. No private key is included in the image.

## Checks performed

- The build script completed its ext4 filesystem check and confirmed the `encrypt` filesystem feature.
- Read-only inspection of the generated image confirmed the expected `/prime4-mod` and `/system/var-lib/alsa/conf.d` payload paths.
- Extracted AArch64 ELF shared object had these dynamic symbols: `prime4_mod_alsa_hook`, `snd_config_hook_prime4_mod_alsa_hook`, `_prime4_mod_alsa_hook_dlsym_config_hook_001`, and `_snd_config_hook_prime4_mod_alsa_hook_dlsym_config_hook_001`.
- Both version-symbol strings contained `ALSA_0.9`; the ELF contained a non-empty `.init_array` section for constructor execution.
- `/prime4-mod/boot-activate.sh` had mode `0755` in the image.

## Limits

These checks establish the local image contents and ELF metadata only. They do not prove that the G2's modified ALSA loader accepts the symbol version contract, that the constructor runs under the firmware's runtime conditions, or that LEDs/SSH work on hardware. The recorded hash applies only to this local build; generated filesystem metadata may make later builds differ.

This recorded artifact is 256 MiB. The builder and writer also accept an exact 64 MiB profile. The Vault reports successful G2 use of a 256 MiB image and separately summarizes `/data` as “56 MB” at 24% used, without raw `df` output or clear units. These facts do not establish the partition's maximum capacity. No raw `/data` backup or demonstrated restore path is documented. The Vault's signed-firmware recovery idea and UART/U-Boot UMS route remain unverified.

## Additional local build: 64 MiB profile

- Path: `bootstrap/jp24/build/prime4-g2-ssh-64.img` (generated output; not committed or uploaded).
- SHA-256: `c0fd695de9d7ab28554a8820adb1dc1d5616e18a95481677004b0e3fccddce75`.
- The builder completed the ext4 filesystem check and confirmed the `encrypt` feature. Both profiles use 4096-byte ext4 blocks, matching the Vault recipe.
- The same source payload and public SSH key were used as the 256 MiB profile.

Neither local artifact has been written to hardware during this build session.

### Historical probe finding: common packet header

A recovered Claude session records the initial general-header breakthrough: request magic bytes `41 5A 30 4F` (`AZ0O`), opcode at offset `0x04`, packet size (`uint16`) at `0x16`, and payload at `0x18`. The report says an `imbupd_write` binary was uploaded to a Linux VM and its `query` command was then tested. This establishes the general request framing for the probe/query path; it does not document or prove a successful partition write.

The current host code already constructs this common header for property and erase commands. It also calls `erase_part("data")` before sending its specialized write-start descriptor, so it has the same high-level erase-then-write ordering. The recovered Claude `query` test verifies only general request framing; it does not verify the descriptor, sequence continuity, streaming, or final response.

## Static analysis of the vendor write handshake (2026-11-14)

The arm64 slice of the extracted PRIME 4 G2 5.3.0 Updater binary was disassembled read-only with Apple's `otool`. Relevant symbols were present at the addresses already listed in the Vault: `_imbupd_write_partition` (0x10018231c), `_imbupd_stream_partition` (0x100182500), `_imbupd_transfer` (0x10018315c), and `_imbupd_start_write` (0x1001833d8).

Correction from the literal constants and call flow: `_imbupd_start_write` sends two different commands before streaming, not two `write-part` commands:

1. It builds a standard 0x18-byte header with `erase-part` opcode `0x5AA5FC03`, appends the NUL-terminated partition name at offset 0x18, and sends it through the common response parser. This is the vendor's erase step.
2. It then builds the start/stream descriptor with `write-part` opcode `0x5AA5FB04`, total image size at offset 0x08, partition name at offset 0x28, and packet size `0x29 + strlen(partition)`, then sends it through the common response parser.
3. Only after both commands succeed does `_imbupd_write_partition` call the chunk-transfer routine.

The opcode identification is grounded in the constants loaded by `_imbupd_start_write`: the first 8-byte template at `__TEXT,__const` address 0x1002556d8 contains request magic plus `erase-part`; the second at 0x100255700 contains request magic plus `write-part`. The common response parser checks response length, matching opcode and sequence, and success status. This is static disassembly evidence, not a hardware trace.

The earlier writer accepted any response of at least 0x18 bytes at the start stage and streamed anyway; that weak check is not evidence that the response was valid. A previous Vault entry reports a successful 256 MiB G2 transfer, but the saved notes do not include a raw USB trace proving which handshake the device accepted.

### Further handshake details from the vendor caller

- `_imbupd_write_partition` passes the image size to `_imbupd_start_write`, then calls `_imbupd_transfer` only if start succeeds.
- The common response parser requires a response at least as long as requested, matching opcode and sequence, and a success status bit; a response of length 0x18 alone is not sufficient evidence of success.
- The transfer routine sends chunks capped at 0x100000 bytes (1 MiB), matching the host chunk size.
- After streaming, the vendor caller waits for a response through its read-response routine. A user-provided Claude session log records a separate completion step followed by final status `1`, but the pasted log does not include the completion bytes. The exact correspondence to the vendor routine remains unconfirmed.
- The vendor's request sequence is incremented from a per-connection counter for each command. The host's separate fixed counters (0, 100, 200, 300) do not reproduce that sequence model; continuity is a plausible cause of the response mismatch, but has not been confirmed on hardware.

These observations strengthen the case that the host protocol needs a complete stateful implementation. They do not establish what happened to `/data` in the interrupted session, and do not justify retrying with a relaxed response check.

## User-provided transfer log and LED behavior (2026-11-15)

A pasted Claude-session transcript reports a 64 MiB `/data` write from a Linux VM. It is user-provided terminal output, not independently captured by this session:

- The tool first queried protocol version and power source, then sent `erase-part data` at sequence 3; response status was 1.
- It sent a 45-byte `write-part` descriptor at sequence 4: image size 0x04000000 at offset 0x08, packet size 0x002d at offset 0x16, and `data\\0` at offset 0x28.
- The write-start response echoed sequence 4 and returned status 5. The tool proceeded to stream 67,108,864 bytes in 1 MiB chunks, sent a completion step, then received a final response with sequence 4 and status 1.
- The transcript reports the unit rebooted normally, with LEDs continuing a running-light pattern. SSH success was not reported.

This trace shows status 5 was accepted as the write-start response by the reported tool, followed by final status 1. The native writer's former `status == 1` start check was too strict for this flow. The observed sequence 3 then 4 follows two property queries and matches the vendor's per-connection counter, which starts from zeroed context and increments before each request. The writer has now been changed to query version and power first, use one pre-incrementing sequence counter, and accept status 5 for write start while still requiring matching opcode/sequence and final status 1. This is a code-level change based on the pasted trace, not a hardware-verified repair.


The updated writer passed a C syntax-only compilation on the workstation. It has not been run against USB hardware. No new transfer was started by this session.

### Older local image associated with the pasted path

Read-only inspection of the local file `mod/alsa-trigger/prime4-data.img` (SHA-256 `50e42d90de48c5ccdf259e5769cef5632c7e1316a4099ab7648946f397ce3349`) found an older ALSA configuration using `func load` with an empty `files` list. The embedded AArch64 hook's dynamic symbol table exposed `prime4_mod_alsa_hook` only; the expected `snd_config_hook_` function and `_dlsym_config_hook_001` version symbols were absent. This is materially different from the corrected payload in `bootstrap/jp24/payload/`, which exports both hook symbols and both `ALSA_0.9` version strings.

The old image is a plausible explanation for the reported LED running-light behavior because the Vault documents that incorrect ALSA hook symbols can cause ALSA/LED initialization errors. This remains an inference: we inspected the current local file at that path and cannot prove it is byte-for-byte identical to the image that was uploaded and flashed. The transcript does not report whether SSH on port 2222 became reachable.

Do not treat the transfer log as permission to resend an image. First confirm the current hardware state and use the corrected hook image only after the write implementation is reviewed.
## Interrupted transfer on 2026-11-14

A user-authorized 64 MiB transfer attempt sent an `erase-part data` request and then stopped at a write-start response mismatch before streaming image bytes or rebooting. The erase response was not instrumented with strict opcode/sequence checks in that build, so the exact erase outcome is unknown; treat `/data` as potentially erased. A following query received sequence-shifted responses and did not confirm current `version` or `power-source`. No further USB commands have been sent.

The pasted `az01-signed-fs` analysis concerns signed test-app containers and does not explain the `imbupd` write-start response. The corrected local images use 4 KiB ext4 blocks as documented in the Vault, but have not been streamed to hardware. The original updater handshake must be fully decoded and reviewed before another write attempt.

## macOS USB capture availability (2026-11-15)

The Prime 4 G2 was visible in macOS IORegistry by its product name while connected. The host nevertheless exposed no Wireshark USB capture interface: `tshark -D` and `dumpcap -D` listed no `XHC20`, `ifconfig XHC20` reported that the interface does not exist, and the installed Wireshark extcap directory contained no USB capture backend. The Linux VM at `192.168.64.3` was not reachable over SSH during this check. No capture was started and the Denon updater was not launched.

Wireshark documents macOS USB capture through `XHC20`; that path is unavailable in the current host configuration. The installed Wireshark build has no alternate local USB extcap. Do not disable SIP or change host security settings as an implicit workaround. A Windows host/guest with USBPcap or a suitable external USB analyzer would be an alternate capture path; neither was available in this session.


## EngineX host-side protocol cross-check (2026-11-15)

Read-only review of [EngineX OS commit c8b1f097](https://github.com/Cd1s/EngineX-OS/tree/c8b1f097bc6e27cb7c4f779b7db2801d4d7e1a6e) found a direct host implementation in [`tools/az01-packer/host-install-usbupdate.py`](https://github.com/Cd1s/EngineX-OS/blob/c8b1f097bc6e27cb7c4f779b7db2801d4d7e1a6e/tools/az01-packer/host-install-usbupdate.py), plus the recovery-side protocol analysis in [`docs/GEN2-USB-UPDATE-MODE.md`](https://github.com/Cd1s/EngineX-OS/blob/c8b1f097bc6e27cb7c4f779b7db2801d4d7e1a6e/docs/GEN2-USB-UPDATE-MODE.md). This is useful protocol evidence, but not a capture of the Denon macOS updater.

The EngineX helper's documented flow is: send a 45-byte `AZ0O` `write-part` descriptor, receive `AZ0I` status 5 (ready), stream the raw image, send a zero-length packet, and require final status 1. It starts sequence numbering at 1, tries partition `data` then `userdata` if it receives status 3, and does not issue an `erase-part` command in this helper. The descriptor is built as little-endian `<4sIQIHHQQ`, with the total image size, sequence, packet length, offset 0, write-if-changed flag 1, then the NUL-terminated partition name.

This matches the user-provided historical G2 log at the field level: the `write-part` opcode is `0x5AA5FB04`, a 64 MiB size is `0x04000000`, the descriptor length is 45 bytes for `data\\0`, status 5 precedes the stream, and status 1 follows completion. The historical log has an extra preflight/erase phase and therefore reaches sequence 4; EngineX's standalone helper begins directly at sequence 1. Those are two host flows, not contradictory evidence. The exact logged sequence and bytes remain user-provided transcript data, not an independently captured USB trace.

The Gen-2 recovery analysis identifies the device side as the `az0x-splash usbupdate` FunctionFS gadget and lists `get-property`, `erase-part`, `write-part`, `write-boot`, and `get-pubkey` commands. This explains why EngineX can inform our own writer and packet decoder without revealing whether the vendor GUI enables its private trace callback or what additional initialization it performs. No host USB stream was captured, no firmware updater was launched, and no hardware write was performed during this review.
