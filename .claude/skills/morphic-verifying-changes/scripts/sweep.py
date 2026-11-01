#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Compile every spec in a corpus with cmd/morphic built at a base revision and at the working
tree, and print each side's exit codes and each spec whose exit code, IR or diagnostics differ.

Usage: python3.14 .claude/skills/morphic-verifying-changes/scripts/sweep.py [-b BASE] [-t SECONDS]
       [-j JOBS] [-o OUTDIR] [CORPUS...] [-- COMPILE-FLAGS...]

BASE is the revision to compare against (default origin/main); when HEAD does not hold it, a
warning says the differences also count what BASE gained since. A compile running past SECONDS
(default 60) is killed and recorded as 137, and one ended by a signal, such as the kill that ends
a compile out of memory, as minus the signal's number. JOBS compiles run at once (default half the
CPUs), each holding its output in memory until it is compared. OUTDIR defaults to a new temporary
directory, and a run replaces its runs/ directory: a spec that differs keeps both sides' outputs
under OUTDIR/runs/<n>/ for diffing, and when they cannot be written the sweep goes on and says so,
as nothing it compares is on disk. OUTDIR holds a row per spec in results.tsv: its number, both
exit codes, what differs and its path. The IR version a document opens with is set aside before
comparing, so a bump lists only the specs that moved: the summary gives it once, and a spec
differing in nothing else is "stamp" in results.tsv and keeps no outputs. The .yaml, .yml and
.json files CORPUS names, and those under its directories (default testdata), are read for OpenAPI
and Swagger documents: a JSON one, even one saved as .yaml or .yml, by its top-level keys, minified
or not, and a YAML one by a version key anywhere in it, so a file naming one in a comment or a
nested mapping is compiled too, and fails alike on both sides. The sweep reports facts and judges
none: specs that fail alike on both sides agree, so read the exit codes before taking agreement
for a pass. Exit 0 when no spec differs but for its stamp, 1 when one does, 2 when the sweep cannot
run.
"""

import argparse
import concurrent.futures
import contextlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import traceback
from collections import Counter
from pathlib import Path
from typing import NamedTuple

# A version key, wherever it is written: YAML can put one after a "{" or a ",", an anchor or a tag,
# or a "? ". A plain one is ended by a colon a space or the end of the file follows, so
# "openapi:schema" is another key, while a quoted one, as JSON writes it, needs no space. A key
# spelled through an escape or a folded line is still missed, as only a YAML reader would see it.
SPEC = re.compile(
    rb"""(?<![\w.-])(?:(?:openapi|swagger)\s*:(?:\s|$)|["'](?:openapi|swagger)["']\s*:)""")
# The exit recorded for a compile killed at the time limit, as a shell reports a SIGKILL.
KILLED = 137
# The member every document opens with, read as text: a document can run to hundreds of megabytes,
# and an output that opens otherwise, as another flag's may, is simply compared whole.
STAMP = re.compile(rb'\{\s*"irVersion":\s*"([^"]*)"')
# What results.tsv says of a spec whose outputs differ in nothing but the stamp.
STAMP_ALONE = "stamp"


class Output(NamedTuple):
    """What one build made of one spec."""
    code: int
    ir: bytes
    diag: bytes


class Result(NamedTuple):
    """One spec's comparison, each pair base first: stamps holds the two versions when they
    differ, and unkept why the spec's outputs could not be written."""
    n: int
    spec: Path
    codes: tuple[int, int]
    sizes: tuple[int, int]
    what: str
    stamps: tuple[str, str] | None
    unkept: str


def is_spec(data: bytes) -> bool:
    """Report whether a document is OpenAPI or Swagger: one written as JSON, which YAML files
    may hold too, by its top-level keys, and any other, JSON too deep or malformed to parse among
    it, by a version key anywhere in it. A YAML reader would settle a key's depth, but the
    standard library has none, and a file left out is a spec neither build is asked about, where
    one let in only fails alike. A byte-order mark before either is read past, as the compiler
    reads past it."""
    data = data.removeprefix(b"\xef\xbb\xbf")
    if data.lstrip()[:1] == b"{":
        try:
            doc = json.loads(data)
        except (ValueError, RecursionError):
            pass  # read as text below, as a malformed fixture still is a spec to compile
        else:
            return "openapi" in doc or "swagger" in doc
    return SPEC.search(data) is not None


def find_specs(corpus: list[Path]) -> list[Path]:
    """Return the OpenAPI and Swagger documents among the .yaml, .yml and .json files corpus names
    and those under its directories."""
    out = []
    for d in corpus:
        for p in [d] if d.is_file() else sorted(d.rglob("*")):
            if p.suffix in (".yaml", ".yml", ".json") and p.is_file() and \
                    is_spec(p.read_bytes()):
                out.append(p.resolve())
    return out


def compile_one(binary: Path, spec: Path, flags: list[str], limit: int) -> Output:
    """Compile spec with binary and return what it wrote. The output comes back through pipes, as
    a compile writing to a full disk would fail, and read as a difference between the builds."""
    try:
        done = subprocess.run([str(binary), "compile", *flags, "--", str(spec)],
                              capture_output=True, timeout=limit)
    except subprocess.TimeoutExpired as e:
        return Output(KILLED, e.stdout or b"", e.stderr or b"")
    return Output(done.returncode, done.stdout, done.stderr)


def split_stamp(ir: bytes) -> tuple[str | None, bytes]:
    """Return the IR version a document opens with and the bytes after it, or None and the whole
    output when it opens with none. An empty version is a version, so it is compared as one."""
    m = STAMP.match(ir)
    return (m[1].decode(errors="replace"), ir[m.end():]) if m else (None, ir)


def difference(b: Output, h: Output) -> tuple[str, tuple[str, str] | None]:
    """Return what differs between two builds' outputs, and their versions when those differ."""
    base_stamp, base_rest = split_stamp(b.ir)
    head_stamp, head_rest = split_stamp(h.ir)
    what = [] if b.code == h.code else [f"exit({b.code}->{h.code})"]
    if base_rest != head_rest:
        what.append("ir")
    if b.diag != h.diag:
        what.append("diagnostics")
    both = base_stamp is not None and head_stamp is not None
    stamps = (base_stamp, head_stamp) if both and base_stamp != head_stamp else None
    return " ".join(what) or (STAMP_ALONE if stamps else ""), stamps


def keep(run: Path, b: Output, h: Output) -> str:
    """Write a spec's outputs under run for diffing; on failure leave none of them, and return why,
    so that one disk's limit costs a diff and never a result."""
    try:
        run.mkdir(parents=True, exist_ok=True)
        for side, output in (("base", b), ("head", h)):
            (run / f"{side}.ir").write_bytes(output.ir)
            (run / f"{side}.diag").write_bytes(output.diag)
    except OSError as e:
        shutil.rmtree(run, ignore_errors=True)
        return str(e)
    return ""


def compare(base: Path, head: Path, specs: list[Path], out: Path, flags: list[str],
            limit: int, jobs: int) -> list[Result]:
    """Run both binaries over every spec, keeping the outputs of each that differs but for its
    stamp, which is what a later diff can use."""
    def one(item):
        n, spec = item
        b = compile_one(base, spec, flags, limit)
        h = compile_one(head, spec, flags, limit)
        what, stamps = difference(b, h)
        unkept = keep(out / "runs" / str(n), b, h) if what not in ("", STAMP_ALONE) else ""
        return Result(n, spec, (b.code, h.code), (len(b.ir), len(h.ir)), what, stamps, unkept)

    # A number names another spec once the corpus changes, so no earlier run's pair may stand.
    shutil.rmtree(out / "runs", ignore_errors=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=jobs) as pool:
        return list(pool.map(one, enumerate(specs, 1)))


def write_table(results: list[Result], table: Path, rel) -> str:
    """Write a row per spec to table; return where they are, or why they are not."""
    rows = "".join(f"{r.n}\t{r.codes[0]}\t{r.codes[1]}\t{r.what or '-'}\t{rel(r.spec)}\n"
                   for r in results)
    try:
        table.write_text("n\tbase\thead\tdiffers\tspec\n" + rows)
    except OSError as e:
        with contextlib.suppress(OSError):  # a table cut short reads as a sweep of fewer specs
            table.unlink()
        return f"no rows written: {e}"
    return f"a row per spec in {table}"


def report(results: list[Result], root: Path, out: Path) -> int:
    def rel(p: Path) -> str:
        return p.relative_to(root).as_posix() if p.is_relative_to(root) else str(p)

    print(f"sweep: {len(results)} specs; {write_table(results, out / 'results.tsv', rel)}")
    for i, side in enumerate(("base", "head")):
        dist = Counter(r.codes[i] for r in results)
        size = sum(r.sizes[i] for r in results)
        codes = " ".join(f"{code}x{count}" for code, count in sorted(dist.items()))
        print(f"  {side} exit codes: {codes}; IR bytes: {size}")
    for (old, new), count in sorted(Counter(r.stamps for r in results if r.stamps).items()):
        alone = sum(r.stamps == (old, new) and r.what == STAMP_ALONE for r in results)
        print(f"sweep: irVersion {old} -> {new} on {count} of {len(results)} specs, set aside "
              f"below; {alone} of them differ in nothing else")
    differ = [r for r in results if r.what not in ("", STAMP_ALONE)]
    if not differ:
        print("sweep: no spec's exit code, IR or diagnostics differ")
        return 0
    print(f"sweep: {len(differ)} of {len(results)} specs differ (n, then what):")
    for r in differ:
        print(f"  {r.n}  {rel(r.spec)}: {r.what}")
    print(f"compare one with: diff {out}/runs/<n>/base.ir {out}/runs/<n>/head.ir, and the "
          ".diag pair for its diagnostics")
    unkept = [r for r in differ if r.unkept]
    if unkept:
        print(f"sweep: the outputs of {len(unkept)} of them were not kept: {unkept[0].unkept}")
    return 1


def git(root: Path, *args: str) -> str:
    done = subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True,
                          text=True, errors="replace")
    return done.stdout.strip()


def unheld_base_warning(root: Path, sha: str) -> str | None:
    """Return a warning when HEAD does not hold the base, so the comparison also counts the
    commits the base gained since the branch left it."""
    held = subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", sha, "HEAD"],
                          capture_output=True).returncode == 0
    if held:
        return None
    return (f"sweep: HEAD does not hold {sha[:12]}, so every difference includes what the base "
            "gained since the branch left it; merge it in, or pass -b $(git merge-base HEAD "
            "origin/main)")


