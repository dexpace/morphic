"""Tests for affected_packages.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts
"""
import io
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

import affected_packages as ap

HERE = Path(__file__).resolve().parent

M = "example.com/m"


class HitPackagesTest(unittest.TestCase):
    def test_a_file_maps_to_its_nearest_package(self):
        dirs = {"", "ir", "ir/irverify", "compilers/openapi"}
        changed = ["ir/irverify/testdata/x.json", "compilers/openapi/internal/doc.txt", "x.txt"]
        got = ap.hit_packages(changed, dirs, M)
        self.assertEqual(got, ({f"{M}/ir/irverify", f"{M}/compilers/openapi", M}, []))

    def test_go_and_markdown_changes_add_archtest(self):
        got = ap.hit_packages(["ir/a.go", "README.md"], {"ir"}, M)
        self.assertEqual(got, ({f"{M}/ir", f"{M}/internal/archtest"}, []))

    def test_a_go_file_no_package_holds_is_returned(self):
        got = ap.hit_packages(["stray/a.go", "notes/b.txt"], {"ir"}, M)
        self.assertEqual(got, ({f"{M}/internal/archtest"}, ["stray/a.go"]))

    def test_skill_files_affect_no_package(self):
        self.assertEqual(ap.hit_packages([".claude/skills/s/SKILL.md"], {""}, M), (set(), []))

    def test_shared_inputs_mean_everything(self):
        shared = ["go.mod", "testdata/golden/a.json", "docs/ir-design.md", "Makefile",
                  ".github/workflows/gate.yml"]
        for f in shared:
            with self.subTest(f=f):
                self.assertTrue(ap.EVERYTHING.match(f))
        self.assertFalse(ap.EVERYTHING.match("ir/testdata/a.json"))


class CitationTest(unittest.TestCase):
    def test_only_an_added_citation_counts(self):
        cases = [
            (["// See docs/compilers/openapi.md#references."], True),
            (["See [it](docs/parser-library.md#merge-keys)."], True),
            (["plain text", "a link to README.md, with no key"], False),
            ([], False),
        ]
        for lines, want in cases:
            with self.subTest(lines=lines):
                self.assertEqual(ap.adds_a_citation(lines), want)

    def test_added_lines_come_from_the_diff_and_whole_untracked_files(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
                   "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
            git = ["git", "-C", d]
            subprocess.run(git + ["init", "-q"], check=True, env=env)
            (root / "a.go").write_text("// gone\n// kept\n")
            subprocess.run(git + ["add", "-A"], check=True, env=env)
            subprocess.run(git + ["commit", "-qm", "base"], check=True, env=env)
            mb = subprocess.run(git + ["rev-parse", "HEAD"], check=True, env=env,
                                capture_output=True, text=True).stdout.strip()
            (root / "a.go").write_text("// kept\n++ see docs/x.md#y\n")
            (root / "b.md").write_text("one\ntwo\n")
            got = ap.added(root, mb, ["a.go", "b.md"], {"b.md"})
        self.assertEqual(got, ["++ see docs/x.md#y", "one", "two"])

    def test_each_reading_test_file_names_its_package(self):
        grep = ("compilers/openapi/e2e/a_test.go\ncompilers/openapi/e2e/b_test.go\n"
                "internal/archtest/c_test.go\n")
        self.assertEqual(ap.citation_readers(grep, M),
                         {f"{M}/compilers/openapi/e2e", f"{M}/internal/archtest"})


class DependentsTest(unittest.TestCase):
    def test_code_and_test_variants_that_import_a_hit_are_listed(self):
        listing = "\n".join([
            f"{M}/a {M}/hit fmt",
            f"{M}/b fmt",
            f"{M}/b [{M}/b.test] {M}/hit fmt",
            f"{M}/c_test [{M}/c.test] {M}/hit",
            f"{M}/c.test {M}/c_test",
            f"{M}/hit fmt",
            f"{M}/d fmt",
        ])
        want = [f"{M}/a", f"{M}/b", f"{M}/c", f"{M}/hit"]
        self.assertEqual(ap.dependents(listing, {f"{M}/hit"}), want)


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        script = str(HERE / "affected_packages.py")
        run = subprocess.run([sys.executable, script], capture_output=True, text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


class DecodeTest(unittest.TestCase):
    def test_a_diff_that_is_not_utf8_is_read_not_crashed_on(self):
        cut = "import sys; sys.stdout.buffer.write(b'cut \\xe2\\x82 rune\\n')"
        self.assertEqual(ap.run(HERE, sys.executable, "-c", cut), "cut \ufffd rune\n")


class UsageTest(unittest.TestCase):
    def test_help_prints_the_usage_and_any_other_option_is_refused(self):
        cases = [(["--help"], 0), (["-h"], 0), (["--octopus"], 2), (["origin/main", "extra"], 2)]
        for args, want in cases:
            with self.subTest(args=args):
                out, err = io.StringIO(), io.StringIO()
                with redirect_stdout(out), redirect_stderr(err):
                    self.assertEqual(ap.main(args), want)
                self.assertIn("Usage:", (out if want == 0 else err).getvalue())


if __name__ == "__main__":
    unittest.main()
