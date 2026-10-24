#!/usr/bin/env bash
# Prints "<efficiency %> <wasted MB>" for an image, as dive measures it.
set -euo pipefail
out=$(mktemp)
trap 'rm -f "$out"' EXIT
dive --ci --json "$out" "$1" >/dev/null 2>&1
jq -r '.image | "\(.efficiencyScore * 10000 | floor / 100) \(.inefficientBytes / 1e5 | floor / 10)"' "$out"
