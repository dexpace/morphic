#!/usr/bin/env python3
"""List the packages a change can affect, for a fast `go test` while iterating.

Usage: go test $(python3 affected_packages.py [BASE])    BASE defaults to origin/main.

Lists each package holding a changed file, each package whose code or tests import one of them,
and internal/archtest whenever a Go or Markdown file changed, since it reads both. Prints ./...
when the change reaches files any package may read: go.mod, root testdata, docs, the Makefile,
lint config, CI. The full gate still runs before a change is called done.
"""
from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path

EVERYTHING = re.compile(r"^(go\.(mod|sum)|testdata/|docs/|Makefile|\.golangci\.yml|\.github/)")


def run(root: Path, *args: str) -> str:
    return subprocess.run(args, cwd=root, check=True, capture_output=True, text=True).stdout


def hit_packages(changed: list[str], dirs: set[str], module: str) -> set[str]:
    """Map each changed file to the package of its nearest enclosing package directory; dirs
    are package directories relative to the module root, "" for the root itself."""
    hits = set()
    for f in changed:
        if f.startswith(".claude/"):
            continue
        if f.endswith((".go", ".md")):
            hits.add(f"{module}/internal/archtest")
        parts = f.split("/")[:-1]
        while parts:
            d = "/".join(parts)
            if d in dirs:
                hits.add(f"{module}/{d}")
                break
            parts.pop()
    return hits


def dependents(listing: str, hits: set[str]) -> list[str]:
    """Read `go list -test -f '{{.ImportPath}} {{join .Deps " "}}'` and return each package that
    is a hit or whose code or a test variant of it depends on one."""
    out = set()
    for line in listing.splitlines():
        fields = line.split()  # "p dep...", "p [p.test] dep...", "p_test [p.test] dep...", "p.test dep..."
        if fields and any(f in hits for f in fields):
            out.add(re.sub(r"(\.test|_test)$", "", fields[0]))
    return sorted(out)


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    base = args[0] if args else "origin/main"
    try:
        root = Path(run(Path.cwd(), "git", "rev-parse", "--show-toplevel").strip())
        mb = run(root, "git", "merge-base", base, "HEAD").strip()
        changed = sorted(set(run(root, "git", "diff", "--name-only", mb).split()) |
                         set(run(root, "git", "ls-files", "--others", "--exclude-standard").split()))
    except subprocess.CalledProcessError as e:
        print(f"affected_packages: {e.stderr.strip() or e}", file=sys.stderr)
        return 2
    if not changed:
        print(f"affected_packages: nothing changed since {base}", file=sys.stderr)
        return 0
    if any(EVERYTHING.match(f) for f in changed):
        print("./...")
        return 0
    module = run(root, "go", "list", "-m").strip()
    dirs = {d[len(str(root)) + 1:] if d != str(root) else ""
            for d in run(root, "go", "list", "-f", "{{.Dir}}", "./...").split()}
    hits = hit_packages(changed, dirs, module)
    if hits:
        listing = run(root, "go", "list", "-test", "-f", '{{.ImportPath}} {{join .Deps " "}}', "./...")
        print("\n".join(dependents(listing, hits)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
