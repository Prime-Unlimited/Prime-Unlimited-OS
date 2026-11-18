import hashlib
import subprocess
import sys
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
PNG = ROOT / "modloader" / "device" / "common" / "splash" / "SplashScreen.png"
SVG = ROOT / "assets" / "prime-unlimited-logo.svg"
RENDER = ROOT / "tools" / "splash" / "render_second_stage.py"
HOOK = ROOT / "modloader" / "device" / "common" / "splash_resource.c"


def test_committed_splash_is_the_engine_second_stage_size():
    image = Image.open(PNG)
    assert image.size == (1280, 800)
    assert image.format == "PNG"
    header = PNG.read_bytes()[:8]
    assert header == b"\x89PNG\r\n\x1a\n"
    assert 24 < PNG.stat().st_size <= 2 * 1024 * 1024


def test_renderer_uses_the_repository_logo_and_is_stable(tmp_path):
    source = RENDER.read_text()
    assert "assets" in source and "prime-unlimited-logo.svg" in source
    assert "1280" in source and "800" in source
    svg = SVG.read_text()
    assert "PRIME" in svg and "UNLIMITED" in svg
    rendered = tmp_path / "SplashScreen.png"
    subprocess.run([sys.executable, str(RENDER), str(rendered)], check=True)
    assert hashlib.sha256(rendered.read_bytes()).digest() == hashlib.sha256(PNG.read_bytes()).digest()


def test_host_resource_swap_covers_fallback_and_sxtw():
    binary = Path("/tmp/prime-splash-resource-host")
    subprocess.run(
        ["gcc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-DSPLASH_HOST_TEST", "-o", str(binary),
         str(ROOT / "tests" / "splash_resource_host.c"), str(HOOK)],
        check=True,
    )
    subprocess.run([str(binary), str(PNG)], check=True)
    source = HOOK.read_text()
    assert "0x20000000" in source
    assert "0x7fffffff" in source
    assert "stock splash kept" in source
    assert "/data/prime-unlimited/ui/splash/SplashScreen.png" in source
    assert "mmcblk" not in source
    assert "fastboot" not in source


def test_both_devices_wire_the_swap_without_a_new_service():
    for device in ("jc11s", "jp24"):
        hook = (ROOT / f"modloader/device/{device}/ui-hook/prime_ui_hook.c").read_text()
        build = (ROOT / f"modloader/device/{device}/ui-hook/build-hook.sh").read_text()
        installer = (ROOT / f"modloader/device/{device}/ui-hook/install-device.sh").read_text()
        verifier = (ROOT / f"modloader/device/{device}/ui-hook/verify-device.sh").read_text()
        assert "splash_visit(" in hook
        assert "splash_resource.c" in build
        assert 'cp "$SPLASH" "$PU/ui/splash/SplashScreen.png"' in installer
        assert "splash swap applied SplashScreen.png" in verifier
        assert "mmcblk" not in installer
        assert "fastboot" not in installer
        for forbidden in ("systemctl start prime-splash", "python", "node "):
            assert forbidden not in installer
