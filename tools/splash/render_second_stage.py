#!/usr/bin/env python3
"""Stretch assets/prime-unlimited-os-brand.png onto Engine's 1280x800 second splash.

Engine paints this PNG from the QML resource Skins/Embedded/img/SplashScreen.png
after Linux and Qt are up. It is not the firmware splash partition.

The brand asset is 2560x1280. The Prime 4+ panel is 800x1280 and Engine rotates
it 90 degrees, so the QML view is 1280x800. Resize is non-uniform on purpose:
the picture is stretched to the full view, not letterboxed.
"""

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BRAND = ROOT / "assets" / "prime-unlimited-os-brand.png"
DEFAULT_OUT = ROOT / "modloader" / "device" / "common" / "splash" / "SplashScreen.png"
WIDTH = 1280
HEIGHT = 800


def render(brand_path=BRAND, out_path=DEFAULT_OUT):
    from PIL import Image

    brand_path = Path(brand_path)
    out_path = Path(out_path)
    if not brand_path.is_file():
        raise SystemExit(f"brand PNG missing: {brand_path}")
    image = Image.open(brand_path).convert("RGB")
    stretched = image.resize((WIDTH, HEIGHT), Image.Resampling.LANCZOS)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    stretched.save(out_path, format="PNG", compress_level=9)
    return out_path


def main(argv):
    out = Path(argv[1]) if len(argv) > 1 else DEFAULT_OUT
    written = render(out_path=out)
    print(written)


if __name__ == "__main__":
    main(sys.argv)
