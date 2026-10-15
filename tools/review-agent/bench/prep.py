#!/usr/bin/env python3
"""Prepare blind back-test cases. For each PR: fetch its head under refs/bench/<PR> (a squash
merge leaves its first commit on no branch), write the grader's ground truth to
truth-<PR>.md, and print the reviewer and grader prompts.

Read the truth file before running the review: a PR whose later commits continue the work
instead of fixing it measures nothing (#564 and #136 here). Use PRs from
build/data/holdout.json; any other PR fed the catalog. Remove the refs afterwards with
git for-each-ref --format='delete %(refname)' refs/bench | git update-ref --stdin

Usage: prep.py <PR> [<PR>...]"""
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = subprocess.run(["git", "-C", str(HERE), "rev-parse", "--show-toplevel"],
                      capture_output=True, text=True, check=True).stdout.strip()
HOLDOUT = set(json.loads((HERE.parent / "build" / "data" / "holdout.json").read_text()))


def gh(*args):
    return json.loads(subprocess.run(["gh", *args], capture_output=True, text=True, check=True).stdout)


def prep(n):
    subprocess.run(["git", "-C", REPO, "fetch", "-q", "origin", f"+pull/{n}/head:refs/bench/{n}"], check=True)
    commits = gh("api", "--paginate", f"repos/dexpace/morphic/pulls/{n}/commits")
    first = commits[0]
    sha, date = first["sha"], first["commit"]["committer"]["date"][:10]
    later = [c for c in commits[1:] if not c["commit"]["message"].startswith("Merge")]
    lines = [f"# Ground truth for #{n}", "", f"Initial commit reviewed: {sha}", "",
             "Each item below is a commit pushed after the initial one. Most fix a defect a local "
             "review found in the initial change; some are planned continuation work and are NOT "
             "defects -- judge each.", ""]
    lines += [f"## {c['sha'][:8]}\n{c['commit']['message'].strip()}\n" for c in later]
    truth = HERE / f"truth-{n}.md"
    truth.write_text("\n".join(lines))
    review = f"${{XDG_CACHE_HOME:-$HOME/.cache}}/morphic-review/bt-{n}.md"
    if n not in HOLDOUT:
        print(f"warning: #{n} is not a hold-out PR, so the catalog learned from it")
    print(f"#{n}: initial {sha[:10]} ({date}), {len(later)} later non-merge commits -> {truth}")
    print(f"  reviewer: Review BASE={sha[:10]}^ HEAD={sha[:10]} (committed {date}). Label: bt-{n}. "
          f"Blind: no `gh pr`, no git log past HEAD, nothing under refs/bench or refs/remotes, "
          f"add `created:<{date}` to every issue search.")
    print(f"  grader:   Follow {HERE / 'grader-brief.md'}. REVIEW={review} TRUTH={truth} "
          f"OUTPUT={HERE / f'grade-{n}.json'}")


def main():
    if len(sys.argv) < 2:
        raise SystemExit(__doc__)
    for arg in sys.argv[1:]:
        prep(int(arg))


if __name__ == "__main__":
    main()
