# Prime 4+ (JC11S)

Status: SSH access has been demonstrated on a test device. Native Windows WinUSB communication is also confirmed for `getvar:version` returning `OKAY0.4` and an explicitly requested physical reboot. The Microsoft inbox WinUSB driver should be retained; no driver replacement or WSL2 bridge is required for those operations.

The desktop app now implements dynamic geometry probing, device-specific offline image preparation, Android sparse validation and the native Fastboot download/`flash:data` transport. Those implementation paths remain behind a separate closed hardware-release policy. VID:PID `15e4:d007`, `FF/42/03`, and a working Fastboot response are transport evidence, not sufficient authorization to write. Enabling the policy still requires an independently confirmed `data` partition mapping, fresh-filesystem boot/ALSA/SSH compatibility, and a tested recovery procedure.
