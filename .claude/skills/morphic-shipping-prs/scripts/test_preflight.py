"""Tests for preflight.

Run: python3 -m unittest discover -s .claude/skills/morphic-shipping-prs/scripts
"""
import subprocess
import sys
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
    DIFF = "\n".join([
        "--- a/ir/edit.go",
        "+++ b/ir/edit.go",
        "-func NewTypes(src int) *Types {",
        "+func NewTypes() *Types {",
        "-func (e *Editor) Remove(id TypeID) bool {",
        "-func (w *walker) Visit(v Value) bool {",
        "-func helper() {}",
        "-type Gone struct {",
        "-const Version = \"0.1.0\"",
        "-var Default = New()",
        "-const internalOnly = 1",
        "-\tGrouped Kind = \"g\"",
        "--- a/ir/moved.go",
        "+++ /dev/null",
        "-func Moved() int {",
        "--- /dev/null",
        "+++ b/ir/new.go",
        "+func Moved() int {",
    ])

    def test_removed_and_rewritten_exports_are_listed_and_moves_are_not(self):
        want = ["ir/edit.go: func NewTypes(src int) *Types {",
                "ir/edit.go: func (e *Editor) Remove(id TypeID) bool {",
                "ir/edit.go: type Gone struct {",
                "ir/edit.go: const Version = \"0.1.0\"",
                "ir/edit.go: var Default = New()"]
        self.assertEqual(pf.exported_changes(self.DIFF), want)

    def test_a_change_needs_a_bang_only_when_the_title_lacks_one(self):
        changes = ["ir/edit.go: type Gone struct {"]
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


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        run = subprocess.run([sys.executable, str(HERE / "preflight.py")], capture_output=True,
                             text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


if __name__ == "__main__":
    unittest.main()
