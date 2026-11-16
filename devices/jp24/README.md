# Prime 4 G2 (JP24)

Status: Root SSH access works fully automatically on real hardware - flash,
reboot, SSH in, no manual intervention. Confirmed end to end through the GUI
installer itself (device selection → USB diagnostics → SSH key setup →
flash → automatic reboot → `ssh -p 2222 root@<device-ip>`). The payload is
in [`ssh-bootstrap/`](ssh-bootstrap/); the flash step is wired into
`core/jp24_bootstrap.py` / `core/transports.py`. Not yet wired: an
authenticated post-flash SSH connection test inside the wizard itself
(currently a manual step after the GUI finishes) and the mod-manager flow
in `core/ssh_service.py` / `core/mods.py`.

## Device identity

- USB Firmware Update Mode: VID `0x15E4`, PID `0xD014`
- SoC: Rockchip RK3588, AArch64
- Protocol: proprietary `imbupd` (not Fastboot, not USB Mass Storage) -
  magic `AZ0O`/`AZ0I`, bulk transfer, opcodes for get-property,
  write-partition, erase-partition, reboot
- Rootfs is dm-verity protected (read-only, no overlay) - the only
  writable, persistent location is the `/data` partition

## How SSH access works

1. **`imbupd_write_mac.c`** flashes a prepared ext4 image onto the `/data`
   partition while the device is in Firmware Update Mode.
2. That image contains an ALSA hook: `99-prime4-mod.conf` lands at
   `/data/system/var-lib/alsa/conf.d/`, which systemd's `var-lib.mount`
   unit bind-mounts to `/var/lib/alsa/conf.d/`. ALSA's own top-level
   config loads everything there on first use.
3. The conf.d file's `hook_func` + `@hooks` block `dlopen()`s
   `libalsa-boot-hook.so` (built from `alsa-boot-hook.c`) and calls
   `prime4_mod_alsa_hook()`, which runs as root.
4. That hook forks and `execl()`s `sshd` directly (no shell script, no
   systemd unit) on port 2222 - port 22 is occupied by inMusic's own
   sshd, which only accepts their CA-signed certs.

Four bugs blocked this for a long time, each silent by default - sshd and
ALSA both just do nothing observable rather than report an error anyone
would see without adding diagnostics first:

- **musl vs glibc**: the hook `.so` must be built for glibc
  (`zig cc -target aarch64-linux-gnu.2.17 ...`), not musl. A musl build
  requests `libc.so` as a dynamic dependency; the device only has
  `libc.so.6`. `dlopen()` just fails every boot with nothing to see.
- **ALSA conf.d syntax**: a file loaded into `conf.d/` needs
  `hook_func.NAME { lib "..." func NAME }` plus `@hooks [ { func NAME } ]`.
  The top-level `alsa.conf` syntax (`hooks.0 { type hooks ... }`) does not
  get evaluated when merged in this way.
- **Missing privilege-separation directory**: `sshd` refuses to start
  without `/var/run/sshd` - normally created by its systemd unit's
  tmpfiles.d entry before sshd is ever invoked. Without it sshd exits
  immediately (a `<defunct>` zombie); `fork()`/`execl()` both still look
  successful from the caller's side. `mkdir("/var/run/sshd", 0755)`
  before exec'ing it.
- **`execl()`'s `argv[0]` must be the absolute path**: `sshd` re-execs
  itself for privilege separation and checks its own `argv[0]` for that;
  passing just `"sshd"` instead of `"/usr/sbin/sshd"` makes it exit with
  "sshd re-exec requires execution with an absolute path" - again only on
  its own stderr. This one is particularly deceptive: typing the exact
  same command into an interactive shell always works, because a shell
  sets `argv[0]` to whatever you typed (the full path), masking the bug
  during manual testing.

The hook's exported symbol names must also match exactly (the "Cd1s
fix"): `prime4_mod_alsa_hook`, `snd_config_hook_prime4_mod_alsa_hook`,
and both `_dlsym_config_hook_001` version-tag data symbols. Without
these, LED and button control on the device break even though the hook
itself still loads fine.

`fork()`/`execve()` from inside the hook are not restricted on this
device - this was a real concern during development but turned out not
to be the actual blocker once the bugs above were fixed.

## Files

- `ssh-bootstrap/alsa-boot-hook.c` - the ALSA hook shared library (also
  starts sshd directly - no separate boot script needed)
- `ssh-bootstrap/99-prime4-mod.conf` - the ALSA conf.d file
- `ssh-bootstrap/build-data-image.sh` - builds the ext4 `/data` image
  (macOS, needs `e2fsprogs` and either `zig` or `aarch64-linux-gnu-gcc`)
- `ssh-bootstrap/imbupd_write_mac.c` - the USB flash tool (macOS/libusb)

## Usage

Via the GUI installer (`python -m app.main`, run in the user's own
terminal - not through an agent's sandboxed shell, which can break
`system_profiler`/`ioreg`-based USB detection): select "Denon DJ Prime 4
G2" → run diagnostics → generate or import an SSH key → "Flash and enable
SSH".

Or directly:

```bash
brew install libusb e2fsprogs

cc -o imbupd_write_mac ssh-bootstrap/imbupd_write_mac.c \
  -I/opt/homebrew/include -L/opt/homebrew/lib -lusb-1.0

ssh-keygen -t ed25519 -f ./denon_prime4_ed25519 -C "prime4-g2-mod"

bash ssh-bootstrap/build-data-image.sh ./denon_prime4_ed25519.pub ./prime4-data.img

# Put the device into Firmware Update Mode, then:
sudo ./imbupd_write_mac query
sudo ./imbupd_write_mac write data ./prime4-data.img --confirm "FLASH JP24 /data"
sudo ./imbupd_write_mac reboot

# After boot + Wi-Fi comes up:
ssh -i ./denon_prime4_ed25519 -p 2222 -o StrictHostKeyChecking=accept-new root@<device-ip>
```

Survives firmware updates: they only replace the root partition image
(new dm-verity tree), never `/data`.

## Not yet done

- No authenticated SSH connection test inside the wizard itself (the GUI
  flash step succeeds and reboots the device; verifying the SSH login is
  currently a manual step afterward)
- No automatic device-IP discovery (mDNS/avahi or router ARP scan)
- No uninstall path exposed through the installer UI (manual removal via
  an active SSH session works: delete `/data/prime4-mod/` and the
  `99-prime4-mod.conf` file, then reboot)
- The mod-manager flow (`core/ssh_service.py`, `core/mods.py`) is not yet
  connected to this device profile
