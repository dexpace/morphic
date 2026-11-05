"""Tests for facts.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-reviewing-prs/scripts
"""
import contextlib
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path
from unittest import mock

import facts

REF = ".claude/skills/s/references/r.md"
B = "Run `go vet ./...` 2 times."
D = "Do d, after `references/r.md#a`."
MOVED_TO = ".claude/skills/s/references/q.md"
SKILL = ".claude/skills/s/SKILL.md"


def rule(key: str, body: str, probe: str | None = None, title: str = "") -> str:
    """Return a rule block: its heading, its key and its bullets."""
    text = (f"## {title or f'The {key} rule'}\n`{key}`\n\n- **Rule.** {body}\n"
            "- **Why.** It failed (#1).\n")
    return text + (f"- **Probe.** {probe}\n" if probe else "")


class RulesTest(unittest.TestCase):
    def test_a_rule_runs_from_its_keyed_heading_to_the_next(self):
        fenced = "Run:\n\n  ```bash\n  # a comment\n  make gate\n  ```"
        text = ("# Title\n\n" + rule("a", "Do a.", fenced) + "\n## Not a rule\n\nprose\n\n"
                + rule("b", "Do b."))
        found = facts.rules_in("r.md", text)
        self.assertEqual(sorted(found), ["a", "b"])
        self.assertEqual((found["a"].first, found["a"].last), (3, 13))
        self.assertEqual(found["a"].lines[-1], "  ```")

    def test_only_a_level_two_heading_ends_a_rule_as_the_lint_reads_one(self):
        text = rule("a", "Do a.") + "### Within\n# Also within\n\n" + rule("b", "Do b.")
        found = facts.rules_in("r.md", text)
        self.assertEqual(found["a"].lines[-2:], ("### Within", "# Also within"))

    def test_each_change_is_named(self):
        a = rule("a", "Do a.")
        cited = rule("c", "See `references/r.md#a`.")
        base = {**facts.rules_in("r.md", a + "\n" + rule("b", "Do b.") + "\n"
                                + rule("d", "Do d.", title="D")),
                **facts.rules_in("p.md", cited + "\n" + rule("e", "Do e.") + "\n"
                                 + rule("g", "G."))}
        head = {**facts.rules_in("r.md", rule("a", "Do\na.") + "\n" + rule("b", "Do b now.")),
                **facts.rules_in("q.md", rule("c", "See `references/q.md#a`.") + "\n"
                                 + rule("e", "Do e.") + "\n" + rule("f", "Do d.", title="D") + "\n"
                                 + rule("h", "New."))}
        found = facts.compare(base, head)
        self.assertEqual(list(found.items()), [
            ("c", "moved from p.md to q.md:1, same words but its citations' files"),
            ("e", "moved from p.md to q.md:7, same words"),
            ("f", "renamed from d at r.md:13 to q.md:13, same words"),
            ("h", "added at q.md:19"),
            ("b", "reworded at r.md:8"),
            ("g", "removed from p.md:13"),
        ])

    def test_a_retargeted_citation_in_place_is_an_edit_with_the_same_words(self):
        base = facts.rules_in("r.md", rule("a", "See `references/p.md#b`."))
        head = facts.rules_in("r.md", rule("a", "See `references/q.md#b`."))
        fact = facts.compare(base, head)["a"]
        self.assertEqual(fact, "edited at r.md:1, same words but its citations' files")
        self.assertTrue(facts.unchanged(fact))
        self.assertFalse(facts.unchanged("reworded at r.md:1"))

    def test_a_path_that_cites_no_rule_is_a_rewording(self):
        base = facts.rules_in("r.md", rule("a", "Read `references/p.md`."))
        head = facts.rules_in("r.md", rule("a", "Read `references/q.md`."))
        self.assertEqual(facts.compare(base, head), {"a": "reworded at r.md:1"})

    def test_a_rule_whose_words_change_is_never_called_renamed(self):
        base = facts.rules_in("r.md", rule("d", "Do d."))
        head = facts.rules_in("r.md", rule("f", "Do f."))
        self.assertEqual(facts.compare(base, head),
                         {"f": "added at r.md:1", "d": "removed from r.md:1"})

    def test_only_the_rule_and_probe_bullets_prescribe(self):
        text = "intro\n\n" + rule("a", "Run `go vet ./...`\n  then `make lint`.", "`git log -1`.")
        lines = text.split("\n")
        picked = facts.prescribing(facts.rules_in("r.md", text)["a"])
        self.assertEqual([lines[i] for i in picked],
                         ["- **Rule.** Run `go vet ./...`", "  then `make lint`.",
                          "- **Probe.** `git log -1`."])


