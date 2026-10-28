#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""List the citations in the skills' rules that the cited issue or PR may not bear out, for a person
to read against the item.

Usage: python3.14 citations.py [KEY...]    every rule by default, else the rules with these keys

A rule's Why cites the PRs and issues that showed its failure. For each citation this fetches the
item's text (title, body, comments, reviews, review comments and commit messages) through gh,
caching it under the system's temporary directory, and scores the clause the citation closes by how
many of its terms the text holds. A citation scoring under 0.4, or well under the other citations
in its clause, is listed with the terms the item lacks. Most listed citations hold when read: the
score only orders the reading. A number that names no issue or PR is an error. Exit 0 when every
cited number exists, 1 when one does not, 2 when it cannot run.
"""

import argparse
import concurrent.futures
import json
import re
import subprocess
import sys
import tempfile
import traceback
from collections.abc import Callable
from pathlib import Path
from typing import NamedTuple

import skills_lint

LOW = 0.4  # a citation under this score is listed
OUTLIER = 0.6  # so is one under this share of the best score in its clause
JOBS = 8  # fetches at once
GROUP = re.compile(r"\(([^()]*#\d+[^()]*)\)|#\d+")
WORD = re.compile(r"[A-Za-z][A-Za-z0-9_]{4,}")
IDENTIFIER = re.compile(r"[A-Za-z_][A-Za-z0-9_.]{2,}")
STEM = 6
STOP = set("""about above after again against along already also although always among another
answer anything around became because become before being below between beyond both cannot could
didn every everything first found from given going having however instead itself later least less
made make makes many might more most much must never none nothing often once only other others
ought over passed passes rather really same seems should since some something still such than that
their them then there these they thing things this those though through under until upon used uses
using very were what when where whether which while whole will with within without would your rule
rules test tests change changes changed case cases code input inputs line lines name named names
reported report reports value values check checks checked showed shows show held holds hold
green""".split())


class Rule(NamedTuple):
    file: str
    key: str
    why: str


class Listed(NamedTuple):
    score: float
    rule: Rule
    number: int
    title: str
    clause: str
    absent: list[str]


def rules_in(root: Path) -> list[Rule]:
    """Return every rule the skills' reference files hold, read as skills_lint reads them."""
    out = []
    for ref in sorted(root.glob(".claude/skills/*/references/*.md")):
        for _, block in skills_lint.rule_blocks(ref.read_text()):
            key = skills_lint.RULE_KEY.match(block[1]) if len(block) > 1 else None
            bullets, _ = skills_lint.rule_bullets(block)
            why = next((text for name, text in bullets if name == "Why"), "")
            if key and why:
                why = why.split("**Why.**", 1)[1].strip()
                out.append(Rule(ref.relative_to(root).as_posix(), key.group(1), why))
    return out


def clauses(why: str) -> dict[int, str]:
    """Map each number a Why cites to the clause it is evidence for: the text before its
    parenthesised group, back to the previous group, or the sentence around a bare #N."""
    out: dict[int, str] = {}
    start = 0
    for m in GROUP.finditer(why):
        if m.group(0).startswith("("):
            clause = why[start:m.start()]
            start = m.end()
        else:
            sentence = re.split(r"(?<=[.;])\s", why[:m.start()])[-1]
            clause = sentence + why[m.end():m.end() + 120]
        for n in re.findall(r"#(\d+)", m.group(0)):
            out.setdefault(int(n), clause if clause.strip(" ;,.") else why)
    return out


def terms(clause: str) -> tuple[list[str], list[str]]:
    """Return the terms a clause rests on: the identifiers it quotes, and its longer words."""
    quoted = {t for span in re.findall(r"`([^`]+)`", clause) for t in IDENTIFIER.findall(span)}
    prose = {w.lower() for w in WORD.findall(re.sub(r"`[^`]*`", " ", clause))} - STOP
    return sorted(quoted), sorted(prose)


def score(clause: str, text: str) -> tuple[float, list[str]]:
    """Return the share of a clause's terms the text holds, and the terms it lacks: a quoted
    identifier must appear as written, a word by its first STEM letters in any case."""
    quoted, prose = terms(clause)
    stems = {w.lower()[:STEM] for w in WORD.findall(text)}
    absent = [t for t in quoted if t not in text] + [w for w in prose if w[:STEM] not in stems]
    total = len(quoted) + len(prose)
    return ((total - len(absent)) / total if total else 1.0), absent


