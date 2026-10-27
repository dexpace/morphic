#!/usr/bin/env bash
# Checks a PR's head out in a worktree of its own and prints what a first review pass needs:
# the title measured as it will land, commit subjects, closing keywords, files by area, whether
# it merges cleanly into origin/main, CI, the squash-message setting, and an artifact scan.
#
# Usage: bash review-setup.sh PR-NUMBER [DIR]
#   DIR defaults to ${TMPDIR:-/tmp}/morphic-review-<PR>. An existing clean worktree there is
#   moved to the new head; one with local changes is left alone.
# Exit 0 when set up, 2 when it cannot be.
set -euo pipefail
export LC_ALL=C

[ "$#" -ge 1 ] || { sed -n '5,8p' "$0" >&2; exit 2; }
n=$1
case "$n" in '' | *[!0-9]*) echo "review-setup: $n is not a PR number" >&2; exit 2 ;; esac
command -v gh >/dev/null || { echo "review-setup: gh is required" >&2; exit 2; }
root=$(git rev-parse --show-toplevel)
here=$(cd "$(dirname "$0")" && pwd)
tmp=${TMPDIR:-/tmp}
dir=${2:-${tmp%/}/morphic-review-$n}

git -C "$root" fetch -q origin main
git -C "$root" fetch -q origin "pull/$n/head"
head=$(git -C "$root" rev-parse FETCH_HEAD)
mb=$(git -C "$root" merge-base origin/main "$head")

if [ -e "$dir" ]; then
  if [ -n "$(git -C "$dir" status --porcelain 2>/dev/null || echo dirty)" ]; then
    echo "review-setup: $dir has local changes; remove it (git worktree remove --force) or pass another DIR" >&2
    exit 2
  fi
  git -C "$dir" checkout -q --detach "$head"
else
  git -C "$root" worktree add -q --detach "$dir" "$head"
fi

title=$(gh pr view "$n" --json title --jq .title)
body=$(gh pr view "$n" --json body --jq .body)
len=$(perl -CA -e 'print length shift' "$title (#$n)")

echo "PR #$n  $(gh pr view "$n" --json state,isDraft,author \
  --jq '"\(.state)\(if .isDraft then ", draft" else "" end), by \(.author.login)"')"
echo "worktree   $dir @ ${head:0:12} (merge base ${mb:0:12})"
echo
echo "title      $title"
printf '           %s characters with " (#%s)", cap 72' "$len" "$n"
[ "$len" -le 72 ] && echo "" || echo "  OVER"
printf '%s' "$title" | grep -qE '^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\([^)]+\))?!?: [^ ]' &&
  ! printf '%s' "$title" | grep -qE '\.$' && echo "           conventional form: ok" ||
  echo "           conventional form: NOT type(scope): subject without a trailing period"
for h in Summary 'Test plan'; do
  printf '%s\n' "$body" | grep -qE "^#+ +$h" && echo "body       has $h" || echo "body       MISSING a $h section"
done
printf '%s\n' "$body" | grep -oiE '(close[sd]?|fix(e[sd])?|resolve[sd]?) +#[0-9]+' | sort -u | sed 's/^/closes     /' || true
gh pr view "$n" --json closingIssuesReferences --jq '.closingIssuesReferences[] | "linked     #\(.number)"' || true

echo
echo "commits    $(git -C "$root" rev-list --count "$mb..$head") on the branch:"
git -C "$root" log --format='%s' "$mb..$head" | while IFS= read -r s; do
  l=$(perl -CA -e 'print length shift' "$s")
  flag=
  [ "$l" -le 72 ] || flag="  OVER 72"
  printf '           %3s  %s%s\n' "$l" "$s" "$flag"
done

echo
echo "files by area:"
git -C "$root" diff --name-only "$mb" "$head" | awk -F/ '{ a = (NF > 2 ? $1 "/" $2 : (NF > 1 ? $1 : "(root)")); c[a]++ }
  END { for (k in c) printf "           %4d  %s\n", c[k], k }' | sort -k2
git -C "$root" diff --shortstat "$mb" "$head" | sed 's/^ */           /'
if git -C "$root" diff --name-only "$mb" "$head" | grep -qE '^(compilers/openapi/|docs/compilers/openapi\.md|docs/parser-library\.md|testdata/(conformance/)?openapi/)'; then
  echo "load       morphic-openapi-compiler"
fi

echo
if git -C "$root" merge-tree --write-tree origin/main "$head" >/dev/null 2>&1; then
  echo "merge      clean into origin/main"
elif git -C "$root" merge-tree --write-tree HEAD HEAD >/dev/null 2>&1; then
  echo "merge      CONFLICTS with origin/main:"
  { git -C "$root" merge-tree --write-tree --name-only origin/main "$head" 2>/dev/null || true; } |
    sed -n '2,$p' | sed '/^$/,$d; s/^/           /'
else
  echo "merge      not checked: this git has no merge-tree --write-tree"
fi
echo "squash     body default: $(gh api 'repos/{owner}/{repo}' --jq .squash_merge_commit_message 2>/dev/null || echo unknown)"
echo "checks:"
gh pr checks "$n" 2>/dev/null | awk -F'\t' '{ printf "           %-8s %s\n", $2, $1 }' || true

echo
bash "$here/artifact-scan.sh" "$mb" "$head" --pr "$n" || true
echo
echo "next: cd $dir && make gate"
