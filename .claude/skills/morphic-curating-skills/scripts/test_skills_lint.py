"""Tests for skills_lint.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-curating-skills/scripts
"""
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import skills_lint

# A script's first line and the script block it declares Python 3.14 in, written out rather than
# read from the lint.
SHEBANG = "#!/usr/bin/env python3.14\n"
HEADER = '# /// script\n# requires-python = ">=3.14"\n# ///\n'
GOOD_SKILL = """---
name: good-skill
description: Use when testing.
---

See `references/r.md`, `scripts/tool.py`, `ir/a.go`, ir/a.go, TestKnown, TestKnown_Case,
ir-design §4.3, docs/parser-library.md#known, `morphic-harness` and [a link](references/r.md).
Run `go test ./ir/... $(python3.14 scripts/tool.py)` or `go run ./cmd/morphic-harness <spec>`
and `gh api repos/{owner}/{repo}`, by `references/r.md#do-the-thing` and
`.claude/skills/good-skill/references/r.md#do-the-thing`; see `references/<file>.md#<key>`.
Name `tool.py`, `a.go` and `SKILL.md` alone, and a suffix such as `_test.go`.
"""
RULE = ("## Do the thing\n`do-the-thing`\n\n- **Rule.** Do it, as `ir/a.go` and TestKnown do.\n"
        "- **Why.** It broke\n  once (#1).\n- **Probe.** ir-design §4.3.\n")
GOOD_REFERENCE = "# R\n\nWhat these rules are about.\n\n" + RULE


def write(root: Path, rel: str, text: str) -> Path:
    p = root / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(text)
    return p


