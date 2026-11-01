"""Tests for skills_lint.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-curating-skills/scripts
"""
import contextlib
import io
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import skills_lint

HERE = Path(__file__).resolve().parent

# A script's first line and the script block it declares Python 3.14 in, written out rather than
# read from the lint.
SHEBANG = "#!/usr/bin/env python3.14\n"
HEADER = '# /// script\n# requires-python = ">=3.14"\n# ///\n'
GOOD_SKILL = """---
name: good-skill
description: Use when testing.
---

See `references/r.md`, `scripts/tool.py`, `ir/a.go`, ir/a.go, TestKnown, TestKnown_Case,
architecture §4.3, docs/parser-library.md#known, `morphic-harness` and [a link](references/r.md).
Run `go test ./ir/... $(python3.14 scripts/tool.py)` or `go run ./cmd/morphic-harness <spec>`
and `gh api repos/{owner}/{repo}`, by `references/r.md#do-the-thing` and
`.claude/skills/good-skill/references/r.md#do-the-thing`; see `references/<file>.md#<key>`.
Name `tool.py`, `a.go` and `SKILL.md` alone, and a suffix such as `_test.go`.
Hold `docs/parser-library.md#known`, `docs/ir/models.md#the-flattening`, `ir/a.go:1` and `ir/*.go` up to the entry, line or glob.
Links to [a site](https://example.com/a/b) and [mail](mailto:x@example.com) leave the tree.
"""
RULE = ("## Do the thing\n`do-the-thing`\n\n- **Rule.** Do it, as `ir/a.go` and TestKnown do.\n"
        "- **Why.** It broke\n  once (#1).\n- **Probe.** architecture §4.3.\n")
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


def fixture(root: Path) -> None:
    """Write a tree holding one clean skill and everything it names."""
    write(root, "docs/parser-library.md", "### `known`\n")
    write(root, "docs/architecture.md", "## 4. Types\n\n### 4.3 Unions\n")
    write(root, "docs/ir/models.md", "# Models\n\n```\n# not a heading\n```\n\n## The flattening\n")
    write(root, "docs/ir/README.md", "# IR\n\n## Design rules\n\n## snake_case names\n")
    write(root, "ir/a.go", "package ir\n")
    write(root, "cmd/morphic-harness/main.go", "package main\n")
    skill = ".claude/skills/good-skill/"
    write(root, skill + "SKILL.md", GOOD_SKILL)
    write(root, skill + "references/r.md", GOOD_REFERENCE)
    script(root, skill + "scripts/tool.py", HEADER + "import json\n")
    write(root, skill + "scripts/test_tool.py", "import unittest\nimport tool\n")
    write(root, skill + "scripts/__pycache__/tool.cpython-314.pyc", "")


class LintTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        # The repository sits one level down, so a test can put a file outside it.
        self.root = Path(self.dir.name) / "repo"
        # Files on disk that git ignores, which the tree a clone holds leaves out.
        self.ignored: set[str] = set()
        fixture(self.root)

    def tearDown(self):
        self.dir.cleanup()

    def lint(self, toolchain: Path | None = None):
        files = [p for p in (self.root / ".claude" / "skills").rglob("*") if p.is_file()]
        top_level = {"docs", "ir", "cmd", "internal", ".claude"}
        tests = {"TestKnown", "TestKnown_Case"}
        on_disk = {p.relative_to(self.root).as_posix() for p in self.root.rglob("*") if p.is_file()}
        lint = skills_lint.Linter(self.root, top_level, tests, on_disk - self.ignored, files,
                                  toolchain)
        return lint.run(), lint.findings

    def test_a_clean_skill_has_no_finding(self):
        self.assertEqual(self.lint(), (1, []))

    def test_a_skill_without_its_file_or_whole_frontmatter_is_reported(self):
        (self.root / ".claude" / "skills" / "no-file").mkdir()
        write(self.root, ".claude/skills/no-open/SKILL.md", "name: no-open\n")
        write(self.root, ".claude/skills/no-close/SKILL.md", "---\nname: no-close\n")
        want = [".claude/skills/no-file: no SKILL.md",
                ".claude/skills/no-open/SKILL.md:1: frontmatter must open with ---",
                ".claude/skills/no-close/SKILL.md:1: frontmatter never closes"]
        self.assertEqual(sorted(self.lint()[1]), sorted(want))

    def test_each_planted_problem_is_reported(self):
        skill = ".claude/skills/Bad_Skill/"
        front = "---\nname: other\ndescription: For tests.\n---\n\n"
        body = ("Bad `ir/nope.go`, prose ir/gone.go, TestMissing, architecture §99.1,"
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
            f"{bad}SKILL.md:7: docs/architecture.md has no section 99.1",
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
            # This skill has no r.md of its own; good-skill's holds no such rule.
            f"{bad}SKILL.md:10: references/r.md does not exist",
            f"{bad}SKILL.md:10: .claude/skills/good-skill/references/r.md holds no rule gone",
            f"{bad}SKILL.md:10: no file named gone.py",
        ]
        # The line a too-new construct is reported at depends on the interpreter's parser.
        version = [g for g in got if " is not Python 3.14: " in g]
        self.assertEqual(len(version), 1, "\n".join(got))
        self.assertStartsWith(version[0], f"{bad}scripts/new.py:", version[0])
        self.assertEqual(sorted(g for g in got if g not in version), sorted(want), "\n".join(got))

    def test_a_span_path_is_held_whatever_its_first_segment(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        # Each span below names no repository path, by its form or by its first segment.
        elsewhere = ("`/components/schemas`, `-o=/dev/null`, `:(exclude)*/x.go`, `+(a/b)`, "
                     "`[[:alnum:]]/x`, `http://h/x`, `<owner>/<repo>`, `origin/main`, "
                     "`go/types`, `runtime/metrics`, `golang.org/x/exp`, `openapi/cyclic-ref`, "
                     "`.git/info/exclude`, `_test.go`, `./...`.\n")
        # A misspelt or renamed first segment is held in a span and in a command, never in
        # prose, where words join with a slash.
        held = "See `gone/a.go`, `./gone/b` and `go test ./gone/...`, but prose gone/c.go.\n"
        write(self.root, skill, GOOD_SKILL + elsewhere + held)
        n = GOOD_SKILL.count("\n") + 2
        want = [f"{skill}:{n}: {p} does not exist" for p in ("gone/a.go", "./gone/b", "./gone/...")]
        self.assertEqual(sorted(self.lint()[1]), sorted(want))

    def test_a_path_the_tree_lacks_may_name_the_go_toolchains_sources(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        src = Path(self.dir.name) / "goroot" / "src"
        write(src, "internal/runtime/maps/map.go", "package maps\n")
        write(src, "cmd/compile/internal/walk/range.go", "package walk\n")
        # Beside src, so only a path climbing out of it reaches the file.
        write(src.parent, "secret.txt", "x\n")
        named = ("See `internal/runtime/maps`, prose internal/runtime/maps/map.go,\n"
                 "`$GOROOT/src/internal/runtime/maps/map.go`, `cmd/compile/internal/walk/range.go`"
                 "\nand `sed -n 1p $(go env GOROOT)/src/internal/runtime/maps/map.go` in "
                 "`$GOROOT/src`,\n"
                 "quoted `cat \"$(go env GOROOT)\"/src/internal/runtime/maps/map.go` or "
                 "`ls 'internal/runtime/maps'`.\n")
        lost = ("Not `internal/runtime/gone`, `$GOROOT/src/internal/gone.go`, "
                "`internal/../../secret.txt`\nor `cat $(go env GOROOT)/src/gone.go` or "
                "`cat \"$GOROOT\"/src/lost.go`.\n")
        write(self.root, skill, GOOD_SKILL + named + lost)
        n = GOOD_SKILL.count("\n") + 1
        gone = [f"{skill}:{n + 4}: internal/runtime/gone does not exist",
                f"{skill}:{n + 4}: $GOROOT/src/internal/gone.go does not exist",
                f"{skill}:{n + 4}: internal/../../secret.txt does not exist",
                f"{skill}:{n + 5}: GOROOT/src/gone.go does not exist",
                f"{skill}:{n + 5}: GOROOT/src/lost.go does not exist"]
        self.assertEqual(sorted(self.lint(src)[1]), sorted(gone))
        # Where go cannot say, the toolchain's paths are held to the tree, as any other.
        held = [f"{skill}:{n}: internal/runtime/maps does not exist",
                f"{skill}:{n}: internal/runtime/maps/map.go does not exist",
                f"{skill}:{n + 1}: $GOROOT/src/internal/runtime/maps/map.go does not exist",
                f"{skill}:{n + 1}: cmd/compile/internal/walk/range.go does not exist",
                f"{skill}:{n + 2}: GOROOT/src/internal/runtime/maps/map.go does not exist",
                f"{skill}:{n + 2}: $GOROOT/src does not exist",
                f"{skill}:{n + 3}: GOROOT/src/internal/runtime/maps/map.go does not exist",
                f"{skill}:{n + 3}: internal/runtime/maps does not exist"]
        self.assertEqual(sorted(self.lint()[1]), sorted(held + gone))

    def test_a_quoted_one_word_span_is_read_as_a_commands_word_is(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        src = Path(self.dir.name) / "goroot" / "src"
        write(src, "internal/runtime/maps/map.go", "package maps\n")
        named = ("See `\"ir/a.go\"`, `'ir/a.go'`, `\"tool.py\"`, `'morphic-harness'`, `ir/a.go,`,\n"
                 "`cat ir/a.go; true`, `\"$GOROOT\"/src/internal/runtime/maps/map.go` and "
                 "`\"$GOROOT/src/internal/runtime/maps/map.go\"`.\n")
        lost = "Not `\"ir/gone.go\"`, `'ir/lost.go'`, `\"gone.py\"` or `'morphic-gone'`.\n"
        write(self.root, skill, GOOD_SKILL + named + lost)
        n = GOOD_SKILL.count("\n") + 3
        want = [f"{skill}:{n}: \"ir/gone.go\" does not exist",
                f"{skill}:{n}: 'ir/lost.go' does not exist",
                f"{skill}:{n}: no file named gone.py",
                f"{skill}:{n}: no skill or command named morphic-gone"]
        self.assertEqual(sorted(self.lint(src)[1]), sorted(want))

    def test_a_go_name_placed_in_a_file_is_held_to_its_declarations(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, "ir/b.go", "package ir\n\n// Mentioned is named, as is type Buried.\n"
                                    "func Free() {\n\tCalled()\n\tLocal := 1\n\t_ = Local\n}\n\n"
                                    "func (s *S) Method() {}\n\ntype T int\n\nconst Solo = 1\n\n"
                                    "var Plain = 1\n\nconst (\n\tGrouped = 1\n)\n\n"
                                    "func Between() {\n\tInside()\n}\n\n"
                                    "var (\n\tVarred = 1\n)\n\ntype (\n\tAliased = T\n)\n\n"
                                    "type R struct {\n\tField int\n}\n\n"
                                    "type I interface {\n\tAsked()\n}\n\n"
                                    "type G[K ~[]byte] struct {\n\tItem K\n}\n\n"
                                    "func After() {\n\tLater()\n}\n")
        named = "`Free` in `ir/b.go`, `Method` in `ir/b.go`, `T` in `ir/b.go`.\n"
        also = ("`Solo` in `ir/b.go`, `Plain` in `ir/b.go`, `Grouped` in `ir/b.go`, `a` in a "
                "sentence and `Heading` in `docs/parser-library.md`.\n"
                "`Varred` in `ir/b.go`, `Aliased` in `ir/b.go`, `Field` in `ir/b.go`, `Asked` in "
                "`ir/b.go` and `Item` in `ir/b.go`.\n")
        lost = ("`Mentioned` in `ir/b.go`, `Buried` in `ir/b.go`, `Gone` in `ir/b.go`, `Fre` in "
                "`ir/b.go` and `Lost` in `ir/gone.go`.\n"
                "`Called` in `ir/b.go`, `Local` in `ir/b.go`, `Inside` in `ir/b.go`, `Later` in "
                "`ir/b.go` and `Fiel` in `ir/b.go`.\n")
        write(self.root, skill, GOOD_SKILL + named + also + lost)
        n = GOOD_SKILL.count("\n") + 4
        want = [f"{skill}:{n}: ir/b.go declares no Mentioned",
                f"{skill}:{n}: ir/b.go declares no Buried",
                f"{skill}:{n}: ir/b.go declares no Gone",
                f"{skill}:{n}: ir/b.go declares no Fre",
                f"{skill}:{n}: ir/gone.go does not exist",
                f"{skill}:{n + 1}: ir/b.go declares no Called",
                f"{skill}:{n + 1}: ir/b.go declares no Local",
                f"{skill}:{n + 1}: ir/b.go declares no Inside",
                f"{skill}:{n + 1}: ir/b.go declares no Later",
                f"{skill}:{n + 1}: ir/b.go declares no Fiel"]
        self.assertEqual(sorted(self.lint()[1]), sorted(want))

    def test_a_path_git_ignores_is_held_as_no_clone_has_it(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        local = ("# L\n\nRules kept on one machine.\n\n## Local\n`local-rule`\n\n"
                 "- **Rule.** x.\n- **Why.** y (#1).\n")
        for rel, text in (("docs/plan.md", "a plan\n"),
                          ("cmd/morphic-local/main.go", "package main\n"),
                          (".claude/skills/good-skill/references/local.md", local)):
            write(self.root, rel, text)
            self.ignored.add(rel)
        # A file outside the repository is in no clone either, whatever is on disk.
        write(Path(self.dir.name), "outside.md", "x\n")
        write(self.root, skill, GOOD_SKILL
              + "Ignored: `docs/plan.md`, `go run ./cmd/morphic-local`, prose docs/plan.md, "
                "[the plan](../../../docs/plan.md), `morphic-local`, "
                "`references/local.md#local-rule`,\nand outside: [x](../../../../outside.md), "
                "but `ir/./a.go` is in the tree.\n")
        n = GOOD_SKILL.count("\n") + 1
        lost = "is ignored by git, so no clone holds it"
        want = [f"{skill}:{n}: docs/plan.md {lost}", f"{skill}:{n}: ./cmd/morphic-local {lost}",
                f"{skill}:{n}: docs/plan.md {lost}",
                f"{skill}:{n}: link target ../../../docs/plan.md {lost}",
                f"{skill}:{n}: no skill or command named morphic-local",
                f"{skill}:{n}: references/local.md {lost}",
                f"{skill}:{n + 1}: link target ../../../../outside.md does not exist"]
        self.assertEqual(sorted(self.lint()[1]), sorted(want))

    def test_a_code_span_broken_across_lines_is_reported_once(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        # Each fence holds an odd backtick: tildes around backticks, an indented fence, a longer
        # fence around a shorter one, and a block around a line that, with an info string after
        # its run, cannot close it.
        fences = ("```markdown\n`odd\n```\n\n~~~\n```\n`odd\n~~~\n\n   ```\n`odd\n   ```\n\n"
                  "````\n```\n`odd\n````\n\n```\n```text\n`odd\n```\n\n")
        # A backtick run with a backtick in its info string opens no block, so the span after it
        # is read.
        text = (GOOD_SKILL + "Run `go vet\n--all` there.\n\n" + fences
                + "```a`b\nRun `go vet\n--all` too.\n")
        write(self.root, skill, text)
        lines = [GOOD_SKILL.count("\n") + 1, text.count("\n") - 1]
        want = [f"{skill}:{n}: a code span breaks onto the next line" for n in lines]
        self.assertEqual(self.lint()[1], want)

    def test_a_section_wrapped_onto_the_next_line_is_checked_once(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, skill, GOOD_SKILL + "See architecture\n"
              "§99.1 and architecture §97.1, then architecture §\n98.2, but architecture §4.3 holds.\n")
        n = GOOD_SKILL.count("\n") + 1
        want = [f"{skill}:{n}: docs/architecture.md has no section 99.1",
                f"{skill}:{n + 1}: docs/architecture.md has no section 97.1",
                f"{skill}:{n + 1}: docs/architecture.md has no section 98.2"]
        self.assertEqual(self.lint()[1], want)

    def test_a_section_naming_no_document_is_reported(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, skill, GOOD_SKILL + "See architecture §4.3 and §4.3, RFC 3986 §5.2, or\n"
              "architecture.md\n§4.3.\n")
        n = GOOD_SKILL.count("\n") + 1
        want = f"{skill}:{n}: a § names no document: name it first, as in architecture §N"
        self.assertEqual(self.lint()[1], [want])

    def test_the_ir_design_is_cited_by_heading_and_not_by_number(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, skill, GOOD_SKILL + "See `docs/ir/models.md#the-flattening` and\n"
              "`docs/ir/models.md#gone`, `docs/ir/models.md#not-a-heading`,\n"
              "`docs/ir/gone.md#x`, and ir-design §4.3.\n")
        n = GOOD_SKILL.count("\n") + 1
        want = [f"{skill}:{n + 1}: docs/ir/models.md has no heading gone",
                f"{skill}:{n + 1}: docs/ir/models.md has no heading not-a-heading",
                f"{skill}:{n + 2}: a § names no document: name it first, as in architecture §N",
                f"{skill}:{n + 2}: names a retired page: cite a document of docs/ir or docs/emitters",
                f"{skill}:{n + 2}: docs/ir/gone.md#x does not exist",
                f"{skill}:{n + 2}: docs/ir/gone.md has no heading x"]
        self.assertEqual(sorted(self.lint()[1]), sorted(want))

    def test_a_design_citation_is_read_in_the_root_and_past_an_underscore(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        write(self.root, skill, GOOD_SKILL + "See `docs/ir/README.md#design-rules`,\n"
              "`docs/ir/README.md#snake_case-names`, and\n"
              "`docs/ir/README.md#gone`.\n")
        n = GOOD_SKILL.count("\n") + 1
        self.assertEqual(self.lint()[1], [f"{skill}:{n + 2}: docs/ir/README.md has no heading gone"])

    def test_a_scalar_is_read_as_yaml_reads_it(self):
        cases = {"Use when: plain": "Use when: plain", "'it''s'": "it's",
                 '"a \\"b\\" \\\\c"': 'a "b" \\c', "''": "", "": "",
                 '"x" # a comment': "x", "'x'\t# a comment": "x",
                 ">-": None, "|": None, "'open": None, '"open': None, '"ends in \\"': None,
                 "'it''": None, '"x" b': None, '"x"#c': None}
        for value, want in cases.items():
            with self.subTest(value=value):
                self.assertEqual(skills_lint.scalar(value), want)

    def test_a_quoted_value_is_read_as_yaml_reads_it_and_a_block_scalar_is_refused(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        body = GOOD_SKILL.split("---\n", 2)[2]
        spans = f"{skill}:1: description is not a one-line plain or quoted value; write it as one"
        cases = [
            ('name: "good-skill"\ndescription: "Use when quoted: \\"as told\\"."\n', []),
            ("name: good-skill\ndescription: 'Use when it''s quoted: once.'\n", []),
            ("name: good-skill\ndescription: >-\n  Use when folded.\n", [spans]),
            ('name: good-skill\ndescription: "Use when\n  never closed."\n', [spans]),
        ]
        for front, want in cases:
            with self.subTest(front=front):
                write(self.root, skill, f"---\n{front}---\n{body}")
                self.assertEqual(self.lint()[1], want)
        # A value the lint does not read leaves the body's limit checked.
        long = "word " * 501 + body
        write(self.root, skill, f"---\n{cases[2][0]}---\n{long}")
        words = len(long.split())
        self.assertEqual(self.lint()[1], [spans, f"{skill}: body is {words} words, over 500"])

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

    def test_a_description_is_held_to_500_characters(self):
        skill = ".claude/skills/good-skill/SKILL.md"
        body = GOOD_SKILL.split("---\n", 2)[2]
        over = f"{skill}:1: description is 501 characters, over 500"
        for size, want in ((500, []), (501, [over])):
            with self.subTest(size=size):
                description = "Use when " + "x" * (size - len("Use when "))
                front = f"---\nname: good-skill\ndescription: {description}\n---\n"
                write(self.root, skill, front + body)
                self.assertEqual(self.lint()[1], want)

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


class ArgumentsTest(unittest.TestCase):
    def test_an_argument_is_refused_before_anything_is_linted(self):
        for argv in (["--strict"], [".claude/skills/one-skill"]):
            with self.subTest(argv=argv):
                with mock.patch.object(skills_lint, "findings") as lint, \
                        contextlib.redirect_stderr(io.StringIO()), \
                        self.assertRaises(SystemExit) as e:
                    skills_lint.main(argv)
                self.assertEqual(e.exception.code, 2)
                lint.assert_not_called()

    def test_help_prints_the_usage_and_lints_nothing(self):
        out = io.StringIO()
        with mock.patch.object(skills_lint, "findings") as lint, \
                contextlib.redirect_stdout(out), self.assertRaises(SystemExit) as e:
            skills_lint.main(["--help"])
        self.assertEqual(e.exception.code, 0)
        self.assertIn("Hold the skills under .claude/skills", out.getvalue())
        lint.assert_not_called()


class LoadTreeTest(unittest.TestCase):
    def test_tests_top_level_entries_and_every_file_path_are_read(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
                   "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"}
            git = ["git", "-C", d]
            subprocess.run(git + ["init", "-q"], check=True, env=env)
            write(root, "ir/a_test.go", "package ir\n\nfunc TestA_B(t *testing.T) {}\n")
            write(root, "ir/gone.go", "package ir\n")
            write(root, "ir/gone_test.go", "package ir\n\nfunc TestGone(t *testing.T) {}\n")
            # Names git would quote and escape, as it does one holding a non-ASCII letter.
            write(root, "données/b_test.go", "package d\n\nfunc TestB(t *testing.T) {}\n")
            write(root, ".gitignore", "out/\n")
            subprocess.run(git + ["add", "-A"], check=True, env=env)
            subprocess.run(git + ["commit", "-qm", "x"], check=True, env=env)
            write(root, "scripts/new.py", "")
            write(root, "notes/café.md", "")
            write(root, "out/built.txt", "")
            # A test written beside the skill naming it, which is not staged yet, and a built one.
            write(root, "ir/new_test.go", "package ir\n\nfunc TestNew_Case(t *testing.T) {}\n")
            write(root, "out/built_test.go", "package o\n\nfunc TestBuilt(t *testing.T) {}\n")
            # A tracked test file deleted mid-change, as before a git rm, declares nothing.
            for gone in ("gone.go", "gone_test.go"):
                (root / "ir" / gone).unlink()
            top_level, tests, known = skills_lint.load_tree(root)
        self.assertEqual(top_level, {".claude", ".gitignore", "ir", "données"})
        self.assertEqual(tests, {"TestA", "TestA_B", "TestB", "TestNew", "TestNew_Case"})
        self.assertEqual(known, {".gitignore", "ir/a_test.go", "données/b_test.go", "ir/new_test.go",
                                 "scripts/new.py", "notes/café.md"},
                         "untracked files count, and ignored or deleted ones do not")


class GoSourcesTest(unittest.TestCase):
    def go_env(self, stdout: str = "", fail: Exception | None = None) -> Path | None:
        root = Path("module")

        def run(*args, **kwargs):
            # Asked elsewhere, go may name the toolchain of another module, or of none.
            self.assertEqual(kwargs.get("cwd"), root, "go is asked at the root")
            # A failing go is trusted for nothing it printed, and a GOROOT that is not UTF-8 still
            # decodes rather than crashing the run.
            self.assertTrue(kwargs.get("check"), "a failing go raises")
            self.assertEqual(kwargs.get("errors"), "replace", "undecodable bytes are replaced")
            if fail:
                raise fail
            return subprocess.CompletedProcess(args, 0, stdout=stdout)
        with mock.patch.object(skills_lint.subprocess, "run", run):
            return skills_lint.go_sources(root)

    def test_the_toolchain_src_is_found_or_none_is_trusted(self):
        with tempfile.TemporaryDirectory() as d, contextlib.chdir(d):
            # A src below the cwd, which a relative or empty answer would otherwise reach.
            (Path(d) / "src").mkdir()
            (Path(d) / "goroot" / "src").mkdir(parents=True)
            self.assertEqual(self.go_env(d + "/goroot\n"), Path(d) / "goroot" / "src")
            self.assertIsNone(self.go_env(d + "/absent\n"), "a GOROOT with no src")
            self.assertIsNone(self.go_env("\n"), "an empty answer")
            self.assertIsNone(self.go_env("goroot\n"), "a relative answer")
        self.assertIsNone(self.go_env(fail=FileNotFoundError("go")), "no go on the PATH")
        self.assertIsNone(self.go_env(fail=subprocess.CalledProcessError(1, "go")), "go failing")

    def test_a_run_lints_with_the_toolchain_go_names_at_the_root(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d) / "repo"
            fixture(root)
            write(root, "ir/a_test.go", "package ir\n\nfunc TestKnown_Case(t *testing.T) {}\n")
            skill = ".claude/skills/good-skill/SKILL.md"
            write(root, skill, GOOD_SKILL + "Read `internal/runtime/maps`.\n")
            subprocess.run(["git", "-C", str(root), "init", "-q"], check=True)
            subprocess.run(["git", "-C", str(root), "add", "-A"], check=True)
            src = Path(d) / "goroot" / "src"
            write(src, "internal/runtime/maps/map.go", "package maps\n")
            asked = []
            with mock.patch.object(skills_lint, "go_sources", lambda at: asked.append(at) or src):
                self.assertEqual(skills_lint.findings(root), (1, []))
            self.assertEqual(asked, [root], "go is asked once, at the root")
            n = GOOD_SKILL.count("\n") + 1
            with mock.patch.object(skills_lint, "go_sources", lambda at: None):
                self.assertEqual(skills_lint.findings(root),
                                 (1, [f"{skill}:{n}: internal/runtime/maps does not exist"]))


class MainTest(unittest.TestCase):
    def run_lint(self, cwd: Path) -> tuple[int, str]:
        out = io.StringIO()
        with contextlib.chdir(cwd), contextlib.redirect_stdout(out), \
                contextlib.redirect_stderr(out):
            return skills_lint.main([]), out.getvalue()

    def test_each_outcome_has_its_exit_status(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d) / "repo"
            fixture(root)
            subprocess.run(["git", "-C", str(root), "init", "-q"], check=True)
            subprocess.run(["git", "-C", str(root), "add", "-A"], check=True)
            self.assertEqual(self.run_lint(root)[0], 2, "a tree declaring no test")
            write(root, "ir/a_test.go", "package ir\n\nfunc TestKnown_Case(t *testing.T) {}\n")
            subprocess.run(["git", "-C", str(root), "add", "-A"], check=True)
            code, out = self.run_lint(root)
            self.assertEqual((code, out), (0, "skills_lint: clean (1 skills)\n"))
            # Seen only in the skill files the lint lists from git, not by any rule check.
            skill = root / ".claude/skills/good-skill/SKILL.md"
            skill.write_text(skill.read_text() + "Kept at /U" "sers/someone.\n")
            code, out = self.run_lint(root)
            self.assertEqual(code, 1, out)
            self.assertIn("names one machine or a memory link", out)
            outside = Path(d) / "outside"
            outside.mkdir()
            self.assertEqual(self.run_lint(outside)[0], 2, "a directory outside any repository")

    def test_a_tree_with_no_skill_is_no_pass(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            subprocess.run(["git", "-C", str(root), "init", "-q"], check=True)
            skills = root / ".claude/skills"
            for state in ("no skills directory", "an empty one", "one holding only a file"):
                with self.subTest(state=state):
                    code, out = self.run_lint(root)
                    self.assertEqual(code, 2, out)
                    self.assertIn("found no skill", out)
                    self.assertNotIn("clean", out)
                skills.mkdir(parents=True, exist_ok=True)
                if state == "an empty one":
                    (skills / "README.md").write_text("x\n")


class UnexpectedErrorTest(unittest.TestCase):
    def test_an_unexpected_error_exits_2_never_a_verdict(self):
        # Every command the script runs fails in a way no handler of its own expects.
        crash = ("import runpy, subprocess, sys\n"
                 "def broken(*args, **kwargs): raise ZeroDivisionError('planted')\n"
                 "subprocess.run = broken\n"
                 f"sys.argv = [{str(HERE / 'skills_lint.py')!r}]\n"
                 "runpy.run_path(sys.argv[0], run_name='__main__')\n")
        run = subprocess.run([sys.executable, "-c", crash], capture_output=True, text=True,
                             env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertIn("ZeroDivisionError: planted", run.stderr)


if __name__ == "__main__":
    unittest.main()
