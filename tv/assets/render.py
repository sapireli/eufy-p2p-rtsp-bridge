"""Render the checked-in SVG sources to Android TV launcher resources.

Requires ImageMagick. Run from any directory: python3 tv/assets/render.py
The generated PNG files are checked in, so Android builds need no renderer.
"""

from pathlib import Path
import os
import subprocess

root = Path(__file__).resolve().parents[1]
resources = root / "app/src/main/res"
font = os.environ.get("ICON_FONT") or next(
    (str(path) for path in (
        Path("/System/Library/Fonts/Helvetica.ttc"),
        Path("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"),
    ) if path.exists()),
    None,
)
if font is None:
    raise SystemExit("Set ICON_FONT to a readable sans-serif font file")
densities = {
    "mdpi": (80, 160),
    "hdpi": (120, 240),
    "xhdpi": (160, 320),
    "xxhdpi": (240, 480),
    "xxxhdpi": (320, 640),
}

for density, (icon_size, banner_width) in densities.items():
    for source, size, destination in (
        ("icon.svg", f"{icon_size}x{icon_size}", resources / f"mipmap-{density}/ic_launcher.png"),
        ("banner.svg", f"{banner_width}x{banner_width * 9 // 16}", resources / f"drawable-{density}/banner.png"),
    ):
        destination.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(
            ["magick", "-font", font, "-background", "none", str(Path(__file__).parent / source),
             "-filter", "Lanczos", "-resize", size, "-strip", str(destination)],
            check=True,
        )
