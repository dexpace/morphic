"""Tests for citations.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-curating-skills/scripts
"""
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import citations as ct

HERE = Path(__file__).resolve().parent

# The boundary rule's Why as it stood with a citation its item does not bear out (#828).
BOUNDARY = ("Off-by-one mutants passed tests placed a level past the bound; ten bound sites "
            "survived on `main` because their tests used budgets far from the edge; `AtMost(-1)` "
            "clamps to zero, so planting through it changed nothing (#340, #828, #1140).")
# Each item's title and sentences of its own text.
ITEMS = {
    340: ("perf(compilers/openapi): index the source tree once per compile\n"
          "TestBuild_TracksAncestorsOnlyToItsDepthBound places its alias one level past. That "
          "off-by-one is a real regression against the recursive descent this walk replaced. An "
          "alias past the bound is out of the walk. Each new test was checked to bite by "
          "planting the defect it names: dropping the node bound reddens it."),
    828: ("build: judge the coverage profile with golang.org/x/tools/cover\n"
          "Mutations of `main.go`, each built before running, with a no-op control that "
          "survived. Each of the following reddens a named test: threshold and the cap itself "
          "unobserved at the edge."),
    1140: ("refactor: count every budget through one type, drop -1 sentinels\n"
           "Each bound lowered by one fails a test. I planted the change at each site "
           "separately: one plant per site, 30 sites. Ten sites had no test at their edge on "
           "main, confirmed by planting the same lowering there: the overlay's byte budget, an "
           "external document's node budget. `AtMost(n)` takes a negative `n` as zero (admits "
           "nothing, the safe direction)."),
}


def rule(why, key="r"):
    return ct.Rule("x/references/r.md", key, why)


class ClausesTest(unittest.TestCase):
    def test_each_citation_takes_the_clause_before_its_group(self):
        got = ct.clauses("A broke (#1), and B broke too (#2, #3).")
        self.assertEqual(got, {1: "A broke ", 2: ", and B broke too ", 3: ", and B broke too "})

    def test_a_bare_citation_takes_its_sentence(self):
        got = ct.clauses("Setup ran. #7 and #8 both set the constant; later text.")
        self.assertIn("#7 and #8 both set the constant", "#7" + got[7])
        self.assertEqual(set(got), {7, 8})

    def test_a_group_with_no_text_before_it_takes_the_whole_why(self):
        why = "(#4) opened it."
        self.assertEqual(ct.clauses(why), {4: why})


class ScoreTest(unittest.TestCase):
    def test_a_quoted_identifier_must_appear_as_written_and_a_word_by_its_stem(self):
        s, absent = ct.score("`FieldByName` allocated memory", "fieldbyname allocation memory")
        self.assertEqual(absent, ["FieldByName"])
        self.assertAlmostEqual(s, 2 / 3)
        self.assertEqual(ct.score("`FieldByName` allocated", "FieldByName allocations")[0], 1.0)

    def test_the_misattributed_citation_scores_lowest_and_is_listed(self):
        listed = ct.audit([rule(BOUNDARY)], ITEMS)
        self.assertEqual(listed[0].number, 828, listed)
        self.assertLess(listed[0].score, ct.LOW)
        self.assertNotIn(1140, [x.number for x in listed], "a citation its item bears out")

    def test_a_citation_well_under_its_clause_is_listed_above_the_floor(self):
        why = "The walker allocated memory per visited entry (#1, #2)."
        found = {1: "the walker allocated memory per visited entry",
                 2: "the walker allocated, and something else entirely"}
        scores = {n: ct.score(ct.clauses(why)[n], t)[0] for n, t in found.items()}
        self.assertGreaterEqual(scores[2], ct.LOW, "the plant must sit above the floor")
        self.assertLess(scores[2], ct.OUTLIER * scores[1])
        self.assertEqual([x.number for x in ct.audit([rule(why)], found)], [2])


class TextsTest(unittest.TestCase):
    def test_each_number_is_fetched_once_and_a_missing_one_is_not_cached(self):
        calls = []

        def get(n):
            calls.append(n)
            return None if n == 9 else f"title {n}\nbody"

        with tempfile.TemporaryDirectory() as d:
            first = ct.texts([1, 9], Path(d), get)
            second = ct.texts([1, 9], Path(d), get)
        self.assertEqual(first, {1: "title 1\nbody", 9: None})
        self.assertEqual(second, first)
        self.assertEqual(sorted(calls), [1, 9, 9], "a cached text is read, not fetched again")


