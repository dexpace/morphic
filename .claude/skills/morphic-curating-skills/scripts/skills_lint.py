#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Hold the skills under .claude/skills to their frontmatter, their limits and the tree they
describe.

Usage: python3.14 .claude/skills/morphic-curating-skills/scripts/skills_lint.py

Prints one "path:line: problem" per finding. Exit 0 when clean, 1 on a finding, 2 when it
cannot run. Each rule in a reference is held to the format morphic-curating-skills gives. Scripts
are Python 3.14, declared in a PEP 723 script block, with the standard library only and a
test_<name>.py beside each.
"""

import ast
import itertools
import re
import subprocess
import sys
import traceback
from pathlib import Path

MAX_BODY_WORDS = 500
MAX_REFERENCE_WORDS = 4000
MAX_RULE_WORDS = 150
MAX_DESCRIPTION_CHARS = 500
MAX_FRONTMATTER_CHARS = 1024
PYTHON_TARGET = (3, 14)
PYTHON = ".".join(map(str, PYTHON_TARGET))
# The block each script declares its interpreter in (PEP 723), which `uv run` and `pipx run` read.
REQUIRES = f'# /// script\n# requires-python = ">={PYTHON}"\n# ///\n'
RULE_KEY = re.compile(r"^`([a-z0-9]+(?:-[a-z0-9]+)*)`$")
BULLET = re.compile(r"^- \*\*([A-Za-z]+)\.\*\* \S")
RULE_BULLETS = (["Rule", "Why"], ["Rule", "Why", "Probe"])
CITATION = re.compile(r"#[0-9]+")
# A rule is cited by the file that holds it: references/<file>.md#<key> within its skill, or with
# the path from the repository root from another.
RULE_CITE = re.compile(
    r"((?:\.claude/skills/[a-z0-9-]+/)?references/[A-Za-z0-9_.-]+\.md)#([a-z0-9-]+)")
# A bare code span of lowercase words joined by hyphens, which may be a rule key written alone.
BARE_KEY = re.compile(r"`([a-z0-9]+(?:-[a-z0-9]+)+)`")

# Split so this file does not match itself.
MACHINE = re.compile(
    "/U" "sers/|/h" "ome/[a-z]|/p" "rivate/|~" "/|"
    r"\[" r"\[[a-z0-9-]+\]\]|\$HOME/\.cl" "aude|scratch" "pad"
)
SPAN = re.compile(r"`([^` ]+)`")
ANY_SPAN = re.compile(r"`[^`]*`")
PROSE_PATH = re.compile(r"[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.*-]+)+")
LINK = re.compile(r"\]\(([^)#]+)")
LINK_TARGET = re.compile(r"\]\([^)]*\)")
TEST = re.compile(r"(?:^|[^A-Za-z0-9_])(Test[A-Z][A-Za-z0-9_]*)")
SECTION = re.compile(r"(ir-design|emitter-design|architecture)(?:\.md)? ?§ ?([0-9]+(?:\.[0-9]+)*)")
KEY = re.compile(r"(parser-library|compilers/openapi)\.md#([a-z0-9-]+)")
# A skill's name, as the Agent Skills specification gives it: 1 to MAX_NAME_CHARS lowercase
# letters and digits, joined by single hyphens.
NAME = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
MAX_NAME_CHARS = 64
# The specification's frontmatter fields; any other key is a typo until a reason adds it here.
FIELDS = ("name", "description", "license", "compatibility", "metadata", "allowed-tools")
FRONT_KEY = re.compile(r"^([^\s#:][^:]*):(?: (.*))?$")
PATH_LIKE = re.compile(r"/|\.(go|md|py|yaml|json|txt)$")
TEST_DECL = re.compile(r"^func (Test[A-Za-z0-9_]*)\(", re.M)
LINE_SUFFIX = re.compile(r":[0-9]+$")
# A placeholder or template inside a command, such as <spec> or {owner}, names no path.
TEMPLATE = re.compile(r"[<>{}]")
# A file named without its directory, such as `preflight.py`; a suffix such as `_test.go` is none.
BARE_FILE = re.compile(r"[A-Za-z0-9][\w.-]*\.(go|md|py|yaml|yml|json|txt)")


def git(root: Path, *args: str) -> str:
    done = subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True,
                          text=True, errors="replace")
    return done.stdout


def load_tree(root: Path) -> tuple[set[str], set[str], set[str]]:
    """Return the first segment of every tracked path, every declared Go test name with each
    family prefix of it, as TestA_B declares TestA, and the name of every file the tree holds,
    untracked ones included."""
    top_level, tests = {".claude"}, set()
    for f in git(root, "ls-files").splitlines():
        top_level.add(f.split("/", 1)[0])
        if f.endswith("_test.go") and (root / f).is_file():
            for name in TEST_DECL.findall((root / f).read_text(errors="replace")):
                parts = name.split("_")
                tests.update("_".join(parts[:i]) for i in range(1, len(parts) + 1))
    if not tests:
        raise RuntimeError("found no declared test, so a test name cannot be checked")
    listed = git(root, "ls-files", "--cached", "--others", "--exclude-standard")
    return top_level, tests, {f.rsplit("/", 1)[-1] for f in listed.splitlines()}


def rule_blocks(text: str) -> list[tuple[int, list[str]]]:
    """Return each rule a reference holds as the line number of its heading and its lines, from
    that "## " heading to the next."""
    lines = text.split("\n")
    starts = [i for i, line in enumerate(lines) if line.startswith("## ")]
    bounds = itertools.pairwise([*starts, len(lines)])
    return [(start + 1, lines[start:end]) for start, end in bounds]


def rule_bullets(block: list[str]) -> tuple[list[list[str]], list[int]]:
    """Return a rule's bullets as [name, text] with each continuation line joined to its text, and
    the index in block of each line that belongs to no bullet."""
    bullets: list[list[str]] = []
    stray = []
    for i, text in enumerate(block[2:], 2):
        m = BULLET.match(text)
        if m:
            bullets.append([m.group(1), text])
        elif text.startswith("  ") and bullets:
            bullets[-1][1] += " " + text.strip()
        elif text.strip():
            stray.append(i)
    return bullets, stray


class Linter:
    def __init__(self, root: Path, top_level: set[str], tests: set[str], names: set[str],
                 files: list[Path]):
        self.root, self.top_level, self.tests, self.files = root, top_level, tests, files
        self.names = names
        self.findings: list[str] = []
        self.keys: dict[str, str] = {}
        self.citations: list[tuple[Path, int, str]] = []

    def report(self, path: Path, line: int, message: str) -> None:
        where = path.relative_to(self.root).as_posix()
        self.findings.append(f"{where}:{line}: {message}" if line else f"{where}: {message}")

    def run(self) -> int:
        """Check every skill, then every file under the skills for what names one machine.
        Return how many skills were read."""
        skills = sorted(d for d in (self.root / ".claude" / "skills").iterdir() if d.is_dir())
        for d in skills:
            self.check_skill(d)
        for f, line, key in self.citations:
            if key in self.keys:
                self.report(f, line, f"cites rule {key} without the file that holds it")
        for f in self.files:
            for i, line in enumerate(f.read_text(errors="replace").split("\n"), 1):
                if MACHINE.search(line):
                    self.report(f, i, "names one machine or a memory link")
        return len(skills)

    def check_skill(self, d: Path) -> None:
        skill = d / "SKILL.md"
        if not skill.is_file():
            self.report(d, 0, "no SKILL.md")
            return
        text = skill.read_text()
        self.check_frontmatter(d.name, skill, text)
        refs = sorted((d / "references").glob("*.md"))
        for ref in refs:
            words = len(ref.read_text().split())
            if words > MAX_REFERENCE_WORDS:
                self.report(ref, 0, f"{words} words, over {MAX_REFERENCE_WORDS}")
            if f"references/{ref.name}" not in text:
                self.report(ref, 0, f"not named in {skill.relative_to(self.root).as_posix()}")
            self.check_rules(ref)
        for f in [skill, *refs]:
            self.check_names(d, f)
        self.check_scripts(d, text)

    def check_frontmatter(self, name: str, skill: Path, text: str) -> None:
        if not NAME.match(name) or len(name) > MAX_NAME_CHARS:
            self.report(skill, 1, f"name {name!r} is not 1-{MAX_NAME_CHARS} lowercase letters "
                                  "and digits joined by single hyphens")
        lines = text.split("\n")
        if lines[0] != "---":
            self.report(skill, 1, "frontmatter must open with ---")
            return
        if "---" not in lines[1:]:
            self.report(skill, 1, "frontmatter never closes")
            return
        end = lines.index("---", 1)
        front = lines[1:end]
        if len("\n".join(front)) > MAX_FRONTMATTER_CHARS:
            self.report(skill, 1, f"frontmatter is over {MAX_FRONTMATTER_CHARS} characters")
        self.check_front_keys(skill, front)
        if f"name: {name}" not in front:
            self.report(skill, 1, f"frontmatter name must be {name!r}")
        desc = next((f[len("description: "):] for f in front if f.startswith("description: ")), "")
        if not desc.startswith("Use when "):
            self.report(skill, 1, 'description must start "Use when"')
        if len(desc) > MAX_DESCRIPTION_CHARS:
            self.report(skill, 1, f"description is {len(desc)} characters, "
                                  f"over {MAX_DESCRIPTION_CHARS}")
        words = len("\n".join(lines[end + 1:]).split())
        if words > MAX_BODY_WORDS:
            self.report(skill, 0, f"body is {words} words, over {MAX_BODY_WORDS}")

    def check_front_keys(self, skill: Path, front: list[str]) -> None:
        """Hold each frontmatter key to the specification's fields, once each, and each plain
        value to what a YAML parser reads: an unquoted ": " or trailing ":" does not parse, and
        " #" starts a comment that cuts the value there."""
        seen = set()
        for i, line in enumerate(front, 2):
            if not line.strip() or line.startswith((" ", "\t", "#")):
                continue  # a nested value, such as metadata's, or a comment
            m = FRONT_KEY.match(line)
            if not m:
                self.report(skill, i, "frontmatter line is no key: value")
                continue
            key, value = m.group(1), (m.group(2) or "").strip()
            if key not in FIELDS:
                self.report(skill, i, f"{key} is not a frontmatter field ({', '.join(FIELDS)})")
            if key in seen:
                self.report(skill, i, f"{key} is set twice, and YAML keeps only the last")
            seen.add(key)
            if value[:1] in ("'", '"', "|", ">"):
                continue  # quoted or a block scalar: YAML reads it whole
            if ": " in value or value.endswith(":"):
                self.report(skill, i, f"{key} holds an unquoted ':', which YAML refuses: quote it")
            if " #" in value:
                self.report(skill, i, f"{key} holds ' #', which YAML reads as a comment that cuts "
                                      "the value: quote it")

    def check_rules(self, ref: Path) -> None:
        """Hold each rule a reference holds, from its heading to the next, to the rule format."""
        for line, block in rule_blocks(ref.read_text()):
            self.check_rule(ref, line, block)

    def check_rule(self, ref: Path, line: int, block: list[str]) -> None:
        """Check one rule: its key is unique across the skills, its bullets are Rule, Why and an
        optional Probe, its Why cites a PR or issue, and it stays under the word cap."""
        key = RULE_KEY.match(block[1]) if len(block) > 1 else None
        if not key:
            self.report(ref, line, "a rule's heading must be followed by its `key`")
            return
        name = key.group(1)
        where = f"{ref.relative_to(self.root).as_posix()}:{line + 1}"
        if name in self.keys:
            self.report(ref, line + 1, f"key {name} is also used at {self.keys[name]}")
        self.keys.setdefault(name, where)
        bullets, stray = rule_bullets(block)
        for i in stray:
            self.report(ref, line + i, f"rule {name} has a line that is no bullet of it")
        names = [b[0] for b in bullets]
        if names not in RULE_BULLETS:
            self.report(ref, line, f"rule {name} has {', '.join(names) or 'no'} bullets, "
                                   "not Rule, Why and an optional Probe")
        why = next((b[1] for b in bullets if b[0] == "Why"), "")
        if why and not CITATION.search(why):
            self.report(ref, line, f"rule {name} cites no PR or issue in its Why")
        words = len(" ".join(block).split())
        if words > MAX_RULE_WORDS:
            self.report(ref, line, f"rule {name} is {words} words, over {MAX_RULE_WORDS}")

    def check_names(self, d: Path, f: Path) -> None:
        """Hold every path, link, skill, test, section, keyed entry and rule a file names to the
        tree, and record each bare span that may be a rule key, which run judges once every key
        is known."""
        for i, line in enumerate(f.read_text().split("\n"), 1):
            for tok in SPAN.findall(line):
                self.check_span(d, f, i, tok)
            if not RULE_KEY.match(line):
                self.citations += [(f, i, k) for k in BARE_KEY.findall(line)]
            for path, key in RULE_CITE.findall(line):
                held = d / path if path.startswith("references/") else self.root / path
                if not held.is_file() or f"\n`{key}`\n" not in "\n" + held.read_text():
                    self.report(f, i, f"{path} holds no rule {key}")
            for command in (s for s in ANY_SPAN.findall(line) if " " in s):
                for word in command.strip("`").split():
                    word = word.strip("'\"$();,")
                    if "/" in word and not TEMPLATE.search(word) and not self.exists(d, word):
                        self.report(f, i, f"{word} does not exist")
            for p in PROSE_PATH.findall(LINK_TARGET.sub("]", ANY_SPAN.sub("", line))):
                p = p.rstrip(".,;:)")
                if not self.exists(d, p):
                    self.report(f, i, f"{p} does not exist")
            for target in LINK.findall(line):
                if not target.startswith(("http", "mailto:")) and not (f.parent / target).exists():
                    self.report(f, i, f"link target {target} does not exist")
            for name in TEST.findall(line):
                if name not in self.tests:
                    self.report(f, i, f"no test named {name}")
            for doc, num in SECTION.findall(line):
                if not self.has(doc, r"^#{2,6} +" + re.escape(num) + r"([. ]|$)"):
                    self.report(f, i, f"docs/{doc}.md has no section {num}")
            for doc, key in KEY.findall(line):
                if not self.has(doc, "^### `" + re.escape(key) + "`"):
                    self.report(f, i, f"docs/{doc}.md has no entry {key}")

    def check_span(self, d: Path, f: Path, line: int, tok: str) -> None:
        if RULE_CITE.fullmatch(tok):
            return  # check_names holds a rule citation to its file, and reports it once
        if tok.startswith("morphic-") and "/" not in tok:
            homes = (self.root / ".claude" / "skills" / tok, self.root / "cmd" / tok)
            if not any(h.is_dir() for h in homes):
                self.report(f, line, f"no skill or command named {tok}")
        elif BARE_FILE.fullmatch(tok):
            if tok not in self.names:
                self.report(f, line, f"no file named {tok}")
        elif PATH_LIKE.search(tok) and not TEMPLATE.search(tok) and not self.exists(d, tok):
            self.report(f, line, f"{tok} does not exist")

    def exists(self, d: Path, p: str) -> bool:
        """Report whether a path a skill names exists: relative to the skill for its own
        references/ and scripts/, to the repository otherwise. A path whose first segment is
        no tracked top-level entry is not a repository path, and passes."""
        p = LINE_SUFFIX.sub("", p.removeprefix("./").split("#", 1)[0]).split("*", 1)[0]
        p = p[:-len("/...")] if p.endswith("/...") else p.rstrip("/")
        if not p:
            return True
        first = p.split("/", 1)[0]
        if first in ("references", "scripts"):
            return (d / p).exists()
        return first not in self.top_level or (self.root / p).exists()

    def has(self, doc: str, pattern: str) -> bool:
        path = self.root / "docs" / f"{doc}.md"
        return path.is_file() and re.search(pattern, path.read_text(), re.M) is not None

    def check_scripts(self, d: Path, text: str) -> None:
        """Hold each script to Python, its declared interpreter, a test beside it, and a mention
        in SKILL.md, where a reader finds it."""
        scripts = d / "scripts"
        if not scripts.is_dir():
            return
        # A script may import any skill's script, as preflight.py imports the review's.
        siblings = {p.stem for p in (self.root / ".claude" / "skills").glob("*/scripts/*.py")}
        for s in sorted(scripts.iterdir()):
            if s.name == "__pycache__":
                continue
            if s.suffix != ".py":
                self.report(s, 0, "scripts are Python files")
                continue
            if not s.name.startswith("test_") and not (scripts / f"test_{s.name}").is_file():
                self.report(s, 0, f"has no test_{s.name} beside it")
            if not s.name.startswith("test_") and REQUIRES not in s.read_text():
                self.report(s, 0, f'declares no `requires-python = ">={PYTHON}"` script block')
            if not s.name.startswith("test_") and f"scripts/{s.name}" not in text:
                skill = (d / "SKILL.md").relative_to(self.root).as_posix()
                self.report(s, 0, f"not named in {skill}")
            self.check_python(s, siblings)

    def check_python(self, s: Path, siblings: set[str]) -> None:
        try:
            tree = ast.parse(s.read_text(), str(s), feature_version=PYTHON_TARGET)
        except SyntaxError as e:
            self.report(s, e.lineno or 0, f"is not Python {PYTHON}: {e.msg}")
            return
        for node in ast.walk(tree):
            names = []
            if isinstance(node, ast.Import):
                names = [a.name for a in node.names]
            elif isinstance(node, ast.ImportFrom) and node.module and not node.level:
                names = [node.module]
            for n in names:
                top = n.split(".", 1)[0]
                if top in siblings or top in sys.stdlib_module_names:
                    continue
                self.report(s, node.lineno, f"imports {top}, outside the standard library")


def findings(root: Path) -> tuple[int, list[str]]:
    """Lint the skills of the repository at root, as other scripts do for a branch they check;
    return how many skills there are and every finding. A tree with no skills has neither."""
    if not (root / ".claude" / "skills").is_dir():
        return 0, []
    top_level, tests, names = load_tree(root)
    listed = git(root, "ls-files", "--cached", "--others", "--exclude-standard", ".claude/skills")
    files = [root / f for f in listed.splitlines() if (root / f).is_file()]
    lint = Linter(root, top_level, tests, names, files)
    return lint.run(), lint.findings


def main() -> int:
    try:
        root = Path(git(Path.cwd(), "rev-parse", "--show-toplevel").strip())
        n, found = findings(root)
    except (subprocess.CalledProcessError, RuntimeError, OSError) as e:
        print(f"skills_lint: {e}", file=sys.stderr)
        return 2
    for finding in found:
        print(finding)
    if found:
        print(f"skills_lint: {len(found)} finding(s)")
        return 1
    print(f"skills_lint: clean ({n} skills)")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
