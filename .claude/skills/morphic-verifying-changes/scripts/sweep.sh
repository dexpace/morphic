#!/usr/bin/env bash
# Compiles every spec in a corpus with cmd/morphic built at a base revision and at the working
# tree, and reports each spec whose exit code, IR or diagnostics differ.
#
# Usage: bash sweep.sh [-b BASE] [-t SECONDS] [-o OUTDIR] [CORPUS-DIR...] [-- COMPILE-FLAGS...]
#   BASE      revision to compare against (default origin/main)
#   SECONDS   per-compile limit; a compile past it is killed and recorded as 137 (default 60)
#   OUTDIR    where binaries and outputs are kept for diffing (default a new temporary dir)
#   CORPUS    directories searched for OpenAPI and Swagger documents (default testdata)
# Exit 0 when nothing differs, 1 when something does, 2 when the sweep cannot run.
set -euo pipefail
export LC_ALL=C

usage() {
  sed -n '5,10p' "$0" >&2
  exit 2
}
base=origin/main
limit=60
out=
while getopts 'b:t:o:h' opt; do
  case "$opt" in
    b) base=$OPTARG ;;
    t) limit=$OPTARG ;;
    o) out=$OPTARG ;;
    *) usage ;;
  esac
done
shift $((OPTIND - 1))
corpus=()
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do
  corpus+=("$(cd "$1" && pwd)")
  shift
done
[ "$#" -gt 0 ] && shift
flags=("$@")
case "$limit" in '' | *[!0-9]*) echo "sweep: -t takes whole seconds" >&2; exit 2 ;; esac
command -v perl >/dev/null || { echo "sweep: perl is required" >&2; exit 2; }

root=$(git rev-parse --show-toplevel)
sha=$(git -C "$root" rev-parse --verify --quiet "$base^{commit}") || { echo "sweep: no revision $base" >&2; exit 2; }
[ "${#corpus[@]}" -gt 0 ] || corpus=("$root/testdata")
[ -n "$out" ] || out=$(mktemp -d)
mkdir -p "$out"
out=$(cd "$out" && pwd)
src="$out/base-src"
cleanup() { git -C "$root" worktree remove --force "$src" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "sweep: building head from the working tree and base at ${sha:0:12}"
(cd "$root" && go build -o "$out/head" ./cmd/morphic)
git -C "$root" worktree add --detach --quiet "$src" "$sha"
(cd "$src" && go build -o "$out/base" ./cmd/morphic)
cleanup

specs="$out/specs.txt"
find "${corpus[@]}" -type f \( -name '*.yaml' -o -name '*.yml' -o -name '*.json' \) -print0 |
  xargs -0 grep -lE '^[[:space:]]*"?(openapi|swagger)"?[[:space:]]*:' | sort >"$specs" || true
total=$(wc -l <"$specs" | tr -d ' ')
[ "$total" -gt 0 ] || { echo "sweep: no OpenAPI or Swagger documents under ${corpus[*]}" >&2; exit 2; }

# run BIN SPEC PREFIX: compile under a kill-on-timeout parent; print the exit status.
run() {
  local status=0
  perl -e '$t = shift; $p = fork; if (!$p) { exec @ARGV; exit 127 }
    $SIG{ALRM} = sub { kill 9, $p }; alarm $t; waitpid($p, 0);
    exit($? & 127 ? 128 + ($? & 127) : $? >> 8)' \
    "$limit" "$1" compile ${flags[@]+"${flags[@]}"} -- "$2" >"$3.ir" 2>"$3.diag" || status=$?
  printf '%s' "$status"
}

results="$out/results.txt"
: >"$results"
i=0
while IFS= read -r spec; do
  i=$((i + 1))
  p="$out/runs/$i"
  mkdir -p "$p"
  b=$(run "$out/base" "$spec" "$p/base")
  h=$(run "$out/head" "$spec" "$p/head")
  what=
  [ "$b" = "$h" ] || what="$what exit($b->$h)"
  cmp -s "$p/base.ir" "$p/head.ir" || what="$what ir"
  cmp -s "$p/base.diag" "$p/head.diag" || what="$what diagnostics"
  printf '%s\t%s\t%s\t%s\t%s\n' "$i" "$b" "$h" "${spec#"$root"/}" "${what# }" >>"$results"
done <"$specs"

echo "sweep: $total specs from ${corpus[*]#"$root"/}; outputs under $out/runs/<n>/"
for side in base head; do
  col=2
  [ "$side" = head ] && col=3
  printf '  %s exit codes:' "$side"
  cut -f"$col" "$results" | sort | uniq -c | awk '{ printf " %s×%s", $2, $1 }'
  printf '; IR bytes: %s\n' "$(cat "$out"/runs/*/"$side".ir | wc -c | tr -d ' ')"
done
differ=$(awk -F'\t' '$5 != ""' "$results" | wc -l | tr -d ' ')
if [ "$differ" -eq 0 ]; then
  echo "sweep: no spec differs"
  exit 0
fi
echo "sweep: $differ of $total specs differ (n, then what):"
awk -F'\t' '$5 != "" { printf "  %s  %s: %s\n", $1, $4, $5 }' "$results"
echo "compare one with: diff $out/runs/<n>/base.diag $out/runs/<n>/head.diag"
exit 1
