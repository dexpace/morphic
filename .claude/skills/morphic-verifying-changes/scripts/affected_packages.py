#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""List the packages a change can affect, for a fast `go test` while iterating.

Usage: go test $(python3.14 affected_packages.py [BASE])    BASE defaults to origin/main.

Lists each package holding a changed file, each package whose code or tests import one of them,
internal/archtest whenever a Go or Markdown file changed, since it reads both, and, when the change
adds a citation of a keyed document's entry (docs/parser-library.md#<key>), each package whose
tests read citations across the tree, found by their calls into internal/keydoc. Prints ./... when
the change reaches files any package may read: go.mod, root testdata, docs, the Makefile, lint
config, CI. Prints nothing, and says why on stderr, when no Go package reads the change, as for one
under .claude alone; run from the repository root, `go test` given no package then fails with "no
Go files", which says nothing about the change. The full gate still runs before a change is called
done.
"""

import re
import subprocess
import sys
import traceback
from pathlib import Path

EVERYTHING = re.compile(r"^(go\.(mod|sum)|testdata/|docs/|Makefile|\.golangci\.yml|\.github/)")
# internal/keydoc reads a citation on any Markdown line and in any Go or YAML comment.
CITING = (".go", ".md", ".yaml", ".yml")
CITATION = re.compile(r"\.md#[A-Za-z0-9_-]")
READS_CITATIONS = ("keydoc.Citations(", "keydoc.Walk(")


def run(root: Path, *args: str) -> str:
    return subprocess.run(args, cwd=root, check=True, capture_output=True, text=True,
                          errors="replace").stdout


def hit_packages(changed: list[str], dirs: set[str], module: str) -> tuple[set[str], list[str]]:
    """Map each changed file to the package of its nearest enclosing package directory; dirs
    are package directories relative to the module root, "" for the root itself. Also return
    each changed Go file no package holds, for which no list is trustworthy."""
    hits, unmapped = set(), []
    for f in changed:
        if f.startswith(".claude/"):
            continue
        if f.endswith((".go", ".md")):
            hits.add(f"{module}/internal/archtest")
        parts = f.split("/")[:-1]
        while True:
            d = "/".join(parts)
            if d in dirs:
                hits.add(f"{module}/{d}" if d else module)
                break
            if not parts:
                if f.endswith(".go"):
                    unmapped.append(f)
                break
            parts.pop()
    return hits, unmapped


def adds_a_citation(diff: str) -> bool:
    """Report whether a unified diff adds a line citing a keyed document's entry."""
    return any(line.startswith("+") and not line.startswith("+++") and CITATION.search(line)
               for line in diff.splitlines())


def citation_readers(grep: str, module: str) -> set[str]:
    """Return the package of each test file `git grep -l` found reading citations."""
    return {f"{module}/{Path(f).parent.as_posix()}" for f in grep.split()}


def dependents(listing: str, hits: set[str]) -> list[str]:
    """Read `go list -test -f '{{.ImportPath}} {{join .Deps " "}}'` and return each package that
    is a hit or whose code or a test variant of it depends on one."""
    out = set()
    for line in listing.splitlines():
        # "p dep...", "p [p.test] dep...", "p_test [p.test] dep...", "p.test dep..."
        fields = line.split()
        if fields and any(f in hits for f in fields):
            out.add(re.sub(r"(\.test|_test)$", "", fields[0]))
    return sorted(out)


def added(root: Path, mb: str, files: list[str], untracked: set[str]) -> str:
    """Return a diff of files since mb, each untracked file's lines counted as added."""
    tracked = [f for f in files if f not in untracked]
    text = run(root, "git", "diff", "-U0", mb, "--", *tracked) if tracked else ""
    for f in sorted(untracked & set(files)):
        lines = (root / f).read_text(errors="replace").splitlines()
        text += "".join(f"+{line}\n" for line in lines)
    return text


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    base = args[0] if args else "origin/main"
    try:
        root = Path(run(Path.cwd(), "git", "rev-parse", "--show-toplevel").strip()).resolve()
        mb = run(root, "git", "merge-base", base, "HEAD").strip()
        diffed = run(root, "git", "diff", "--name-only", mb).split()
        untracked = run(root, "git", "ls-files", "--others", "--exclude-standard").split()
        changed = sorted(set(diffed) | set(untracked))
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
    dirs = set()
    for d in run(root, "go", "list", "-f", "{{.Dir}}", "./...").split():
        real = Path(d).resolve()  # git and go may spell one directory through different links
        if real != root and root not in real.parents:
            print(f"affected_packages: {d} is outside {root}; listing everything", file=sys.stderr)
            print("./...")
            return 0
        dirs.add(real.relative_to(root).as_posix() if real != root else "")
    hits, unmapped = hit_packages(changed, dirs, module)
    if unmapped:
        held = ", ".join(unmapped)
        print(f"affected_packages: no package holds {held}; listing everything", file=sys.stderr)
        print("./...")
        return 0
    if not hits:
        print("affected_packages: no Go package reads the changed files", file=sys.stderr)
        return 0
    citing = [f for f in changed if f.endswith(CITING) and not f.startswith(".")]
    if citing and adds_a_citation(added(root, mb, citing, set(untracked))):
        grep = ["git", "grep", "-l", "-F", *(a for r in READS_CITATIONS for a in ("-e", r))]
        hits |= citation_readers(run(root, *grep, "--", "*_test.go"), module)
    imports = '{{.ImportPath}} {{join .Deps " "}}'
    listing = run(root, "go", "list", "-test", "-f", imports, "./...")
    print("\n".join(dependents(listing, hits)))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
