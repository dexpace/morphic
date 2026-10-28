"""Tests for preflight.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-shipping-prs/scripts
"""
import argparse
import contextlib
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import preflight as pf

HERE = Path(__file__).resolve().parent
ENV = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
       "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}


def commit_file(d: str, name: str, text: str, message: str) -> str:
    """Write name in the repository at d, commit it with message, and return the commit."""
    (Path(d) / name).write_text(text)
    subprocess.run(["git", "-C", d, "add", name], check=True, env=ENV)
    subprocess.run(["git", "-C", d, "commit", "-q", "-m", message], check=True, env=ENV)
    return subprocess.run(["git", "-C", d, "rev-parse", "HEAD"], check=True, env=ENV,
                          capture_output=True, text=True).stdout.strip()


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
        self.assertEqual(pf.title_report(title, "1234")[1][1], False)
        line, fix = pf.title_report(title + "y", "1234")[1]
        self.assertTrue(fix)
        self.assertIn("OVER", line)

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
                line, fix = pf.title_report(title, "1")[2]
                self.assertEqual((line.endswith(": ok"), fix), (ok, not ok))

    def test_a_valid_title_holding_a_marker_word_needs_no_fix(self):
        for title in ("fix(ir): read OVERRIDE keys", "docs: say what is NOT kept",
                      "feat: report MISSING fields"):
            with self.subTest(title=title):
                self.assertFalse(any(fix for _, fix in pf.title_body_lines(title, 1143, None)))


class BodyTest(unittest.TestCase):
    def test_sections_and_closing_keywords(self):
        body = "## Summary\n\nCloses #12 and fixes #13; refers to #14.\n\n## Test plan\n"
        want = ["body       has Summary", "body       has Test plan",
                "closes     Closes #12", "closes     fixes #13"]
        self.assertEqual([line for line, _ in pf.body_report(body, False)], want)

    def test_every_spelling_github_closes_on_is_listed(self):
        body = "CLOSES: #1, Fixed octo-org/octo-repo#2, resolve #3; prefix#4, fixing #5, fix:#6"
        want = ["closes     CLOSES: #1", "closes     Fixed octo-org/octo-repo#2",
                "closes     resolve #3"]
        self.assertEqual([line for line, _ in pf.body_report(body, False)[2:]], want)

    def test_a_missing_section_is_named(self):
        got = pf.body_report("## Summary\n", False)
        self.assertIn(("body       MISSING a Test plan section", True), got)

    def test_a_break_needs_its_section_and_only_a_break_has_one(self):
        both = "## Summary\n## Test plan\n"
        cases = [
            (True, both, ("body       MISSING a Breaking section", True)),
            (True, both + "## Breaking\n", ("body       has Breaking", False)),
            (False, both + "## Breaking\n",
             ("body       has a Breaking section, but the title marks no break with !", True)),
        ]
        for breaking, body, want in cases:
            with self.subTest(breaking=breaking, body=body):
                self.assertIn(want, pf.body_report(body, breaking))
        self.assertEqual(len(pf.body_report(both, False)), 2, "no break, no Breaking line")
        self.assertEqual(len(pf.body_report(both + "## Breaking\n", None)), 2,
                         "with no title, Breaking is not judged")


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


class PrHeadTest(unittest.TestCase):
    def test_a_tree_without_the_prs_head_is_a_fix(self):
        env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
               "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
        with tempfile.TemporaryDirectory() as d:
            git = ["git", "-C", d]

            def commit(message: str) -> str:
                subprocess.run(git + ["commit", "-q", "--allow-empty", "-m", message], check=True,
                               env=env)
                return subprocess.run(git + ["rev-parse", "HEAD"], check=True, env=env,
                                      capture_output=True, text=True).stdout.strip()

            subprocess.run(git + ["init", "-q"], check=True, env=env)
            base, head = commit("base"), commit("head")
            subprocess.run(git + ["checkout", "-q", "-b", "other", base], check=True, env=env)
            sibling = commit("sibling")
            subprocess.run(git + ["checkout", "-q", "--detach", head], check=True, env=env)
            cases = [(head, False), (base, False), (sibling, True), ("0" * 40, True)]
            with contextlib.chdir(d):
                for oid, fix in cases:
                    with self.subTest(oid=oid):
                        line, got = pf.pr_head_line(7, oid)
                        self.assertEqual(got, fix, line)
                        self.assertIn(f"#7's head {oid[:12]}", line)
                # A run for a PR reads the head from the PR and reports it with the rest.
                pr = {"title": "fix: x", "body": "## Summary\n## Test plan\n",
                      "headRefOid": sibling}
                real = pf.run

                def answer(*command: str) -> str:
                    return json.dumps(pr) if command[0] == "gh" else real(*command)

                args = argparse.Namespace(base=base, pr=7, title=None, body=None)
                with mock.patch.object(pf, "run", side_effect=answer):
                    lines = pf.report(args)
        want = (f"pr         does not hold #7's head {sibling[:12]}: check out the PR's tree, "
                "fetching it first", True)
        self.assertIn(want, lines)


