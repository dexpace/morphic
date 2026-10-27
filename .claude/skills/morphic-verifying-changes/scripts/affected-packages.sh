#!/usr/bin/env bash
# Lists the packages a change can affect, for a fast `go test` while iterating: each package
# holding a changed file, each package whose code or tests import one of them, and
# internal/archtest whenever a Go or Markdown file changed. Prints ./... when the change reaches
# files any package may read (go.mod, root testdata, docs, the Makefile, lint config).
# The full gate still runs before a change is called done.
#
# Usage: go test $(bash affected-packages.sh [BASE])    BASE defaults to origin/main.
set -euo pipefail
export LC_ALL=C

root=$(git rev-parse --show-toplevel)
cd "$root"
base=${1:-origin/main}
mb=$(git merge-base "$base" HEAD) || { echo "affected-packages: no merge base with $base" >&2; exit 2; }
changed=$({ git diff --name-only "$mb"; git ls-files --others --exclude-standard; } | sort -u)
if [ -z "$changed" ]; then
  echo "affected-packages: nothing changed since $base" >&2
  exit 0
fi

if printf '%s\n' "$changed" | grep -qE '^(go\.(mod|sum)|testdata/|docs/|Makefile|\.golangci\.yml|\.github/)'; then
  echo ./...
  exit 0
fi

module=$(go list -m)
dirs=$(go list -f '{{.Dir}}' ./...)
hit=$(mktemp)
trap 'rm -f "$hit"' EXIT
while IFS= read -r f; do
  case "$f" in .claude/*) continue ;; esac
  case "$f" in *.go | *.md) echo "$module/internal/archtest" >>"$hit" ;; esac
  d=$(dirname "$root/$f")
  while [ "$d" != "$root" ] && [ "${#d}" -gt "${#root}" ]; do
    if printf '%s\n' "$dirs" | grep -qxF "$d"; then
      echo "$module${d#"$root"}" >>"$hit"
      break
    fi
    d=$(dirname "$d")
  done
done <<<"$changed"
sort -u -o "$hit" "$hit"
[ -s "$hit" ] || exit 0

# Every package whose own code, or a test variant of it, depends on a hit package.
go list -test -f '{{.ImportPath}} {{join .Deps " "}}' ./... |
  awk -v hits="$hit" '
    BEGIN { while ((getline p < hits) > 0) want[p] = 1 }
    {
      pkg = $1
      sub(/ \[.*$/, "", pkg); sub(/\.test$/, "", pkg); sub(/_test$/, "", pkg)
      for (i = 1; i <= NF; i++) if ($i in want) { print pkg; break }
    }' |
  sort -u
