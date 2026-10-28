#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Check a branch before it is pushed, or a PR's tree in review: each commit subject, the PR title
measured as it will land, the body's sections and closing keywords, files the working tree holds
apart from HEAD, the merge with main, the artifact scan, and that every name a skill uses exists.

Usage: python3.14 preflight.py [--base BASE] [--pr N] [--title TITLE] [--body FILE]

Run it anywhere in the repository: it reads the whole tree from the root. BASE defaults to
origin/main. With --pr, the title and body are the PR's own unless --title or --body gives a draft;
without it, the title is measured with the number the next issue or PR takes. Prints a line per
check, FIX before each that needs fixing. Exit 0 when nothing does, 1 when something does, 2 when
it cannot run.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import traceback
from pathlib import Path

SKILLS = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(SKILLS / "morphic-reviewing-prs" / "scripts"))
sys.path.insert(0, str(SKILLS / "morphic-curating-skills" / "scripts"))
import artifact_scan  # noqa: E402  (a script of the review skill)
import skills_lint  # noqa: E402  (a script of the curating skill)
from artifact_scan import run  # noqa: E402  (the same helper, not a copy)

SUBJECT_CAP = 72
INDENT = " " * 11
CONVENTIONAL = re.compile(r"^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\([^)]+\))?!?: \S")
BREAKING = re.compile(r"^[a-z]+(\([^)]+\))?!:")
# GitHub reads a keyword in any case, a colon after it or not, before #N or OWNER/REPO#N.
CLOSING = re.compile(r"\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?):? +([\w.-]+/[\w.-]+)?#[0-9]+",
                     re.I)

# A report line, and whether it needs fixing.
type Line = tuple[str, bool]


def subject_lines(subjects: list[str]) -> list[Line]:
    """Judge each commit subject by the form CLAUDE.md gives it."""
    out = []
    for s in subjects:
        problems = []
        if len(s) > SUBJECT_CAP:
            problems.append(f"over {SUBJECT_CAP}")
        if not CONVENTIONAL.match(s) or s.endswith("."):
            problems.append("not type(scope): subject without a trailing period")
        note = "  " + "; ".join(problems) if problems else ""
        out.append((f"subject    {len(s):3}  {s}{note}", bool(problems)))
    return out


def title_report(title: str, number: str) -> list[Line]:
    landed = len(f"{title} (#{number})")
    over = landed > SUBJECT_CAP
    ok = CONVENTIONAL.match(title) is not None and not title.endswith(".")
    form = "ok" if ok else "NOT type(scope): subject without a trailing period"
    count = f'{INDENT}{landed} characters with " (#{number})", cap {SUBJECT_CAP}'
    return [(f"title      {title}", False), (count + ("  OVER" if over else ""), over),
            (f"{INDENT}conventional form: {form}", not ok)]


def body_report(body: str, breaking: bool | None) -> list[Line]:
    """Report the sections CLAUDE.md asks of a PR body and every closing keyword. A Breaking
    section follows the title's !, so with breaking None, for a body with no title, it is not
    judged."""
    lines = []
    sections = ("Summary", "Test plan") + (("Breaking",) if breaking is not None else ())
    for section in sections:
        has = re.search(rf"^#+ +{section}", body, re.M) is not None
        wanted = section != "Breaking" or breaking
        if has and wanted:
            lines.append((f"body       has {section}", False))
        elif has:
            lines.append(("body       has a Breaking section, but the title marks no break with !",
                          True))
        elif wanted:
            lines.append((f"body       MISSING a {section} section", True))
    closing = sorted({m.group(0) for m in CLOSING.finditer(body)})
    return lines + [(f"closes     {keyword}", False) for keyword in closing]


def title_body_lines(title: str | None, number: int, body: str | None) -> list[Line]:
    """Judge the title as it will land and the body against it."""
    if title is None:
        out = [("title      none given: pass --pr or --title to measure it", False)]
    else:
        out = title_report(title, str(number))
    if body is not None:
        out += body_report(body, None if title is None else BREAKING.match(title) is not None)
    return out