def script(root: Path, rel: str, text: str) -> Path:
    """Write a script with the first line and the mode the lint requires."""
    p = write(root, rel, SHEBANG + text)
    p.chmod(0o755)
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
        script(self.root, skill + "scripts/tool.py", HEADER + "import json\n")
        write(self.root, skill + "scripts/test_tool.py", "import unittest\nimport tool\n")

    def tearDown(self):
        self.dir.cleanup()

    def lint(self):
        files = [p for p in (self.root / ".claude" / "skills").rglob("*") if p.is_file()]
        top_level, tests = {"docs", "ir", "cmd", ".claude"}, {"TestKnown", "TestKnown_Case"}
        names = {p.name for p in self.root.rglob("*") if p.is_file()}
        lint = skills_lint.Linter(self.root, top_level, tests, names, files)
        return lint.run(), lint.findings

    def test_a_clean_skill_has_no_finding(self):
        self.assertEqual(self.lint(), (1, []))

    def test_each_planted_problem_is_reported(self):
        skill = ".claude/skills/Bad_Skill/"
        front = "---\nname: other\ndescription: For tests.\n---\n\n"
        body = ("Bad `ir/nope.go`, prose ir/gone.go, TestMissing, ir-design §99.1,"
                " parser-library.md#nope,\n"
                "`morphic-nope`, [x](references/none.md), /U" "sers/someone.\n"
                "Run `python3 scripts/gone.py --flag` and `go test ./ir/nope/...`, by `no-rule`.\n"
                "Cite `do-the-thing`, `references/r.md#gone` and "
                "`.claude/skills/good-skill/references/r.md#gone`, and `gone.py` alone.\n")
        write(self.root, skill + "SKILL.md", front + "word " * 501 + "\n" + body)
        write(self.root, skill + "references/orphan.md", "word " * 4001)
        write(self.root, skill + "scripts/tool.sh", "echo\n")
        write(self.root, skill + "scripts/new.py", "def broken(:\n")
        n, got = self.lint()
        self.assertEqual(n, 2)
        bad = ".claude/skills/Bad_Skill/"
        want = [
            f"{bad}SKILL.md:1: name 'Bad_Skill' is not 1-64 lowercase letters and digits joined by "
            "single hyphens",
            f"{bad}SKILL.md:1: frontmatter name must be 'Bad_Skill'",
            f'{bad}SKILL.md:1: description must start "Use when"',
            f"{bad}SKILL.md: body is 530 words, over 500",
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
            f"{bad}scripts/new.py: not named in {bad}SKILL.md",
            f'{bad}scripts/new.py: declares no `requires-python = ">=3.14"` script block',
            f"{bad}scripts/new.py: has no `#!/usr/bin/env python3.14` first line",
            f"{bad}scripts/new.py: is not executable",
            f"{bad}scripts/tool.sh: scripts are Python files",
            f"{bad}SKILL.md:8: names one machine or a memory link",
            f"{bad}SKILL.md:9: scripts/gone.py does not exist",
            f"{bad}SKILL.md:9: ./ir/nope/... does not exist",
            f"{bad}SKILL.md:10: cites rule do-the-thing without the file that holds it",
            f"{bad}SKILL.md:10: references/r.md holds no rule gone",
            f"{bad}SKILL.md:10: .claude/skills/good-skill/references/r.md holds no rule gone",
            f"{bad}SKILL.md:10: no file named gone.py",
        ]
        # The line a too-new construct is reported at depends on the interpreter's parser.
        version = [g for g in got if " is not Python 3.14: " in g]
        self.assertEqual(len(version), 1, "\n".join(got))
        self.assertStartsWith(version[0], f"{bad}scripts/new.py:", version[0])
        self.assertEqual(sorted(g for g in got if g not in version), sorted(want), "\n".join(got))

    def test_a_code_span_broken_across_lines_is_reported_once(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, skill,
              GOOD_SKILL + "Run `go vet\n--all` there.\n\n```markdown\n`odd\n```\n")
        line = GOOD_SKILL.count("\n") + 1
        self.assertEqual(self.lint()[1], [f"{skill}:{line}: a code span breaks onto the next line"])

    def test_a_section_wrapped_onto_the_next_line_is_checked_once(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, skill, GOOD_SKILL + "See ir-design\n"
              "§99.1 and ir-design §97.1, then ir-design §\n98.2, but ir-design §4.3 holds.\n")
        n = GOOD_SKILL.count("\n") + 1
        want = [f"{skill}:{n}: docs/ir-design.md has no section 99.1",
                f"{skill}:{n + 1}: docs/ir-design.md has no section 97.1",
                f"{skill}:{n + 1}: docs/ir-design.md has no section 98.2"]
        self.assertEqual(sorted(self.lint()[1]), want)

    def test_a_name_is_held_to_the_specification(self):
        for name in ("double--hyphen", "-leading", "trailing-", "x" * 65):
            with self.subTest(name=name):
                skill = f".claude/skills/{name}/"
                write(self.root, skill + "SKILL.md",
                      f"---\nname: {name}\ndescription: Use when testing.\n---\n")
                _, got = self.lint()
                want = (f"{skill}SKILL.md:1: name {name!r} is not 1-64 lowercase letters and "
                        "digits joined by single hyphens")
                self.assertIn(want, got)
                (self.root / skill / "SKILL.md").unlink()
                (self.root / skill).rmdir()
        write(self.root, ".claude/skills/" + "x" * 64 + "/SKILL.md",
              "---\nname: " + "x" * 64 + "\ndescription: Use when testing.\n---\n")
        self.assertEqual(self.lint()[1], [], "64 characters is within the limit")

    def test_frontmatter_holds_only_fields_once_and_values_yaml_reads_whole(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, skill, "---\nname: good-skill\n"
              "description: Use when testing: a colon.\n"
              "descripton: typo\n"
              "license: MIT #note\n"
              "compatibility: Needs git:\n"
              "name: good-skill\n"
              "metadata:\n  author: x\n"
              "allowed-tools: 'Bash(a: b) #ok'\n"
              "# a comment\n"
              "not a key\n"
              "---\n\n" + GOOD_SKILL.split("---\n", 2)[2])
        _, got = self.lint()
        want = [
            f"{skill}:3: description holds an unquoted ':', which YAML refuses: quote it",
            f"{skill}:4: descripton is not a frontmatter field (name, description, license, "
            "compatibility, metadata, allowed-tools)",
            f"{skill}:5: license holds ' #', which YAML reads as a comment that cuts the value: "
            "quote it",
            f"{skill}:6: compatibility holds an unquoted ':', which YAML refuses: quote it",
            f"{skill}:7: name is set twice, and YAML keeps only the last",
            f"{skill}:12: frontmatter line is no key: value",
        ]
        self.assertEqual(sorted(got), sorted(want), "\n".join(got))

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

    def test_an_import_outside_the_standard_library_is_reported(self):
        write(self.root, ".claude/skills/other-skill/SKILL.md",
              "---\nname: other-skill\ndescription: Use when testing.\n---\n\n"
              "Run `scripts/shared.py`.\n")
        script(self.root, ".claude/skills/other-skill/scripts/shared.py", HEADER + "x = 1\n")
        write(self.root, ".claude/skills/other-skill/scripts/test_shared.py", "import shared\n")
        script(self.root, ".claude/skills/good-skill/scripts/tool.py",
               HEADER + "import json\nimport concurrent.futures\nfrom os import path\n"
               "import no_such_module_anywhere\nimport shared\n")
        _, got = self.lint()
        want = (".claude/skills/good-skill/scripts/tool.py:8: "
                "imports no_such_module_anywhere, outside the standard library")
        self.assertEqual(got, [want])

    def test_a_script_declares_python_3_14_in_its_script_block(self):
        tool = ".claude/skills/good-skill/scripts/tool.py"
        want = f'{tool}: declares no `requires-python = ">=3.14"` script block'
        for text in ("import json\n", HEADER.replace("3.14", "3.9") + "import json\n"):
            with self.subTest(text=text):
                script(self.root, tool, text)
                self.assertEqual(self.lint()[1], [want])

    def test_a_script_runs_on_its_own_under_python_3_14(self):
        tool = ".claude/skills/good-skill/scripts/tool.py"
        first = f"{tool}: has no `#!/usr/bin/env python3.14` first line"
        cases = [(HEADER, 0o755, first), ("#!/usr/bin/env python3\n" + HEADER, 0o755, first),
                 ("\n" + SHEBANG + HEADER, 0o755, first),
                 (SHEBANG + HEADER, 0o644, f"{tool}: is not executable")]
        for text, mode, want in cases:
            with self.subTest(text=text, mode=oct(mode)):
                write(self.root, tool, text + "import json\n").chmod(mode)
                self.assertEqual(self.lint()[1], [want])


class LoadTreeTest(unittest.TestCase):
    def test_tests_top_level_entries_and_every_file_name_are_read(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
                   "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
            git = ["git", "-C", d]
            subprocess.run(git + ["init", "-q"], check=True, env=env)
            write(root, "ir/a_test.go", "package ir\n\nfunc TestA_B(t *testing.T) {}\n")
            write(root, ".gitignore", "out/\n")
            subprocess.run(git + ["add", "-A"], check=True, env=env)
            subprocess.run(git + ["commit", "-qm", "x"], check=True, env=env)
            write(root, "scripts/new.py", "")
            write(root, "out/built.txt", "")
            top_level, tests, names = skills_lint.load_tree(root)
        self.assertEqual(top_level, {".claude", ".gitignore", "ir"})
        self.assertEqual(tests, {"TestA", "TestA_B"})
        self.assertEqual(names, {".gitignore", "a_test.go", "new.py"},
                         "untracked files count, ignored ones do not")


if __name__ == "__main__":
    unittest.main()
