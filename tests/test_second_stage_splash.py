import hashlib
import subprocess
import sys
from pathlib import Path

import pytest
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
PNG = ROOT / "modloader" / "device" / "common" / "splash" / "SplashScreen.png"
BRAND = ROOT / "assets" / "prime-unlimited-os-brand.png"
RENDER = ROOT / "tools" / "splash" / "render_second_stage.py"
HOOK = ROOT / "modloader" / "device" / "common" / "splash_resource.c"


def test_committed_splash_is_the_engine_second_stage_size():
    image = Image.open(PNG)
    assert image.size == (1280, 800)
    assert image.format == "PNG"
    header = PNG.read_bytes()[:8]
    assert header == b"\x89PNG\r\n\x1a\n"
    assert 24 < PNG.stat().st_size <= 2 * 1024 * 1024


def test_renderer_stretches_the_brand_image_to_the_runtime_geometry(tmp_path):
    source = RENDER.read_text()
    assert "assets" in source and "prime-unlimited-os-brand.png" in source
    assert "1280" in source and "800" in source
    assert "letterbox" in source
    brand = Image.open(BRAND)
    assert brand.size == (2560, 1280)
    rendered = tmp_path / "SplashScreen.png"
    subprocess.run([sys.executable, str(RENDER), str(rendered)], check=True)
    assert Image.open(rendered).size == (1280, 800)
    expected = "516c16525a141db348f54be4fc55dbc54b8f26d00ace89cba4ca096f24352907"
    assert hashlib.sha256(PNG.read_bytes()).hexdigest() == expected


@pytest.mark.skipif(sys.platform != "linux", reason="resource hook uses Linux mmap semantics")
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
    assert "splash image stretched to parent" in source
    assert "splash version anchored to os-version.qml" in source
    assert "fillMode: Image.Stretch" in source
    about = (ROOT / "modloader/device/jc11s/ui-hook/Version.qml").read_text()
    assert "file:///data/prime-unlimited/os-version.qml" in about
    assert "file:///data/prime-unlimited/os-version.qml" in source
    assert "Prime Unlimited OS " in about
    assert "Prime Unlimited OS " in source
    assert "/data/prime-unlimited/ui/splash/SplashScreen.png" in source
    assert "mmcblk" not in source
    assert "fastboot" not in source


def test_jc11s_wires_the_swap_without_a_new_service():
    device = "jc11s"
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
