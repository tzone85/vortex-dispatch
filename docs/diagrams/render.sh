#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

echo "=== Rendering d2 diagrams ==="
for f in *.d2; do
  [ -f "$f" ] || continue
  out="${f%.d2}.svg"
  echo "  $f → $out"
  d2 --layout=elk "$f" "$out"
done

echo "=== Rendering plantuml diagrams ==="
for f in *.puml; do
  [ -f "$f" ] || continue
  echo "  $f → ${f%.puml}.png"
  plantuml "$f"
done

echo "=== Rendering graphviz diagrams ==="
for f in *.dot; do
  [ -f "$f" ] || continue
  out="${f%.dot}.svg"
  echo "  $f → $out"
  dot -Tsvg "$f" -o "$out"
done

echo "All diagrams rendered."
