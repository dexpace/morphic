#!/usr/bin/env bash
#
# verify-atomic-output.sh checks, against a real filesystem, that `morphic
# compile -o` publishes its output atomically.
#
# The unit tests in cmd/morphic cover these paths by injecting failures through
# package vars. This script is the end-to-end counterpart: it drives the built
# binary, fails the write for real, and inspects what is left on disk. The two
# answer different questions — the unit tests ask whether the code takes the
# right branch, this asks whether the destination actually survives.
#
# It also pins the three behaviours that publishing-by-rename gives up, so a
# later change cannot alter them without a test going red. See replaceFile in
# cmd/morphic/compile.go, where the same three are recorded.
#
# Usage:
#   scripts/verify-atomic-output.sh                     # check the working tree
#   scripts/verify-atomic-output.sh --baseline <ref>    # and contrast with <ref>
#
# --baseline additionally builds the binary at <git-ref> and runs the atomicity
# case against it, to demonstrate that the case reaches what it claims: at a ref
# predating the fix, the destination must be destroyed. It is a manual argument
# rather than a fixed ref on purpose, since main carries the fix once this lands
# and would then prove nothing.
set -euo pipefail

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
spec="$repo_root/testdata/golden/openapi/petstore.yaml"

baseline_ref=""
if [[ "${1:-}" == "--baseline" ]]; then
  baseline_ref="${2:?--baseline needs a git ref}"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0

# fail records a failed check and keeps going, so one run reports every problem
# rather than only the first.
fail() {
  printf '  FAIL: %s\n' "$1"
  failures=$((failures + 1))
}

pass() { printf '  ok: %s\n' "$1"; }

# build_at checks out <ref> into a scratch worktree and builds the CLI from it.
build_at() {
  local ref="$1" out="$2" src="$work/src-$3"
  git -C "$repo_root" worktree add -q --detach "$src" "$ref"
  (cd "$src" && go build -o "$out" ./cmd/morphic)
  git -C "$repo_root" worktree remove --force "$src"
}

printf 'building the working tree\n'
(cd "$repo_root" && go build -o "$work/current" ./cmd/morphic)

# The size cap the atomicity case writes under. bash's ulimit -f counts
# 1024-byte blocks, so this is 8 KiB.
readonly fsize_blocks=8
readonly fsize_bytes=$((fsize_blocks * 1024))

# reference_output is a full, successful compile, used both as the "previous
# good output" the atomicity case must preserve and as the size to check the cap
# against.
reference_output="$work/reference.json"
"$work/current" compile "$spec" -o "$reference_output" >/dev/null

# A cap at or above the output size would let the write finish, and the case
# below would pass without ever failing a write. Refuse to report a vacuous run.
output_size="$(stat -c%s "$reference_output")"
if ((output_size <= fsize_bytes)); then
  printf 'the %d-byte cap does not bite: output is only %d bytes\n' \
    "$fsize_bytes" "$output_size" >&2
  exit 1
fi

# atomicity_case runs one compile whose write is guaranteed to fail partway, and
# echoes the verdict on the destination: PRESERVED or DESTROYED. RLIMIT_FSIZE
# makes the write fail after it has begun, which is the shape of a full disk
# without needing one.
atomicity_case() {
  local bin="$1" dir="$work/atomic-$2"
  rm -rf "$dir" && mkdir -p "$dir"
  cp "$reference_output" "$dir/ir.json"

  local before after
  before="$(sha256sum <"$dir/ir.json")"
  ( ulimit -f "$fsize_blocks"; "$bin" compile "$spec" -o "$dir/ir.json" ) \
    >/dev/null 2>&1 || true
  after="$(sha256sum <"$dir/ir.json")"

  if [[ -n "$(find "$dir" -name '*.tmp*' -print -quit)" ]]; then
    printf 'DEBRIS'
  elif [[ "$before" == "$after" ]]; then
    printf 'PRESERVED'
  else
    printf 'DESTROYED'
  fi
}

