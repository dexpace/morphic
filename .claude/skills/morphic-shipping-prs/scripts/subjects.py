#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Print each commit subject on the branch, and the PR title, that breaks the message rules as it
lands, so that a push can run behind it.

Usage: python3.14 .claude/skills/morphic-shipping-prs/scripts/subjects.py [--base REF]... [--pushed REF] NUMBER TITLE

NUMBER is the PR's number, or the one it will get. The subjects are those of the commits on HEAD's
first-parent line that neither origin/main nor any --base reaches, merges left out, so a commit a
merge brought in is not counted even where the base lags it. A stacked branch passes the branch it
targets, and one that left a stack also passes its former base's tip, whose commits sit on its own
first-parent line (#1341). A squash lands the title, or a branch's one commit under its own
subject, with " (#NUMBER)" appended, so both are measured with it; a subject the squash replaces
never lands as the subject, and is measured without. Each is held to the Conventional Commits form,
no trailing period and 72 characters. A commit --pushed reaches is counted, but its finding is
printed as a "pushed commit" and refuses nothing, as a pushed commit is never rewritten; a branch's
one commit is held all the same, as its subject lands until a second commit displaces it. A ref
naming no commit yet, as before a branch's first push, reaches none. Prints a "what: problem:
subject" line per finding, then a summary line. Exit 0 when the title and every subject held keep
the rules, 1 on a finding in one, 2 when it cannot run.
"""

import argparse
import re
import subprocess
import sys
import traceback

CAP = 72
BASE = "origin/main"
PUSHED = "pushed commit"
FORM = re.compile(r"(feat|fix|refactor|docs|test|build|chore|ci|perf)(\(.+\))?!?: [^ ]")


def problems(subject: str, suffix: str) -> list[str]:
    """Return what is wrong with subject as it lands, suffix appended where the squash adds one."""
    found = []
    if not FORM.match(subject):
        found.append("not type(scope): subject")
    if subject.endswith("."):
        found.append("ends in a period")
    landed = len(subject + suffix)
    if landed > CAP:
        counted = f" with '{suffix.strip()}'" if suffix else ""
        found.append(f"{landed} characters{counted}, over {CAP}")
    return found


def check(number: int, title: str, commits: list[str],
          pushed: frozenset[int] = frozenset()) -> list[str]:
    """Return a line per problem in the title and each commit subject, measuring the title and a
    branch's one commit with the number the squash appends. A commit whose index pushed holds is
    named a pushed commit, bar a branch's one commit, whose subject lands until a second displaces
    it."""
    assert pushed <= set(range(len(commits))), "a pushed index names a commit of the branch"
    suffix = f" (#{number})"
    lands = len(commits) == 1
    subjects = [("title", title, suffix)]
    subjects += [(PUSHED if i in pushed and not lands else "commit", s, suffix if lands else "")
                 for i, s in enumerate(commits)]
    return [f"{what}: {p}: {s}" for what, s, end in subjects for p in problems(s, end)]


def git(*args: str) -> str:
    """Return what git prints for args, raising on a failure."""
    return subprocess.run(["git", *args], capture_output=True, text=True, check=True).stdout


def branch_commits(bases: list[str]) -> list[tuple[str, str]]:
    """Return the hash and subject of each commit on HEAD's first-parent line that no base reaches,
    merges left out."""
    assert bases, "a branch's commits are read against at least one base"
    out = git("log", "--no-merges", "--first-parent", "--format=%H %s", "HEAD", "--not", *bases)
    return [(h, s) for h, _, s in (line.partition(" ") for line in out.splitlines())]


def pushed_hashes(ref: str | None, bases: list[str]) -> set[str]:
    """Return the hashes ref reaches that no base does: none when ref is unset or names no commit,
    as a branch's own ref does before its first push, so every commit is then held."""
    if ref is None:
        return set()
    found = subprocess.run(["git", "rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"],
                           capture_output=True, text=True)
    if found.returncode != 0:
        return set()
    return set(git("rev-list", ref, "--not", *bases).split())


def parse(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--base", action="append", default=[], metavar="REF",
                   help=f"a ref whose commits are not the branch's own, beside {BASE}; repeatable")
    p.add_argument("--pushed", metavar="REF",
                   help="the branch as pushed, whose commits are counted but refuse nothing, "
                        "bar a branch's one commit")
    p.add_argument("number", type=int, help="the PR's number, or the one it will get")
    p.add_argument("title", help="the PR's title")
    args = p.parse_args(argv)
    if not all(b.strip() and not b.endswith("/") for b in args.base):
        p.error("a base names no branch: a failed `gh pr view` leaves it empty or origin/")
    if args.pushed is not None and (not args.pushed.strip() or args.pushed.endswith("/")):
        p.error("--pushed names no branch: a failed `gh pr view` leaves it empty or origin/")
    args.base = [BASE, *args.base]
    if args.number < 1:
        p.error(f"number must be positive, not {args.number}")
    if not args.title.strip():
        p.error("title is empty: a failed `gh pr view` prints none")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse(sys.argv[1:] if argv is None else argv)
    try:
        commits = branch_commits(args.base)
        pushed = pushed_hashes(args.pushed, args.base)
    except (OSError, subprocess.CalledProcessError) as e:
        print(f"subjects: {getattr(e, 'stderr', '').strip() or e}", file=sys.stderr)
        return 2
    at = frozenset(i for i, (h, _) in enumerate(commits) if h in pushed)
    found = check(args.number, args.title, [s for _, s in commits], at)
    for line in found:
        print(line)
    refused = [f for f in found if not f.startswith(f"{PUSHED}: ")]
    held = f"the title and {len(commits)} commit subject(s)"
    if refused:
        print(f"subjects: {len(refused)} finding(s) in {held}")
    elif found:
        print(f"subjects: {len(found)} finding(s) on pushed commits alone ({held})")
    else:
        print(f"subjects: clean ({held})")
    return 1 if refused else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
