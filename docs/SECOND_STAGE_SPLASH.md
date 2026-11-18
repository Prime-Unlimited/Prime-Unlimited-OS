# Second-stage splash

Engine OS shows two different boot pictures.

1. The firmware `splash` partition is drawn by the bootloader before Linux starts. Prime Unlimited does not modify that partition, `updatesplash`, dm-verity, or any signed vendor image.
2. After Engine starts, Qt paints `Skins/Embedded/img/SplashScreen.png` from the QML resource tree. That is the second splash. `SplashScreen_16x9.png` is a different asset and is left alone.

The replacement principle is the same one EngineX uses for its UI-pack start screen: swap that one PNG when Qt registers resources, and keep the stock resource when the swap cannot be done safely. The implementation is the existing Prime Unlimited preload hook, not an EngineX UI pack, not a new process, and not a copied hook.

## Runtime contract

- File: `/data/prime-unlimited/ui/splash/SplashScreen.png`
- Picture: `assets/prime-unlimited-os-brand.png` stretched to 1280x800. The Prime 4+ framebuffer is 800x1280 and Engine rotates it 90 degrees, so the QML view is 1280x800. Stretch is non-uniform so the artwork touches every edge
- Layout: when the boot splash QML matches, its `FocusScope` and boot `Image` get `anchors.fill: parent` and that `Image` gets `fillMode: Image.Stretch`. A bottom-right `Text` reads `file:///data/prime-unlimited/os-version.qml`, the same file as the About headline, and shows `Prime Unlimited OS ` plus that `version` property. The overlay image, `SplashScreen_NoText`, and `SplashScreen_16x9.png` stay stock. A QML mismatch keeps the stock layout and still shows the 1280x800 PNG at its natural size
- Hook: `modloader/device/common/splash_resource.c`, called from the JC11S and JP24 `qRegisterResourceData` interceptors
- Missing file, bad PNG, wrong dimensions, or a failed relocation: the stock splash stays in place
- Removing the file and restarting Engine through the existing installer path restores the original picture
- JC11S (32-bit) may place a larger PNG anywhere in the process; the 32-bit resource offset wraps
- JP24 (aarch64) must place that PNG at a positive 32-bit offset below `0x80000000` from the resource data pointer. Qt 6.7 sign-extends the offset. A failed mapping keeps the stock splash instead of crashing Engine

An uncompressed replacement that already fits in the original qrc slot is written in place. A larger or compressed slot is relocated and the compression flags on that one node are cleared. This path rewrites that PNG node and, when the boot-splash QML matches, that one QML document.

The JP24 diagnostic hook (`DIAGNOSTIC_ONLY`, the `build-hook.sh` default) logs the candidate and does not change it. `PRIME_UI_HOOK_PATCH=1` is required before the JP24 swap is compiled in.

## Picture data

| Item | Value |
| --- | --- |
| Brand source | `assets/prime-unlimited-os-brand.png` |
| Brand git blob on `origin/main` | `77f2ed634b2187619f05b120c16ac503055b098d` |
| Brand pixels | 2560x1280 RGBA, 288168 bytes |
| Brand sha256 | `74836658fc46682b453b155f894a6093aa89148f8899385f373f48ab71d293e3` |
| Shipped splash | `modloader/device/common/splash/SplashScreen.png` |
| Shipped pixels | 1280x800 RGB, 141620 bytes, non-uniform LANCZOS stretch |
| Shipped sha256 | `516c16525a141db348f54be4fc55dbc54b8f26d00ace89cba4ca096f24352907` |
| OS version file | `VERSION` and `os/OSVersion.qml`, both `0.0.1` |
| About binding | `modloader/device/jc11s/ui-hook/Version.qml` loads `file:///data/prime-unlimited/os-version.qml` and shows `Prime Unlimited OS ` plus `version` |
| Splash label | same URL, same string, `anchors.right` and `anchors.bottom`, margin 18, color `#f5f2eb`, `font.pixelSize` 28 |

Regenerate the shipped PNG from the brand file:

```sh
python3 tools/splash/render_second_stage.py
```

The renderer refuses a missing brand file. It does not letterbox and it does not crop. A 2:1 banner on a 16:10 view is squashed on purpose so every screen pixel is brand artwork.

## Why the QML is patched

