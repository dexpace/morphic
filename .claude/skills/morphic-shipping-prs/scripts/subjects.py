#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Print each commit subject on the branch, and the PR title, that breaks the message rules as it
lands, so that a push can run behind it.

Usage: python3.14 .claude/skills/morphic-shipping-prs/scripts/subjects.py NUMBER TITLE

NUMBER is the PR's number, or the one it will get. The subjects are those of the commits on HEAD's
first-parent line back to origin/main, merges left out, so a commit a merge brought in, from main or
a stacked base, is not counted even where origin/main lags it, while a branch started on another
PR's tip counts that PR's commits as its own (#1341). A squash lands the title, or a branch's one
commit under its own subject, with " (#NUMBER)" appended, so both are measured with it; a subject
the squash replaces never lands as the subject, and is measured without. Each is held to the
Conventional Commits form, no trailing period and 72 characters. Prints a "what: problem: subject"
line per finding, then a summary line. Exit 0 when every subject holds, 1 on a finding, 2 when it
cannot run.
"""

import argparse
import re
import subprocess
import sys
import traceback

CAP = 72
BASE = "origin/main"
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


def check(number: int, title: str, commits: list[str]) -> list[str]:
    """Return a line per problem in the title and each commit subject, measuring the title and a
    branch's one commit with the number the squash appends."""
    suffix = f" (#{number})"
    subjects = [("title", title, suffix)]
    subjects += [("commit", s, suffix if len(commits) == 1 else "") for s in commits]
    return [f"{what}: {p}: {s}" for what, s, end in subjects for p in problems(s, end)]


def branch_subjects() -> list[str]:
    """Return the subject of each commit on HEAD's first-parent line back to origin/main, merges
    left out."""
    out = subprocess.run(["git", "log", "--no-merges", "--first-parent", "--format=%s",
                          f"{BASE}..HEAD"],
                         capture_output=True, text=True, check=True).stdout
    return out.splitlines()


def parse(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("number", type=int, help="the PR's number, or the one it will get")
    p.add_argument("title", help="the PR's title")
    args = p.parse_args(argv)
    if args.number < 1:
        p.error(f"number must be positive, not {args.number}")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse(sys.argv[1:] if argv is None else argv)
    try:
        commits = branch_subjects()
    except (OSError, subprocess.CalledProcessError) as e:
        print(f"subjects: {getattr(e, 'stderr', '').strip() or e}", file=sys.stderr)
        return 2
    found = check(args.number, args.title, commits)
    for line in found:
        print(line)
    held = f"the title and {len(commits)} commit subject(s)"
    print(f"subjects: {len(found)} finding(s) in {held}" if found else f"subjects: clean ({held})")
    return 1 if found else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
