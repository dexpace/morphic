#!/usr/bin/env python3
"""List the open pull requests that move IRVersion, the version each claims, and whether the
working tree's claim collides with one or trails main.

Usage: python3 irversion_peers.py

Reads ir.IRVersion from main, from the working tree, and from the head of each open PR, fetched
from origin unless REMOTE names another remote. A PR whose head the working tree already holds is
taken for this branch's own and left out. Exit 0 when nothing collides, 1 when the working tree
claims a version another PR claims or one main has passed, 2 when it cannot run.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import traceback
from pathlib import Path

DECLARED = re.compile(r'^const IRVersion = "([^"]+)"', re.M)


def git(root: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(["git", "-C", str(root), *args], check=check, capture_output=True,
                          text=True)


def declared(text: str) -> str | None:
    """Return the IRVersion a Go file declares, or None when it declares none."""
    m = DECLARED.search(text)
    return m.group(1) if m else None


def order(version: str) -> tuple[int, ...] | None:
    """Return a 0.MINOR.PATCH version as numbers to compare, or None when it is no such version."""
    parts = version.split(".")
    return tuple(map(int, parts)) if all(p.isdigit() for p in parts) and len(parts) == 3 else None


def ahead(version: str, main: str) -> bool:
    """Report whether version is past main's; one that is no version counts, so it is shown."""
    v, m = order(version), order(main)
    return v is None or m is None or v > m


def judge(main: str, tree: str, peers: list[tuple[int, str, str]]) -> tuple[list[str], bool]:
    """Report main's version, the working tree's, and each peer claiming one past main's, since a
    branch behind main claims nothing; return the lines and whether the working tree's claim
    collides with a peer or trails main."""
    lines = [f"main       {main}", f"this tree  {tree}" + ("" if tree != main else " (unchanged)")]
    movers = [(n, title, v) for n, title, v in peers if ahead(v, main)]
    lines += [f"#{n:<9} {v}  {title}" for n, title, v in movers]
    bad = False
    if tree != main:
        if order(tree) is None or not ahead(tree, main):
            lines.append(f"TRAILS     main is at {main}: claim the next version after it")
            bad = True
        for n, _, v in movers:
            if v == tree:
                lines.append(f"COLLIDES   #{n} also claims {tree}: whichever merges second takes "
                             "the next version")
                bad = True
    return lines, bad


def peers_of(root: Path, remote: str, path: str) -> list[tuple[int, str, str]]:
    """Return (number, title, version) for each open PR whose head the working tree lacks."""
    listing = subprocess.run(["gh", "pr", "list", "--state", "open", "--limit", "200", "--json",
                              "number,title,headRefOid"], check=True, capture_output=True,
                             text=True, cwd=root).stdout
    out = []
    for pr in json.loads(listing):
        sha = pr["headRefOid"]
        if git(root, "cat-file", "-e", f"{sha}^{{commit}}", check=False).returncode != 0:
            git(root, "fetch", "-q", remote, f"pull/{pr['number']}/head")
        if git(root, "merge-base", "--is-ancestor", sha, "HEAD", check=False).returncode == 0:
            continue  # this branch's own PR
        shown = git(root, "show", f"{sha}:{path}", check=False)
        version = declared(shown.stdout) if shown.returncode == 0 else None
        if version is not None:  # a head that declares no version claims none
            out.append((pr["number"], pr["title"], version))
    return out


def main() -> int:
    try:
        root = Path(git(Path.cwd(), "rev-parse", "--show-toplevel").stdout.strip())
        remote = os.environ.get("REMOTE", "origin")
        git(root, "fetch", "-q", remote, "main")
        base = f"{remote}/main"
        found = git(root, "grep", "-l", "const IRVersion = ", base, "--", "ir/*.go").stdout.split()
        if not found:
            raise RuntimeError(f"no file under ir/ declares IRVersion on {base}")
        path = found[0].split(":", 1)[1]
        main_v = declared(git(root, "show", f"{base}:{path}").stdout)
        tree_v = declared((root / path).read_text())
        if main_v is None or tree_v is None:
            raise RuntimeError(f"{path} declares no IRVersion on {base} or in the working tree")
        lines, bad = judge(main_v, tree_v, peers_of(root, remote, path))
    except subprocess.CalledProcessError as e:
        print(f"irversion_peers: {' '.join(e.cmd)}: {e.stderr.strip()}", file=sys.stderr)
        return 2
    except (RuntimeError, OSError) as e:
        print(f"irversion_peers: {e}", file=sys.stderr)
        return 2
    print("\n".join(lines))
    return 1 if bad else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
