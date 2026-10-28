#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""List the open pull requests that move IRVersion, the version each claims, and whether the
working tree's claim collides with one or trails main.

Usage: python3.14 irversion_peers.py

Reads ir.IRVersion from main, from the working tree, and from the head of each open PR, all of
origin's repository unless REMOTE names another remote, from whichever file under ir/ declares it
in each. A PR whose head the working tree already
holds is taken for this branch's own and left out, and so is one whose head declares the version
its merge base with main does. A PR that bumped from a base main has since passed is listed too,
since it claims a version past main's once it merges main. Exit 0 when nothing collides, 1 when the
working tree claims a version another PR claims or one main has passed, 2 when it cannot run.
"""

import json
import os
import re
import subprocess
import sys
import traceback
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "morphic-reviewing-prs" / "scripts"))
from review_setup import point_gh_at  # noqa: E402  (a script of the review skill)

DECLARED = re.compile(r'^const IRVersion = "([^"]+)"', re.M)
MAX_PRS = 200  # open PRs read; more is refused, since a collision past them would go unseen


def git(root: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(["git", "-C", str(root), *args], check=check, capture_output=True,
                          text=True, errors="replace")


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


type Peer = tuple[int, str, str, str | None]  # number, title, version, its merge base's version


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


def declared_at(root: Path, rev: str | None) -> str | None:
    """Return the IRVersion the tree at rev declares, or the working tree with rev None, wherever
    under ir/ the constant sits: a branch, a peer or main may have moved it to another file."""
    flags, trees = (["--untracked"], []) if rev is None else ([], [rev])
    found = git(root, "grep", "-h", *flags, "-E", "^const IRVersion = ", *trees, "--", "ir/*.go",
                ":(exclude)ir/*_test.go", check=False)
    return declared(found.stdout)


def peers_of(root: Path, remote: str, base: str) -> list[Peer]:
    """Return a Peer for each open PR whose head the working tree lacks and declares a version."""
    listing = subprocess.run(["gh", "pr", "list", "--state", "open", "--limit", str(MAX_PRS + 1),
                              "--json", "number,title,headRefOid"], check=True,
                             capture_output=True, text=True, errors="replace", cwd=root).stdout
    prs = json.loads(listing)
    if len(prs) > MAX_PRS:
        raise RuntimeError(f"more than {MAX_PRS} open PRs; raise MAX_PRS to read them all")
    out = []
    for pr in prs:
        sha = pr["headRefOid"]
        if git(root, "cat-file", "-e", f"{sha}^{{commit}}", check=False).returncode != 0:
            git(root, "fetch", "-q", remote, f"pull/{pr['number']}/head")
        if git(root, "merge-base", "--is-ancestor", sha, "HEAD", check=False).returncode == 0:
            continue  # this branch's own PR
        version = declared_at(root, sha)
        if version is None:  # a head that declares no version claims none
            continue
        mb = git(root, "merge-base", sha, base, check=False).stdout.strip()
        before = declared_at(root, mb) if mb else None
        out.append((pr["number"], pr["title"], version, before))
    return out


def main() -> int:
    try:
        root = Path(git(Path.cwd(), "rev-parse", "--show-toplevel").stdout.strip())
        remote = os.environ.get("REMOTE", "origin")
        point_gh_at(root, remote)
        git(root, "fetch", "-q", remote, "main")
        base = f"{remote}/main"
        main_v, tree_v = declared_at(root, base), declared_at(root, None)
        if main_v is None or tree_v is None:
            raise RuntimeError(f"no file under ir/ declares IRVersion on {base} or in the "
                               "working tree")
        lines, bad = judge(main_v, tree_v, peers_of(root, remote, base))
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
