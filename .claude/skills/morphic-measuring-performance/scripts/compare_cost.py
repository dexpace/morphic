#!/usr/bin/env python3
"""Build a command at a base revision and at the working tree, run the two builds in turn, and
report what each run cost: instructions retired, CPU time, peak memory and wall time.

Usage: python3 compare_cost.py [-b BASE] [-n RUNS] [-p PACKAGE] -- ARGS...

PACKAGE (default ./cmd/morphic) is built at BASE (default origin/main) and at the working tree,
and each build runs with ARGS RUNS times (default 7), base and head alternating, after one
uncounted run each; stdout is discarded. For the golden spec, pass
compile "$(python3 .claude/skills/morphic-verifying-changes/scripts/golden_spec.py)".
Instructions retired come from /usr/bin/time -l on macOS and perf stat on Linux; without either,
the instructions row is left out. Prints each measure's median and range per side, and head's
median over base's. A side whose runs exit differently from each other, or from the other side,
is reported, and so is a BASE the working tree's HEAD does not hold, since the difference then
counts what BASE gained too. It judges nothing: exit 0 when measured, 2 when it cannot run.
"""
from __future__ import annotations

import argparse
import os
import platform
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
import traceback
from pathlib import Path

# The warning about a base the branch does not hold is the corpus sweep's, so the two never differ.
VERIFYING = Path(__file__).resolve().parents[2] / "morphic-verifying-changes" / "scripts"
sys.path.insert(0, str(VERIFYING))
from sweep import unheld_base_warning  # noqa: E402  (a script of the verifying skill)

INSTRUCTIONS = {
    "Darwin": re.compile(r"^\s*(\d+)\s+instructions retired", re.M),
    "Linux": re.compile(r"^(\d+),[^,]*,instructions", re.M),
}


def wrapper(system: str) -> list[str]:
    """Return the command that reports instructions retired on this system, or nothing."""
    if system == "Darwin" and Path("/usr/bin/time").is_file():
        return ["/usr/bin/time", "-l"]
    if system == "Linux" and shutil.which("perf"):
        return ["perf", "stat", "-x,", "-e", "instructions:u", "--"]
    return []


def instructions(stderr: str, system: str) -> int | None:
    pattern = INSTRUCTIONS.get(system)
    m = pattern.search(stderr) if pattern else None
    return int(m.group(1)) if m else None


def measure(command: list[str], wrap: list[str], system: str) -> dict:
    """Run command once and return its exit status and what it cost."""
    with open(os.devnull, "wb") as null, tempfile.TemporaryFile() as err:
        start = time.monotonic()
        proc = subprocess.Popen(wrap + command, stdout=null, stderr=err)
        _, status, usage = os.wait4(proc.pid, 0)
        wall = time.monotonic() - start
        proc.returncode = os.waitstatus_to_exitcode(status)
        err.seek(0)
        text = err.read().decode(errors="replace")
    rss = usage.ru_maxrss * (1 if system == "Darwin" else 1024)  # bytes on macOS, KiB elsewhere
    return {"status": proc.returncode, "instructions": instructions(text, system),
            "cpu": usage.ru_utime + usage.ru_stime, "rss": rss, "wall": wall}


def spread(values: list[float]) -> tuple[float, float, float]:
    """Return the median, the least and the greatest of values."""
    return statistics.median(values), min(values), max(values)


def human(value: float, unit: str) -> str:
    if unit == "count":
        return f"{value / 1e9:.3f}G" if value >= 1e9 else f"{value / 1e6:.3f}M"
    if unit == "bytes":
        return f"{value / 2**20:.1f}MiB"
    return f"{value:.3f}s"


def table(base: list[dict], head: list[dict]) -> list[str]:
    """Render each measure's median and range per side, and head's median over base's."""
    out = [f"{'':14}{'base median [range]':34}{'head median [range]':34}head/base"]
    rows = [("instructions", "instructions", "count"), ("cpu", "cpu", "s"),
            ("peak memory", "rss", "bytes"), ("wall", "wall", "s")]
    for label, key, unit in rows:
        if any(r[key] is None for r in base + head):
            continue
        cells = []
        for side in (base, head):
            m, lo, hi = spread([r[key] for r in side])
            cells.append(f"{human(m, unit)} [{human(lo, unit)}..{human(hi, unit)}]")
        ratio = spread([r[key] for r in head])[0] / max(spread([r[key] for r in base])[0], 1e-12)
        out.append(f"{label:14}{cells[0]:34}{cells[1]:34}{ratio:.3f}")
    for name, side in (("base", base), ("head", head)):
        codes = sorted({r["status"] for r in side})
        if len(codes) > 1:
            out.append(f"{name} runs exited differently: {codes}")
    if {r["status"] for r in base} != {r["status"] for r in head}:
        out.append("base and head exit differently, so they did different work")
    return out


def build(src: Path, package: str, dest: Path) -> None:
    subprocess.run(["go", "build", "-o", str(dest), package], cwd=src, check=True,
                   capture_output=True, text=True)


def main(argv: list[str] | None = None) -> int:
    argv = sys.argv[1:] if argv is None else argv
    rest = argv[argv.index("--") + 1:] if "--" in argv else []
    p = argparse.ArgumentParser(description="Compare what two builds of a command cost.")
    p.add_argument("-b", dest="base", default="origin/main")
    p.add_argument("-n", dest="runs", type=int, default=7)
    p.add_argument("-p", dest="package", default="./cmd/morphic")
    args = p.parse_args(argv[:argv.index("--")] if "--" in argv else argv)
    if args.runs < 1:
        p.error("-n must be at least 1")
    out = Path(tempfile.mkdtemp(prefix="compare-cost-"))
    try:
        return compare(args, rest, out)
    finally:
        shutil.rmtree(out, ignore_errors=True)  # two builds of the command, not worth keeping


def compare(args: argparse.Namespace, rest: list[str], out: Path) -> int:
    """Build both sides under out, run each with rest in turn, and print what each cost."""
    system = platform.system()
    src = out / "base-src"
    try:
        root = Path(subprocess.run(["git", "rev-parse", "--show-toplevel"], check=True,
                                   capture_output=True, text=True).stdout.strip())
        sha = subprocess.run(["git", "-C", str(root), "rev-parse", "--verify",
                              f"{args.base}^{{commit}}"], check=True, capture_output=True,
                             text=True).stdout.strip()
        warning = unheld_base_warning(root, sha, "compare_cost")
        if warning:
            print(warning, file=sys.stderr)
        build(root, args.package, out / "head")
        subprocess.run(["git", "-C", str(root), "worktree", "add", "--detach", "--quiet",
                        str(src), sha], check=True, capture_output=True, text=True)
        build(src, args.package, out / "base")
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        print(f"compare_cost: {getattr(e, 'stderr', '') or e}", file=sys.stderr)
        return 2
    finally:
        subprocess.run(["git", "worktree", "remove", "--force", str(src)], capture_output=True)
    wrap = wrapper(system)
    for side in ("base", "head"):  # one uncounted run each, so neither pays for a cold cache
        measure([str(out / side), *rest], wrap, system)
    runs: dict[str, list[dict]] = {"base": [], "head": []}
    for _ in range(args.runs):
        for side in ("base", "head"):
            runs[side].append(measure([str(out / side), *rest], wrap, system))
    print(f"compare_cost: base {sha[:12]}, head the working tree; {args.runs} runs each, "
          f"alternating: {args.package} {' '.join(rest)}")
    print("\n".join(table(runs["base"], runs["head"])))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