printf '\n1. a failed write leaves the previous output intact\n'
verdict="$(atomicity_case "$work/current" current)"
if [[ "$verdict" == PRESERVED ]]; then
  pass "destination unchanged after a failed write, no temp file left behind"
else
  fail "destination was $verdict after a failed write"
fi

printf '\n2. a successful write replaces content, keeps mode, leaves no debris\n'
ok_dir="$work/success"
mkdir -p "$ok_dir"
printf 'stale\n' >"$ok_dir/ir.json"
chmod 640 "$ok_dir/ir.json"
"$work/current" compile "$spec" -o "$ok_dir/ir.json" >/dev/null

if grep -q '"irVersion"' "$ok_dir/ir.json"; then
  pass "destination holds the new document"
else
  fail "destination does not hold the new document"
fi

mode="$(stat -c%a "$ok_dir/ir.json")"
if [[ "$mode" == 640 ]]; then
  pass "replacing preserved the destination's 0640 mode"
else
  fail "replacing changed the mode to 0$mode, expected 0640"
fi

if [[ "$(find "$ok_dir" -type f | wc -l)" == 1 ]]; then
  pass "no temp file survived"
else
  fail "a temp file survived a successful write"
fi

# The three checks below pin behaviours that publishing by rename gives up. They
# assert the documented outcome, so a change to any of them is a change to the
# contract recorded on replaceFile, not a silent drift.
printf '\n3. documented limitations of publishing by rename\n'

ro_dir="$work/readonly"
mkdir -p "$ro_dir/d"
printf 'PREVIOUS\n' >"$ro_dir/d/ir.json"
chmod 644 "$ro_dir/d/ir.json"
chmod 555 "$ro_dir/d"
set +e
"$work/current" compile "$spec" -o "$ro_dir/d/ir.json" >/dev/null 2>&1
ro_status=$?
set -e
chmod 755 "$ro_dir/d"
if ((ro_status != 0)) && [[ "$(cat "$ro_dir/d/ir.json")" == PREVIOUS ]]; then
  pass "a read-only directory fails the write and leaves the destination alone"
else
  fail "expected a read-only directory to fail with the destination intact"
fi

ln_dir="$work/symlink"
mkdir -p "$ln_dir/real"
printf 'PREVIOUS\n' >"$ln_dir/real/target.json"
ln -s "$ln_dir/real/target.json" "$ln_dir/link.json"
"$work/current" compile "$spec" -o "$ln_dir/link.json" >/dev/null
if [[ ! -L "$ln_dir/link.json" ]] \
  && [[ "$(cat "$ln_dir/real/target.json")" == PREVIOUS ]]; then
  pass "a symlink destination is replaced, its target left untouched"
else
  fail "expected the symlink to be replaced with its target untouched"
fi

hl_dir="$work/hardlink"
mkdir -p "$hl_dir"
printf 'PREVIOUS\n' >"$hl_dir/a.json"
ln "$hl_dir/a.json" "$hl_dir/b.json"
"$work/current" compile "$spec" -o "$hl_dir/a.json" >/dev/null
if grep -q '"irVersion"' "$hl_dir/a.json" \
  && [[ "$(cat "$hl_dir/b.json")" == PREVIOUS ]]; then
  pass "a hard link is broken rather than followed"
else
  fail "expected the hard link to be broken, keeping its old content"
fi

if [[ -n "$baseline_ref" ]]; then
  printf '\n4. contrast with %s\n' "$baseline_ref"
  build_at "$baseline_ref" "$work/baseline" baseline
  verdict="$(atomicity_case "$work/baseline" baseline)"
  # A baseline predating the fix must fail case 1. If it passes, the case is not
  # reaching the behaviour it claims to test and case 1's result means nothing.
  if [[ "$verdict" == DESTROYED ]]; then
    pass "$baseline_ref destroys the destination, so case 1 reaches the defect"
  else
    fail "$baseline_ref reported $verdict; expected DESTROYED"
  fi
fi

printf '\n'
if ((failures > 0)); then
  printf '%d check(s) failed\n' "$failures" >&2
  exit 1
fi
printf 'all checks passed\n'
