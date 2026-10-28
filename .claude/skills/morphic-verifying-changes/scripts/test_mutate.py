"""Tests for mutate.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts
"""
import contextlib
import errno
import importlib.util
import io
import json
import os
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

import mutate

HERE = Path(__file__).resolve().parent
# A stand-in test command: it fails like `go test` when the file holds "bad", passes otherwise.
FAKE_TEST = (
    "import sys; text = open(sys.argv[1]).read()\n"
    "if 'bad' in text: print('--- FAIL: TestThing (0.00s)'); print('FAIL'); sys.exit(1)\n"
    "print('ok  \tfake/pkg\t0.1s')"
)
# The same failure behind an exit status of 0 and another package's pass, as a pipe gives.
MASKED_TEST = FAKE_TEST.replace("sys.exit(1)", "print('ok  \tfake/other\t0.1s'); sys.exit(0)")


def quietly(fn, *args):
    """Call fn, returning its result and everything it printed, so no run reaches the console."""
    out = io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(out):
        return fn(*args), out.getvalue()


def full(write, path, data):
    """Fail as a full disk does once a write has begun: the file emptied, then an error."""
    write(path, b"")
    raise OSError(errno.ENOSPC, "No space left on device")


def torn(write, path, data):
    """Write half of data and report success, as when another writer races the write."""
    write(path, data[: len(data) // 2])


def failing(when, fail):
    """Patch Path.write_bytes so that each write when(path, data) picks goes to fail instead."""
    write = Path.write_bytes

    def write_bytes(path, data):
        return fail(write, path, data) if when(path, data) else write(path, data)

    return mock.patch.object(Path, "write_bytes", write_bytes)


def copy_named(printed: str) -> Path:
    """Return the copy of the original a failed restore names."""
    return Path(printed.split("copy it back from ", 1)[1].split("\n", 1)[0])


class VerdictTest(unittest.TestCase):
    def test_each_outcome(self):
        cases = [
            (1, "--- FAIL: TestA (0.00s)\n    --- FAIL: TestA/sub (0.00s)\nFAIL\n", mutate.KILLED),
            (0, "ok  \tpkg\t0.1s\n", mutate.SURVIVED),
            (0, "?   \tpkg\t[no test files]\n", mutate.UNVERIFIED),
            (0, "ok  \tpkg\t0.1s [no tests to run]\n", mutate.UNVERIFIED),
            (0, "ok  \tpkg\t0.1s\tcoverage: 0.0% of statements [no tests to run]\n",
             mutate.UNVERIFIED),
            (0, "ok  \tpkg\t0.1s [no tests to run]\nok  \tpkg2\t0.1s\n", mutate.SURVIVED),
            (1, "# pkg\nx.go:1: undefined: y\nFAIL\tpkg [build failed]\n", mutate.UNVERIFIED),
            (1, "FAIL\t./nope [setup failed]\n", mutate.UNVERIFIED),
            # A package that does not build beside one whose test fails is no verdict.
            (1, "--- FAIL: TestA (0.00s)\nFAIL\tpkga\t0.1s\n# pkgb\nb.go:1: undefined: y\n"
                "FAIL\tpkgb [build failed]\nFAIL\n", mutate.UNVERIFIED),
            # A failing test whose message quotes what go test or unittest writes still fails.
            (1, "--- FAIL: TestA (0.00s)\n    a_test.go:9: want FAIL\tpkg [build failed]\n"
                "FAIL\tpkga\t0.1s\nFAIL\n", mutate.KILLED),
            # A heading the code under test prints is no build failure's header.
            (1, "# Heading the code printed\n--- FAIL: TestG (0.00s)\nFAIL\nFAIL\tx/b\t0.6s\n",
             mutate.KILLED),
            # Each header go writes for a package that did not build, seen where a pipe cut the
            # FAIL line: a package, its vet run, and a test file of it or of its external tests.
            (0, "# x/a\na.go:1: undefined: y\nok  \tx/b\t0.1s\n", mutate.UNVERIFIED),
            (0, "# [x/a]\na.go:5: bad format\nok  \tx/b\t0.1s\n", mutate.UNVERIFIED),
            (0, "# x/c_test [x/c.test]\nc_test.go:5: undefined: z\nok  \tx/b\t0.1s\n",
             mutate.UNVERIFIED),
            (1, "FAIL: test_x (m.T.test_x) (output='ERROR: test_m (unittest.loader._FailedTest)')"
                "\nRan 3 tests in 0.1s\n\nFAILED (failures=1)\n", mutate.KILLED),
            (1, "exit status 3\n", mutate.UNVERIFIED),
            (0, "...\nRan 3 tests in 0.1s\n\nOK\n", mutate.SURVIVED),
            (0, "\nRan 0 tests in 0.000s\n\nOK\n", mutate.UNVERIFIED),
            # A run that exits 0 without unittest's OK, as one cut short behind a pipe.
            (0, "\nRan 2 tests in 0.1s\n", mutate.UNVERIFIED),
            (0, "\nRan 0 tests in 0.0s\n\nOK\n\nRan 2 tests in 0.1s\n\nOK\n", mutate.SURVIVED),
            (0, "\nRan 2 tests in 0.1s\n\nOK\n\nRan 0 tests in 0.0s\n\nOK\n", mutate.SURVIVED),
            (5, "\nRan 0 tests in 0.000s\n\nNO TESTS RAN\n", mutate.UNVERIFIED),
            (1, "FAIL: test_x (m.T)\nRan 3 tests in 0.1s\n\nFAILED (failures=1)\n", mutate.KILLED),
            (1, "ERROR: test_m (unittest.loader._FailedTest)\nRan 1 test in 0.0s\n\n"
                "FAILED (errors=1)\n", mutate.UNVERIFIED),
            (1, "Traceback\nRan 3 tests in 0.1s\n", mutate.UNVERIFIED),
            # A pipe or ; hands back the status of a command that is not the tests.
            (0, "--- FAIL: TestA (0.00s)\nFAIL\nFAIL\tpkg\t0.1s\nok  \tpkg2\t0.1s\n",
             mutate.UNVERIFIED),
            (0, "FAIL: test_x (m.T)\nRan 1 test in 0.0s\n\nFAILED (failures=1)\n"
                "\nRan 2 tests in 0.1s\n\nOK\n", mutate.UNVERIFIED),
            # A failure that prints no failing test's line: an os.Exit in a Go test, as go test
            # reports it, and an unexpected success, as unittest does.
            (0, "exit status 1\nFAIL\tx/a\t1.059s\nok  \tx/b\t0.375s\nFAIL\n",
             mutate.UNVERIFIED),
            (0, "u\nUNEXPECTED SUCCESS: test_x (test_u.T.test_x)\nRan 1 test in 0.000s\n\n"
                "FAILED (unexpected successes=1)\n\nRan 2 tests in 0.1s\n\nOK\n",
             mutate.UNVERIFIED),
            # A passing run whose words hold FAIL fails nothing.
            (0, "=== RUN   TestNoFAIL\n--- PASS: TestNoFAIL (0.00s)\nPASS\nok  \tpkg\t0.1s\n",
             mutate.SURVIVED),
        ]
        for status, output, want in cases:
            with self.subTest(output=output):
                self.assertEqual(mutate.verdict(status, output)[0], want)

    def test_a_kill_lists_every_failing_test_and_flags_a_panic(self):
        output = "panic: boom\n--- FAIL: TestA (0.00s)\n    --- FAIL: TestA/b (0.00s)\n"
        code, text = mutate.verdict(2, output)
        self.assertEqual(code, mutate.KILLED)
        self.assertIn("    --- FAIL: TestA/b (0.00s)", text)
        self.assertIn("note: a panic ended the run", text)


class UnittestTest(unittest.TestCase):
    def test_a_kill_names_its_tests_and_flags_an_error(self):
        output = ("ERROR: test_y (m.T)\nFAIL: test_x (m.T)\n----\nRan 2 tests in 0.1s\n\n"
                  "FAILED (failures=1, errors=1)\n")
        code, text = mutate.verdict(1, output)
        self.assertEqual(code, mutate.KILLED)
        self.assertIn("note: an error, not a failed assertion", text)
        self.assertEqual(mutate.summary(code, text, "m"), "KILLED     m: test_y, test_x")


def keep_outputs_in(test: unittest.TestCase, directory: str) -> None:
    """Make the directories mutate keeps outputs in land under directory for the test's life."""
    previous = tempfile.tempdir
    tempfile.tempdir = directory
    test.addCleanup(setattr, tempfile, "tempdir", previous)


class MutateTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        keep_outputs_in(self, self.dir.name)
        self.file = Path(self.dir.name) / "subject.txt"
        self.file.write_text("keep good keep\n")
        self.command = [sys.executable, "-c", FAKE_TEST, str(self.file)]
        # A run of the script as a process keeps its outputs here too.
        self.env = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1", "TMPDIR": self.dir.name}

    def tearDown(self):
        self.dir.cleanup()

    def run_mutate(self, old, new, command=None):
        return quietly(mutate.main, [str(self.file), old, new, "--", *(command or self.command)])[0]

    def test_a_caught_mutant_is_killed_and_restored(self):
        self.assertEqual(self.run_mutate("good", "bad"), mutate.KILLED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_the_whole_output_is_kept_and_named(self):
        probe = [sys.executable, "-c", "print('probe hit 42'); print('ok  \tfake/pkg\t0.1s')"]
        code, printed = quietly(mutate.main, [str(self.file), "good", "bad", "--", *probe])
        self.assertEqual(code, mutate.SURVIVED, printed)
        kept = [l.split(": ", 1)[1] for l in printed.splitlines() if l.startswith("output: ")]
        self.assertEqual(len(kept), 1, printed)
        self.assertIn("probe hit 42", Path(kept[0]).read_text())
        self.assertTrue(Path(kept[0]).is_relative_to(self.dir.name))
        self.assertEqual([p.name for p in Path(kept[0]).parent.iterdir()], ["output.txt"],
                         "the copy of the original outlived the restore")

    def restoring(self, data: bytes):
        """Pick the write that puts data back into the subject file."""
        target = self.file.resolve()
        return lambda path, written: path.resolve() == target and written == data

    def test_a_restore_that_fails_names_the_copy_of_the_original(self):
        original = self.file.read_bytes()
        for fail, left in ((full, b""), (torn, original[: len(original) // 2])):
            with self.subTest(fail=fail.__name__):
                with failing(self.restoring(original), fail):
                    code, printed = quietly(mutate.main,
                                            [str(self.file), "good", "bad", "--", *self.command])
                self.assertEqual(code, mutate.UNVERIFIED, printed)
                self.assertIn("RESTORE FAILED", printed)
                self.assertEqual(self.file.read_bytes(), left)
                self.assertEqual(copy_named(printed).read_bytes(), original)
                self.file.write_bytes(original)

    def test_nothing_is_planted_when_the_original_cannot_be_copied_aside(self):
        marker = Path(self.dir.name) / "ran"
        command = [sys.executable, "-c", f"open({str(marker)!r}, 'a').write('x')\n" + FAKE_TEST,
                   str(self.file)]
        with failing(lambda path, data: path.suffix == ".original", full):
            code, printed = quietly(mutate.main, [str(self.file), "good", "bad", "--", *command])
        self.assertEqual(code, mutate.UNVERIFIED, printed)
        self.assertIn("so nothing was planted", printed)
        self.assertFalse(marker.exists(), "the command ran")
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_no_signal_that_ends_a_run_can_cut_the_restore_short(self):
        def interrupted(write, path, data):
            # Each signal that ends a run arrives as the restore starts writing.
            for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
                os.kill(os.getpid(), sig)
            return write(path, data)

        def own(signum, frame):
            raise AssertionError("a SIGINT reached the run during its restore")

        self.addCleanup(signal.signal, signal.SIGINT, signal.signal(signal.SIGINT, own))
        with failing(self.restoring(self.file.read_bytes()), interrupted):
            self.assertEqual(self.run_mutate("good", "bad"), mutate.KILLED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")
        self.assertIs(signal.getsignal(signal.SIGINT), own, "the handler was not put back")

    def test_a_kill_needs_the_command_to_pass_on_the_clean_tree(self):
        self.file.write_text("keep good bad\n")
        code, printed = quietly(mutate.main, [str(self.file), "good", "fine", "--", *self.command])
        self.assertEqual(code, mutate.UNVERIFIED, printed)
        self.assertIn("does not pass on the clean tree either", printed)
        clean = printed.split("the clean run is in ", 1)[1].split("\n", 1)[0]
        self.assertIn("--- FAIL: TestThing", Path(clean).read_text(), "the clean run is kept")
        self.assertEqual(self.file.read_text(), "keep good bad\n")

    def test_a_status_that_hides_a_failing_test_gives_no_verdict(self):
        masked = [sys.executable, "-c", MASKED_TEST, str(self.file)]
        self.assertEqual(self.run_mutate("good", "bad", masked), mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_harmless_mutant_survives_and_is_restored(self):
        self.assertEqual(self.run_mutate("keep\n", "keep \n"), mutate.SURVIVED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_an_ambiguous_or_empty_replacement_is_refused(self):
        self.assertEqual(self.run_mutate("keep", "x"), mutate.UNVERIFIED)
        self.assertEqual(self.run_mutate("good", "good"), mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_refusal_says_how_to_fix_old(self):
        keep = Path(self.dir.name) / "output.txt"
        for old, fix in (("keep", "widen it until it is unique"),
                         ("absent", "copy it from the file as written")):
            with self.subTest(old=old):
                code, text = mutate.mutate(self.file, old, "x", ["true"], keep)
                self.assertEqual(code, mutate.UNVERIFIED)
                self.assertTrue(text.endswith(fix), text)

    def test_a_usage_error_changes_nothing(self):
        code, _ = quietly(mutate.main, [str(self.file), "good", "bad", "echo"])
        self.assertEqual(code, mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_missing_file_or_batch_is_a_usage_error(self):
        for argv in (["/no/such/file", "good", "bad", "--", "true"],
                     ["--batch", "/no/such/batch", "--", "true"]):
            with self.subTest(argv=argv):
                self.assertEqual(quietly(mutate.main, argv)[0], mutate.UNVERIFIED)

    def test_a_go_mutant_that_does_not_build_is_no_verdict(self):
        repo = Path(self.dir.name) / "repo"
        target = repo / "pkg" / "x.go"
        target.parent.mkdir(parents=True)
        target.write_text("package pkg\n\nconst X = 1\n")
        subprocess.run(["git", "-C", str(repo), "init", "-q"], check=True)
        # A go whose build of the package fails, and a test command that passes whatever the
        # file holds, as one testing another package does.
        go = Path(self.dir.name) / "bin" / "go"
        go.parent.mkdir()
        go.write_text("#!/bin/sh\necho '# x/pkg'\necho 'pkg/x.go:3:11: undefined: y'\nexit 1\n")
        go.chmod(0o755)
        passing = [sys.executable, "-c", "print('ok  \\tother/pkg\\t0.1s')"]
        with mock.patch.dict(os.environ, {"PATH": f"{go.parent}{os.pathsep}{os.environ['PATH']}"}):
            code, printed = quietly(mutate.main, [str(target), "X = 1", "X = y", "--", *passing])
        self.assertEqual(code, mutate.UNVERIFIED, printed)
        self.assertIn("does not build or vet", printed)
        self.assertEqual(target.read_text(), "package pkg\n\nconst X = 1\n")

    def test_an_interrupted_run_restores_the_file_and_gives_no_verdict(self):
        # The clean tree passes at once, so a batch's baseline is done before the mutant hangs.
        hang = ("import sys, time; t = open(sys.argv[1]).read()\n"
                "time.sleep(30) if 'bad' in t else print('ok  \\tfake/pkg\\t0.1s')")
        batch = Path(self.dir.name) / "one.jsonl"
        batch.write_text(json.dumps({"file": str(self.file), "old": "good", "new": "bad"}))
        for args in ([str(self.file), "good", "bad"], ["--batch", str(batch)]):
            with self.subTest(mode=args[0]):
                # SIGINT at its default, so Python turns it into KeyboardInterrupt.
                proc = subprocess.Popen(
                    [sys.executable, str(HERE / "mutate.py"), *args, "--", sys.executable, "-c",
                     hang, str(self.file)], env=self.env, stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT, text=True,
                    preexec_fn=lambda: signal.signal(signal.SIGINT, signal.SIG_DFL))
                for _ in range(100):
                    if "bad" in self.file.read_text():
                        break
                    time.sleep(0.05)
                self.assertIn("bad", self.file.read_text(), "the mutant was never planted")
                proc.send_signal(signal.SIGINT)
                out = proc.communicate(timeout=10)[0]
                self.assertEqual(proc.returncode, mutate.UNVERIFIED, out)
                self.assertIn("mutate: interrupted;", out)
                self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_command_that_cannot_start_is_no_verdict(self):
        command = [sys.executable, str(HERE / "mutate.py"), str(self.file), "good", "bad", "--",
                   "/no/such/command"]
        run = subprocess.run(command, capture_output=True, text=True,
                             env=self.env)
        self.assertEqual(run.returncode, mutate.UNVERIFIED, run.stderr)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_terminated_run_restores_the_file(self):
        script = HERE / "mutate.py"
        proc = subprocess.Popen([sys.executable, str(script), str(self.file), "good", "bad", "--",
                                 sys.executable, "-c", "import time; time.sleep(30)"],
                                env=self.env)
        for _ in range(100):
            if "bad" in self.file.read_text():
                break
            time.sleep(0.05)
        self.assertIn("bad", self.file.read_text(), "the mutant was never planted")
        proc.send_signal(signal.SIGTERM)
        self.assertEqual(proc.wait(timeout=10), mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_python_mutant_must_parse(self):
        target = Path(self.dir.name) / "planted.py"
        target.write_text("X = 1\n")
        code, out = quietly(mutate.main, [str(target), "X = 1", "X = (", "--", "false"])
        self.assertEqual(code, mutate.UNVERIFIED, out)
        self.assertIn("the mutant is not Python", out)
        self.assertEqual(target.read_text(), "X = 1\n")

    def test_a_python_mutant_leaves_no_bytecode_behind(self):
        target = Path(self.dir.name) / "planted.py"
        target.write_text("X = 1\n")
        load = (f"import sys; sys.path.insert(0, {self.dir.name!r}); import planted; "
                "print('ok  \\tpkg\\t0.1s')")
        with mock.patch.dict(os.environ):
            os.environ.pop("PYTHONDONTWRITEBYTECODE", None)
            quietly(mutate.main, [str(target), "X = 1", "X = 2", "--", sys.executable, "-c", load])
        self.assertFalse(Path(importlib.util.cache_from_source(str(target))).exists(),
                         "bytecode of the mutant would outlive its restore")


    def test_output_that_is_not_utf8_is_judged_not_crashed_on(self):
        # Both the mutant's run and the clean one print a cut rune.
        printing = ("import sys; bad = 'bad' in open(sys.argv[1]).read(); "
                    "sys.stdout.buffer.write(b'cut \\xe2\\x82 rune\\n' + (b'--- FAIL: TestCut "
                    "(0.00s)\\nFAIL\\n' if bad else b'ok  \\tfake/pkg\\t0.1s\\n')); "
                    "sys.exit(1 if bad else 0)")
        code = self.run_mutate("good", "bad", [sys.executable, "-c", printing, str(self.file)])
        self.assertEqual(code, mutate.KILLED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")


class BatchTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        keep_outputs_in(self, self.dir.name)
        self.file = Path(self.dir.name) / "subject.txt"
        self.file.write_text("keep good keep\nmore\n")
        self.marker = Path(self.dir.name) / "ran"
        # The fake test also records that it ran, so a refused batch can be shown to run nothing.
        self.command = [sys.executable, "-c",
                        f"open({str(self.marker)!r}, 'a').write('x')\n" + FAKE_TEST,
                        str(self.file)]

    def tearDown(self):
        self.dir.cleanup()

    def run_batch(self, *lines):
        batch = Path(self.dir.name) / "mutants.jsonl"
        batch.write_text("\n".join(l if isinstance(l, str) else json.dumps(l) for l in lines))
        return quietly(mutate.main, ["--batch", str(batch), "--", *self.command])

    def mutant(self, old, new, name=None):
        m = {"file": str(self.file), "old": old, "new": new}
        return {**m, "name": name} if name else m

    def test_every_mutant_killed_is_a_kill(self):
        code, out = self.run_batch("# comment", "", self.mutant("good", "bad", "plant"),
                                   self.mutant("more", "bad more"))
        self.assertEqual(code, mutate.KILLED, out)
        self.assertIn("KILLED     plant: TestThing", out)
        self.assertIn("(line 4)", out)
        self.assertIn("mutate: 2 killed, 0 survived, 0 unverified", out)
        self.assertEqual(self.file.read_text(), "keep good keep\nmore\n")
        outputs = Path(out.rsplit("each run's output is in ", 1)[1].strip())
        names = sorted(p.name for p in outputs.iterdir())
        self.assertEqual(names, ["1.txt", "2.txt", "baseline.txt"])
        self.assertIn("--- FAIL: TestThing", (outputs / "1.txt").read_text())
        self.assertNotIn("FAIL", (outputs / "baseline.txt").read_text())

    def test_a_restore_that_fails_ends_the_batch(self):
        original, target = self.file.read_bytes(), self.file.resolve()
        with failing(lambda path, data: path.resolve() == target and data == original, full):
            code, out = self.run_batch(self.mutant("good", "bad"), self.mutant("more", "bad more"))
        self.assertEqual(code, mutate.UNVERIFIED, out)
        self.assertEqual(copy_named(out).read_bytes(), original)
        self.assertEqual(self.marker.read_text(), "xx",
                         "a mutant ran on a tree still holding the one before it")

    def test_a_mutant_without_a_verdict_says_why(self):
        target = Path(self.dir.name) / "planted.py"
        target.write_text("X = 1\n")
        unparsed = {"file": str(target), "old": "X = 1", "new": "X = (", "name": "unparsed"}
        code, out = self.run_batch(self.mutant("good", "bad"), unparsed)
        self.assertEqual(code, mutate.UNVERIFIED, out)
        self.assertIn("UNVERIFIED unparsed: the mutant is not Python", out)

    def test_one_survivor_makes_the_batch_survive(self):
        code, out = self.run_batch(self.mutant("good", "bad"), self.mutant("more", "less", "x"))
        self.assertEqual(code, mutate.SURVIVED, out)
        self.assertIn("SURVIVED   x: no test noticed", out)
        self.assertEqual(self.file.read_text(), "keep good keep\nmore\n")

    def test_a_batch_with_any_problem_runs_nothing(self):
        for bad in ("{not json", {"file": str(self.file), "old": "keep"},
                    self.mutant("keep", "x"), self.mutant("good", "good"),
                    {"file": "/no/such/file", "old": "a", "new": "b"}):
            with self.subTest(bad=bad):
                code, out = self.run_batch(self.mutant("good", "bad"), bad)
                self.assertEqual(code, mutate.UNVERIFIED, out)
                self.assertIn("mutants.jsonl:2:", out)
                self.assertFalse(self.marker.exists(), "the command ran for a refused batch")

    def test_an_empty_batch_is_refused(self):
        code, out = self.run_batch("# nothing")
        self.assertEqual(code, mutate.UNVERIFIED)
        self.assertIn("holds no mutant", out)

    def test_a_failing_baseline_judges_no_mutant(self):
        self.file.write_text("keep good keep\nmore\nbad\n")
        code, out = self.run_batch(self.mutant("good", "worse"))
        self.assertEqual(code, mutate.UNVERIFIED, out)
        self.assertIn("must pass on the clean tree", out)
        self.assertEqual(self.marker.read_text(), "x", "only the baseline ran")
        self.assertEqual(self.file.read_text(), "keep good keep\nmore\nbad\n")

    def test_a_baseline_that_runs_no_test_judges_no_mutant(self):
        self.command = [sys.executable, "-c",
                        f"open({str(self.marker)!r}, 'a').write('x')\n"
                        "print('ok  \\tfake/pkg\\t0.1s [no tests to run]')"]
        code, out = self.run_batch(self.mutant("good", "bad"))
        self.assertEqual(code, mutate.UNVERIFIED, out)
        self.assertIn("no test ran", out)
        self.assertEqual(self.marker.read_text(), "x", "only the baseline ran")


if __name__ == "__main__":
    unittest.main()
