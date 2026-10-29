#!/usr/bin/env bash
# The installed app's PNG icons, made from icons/app-icon.svg with macOS's sips. Run again
# when the icon changes, and commit the PNGs.
set -euo pipefail
cd "$(dirname "$0")/.."
for spec in 180:apple-touch-icon 192:icon-192 512:icon-512; do
  size=${spec%%:*}
  sips -s format png -z "$size" "$size" icons/app-icon.svg --out "public/${spec#*:}.png" >/dev/null
done