class TextTest(unittest.TestCase):
    def test_a_fence_closes_on_a_bare_run_as_long(self):
        lines = ["````bash", "```", "```` x", "go test", "````", "after", "~~~text", "a", "~~~",
                 "``` a`b", "prose", "   ```bash", "  make x", "  ```"]
        self.assertEqual(facts.fenced(lines), {1: True, 2: True, 3: True, 7: False, 12: True})

    def test_commands_are_spans_with_a_space_and_shell_lines(self):
        lines = ["Run `git status` and `HEAD`.", "```bash", "make gate", "```",
                 "```text", "`not run`", "```"]
        self.assertEqual(facts.commands(lines, list(range(len(lines)))),
                         [(0, "git status"), (2, "make gate")])

    def test_a_number_is_one_a_reader_derives(self):
        cases = {
            "27 of 36 rules moved": True,
            "about 1240251 tokens": True,
            "Priority: P4": True,
            "3. **Facts**: the facts": False,
            "  12) an item": False,
            "1.5 times as long": True,
            "fixed in #1522 and (#1530)": False,
            "head `29372a248` at 29372a2489": False,
            "see [the run](https://example.com/run/1234)": False,
            "see [the rule](../r2.md)": False,
            "seen at https://example.com/1": False,
            "`python3.14 -B`": False,
            "deadbeef0 is a revision, deadbee1 too": False,
        }
        for line, want in cases.items():
            with self.subTest(line):
                self.assertEqual(facts.counted(line), want)

    def test_numbers_skip_fenced_lines(self):
        lines = ["3 rules", "```go", "const Max = 64", "```"]
        self.assertEqual(facts.numbers(lines, [0, 2]), [0])

    def test_every_closing_keyword_closes(self):
        body = ("Closes #1\nfixes: #2 and Resolved #3; close #4.\nSee #5. reclosed #6\nCloses #1\n"
                "Closes dexpace/morphic#7, fixes other/repo#8, Closes Dexpace/Morphic#9")
        self.assertEqual(facts.closing(body, [10], "dexpace/morphic"), [1, 2, 3, 4, 7, 9, 10])

    def test_a_remote_names_its_repository(self):
        for url in ("https://github.com/dexpace/morphic.git", "git@github.com:dexpace/morphic.git",
                    "https://github.com/dexpace/morphic", "ssh://git@github.com/dexpace/morphic/"):
            with self.subTest(url):
                self.assertEqual(facts.REPOSITORY.search(url).group(1), "dexpace/morphic")

    def test_a_file_and_its_test_pair_by_name(self):
        listed = {"s/x.py", "s/test_x.py", "s/y.py", "p/a.go", "p/a_test.go",
                  "p/a_internal_test.go", "p/b_test.go", "p/c.go", "p/q/a_test.go"}
        cases = {
            "s/x.py": ["s/test_x.py"],
            "s/test_x.py": ["s/x.py"],
            "s/y.py": [],
            "p/a.go": ["p/a_internal_test.go", "p/a_test.go"],
            "p/a_test.go": ["p/a.go"],
            "p/a_internal_test.go": ["p/a.go"],
            "p/b_test.go": [],
            "p/c.go": [],
            "p/r.md": [],
        }
        for path, want in cases.items():
            with self.subTest(path):
                self.assertEqual(facts.beside(path, listed), want)


FAKE_GH = """#!/usr/bin/env python3.14
import json, os, re, sys
data = json.load(open(os.environ["FAKE_GH"]))
args = sys.argv[1:]
with open(os.environ["FAKE_GH"] + ".log", "a") as log:
    log.write(" ".join(args) + "\\n")
if args[:2] == ["pr", "view"] and "view" in data:
    print(json.dumps(data["view"]))
elif args[:2] == ["pr", "list"] and "list" in data:
    print(json.dumps(data["list"]))
elif args[:2] == ["api", "graphql"] and "issues" in data:
    query = next(a for a in args if a.startswith("query="))
    asked = re.findall(r"i([0-9]+): issue", query)
    found = {f"i{n}": data["issues"].get(n) for n in asked}
    # As GitHub does: a number no issue has is null beside a NOT_FOUND error, and gh exits 1.
    errors = [{"type": data.get("error", "NOT_FOUND"), "path": ["repository", k]}
              for k, v in found.items() if v is None]
    print(json.dumps({"data": {"repository": found}, **({"errors": errors} if errors else {})}))
    sys.exit(1 if errors else 0)
else:
    print("gh: no such fake", file=sys.stderr)
    sys.exit(1)
"""


class ScriptTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.dir)
        self.bin = self.dir / "bin"
        self.bin.mkdir()
        (self.bin / "git").symlink_to(shutil.which("git"))
        (self.bin / "python3.14").symlink_to(sys.executable)
        env = {"PATH": str(self.bin), "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1",
               "GIT_AUTHOR_NAME": "a", "GIT_AUTHOR_EMAIL": "a@example.com",
               "GIT_COMMITTER_NAME": "a", "GIT_COMMITTER_EMAIL": "a@example.com",
               "FAKE_GH": str(self.dir / "gh.json")}
        patcher = mock.patch.dict(os.environ, env)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(os.chdir, os.getcwd())
        self.origin = self.dir / "origin.git"
        self.repo = self.dir / "repo"
        subprocess.run(["git", "init", "-q", "--bare", "-b", "main", str(self.origin)], check=True)
        subprocess.run(["git", "init", "-q", "-b", "main", str(self.repo)], check=True)
        self.git("remote", "add", "origin", str(self.origin))
        self.write(REF, rule("a", "Run `git log -1`.") + "\n" + rule("b", B) + "\n"
                   + rule("c", "Do c.") + "\n" + rule("d", D, title="D"))
        self.write(SKILL, "# S\n\nSee `references/r.md#a` and `references/r.md#b`.\n")
        self.write("scripts/x.py", "X = 1\nA = 1\nB = 1\nC = 1\nZ = 0\n")
        self.write("scripts/test_x.py", "import x\n")
        self.write("other.txt", "one\n")
        self.write("docs/old.md", "Has 7 parts.\n")
        self.commit("chore: start")
        self.git("push", "-q", "origin", "main")
        self.git("fetch", "-q", "origin")
        self.base = self.git("rev-parse", "HEAD")

    def git(self, *args: str, cwd: Path | None = None) -> str:
        return subprocess.run(["git", *args], cwd=cwd or self.repo, check=True,
                              capture_output=True, text=True).stdout.strip()

    def write(self, path: str, text: str, root: Path | None = None) -> None:
        f = (root or self.repo) / path
        f.parent.mkdir(parents=True, exist_ok=True)
        f.write_text(text)

    def commit(self, subject: str, root: Path | None = None) -> str:
        self.git("add", "-A", cwd=root)
        self.git("commit", "-q", "-m", subject, cwd=root)
        return self.git("rev-parse", "HEAD", cwd=root)

    def topic(self) -> str:
        """Commit a change that rewords, moves, renames, removes and adds rules."""
        self.git("switch", "-q", "-c", "topic")
        self.write(REF, rule("a", "Run `git log -2`, not `references/r.md#a`.") + "\n"
                   + rule("e", D, title="D"))
        self.write(MOVED_TO, rule("b", B) + "\n" + rule("f", "New.", "`git status --short`."))
        self.write(SKILL, "# S\n\nSee `references/r.md#a` and `references/q.md#b`.\n\n"
                          "Its 3 rules, and `make gate`, not `references/r.md#a-b`.\n")
        self.write("docs/d.md", "Run `go test ./...` 4 times.\n")
        return self.commit("feat: change the rules")

    def other_pr(self, number: int, path: str, text: str) -> str:
        """Push a PR's head from a clone, so this repository lacks its commit."""
        clone = self.dir / f"pr{number}"
        subprocess.run(["git", "clone", "-q", str(self.origin), str(clone)], check=True)
        self.write(path, text, clone)
        sha = self.commit(f"fix: pr {number}", clone)
        self.git("push", "-q", "origin", f"HEAD:refs/pull/{number}/head", cwd=clone)
        return sha

    def gh(self, data: dict) -> None:
        (self.dir / "gh.json").write_text(json.dumps(data))
        (self.bin / "gh").write_text(FAKE_GH)
        (self.bin / "gh").chmod(0o755)

    def run_facts(self, *argv: str) -> tuple[int, list[str]]:
        os.chdir(self.repo / ".claude")
        out = io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
            code = facts.main(list(argv))
        return code, out.getvalue().splitlines()

    def full_gh(self, head: str, *others: tuple[int, str]) -> None:
        self.gh({
            "view": {"body": "Moves 2 rules.\n\nCloses #11\nfixes #12\n",
                     "closingIssuesReferences": [{"number": 11}]},
            "list": [{"number": 5, "headRefOid": head}]
                    + [{"number": n, "headRefOid": sha} for n, sha in others],
            "issues": {"11": {"state": "OPEN", "updatedAt": "2000-01-01T00:00:00Z",
                              "comments": {"totalCount": 2}},
                       "12": {"state": "CLOSED", "updatedAt": "2999-01-01T00:00:00Z",
                              "comments": {"totalCount": 0}}},
        })

    def test_every_kind_of_fact_is_printed(self):
        conflicting = self.other_pr(7, SKILL, "# S\n\nRewritten.\n")
        apart = self.other_pr(8, "other.txt", "two\n")
        head = self.topic()
        self.full_gh(head, (7, conflicting), (8, apart))
        refs = self.git("for-each-ref")
        fetched = (self.repo / ".git" / "FETCH_HEAD").read_text()
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        want = [
            f"rule: b: moved from {REF} to {MOVED_TO}:1, same words",
            f"rule: f: added at {MOVED_TO}:7",
            f"rule: a: reworded at {REF}:1",
            f"rule: e: renamed from d at {REF}:19 to {REF}:7, same words",
            f"rule: c: removed from {REF}:13",
            f"cite: b: {SKILL}:3",
            "cite: f: none",
            f"cite: a: {SKILL}:3",
            f"cite: a: {REF}:10",
            "cite: e: none",
            "cite: c: none",
            f"command: {SKILL}:5: make gate",
            f"command: {MOVED_TO}:12: git status --short",
            f"command: {REF}:4: git log -2",
            f"number: {SKILL}:5: Its 3 rules, and `make gate`, not `references/r.md#a-b`.",
            "number: docs/d.md:1: Run `go test ./...` 4 times.",
            "number: body:1: Moves 2 rules.",
            "issue: #11: OPEN, 2 comment(s), updated 2000-01-01T00:00:00Z",
            "issue: #12: CLOSED, 0 comment(s), updated 2999-01-01T00:00:00Z, after the change's "
            "last commit",
            f"pr: #7: shares {SKILL}; conflicts in {SKILL}",
            "facts: 5 rule, 6 cite, 3 command, 3 number, 2 issue, 1 pr, 0 stale",
        ]
        self.assertEqual(lines, want)
        self.assertEqual(self.git("for-each-ref"), refs, "a run writes no ref")
        self.assertEqual((self.repo / ".git" / "FETCH_HEAD").read_text(), fetched)

    def test_a_shared_file_that_merges_says_so(self):
        clean = self.other_pr(9, "scripts/x.py", "X = 2\nA = 1\nB = 1\nC = 1\nZ = 0\n")
        self.topic()
        self.write("scripts/x.py", "X = 1\nA = 1\nB = 1\nC = 1\nZ = 9\n")
        head = self.commit("feat: add Y")
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}, {"number": 9, "headRefOid": clean}]})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertIn("pr: #9: shares scripts/x.py; merges clean", lines)

    def test_a_conflict_the_base_shares_is_marked(self):
        apart = self.other_pr(10, "scripts/x.py", "X = 2\nA = 1\nB = 1\nC = 1\nZ = 0\n")
        self.git("fetch", "-q", "origin", "refs/pull/10/head")
        self.git("switch", "-q", "-c", "pr10", "FETCH_HEAD")
        self.write("other.txt", "ten\n")
        apart = self.commit("fix: pr 10 again")
        self.git("push", "-q", "origin", "HEAD:refs/pull/10/head")
        self.git("switch", "-q", "main")
        self.write("other.txt", "main\n")
        self.commit("fix: main moves")
        self.git("push", "-q", "origin", "main")
        self.git("fetch", "-q", "origin")
        self.topic()
        self.write("scripts/x.py", "X = 1\nA = 1\nB = 1\nC = 1\nZ = 9\n")
        head = self.commit("feat: change Z")
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}, {"number": 10, "headRefOid": apart}]})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertIn("pr: #10: shares scripts/x.py; conflicts in other.txt (also against "
                      "origin/main)", lines)

    def test_another_pr_is_read_from_its_own_fork_with_main(self):
        self.write("scripts/x.py", "X = 1\nA = 1\nB = 1\nC = 1\nZ = 5\n")
        self.commit("feat: start a stack")
        self.git("push", "-q", "origin", "HEAD:refs/heads/stack")
        self.git("reset", "-q", "--hard", self.base)
        self.write(SKILL, "# Main\n\nSee `references/r.md#a` and `references/r.md#b`.\n")
        self.commit("fix: main moves")
        self.git("push", "-q", "origin", "main")
        later = self.other_pr(11, "other.txt", "eleven\n")
        self.git("fetch", "-q", "origin")
        self.git("switch", "-q", "--detach", "origin/stack")
        head = self.topic()
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}, {"number": 11, "headRefOid": later}]})
        code, lines = self.run_facts("--base", "origin/stack", "5")
        self.assertEqual(code, 0, lines)
        self.assertFalse([line for line in lines if line.startswith("pr: ")], lines)

    def test_a_stacked_review_marks_a_conflict_main_shares(self):
        self.write("scripts/test_x.py", "import x\n# stack\n")
        self.commit("test: start a stack")
        self.git("push", "-q", "origin", "HEAD:refs/heads/stack")
        self.git("reset", "-q", "--hard", self.base)
        self.other_pr(12, "scripts/x.py", "X = 2\nA = 1\nB = 1\nC = 1\nZ = 0\n")
        clone = self.dir / "pr12"
        self.write("other.txt", "twelve\n", clone)
        twelve = self.commit("fix: pr 12 again", clone)
        self.git("push", "-q", "origin", "HEAD:refs/pull/12/head", cwd=clone)
        self.write("other.txt", "main\n")
        self.commit("fix: main moves")
        self.git("push", "-q", "origin", "main")
        self.git("fetch", "-q", "origin")
        self.git("switch", "-q", "--detach", "origin/stack")
        self.git("merge", "-q", "--no-edit", "origin/main")
        self.topic()
        self.write("scripts/x.py", "X = 1\nA = 1\nB = 1\nC = 1\nZ = 9\n")
        head = self.commit("feat: change Z")
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}, {"number": 12, "headRefOid": twelve}]})
        code, lines = self.run_facts("--base", "origin/stack", "5")
        self.assertEqual(code, 0, lines)
        self.assertIn("pr: #12: shares scripts/x.py; conflicts in other.txt (also against "
                      "origin/main)", lines)

    def test_a_stack_behind_main_counts_none_of_mains_changes(self):
        self.write("scripts/test_x.py", "import x\n# stack\n")
        self.commit("test: start a stack")
        self.git("push", "-q", "origin", "HEAD:refs/heads/stack")
        self.git("reset", "-q", "--hard", self.base)
        added = rule("m", "Run `go test ./m` 3 times.")
        self.write(REF, (self.repo / REF).read_text() + "\n" + added)
        self.write("docs/main.md", "Main took 8 steps.\n")
        self.commit("feat: main adds a rule")
        self.git("push", "-q", "origin", "main")
        self.git("fetch", "-q", "origin")
        self.git("switch", "-q", "--detach", "origin/stack")
        self.git("merge", "-q", "--no-edit", "origin/main")
        self.write(SKILL, (self.repo / SKILL).read_text() + "\nIts 5 rules.\n")
        head = self.commit("docs: count the rules")
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}]})
        code, lines = self.run_facts("--base", "origin/stack", "5")
        self.assertEqual(code, 0, lines)
        self.assertEqual(lines, [
            f"number: {SKILL}:5: Its 5 rules.",
            "facts: 0 rule, 0 cite, 0 command, 1 number, 0 issue, 0 pr, 0 stale",
        ])

    def test_a_stack_base_that_conflicts_with_main_is_not_read_apart(self):
        self.write("other.txt", "stack\n")
        self.commit("fix: start a stack")
        self.git("push", "-q", "origin", "HEAD:refs/heads/stack")
        self.git("reset", "-q", "--hard", self.base)
        self.write("other.txt", "main\n")
        self.write("docs/main.md", "Main took 8 steps.\n")
        self.commit("fix: main moves")
        self.git("push", "-q", "origin", "main")
        self.git("fetch", "-q", "origin")
        self.git("switch", "-q", "--detach", "origin/stack")
        subprocess.run(["git", "merge", "-q", "origin/main"], cwd=self.repo, capture_output=True)
        self.write("other.txt", "resolved\n")
        self.git("add", "other.txt")
        self.git("commit", "-q", "--no-edit")
        self.topic()
        self.gh({"view": {"body": "", "closingIssuesReferences": []}, "list": []})
        code, lines = self.run_facts("--base", "origin/stack", "5")
        self.assertEqual(code, 2)
        self.assertIn(f"rule: a: reworded at {REF}:1", lines)
        self.assertFalse([line for line in lines if "docs/main.md" in line], lines)
        self.assertIn("the change apart from origin/main's commits in other.txt (origin/stack and "
                      "origin/main conflict there", lines[-1])

    def test_a_moved_markdown_file_adds_no_lines(self):
        self.git("config", "diff.renames", "false")
        self.topic()
        self.git("mv", "docs/old.md", "docs/moved.md")
        self.commit("docs: move a page")
        self.gh({"view": {"body": "", "closingIssuesReferences": []}, "list": []})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertFalse([line for line in lines if "docs/moved.md" in line], lines)

    def test_a_pr_editing_a_file_the_change_moves_shares_it(self):
        fourteen = self.other_pr(14, "other.txt", "fourteen\n")
        self.topic()
        self.git("mv", "other.txt", "moved.txt")
        head = self.commit("chore: move a file")
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head},
                          {"number": 14, "headRefOid": fourteen}]})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertIn("pr: #14: shares other.txt; merges clean", lines)

    def test_an_issue_is_late_only_after_the_changes_own_last_commit(self):
        with mock.patch.dict(os.environ, {"GIT_COMMITTER_DATE": "2021-01-01T00:00:00+00:00"}):
            self.topic()
        with mock.patch.dict(os.environ, {"GIT_COMMITTER_DATE": "2023-01-01T00:00:00+00:00"}):
            self.git("switch", "-q", "main")
            self.write("other.txt", "main\n")
            self.commit("fix: main moves")
            self.git("push", "-q", "origin", "main")
            self.git("fetch", "-q", "origin")
            self.git("switch", "-q", "topic")
            self.git("merge", "-q", "--no-edit", "origin/main")
        updated = {"11": "2021-01-01T00:00:00Z", "12": "2022-01-01T00:00:00Z"}
        self.gh({"view": {"body": "Closes #11\nCloses #12", "closingIssuesReferences": []},
                 "list": [], "issues": {n: {"state": "OPEN", "updatedAt": u,
                                            "comments": {"totalCount": 0}}
                                        for n, u in updated.items()}})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertIn("issue: #11: OPEN, 0 comment(s), updated 2021-01-01T00:00:00Z", lines)
        self.assertIn("issue: #12: OPEN, 0 comment(s), updated 2022-01-01T00:00:00Z, after the "
                      "change's last commit", lines)

    def test_a_stacked_changes_last_commit_is_its_own(self):
        self.write("scripts/test_x.py", "import x\n# stack\n")
        self.commit("test: start a stack")
        self.git("push", "-q", "origin", "HEAD:refs/heads/stack")
        self.git("reset", "-q", "--hard", self.base)
        self.git("fetch", "-q", "origin")
        self.git("switch", "-q", "--detach", "origin/stack")
        with mock.patch.dict(os.environ, {"GIT_COMMITTER_DATE": "2021-01-01T00:00:00+00:00"}):
            self.topic()
        with mock.patch.dict(os.environ, {"GIT_COMMITTER_DATE": "2023-01-01T00:00:00+00:00"}):
            self.git("switch", "-q", "main")
            self.write("other.txt", "main\n")
            self.commit("fix: main moves")
            self.git("push", "-q", "origin", "main")
            self.git("fetch", "-q", "origin")
            self.git("switch", "-q", "topic")
            self.git("merge", "-q", "--no-edit", "origin/main")
        self.gh({"view": {"body": "Closes #12", "closingIssuesReferences": []}, "list": [],
                 "issues": {"12": {"state": "OPEN", "updatedAt": "2022-01-01T00:00:00Z",
                                   "comments": {"totalCount": 0}}}})
        code, lines = self.run_facts("--base", "origin/stack", "5")
        self.assertEqual(code, 0, lines)
        self.assertIn("issue: #12: OPEN, 0 comment(s), updated 2022-01-01T00:00:00Z, after the "
                      "change's last commit", lines)

    def test_a_conflict_each_base_shares_names_both(self):
        self.other_pr(13, "scripts/x.py", "X = 2\nA = 1\nB = 1\nC = 1\nZ = 0\n")
        clone = self.dir / "pr13"
        self.write("other.txt", "thirteen\n", clone)
        thirteen = self.commit("fix: pr 13 again", clone)
        self.git("push", "-q", "origin", "HEAD:refs/pull/13/head", cwd=clone)
        self.write("other.txt", "main\n")
        self.commit("fix: main moves")
        self.git("push", "-q", "origin", "main")
        self.write("scripts/test_x.py", "import x\n# stack\n")
        self.commit("test: start a stack")
        self.git("push", "-q", "origin", "HEAD:refs/heads/stack")
        self.git("fetch", "-q", "origin")
        self.git("switch", "-q", "--detach", "origin/stack")
        self.topic()
        self.write("scripts/x.py", "X = 1\nA = 1\nB = 1\nC = 1\nZ = 9\n")
        head = self.commit("feat: change Z")
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head},
                          {"number": 13, "headRefOid": thirteen}]})
        code, lines = self.run_facts("--base", "origin/stack", "5")
        self.assertEqual(code, 0, lines)
        self.assertIn("pr: #13: shares scripts/x.py; conflicts in other.txt (also against "
                      "origin/stack and origin/main)", lines)

    def test_a_head_already_held_is_not_fetched(self):
        head = self.topic()
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head},
                          {"number": 20, "headRefOid": self.base}]})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)

    def test_since_lists_what_moved_after_a_round(self):
        reviewed = self.topic()
        self.write(REF, rule("a", "Run `git log -3`.") + "\n" + rule("e", D, title="D")
                   + "\n" + rule("b", B))
        self.write(MOVED_TO, rule("f", "New.", "`git status --short`."))
        self.write(SKILL, "# S\n\nSee `references/r.md#a` and `references/r.md#b`.\n\nIts 3.\n")
        self.write("scripts/x.py", "X = 3\n")
        self.write("p/a.go", "package p\n")
        self.write("p/a_test.go", "package p\n")
        self.git("mv", "docs/d.md", "docs/e.md")
        head = self.commit("fix: reword a again")
        self.full_gh(head)
        code, lines = self.run_facts("--since", reviewed, "5")
        self.assertEqual(code, 0, lines)
        stale = [line for line in lines if line.startswith("stale: ")]
        self.assertEqual(stale, [
            f"stale: {SKILL}",
            f"stale: {MOVED_TO}",
            f"stale: {REF}",
            "stale: docs/d.md (outside the change)",
            "stale: docs/e.md",
            "stale: p/a.go",
            "stale: p/a_test.go",
            "stale: scripts/x.py",
            f"stale: {SKILL}:3: cites a, reworded at {REF}:1",
            f"stale: {REF}:10: cites a, reworded at {REF}:1",
            "stale: scripts/test_x.py: beside scripts/x.py",
        ])

    def test_a_change_main_made_is_stale_outside_the_change(self):
        reviewed = self.topic()
        self.git("switch", "-q", "main")
        self.write("other.txt", "three\n")
        self.commit("fix: main moves")
        self.git("push", "-q", "origin", "main")
        self.git("fetch", "-q", "origin")
        self.git("switch", "-q", "topic")
        self.git("merge", "-q", "--no-edit", "origin/main")
        self.full_gh(self.git("rev-parse", "HEAD"))
        code, lines = self.run_facts("--since", reviewed, "5")
        self.assertEqual(code, 0, lines)
        self.assertIn("stale: other.txt (outside the change)", lines)

    def test_a_body_file_stands_in_before_the_pr_exists(self):
        self.topic()
        body = self.dir / "body.md"
        body.write_text("Resolves #12, in 4 steps.\n")
        self.gh({"list": [], "issues": {"12": {"state": "OPEN", "updatedAt": "2000-01-01T00:00:00Z",
                                                "comments": {"totalCount": 1}}}})
        code, lines = self.run_facts("--body", str(body), "5")
        self.assertEqual(code, 0, lines)
        self.assertIn("number: body:1: Resolves #12, in 4 steps.", lines)
        self.assertIn("issue: #12: OPEN, 1 comment(s), updated 2000-01-01T00:00:00Z", lines)
        log = (self.dir / "gh.json.log").read_text()
        self.assertNotIn("pr view", log)

    def test_what_gh_cannot_read_is_named_and_the_rest_printed(self):
        self.topic()
        code, lines = self.run_facts("5")
        self.assertEqual(code, 2)
        self.assertIn(f"rule: a: reworded at {REF}:1", lines)
        self.assertTrue(lines[-1].startswith("facts: not read: the body and the issues it "
                                             "closes ("), lines[-1])
        self.assertIn("; the open PRs (", lines[-1])

    def test_a_number_that_is_no_issue_is_said(self):
        head = self.topic()
        body = f"Closes {self.dir.name}/origin#13"
        self.gh({"view": {"body": body, "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}], "issues": {}})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertIn("issue: #13: no issue of that number", lines)

    def test_an_error_other_than_a_missing_issue_is_not_read(self):
        head = self.topic()
        self.gh({"view": {"body": "Closes #13", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}], "issues": {}, "error": "FORBIDDEN"})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 2)
        self.assertNotIn("issue: #13: no issue of that number", lines)
        self.assertIn("the issues the PR closes (", lines[-1])

    def test_a_query_gh_cannot_send_says_why(self):
        head = self.topic()
        self.gh({"view": {"body": "Closes #13", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}]})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 2)
        self.assertIn("the issues the PR closes (gh: no such fake)", lines[-1])

    def test_a_head_that_cannot_be_fetched_is_not_read(self):
        head = self.topic()
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head},
                          {"number": 6, "headRefOid": "0" * 40}]})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 2)
        self.assertIn("the heads of open PRs this repository lacks", lines[-1])
        self.assertIn("#6's head 000000000000 (not fetched)", lines[-1])

    def test_a_listing_at_its_limit_says_more_may_be_open(self):
        head = self.topic()
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}, {"number": 4, "headRefOid": head}]})
        with mock.patch.object(facts, "OPEN_PR_LIMIT", 2):
            code, lines = self.run_facts("5")
        self.assertEqual(code, 2)
        self.assertIn("the open PRs past the first 2", lines[-1])

    def test_an_added_line_reading_like_a_header_is_counted(self):
        self.topic()
        self.write("docs/d.md", "Run `go test ./...` 4 times.\n++ 5 more\n")
        self.commit("docs: add a line")
        self.gh({"view": {"body": "", "closingIssuesReferences": []}, "list": []})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertIn("number: docs/d.md:2: ++ 5 more", lines)

    def test_a_path_git_would_quote_is_read(self):
        self.topic()
        self.write("docs/é.md", "Takes 6 steps.\n")
        self.commit("docs: add a page")
        self.gh({"view": {"body": "", "closingIssuesReferences": []}, "list": []})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 0, lines)
        self.assertIn("number: docs/é.md:1: Takes 6 steps.", lines)

    def test_a_pr_with_no_history_in_common_is_not_read(self):
        head = self.topic()
        orphan = self.dir / "orphan"
        subprocess.run(["git", "init", "-q", "-b", "o", str(orphan)], check=True)
        self.write(SKILL, "# Other\n", orphan)
        sha = self.commit("chore: unrelated", orphan)
        self.git("push", "-q", str(self.origin), "HEAD:refs/pull/3/head", cwd=orphan)
        self.gh({"view": {"body": "", "closingIssuesReferences": []},
                 "list": [{"number": 5, "headRefOid": head}, {"number": 3, "headRefOid": sha}]})
        code, lines = self.run_facts("5")
        self.assertEqual(code, 2)
        self.assertIn("#3's files and merge (", lines[-1])

    def test_a_ref_it_cannot_read_stops_it(self):
        self.topic()
        for argv in (["--base", "nowhere", "5"], ["--since", "nowhere", "5"]):
            with self.subTest(argv):
                code, lines = self.run_facts(*argv)
                self.assertEqual((code, lines), (2, []))

    def test_the_number_is_positive(self):
        with self.assertRaises(SystemExit), contextlib.redirect_stderr(io.StringIO()):
            facts.parse(["0"])


class EntryTest(unittest.TestCase):
    def test_it_runs_as_a_script(self):
        script = Path(facts.__file__)
        done = subprocess.run([sys.executable, str(script), "--help"], capture_output=True,
                              text=True)
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertIn(textwrap.shorten(facts.__doc__.split("\n\n")[0], 40, placeholder=""),
                      " ".join(done.stdout.split()))


if __name__ == "__main__":
    unittest.main()
