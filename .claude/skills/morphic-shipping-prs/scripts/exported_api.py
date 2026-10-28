#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""List the exported Go API a branch removes or changes, and each method it adds to an exported
interface, in every package outside internal/, cmd/ and testdata.

Usage: python3.14 exported_api.py [BASE]    BASE defaults to origin/main.

Reads `go doc -all` of each such package the branch changes, at its merge base with BASE and in
the working tree, so every declaration, struct field, interface method and member of a grouped
declaration is compared, whatever its layout. Names of receivers, parameters and results are left
out of a signature, since renaming one changes no API; so is a variable's initializer. A field is
compared with its struct tag, which fixes how it encodes, and a constant in a block using iota
with its position, which fixes its value. Exit 0 when nothing is listed, 1 when something is, 2
when it cannot run.
"""

import contextlib
import re
import subprocess
import sys
import tempfile
import traceback
from collections.abc import Iterator
from pathlib import Path

PUBLIC_GO = ["*.go", ":(exclude)*_test.go", ":(glob,exclude)**/internal/**",
             ":(glob,exclude)cmd/**", ":(glob,exclude)**/testdata/**"]
# The headings `go doc -all` prints before the declarations; what precedes them is the package's
# documentation, which can hold a line starting "type ".
SECTIONS = {"CONSTANTS", "VARIABLES", "FUNCTIONS", "TYPES"}
DECL = re.compile(r"^(func|type|const|var) ")
IDENT = re.compile(r"[A-Za-z_]\w*")
# Words that start a type, so "chan int" in a parameter list names no parameter.
TYPE_WORDS = {"chan", "func", "map", "struct", "interface"}

# A declaration and one of its members, "" for the declaration itself.
type Element = tuple[str, str]


def code(line: str) -> tuple[str, int]:
    """Return line without a trailing // comment, and how many more brackets it opens than it
    closes, leaving both alone inside a string or rune literal."""
    quote, depth, i = "", 0, 0
    while i < len(line):
        c = line[i]
        if quote:
            if c == "\\" and quote != "`":
                i += 1
            elif c == quote:
                quote = ""
        elif c in "\"'`":
            quote = c
        elif line.startswith("//", i):
            return line[:i], depth
        elif c in "([{":
            depth += 1
        elif c in ")]}":
            depth -= 1
        i += 1
    return line, depth


def closing(s: str, i: int) -> int:
    """Return the index of the bracket that closes the one at s[i]."""
    depth = 0
    for j in range(i, len(s)):
        depth += (s[j] in "([{") - (s[j] in ")]}")
        if depth == 0:
            return j
    raise ValueError(f"no bracket closes {s[i:]!r}")


def split_top(s: str) -> list[str]:
    """Split s at each comma outside brackets, dropping empty items such as a trailing comma's."""
    items, depth, start = [], 0, 0
    for j, c in enumerate(s + ","):
        depth += (c in "([{") - (c in ")]}")
        if c == "," and depth == 0:
            items.append(s[start:j].strip())
            start = j + 1
    return [item for item in items if item]


def types_of(params: str) -> str:
    """Return a parameter or result list as its types alone, so "a, b int" reads "int, int"."""
    items = split_top(params)

    def named(item: str) -> bool:
        head, _, rest = item.partition(" ")
        return bool(rest) and IDENT.fullmatch(head) is not None and head not in TYPE_WORDS

    if not any(map(named, items)):
        return ", ".join(items)
    types, sharing = [], 0
    for item in items:
        if named(item):
            types += [item.partition(" ")[2].strip()] * (sharing + 1)
            sharing = 0
        else:
            sharing += 1  # a name that takes the next item's type
    return ", ".join(types)


def signature(decl: str) -> str:
    """Return a func declaration or an interface method with the names of its receiver,
    parameters and results left out; return anything else, such as a field, unchanged."""
    prefix, s = "", decl
    if s.startswith("func "):
        prefix, s = "func ", s[5:]
        if s.startswith("("):
            end = closing(s, 0)
            prefix += f"({s[1:end].split()[-1]}) "
            s = s[end + 1:].lstrip()
    m = IDENT.match(s)
    if not m:
        return decl
    name, s = m.group(), s[m.end():]
    if s.startswith("["):
        end = closing(s, 0)
        name, s = name + s[:end + 1], s[end + 1:]
    if not s.startswith("("):
        return decl
    end = closing(s, 0)
    params, results = types_of(s[1:end]), s[end + 1:].strip()
    if results.startswith("(") and closing(results, 0) == len(results) - 1:
        results = f"({types_of(results[1:-1])})"
    return f"{prefix}{name}({params}) {results}".rstrip()


def members(body: list[str]) -> list[str]:
    """Return a block's members, one per entry however many lines it spans, comments and blank
    lines left out."""
    out, parts, depth = [], [], 0
    for raw in body:
        text, delta = code(raw)
        if text.strip():
            parts.append(text.strip())
        depth += delta
        if depth <= 0 and parts:
            out.append(" ".join(" ".join(parts).split()))
            parts, depth = [], 0
    return out