class MergeTest(unittest.TestCase):
    def test_only_a_base_that_conflicts_is_a_fix(self):
        with tempfile.TemporaryDirectory() as d:
            git = ["git", "-C", d]
            subprocess.run(git + ["init", "-q"], check=True, env=ENV)
            root = commit_file(d, "a.txt", "a\n", "root")
            subprocess.run(git + ["checkout", "-q", "-b", "clean"], check=True, env=ENV)
            clean = commit_file(d, "b.txt", "b\n", "beside")
            subprocess.run(git + ["checkout", "-q", "-b", "clash", root], check=True, env=ENV)
            clash = commit_file(d, "a.txt", "other\n", "against")
            subprocess.run(git + ["checkout", "-q", "-b", "head", root], check=True, env=ENV)
            commit_file(d, "a.txt", "head\n", "head")
            cases = [(root, "holds", False), (clean, "merges", False), (clash, "CONFLICTS", True)]
            with contextlib.chdir(d):
                for base, word, fix in cases:
                    with self.subTest(word=word):
                        line, got = pf.merge_line(base)
                        self.assertEqual(line.split()[1], word, line)
                        self.assertEqual(got, fix, line)


class ArtifactTest(unittest.TestCase):
    def test_an_artifact_in_an_added_line_or_a_commit_message_is_a_fix(self):
        # Split so this file does not match the patterns preflight scans for.
        path, attribution = "see /U" "sers/someone", "Gene" "rated with a tool"
        with tempfile.TemporaryDirectory() as d:
            subprocess.run(["git", "-C", d, "init", "-q"], check=True, env=ENV)
            base = commit_file(d, "a.txt", "a\n", "docs: start")
            commit_file(d, "x.md", path + "\n", "docs: add x\n\n" + attribution)
            args = argparse.Namespace(base=base, pr=None, title=None, body=None)
            with contextlib.chdir(d):
                lines = pf.report(args)
        found = [(line.split(": ", 2), fix) for line, fix in lines if line.startswith("artifact")]
        self.assertEqual([(where[0], where[-1], fix) for where, fix in found],
                         [("artifact   added line", path, True),
                          ("artifact   commit", attribution, True)], lines)


class MainTest(unittest.TestCase):
    def test_a_clean_branch_exits_0_and_a_failed_git_command_2(self):
        with tempfile.TemporaryDirectory() as d:
            subprocess.run(["git", "-C", d, "init", "-q"], check=True, env=ENV)
            base = commit_file(d, "a.txt", "a\n", "docs: start")
            commit_file(d, "b.txt", "b\n", "docs: add b")
            for argv, want in ((["--base", base], 0), (["--base", "no-such-ref"], 2)):
                with self.subTest(argv=argv):
                    out = io.StringIO()
                    with contextlib.chdir(d), contextlib.redirect_stdout(out), \
                            contextlib.redirect_stderr(out):
                        self.assertEqual(pf.main(argv), want, out.getvalue())
                    self.assertEqual("preflight: clean" in out.getvalue(), want == 0)

    def test_a_draft_title_is_measured_with_the_next_number(self):
        with tempfile.TemporaryDirectory() as d:
            subprocess.run(["git", "-C", d, "init", "-q"], check=True, env=ENV)
            base = commit_file(d, "a.txt", "a\n", "docs: start")
            real = pf.run

            def answer(*command: str) -> str:
                return "1200\n" if command[0] == "gh" else real(*command)

            args = argparse.Namespace(base=base, pr=None, title="fix: x", body=None)
            with contextlib.chdir(d), mock.patch.object(pf, "run", side_effect=answer):
                lines = pf.report(args)
        self.assertIn((f'{pf.INDENT}14 characters with " (#1201)", cap 72', False), lines)


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


class UnexpectedErrorTest(unittest.TestCase):
    def test_an_unexpected_error_exits_2_never_a_verdict(self):
        # Every command the script runs fails in a way no handler of its own expects.
        crash = ("import runpy, subprocess, sys\n"
                 "def broken(*args, **kwargs): raise ZeroDivisionError('planted')\n"
                 "subprocess.run = broken\n"
                 f"sys.argv = [{str(HERE / 'preflight.py')!r}]\n"
                 "runpy.run_path(sys.argv[0], run_name='__main__')\n")
        run = subprocess.run([sys.executable, "-c", crash], capture_output=True, text=True,
                             env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertIn("ZeroDivisionError: planted", run.stderr)


if __name__ == "__main__":
    unittest.main()
