#!/usr/bin/env bash
# Plants one defect, runs the tests, and restores the file whatever happens.
#
# Usage: bash mutate.sh FILE OLD NEW -- TEST-COMMAND...
#   OLD must occur in FILE exactly once, read literally; NEW replaces it.
#   TEST-COMMAND runs from the current directory, e.g. go test ./ir/...
# Prints KILLED, SURVIVED or UNVERIFIED, then every failing test line.
# Exit 0 KILLED, 1 SURVIVED, 2 UNVERIFIED or a usage error.
set -euo pipefail
export LC_ALL=C

usage() {
  echo "usage: bash mutate.sh FILE OLD NEW -- TEST-COMMAND..." >&2
  exit 2
}
[ "$#" -ge 5 ] && [ "$4" = "--" ] || usage
[ -f "$1" ] || { echo "mutate: $1 is not a file" >&2; exit 2; }
command -v perl >/dev/null || { echo "mutate: perl is required" >&2; exit 2; }

file="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
old=$2
new=$3
shift 4
root=$(git -C "$(dirname "$file")" rev-parse --show-toplevel)

count=$(OLD="$old" perl -0777 -ne 'BEGIN { $o = $ENV{OLD} } $n = () = /\Q$o\E/g; print $n' "$file")
[ "$count" = 1 ] || { echo "mutate: OLD occurs $count times in $file; widen it until it is unique" >&2; exit 2; }

backup=$(mktemp)
log=$(mktemp)
cp "$file" "$backup"
restore() {
  cp "$backup" "$file"
  cmp -s "$backup" "$file" || echo "mutate: RESTORE FAILED, $file still holds the mutant" >&2
  rm -f "$backup" "$log"
}
trap restore EXIT
trap 'exit 2' INT TERM HUP

OLD="$old" NEW="$new" perl -0777 -pi -e 'BEGIN { $o = $ENV{OLD}; $r = $ENV{NEW} } s/\Q$o\E/$r/' "$file"
if cmp -s "$backup" "$file"; then
  echo "mutate: the replacement changed nothing" >&2
  exit 2
fi

case "$file" in
  *.go)
    pkg="./${file#"$root"/}"
    pkg=${pkg%/*}
    if ! (cd "$root" && go test -count=1 -run '^$' "$pkg") >"$log" 2>&1; then
      echo "UNVERIFIED: the mutant does not build or vet; adjust it until it compiles"
      sed -n '1,15p' "$log"
      exit 2
    fi
    ;;
esac

status=0
"$@" >"$log" 2>&1 || status=$?
fails=$(grep -E '^[[:space:]]*--- FAIL' "$log" || true)
oks=$(grep -cE '^ok[[:space:]]' "$log" || true)

if grep -qE '\[build failed\]|\[setup failed\]|^# ' "$log"; then
  echo "UNVERIFIED: a package did not build, so no verdict"
  grep -E '\[build failed\]|\[setup failed\]|^# ' "$log" | sed -n '1,10p'
  exit 2
fi
if [ "$status" -eq 0 ]; then
  if [ "$oks" -gt 0 ]; then
    echo "SURVIVED: $oks package(s) passed with the mutant in place"
    exit 1
  fi
  echo "UNVERIFIED: the command passed but no test package reported ok"
  exit 2
fi
if [ -z "$fails" ]; then
  echo "UNVERIFIED: the command failed without a failing test; read its output"
  sed -n '1,15p' "$log"
  exit 2
fi
echo "KILLED"
printf '%s\n' "$fails"
if grep -q '^panic:' "$log"; then
  echo "note: a panic ended the run; run the test written for this property alone to see it pin"
fi
exit 0