def gh(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run(["gh", "api", *args], capture_output=True, text=True, errors="replace")


def fetch(number: int) -> str | None:
    """Return an issue's or PR's text, its title first, or None when the repository has none by
    that number."""
    base = "repos/{owner}/{repo}"
    item = gh(f"{base}/issues/{number}")
    if item.returncode != 0:
        if "HTTP 404" in item.stderr:
            return None
        raise subprocess.CalledProcessError(item.returncode, item.args, item.stdout, item.stderr)
    data = json.loads(item.stdout)
    pr = "pull_request" in data  # the issues endpoint marks a PR by this key alone
    pages = [f"{base}/issues/{number}/comments"]
    if pr:
        pages += [f"{base}/pulls/{number}/reviews", f"{base}/pulls/{number}/comments"]
    parts = [data["title"], data.get("body") or ""]
    for page in pages:
        parts.append(checked(gh("--paginate", page, "--jq", ".[].body")).stdout)
    if pr:
        commits = gh("--paginate", f"{base}/pulls/{number}/commits", "--jq", ".[].commit.message")
        parts.append(checked(commits).stdout)
    return "\n".join(parts)


def checked(run: subprocess.CompletedProcess) -> subprocess.CompletedProcess:
    if run.returncode != 0:
        raise subprocess.CalledProcessError(run.returncode, run.args, run.stdout, run.stderr)
    return run


def texts(numbers: list[int], cache: Path,
          get: Callable[[int], str | None] = fetch) -> dict[int, str | None]:
    """Return each number's text, read from cache or fetched JOBS at a time and cached; a
    number that names nothing maps to None and is not cached."""
    cache.mkdir(parents=True, exist_ok=True)

    def one(n: int) -> tuple[int, str | None]:
        path = cache / f"{n}.txt"
        if path.is_file():
            return n, path.read_text()
        text = get(n)
        if text is not None:
            partial = path.with_suffix(".partial")
            partial.write_text(text)
            partial.replace(path)
        return n, text

    with concurrent.futures.ThreadPoolExecutor(max_workers=JOBS) as pool:
        return dict(pool.map(one, numbers))


def audit(rules: list[Rule], found: dict[int, str | None]) -> list[Listed]:
    """Score every citation and return those to read: under LOW, or under OUTLIER times the
    best score among the citations sharing its clause."""
    listed = []
    for rule in rules:
        scored = []
        for n, clause in clauses(rule.why).items():
            text = found.get(n)
            if text is not None:
                s, absent = score(clause, text)
                scored.append(Listed(s, rule, n, text.split("\n", 1)[0], clause.strip(), absent))
        for item in scored:
            best = max(o.score for o in scored if o.clause == item.clause)
            if item.score < LOW or item.score < OUTLIER * best:
                listed.append(item)
    return sorted(listed, key=lambda x: (x.score, x.rule.key, x.number))


def report(rules: list[Rule], found: dict[int, str | None]) -> tuple[list[str], bool]:
    """Return the report's lines, and whether every cited number names an issue or PR."""
    missing = [(r, n) for r in rules for n in clauses(r.why) if found.get(n) is None]
    listed = audit(rules, found)
    pairs = sum(len(clauses(r.why)) for r in rules)
    lines = [f"citations: {len(rules)} rules, {pairs} citations of {len(found)} numbers; "
             f"{len(listed)} to read against the item, {len(missing)} naming nothing"]
    for rule, n in missing:
        lines.append(f"MISSING {rule.file}#{rule.key}: #{n} names no issue or PR")
    for x in listed:
        lines.append(f"{x.score:.2f}  {x.rule.file}#{x.rule.key}  #{x.number} {x.title[:70]}")
        lines.append(f"      clause: {x.clause[:200]}")
        lines.append(f"      absent: {', '.join(x.absent)[:200]}")
    return lines, not missing


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="List rule citations to read against their items.")
    p.add_argument("keys", nargs="*", metavar="KEY")
    args = p.parse_args(sys.argv[1:] if argv is None else argv)
    try:
        top = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True,
                             text=True, errors="replace")
        root = Path(checked(top).stdout.strip())
        rules = rules_in(root)
        unknown = sorted(set(args.keys) - {r.key for r in rules})
        if unknown:
            print(f"citations: no rule has the key {', '.join(unknown)}", file=sys.stderr)
            return 2
        rules = [r for r in rules if r.key in args.keys] if args.keys else rules
        slug = checked(gh("repos/{owner}/{repo}", "--jq", ".full_name")).stdout.strip()
        cache = Path(tempfile.gettempdir()) / "morphic-citations" / slug
        numbers = sorted({n for r in rules for n in clauses(r.why)})
        lines, complete = report(rules, texts(numbers, cache))
    except subprocess.CalledProcessError as e:
        command = " ".join(map(str, e.cmd))
        print(f"citations: {command}: {(e.stderr or '').strip()}", file=sys.stderr)
        return 2
    print("\n".join(lines))
    return 0 if complete else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