def block(head: str, body: list[str], close: str) -> Iterator[tuple[Element, bool]]:
    """Yield the elements a declaration spanning lines holds, each with whether it is an
    interface's member: a grouped declaration's members, a type's body, or one declaration."""
    if head in ("const (", "var ("):
        kind = head[:-2]
        entries = members(body)
        iota = kind == "const" and any("iota" in e for e in entries)
        for n, e in enumerate(entries):
            yield (single(f"{kind} {e}") + (f" [{n}]" if iota else ""), ""), False
    elif head.startswith("type ") and head.endswith("{"):
        kind = head[:-1].rstrip()
        interface = kind.endswith(" interface")
        yield (kind, ""), False
        for e in members(body):
            yield (kind, signature(e) if interface else e), interface
    else:  # a signature, or a value, written over several lines
        yield (single(" ".join(code(line)[0] for line in [head, *body, close])), ""), False


def single(line: str) -> str:
    """Return a one-line declaration as compared: a func by its signature's types, a variable
    without its initializer."""
    line = " ".join(line.split())
    if line.startswith("func "):
        return signature(line)
    if line.startswith("var "):
        return line.split(" = ", 1)[0]
    return line


def api(doc: str) -> tuple[set[Element], set[Element]]:
    """Read `go doc -all` output into the package's elements, and those that are an interface's
    members."""
    lines = doc.split("\n")
    i = next((n for n, line in enumerate(lines) if line in SECTIONS), len(lines))
    elements, methods = set(), set()
    while i < len(lines):
        head, depth = code(lines[i])
        head = head.rstrip()
        i += 1
        if not DECL.match(head):
            continue
        if depth <= 0:
            elements.add((single(head), ""))
            continue
        # A declaration ends where its brackets balance: a signature's ") (" opens its results.
        spanned = []
        while i < len(lines) and depth > 0:
            spanned.append(lines[i])
            depth += code(lines[i])[1]
            i += 1
        body, close = spanned[:-1], code(spanned[-1])[0].strip() if spanned else ""
        for element, member in block(head, body, close):
            elements.add(element)
            if member:
                methods.add(element)
    return elements, methods


def changes(package: str, base: str | None, head: str | None) -> list[str]:
    """Return what a package's API loses or changes from base to head, each `go doc -all` output
    or None where the package is absent, and each method head adds to an interface base has."""
    old, _ = api(base) if base is not None else (set(), set())
    new, methods = api(head) if head is not None else (set(), set())
    gone = old - new
    gone_decls = {d for d, m in gone if not m}
    out = [f"{package}: {d}: {m}" if m else f"{package}: {d}"
           for d, m in sorted(gone) if not (m and d in gone_decls)]
    out += [f"{package}: {d}: {m}  (added: every implementation outside the repository breaks)"
            for d, m in sorted((new - old) & methods) if (d, "") in old]
    return out


def run(cwd: Path, *args: str) -> str:
    return subprocess.run(args, cwd=cwd, check=True, capture_output=True, text=True,
                          errors="replace").stdout


def doc(tree: Path, package: str) -> str | None:
    """Return `go doc -all` for a package directory, or None when it holds no non-test Go file."""
    d = tree / package
    if not any(f.suffix == ".go" and not f.name.endswith("_test.go") for f in d.glob("*.go")):
        return None
    return run(tree, "go", "doc", "-all", f"./{package}")


@contextlib.contextmanager
def tree_at(root: Path, rev: str) -> Iterator[Path]:
    """Check rev out in a detached worktree, removed when the block ends."""
    with tempfile.TemporaryDirectory(prefix="exported-api-") as d:
        path = Path(d) / "tree"
        run(root, "git", "worktree", "add", "--detach", "--quiet", str(path), rev)
        try:
            yield path
        finally:
            subprocess.run(["git", "-C", str(root), "worktree", "remove", "--force", str(path)],
                           capture_output=True)


def listed(root: Path, base: str) -> list[str]:
    """Return what the working tree changes in the exported API of each public package it touches
    since its merge base with base."""
    mb = run(root, "git", "merge-base", base, "HEAD").strip()
    # A moved file names its source too, so the package it left is read.
    files = run(root, "git", "diff", "--no-renames", "--name-only", mb, "--", *PUBLIC_GO).split()
    packages = sorted({Path(f).parent.as_posix() for f in files})
    if not packages:
        return []
    with tree_at(root, mb) as old:
        return [line for p in packages for line in changes(p, doc(old, p), doc(root, p))]


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if args[:1] in (["-h"], ["--help"]):
        print(__doc__.split("\n\n")[1])
        return 0
    if len(args) > 1 or args[:1] and args[0].startswith("-"):  # git would read it as an option
        print(__doc__.split("\n\n")[1], file=sys.stderr)
        return 2
    base = args[0] if args else "origin/main"
    try:
        root = Path(run(Path.cwd(), "git", "rev-parse", "--show-toplevel").strip())
        found = listed(root, base)
    except subprocess.CalledProcessError as e:
        print(f"exported_api: {' '.join(e.cmd)}: {(e.stderr or '').strip()}", file=sys.stderr)
        return 2
    print("\n".join(found) if found else "exported_api: no exported API removed or changed")
    return 1 if found else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
