"""Render app icons (PNG/ICO + Android mipmaps) from www/img/*.svg with headless Chromium.
Usage: python3 scripts/render_icons.py     (needs: pip install playwright pillow; playwright install chromium)"""
import base64, io, os, pathlib
from playwright.sync_api import sync_playwright
from PIL import Image

ROOT = pathlib.Path(__file__).resolve().parent.parent
ICON = base64.b64encode((ROOT / "www/img/icon.svg").read_bytes()).decode()
MARK = base64.b64encode((ROOT / "www/img/mark.svg").read_bytes()).decode()

def page_html(size, kind):
    if kind == "icon":      inner = f'<img src="data:image/svg+xml;base64,{ICON}" style="width:{size}px;height:{size}px">'
    elif kind == "round":   inner = f'<img src="data:image/svg+xml;base64,{ICON}" style="width:{size}px;height:{size}px;border-radius:50%">'
    else:                   # adaptive foreground: mark on transparent canvas, inside the safe zone
        m = int(size * 0.62)
        inner = f'<img src="data:image/svg+xml;base64,{MARK}" style="position:absolute;left:{(size-m)//2}px;top:{(size-m)//2}px;width:{m}px;height:{m}px">'
    return f'<html><body style="margin:0;background:transparent;width:{size}px;height:{size}px;position:relative;overflow:hidden">{inner}</body></html>'

def render(pw_page, size, kind):
    pw_page.set_viewport_size({"width": size, "height": size})
    pw_page.set_content(page_html(size, kind))
    pw_page.wait_for_timeout(80)
    return pw_page.screenshot(omit_background=True, clip={"x": 0, "y": 0, "width": size, "height": size})

with sync_playwright() as p:
    b = p.chromium.launch(); pg = b.new_page()
    big = render(pg, 1024, "icon")
    (ROOT / "resources/icon-1024.png").write_bytes(big)
    png512 = render(pg, 512, "icon")
    (ROOT / "build/icon.png").write_bytes(png512)
    Image.open(io.BytesIO(png512)).save(ROOT / "build/icon.ico", sizes=[(16,16),(24,24),(32,32),(48,48),(64,64),(128,128),(256,256)])
    res = ROOT / "resources/android/res"
    for name, legacy, fg in [("mdpi",48,108),("hdpi",72,162),("xhdpi",96,216),("xxhdpi",144,324),("xxxhdpi",192,432)]:
        d = res / f"mipmap-{name}"; d.mkdir(parents=True, exist_ok=True)
        (d / "ic_launcher.png").write_bytes(render(pg, legacy, "icon"))
        (d / "ic_launcher_round.png").write_bytes(render(pg, legacy, "round"))
        (d / "ic_launcher_foreground.png").write_bytes(render(pg, fg, "fg"))
    v = res / "values"; v.mkdir(parents=True, exist_ok=True)
    (v / "ic_launcher_background.xml").write_text('<?xml version="1.0" encoding="utf-8"?>\n<resources>\n    <color name="ic_launcher_background">#0B0A14</color>\n</resources>\n')
    b.close()
print("icons rendered")
