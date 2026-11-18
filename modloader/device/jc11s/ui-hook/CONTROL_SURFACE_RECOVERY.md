# JC11S control-surface recovery

Verified on 2026-11-18 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Symptom and cause

Touch input and Engine remained operational, but all physical buttons, faders,
encoders and jog wheels stopped responding. USB enumeration still showed the
Prime 4 audio device, control surface and both wheel displays.

Engine logged that ALSA could not load `prime_ui_hook` from
`libprime-ui-hook.so`, followed by failure to open the ALSA sequencer. A legacy
development file at `/var/lib/alsa/conf.d/99-prime-ui.conf`, backed by
`/data/system/var/overlay/lib/alsa/conf.d/99-prime-ui.conf`, incorrectly treated
the Qt resource interposer as an ALSA configuration hook.

The current JC11S installer image does not contain that file. Its deterministic
image manifest includes only `99-prime-ssh.conf`, which loads the independent
`libprime-ssh.so` bootstrap hook with the required ALSA compatibility symbols.

## Recovery verification

The legacy file was copied to
`/data/prime-unlimited/backups/control-surface-recovery-v1/` and removed from
the active ALSA configuration. After a controlled Engine restart, Engine held
multiple `/dev/snd/seq` descriptors and the owner confirmed that buttons,
faders, encoders and jog wheels worked again. The Mod Loader, all of its touch
events and the UI preload hook remained operational.

The activation script now recognizes and quarantines only this exact legacy
configuration in either historical persistent ALSA path. It never removes or
changes `99-prime-ssh.conf`.
