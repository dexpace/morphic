#!/usr/bin/env python3
"""Check a PR's head out in a worktree of its own and print what a first review pass needs: the
title measured as it will land, commit subjects, closing keywords, files by area, whether it
merges cleanly into main, CI, the squash-message setting, and an artifact scan.

Usage: python3 review_setup.py PR-NUMBER [DIR]

DIR defaults to <temp dir>/morphic-review-<PR>. An existing clean worktree there is moved to the
new head; one with local changes is left alone. Set REMOTE when the repository the PR belongs to
is not origin, as in a clone of a fork. Exit 0 when set up, 2 when it cannot be.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import tempfile
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import artifact_scan  # noqa: E402  (a sibling script)

TITLE_CAP = 72
INDENT = " " * 11
CONVENTIONAL = re.compile(r"^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\([^)]+\))?!?: \S")
CLOSING = re.compile(r"\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?) +#[0-9]+", re.I)
# The domain skill to load for the files a change touches.
DOMAINS = [
    (re.compile(r"^(compilers/openapi/|docs/compilers/openapi\.md|docs/parser-library\.md"
                r"|testdata/(conformance/)?openapi/)"),
     "morphic-openapi-compiler"),
]


def title_report(title: str, number: str) -> list[str]:
    landed = len(f"{title} (#{number})")
    over = "  OVER" if landed > TITLE_CAP else ""
    ok = CONVENTIONAL.match(title) and not title.endswith(".")
    form = "ok" if ok else "NOT type(scope): subject without a trailing period"
    return [f"title      {title}",
            f'{INDENT}{landed} characters with " (#{number})", cap {TITLE_CAP}{over}',
            f"{INDENT}conventional form: {form}"]


def body_report(body: str) -> list[str]:
    lines = []
    for section in ("Summary", "Test plan"):
        if re.search(rf"^#+ +{section}", body, re.M):
            lines.append(f"body       has {section}")
        else:
            lines.append(f"body       MISSING a {section} section")
    for keyword in sorted({m.group(0) for m in CLOSING.finditer(body)}):
        lines.append(f"closes     {keyword}")
    return lines


def areas(files: list[str]) -> list[tuple[int, str]]:
    def area(f: str) -> str:
        parts = f.split("/")
        if len(parts) > 2:
            return "/".join(parts[:2])
        return parts[0] if len(parts) > 1 else "(root)"
    return sorted(((n, a) for a, n in Counter(map(area, files)).items()), key=lambda x: x[1])


def domain_hints(files: list[str]) -> list[str]:
    return sorted({skill for pattern, skill in DOMAINS if any(pattern.match(f) for f in files)})


def run(*args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(args, check=check, capture_output=True, text=True)


def git(root: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess:
    return run("git", "-C", str(root), *args, check=check)


def checkout(root: Path, directory: Path, head: str) -> None:
    if directory.exists():
        status = git(directory, "status", "--porcelain", check=False)
        if status.returncode != 0 or status.stdout.strip():
            raise RuntimeError(f"{directory} has local changes; remove it "
                               "(git worktree remove --force) or pass another DIR")
        git(directory, "checkout", "-q", "--detach", head)
    else:
        git(root, "worktree", "add", "-q", "--detach", str(directory), head)


def merge_report(root: Path, base: str, head: str) -> list[str]:
    clean = git(root, "merge-tree", "--write-tree", base, head, check=False)
    if clean.returncode == 0:
        return [f"merge      clean into {base}"]
    if clean.returncode != 1:
        return ["merge      not checked: this git has no merge-tree --write-tree"]
    named = git(root, "merge-tree", "--write-tree", "--name-only", base, head, check=False)
    files = named.stdout.split("\n\n", 1)[0].splitlines()[1:]
    return [f"merge      CONFLICTS with {base}:"] + [INDENT + f for f in files]


def report(root: Path, number: str, directory: Path, remote: str) -> tuple[list[str], str, str]:
    """Set the worktree up and return the report's lines, the merge base and the head."""
    git(root, "fetch", "-q", remote, "main")
    git(root, "fetch", "-q", remote, f"pull/{number}/head")
    head = git(root, "rev-parse", "FETCH_HEAD").stdout.strip()
    base = f"{remote}/main"
    mb = git(root, "merge-base", base, head).stdout.strip()
    checkout(root, directory, head)

    fields = "title,body,state,isDraft,author,closingIssuesReferences"
    pr = json.loads(run("gh", "pr", "view", number, "--json", fields).stdout)
    draft = ", draft" if pr["isDraft"] else ""
    out = [f"PR #{number}  {pr['state']}{draft}, by {pr['author']['login']}",
           f"worktree   {directory} @ {head[:12]} (merge base {mb[:12]})", ""]
    out += title_report(pr["title"], number) + body_report(pr["body"])
    out += [f"linked     #{i['number']}" for i in pr["closingIssuesReferences"]]

    subjects = git(root, "log", "--format=%s", f"{mb}..{head}").stdout.splitlines()
    out += ["", f"commits    {len(subjects)} on the branch:"]
    for s in subjects:
        over = "  OVER 72" if len(s) > TITLE_CAP else ""
        out.append(f"{INDENT}{len(s):3}  {s}{over}")

    files = git(root, "diff", "--name-only", mb, head).stdout.split()
    out += ["", "files by area:"] + [f"{INDENT}{n:4}  {a}" for n, a in areas(files)]
    out.append(INDENT + git(root, "diff", "--shortstat", mb, head).stdout.strip())
    out += [f"load       {skill}" for skill in domain_hints(files)]

    out += [""] + merge_report(root, base, head)
    squash = run("gh", "api", "repos/{owner}/{repo}", "--jq", ".squash_merge_commit_message",
                 check=False)
    out.append(f"squash     body default: {squash.stdout.strip() or 'unknown'}")
    out.append("checks:")
    for line in run("gh", "pr", "checks", number, check=False).stdout.splitlines():
        cells = line.split("\t")
        if len(cells) >= 2:
            out.append(f"{INDENT}{cells[1]:8} {cells[0]}")
    return out + [""], mb, head


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if not args or not args[0].isdigit():
        print(__doc__.split("\n\n")[1], file=sys.stderr)
        return 2
    number = args[0]
    try:
        root = Path(run("git", "rev-parse", "--show-toplevel").stdout.strip())
        default = Path(tempfile.gettempdir()) / f"morphic-review-{number}"
        directory = Path(args[1] if len(args) > 1 else default).resolve()
        remote = os.environ.get("REMOTE", "origin")
        lines, mb, head = report(root, number, directory, remote)
    except subprocess.CalledProcessError as e:
        print(f"review_setup: {' '.join(e.cmd)}: {e.stderr.strip()}", file=sys.stderr)
        return 2
    except (RuntimeError, FileNotFoundError) as e:
        print(f"review_setup: {e}", file=sys.stderr)
        return 2
    print("\n".join(lines))
    artifact_scan.main([mb, head, "--pr", number])
    print(f"\nnext: cd {directory} && make gate")
    return 0


if __name__ == "__main__":
    sys.exit(main())
