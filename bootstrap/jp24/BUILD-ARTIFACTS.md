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

The current host code already constructs this common header for property and erase commands. Its specialized write-start packet is different: it places the partition name at offset `0x28` and omits the vendor's preceding standard `write-part` request with the name at `0x18`. So the write rejection is not explained by the general packet-size field being absent; the remaining issue is the multi-stage write protocol and its state/response handling.

## Static analysis of the vendor write handshake (2026-11-14)

The arm64 slice of the extracted PRIME 4 G2 5.3.0 Updater binary was disassembled read-only with Apple's `otool`. Relevant symbols were present at the addresses already listed in the Vault: `_imbupd_write_partition` (0x10018231c), `_imbupd_stream_partition` (0x100182500), `_imbupd_transfer` (0x10018315c), and `_imbupd_start_write` (0x1001833d8).

The vendor `_imbupd_start_write` routine appears to perform two distinct `write-part` request/response exchanges before streaming:

1. It builds a standard 0x18-byte protocol header, appends the NUL-terminated partition name at offset 0x18, sets packet size to header plus that name, and calls the common send/response routine. That routine is given a 0x1c-byte response length.
2. It then builds a separate start/stream descriptor with the total image size at offset 0x08, the partition name at offset 0x28, and packet size `0x29 + strlen(partition)`; it sends that as another `write-part` request and reads a response.
3. Only after `_imbupd_start_write` succeeds does `_imbupd_write_partition` call the chunk-transfer routine.

The current host writer sends only a packet shaped like step 2 and immediately expects an exact opcode/sequence/status echo. The vendor implementation therefore exposes a likely protocol mismatch that can explain the rejected start response. This is a static disassembly finding, not a hardware-confirmed repair. The precise response semantics and how the stream's finalization maps to the host implementation still need to be reconciled before changing the writer or attempting another transfer.

The earlier writer accepted any response of at least 0x18 bytes at the start stage and streamed anyway; that weak check is not evidence that the response was valid. A previous Vault entry reports a successful 256 MiB G2 transfer, but the saved notes do not include a raw USB trace proving which handshake the device accepted.

### Further handshake details from the vendor caller

- `_imbupd_write_partition` passes the image size to `_imbupd_start_write`, then calls `_imbupd_transfer` only if start succeeds.
- The common response parser requires a response at least as long as requested, matching opcode and sequence, and a success status bit; a response of length 0x18 alone is not sufficient evidence of success.
- The transfer routine sends chunks capped at 0x100000 bytes (1 MiB), matching the host chunk size.
- After streaming, the vendor caller waits for a response through its read-response routine. No explicit one-byte zero terminator is visible in the inspected vendor caller/transfer paths. The host writer currently sends one; whether that is tolerated or harmful is unverified.
- The vendor's request sequence is incremented from a per-connection counter for each command. The host's separate fixed counters (0, 100, 200, 300) do not reproduce that sequence model.

These observations strengthen the case that the host protocol needs a complete stateful implementation. They do not establish what happened to `/data` in the interrupted session, and do not justify retrying with a relaxed response check.

## Interrupted transfer on 2026-11-14

A user-authorized 64 MiB transfer attempt sent an `erase-part data` request and then stopped at a write-start response mismatch before streaming image bytes or rebooting. The erase response was not instrumented with strict opcode/sequence checks in that build, so the exact erase outcome is unknown; treat `/data` as potentially erased. A following query received sequence-shifted responses and did not confirm current `version` or `power-source`. No further USB commands have been sent.

The pasted `az01-signed-fs` analysis concerns signed test-app containers and does not explain the `imbupd` write-start response. The corrected local images use 4 KiB ext4 blocks as documented in the Vault, but have not been streamed to hardware. The original updater handshake must be fully decoded and reviewed before another write attempt.
