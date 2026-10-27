#!/usr/bin/env python3
"""Plant one defect, run the tests, and restore the file whatever happens.

Usage: python3 mutate.py FILE OLD NEW -- TEST-COMMAND...
       python3 mutate.py --batch MUTANTS -- TEST-COMMAND...

OLD must occur in FILE exactly once, read literally; NEW replaces it. TEST-COMMAND runs from the
current directory, such as `go test ./ir/...` or `python3 -m unittest discover -s DIR`, with
Python's bytecode cache off, since a mutant restored within the second it was written keeps the
size and mtime by which Python trusts bytecode cached from it. The verdict is read from `go test`
or unittest output: a kill needs a failing test, a survival a passing run that ran a test, so a
pattern matching no test gives no verdict. With the mutant in place a Go file's package and tests
must build, a Python file must parse, and every unittest module must load, or there is no
verdict. Prints KILLED, SURVIVED or UNVERIFIED, then every failing test line, then the file that
keeps the run's whole output, as instrumentation needs. Exit 0 KILLED, 1 SURVIVED, 2 UNVERIFIED or
a usage error.

MUTANTS is a JSON-lines file, one {"file", "old", "new"} object per line with an optional "name";
blank lines and lines starting with # are skipped, and a relative file is read from the current
directory. Every mutant is checked before any runs, and TEST-COMMAND must pass on the clean tree,
running a test, or no mutant can be judged. Prints a line per mutant and a tally, and keeps each
run's output in a directory, the baseline's as baseline.txt and the mutants' numbered in batch
order. Exit 0 when every mutant was killed, 1 when one survived, 2 when one has no verdict.
"""
from __future__ import annotations

import json
import os
import re
import signal
import subprocess
import sys
import tempfile
import traceback
from pathlib import Path

FAIL_LINE = re.compile(r"^\s*--- FAIL", re.M)
OK_LINE = re.compile(r"^ok\s", re.M)
NO_TESTS = "[no tests to run]"
NO_BUILD = re.compile(r"\[build failed\]|\[setup failed\]|^# ", re.M)
KILLED, SURVIVED, UNVERIFIED = 0, 1, 2
LABELS = ("KILLED", "SURVIVED", "UNVERIFIED")
FAILED_TEST = re.compile(r"^(?:--- FAIL: |FAIL: |ERROR: )(\S+)", re.M)
UNITTEST_RAN = re.compile(r"^Ran (\d+) tests? in ", re.M)
UNITTEST_FAIL = re.compile(r"^(?:FAIL|ERROR): .+", re.M)
UNITTEST_OK = re.compile(r"^OK\b", re.M)
NOT_LOADED = "unittest.loader._FailedTest"


def verdict(status: int, output: str) -> tuple[int, str]:
    """Judge a test run's exit status and output: a kill needs a failing test, a survival a
    passing package, and a build failure is never either."""
    ran = UNITTEST_RAN.search(output)
    if ran:
        return unittest_verdict(status, output, int(ran.group(1)))
    fails = [line for line in output.splitlines() if FAIL_LINE.match(line)]
    if NO_BUILD.search(output):
        lines = [line for line in output.splitlines() if NO_BUILD.search(line)][:10]
        return UNVERIFIED, "UNVERIFIED: a package did not build, so no verdict\n" + "\n".join(lines)
    if status == 0:
        oks = [line for line in output.splitlines() if OK_LINE.match(line)]
        ran = [line for line in oks if NO_TESTS not in line]
        if ran:
            return SURVIVED, f"SURVIVED: {len(ran)} package(s) passed with the mutant in place"
        if oks:
            return UNVERIFIED, "UNVERIFIED: no test ran; check the -run pattern"
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


def unittest_verdict(status: int, output: str, ran: int) -> tuple[int, str]:
    """Judge a unittest run as verdict judges go test's: a module that does not load is no
    verdict, a kill needs a failing or erroring test, and a survival a run that ran a test."""
    fails = UNITTEST_FAIL.findall(output)
    if NOT_LOADED in output:
        return UNVERIFIED, ("UNVERIFIED: a test module did not load, so no verdict\n"
                            + "\n".join(fails[:10]))
    if ran == 0:
        return UNVERIFIED, "UNVERIFIED: no test ran; check the start directory and pattern"
    if status == 0:
        if UNITTEST_OK.search(output):
            return SURVIVED, f"SURVIVED: {ran} test(s) passed with the mutant in place"
        return UNVERIFIED, "UNVERIFIED: the command passed but unittest reported no OK"
    if not fails:
        head = "\n".join(output.splitlines()[:15])
        return UNVERIFIED, ("UNVERIFIED: the command failed without a failing test; read its "
                            "output\n" + head)
    text = "KILLED\n" + "\n".join(fails)
    if any(f.startswith("ERROR:") for f in fails):
        text += ("\nnote: an error, not a failed assertion, ended a test; run the test written"
                 " for this property alone to see it pin")
    return KILLED, text


def run_tests(command: list[str]) -> subprocess.CompletedProcess:
    """Run the test command with Python's bytecode cache off, so no mutant outlives its restore
    as bytecode Python still trusts."""
    return subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
                          env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})


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


