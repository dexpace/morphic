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


class TitleTest(unittest.TestCase):
    def test_the_title_is_measured_as_it_lands(self):
        title = "fix(ir): " + "x" * 55  # 64 characters, 72 with " (#1234)"
        self.assertNotIn("OVER", pf.title_report(title, "1234")[1])
        self.assertIn("OVER", pf.title_report(title + "y", "1234")[1])

    def test_the_conventional_form_is_checked(self):
        cases = [
            ("feat(ir)!: add a thing", True),
            ("chore: tidy", True),
            ("Add a thing", False),
            ("fix: end with a period.", False),
            ("fix(ir):missing space", False),
        ]
        for title, ok in cases:
            with self.subTest(title=title):
                self.assertEqual(pf.title_report(title, "1")[2].endswith(": ok"), ok)


class BodyTest(unittest.TestCase):
    def test_sections_and_closing_keywords(self):
        body = "## Summary\n\nCloses #12 and fixes #13; refers to #14.\n\n## Test plan\n"
        want = ["body       has Summary", "body       has Test plan",
                "closes     Closes #12", "closes     fixes #13"]
        self.assertEqual(pf.body_report(body, "fix: x"), want)

    def test_every_spelling_github_closes_on_is_listed(self):
        body = "CLOSES: #1, Fixed octo-org/octo-repo#2, resolve #3; prefix#4, fixing #5, fix:#6"
        want = ["closes     CLOSES: #1", "closes     Fixed octo-org/octo-repo#2",
                "closes     resolve #3"]
        self.assertEqual(pf.body_report(body, "fix: x")[2:], want)

    def test_a_missing_section_is_named(self):
        got = pf.body_report("## Summary\n", "fix: x")
        self.assertIn("body       MISSING a Test plan section", got)

    def test_a_break_needs_its_section_and_only_a_break_has_one(self):
        both = "## Summary\n## Test plan\n"
        cases = [
            ("feat(ir)!: x", both, "body       MISSING a Breaking section"),
            ("feat!: x", both + "## Breaking\n", "body       has Breaking"),
            ("feat(ir): x", both + "## Breaking\n",
             "body       has a Breaking section, but the title marks no break with !"),
        ]
        for title, body, want in cases:
            with self.subTest(title=title):
                self.assertIn(want, pf.body_report(body, title))
        self.assertEqual(len(pf.body_report(both, "fix: x")), 2, "no break, no Breaking line")


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
            (root / "other" / "README.md").write_text("y\n")
            (root / "other" / "new.txt").write_text("z\n")
            (root / "pkg" / "stray.txt").write_text("s\n")  # outside the directory it runs from
            command = [sys.executable, str(HERE / "preflight.py"), "--base", base,
                       "--body", "body.md"]
            run = subprocess.run(command, cwd=root / "other", capture_output=True, text=True,
                                 env=env)
        outside = "FIX untracked  pkg/stray.txt: commit it, remove it, or ignore it"
        self.assertIn(outside, run.stdout, run.stdout + run.stderr)
        self.assertIn("body       has Summary", run.stdout, "a relative --body is read where given")
        changed = "FIX changed    other/README.md: commit or stash it, since the push carries HEAD"
        self.assertIn(changed, run.stdout, "a check of the working tree speaks for no push")
        untracked = "FIX untracked  other/new.txt: commit it, remove it, or ignore it"
        self.assertIn(untracked, run.stdout)
        self.assertEqual(run.returncode, 1, run.stdout)


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        run = subprocess.run([sys.executable, str(HERE / "preflight.py")], capture_output=True,
                             text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


if __name__ == "__main__":
    unittest.main()
