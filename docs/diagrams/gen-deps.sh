#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
MODULE=$(go list -m)

{
  echo "digraph deps {"
  echo "  rankdir=LR;"
  echo "  node [shape=box, style=filled, fillcolor=\"#f3f4f6\"];"
  go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
    | while read -r line; do
        pkg=$(echo "$line" | awk '{print $1}')
        short=$(echo "$pkg" | sed "s|$MODULE/||")
        for dep in $(echo "$line" | cut -d' ' -f2-); do
          if [[ "$dep" == "$MODULE"/* ]]; then
            dep_short=$(echo "$dep" | sed "s|$MODULE/||")
            echo "  \"$short\" -> \"$dep_short\";"
          fi
        done
      done
  echo "}"
} > docs/diagrams/package-deps.dot
