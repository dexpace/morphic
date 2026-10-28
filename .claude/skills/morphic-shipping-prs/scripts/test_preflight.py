"""Tests for preflight.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-shipping-prs/scripts
"""
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import preflight as pf

HERE = Path(__file__).resolve().parent


class SubjectTest(unittest.TestCase):
    def test_each_subject_is_held_to_the_form(self):
        cases = [
            ("fix(ir): keep it", False),
            ("fix(ir): " + "x" * 64, True),  # 73 characters
            ("Fix the thing", True),
            ("docs: end with a period.", True),
        ]
        for subject, fix in cases:
            with self.subTest(subject=subject):
                self.assertEqual(pf.subject_lines([subject])[0][1], fix)


class ExportedTest(unittest.TestCase):
    def test_a_change_needs_a_bang_only_when_the_title_lacks_one(self):
        changes = ["ir: type Gone struct"]
        self.assertTrue(pf.exported_lines(changes, "fix(ir): drop Gone")[0][1])
        self.assertFalse(pf.exported_lines(changes, "fix(ir)!: drop Gone")[0][1])
        self.assertFalse(pf.exported_lines(changes, None)[0][1], "no title, nothing to judge")
        self.assertEqual(pf.exported_lines([], "fix: x"), [])


class TitleBodyTest(unittest.TestCase):
    def test_the_review_marks_become_fixes(self):
        body = "## Summary\n\n## Test plan\n"
        got = dict(pf.title_body_lines("feat(ir)!: x", 1143, body))
        self.assertTrue(got["body       MISSING a Breaking section"])
        self.assertFalse(got["body       has Summary"])
        over = pf.title_body_lines("fix(ir): " + "x" * 60, 1143, None)
        self.assertTrue(any(fix for line, fix in over if "OVER" in line), over)

    def test_no_title_is_a_note_and_the_body_is_still_judged(self):
        got = pf.title_body_lines(None, 0, "## Summary\n## Breaking\n")
        note = "title      none given: pass --pr or --title to measure it"
        self.assertEqual(got[0], (note, False))
        self.assertIn(("body       MISSING a Test plan section", True), got)
        self.assertFalse(any("Breaking" in line for line, _ in got), "the title decides Breaking")


class SkillsTest(unittest.TestCase):
    def test_a_name_a_skill_uses_that_the_branch_removed_is_a_fix(self):
        finding = ".claude/skills/s/SKILL.md:3: no test named TestGone"
        with mock.patch.object(pf.skills_lint, "findings", return_value=(1, [finding])):
            self.assertEqual(pf.skills_lines(Path(".")), [("skills     " + finding, True)])
        with mock.patch.object(pf.skills_lint, "findings", return_value=(1, [])):
            self.assertEqual(pf.skills_lines(Path(".")), [])


class RootTest(unittest.TestCase):
    def test_a_run_from_a_subdirectory_checks_the_whole_tree(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            env = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1",
                   "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
                   "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
            git = ["git", "-C", d]
            subprocess.run(git + ["init", "-q"], check=True, env=env)
            (root / "pkg").mkdir()
            (root / "go.mod").write_text("module example.com/m\n\ngo 1.25\n")
            (root / "other").mkdir()
            (root / "other" / "README.md").write_text("x\n")
            (root / ".gitignore").write_text("body.md\n")
            (root / "other" / "body.md").write_text("## Summary\n\n## Test plan\n")
            (root / "pkg" / "a.go").write_text("package pkg\n\nfunc Exported() {}\n")
            subprocess.run(git + ["add", "-A"], check=True, env=env)
            subprocess.run(git + ["commit", "-qm", "base"], check=True, env=env)
            base = subprocess.run(git + ["rev-parse", "HEAD"], check=True, env=env,
                                  capture_output=True, text=True).stdout.strip()
            (root / "pkg" / "a.go").write_text("package pkg\n\nfunc Exported(x int) {}\n")
            subprocess.run(git + ["commit", "-qam", "fix: change it"], check=True, env=env)
            command = [sys.executable, str(HERE / "preflight.py"), "--base", base,
                       "--body", "body.md"]
            run = subprocess.run(command, cwd=root / "other", capture_output=True, text=True,
                                 env=env)
        self.assertIn("pkg: func Exported()", run.stdout, run.stdout + run.stderr)
        self.assertIn("body       has Summary", run.stdout, "a relative --body is read where given")


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        run = subprocess.run([sys.executable, str(HERE / "preflight.py")], capture_output=True,
                             text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


if __name__ == "__main__":
    unittest.main()