def resolve_base(base: str) -> tuple[Path, str]:
    """Return the repository root the current directory is in, and the commit base names."""
    root = Path(git(Path.cwd(), "rev-parse", "--show-toplevel"))
    return root, git(root, "rev-parse", "--verify", f"{base}^{{commit}}")


def build_pair(root: Path, sha: str, out: Path) -> tuple[Path, Path]:
    """Build cmd/morphic at commit sha and at the working tree, as out/base and out/head, the base
    in a detached worktree removed afterwards; warn when HEAD does not hold sha."""
    warning = unheld_base_warning(root, sha)
    if warning:
        print(warning, file=sys.stderr)
    src = out / "base-src"
    remove = ["git", "-C", str(root), "worktree", "remove", "--force", str(src)]
    subprocess.run(remove, capture_output=True)  # a crashed run's worktree, if any
    try:
        build(root, "./cmd/morphic", out / "head")
        git(root, "worktree", "add", "--detach", "--quiet", str(src), sha)
        build(src, "./cmd/morphic", out / "base")
    finally:
        subprocess.run(remove, capture_output=True)
    return out / "base", out / "head"


def build(src: Path, package: str, dest: Path) -> None:
    subprocess.run(["go", "build", "-o", str(dest), package], cwd=src, check=True,
                   capture_output=True, text=True, errors="replace")


