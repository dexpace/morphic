"""Tests for mutate. Run: python3 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts"""
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
        code, text = mutate.verdict(2, "panic: boom\n--- FAIL: TestA (0.00s)\n    --- FAIL: TestA/b (0.00s)\n")
        self.assertEqual(code, mutate.KILLED)
        self.assertIn("    --- FAIL: TestA/b (0.00s)", text)
        self.assertIn("note: a panic ended the run", text)


class MutateTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.file = Path(self.dir.name) / "subject.txt"
        self.file.write_text("keep good keep\n")
        self.command = [sys.executable, "-c", FAKE_TEST, str(self.file)]

    def tearDown(self):
        self.dir.cleanup()

    def run_mutate(self, old, new, command=None):
        return mutate.main([str(self.file), old, new, "--", *(command or self.command)])

    def test_a_caught_mutant_is_killed_and_restored(self):
        self.assertEqual(self.run_mutate("good", "bad"), mutate.KILLED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_harmless_mutant_survives_and_is_restored(self):
        self.assertEqual(self.run_mutate("keep\n", "keep \n"), mutate.SURVIVED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_an_ambiguous_or_empty_replacement_is_refused(self):
        self.assertEqual(self.run_mutate("keep", "x"), mutate.UNVERIFIED)
        self.assertEqual(self.run_mutate("good", "good"), mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_usage_error_changes_nothing(self):
        self.assertEqual(mutate.main([str(self.file), "good", "bad", "echo"]), mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")

    def test_a_terminated_run_restores_the_file(self):
        script = HERE / "mutate.py"
        proc = subprocess.Popen([sys.executable, str(script), str(self.file), "good", "bad", "--",
                                 sys.executable, "-c", "import time; time.sleep(30)"],
                                env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
        for _ in range(100):
            if "bad" in self.file.read_text():
                break
            time.sleep(0.05)
        self.assertIn("bad", self.file.read_text(), "the mutant was never planted")
        proc.send_signal(signal.SIGTERM)
        self.assertEqual(proc.wait(timeout=10), mutate.UNVERIFIED)
        self.assertEqual(self.file.read_text(), "keep good keep\n")


if __name__ == "__main__":
    unittest.main()
