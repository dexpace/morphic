#!/usr/bin/env python3
"""Plant one defect, run the tests, and restore the file whatever happens.

Usage: python3 mutate.py FILE OLD NEW -- TEST-COMMAND...

OLD must occur in FILE exactly once, read literally; NEW replaces it. TEST-COMMAND runs from the
current directory, such as `go test ./ir/...`, and the verdict is read from `go test` output: a
kill needs a `--- FAIL` line, a survival an `ok` line. A Go file's package and tests must build
with the mutant in place, or there is no verdict. Prints KILLED, SURVIVED or UNVERIFIED, then every
failing test line. Exit 0 KILLED, 1 SURVIVED, 2 UNVERIFIED or a usage error.
"""
from __future__ import annotations

import re
import signal
import subprocess
import sys
from pathlib import Path

FAIL_LINE = re.compile(r"^\s*--- FAIL", re.M)
OK_LINE = re.compile(r"^ok\s", re.M)
NO_BUILD = re.compile(r"\[build failed\]|\[setup failed\]|^# ", re.M)
KILLED, SURVIVED, UNVERIFIED = 0, 1, 2


def verdict(status: int, output: str) -> tuple[int, str]:
    """Judge a test run's exit status and output: a kill needs a failing test, a survival a
    passing package, and a build failure is never either."""
    fails = [line for line in output.splitlines() if FAIL_LINE.match(line)]
    if NO_BUILD.search(output):
        lines = [line for line in output.splitlines() if NO_BUILD.search(line)][:10]
        return UNVERIFIED, "UNVERIFIED: a package did not build, so no verdict\n" + "\n".join(lines)
    if status == 0:
        oks = len(OK_LINE.findall(output))
        if oks:
            return SURVIVED, f"SURVIVED: {oks} package(s) passed with the mutant in place"
        return UNVERIFIED, "UNVERIFIED: the command passed but no test package reported ok"
    if not fails:
        head = "\n".join(output.splitlines()[:15])
        reason = "UNVERIFIED: the command failed without a failing test; read its output\n"
        return UNVERIFIED, reason + head
    text = "KILLED\n" + "\n".join(fails)
    if re.search(r"^panic:", output, re.M):
        text += ("\nnote: a panic ended the run; run the test written for this property alone"
                 " to see it pin")
    return KILLED, text


def builds(path: Path) -> tuple[bool, str]:
    """Report whether a Go file's package and its tests build and vet with the file as it is."""
    root = Path(subprocess.run(["git", "-C", str(path.parent), "rev-parse", "--show-toplevel"],
                               check=True, capture_output=True, text=True).stdout.strip())
    pkg = "./" + path.parent.relative_to(root).as_posix()
    run = subprocess.run(["go", "test", "-count=1", "-run", "^$", pkg], cwd=root,
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    return run.returncode == 0, run.stdout


def interrupted(signum, frame):
    raise SystemExit(UNVERIFIED)


def mutate(path: Path, old: str, new: str, command: list[str]) -> tuple[int, str]:
    original = path.read_bytes()
    count = original.count(old.encode())
    if count != 1:
        reason = f"mutate: OLD occurs {count} times in {path}; widen it until it is unique"
        return UNVERIFIED, reason
    mutant = original.replace(old.encode(), new.encode(), 1)
    if mutant == original:
        return UNVERIFIED, "mutate: the replacement changed nothing"
    for sig in (signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupted)
    try:
        path.write_bytes(mutant)
        if path.suffix == ".go":
            ok, out = builds(path)
            if not ok:
                head = "\n".join(out.splitlines()[:15])
                reason = "UNVERIFIED: the mutant does not build or vet; adjust it until it compiles"
                return UNVERIFIED, reason + "\n" + head
        run = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        return verdict(run.returncode, run.stdout)
    finally:
        path.write_bytes(original)
        if path.read_bytes() != original:
            print(f"mutate: RESTORE FAILED, {path} still holds the mutant", file=sys.stderr)


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if len(args) < 5 or args[3] != "--":
        print(__doc__.split("\n\n")[1], file=sys.stderr)
        return UNVERIFIED
    path = Path(args[0]).resolve()
    if not path.is_file():
        print(f"mutate: {args[0]} is not a file", file=sys.stderr)
        return UNVERIFIED
    try:
        code, text = mutate(path, args[1], args[2], args[4:])
    except KeyboardInterrupt:
        code, text = UNVERIFIED, "mutate: interrupted; the file is restored"
    print(text, file=sys.stderr if text.startswith("mutate:") else sys.stdout)
    return code


if __name__ == "__main__":
    sys.exit(main())