def mutate(path: Path, old: str, new: str, command: list[str], keep: Path) -> tuple[int, str]:
    """Plant the mutant, judge the run, and restore the file; keep receives the output of the
    build that failed or of the test command."""
    original = path.read_bytes()
    count = original.count(old.encode())
    if count != 1:
        reason = f"mutate: OLD occurs {count} times in {path}; widen it until it is unique"
        return UNVERIFIED, reason
    mutant = original.replace(old.encode(), new.encode(), 1)
    if mutant == original:
        return UNVERIFIED, "mutate: the replacement changed nothing"
    if path.suffix == ".py":
        try:
            compile(mutant, str(path), "exec")
        except SyntaxError as e:
            return UNVERIFIED, f"UNVERIFIED: the mutant is not Python ({e.msg}); adjust it"
    for sig in (signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupted)
    try:
        path.write_bytes(mutant)
        if path.suffix == ".go":
            ok, out = builds(path)
            if not ok:
                keep.write_text(out)
                head = "\n".join(out.splitlines()[:15])
                reason = "UNVERIFIED: the mutant does not build or vet; adjust it until it compiles"
                return UNVERIFIED, reason + "\n" + head
        run = run_tests(command)
        keep.write_text(run.stdout)
        return verdict(run.returncode, run.stdout)
    finally:
        path.write_bytes(original)
        if path.read_bytes() != original:
            print(f"mutate: RESTORE FAILED, {path} still holds the mutant", file=sys.stderr)


def load_batch(path: Path) -> tuple[list[dict], list[str]]:
    """Read a batch of mutants; return them and every problem found, so that a batch with any
    problem runs nothing."""
    mutants, problems = [], []
    for n, line in enumerate(path.read_text().splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        where = f"{path}:{n}"
        try:
            m = json.loads(line)
        except json.JSONDecodeError as e:
            problems.append(f"{where}: not JSON: {e.msg}")
            continue
        if not isinstance(m, dict) or not all(isinstance(m.get(k), str)
                                              for k in ("file", "old", "new")):
            problems.append(f"{where}: needs the string fields file, old and new")
            continue
        target = Path(m["file"]).resolve()
        if not target.is_file():
            problems.append(f"{where}: {m['file']} is not a file")
            continue
        count = target.read_bytes().count(m["old"].encode())
        if count != 1 or m["old"] == m["new"]:
            problems.append(f"{where}: OLD occurs {count} times in {m['file']}" if count != 1
                            else f"{where}: the replacement changes nothing")
            continue
        mutants.append({"file": target, "old": m["old"], "new": m["new"],
                        "name": m.get("name") or f"{m['file']} (line {n})"})
    if not mutants and not problems:
        problems.append(f"{path}: holds no mutant")
    return mutants, problems


def summary(code: int, text: str, name: str) -> str:
    """One line for a mutant's verdict: the tests that killed it, or why there is no verdict."""
    if code == KILLED:
        tests = list(dict.fromkeys(FAILED_TEST.findall(text)))
        detail = ", ".join(tests) if tests else "a subtest failed"
    elif code == SURVIVED:
        detail = "no test noticed"
    else:
        detail = text.split("\n", 1)[0].split(": ", 1)[-1]
    return f"{LABELS[code]:<10} {name}: {detail}"


def batch(path: Path, command: list[str]) -> int:
    """Run every mutant in a batch after a clean baseline; return the worst verdict."""
    mutants, problems = load_batch(path)
    if problems:
        print("\n".join(problems), file=sys.stderr)
        return UNVERIFIED
    outputs = Path(tempfile.mkdtemp(prefix="mutate-"))
    run = run_tests(command)
    (outputs / "baseline.txt").write_text(run.stdout)
    code, text = verdict(run.returncode, run.stdout)
    if code != SURVIVED:
        print("mutate: the command must pass on the clean tree, running a test, before any mutant"
              " can be judged\n" + text, file=sys.stderr)
        return UNVERIFIED
    tally = [0, 0, 0]
    for n, m in enumerate(mutants, 1):
        code, text = mutate(m["file"], m["old"], m["new"], command, outputs / f"{n}.txt")
        tally[code] += 1
        print(summary(code, text, m["name"]), flush=True)
    print(f"mutate: {tally[KILLED]} killed, {tally[SURVIVED]} survived, "
          f"{tally[UNVERIFIED]} unverified; each run's output is in {outputs}")
    return UNVERIFIED if tally[UNVERIFIED] else SURVIVED if tally[SURVIVED] else KILLED


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if args[:1] == ["--batch"]:
        if len(args) < 4 or args[2] != "--" or not Path(args[1]).is_file():
            print(__doc__.split("\n\n")[1], file=sys.stderr)
            return UNVERIFIED
        try:
            return batch(Path(args[1]), args[3:])
        except KeyboardInterrupt:
            print("mutate: interrupted; every file is restored", file=sys.stderr)
            return UNVERIFIED
    if len(args) < 5 or args[3] != "--":
        print(__doc__.split("\n\n")[1], file=sys.stderr)
        return UNVERIFIED
    path = Path(args[0]).resolve()
    if not path.is_file():
        print(f"mutate: {args[0]} is not a file", file=sys.stderr)
        return UNVERIFIED
    keep = Path(tempfile.mkdtemp(prefix="mutate-")) / "output.txt"
    try:
        code, text = mutate(path, args[1], args[2], args[4:], keep)
    except KeyboardInterrupt:
        code, text = UNVERIFIED, "mutate: interrupted; the file is restored"
    print(text, file=sys.stderr if text.startswith("mutate:") else sys.stdout)
    if keep.is_file():
        print(f"output: {keep}")
    return code


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
