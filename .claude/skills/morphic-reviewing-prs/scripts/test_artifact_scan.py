"""Tests for artifact_scan.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-reviewing-prs/scripts
"""
import contextlib
import io
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import artifact_scan as scan

HERE = Path(__file__).resolve().parent

# Each planted line is split so this file does not match the patterns it tests.
PLANTED = [
    "Co-Authored-By: Cl" "aude <noreply@anthr" "opic.com>",
    "Gene" "rated with a tool",
    "as the bri" "ef says",
    "kept by the coordi" "nator",
    "see /U" "sers/someone/x",
    "an audit sw" "eep found M" "3",
    "Co-authored-by: cl" "aude <cl" "aude@users.noreply.github.com>",
    "co-authored-by: cl" "aude-code <bot@example.com>",
    "gene" "rated with cl" "aude code",
    "https://cl" "aude.ai/code/session_0123",
    "Co-authored-by: Cop" "ilot <175728472+Cop" "ilot@users.noreply.github.com>",
    "Co-authored-by: Cur" "sor Agent <cur" "soragent@cur" "sor.com>",
    "Co-Authored-By: gem" "ini-code-assist[bot] <bot@example.com>",
    "Gene" "rated by Open" "AI Cod" "ex",
]
CLEAN = [
    "the .claude/skills directory",
    "CLAUDE.md says so",
    "a brief description",
    "an H2O molecule and M10K",
    "a scratch module",
    "a golden generated with -update",
    "the co-authored-by trailer, which the commit rules forbid",
    "the cursor stops at the generated code",
]


class ScanTest(unittest.TestCase):
    def test_each_planted_artifact_is_found(self):
        for text in PLANTED:
            with self.subTest(text=text):
                self.assertTrue(scan.ARTIFACT.search(text))

    def test_ordinary_words_pass(self):
        for text in CLEAN:
            with self.subTest(text=text):
                self.assertIsNone(scan.ARTIFACT.search(text))

    def test_added_lines_carry_their_file_and_line(self):
        diff = "\n".join([
            "diff --git a/x.go b/x.go", "--- a/x.go", "+++ b/x.go",
            "@@ -1,0 +4,2 @@", "+first", "+second",
            "@@ -9 +20 @@", "-gone", "+third",
            "--- /dev/null", "+++ b/new.md", "@@ -0,0 +1 @@", "+fourth",
        ])
        want = [("x.go:4", "first"), ("x.go:5", "second"), ("x.go:20", "third"),
                ("new.md:1", "fourth")]
        self.assertEqual(list(scan.added_lines(diff)), want)

    def test_a_line_reading_like_a_header_inside_a_hunk_is_content(self):
        diff = "\n".join([
            "--- a/x.md", "+++ b/x.md", "@@ -3,2 +3,2 @@", "--- removed", " kept",
            "+++ " + PLANTED[1], "\\ No newline at end of file",
            "--- a/y.md", "+++ b/y.md", "@@ -1 +1 @@", "-old", "+new",
        ])
        want = [("x.md:4", "++ " + PLANTED[1]), ("y.md:1", "new")]
        self.assertEqual(list(scan.added_lines(diff)), want)

    def test_hits_are_labelled(self):
        got = scan.hits("commit", [("abc123", PLANTED[0]), ("def456", "fine")])
        self.assertEqual(got, [f"commit: abc123: {PLANTED[0]}"])


class MainTest(unittest.TestCase):
    def test_each_outcome_has_its_exit_status(self):
        env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
               "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
        with tempfile.TemporaryDirectory() as d:
            def commit(message: str) -> str:
                subprocess.run(["git", "-C", d, "commit", "-q", "--allow-empty", "-m", message],
                               check=True, env=env)
                return subprocess.run(["git", "-C", d, "rev-parse", "HEAD"], check=True,
                                      capture_output=True, text=True).stdout.strip()

            subprocess.run(["git", "-C", d, "init", "-q"], check=True, env=env)
            base = commit("docs: start")
            clean = commit("docs: go on")
            hit = commit("docs: end\n\n" + PLANTED[1])
            cases = [([base, clean], 0), ([base, hit], 1), ([base, "no-such-ref"], 2)]
            for argv, want in cases:
                with self.subTest(argv=argv):
                    out = io.StringIO()
                    with contextlib.chdir(d), contextlib.redirect_stdout(out), \
                            contextlib.redirect_stderr(out):
                        self.assertEqual(scan.main(argv), want, out.getvalue())


class DecodeTest(unittest.TestCase):
    def test_a_diff_that_is_not_utf8_is_read_not_crashed_on(self):
        cut = "import sys; sys.stdout.buffer.write(b'cut \\xe2\\x82 rune\\n')"
        self.assertEqual(scan.run(sys.executable, "-c", cut), "cut \ufffd rune\n")


class UnexpectedErrorTest(unittest.TestCase):
    def test_an_unexpected_error_exits_2_never_a_verdict(self):
        # Every command the script runs fails in a way no handler of its own expects.
        crash = ("import runpy, subprocess, sys\n"
                 "def broken(*args, **kwargs): raise ZeroDivisionError('planted')\n"
                 "subprocess.run = broken\n"
                 f"sys.argv = [{str(HERE / 'artifact_scan.py')!r}]\n"
                 "runpy.run_path(sys.argv[0], run_name='__main__')\n")
        run = subprocess.run([sys.executable, "-c", crash], capture_output=True, text=True,
                             env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertIn("ZeroDivisionError: planted", run.stderr)


if __name__ == "__main__":
    unittest.main()
