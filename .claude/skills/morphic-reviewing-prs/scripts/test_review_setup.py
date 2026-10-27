"""Tests for review_setup.

Run: python3 -m unittest discover -s .claude/skills/morphic-reviewing-prs/scripts
"""
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import review_setup as rs


class TitleTest(unittest.TestCase):
    def test_the_title_is_measured_as_it_lands(self):
        title = "fix(ir): " + "x" * 55  # 64 characters, 72 with " (#1234)"
        self.assertNotIn("OVER", rs.title_report(title, "1234")[1])
        self.assertIn("OVER", rs.title_report(title + "y", "1234")[1])

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
                self.assertEqual(rs.title_report(title, "1")[2].endswith(": ok"), ok)


class BodyTest(unittest.TestCase):
    def test_sections_and_closing_keywords(self):
        body = "## Summary\n\nCloses #12 and fixes #13; refers to #14.\n\n## Test plan\n"
        want = ["body       has Summary", "body       has Test plan",
                "closes     Closes #12", "closes     fixes #13"]
        self.assertEqual(rs.body_report(body, "fix: x"), want)

    def test_a_missing_section_is_named(self):
        got = rs.body_report("## Summary\n", "fix: x")
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
                self.assertIn(want, rs.body_report(body, title))
        self.assertEqual(len(rs.body_report(both, "fix: x")), 2, "no break, no Breaking line")


class FilesTest(unittest.TestCase):
    def test_files_group_by_area(self):
        files = ["ir/a.go", "ir/irverify/b.go", "ir/irverify/c.go", "README.md",
                 "compilers/openapi/x/y.go"]
        want = [(1, "(root)"), (1, "compilers/openapi"), (1, "ir"), (2, "ir/irverify")]
        self.assertEqual(rs.areas(files), want)

    def test_the_domain_skill_follows_the_files(self):
        openapi = ["morphic-openapi-compiler"]
        self.assertEqual(rs.domain_hints(["docs/parser-library.md"]), openapi)
        self.assertEqual(rs.domain_hints(["testdata/conformance/openapi/a.yaml"]), openapi)
        ir = ["morphic-changing-the-ir"]
        self.assertEqual(rs.domain_hints(["ir/a.go", "docs/ir-design.md"]), ir)
        self.assertEqual(rs.domain_hints(["pass/validate.go"], "perf(pass)!: x"),
                         ["morphic-changing-the-ir", "morphic-measuring-performance"])
        self.assertEqual(rs.domain_hints(["README.md"], "fix: perf of x"), [])


class LandingTest(unittest.TestCase):
    """landing hands the review the tree as it will land, on a repository built here."""

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name)
        self.git("init", "-q", "-b", "main")
        self.base = self.commit("a.txt", "a\n")

    def tearDown(self):
        self.dir.cleanup()

    def git(self, *args: str) -> str:
        return rs.git(self.root, *args, env=rs.IDENTITY).stdout.strip()

    def commit(self, name: str, text: str) -> str:
        (self.root / name).write_text(text)
        self.git("add", name)
        self.git("commit", "-q", "-m", name)
        return self.git("rev-parse", "HEAD")

    def branch_from(self, start: str, name: str, text: str) -> str:
        self.git("checkout", "-q", "--detach", start)
        return self.commit(name, text)

    def test_a_clean_merge_is_reviewed_as_the_merge(self):
        head = self.branch_from(self.base, "h.txt", "h\n")
        main = self.branch_from(self.base, "m.txt", "m\n")
        lines, target, lands = rs.landing(self.root, "origin/main", main, head)
        self.assertTrue(lands)
        self.assertNotIn(target, (head, main))
        self.assertEqual(self.git("rev-list", "--parents", "-n1", target).split()[1:], [head, main])
        self.assertEqual(self.git("ls-tree", "--name-only", target).split(),
                         ["a.txt", "h.txt", "m.txt"])
        self.assertIn("clean into origin/main", lines[0])

    def test_a_head_holding_main_is_reviewed_as_it_is(self):
        head = self.branch_from(self.base, "h.txt", "h\n")
        lines, target, lands = rs.landing(self.root, "origin/main", self.base, head)
        self.assertEqual((target, lands), (head, True))
        self.assertIn("CI's run on the head ran this tree", lines[0])

    def test_a_conflict_leaves_the_head_alone_and_names_the_file(self):
        head = self.branch_from(self.base, "a.txt", "head\n")
        main = self.branch_from(self.base, "a.txt", "main\n")
        lines, target, lands = rs.landing(self.root, "origin/main", main, head)
        self.assertEqual((target, lands), (head, False))
        self.assertIn("CONFLICTS", lines[0])
        self.assertEqual(lines[1:], [rs.INDENT + "a.txt"])

    def test_a_merge_needs_no_configured_identity(self):
        head = self.branch_from(self.base, "h.txt", "h\n")
        main = self.branch_from(self.base, "m.txt", "m\n")
        self.git("config", "user.useConfigOnly", "true")
        # patch.dict restores the whole environment, the identity variables removed here too.
        bare = {"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}
        with mock.patch.dict(os.environ, bare):
            for k in rs.IDENTITY:
                os.environ.pop(k, None)
            _, target, _ = rs.landing(self.root, "origin/main", main, head)
        self.assertEqual(self.git("log", "-1", "--format=%an", target), "review_setup")


if __name__ == "__main__":
    unittest.main()
