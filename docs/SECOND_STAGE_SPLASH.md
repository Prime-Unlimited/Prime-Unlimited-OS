# Second-stage splash

Engine OS shows two different boot pictures.

1. The firmware `splash` partition is drawn by the bootloader before Linux starts. Prime Unlimited does not modify that partition, `updatesplash`, dm-verity, or any signed vendor image.
2. After Engine starts, Qt paints `Skins/Embedded/img/SplashScreen.png` from the QML resource tree. That is the second splash. `SplashScreen_16x9.png` is a different asset and is left alone.

The replacement principle is the same one EngineX uses for its UI-pack start screen: swap that one PNG when Qt registers resources, and keep the stock resource when the swap cannot be done safely. The implementation is the existing Prime Unlimited preload hook, not an EngineX UI pack, not a new process, and not a copied hook.

## Runtime contract

- File: `/data/prime-unlimited/ui/splash/SplashScreen.png`
- Picture: 1280x800 PNG rasterised from `assets/prime-unlimited-logo.svg`
- Hook: `modloader/device/common/splash_resource.c`, called from the JC11S and JP24 `qRegisterResourceData` interceptors
- Missing file, bad PNG, wrong dimensions, or a failed relocation: the stock splash stays in place
- Removing the file and restarting Engine through the existing installer path restores the original picture
- JC11S (32-bit) may place a larger PNG anywhere in the process; the 32-bit resource offset wraps
- JP24 (aarch64) must place that PNG at a positive 32-bit offset below `0x80000000` from the resource data pointer. Qt 6.7 sign-extends the offset. A failed mapping keeps the stock splash instead of crashing Engine

An uncompressed replacement that already fits in the original qrc slot is written in place. A larger or compressed slot is relocated and the compression flags on that one node are cleared. No other resource is rewritten by this path.

The JP24 diagnostic hook (`DIAGNOSTIC_ONLY`, the `build-hook.sh` default) logs the candidate and does not change it. `PRIME_UI_HOOK_PATCH=1` is required before the JP24 swap is compiled in.

## Hardware still required

This branch does not install or restart anything on a Prime. On JC11S and JP24, after an approved install:

1. Confirm the log contains `splash swap applied SplashScreen.png`.
2. Confirm the picture on screen is the Prime Unlimited logo, while the pre-Linux splash partition picture is unchanged.
3. Delete `/data/prime-unlimited/ui/splash/SplashScreen.png`, restart Engine once, and confirm the stock Engine splash returns.
4. Confirm Control Center, About, and the overlay host still behave as before.
