"""Tests for mutate.

Run: python3 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts
"""
import contextlib
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

import mutate

HERE = Path(__file__).resolve().parent
# A stand-in test command: it fails like `go test` when the file holds "bad", passes otherwise.
FAKE_TEST = (
    "import sys; text = open(sys.argv[1]).read()\n"
    "if 'bad' in text: print('--- FAIL: TestThing (0.00s)'); print('FAIL'); sys.exit(1)\n"
    "print('ok  \tfake/pkg\t0.1s')"
)


def quietly(fn, *args):
    """Call fn, returning its result and everything it printed, so no run reaches the console."""
    out = io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(out):
        return fn(*args), out.getvalue()


class VerdictTest(unittest.TestCase):
    def test_each_outcome(self):
        cases = [
            (1, "--- FAIL: TestA (0.00s)\n    --- FAIL: TestA/sub (0.00s)\nFAIL\n", mutate.KILLED),
            (0, "ok  \tpkg\t0.1s\n", mutate.SURVIVED),
            (0, "?   \tpkg\t[no test files]\n", mutate.UNVERIFIED),
            (1, "# pkg\nx.go:1: undefined: y\nFAIL\tpkg [build failed]\n", mutate.UNVERIFIED),
            (1, "FAIL\t./nope [setup failed]\n", mutate.UNVERIFIED),
            (1, "exit status 3\n", mutate.UNVERIFIED),
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

    def test_a_harmless_mutant_survives_and_is_restored(self):
        self.assertEqual(self.run_mutate("keep\n", "keep \n"), mutate.SURVIVED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_an_ambiguous_or_empty_replacement_is_refused(self):
        self.assertEqual(self.run_mutate("keep", "x"), mutate.UNVERIFIED)
        self.assertEqual(self.run_mutate("good", "good"), mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_usage_error_changes_nothing(self):
        code, _ = quietly(mutate.main, [str(self.file), "good", "bad", "echo"])
        self.assertEqual(code, mutate.UNVERIFIED)
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
        self.assertIn("does not pass on the clean tree", out)
        self.assertEqual(self.marker.read_text(), "x", "only the baseline ran")
        self.assertEqual(self.file.read_text(), "keep good keep\nmore\nbad\n")


if __name__ == "__main__":
    unittest.main()
