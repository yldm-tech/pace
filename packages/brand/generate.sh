#!/usr/bin/env bash
# Regenerates every brand raster in the repository from packages/brand/mark.svg.
#
# Run it from the repository root after changing the mark, so that no icon is a binary nobody can reproduce:
#   bash packages/brand/generate.sh
#
# Needs rsvg-convert (librsvg) and magick (ImageMagick 7).

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
brand="$root/packages/brand"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

BLUE="#3f76ff"
# The glyph covers this share of a tile's width, which leaves it air on every side at small sizes.
COVERAGE="0.62"
# What Apple and Android both round a 512px tile to.
RADIUS="115"

require() { command -v "$1" >/dev/null || { echo "missing $1" >&2; exit 1; }; }
require rsvg-convert
require magick

# tile writes one square app icon: the white mark centred on a rounded brand-blue square.
tile() {
  local size="$1" out="$2"
  python3 - "$brand/mark.svg" "$COVERAGE" "$RADIUS" > "$work/tile.svg" <<'PY'
import re, sys
mark, coverage, radius = sys.argv[1], float(sys.argv[2]), float(sys.argv[3])
source = open(mark).read()
path = re.search(r"<path\b.*?/>", source, re.S).group(0).replace('fill="currentColor"', "")
view_width = float(re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', source).group(1))
view_height = float(re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', source).group(2))
size = 512.0
height = size * coverage
scale = height / view_height
x = (size - view_width * scale) / 2
y = (size - height) / 2
print(
    f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">'
    f'<rect width="512" height="512" rx="{radius}" ry="{radius}" fill="#3f76ff"/>'
    f'<g transform="translate({x:.3f} {y:.3f}) scale({scale:.5f})" fill="#ffffff">{path}</g>'
    f"</svg>"
)
PY
  rsvg-convert -w "$size" -h "$size" "$work/tile.svg" -o "$out"
}

# One 512px master that every raster below is derived from, so they cannot drift apart.
tile 512 "$work/master-512.png"

for app in web space admin; do
  assets="$root/apps/$app/core/assets/favicon"
  public="$root/apps/$app/public/favicon"
  mkdir -p "$assets" "$public"

  tile 16 "$work/16.png"
  tile 32 "$work/32.png"
  tile 48 "$work/48.png"
  cp "$work/16.png" "$assets/favicon-16x16.png"
  cp "$work/32.png" "$assets/favicon-32x32.png"
  magick "$work/16.png" "$work/32.png" "$work/48.png" "$assets/favicon.ico"

  tile 180 "$assets/apple-touch-icon.png"
  tile 192 "$public/android-chrome-192x192.png"
  cp "$work/master-512.png" "$public/android-chrome-512x512.png"
done

# web keeps a second copy of the two sizes its manifest and apple-touch links point at.
tile 180 "$root/apps/web/core/assets/icons/icon-180x180.png"
cp "$work/master-512.png" "$root/apps/web/core/assets/icons/icon-512x512.png"

# The "instance not ready" screen shows the mark twice: once solid over a gradient wash, once as the wash itself.
gradient() {
  local width="$1" height="$2" opacity="$3" out="$4"
  python3 - "$brand/mark.svg" "$width" "$height" "$opacity" > "$work/gradient.svg" <<'PY'
import re, sys
mark, width, height, opacity = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), sys.argv[4]
source = open(mark).read()
path = re.search(r"<path\b.*?/>", source, re.S).group(0).replace('fill="currentColor"', "")
view_width = float(re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', source).group(1))
view_height = float(re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', source).group(2))
scale = (height * 0.82) / view_height
x = (width - view_width * scale) / 2
y = (height - view_height * scale) / 2
print(
    f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width:.0f} {height:.0f}">'
    f'<defs><linearGradient id="wash" x1="0" y1="0" x2="1" y2="1">'
    f'<stop offset="0" stop-color="#6f9bff"/><stop offset="1" stop-color="#2b53c8"/>'
    f"</linearGradient></defs>"
    f'<g transform="translate({x:.3f} {y:.3f}) scale({scale:.5f})" fill="url(#wash)" opacity="{opacity}">{path}</g>'
    f"</svg>"
)
PY
  rsvg-convert -w "$width" -h "$height" "$work/gradient.svg" -o "$work/gradient.png"
  magick "$work/gradient.png" -define webp:lossless=true "$out"
}

gradient 561 312 1 "$root/apps/web/core/assets/auth/gradient-logo.webp"
gradient 1080 672 0.35 "$root/apps/web/core/assets/auth/gradient-bg-logo.webp"

# Storybook reads its brand image from propel's public directory rather than from a package it depends on, so that one copy is written here instead of being kept in step by hand.
cp "$brand/lockup.svg" "$root/packages/propel/public/pace-lockup-light.svg"

echo "brand assets regenerated from packages/brand/mark.svg"