def parse(argv: list[str]) -> argparse.Namespace:
    flags = []
    if "--" in argv:
        i = argv.index("--")
        argv, flags = argv[:i], argv[i + 1:]
    # --help prints the docstring, whose usage line names the compile flags and whose prose says
    # what each option does; argparse's own usage would leave the flags out.
    p = argparse.ArgumentParser(usage=argparse.SUPPRESS, description=__doc__,
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("-b", dest="base", metavar="BASE", default="origin/main")
    p.add_argument("-t", dest="limit", metavar="SECONDS", type=int, default=60)
    p.add_argument("-j", dest="jobs", metavar="JOBS", type=int,
                   default=max(1, (os.process_cpu_count() or 2) // 2))
    p.add_argument("-o", dest="out", metavar="OUTDIR")
    p.add_argument("corpus", metavar="CORPUS", nargs="*")
    args = p.parse_args(argv)
    if args.limit < 1 or args.jobs < 1:
        p.error("-t and -j must be at least 1")
    args.flags = flags
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse(sys.argv[1:] if argv is None else argv)
    try:
        root, sha = resolve_base(args.base)
    except subprocess.CalledProcessError as e:
        print(f"sweep: {e.stderr.strip() or e}", file=sys.stderr)
        return 2
    try:
        out = Path(args.out or tempfile.mkdtemp()).resolve()
        out.mkdir(parents=True, exist_ok=True)
    except OSError as e:
        print(f"sweep: {e}", file=sys.stderr)
        return 2
    corpus = [Path(c).resolve() for c in args.corpus] or [root / "testdata"]
    specs = find_specs(corpus)
    if not specs:
        where = " ".join(map(str, corpus))
        print(f"sweep: no OpenAPI or Swagger documents under {where}", file=sys.stderr)
        return 2
    print(f"sweep: building head from the working tree and base at {sha[:12]}")
    try:
        base, head = build_pair(root, sha, out)
    except subprocess.CalledProcessError as e:
        print(f"sweep: {(e.stderr or '').strip() or e}", file=sys.stderr)
        return 2
    results = compare(base, head, specs, out, args.flags, args.limit, args.jobs)
    return report(results, root, out)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
