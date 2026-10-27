"""Tests for review_setup. Run: python3 -m unittest discover -s .claude/skills/morphic-reviewing-prs/scripts"""
import unittest

import review_setup as rs


class TitleTest(unittest.TestCase):
    def test_the_title_is_measured_as_it_lands(self):
        title = "fix(ir): " + "x" * 55  # 64 characters, 72 with " (#1234)"
        self.assertNotIn("OVER", rs.title_report(title, "1234")[1])
        self.assertIn("OVER", rs.title_report(title + "y", "1234")[1])

    def test_the_conventional_form_is_checked(self):
        for title, ok in [("feat(ir)!: add a thing", True), ("chore: tidy", True),
                          ("Add a thing", False), ("fix: end with a period.", False), ("fix(ir):missing space", False)]:
            with self.subTest(title=title):
                self.assertEqual(rs.title_report(title, "1")[2].endswith(": ok"), ok)


class BodyTest(unittest.TestCase):
    def test_sections_and_closing_keywords(self):
        body = "## Summary\n\nCloses #12 and fixes #13; refers to #14.\n\n## Test plan\n"
        self.assertEqual(rs.body_report(body), ["body       has Summary", "body       has Test plan",
                                                "closes     Closes #12", "closes     fixes #13"])

    def test_a_missing_section_is_named(self):
        self.assertIn("body       MISSING a Test plan section", rs.body_report("## Summary\n"))


class FilesTest(unittest.TestCase):
    def test_files_group_by_area(self):
        files = ["ir/a.go", "ir/irverify/b.go", "ir/irverify/c.go", "README.md", "compilers/openapi/x/y.go"]
        self.assertEqual(rs.areas(files), [(1, "(root)"), (1, "compilers/openapi"), (1, "ir"), (2, "ir/irverify")])

    def test_the_domain_skill_follows_the_files(self):
        self.assertEqual(rs.domain_hints(["docs/parser-library.md"]), ["morphic-openapi-compiler"])
        self.assertEqual(rs.domain_hints(["testdata/conformance/openapi/a.yaml"]), ["morphic-openapi-compiler"])
        self.assertEqual(rs.domain_hints(["ir/a.go", "docs/ir-design.md"]), [])


if __name__ == "__main__":
    unittest.main()
