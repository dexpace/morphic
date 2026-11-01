#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Hold the skills under .claude/skills to their frontmatter, their limits and the tree they
describe.

Usage: python3.14 .claude/skills/morphic-curating-skills/scripts/skills_lint.py

Prints one "path:line: problem" per finding, the line left out for one about a whole file, then a
summary line. Exit 0 when clean, 1 on a finding, 2 when it cannot run or finds no skill to check.
Each rule in a reference is held to the format morphic-curating-skills gives. Scripts are executable
Python 3.14, declared in their first line and a PEP 723 script block, with the standard library only
and a test_<name>.py beside each. A path the tree lacks may name the Go toolchain's sources, which
`go env GOROOT` locates; where go cannot say, every path is held to the tree. A Go name spelled
`Name` in `path/to/file.go` on one line is held to what that file declares.
"""

import argparse
import ast
import itertools
import os
import re
import subprocess
import sys
import traceback
from pathlib import Path

MAX_BODY_WORDS = 500
MAX_REFERENCE_WORDS = 4000
MAX_RULE_WORDS = 150
# Every session loads every description, so the curating skill holds one to half the
# specification's 1,024 characters.
MAX_DESCRIPTION_CHARS = 500
PYTHON_TARGET = (3, 14)
PYTHON = ".".join(map(str, PYTHON_TARGET))
# The block each script declares its interpreter in (PEP 723), which `uv run` and `pipx run` read.
REQUIRES = f'# /// script\n# requires-python = ">={PYTHON}"\n# ///\n'
# The first line, through which `./script.py` runs under the interpreter the block declares.
SHEBANG = f"#!/usr/bin/env python{PYTHON}\n"
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
# A Go name placed in a file: `Name` in `path/to/file.go`.
NAME_IN_FILE = re.compile(r"`([A-Za-z_]\w*)` in `([^`\s]+\.go)`")
# A const, var or type group, or a struct or interface type, up to what closes it, which gofmt
# puts first on its line and indents nothing it nests to match.
GO_BLOCK = re.compile(r"^(?:(?:const|var|type) \(|type [^\n]* (?:struct|interface) \{)\n"
                      r"(.*?)^[)}]$", re.M | re.S)
SECTION = re.compile(r"(architecture)(?:\.md)? ?§ ?([0-9]+(?:\.[0-9]+)*)")
# A § after the name of a document it can be in, a line break between them allowed: one of the
# design documents SECTION holds to the tree, or an RFC. Any other names no document.
NAMED_SECTION = re.compile(r"(?:architecture(?:\.md)?|RFC \d+)\s?§")
KEY = re.compile(r"(parser-library|compilers/openapi)\.md#([a-z0-9-]+)")
# A section of a design tree, cited as GitHub anchors its heading.
DESIGN_SECTION = re.compile(r"docs/((?:ir|emitters)/[A-Za-z0-9_/-]+\.md)#([A-Za-z0-9_-]+)")
# A design tree's name from when it was a single page, which names nothing now.
RETIRED = re.compile(r"(?:ir|emitter)-design")
HEADING = re.compile(r"#{1,6} +(.+)")
# A fenced block's opening or closing run, as CommonMark reads one: three or more backticks or
# tildes, indented by at most three spaces, then the info string.
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})(.*)$")
# A skill's name, as the Agent Skills specification gives it: 1 to MAX_NAME_CHARS lowercase
# letters and digits, joined by single hyphens.
NAME = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
MAX_NAME_CHARS = 64
# The specification's frontmatter fields; any other key is a typo until a reason adds it here.
FIELDS = ("name", "description", "license", "compatibility", "metadata", "allowed-tools")
FRONT_KEY = re.compile(r"^([^\s#:][^:]*):(?: (.*))?$")
# A one-line quoted value, and the comment YAML lets follow it after a space: a double-quoted one
# closes at the first quote no backslash escapes, a single-quoted one at the first quote not
# doubled.
DOUBLE_QUOTED = re.compile(r'"((?:[^"\\]|\\.)*)"(?:[ \t]+#.*)?')
SINGLE_QUOTED = re.compile(r"'((?:[^']|'')*)'(?:[ \t]+#.*)?")
TEST_DECL = re.compile(r"^func (Test[A-Za-z0-9_]*)\(", re.M)
LINE_SUFFIX = re.compile(r":[0-9]+$")
# A code span holding a "/" names a repository path unless its form marks it as something else:
# a JSON pointer or device (/), a flag (-), a pathspec's magic (:(), a pattern (+ or [), a URL
# (://), or a placeholder (<spec>, {owner}).
NOT_A_PATH = re.compile(r"^(?:/|-|:\(|\+)|://|[\[<>{}]")
# The first segments of what a span names outside the tree. Any other first segment is held to
# the tree, then to the Go toolchain's sources, so a misspelt or renamed directory is reported
# rather than passed.
ELSEWHERE = {
    ".git": "git's own files, which no listing holds and a worktree keeps elsewhere",
    "origin": "a remote's branch",
    "go": "a Go package",
    "golang.org": "a Go module",
    "runtime": "a Go package",
    "openapi": "a diagnostic code",
}
# A file named without its directory, such as `sweep.py`; a suffix such as `_test.go` is none.
BARE_FILE = re.compile(r"[A-Za-z0-9][\w.-]*\.(go|md|py|yaml|yml|json|txt)")
# A path below the toolchain's src, as path_word leaves one, with GO_ENV_GOROOT folded into $GOROOT
# before it reads a command's words.
GOROOT_SRC = re.compile(r"^GOROOT/src(?:/|$)")
GO_ENV_GOROOT = "$(go env GOROOT)"


def git(root: Path, *args: str) -> str:
    done = subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True,
                          text=True, errors="replace")
    return done.stdout


def listing(root: Path, *args: str) -> list[str]:
    """Return the paths git ls-files lists, each as it is on disk: without -z, git quotes and
    escapes a name holding a non-ASCII letter."""
    return [f for f in git(root, "ls-files", "-z", *args).split("\0") if f]


def load_tree(root: Path) -> tuple[set[str], set[str], set[str]]:
    """Return the first segment of every tracked path, every declared Go test name with each
    family prefix of it, as TestA_B declares TestA, and the path of every file the tree holds:
    each one git lists, untracked ones included and ignored ones not, that is on disk."""
    top_level, tests = {".claude"}, set()
    for f in listing(root):
        top_level.add(f.split("/", 1)[0])
        if f.endswith("_test.go") and (root / f).is_file():
            for name in TEST_DECL.findall((root / f).read_text(errors="replace")):
                parts = name.split("_")
                tests.update("_".join(parts[:i]) for i in range(1, len(parts) + 1))
    if not tests:
        raise RuntimeError("found no declared test, so a test name cannot be checked")
    listed = listing(root, "--cached", "--others", "--exclude-standard")
    return top_level, tests, {f for f in listed if (root / f).is_file()}


def go_sources(root: Path) -> Path | None:
    """Return the src directory of the Go toolchain that builds the module at root, or None where
    go cannot say, when every path is held to the tree alone."""
    try:
        done = subprocess.run(["go", "env", "GOROOT"], cwd=root, check=True, capture_output=True,
                              text=True, errors="replace")
    except (OSError, subprocess.CalledProcessError):
        return None
    # An empty answer reads as ".", so a relative one is refused before it can name the cwd.
    src = Path(done.stdout.strip()) / "src"
    return src if src.is_absolute() and src.is_dir() else None


def path_word(word: str) -> str:
    """Return what a word of a code span spells, for the checks a one-word span and a command's
    words share. A double quote, which leaves $GOROOT to expand, is no part of a path wherever it
    stands, so "$(go env GOROOT)"/src/... names the toolchain's src; a single quote and a shell's
    punctuation around the word are none either."""
    return word.replace('"', "").strip("'$();,")


def declares(source: str, name: str) -> bool:
    """Report whether Go source declares name: a function or method, a type, a constant or a
    variable, or a member of a group, struct or interface. A tab opens a statement in a function
    too, so a member is looked for inside its block, or a name a file only calls would pass."""
    n = re.escape(name)
    if re.search(rf"^(?:func (?:\([^)]*\) )?|type |const |var ){n}\b", source, re.M):
        return True
    return any(re.search(rf"^\t{n}\b", body, re.M) for body in GO_BLOCK.findall(source))


def names_a_path(tok: str) -> bool:
    """Report whether a span's token names a repository path: it holds a "/", and neither its
    form nor its first segment marks it as something else."""
    if "/" not in tok or NOT_A_PATH.search(tok):
        return False
    return tok.removeprefix("./").split("/", 1)[0] not in ELSEWHERE


def scalar(value: str) -> str | None:
    """Return what YAML reads from a one-line plain or quoted value, a comment after the quote
    allowed, or None for any other: a block scalar, which this lint does not read, or a quote that
    does not close on its line or has text after it, which YAML refuses."""
    if m := SINGLE_QUOTED.fullmatch(value):
        return m.group(1).replace("''", "'")
    if m := DOUBLE_QUOTED.fullmatch(value):
        return re.sub(r'\\(["\\])', r"\1", m.group(1))
    return None if value[:1] in ("'", '"', "|", ">") else value


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
    def __init__(self, root: Path, top_level: set[str], tests: set[str], known: set[str],
                 files: list[Path], toolchain: Path | None = None):
        self.root, self.top_level, self.tests, self.files = root, top_level, tests, files
        # The Go toolchain's src, which a path the tree lacks may name, or None to hold every
        # path to the tree.
        self.toolchain = toolchain
        # The tree a clone holds, as load_tree lists it, with each directory above a file: a path
        # on disk that git ignores, such as a local plan or a built binary, is in no clone.
        self.names = {f.rsplit("/", 1)[-1] for f in known}
        self.paths = set(known)
        for f in known:
            parts = f.split("/")
            self.paths.update("/".join(parts[:i]) for i in range(1, len(parts)))
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
            self.check_split_spans(f)
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
        self.check_front_keys(skill, front)
        values = {m.group(1): scalar((m.group(2) or "").strip())
                  for m in map(FRONT_KEY.match, front) if m}
        for key in ("name", "description"):
            if key in values and values[key] is None:
                self.report(skill, 1, f"{key} is not a one-line plain or quoted value; write it "
                                      "as one")
        if values.get("name", "") not in (name, None):
            self.report(skill, 1, f"frontmatter name must be {name!r}")
        desc = values.get("description", "")
        if desc is not None and not desc.startswith("Use when "):
            self.report(skill, 1, 'description must start "Use when"')
        if desc is not None and len(desc) > MAX_DESCRIPTION_CHARS:
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

    def check_split_spans(self, f: Path) -> None:
        """Report a code span a line break splits, once, at its first line: the checks read one
        line at a time, so a split span escapes them, and later backticks on its line pair
        wrongly."""
        fence, inside = "", False
        for i, line in enumerate(f.read_text().split("\n"), 1):
            run, info = m.groups() if (m := FENCE.match(line)) else ("", "")
            if not fence and run and not (run[0] == "`" and "`" in info):
                fence = run  # a backtick run with a backtick after it opens no block
                continue
            if fence and run[:1] == fence[0] and len(run) >= len(fence) and not info.strip():
                fence = ""  # only a bare run of the opening character, as long or longer, closes it
                continue
            if not fence and line.count("`") % 2:
                if not inside:
                    self.report(f, i, "a code span breaks onto the next line")
                inside = not inside

    def check_names(self, d: Path, f: Path) -> None:
        """Hold every path, link, skill, test, Go name, section, keyed entry and rule a file names
        to the tree, and record each bare span that may be a rule key, which run judges once every
        key is known."""
        text = f.read_text()
        named = {m.end() - 1 for m in NAMED_SECTION.finditer(text)}
        for m in re.finditer("§", text):
            if m.start() not in named:
                line = text.count("\n", 0, m.start()) + 1
                self.report(f, line, "a § names no document: name it first, as in architecture §N")
        lines = text.split("\n")
        for i, line in enumerate(lines, 1):
            for tok in SPAN.findall(line):
                self.check_span(d, f, i, tok)
            if not RULE_KEY.match(line):
                self.citations += [(f, i, k) for k in BARE_KEY.findall(line)]
            for path, key in RULE_CITE.findall(line):
                held = d / path if path.startswith("references/") else self.root / path
                why = self.absent(path, held.relative_to(self.root).as_posix())
                if why or f"\n`{key}`\n" not in "\n" + held.read_text():
                    self.report(f, i, why or f"{path} holds no rule {key}")
            for command in (s for s in ANY_SPAN.findall(line) if " " in s):
                for word in command.strip("`").replace(GO_ENV_GOROOT, "$GOROOT").split():
                    word = path_word(word)
                    if names_a_path(word) and (why := self.missing(d, word)):
                        self.report(f, i, why)
            # Prose joins words with a slash (I/O, kept/dropped), so a path there is held only
            # when its first segment is a tracked entry; one in a span is held whatever it starts
            # with.
            for p in PROSE_PATH.findall(LINK_TARGET.sub("]", ANY_SPAN.sub("", line))):
                p = p.rstrip(".,;:)")
                if self.first_is_tracked(p) and (why := self.missing(d, p)):
                    self.report(f, i, why)
            for target in LINK.findall(line):
                if target.startswith(("http", "mailto:")):
                    continue
                if why := self.absent(target, os.path.relpath(f.parent / target, self.root)):
                    self.report(f, i, f"link target {why}")
            for name in TEST.findall(line):
                if name not in self.tests:
                    self.report(f, i, f"no test named {name}")
            for name, path in NAME_IN_FILE.findall(line):
                held = self.root / path
                if held.is_file() and not declares(held.read_text(errors="replace"), name):
                    self.report(f, i, f"{path} declares no {name}")
            # A section citation wraps at its spaces, so read on into the next line.
            joined = line + " " + (lines[i].lstrip() if i < len(lines) else "")
            for m in SECTION.finditer(joined):
                if m.start() >= len(line):
                    break  # it starts on the next line, which reports it
                doc, num = m.groups()
                if not self.has(doc, r"^#{2,6} +" + re.escape(num) + r"([. ]|$)"):
                    self.report(f, i, f"docs/{doc}.md has no section {num}")
            for doc, key in KEY.findall(line):
                if not self.has(doc, "^### `" + re.escape(key) + "`"):
                    self.report(f, i, f"docs/{doc}.md has no entry {key}")
            for doc, anchor in DESIGN_SECTION.findall(line):
                if anchor not in self.design_anchors(doc):
                    self.report(f, i, f"docs/{doc} has no heading {anchor}")
            if RETIRED.search(line):
                self.report(f, i, "names a retired page: cite a document of docs/ir or docs/emitters")

    def check_span(self, d: Path, f: Path, line: int, tok: str) -> None:
        shown, tok = tok, path_word(tok)
        if RULE_CITE.fullmatch(tok):
            return  # check_names holds a rule citation to its file, and reports it once
        if tok.startswith("morphic-") and "/" not in tok:
            if not {f".claude/skills/{tok}", f"cmd/{tok}"} & self.paths:
                self.report(f, line, f"no skill or command named {tok}")
        elif BARE_FILE.fullmatch(tok):
            if tok not in self.names:
                self.report(f, line, f"no file named {tok}")
        elif names_a_path(tok) and (why := self.missing(d, tok, shown)):
            self.report(f, line, why)

    def first_is_tracked(self, p: str) -> bool:
        first = p.removeprefix("./").split("/", 1)[0]
        return first in self.top_level or first in ("references", "scripts")

    def missing(self, d: Path, p: str, shown: str = "") -> str | None:
        """Say why a path a skill names is not in the tree, or None when it is: relative to the
        skill for its own references/ and scripts/, to the repository otherwise. A path is held
        up to a #entry, a :line or its first wildcard, and ./... names the module's packages. One
        the tree lacks is None too when it names the Go toolchain's sources. The reason names
        shown, the path as a span wrote it, where p has been read out of that spelling."""
        rel = LINE_SUFFIX.sub("", p.removeprefix("./").split("#", 1)[0]).split("*", 1)[0]
        rel = "" if rel == "..." else rel.removesuffix("/...").rstrip("/")
        if not rel:
            return None
        if rel.split("/", 1)[0] in ("references", "scripts"):
            rel = (d / rel).relative_to(self.root).as_posix()
        rel = os.path.normpath(rel)
        why = self.absent(shown or p, rel)
        return None if why and self.in_toolchain(rel) else why

    def in_toolchain(self, rel: str) -> bool:
        """Report whether rel names a file or directory of the Go toolchain's sources, by its
        import path or its path below $GOROOT/src, never one beside or above them. A path gone
        from the tree therefore passes where the toolchain holds the same one, such as cmd/vet."""
        if self.toolchain is None:
            return False
        rel = GOROOT_SRC.sub("", rel)
        return ".." not in rel.split("/") and (self.toolchain / rel).exists()

    def absent(self, shown: str, rel: str) -> str | None:
        """Say why rel, a path from the repository root, is not in the tree a clone holds, naming
        it as shown, or None when it is."""
        if rel in self.paths:
            return None
        if rel.split("/", 1)[0] != ".." and (self.root / rel).exists():
            return f"{shown} is ignored by git, so no clone holds it"
        return f"{shown} does not exist"

    def design_anchors(self, doc: str) -> set[str]:
        """Return the anchors GitHub gives the headings of a design document, outside its code
        fences, as internal/archtest reads them too."""
        path = self.root / "docs" / doc
        found, fenced = set(), False
        for line in path.read_text().splitlines() if path.is_file() else ():
            fenced ^= line.startswith("```")
            heading = HEADING.fullmatch(line)
            if heading and not fenced:
                found.add(re.sub(r"[^\w\- ]", "", heading[1].lower()).replace(" ", "-"))
        return found

    def has(self, doc: str, pattern: str) -> bool:
        path = self.root / "docs" / f"{doc}.md"
        return path.is_file() and re.search(pattern, path.read_text(), re.M) is not None

    def check_scripts(self, d: Path, text: str) -> None:
        """Hold each script to Python, and each one other than a test to check_tool."""
        scripts = d / "scripts"
        if not scripts.is_dir():
            return
        # A script may import any skill's script, as each test imports the script it tests.
        siblings = {p.stem for p in (self.root / ".claude" / "skills").glob("*/scripts/*.py")}
        for s in sorted(scripts.iterdir()):
            if s.name == "__pycache__":
                continue
            if s.suffix != ".py":
                self.report(s, 0, "scripts are Python files")
                continue
            if not s.name.startswith("test_"):
                self.check_tool(d, s, text)
            self.check_python(s, siblings)

    def check_tool(self, d: Path, s: Path, text: str) -> None:
        """Hold a script to a test beside it, its interpreter declared in its first line and its
        script block, an executable mode, and a mention in SKILL.md, where a reader finds it."""
        source = s.read_text()
        if not (s.parent / f"test_{s.name}").is_file():
            self.report(s, 0, f"has no test_{s.name} beside it")
        if REQUIRES not in source:
            self.report(s, 0, f'declares no `requires-python = ">={PYTHON}"` script block')
        if not source.startswith(SHEBANG):
            self.report(s, 0, f"has no `{SHEBANG.strip()}` first line")
        if not os.access(s, os.X_OK):
            self.report(s, 0, "is not executable")
        if f"scripts/{s.name}" not in text:
            skill = (d / "SKILL.md").relative_to(self.root).as_posix()
            self.report(s, 0, f"not named in {skill}")

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
    """Lint the skills of the repository at root; return how many skills there are and every
    finding. A tree with no skill is an error, since a pass over nothing would print clean."""
    skills = root / ".claude" / "skills"
    if not skills.is_dir() or not any(d.is_dir() for d in skills.iterdir()):
        raise RuntimeError(f"found no skill under {skills}, so nothing was checked")
    top_level, tests, known = load_tree(root)
    files = [root / f for f in sorted(known) if f.startswith(".claude/skills/")]
    lint = Linter(root, top_level, tests, known, files, go_sources(root))
    return lint.run(), lint.findings


def main(argv: list[str] | None = None) -> int:
    # It takes no argument, so --help prints the usage and any other argument is refused, rather
    # than read as an option a clean result would then seem to have honoured.
    argparse.ArgumentParser(description=__doc__.split("\n\n")[0].replace("\n", " ")).parse_args(
        sys.argv[1:] if argv is None else argv)
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
