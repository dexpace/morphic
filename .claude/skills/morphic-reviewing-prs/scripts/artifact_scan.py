#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Scan what a change ships for text that must not land: assistant attribution, the vocabulary
of the working session that wrote it, paths on one machine, and audit-finding labels.

Usage: python3.14 artifact_scan.py [BASE [HEAD]] [--pr N]    BASE origin/main, HEAD HEAD by default.

Reads the lines the change adds, its commit messages and, with --pr, the PR's title and body.
Every hit is printed for a person to judge. Exit 0 when clean, 1 on a hit, 2 when it cannot run.
"""

import argparse
import json
import re
import subprocess
import sys
import traceback
from collections.abc import Iterable, Iterator

# Split so this file does not match its own patterns.
PATTERNS = [
    r"Co-Authored-By:.*(Cl" r"aude|[Aa]nthr" r"opic)|Gene" r"rated with|noreply@anthr" r"opic",
    r"(^|[^A-Za-z.])(Cl" r"aude|Anthr" r"opic|ChatG" r"PT)([^A-Za-z]|$)",
    r"(the|this|your|per the) bri" r"ef([^A-Za-z]|$)|\(bri" r"ef|bri" r"ef:",
    r"(^|[^A-Za-z])(coordi" r"nator|implem" r"enter|sub" r"agent|scratch" r"pad)([^A-Za-z]|$)",
    r"/U" r"sers/|/h" r"ome/[a-z]|/p" r"rivate/(tmp|var)",
    r"remedi" r"ation|audit sw" r"eep|(^|[^A-Za-z0-9])[HM][0-9]{1,2}([^0-9A-Za-z]|$)",
]
ARTIFACT = re.compile("|".join(f"(?:{p})" for p in PATTERNS))
HUNK = re.compile(r"^@@ -\S+ \+(\d+)")


def run(*args: str) -> str:
    return subprocess.run(args, check=True, capture_output=True, text=True, errors="replace").stdout


def added_lines(diff: str) -> Iterator[tuple[str, str]]:
    """Yield ("path:line", text) for each line a unified diff adds."""
    path, line = "", 0
    for raw in diff.split("\n"):
        if raw.startswith("+++ "):
            path = raw[6:] if raw.startswith("+++ b/") else raw[4:]
        elif raw.startswith("@@"):
            m = HUNK.match(raw)
            line = int(m.group(1)) if m else 0
        elif raw.startswith("+"):
            yield f"{path}:{line}", raw[1:]
            line += 1


def hits(label: str, lines: Iterable[tuple[str, str]]) -> list[str]:
    return [f"{label}: {where}: {text}" for where, text in lines if ARTIFACT.search(text)]


def commit_lines(base: str, head: str) -> Iterator[tuple[str, str]]:
    for sha in run("git", "rev-list", "--abbrev-commit", f"{base}..{head}").split():
        for text in run("git", "log", "-1", "--format=%B", sha).split("\n"):
            if text.strip():
                yield sha, text


def pr_lines(number: str) -> Iterator[tuple[str, str]]:
    pr = json.loads(run("gh", "pr", "view", number, "--json", "title,body"))
    yield "title", pr["title"]
    for text in pr["body"].split("\n"):
        yield "body", text


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Scan a change for text that must not ship.")
    p.add_argument("base", nargs="?", default="origin/main")
    p.add_argument("head", nargs="?", default="HEAD")
    p.add_argument("--pr")
    args = p.parse_args(sys.argv[1:] if argv is None else argv)
    try:
        for rev in (args.base, args.head):
            run("git", "rev-parse", "--verify", "--quiet", f"{rev}^{{commit}}")
        diff = run("git", "diff", "-U0", f"{args.base}...{args.head}")
        found = hits("added line", added_lines(diff))
        found += hits("commit", commit_lines(args.base, args.head))
        if args.pr:
            found += hits(f"PR #{args.pr}", pr_lines(args.pr))
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        print(f"artifact_scan: {getattr(e, 'stderr', '') or e}", file=sys.stderr)
        return 2
    for h in found:
        print(h)
    if found:
        print(f"artifact_scan: {len(found)} line(s) to judge")
        return 1
    print("artifact_scan: clean")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
