#!/usr/bin/env python3
"""List the open pull requests that move IRVersion, the version each claims, and whether the
working tree's claim collides with one or trails main.

Usage: python3 irversion_peers.py

Reads ir.IRVersion from main, from the working tree, and from the head of each open PR, fetched
from origin unless REMOTE names another remote. A PR whose head the working tree already holds is
taken for this branch's own and left out, and so is one whose head declares the version its merge
base with main does. A PR that bumped from a base main has since passed is listed too, since it
claims a version past main's once it merges main. Exit 0 when nothing collides, 1 when the working
tree claims a version another PR claims or one main has passed, 2 when it cannot run.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import traceback
from pathlib import Path
from typing import Optional, Tuple

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


Peer = Tuple[int, str, str, Optional[str]]  # number, title, version, its merge base's version


def judge(main: str, tree: str, peers: list[Peer]) -> tuple[list[str], bool]:
    """Report main's version, the working tree's, and each peer that moves the version: one
    claiming a version past main's, or one that bumped from a base main has passed, which claims
    one past main's once it merges main. Return the lines and whether the working tree's claim
    collides with a peer or trails main."""
    lines = [f"main       {main}", f"this tree  {tree}" + ("" if tree != main else " (unchanged)")]
    movers = [(n, title, v) for n, title, v, _ in peers if ahead(v, main)]
    lines += [f"#{n:<9} {v}  {title}" for n, title, v in movers]
    lines += [f"#{n:<9} {v}  {title}  (bumped from {b} on a base main has passed: it claims a "
              f"version past {main} once it merges main)"
              for n, title, v, b in peers if not ahead(v, main) and b is not None and v != b]
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


def peers_of(root: Path, remote: str, path: str, base: str) -> list[Peer]:
    """Return a Peer for each open PR whose head the working tree lacks and declares a version."""
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
        if version is None:  # a head that declares no version claims none
            continue
        mb = git(root, "merge-base", sha, base, check=False).stdout.strip()
        at_base = git(root, "show", f"{mb}:{path}", check=False) if mb else None
        before = declared(at_base.stdout) if at_base and at_base.returncode == 0 else None
        out.append((pr["number"], pr["title"], version, before))
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
        lines, bad = judge(main_v, tree_v, peers_of(root, remote, path, base))
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