def worktree_lines() -> list[Line]:
    """Report each file the working tree holds apart from HEAD: the push carries HEAD, while the
    checks here, the lint among them, read the working tree."""
    changed = run("git", "diff", "--name-only", "HEAD").splitlines()
    untracked = run("git", "ls-files", "--others", "--exclude-standard").splitlines()
    return ([(f"changed    {f}: commit or stash it, since the push carries HEAD", True)
             for f in changed]
            + [(f"untracked  {f}: commit it, remove it, or ignore it", True) for f in untracked])


def skills_lines(root: Path) -> list[Line]:
    """Report each name a skill uses that the branch leaves naming nothing, such as a test it
    renamed: the change that moves a name retargets every skill that cites it."""
    return [(f"skills     {f}", True) for f in skills_lint.findings(root)[1]]


def merge_line(base: str) -> Line:
    if subprocess.run(["git", "merge-base", "--is-ancestor", base, "HEAD"]).returncode == 0:
        return (f"merge      holds {base}", False)
    clean = subprocess.run(["git", "merge-tree", "--write-tree", base, "HEAD"],
                           capture_output=True, text=True, errors="replace").returncode == 0
    if clean:
        return (f"merge      merges {base} cleanly but does not hold it: merge it, then rerun the "
                "suite", False)
    return (f"merge      CONFLICTS with {base}: merge it in and resolve", True)


def next_number() -> int:
    """Return the number the next issue or PR takes, one past the latest of either."""
    latest = run("gh", "api", "repos/{owner}/{repo}/issues?state=all&per_page=1", "--jq",
                 ".[0].number")
    return int(latest.strip()) + 1


def report(args: argparse.Namespace) -> list[Line]:
    run("git", "rev-parse", "--verify", "--quiet", f"{args.base}^{{commit}}")
    title, body, number = args.title, None, args.pr
    if args.pr:
        pr = json.loads(run("gh", "pr", "view", str(args.pr), "--json", "title,body"))
        title, body = title or pr["title"], pr["body"]
    if args.body:
        body = Path(args.body).read_text()
    if number is None and title is not None:
        number = next_number()
    subjects = run("git", "log", "--no-merges", "--format=%s", f"{args.base}..HEAD").splitlines()

    out = subject_lines(subjects)
    out += title_body_lines(title, number or 0, body)
    out += worktree_lines()
    out.append(merge_line(args.base))
    diff = run("git", "diff", "-U0", f"{args.base}...HEAD")
    found = artifact_scan.hits("added line", artifact_scan.added_lines(diff))
    found += artifact_scan.hits("commit", artifact_scan.commit_lines(args.base, "HEAD"))
    texts = [("title", title)] if title else []
    texts += [("body", line) for line in (body or "").split("\n")]
    found += artifact_scan.hits("PR text", texts)
    out += [(f"artifact   {h}", True) for h in found]
    out += skills_lines(Path(run("git", "rev-parse", "--show-toplevel").strip()))
    return out


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Check a branch before it is pushed.")
    p.add_argument("--base", default="origin/main")
    p.add_argument("--pr", type=int)
    p.add_argument("--title")
    p.add_argument("--body")
    args = p.parse_args(sys.argv[1:] if argv is None else argv)
    try:
        if args.body:
            args.body = str(Path(args.body).resolve())
        # git reads pathspecs and untracked files from the working directory, so a run from a
        # subdirectory would check only that subdirectory and call the rest clean.
        os.chdir(run("git", "rev-parse", "--show-toplevel").strip())
        lines = report(args)
    except subprocess.CalledProcessError as e:
        print(f"preflight: {' '.join(e.cmd)}: {(e.stderr or '').strip() or e}", file=sys.stderr)
        return 2
    except (OSError, ValueError, RuntimeError) as e:
        print(f"preflight: {e}", file=sys.stderr)
        return 2
    for line, fix in lines:
        print(("FIX " if fix else "    ") + line)
    fixes = sum(fix for _, fix in lines)
    print(f"preflight: {fixes} to fix" if fixes else "preflight: clean")
    return 1 if fixes else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
