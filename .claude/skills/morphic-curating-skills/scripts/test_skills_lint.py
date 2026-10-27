"""Tests for skills_lint.

Run: python3 -m unittest discover -s .claude/skills/morphic-curating-skills/scripts
"""
import tempfile
import unittest
from pathlib import Path

import skills_lint

GOOD_SKILL = """---
name: good-skill
description: Use when testing.
---

See `references/r.md`, `scripts/tool.py`, `ir/a.go`, ir/a.go, TestKnown, TestKnown_Case,
ir-design §4.3, docs/parser-library.md#known, `morphic-harness` and [a link](references/r.md).
Run `go test ./ir/... $(python3 scripts/tool.py)` or `go run ./cmd/morphic-harness <spec>`
and `gh api repos/{owner}/{repo}`.
"""
RULE = ("## Do the thing\n`do-the-thing`\n\n- **Rule.** Do it, as `ir/a.go` and TestKnown do.\n"
        "- **Why.** It broke\n  once (#1).\n- **Probe.** ir-design §4.3.\n")
GOOD_REFERENCE = "# R\n\nWhat these rules are about.\n\n" + RULE


def write(root: Path, rel: str, text: str) -> Path:
    p = root / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(text)
    return p


class LintTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.root = Path(self.dir.name)
        write(self.root, "docs/parser-library.md", "### `known`\n")
        write(self.root, "docs/ir-design.md", "## 4. Types\n\n### 4.3 Unions\n")
        write(self.root, "ir/a.go", "package ir\n")
        (self.root / "cmd" / "morphic-harness").mkdir(parents=True)
        skill = ".claude/skills/good-skill/"
        write(self.root, skill + "SKILL.md", GOOD_SKILL)
        write(self.root, skill + "references/r.md", GOOD_REFERENCE)
        write(self.root, skill + "scripts/tool.py", "import json\n")
        write(self.root, skill + "scripts/test_tool.py", "import unittest\nimport tool\n")

    def tearDown(self):
        self.dir.cleanup()

    def lint(self):
        files = [p for p in (self.root / ".claude" / "skills").rglob("*") if p.is_file()]
        top_level, tests = {"docs", "ir", "cmd", ".claude"}, {"TestKnown", "TestKnown_Case"}
        lint = skills_lint.Linter(self.root, top_level, tests, files)
        return lint.run(), lint.findings

    def test_a_clean_skill_has_no_finding(self):
        self.assertEqual(self.lint(), (1, []))

    def test_each_planted_problem_is_reported(self):
        skill = ".claude/skills/Bad_Skill/"
        front = "---\nname: other\ndescription: For tests.\n---\n\n"
        body = ("Bad `ir/nope.go`, prose ir/gone.go, TestMissing, ir-design §99.1,"
                " parser-library.md#nope,\n"
                "`morphic-nope`, [x](references/none.md), /U" "sers/someone.\n"
                "Run `python3 scripts/gone.py --flag` and `go test ./ir/nope/...`.\n")
        write(self.root, skill + "SKILL.md", front + "word " * 501 + "\n" + body)
        write(self.root, skill + "references/orphan.md", "word " * 4001)
        write(self.root, skill + "scripts/tool.sh", "echo\n")
        write(self.root, skill + "scripts/new.py", "x = (y := 1)\nmatch x:\n    case 1: pass\n")
        n, got = self.lint()
        self.assertEqual(n, 2)
        bad = ".claude/skills/Bad_Skill/"
        want = [
            f"{bad}SKILL.md:1: name 'Bad_Skill' is not lowercase letters, digits and hyphens",
            f"{bad}SKILL.md:1: frontmatter name must be 'Bad_Skill'",
            f'{bad}SKILL.md:1: description must start "Use when"',
            f"{bad}SKILL.md: body is 520 words, over 500",
            f"{bad}references/orphan.md: 4001 words, over 4000",
            f"{bad}references/orphan.md: not named in {bad}SKILL.md",
            f"{bad}SKILL.md:7: ir/nope.go does not exist",
            f"{bad}SKILL.md:7: ir/gone.go does not exist",
            f"{bad}SKILL.md:7: no test named TestMissing",
            f"{bad}SKILL.md:7: docs/ir-design.md has no section 99.1",
            f"{bad}SKILL.md:7: docs/parser-library.md has no entry nope",
            f"{bad}SKILL.md:8: no skill or command named morphic-nope",
            f"{bad}SKILL.md:8: link target references/none.md does not exist",
            f"{bad}scripts/new.py: has no test_new.py beside it",
            f"{bad}scripts/tool.sh: scripts are Python files",
            f"{bad}SKILL.md:8: names one machine or a memory link",
            f"{bad}SKILL.md:9: scripts/gone.py does not exist",
            f"{bad}SKILL.md:9: ./ir/nope/... does not exist",
        ]
        # The line a too-new construct is reported at depends on the interpreter's parser.
        version = [g for g in got if " is not Python 3.9: " in g]
        self.assertEqual(len(version), 1, "\n".join(got))
        self.assertTrue(version[0].startswith(f"{bad}scripts/new.py:"), version[0])
        self.assertEqual(sorted(g for g in got if g not in version), sorted(want), "\n".join(got))

    def test_a_memory_link_is_caught_and_a_bracket_class_is_not(self):
        write(self.root, ".claude/skills/good-skill/references/r.md",
              "# R\n\nSee [" "[some-note]] and grep '[[:space:]]'.\n\n" + RULE)
        _, got = self.lint()
        want = ".claude/skills/good-skill/references/r.md:3: names one machine or a memory link"
        self.assertEqual(got, [want])

    def test_each_rule_problem_is_reported(self):
        ref = ".claude/skills/good-skill/references/r.md"
        bad = ("# R\n\n"
               "## No key\n\n- **Rule.** x.\n- **Why.** y (#1).\n\n"
               + RULE +
               "## Twice\n`do-the-thing`\n\n- **Rule.** x.\n- **Why.** y (#1).\n\n"
               "## Extra bullet\n`extra`\n\n- **Rule.** x.\n- **Also.** z.\n- **Why.** y (#1).\n\n"
               "## Seen\n`seen`\n\n- **Rule.** x.\n- **Why.** y (#1).\n- **Seen in #2** (a): q\n\n"
               "## No why\n`no-why`\n\n- **Rule.** x.\n- **Probe.** p.\n\n"
               "## Uncited\n`uncited`\n\n- **Rule.** x.\n- **Why.** It broke.\n\n"
               "## Long\n`long`\n\n- **Rule.** " + "word " * 150 + "\n- **Why.** y (#1).\n")
        write(self.root, ref, bad)
        _, got = self.lint()
        want = [
            f"{ref}:3: a rule's heading must be followed by its `key`",
            f"{ref}:16: key do-the-thing is also used at {ref}:9",
            f"{ref}:21: rule extra has Rule, Also, Why bullets, "
            "not Rule, Why and an optional Probe",
            f"{ref}:33: rule seen has a line that is no bullet of it",
            f"{ref}:35: rule no-why has Rule, Probe bullets, not Rule, Why and an optional Probe",
            f"{ref}:41: rule uncited cites no PR or issue in its Why",
            f"{ref}:47: rule long is 159 words, over 150",
        ]
        self.assertEqual(sorted(got), sorted(want), "\n".join(got))

    @unittest.skipUnless(hasattr(skills_lint.sys, "stdlib_module_names"),
                         "Python 3.9 lists no standard library")
    def test_an_import_outside_the_standard_library_is_reported(self):
        write(self.root, ".claude/skills/good-skill/scripts/tool.py", "import yaml\n")
        _, got = self.lint()
        want = (".claude/skills/good-skill/scripts/tool.py:1: "
                "imports yaml, outside the standard library")
        self.assertEqual(got, [want])


if __name__ == "__main__":
    unittest.main()
