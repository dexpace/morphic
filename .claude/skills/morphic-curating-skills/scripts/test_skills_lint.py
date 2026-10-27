"""Tests for skills_lint. Run: python3 -m unittest discover -s .claude/skills/morphic-curating-skills/scripts"""
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
"""
GOOD_REFERENCE = "# R\n\n- **Seen in #1** (abc): `ir/gone.go` TestGone ir-design §9.9\n"


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
        lint = skills_lint.Linter(self.root, {"docs", "ir", "cmd", ".claude"}, {"TestKnown", "TestKnown_Case"}, files)
        return lint.run(), lint.findings

    def test_a_clean_skill_has_no_finding(self):
        self.assertEqual(self.lint(), (1, []))

    def test_each_planted_problem_is_reported(self):
        skill = ".claude/skills/Bad_Skill/"
        write(self.root, skill + "SKILL.md", "---\nname: other\ndescription: For tests.\n---\n\n" + "word " * 501 + "\n"
              "Bad `ir/nope.go`, prose ir/gone.go, TestMissing, ir-design §99.1, parser-library.md#nope,\n"
              "`morphic-nope`, [x](references/none.md), /U" "sers/someone.\n")
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
            f"{bad}SKILL.md: body is 512 words, over 500",
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
        ]
        # The line a too-new construct is reported at depends on the interpreter's parser.
        version = [g for g in got if " is not Python 3.9: " in g]
        self.assertEqual(len(version), 1, "\n".join(got))
        self.assertTrue(version[0].startswith(f"{bad}scripts/new.py:"), version[0])
        self.assertEqual(sorted(g for g in got if g not in version), sorted(want), "\n".join(got))

    def test_a_memory_link_is_caught_and_a_bracket_class_is_not(self):
        write(self.root, ".claude/skills/good-skill/references/r.md",
              GOOD_REFERENCE + "See [" "[some-note]] and grep '[[:space:]]'.\n")
        _, got = self.lint()
        self.assertEqual(got, [".claude/skills/good-skill/references/r.md:4: names one machine or a memory link"])

    @unittest.skipUnless(hasattr(skills_lint.sys, "stdlib_module_names"), "Python 3.9 lists no standard library")
    def test_an_import_outside_the_standard_library_is_reported(self):
        write(self.root, ".claude/skills/good-skill/scripts/tool.py", "import yaml\n")
        _, got = self.lint()
        self.assertEqual(got, [".claude/skills/good-skill/scripts/tool.py:1: imports yaml, outside the standard library"])


if __name__ == "__main__":
    unittest.main()
