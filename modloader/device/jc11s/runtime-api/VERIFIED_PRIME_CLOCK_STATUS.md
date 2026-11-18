# Prime Clock hardware verification

Verified on 2026-11-18 on a Denon DJ Prime 4+ reporting `JC11S`, `armv7l`,
Engine OS 5.3.0 and the Modloader runtime identity `gnueabihf`.

The signed schema-v2 catalog and release package completed the normal cache,
SHA-256 verification, secure stage, inspection and atomic disabled-install path.
The registry preserved the declared overlay and shell entrypoint. Manual Launch
completed with exit code zero without creating another UI instance.

Observed on the physical display:

- one clock overlay rendered inside the normal Engine interface;
- clock/stopwatch touch interaction worked;
- long-press drag followed the finger smoothly after conversion to stable screen
  coordinates and remained inside the display;
- Disable removed the overlay and Enable restored it dynamically;
- Install and Uninstall completed through the Modloader UI;
- the generic host restricted loading to the main screen, eliminating duplicate
  overlay instances;
- hardware buttons and encoders remained operational.

During every controlled Engine restart, the Engine process returned active and
held open `/dev/snd/seq` descriptors. The UI hook was not installed as an ALSA
hook. Safe Mode behavior had already been hardware-verified for the Modloader;
the generic `/api/v1/overlays` path returns an empty set while Safe Mode is on.

JP24 remains build/package-validated only. No JP24 Modloader or hardware result
is claimed.
