# Prime Unlimited OS — SSH Bootstrap

SSH is **mandatory**, not an optional mod. The first installation uses the device's native USB update transport:

| Device | USB transport | SSH port | Verified scope |
|---|---|---:|---|
| Prime 4+ / JC11S | Fastboot | 22 | EngineX-based key-only SSH observed |
| Prime 4 G2 / JP24 | imbupd (not Fastboot) | 2222 | ALSA-hook SSH observed |

The USB bootstrap prepares a device-specific data image with a **user-specific public key**. Private keys remain on the computer. Only after boot and a successful SSH login may the desktop app enable mod management.

**No flashing code is included yet.** A future implementation must validate device identity, firmware, image integrity, backup/recovery and user consent before writing `/data`.

## Documentation

- `docs/JC11S_PROCESS.md`: earlier JC11S experiments and subsequent observed success
- `docs/BOOTSTRAP_SPEC.md`: unified installer design and safety gates

Never use JC11S Fastboot images or commands on JP24.