On the Engine build recorded below, the boot splash is a `Loader` with `id: splashscreenLoader` and `anchors.fill: parent`. The boot `Image` had only a `source` binding. It had no `anchors` and no `fillMode`, so Qt drew the PNG at its natural size. `SplashScreen_NoText` is the confirmation source of that same `Image`. The following `Image` with `id: overlay` is the internal/beta badge and is not restyled.

The resource slot for that QML is 2373 bytes. The hook inserts `anchors.fill: parent` on the `FocusScope`, `anchors.fill: parent` and `fillMode: Image.Stretch` on the boot `Image`, and the version `Loader` plus `Text`. It pays for those bytes by deleting the same number of leading spaces elsewhere in that one QML document, then updates the qrc stamp. The slot length does not change. If the QML pattern is absent, the hook leaves the document alone and the 1280x800 PNG still replaces `img/SplashScreen.png`.

`SplashScreen_16x9.png`, `SplashScreen_portrait.png`, and the multi-screen suffixes are different nodes. This path does not match them.

## Install and check

The device installer copies the PNG, the hook, `os-version`, and `os-version.qml` under `/data/prime-unlimited/`. It does not write `/dev/mmcblk*`, the `splash` partition, or `updatesplash`.

The JC11S check below did not run that full installer. It replaced two files with `mv` onto a new inode, so a running Engine kept the previous mapped hook until restart:

1. `/data/prime-unlimited/lib/libprime-ui-hook.so`
2. `/data/prime-unlimited/ui/splash/SplashScreen.png`

It then removed `/data/AppDataUser/AIR Music Technology/Engine/.cache/qml` and scheduled one `systemctl restart engine.service` through `systemd-run`. No second process was added. `LD_PRELOAD` stayed the existing hook path.

After the next Engine start, `/data/prime-unlimited/logs/ui-hook.log` must contain all of:

- `splash swap applied SplashScreen.png`
- `splash image stretched to parent`
- `splash version anchored to os-version.qml`
- `swap applied ControlCenterTab.qml timestamped`
- `swap applied Version.qml timestamped`
- `overlay host injected into EmbeddedMain.qml`

Removing `/data/prime-unlimited/ui/splash/SplashScreen.png` and restarting Engine once restores the stock picture. That delete-and-restore step is still open.

## JC11S hardware record, 2026-11-18

Prime 4+ (`JC11S`). Read-only display facts from the running unit:

- `/sys/class/graphics/fb0/modes` = `U:800x1280p-0`, name `rockchipdrmfb`
- DRM `card1-DSI-1` mode `800x1280`, status `connected`
- Engine child environment `AIR_SCREEN_ROTATION=90`
- The visible QML view is therefore 1280x800. `geometryTag` was empty on this boot, so Engine selected `img/SplashScreen.png`
- `/usr/Engine/Engine` is 47265736 bytes, mtime `Oct 30 11:41`, sha256 `68cb478d120d2793d5883876abe6eba49a3ca78b80abf041466113f93e1f0d58`
- The QML pattern above was taken from that binary

Files present during the accepted boot:

- Hook `/data/prime-unlimited/lib/libprime-ui-hook.so`, 13300 bytes, sha256 `fcf458f896603cae5bf394fa4ad387e2b5a69db858f87b933f3a7da03c4d9279`
- Splash PNG 141620 bytes, the shipped sha256 above
- `/data/prime-unlimited/os-version` = `0.0.1`

`engine.service` stayed `active` with `NRestarts=0`. The hook was mapped in the Engine child. The last start wrote `splash swap applied SplashScreen.png relocated`, `splash image stretched to parent`, and `splash version anchored to os-version.qml`, together with the Control Center, About, and overlay lines. On the device, the brand image filled the screen and the bottom-right text read the About version.

The firmware `splash` partition was not opened or written. The unit was not power-cycled, so the pre-Linux picture was not looked at again. JP24 was not part of this hardware check. A diagnostic JP24 build still logs the candidate and keeps the stock splash until `PRIME_UI_HOOK_PATCH=1`.

## Still open

1. Delete `/data/prime-unlimited/ui/splash/SplashScreen.png`, restart Engine once, and confirm the stock second splash returns.
2. Repeat the same check on JP24 with `PRIME_UI_HOOK_PATCH=1`.
3. Power-cycle a JC11S only if the pre-Linux picture itself needs to be compared. That check must not write the `splash` partition.