def answered(stdout="", code=0, stderr=""):
    return subprocess.CompletedProcess(["gh"], code, stdout, stderr)


class FetchTest(unittest.TestCase):
    def test_a_pr_brings_its_comments_reviews_review_comments_and_commits(self):
        replies = [answered('{"title": "T", "body": "B", "pull_request": {}}'),
                   answered("comment\n"), answered("review\n"), answered("inline\n"),
                   answered("commit message\n")]
        with mock.patch.object(ct, "gh", side_effect=replies) as gh:
            text = ct.fetch(5)
        self.assertEqual(text.split("\n")[:2], ["T", "B"])
        for part in ("comment", "review", "inline", "commit message"):
            self.assertIn(part, text)
        pages = [c.args[1] if c.args[0] == "--paginate" else c.args[0] for c in gh.call_args_list]
        self.assertEqual(pages, ["repos/{owner}/{repo}/issues/5",
                                 "repos/{owner}/{repo}/issues/5/comments",
                                 "repos/{owner}/{repo}/pulls/5/reviews",
                                 "repos/{owner}/{repo}/pulls/5/comments",
                                 "repos/{owner}/{repo}/pulls/5/commits"])

    def test_an_issue_brings_its_comments_alone(self):
        replies = [answered('{"title": "T", "body": null}'), answered("comment\n")]
        with mock.patch.object(ct, "gh", side_effect=replies) as gh:
            self.assertEqual(ct.fetch(6), "T\n\ncomment\n")
        self.assertEqual(gh.call_count, 2)

    def test_a_number_naming_nothing_is_none_and_any_other_failure_raises(self):
        for stderr in ("gh: Not Found (HTTP 404)", "gh: This issue was deleted (HTTP 410)"):
            with self.subTest(stderr=stderr):
                with mock.patch.object(ct, "gh", return_value=answered(code=1, stderr=stderr)):
                    self.assertIsNone(ct.fetch(7))
        with mock.patch.object(ct, "gh", return_value=answered(code=1, stderr="HTTP 502")):
            with self.assertRaises(subprocess.CalledProcessError):
                ct.fetch(7)


class ReportTest(unittest.TestCase):
    def test_a_number_naming_nothing_is_an_error(self):
        lines, complete = ct.report([rule("It broke (#1, #2).")], {1: "it broke\nbody", 2: None})
        self.assertFalse(complete)
        self.assertIn("MISSING x/references/r.md#r: #2 names no issue or PR", lines)
        self.assertStartsWith(lines[0], "citations: 1 rules, 2 citations of 2 numbers;")

    def test_main_exits_1_when_a_number_names_nothing(self):
        slug = subprocess.CompletedProcess([], 0, "o/r\n", "")
        missing = {340: None, 828: ITEMS[828], 1140: ITEMS[1140]}
        with mock.patch.object(ct, "gh", return_value=slug), \
                mock.patch.object(ct, "texts", return_value=missing), \
                mock.patch("sys.stdout"):
            self.assertEqual(ct.main(["boundary-cases-at-the-boundary"]), 1)
        with mock.patch.object(ct, "gh", return_value=slug), \
                mock.patch.object(ct, "texts", return_value=ITEMS), mock.patch("sys.stdout"):
            self.assertEqual(ct.main(["boundary-cases-at-the-boundary"]), 0)


class RulesTest(unittest.TestCase):
    def test_each_rule_is_read_with_its_why_joined(self):
        with tempfile.TemporaryDirectory() as d:
            ref = Path(d) / ".claude/skills/s/references/r.md"
            ref.parent.mkdir(parents=True)
            ref.write_text("# R\n\n## Do it\n`do-it`\n\n- **Rule.** x.\n- **Why.** It broke\n"
                           "  once (#1).\n\n## No why\n`no-why`\n\n- **Rule.** y.\n")
            got = ct.rules_in(Path(d))
        self.assertEqual(got, [ct.Rule(".claude/skills/s/references/r.md", "do-it",
                                       "It broke once (#1).")])


class UsageTest(unittest.TestCase):
    def test_an_unknown_key_is_a_usage_error(self):
        run = subprocess.run([sys.executable, str(HERE / "citations.py"), "no-such-rule-key"],
                             cwd=HERE, capture_output=True, text=True,
                             env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertIn("no rule has the key no-such-rule-key", run.stderr)

    def test_a_missing_tool_exits_2_never_a_verdict(self):
        run = subprocess.run([sys.executable, str(HERE / "citations.py")], cwd=HERE,
                             capture_output=True, text=True,
                             env={"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"})
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


if __name__ == "__main__":
    unittest.main()
