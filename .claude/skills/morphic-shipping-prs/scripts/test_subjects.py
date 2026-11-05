"""Tests for subjects.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-shipping-prs/scripts
"""
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import subjects

SCRIPT = Path(__file__).resolve().parent / "subjects.py"
CONVENTIONS = Path(__file__).resolve().parents[4] / "CLAUDE.md"
TITLE = "chore(skills): hold each subject as it lands"
# 66 characters alone and 74 with " (#1311)": over the cap only where it lands.
SIXTY_SIX = "build: read the vet plant's file by its path below the module root"


class CheckTest(unittest.TestCase):
    def test_one_commit_lands_under_its_own_subject(self):
        self.assertEqual(subjects.check(1311, TITLE, [SIXTY_SIX]),
                         [f"commit: 74 characters with '(#1311)', over 72: {SIXTY_SIX}"])

    def test_a_replaced_subject_is_measured_bare(self):
        self.assertEqual(subjects.check(1311, TITLE, [SIXTY_SIX, "test: add a case"]), [])

    def test_a_replaced_subject_still_holds_to_the_cap(self):
        long = "test: " + "x" * 67
        self.assertEqual(subjects.check(1311, TITLE, [long, "test: add a case"]),
                         [f"commit: 73 characters, over 72: {long}"])

    def test_the_title_is_measured_with_the_number(self):
        title = "docs: " + "x" * 64
        self.assertEqual(subjects.check(9, title, ["test: a", "test: b"]),
                         [f"title: 75 characters with '(#9)', over 72: {title}"])
        self.assertEqual(subjects.check(9, title[:-3], ["test: a", "test: b"]), [])

    def test_form_and_period_are_held_on_every_subject(self):
        form = "not type(scope): subject"
        cases = {
            "Fix things": form,
            "wip fix: things": form,
            "fix(): things": form,
            "fix:  things": form,
            "fix: things.": "ends in a period",
            "fix!: things": None,
            "refactor(ir)!: drop a field": None,
        }
        for subject, want in cases.items():
            with self.subTest(subject):
                got = subjects.check(1, TITLE, [subject, "test: add a case"])
                self.assertEqual(got, [f"commit: {want}: {subject}"] if want else [])

    def test_the_types_are_those_the_repository_lists(self):
        listed = re.search(r"Common types: ((?:`[a-z]+`[,.]?\s*)+)", CONVENTIONS.read_text())
        self.assertIsNotNone(listed, "CLAUDE.md no longer lists the commit types")
        types = re.findall(r"`([a-z]+)`", listed.group(1))
        accepted = re.match(r"\(([a-z|]+)\)", subjects.FORM.pattern).group(1).split("|")
        self.assertEqual(sorted(types), sorted(accepted))
        for t in types:
            with self.subTest(t):
                self.assertEqual(subjects.check(1, TITLE, [f"{t}: things", "test: add a case"]), [])


class ScriptTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)
        self.repo = Path(self.dir.name)
        self.git("init", "-q", "-b", "main")
        self.commit("chore: start")
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")
        self.git("switch", "-q", "-c", "topic")

    def git(self, *args):
        env = {**os.environ, "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1",
               "GIT_AUTHOR_NAME": "a", "GIT_AUTHOR_EMAIL": "a@example.com",
               "GIT_COMMITTER_NAME": "a", "GIT_COMMITTER_EMAIL": "a@example.com"}
        subprocess.run(["git", *args], cwd=self.repo, env=env, check=True, capture_output=True)

    def commit(self, subject):
        self.git("commit", "-q", "--allow-empty", "-m", subject)

    def run_script(self, *args):
        return subprocess.run([sys.executable, str(SCRIPT), *args], cwd=self.repo,
                              capture_output=True, text=True)

    def test_a_merge_of_main_is_no_second_commit(self):
        self.commit(SIXTY_SIX)
        self.git("switch", "-q", "main")
        self.commit("chore: move main")
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")
        self.git("switch", "-q", "topic")
        self.git("merge", "-q", "--no-edit", "main")
        r = self.run_script("1311", TITLE)
        self.assertEqual((r.returncode, r.stdout.splitlines()[0]),
                         (1, f"commit: 74 characters with '(#1311)', over 72: {SIXTY_SIX}"))

    def test_a_merged_in_commit_is_not_counted_where_origin_main_lags(self):
        self.commit(SIXTY_SIX)
        self.git("switch", "-q", "main")
        self.commit("chore: move main")
        self.git("switch", "-q", "topic")
        self.git("merge", "-q", "--no-edit", "main")
        r = self.run_script("1311", TITLE)
        self.assertEqual((r.returncode, r.stdout.splitlines()[0]),
                         (1, f"commit: 74 characters with '(#1311)', over 72: {SIXTY_SIX}"))

    def test_a_stacked_base_is_left_out_when_named(self):
        self.git("switch", "-q", "-c", "base", "main")
        self.commit("the base's own subject")
        self.git("switch", "-q", "-c", "stacked")
        self.commit("test: add a case")
        named = self.run_script("--base", "base", "1311", TITLE)
        self.assertEqual((named.returncode, named.stdout),
                         (0, "subjects: clean (the title and 1 commit subject(s))\n"))
        unnamed = self.run_script("1311", TITLE)
        self.assertEqual((unnamed.returncode, unnamed.stdout.splitlines()[0]),
                         (1, "commit: not type(scope): subject: the base's own subject"))

    def test_a_former_base_is_left_out_beside_main(self):
        self.git("switch", "-q", "-c", "former", "main")
        self.commit("the former base's own subject")
        self.git("switch", "-q", "-c", "moved")
        self.commit("test: add a case")
        r = self.run_script("--base", "origin/main", "--base", "former", "1311", TITLE)
        self.assertEqual((r.returncode, r.stdout),
                         (0, "subjects: clean (the title and 1 commit subject(s))\n"))

    def test_an_unknown_or_empty_base_exits_two(self):
        self.commit("test: add a case")
        for base in ("no-such-ref", " "):
            with self.subTest(repr(base)):
                self.assertEqual(self.run_script("--base", base, "1311", TITLE).returncode, 2)

    def test_clean_branch_exits_zero(self):
        self.commit(SIXTY_SIX)
        self.commit("test: add a case")
        r = self.run_script("1311", TITLE)
        self.assertEqual((r.returncode, r.stdout),
                         (0, "subjects: clean (the title and 2 commit subject(s))\n"))

    def test_a_push_behind_it_runs_only_when_clean(self):
        self.commit(SIXTY_SIX)
        gated = '"$0" "$1" 1311 "$2" && echo pushed'
        r = subprocess.run(["sh", "-c", gated, sys.executable, str(SCRIPT), TITLE], cwd=self.repo,
                           capture_output=True, text=True)
        self.assertEqual((r.returncode, r.stdout.splitlines()[-1]),
                         (1, "subjects: 1 finding(s) in the title and 1 commit subject(s)"))

    def test_a_number_below_one_exits_two(self):
        r = self.run_script("0", TITLE)
        self.assertEqual((r.returncode, r.stderr.splitlines()[-1:]),
                         (2, ["subjects.py: error: number must be positive, not 0"]))

    def test_an_empty_title_exits_two(self):
        for title in ("", " "):
            with self.subTest(repr(title)):
                r = self.run_script("1311", title)
                self.assertEqual((r.returncode, r.stdout), (2, ""))
                self.assertIn("title is empty", r.stderr)

    def test_cannot_run_exits_two(self):
        self.assertEqual(self.run_script("x", TITLE).returncode, 2)
        self.git("update-ref", "-d", "refs/remotes/origin/main")
        self.assertEqual(self.run_script("1", TITLE).returncode, 2)


if __name__ == "__main__":
    unittest.main()
