"""Tests for artifact_scan.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-reviewing-prs/scripts
"""
import sys
import unittest

import artifact_scan as scan

# Each planted line is split so this file does not match the patterns it tests.
PLANTED = [
    "Co-Authored-By: Cl" "aude <noreply@anthr" "opic.com>",
    "Gene" "rated with a tool",
    "as the bri" "ef says",
    "kept by the coordi" "nator",
    "see /U" "sers/someone/x",
    "an audit sw" "eep found M" "3",
]
CLEAN = [
    "the .claude/skills directory",
    "CLAUDE.md says so",
    "a brief description",
    "an H2O molecule and M10K",
    "a scratch module",
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

    def test_hits_are_labelled(self):
        got = scan.hits("commit", [("abc123", PLANTED[0]), ("def456", "fine")])
        self.assertEqual(got, [f"commit: abc123: {PLANTED[0]}"])


class DecodeTest(unittest.TestCase):
    def test_a_diff_that_is_not_utf8_is_read_not_crashed_on(self):
        cut = "import sys; sys.stdout.buffer.write(b'cut \\xe2\\x82 rune\\n')"
        self.assertEqual(scan.run(sys.executable, "-c", cut), "cut \ufffd rune\n")


if __name__ == "__main__":
    unittest.main()
