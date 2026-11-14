# Prime 4+ (JC11S)

Status: SSH access has been demonstrated on a test device. Native Windows WinUSB communication is also confirmed for `getvar:version` returning `OKAY0.4` and an explicitly requested physical reboot. The Microsoft inbox WinUSB driver should be retained; no driver replacement or WSL2 bridge is required for those operations.

The desktop app uses the native transport only for diagnostics and confirmed reboot. VID:PID `15e4:d007`, `FF/42/03`, and a working Fastboot response are transport evidence, not sufficient authorization to flash. Before enabling flashing, document positive model identity, partition layout, firmware compatibility, tested payload, and recovery procedure.
