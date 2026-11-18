#!/usr/bin/env python3
"""Rasterise assets/prime-unlimited-logo.svg into Engine's 1280x800 second splash.

Engine paints this PNG from the QML resource Skins/Embedded/img/SplashScreen.png
after Linux and Qt are up. It is not the firmware splash partition.
"""

import ctypes
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SVG = ROOT / "assets" / "prime-unlimited-logo.svg"
DEFAULT_OUT = ROOT / "modloader" / "device" / "common" / "splash" / "SplashScreen.png"
WIDTH = 1280
HEIGHT = 800


def _svg_size(path):
    root = ET.parse(path).getroot()
    return float(root.attrib["width"]), float(root.attrib["height"])


def render(svg_path=SVG, out_path=DEFAULT_OUT):
    from PIL import Image

    svg_path = Path(svg_path)
    out_path = Path(out_path)
    if not svg_path.is_file():
        raise SystemExit(f"logo SVG missing: {svg_path}")
    svg_w, svg_h = _svg_size(svg_path)
    rsvg = ctypes.CDLL("librsvg-2.so.2")
    cairo = ctypes.CDLL("libcairo.so.2")

    rsvg.rsvg_handle_new_from_file.argtypes = [ctypes.c_char_p, ctypes.c_void_p]
    rsvg.rsvg_handle_new_from_file.restype = ctypes.c_void_p
    rsvg.rsvg_handle_render_cairo.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
    rsvg.rsvg_handle_render_cairo.restype = ctypes.c_int

    cairo.cairo_image_surface_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int]
    cairo.cairo_image_surface_create.restype = ctypes.c_void_p
    cairo.cairo_create.argtypes = [ctypes.c_void_p]
    cairo.cairo_create.restype = ctypes.c_void_p
    cairo.cairo_translate.argtypes = [ctypes.c_void_p, ctypes.c_double, ctypes.c_double]
    cairo.cairo_scale.argtypes = [ctypes.c_void_p, ctypes.c_double, ctypes.c_double]
    cairo.cairo_pattern_create_linear.argtypes = [ctypes.c_double, ctypes.c_double, ctypes.c_double, ctypes.c_double]
    cairo.cairo_pattern_create_linear.restype = ctypes.c_void_p
    cairo.cairo_pattern_add_color_stop_rgb.argtypes = [
        ctypes.c_void_p, ctypes.c_double, ctypes.c_double, ctypes.c_double, ctypes.c_double
    ]
    cairo.cairo_set_source.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
    cairo.cairo_paint.argtypes = [ctypes.c_void_p]
    cairo.cairo_surface_flush.argtypes = [ctypes.c_void_p]
    cairo.cairo_image_surface_get_data.argtypes = [ctypes.c_void_p]
    cairo.cairo_image_surface_get_data.restype = ctypes.POINTER(ctypes.c_ubyte)
    cairo.cairo_image_surface_get_stride.argtypes = [ctypes.c_void_p]
    cairo.cairo_image_surface_get_stride.restype = ctypes.c_int
    cairo.cairo_status.argtypes = [ctypes.c_void_p]
    cairo.cairo_status.restype = ctypes.c_int

    handle = rsvg.rsvg_handle_new_from_file(str(svg_path).encode(), None)
    if not handle:
        raise SystemExit(f"librsvg could not open {svg_path}")
    surface = cairo.cairo_image_surface_create(0, WIDTH, HEIGHT)  # ARGB32
    if not surface:
        raise SystemExit("cairo surface failed")
    context = cairo.cairo_create(surface)
    if cairo.cairo_status(context) != 0:
        raise SystemExit("cairo context failed")

    pattern = cairo.cairo_pattern_create_linear(0, 0, WIDTH, HEIGHT)
    cairo.cairo_pattern_add_color_stop_rgb(pattern, 0, 7 / 255, 12 / 255, 22 / 255)
    cairo.cairo_pattern_add_color_stop_rgb(pattern, 1, 16 / 255, 35 / 255, 60 / 255)
    cairo.cairo_set_source(context, pattern)
    cairo.cairo_paint(context)

    scale = (WIDTH - 80) / svg_w
    draw_w = svg_w * scale
    draw_h = svg_h * scale
    cairo.cairo_translate(context, (WIDTH - draw_w) / 2, (HEIGHT - draw_h) / 2)
    cairo.cairo_scale(context, scale, scale)
    if not rsvg.rsvg_handle_render_cairo(handle, context):
        raise SystemExit("librsvg render failed")
    cairo.cairo_surface_flush(surface)

    stride = cairo.cairo_image_surface_get_stride(surface)
    raw = cairo.cairo_image_surface_get_data(surface)
    image = Image.new("RGB", (WIDTH, HEIGHT))
    pixels = image.load()
    for y in range(HEIGHT):
        row = y * stride
        for x in range(WIDTH):
            blue = raw[row + x * 4]
            green = raw[row + x * 4 + 1]
            red = raw[row + x * 4 + 2]
            alpha = raw[row + x * 4 + 3]
            if alpha == 0:
                pixels[x, y] = (0, 0, 0)
            elif alpha == 255:
                pixels[x, y] = (red, green, blue)
            else:
                pixels[x, y] = (
                    min(255, red * 255 // alpha),
                    min(255, green * 255 // alpha),
                    min(255, blue * 255 // alpha),
                )
    out_path.parent.mkdir(parents=True, exist_ok=True)
    image.save(out_path, format="PNG", compress_level=9)
    return out_path


def main(argv):
    out = Path(argv[1]) if len(argv) > 1 else DEFAULT_OUT
    written = render(out_path=out)
    print(written)


if __name__ == "__main__":
    main(sys.argv)
