#!/usr/bin/env bash
# Scans what a change ships for text that must not land: assistant attribution, words from the
# working session (a brief, a coordinator, a scratch directory), paths on one machine, and
# audit-finding labels. Reads the lines the change adds, its commit messages and, with --pr, the
# PR's title and body. Every hit is printed for a person to judge.
#
# Usage: bash artifact-scan.sh [BASE [HEAD]] [--pr N]    BASE origin/main, HEAD HEAD by default.
# Exit 0 when clean, 1 on a hit, 2 when it cannot run.
set -euo pipefail
export LC_ALL=C

pr=
args=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pr) pr=${2:?--pr takes a PR number}; shift 2 ;;
    -h | --help) sed -n '2,9p' "$0" >&2; exit 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
base=${args[0]:-origin/main}
head=${args[1]:-HEAD}
git rev-parse --verify --quiet "$base^{commit}" >/dev/null || { echo "artifact-scan: no revision $base" >&2; exit 2; }
git rev-parse --verify --quiet "$head^{commit}" >/dev/null || { echo "artifact-scan: no revision $head" >&2; exit 2; }

# Split so this file does not match its own patterns.
patterns=(
  'Co-Authored-By:.*(Cl''aude|[Aa]nthr''opic)|Generated with|noreply@anthr''opic'
  '(^|[^A-Za-z.])(Cl''aude|Anthr''opic|ChatG''PT)([^A-Za-z]|$)'
  '(the|this|your|per the) bri''ef([^A-Za-z]|$)|\(bri''ef|bri''ef:'
  '(^|[^A-Za-z])(coordi''nator|implem''enter|sub''agent|scratch''pad)([^A-Za-z]|$)'
  '/U''sers/|/h''ome/[a-z]|/p''rivate/(tmp|var)'
  'remedi''ation|audit sw''eep|(^|[^A-Za-z0-9])[HM][0-9]{1,2}([^0-9A-Za-z]|$)'
)
re=$(IFS='|'; printf '%s' "${patterns[*]}")

hits=0
scan() { # LABEL: reads lines on stdin as "where<TAB>text"
  while IFS=$'\t' read -r where text; do
    if printf '%s\n' "$text" | grep -qE "$re"; then
      printf '%s: %s: %s\n' "$1" "$where" "$text"
      hits=$((hits + 1))
    fi
  done
}

self=.claude/skills/morphic-reviewing-prs/scripts/artifact-scan.sh
scan "added line" < <(git diff -U0 "$base...$head" -- . ":(exclude)$self" |
  awk '/^\+\+\+ / { f = substr($0, 7); next }
       /^@@/ { split($3, a, ","); n = substr(a[1], 2) + 0; next }
       /^\+/ { printf "%s:%d\t%s\n", f, n, substr($0, 2); n++ }')
scan "commit" < <(git rev-list --abbrev-commit "$base..$head" | while IFS= read -r sha; do
  git log -1 --format=%B "$sha" | awk -v sha="$sha" 'NF { printf "%s\t%s\n", sha, $0 }'
done)
if [ -n "$pr" ]; then
  command -v gh >/dev/null || { echo "artifact-scan: --pr needs gh" >&2; exit 2; }
  scan "PR #$pr" < <(gh pr view "$pr" --json title,body --jq '"title\t" + .title, (.body | split("\n")[] | "body\t" + .)')
fi

if [ "$hits" -gt 0 ]; then
  echo "artifact-scan: $hits line(s) to judge"
  exit 1
fi
echo "artifact-scan: clean"
