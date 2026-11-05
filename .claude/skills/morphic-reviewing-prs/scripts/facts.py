#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Print the facts a review round would otherwise derive by hand, a line each, so that its reader
spends its budget on judging them.

Usage: python3.14 .claude/skills/morphic-reviewing-prs/scripts/facts.py [--base REF] [--since REF]
       [--body FILE] NUMBER

Run it in the review's worktree. The change is HEAD, uncommitted edits left out, against what it
holds of REF (default origin/main) merged with what it holds of origin/main, so a stacked review
counts no commit of main's as its own; where the two conflict, the files are named unread and
HEAD's resolution of them counts. NUMBER is the PR; before it exists, FILE holds its body.
- rule: each rule key added, removed, renamed, moved or reworded, compared word for word.
- cite: each line citing such a key outside its own rule, or none.
- command: each code span holding a space, and each line of a shell block, in the Rule and Probe
  of each rule added or reworded, and on the lines the change adds outside any rule to Markdown
  under .claude, CLAUDE.md and CONTRIBUTING.md.
- number: each prose line of the body, and each one the change adds to Markdown, holding a number
  outside a list item's marker, a code span, an issue, a revision, a link's target or a URL.
- issue: each issue the PR closes, by its references or a closing keyword in the body before #N
  or this repository's owner/name#N, with its state, comments and last update, marked when that
  update follows the change's own last commit.
- pr: each other open PR whose own change, read from its fork with origin/main, shares a file with
  this one, and whether it merges with HEAD, a file it also conflicts in against REF or origin/main
  marked with each. A head this repository lacks is fetched from origin without writing a ref.
- stale: with --since, each file whose content differs from the tree at REF, each line citing a
  rule reworded since, and the file each changed one pairs with by name, a test with what it
  tests: test_<name>.py with <name>.py, <name>_test.go or <name>_internal_test.go with <name>.go.
A rule moved or renamed with its words unchanged adds no command or number. These are facts, not
findings: a reader judges each. Exit 0 when every fact was read, 2 when one could not be, the rest
still printed.
"""

import argparse
import datetime
import json
import os
import re
import subprocess
import sys
import traceback
from dataclasses import dataclass
from pathlib import Path, PurePosixPath

BASE = "origin/main"
REMOTE = "origin"
OPEN_PR_LIMIT = 200
TIMEOUT = 300
REFERENCE = re.compile(r"^\.claude/skills/[^/]+/references/[^/]+\.md$")
RULE_KEY = re.compile(r"^`([a-z0-9]+(?:-[a-z0-9]+)*)`$")
CITED_KEY = re.compile(r"#([a-z0-9]+(?:-[a-z0-9]+)*)")
# The file a citation of a rule names, which moving the rule it cites rewrites.
CITED_FILE = re.compile(r"(?:\.claude/skills/[a-z0-9-]+/)?references/[A-Za-z0-9_.-]+\.md(?=#)")
SAME = "same words"
RETARGETED = "same words but its citations' files"
BULLET = re.compile(r"^- \*\*([A-Za-z]+)\.\*\*")
SPAN = re.compile(r"`([^`]+)`")
# A fenced block's opening or closing run and its info string, as CommonMark reads one.
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})\s*(\S*)")
SHELL = {"bash", "sh", "shell", "zsh", "console"}
# The Markdown that prescribes the commands a round runs: the skills and the contributor guides.
PRESCRIBING = re.compile(r"^(?:\.claude/.*|CLAUDE|CONTRIBUTING)\.md$")
# What a number in prose is not: a list item's marker, a code span, an issue, a revision, a link's
# target or a URL.
NOT_A_COUNT = re.compile(
    r"^\s*\d+[.)]\s|`[^`]*`|#\d+|\b(?=[0-9a-f]*[a-f])[0-9a-f]{7,40}\b|\]\([^)]*\)|https?://\S+")
DIGIT = re.compile(r"\d")
# GitHub's closing keywords, which close the issue they precede wherever a body writes one.
CLOSING = re.compile(r"\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?):? +([\w.-]+/[\w.-]+)?#(\d+)",
                     re.I)
# The owner/name a remote's URL ends in, as a GitHub HTTPS or SSH URL writes it.
REPOSITORY = re.compile(r"[:/]([^/:]+/[^/]+?)(?:\.git)?/?$")
HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@")
KINDS = ("rule", "cite", "command", "number", "issue", "pr", "stale")


@dataclass(frozen=True)
class Rule:
    """A keyed rule in a skill's reference: its file, the lines of its heading and its last
    line, counted from 1, and its text."""

    path: str
    first: int
    last: int
    lines: tuple[str, ...]

    def words(self, keyed: bool = True, files: bool = True) -> list[str]:
        """Return its words: its key's line among them only when keyed, and the files its
        citations name only when files."""
        text = " ".join(self.lines if keyed else self.lines[:1] + self.lines[2:])
        return (text if files else CITED_FILE.sub("", text)).split()


def likeness(base: Rule, head: Rule, keyed: bool = True) -> str | None:
    """Return how a rule's words compare, or None when they differ beyond its citations' files."""
    if base.words(keyed) == head.words(keyed):
        return SAME
    if base.words(keyed, False) == head.words(keyed, False):
        return RETARGETED
    return None


def rules_in(path: str, text: str) -> dict[str, Rule]:
    """Return each rule of a reference by key: a "## " heading with its key alone on the next
    line, running to the next heading, blank lines at its end left out."""
    lines = text.split("\n")
    heads = [i for i, line in enumerate(lines) if line.startswith("## ")]
    found = {}
    for n, i in enumerate(heads):
        key = RULE_KEY.match(lines[i + 1]) if i + 1 < len(lines) else None
        if not key:
            continue
        end = heads[n + 1] if n + 1 < len(heads) else len(lines)
        while not lines[end - 1].strip():
            end -= 1
        found[key.group(1)] = Rule(path, i + 1, end, tuple(lines[i:end]))
    return found


def compare(base: dict[str, Rule], head: dict[str, Rule]) -> dict[str, str]:
    """Return, by key, how each rule differs from base to head; a rule the same in both is left
    out. A rule whose words but its key are those of a removed one is renamed from it."""
    removed = {k: r for k, r in base.items() if k not in head}
    found = {}
    for key, h in sorted(head.items(), key=lambda kv: (kv[1].path, kv[1].first)):
        b = base.get(key)
        at = f"{h.path}:{h.first}"
        if b is None:
            was = next((k for k, r in removed.items() if likeness(r, h, False)), None)
            if was:
                found[key] = (f"renamed from {was} at {removed_at(removed.pop(was))} to {at}, "
                              f"{likeness(base[was], h, False)}")
            else:
                found[key] = f"added at {at}"
        elif b.path != h.path:
            found[key] = f"moved from {b.path} to {at}, {likeness(b, h) or 'words differ'}"
        elif (same := likeness(b, h)) != SAME:
            found[key] = f"edited at {at}, {same}" if same else f"reworded at {at}"
    for key, b in sorted(removed.items(), key=lambda kv: (kv[1].path, kv[1].first)):
        found[key] = f"removed from {removed_at(b)}"
    return found


def removed_at(rule: Rule) -> str:
    return f"{rule.path}:{rule.first}"


def unchanged(fact: str) -> bool:
    """Report whether a rule's change left its words as they were, bar its citations' files."""
    return fact.endswith((SAME, RETARGETED))


def fenced(lines: list[str]) -> dict[int, bool]:
    """Return each line inside a fenced block, counted from 0, and whether its block is shell."""
    found, opened = {}, None
    for i, line in enumerate(lines):
        fence = FENCE.match(line)
        if opened is None:
            # A backtick run with a backtick after it opens no block.
            if fence and not (fence.group(1)[0] == "`" and "`" in line[fence.end(1):]):
                opened = (fence.group(1), fence.group(2).lower() in SHELL)
            continue
        run = fence.group(1) if fence else ""
        if run[:1] == opened[0][0] and len(run) >= len(opened[0]) and not fence.group(2):
            opened = None
            continue
        found[i] = opened[1]
    return found


def commands(lines: list[str], picked: list[int]) -> list[tuple[int, str]]:
    """Return each line of a shell block, and each code span holding a space, on the picked
    lines, counted from 0."""
    blocks = fenced(lines)
    found = []
    for i in picked:
        if blocks.get(i):
            found.append((i, lines[i].strip()))
        elif i not in blocks:
            found += [(i, s) for s in SPAN.findall(lines[i]) if " " in s.strip()]
    return found


def prescribing(rule: Rule) -> list[int]:
    """Return the lines of a rule's Rule and Probe bullets, counted from 0 in its file."""
    picked, inside = [], False
    for n, line in enumerate(rule.lines):
        bullet = BULLET.match(line)
        if bullet:
            inside = bullet.group(1) in ("Rule", "Probe")
        if inside:
            picked.append(rule.first - 1 + n)
    return picked


def counted(line: str) -> bool:
    """Report whether a prose line states a number a reader derives."""
    return bool(DIGIT.search(NOT_A_COUNT.sub("", line)))


def numbers(lines: list[str], picked: list[int]) -> list[int]:
    """Return the picked lines, counted from 0, that are prose stating a number."""
    blocks = fenced(lines)
    return [i for i in picked if i not in blocks and counted(lines[i])]


def closing(body: str, references: list[int], repository: str) -> list[int]:
    """Return each issue a PR closes: those GitHub lists and those a keyword in its body names,
    by #N or by this repository's owner/name#N."""
    named = {int(n) for owner, n in CLOSING.findall(body)
             if not owner or owner.lower() == repository.lower()}
    return sorted(set(references) | named)


def run_git(*args: str, ok: tuple[int, ...] = (0,),
            stdin: str | None = None) -> subprocess.CompletedProcess:
    """Run git, raising on an exit status outside ok."""
    done = subprocess.run(["git", *args], capture_output=True, text=True, errors="replace",
                          input=stdin, timeout=TIMEOUT)
    if done.returncode not in ok:
        raise subprocess.CalledProcessError(done.returncode, ["git", *args], done.stdout,
                                            done.stderr)
    return done


def git(*args: str, ok: tuple[int, ...] = (0,), stdin: str | None = None) -> str:
    return run_git(*args, ok=ok, stdin=stdin).stdout


def gh(*args: str) -> str:
    return subprocess.run(["gh", *args], capture_output=True, text=True, check=True,
                          timeout=TIMEOUT).stdout


def change_base(report: Report, ref: str) -> str:
    """Return the tree the change is read against: what HEAD holds of ref merged with what it
    holds of origin/main. A stacked review's tree merges main beside the stack's base, and main's
    commits are no change of the PR's own. Where the two conflict, the merge's tree keeps the
    conflict, so the change also counts how HEAD resolved it, and those files are named unread."""
    ours = git("merge-base", ref, "HEAD").strip()
    main = git("merge-base", BASE, "HEAD").strip()
    done = run_git("merge-tree", "--write-tree", "--name-only", "--no-messages", ours, main,
                   ok=(0, 1))
    tree, *conflicted = [line for line in done.stdout.splitlines() if line]
    if conflicted:
        report.missed(f"the change apart from {BASE}'s commits in {', '.join(conflicted)}",
                      ValueError(f"{ref} and {BASE} conflict there, so HEAD's resolution counts"))
    return tree


def tree_rules(rev: str) -> dict[str, Rule]:
    """Return every rule in the skills' references at rev, by key."""
    listed = git("ls-tree", "-r", "--name-only", rev, "--", ".claude/skills").splitlines()
    found = {}
    for path in filter(REFERENCE.match, listed):
        found.update(rules_in(path, git("show", f"{rev}:{path}")))
    return found


def citing(keys: list[str], rules: dict[str, Rule]) -> dict[str, list[str]]:
    """Return, for each key, the lines of HEAD citing it outside its own rule."""
    found: dict[str, list[str]] = {k: [] for k in keys}
    if not keys:
        return found
    patterns = [a for k in keys for a in ("-e", f"#{k}")]
    for hit in git("grep", "-n", "-I", "-F", *patterns, "HEAD", "--", ok=(0, 1)).splitlines():
        _, path, line, text = hit.split(":", 3)
        for key in {m.group(1) for m in CITED_KEY.finditer(text)} & found.keys():
            own = rules.get(key)
            if not (own and own.path == path and own.first <= int(line) <= own.last):
                found[key].append(f"{path}:{line}")
    return found


def added(base: str) -> dict[str, list[int]]:
    """Return, for each Markdown file, the lines the change adds to it, counted from 0. A file's
    header runs from its "diff --git" line to its first hunk, so an added line reading "++ x" is
    no header."""
    diff = git("-c", "core.quotePath=false", "diff", "-U0", "-M", "--no-color", "--no-ext-diff",
               base, "HEAD", "--", "*.md")
    found: dict[str, list[int]] = {}
    path, line, header = None, 0, False
    for row in diff.splitlines():
        if row.startswith("diff --git "):
            path, header = None, True
        elif header and row.startswith("+++ "):
            path = row[6:] if row.startswith("+++ b/") else None
        elif hunk := HUNK.match(row):
            line, header = int(hunk.group(1)) - 1, False
        elif not header and row.startswith("+") and path:
            found.setdefault(path, []).append(line)
            line += 1
    return found


class Report:
    """The facts a run prints, and what it could not read."""

    def __init__(self) -> None:
        self.lines: list[str] = []
        self.unread: list[str] = []

    def add(self, kind: str, fact: str) -> None:
        self.lines.append(f"{kind}: {fact}")

    def missed(self, what: str, error: BaseException) -> None:
        detail = getattr(error, "stderr", "") or str(error)
        self.unread.append(f"{what} ({' '.join(str(detail).split())[:200]})")


def rule_facts(report: Report, base: str) -> tuple[dict[str, Rule], dict[str, str]]:
    """Report each rule the change touches and every line citing it; return HEAD's rules and
    the changes."""
    head = tree_rules("HEAD")
    changes = compare(tree_rules(base), head)
    for key, fact in changes.items():
        report.add("rule", f"{key}: {fact}")
    sites = citing(list(changes), head)
    for key in changes:
        for site in sites[key] or ["none"]:
            report.add("cite", f"{key}: {site}")
    return head, changes


def text_facts(report: Report, base: str, head: dict[str, Rule], changes: dict[str, str]) -> None:
    """Report the commands and the numbers the change writes into Markdown."""
    adds = added(base)
    edited = {k for k, fact in changes.items() if k in head and not unchanged(fact)}
    owner = {(r.path, i): k for k, r in head.items() for i in range(r.first - 1, r.last)}
    paths = sorted(adds.keys() | {head[k].path for k in edited})
    texts = {p: git("show", f"HEAD:{p}").split("\n") for p in paths}
    for path in paths:
        picked = [i for i in adds.get(path, [])
                  if PRESCRIBING.match(path) and (path, i) not in owner]
        picked += [i for k in edited if head[k].path == path for i in prescribing(head[k])]
        for i, command in commands(texts[path], sorted(set(picked))):
            report.add("command", f"{path}:{i + 1}: {command}")
    for path in paths:
        picked = [i for i in adds.get(path, [])
                  if not unchanged(changes.get(owner.get((path, i)), ""))]
        for i in numbers(texts[path], picked):
            report.add("number", f"{path}:{i + 1}: {texts[path][i].strip()}")


def read_body(args: argparse.Namespace) -> tuple[str, list[int]]:
    """Return the PR's body and the issues GitHub lists it as closing."""
    if args.body:
        return Path(args.body).read_text(), []
    view = json.loads(gh("pr", "view", str(args.number), "--json",
                         "body,closingIssuesReferences"))
    return view["body"], [issue["number"] for issue in view["closingIssuesReferences"]]


def issue_states(numbers_: list[int]) -> dict[int, dict | None]:
    """Return each issue's state, update time and comment count, in one query, or None for a
    number no issue has. gh exits 1 on such a number while still printing what it found."""
    fields = " ".join(f"i{n}: issue(number: {n}) {{ state updatedAt comments {{ totalCount }} }}"
                      for n in numbers_)
    query = ("query($owner: String!, $repo: String!) { repository(owner: $owner, name: $repo) "
             f"{{ {fields} }} }}")
    done = subprocess.run(["gh", "api", "graphql", "-f", f"query={query}", "-F", "owner={owner}",
                           "-F", "repo={repo}"], capture_output=True, text=True, timeout=TIMEOUT)
    reply = json.loads(done.stdout or "{}")
    failed = [e for e in reply.get("errors", []) if e.get("type") != "NOT_FOUND"]
    if failed or "data" not in reply:
        raise ValueError(failed or done.stderr.strip() or "gh printed no data")
    found = reply["data"]["repository"]
    return {n: found[f"i{n}"] for n in numbers_}


def issue_facts(report: Report, body: str, references: list[int], ref: str) -> None:
    """Report each issue the PR closes, and whether it changed after the change's last commit, the
    newest one neither ref nor origin/main holds, merges left out."""
    url = run_git("remote", "get-url", REMOTE, ok=(0, 1, 2, 128)).stdout.strip()
    repository = m.group(1) if (m := REPOSITORY.search(url)) else ""
    issues = closing(body, references, repository)
    if not issues:
        return
    last = git("log", "-1", "--no-merges", "--format=%cI", "HEAD", "--not", ref, BASE).strip()
    since = datetime.datetime.fromisoformat(last) if last else None
    for n, state in issue_states(issues).items():
        if state is None:
            report.add("issue", f"#{n}: no issue of that number")
            continue
        updated = state["updatedAt"]
        late = since and datetime.datetime.fromisoformat(updated) > since
        report.add("issue", f"#{n}: {state['state']}, {state['comments']['totalCount']} "
                            f"comment(s), updated {updated}"
                            + (", after the change's last commit" if late else ""))


def body_facts(report: Report, args: argparse.Namespace) -> None:
    """Report the body's numbers and the issues it closes."""
    try:
        body, references = read_body(args)
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, TypeError) as e:
        report.missed("the body and the issues it closes", e)
        return
    lines = body.splitlines()
    for i in numbers(lines, list(range(len(lines)))):
        report.add("number", f"body:{i + 1}: {lines[i].strip()}")
    try:
        issue_facts(report, body, references, args.base)
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, TypeError) as e:
        report.missed("the issues the PR closes", e)


def present(shas: list[str]) -> set[str]:
    """Return the commits among shas this repository holds."""
    out = git("cat-file", "--batch-check", stdin="".join(f"{s}^{{commit}}\n" for s in shas))
    return {s for s, row in zip(shas, out.splitlines()) if not row.endswith(" missing")}


def open_prs(report: Report, number: int) -> list[tuple[int, str]]:
    """Return each other open PR and its head, fetching the heads this repository lacks."""
    listed = json.loads(gh("pr", "list", "--state", "open", "--limit", str(OPEN_PR_LIMIT),
                           "--json", "number,headRefOid"))
    if len(listed) >= OPEN_PR_LIMIT:
        report.missed(f"the open PRs past the first {OPEN_PR_LIMIT}", ValueError("limit reached"))
    others = sorted((pr["number"], pr["headRefOid"]) for pr in listed if pr["number"] != number)
    held = present([sha for _, sha in others])
    missing = [f"pull/{n}/head" for n, sha in others if sha not in held]
    if missing:
        try:
            git("fetch", "--quiet", "--no-write-fetch-head", REMOTE, *missing)
        except subprocess.SubprocessError as e:
            report.missed("the heads of open PRs this repository lacks", e)
    return others


def conflicts(ours: str, theirs: str) -> list[str]:
    """Return the files a merge of theirs into ours conflicts in, which merge-tree lists after the
    tree it wrote, and only on a conflict."""
    done = run_git("merge-tree", "--write-tree", "--name-only", "--no-messages", ours, theirs,
                   ok=(0, 1))
    return [line for line in done.stdout.splitlines()[1:] if line]


def merges(sha: str, bases: list[str]) -> str:
    """Return whether sha merges with HEAD, and the files it conflicts in, marking each it also
    conflicts in against any of bases, which no change of HEAD's own caused, with each such base."""
    found = conflicts("HEAD", sha)
    if not found:
        return "merges clean"
    against = {b: set(conflicts(b, sha)) for b in bases}
    named = {f: [b for b in bases if f in against[b]] for f in found}
    return "conflicts in " + ", ".join(
        f"{f} (also against {' and '.join(named[f])})" if named[f] else f for f in found)


def pr_facts(report: Report, args: argparse.Namespace, changed: set[str]) -> None:
    """Report each other open PR sharing a file with the change."""
    try:
        others = open_prs(report, args.number)
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, TypeError) as e:
        report.missed("the open PRs", e)
        return
    held = present([sha for _, sha in others])
    bases = list(dict.fromkeys([args.base, BASE]))
    for n, sha in others:
        if sha not in held:
            report.missed(f"#{n}'s head {sha[:12]}", ValueError("not fetched"))
            continue
        try:
            fork = git("merge-base", BASE, sha).strip()
            theirs = git("diff", "--name-only", "--no-renames", fork, sha).splitlines()
            shared = sorted(changed.intersection(theirs))
            if shared:
                report.add("pr", f"#{n}: shares {', '.join(shared)}; {merges(sha, bases)}")
        except subprocess.SubprocessError as e:
            report.missed(f"#{n}'s files and merge", e)


def beside(path: str, listed: set[str]) -> list[str]:
    """Return the files listed that a file's name pairs it with, a test with what it tests:
    test_<name>.py with <name>.py, and <name>_test.go or <name>_internal_test.go with <name>.go."""
    p = PurePosixPath(path)
    if p.suffix == ".py":
        names = [p.name.removeprefix("test_") if p.name.startswith("test_") else f"test_{p.name}"]
    elif p.suffix == ".go":
        stem = p.stem
        tested = next((stem.removesuffix(end) for end in ("_internal_test", "_test")
                       if stem.endswith(end)), None)
        names = [f"{tested}.go"] if tested else [f"{stem}_test.go", f"{stem}_internal_test.go"]
    else:
        return []
    return sorted(f for f in (str(p.with_name(n)) for n in names) if f in listed)


def stale_facts(report: Report, since: str, head: dict[str, Rule], changed: set[str]) -> None:
    """Report what differs from the tree at since: each file, each line citing a rule reworded
    since, and the file each changed one pairs with by name."""
    files = git("diff", "--name-only", "--no-renames", since, "HEAD").splitlines()
    for path in files:
        report.add("stale", path + ("" if path in changed else " (outside the change)"))
    moved = {k: f for k, f in compare(tree_rules(since), head).items() if not unchanged(f)}
    for key, sites in citing(list(moved), head).items():
        for site in sites:
            report.add("stale", f"{site}: cites {key}, {moved[key]}")
    listed = set(git("ls-tree", "-r", "--name-only", "HEAD").splitlines())
    tests = {t: path for path in files for t in beside(path, listed) if t not in files}
    for test, path in sorted(tests.items()):
        report.add("stale", f"{test}: beside {path}")


def parse(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--base", default=BASE, metavar="REF",
                   help=f"the branch the change lands on (default {BASE})")
    p.add_argument("--since", metavar="REF", help="the merged tree the previous round reviewed")
    p.add_argument("--body", metavar="FILE", help="the PR's body, before the PR exists")
    p.add_argument("number", type=int, help="the PR's number, or the one it will get")
    args = p.parse_args(argv)
    if args.number < 1:
        p.error(f"number must be positive, not {args.number}")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse(sys.argv[1:] if argv is None else argv)
    try:
        os.chdir(git("rev-parse", "--show-toplevel").strip())
        if args.since:
            git("rev-parse", "--verify", "--quiet", f"{args.since}^{{commit}}")
        report = Report()
        base = change_base(report, args.base)
    except (OSError, subprocess.SubprocessError) as e:
        print(f"facts: cannot run: {getattr(e, 'stderr', '') or e}".strip(), file=sys.stderr)
        return 2
    head, changes = rule_facts(report, base)
    text_facts(report, base, head, changes)
    body_facts(report, args)
    changed = set(git("diff", "--name-only", "--no-renames", base, "HEAD").splitlines())
    pr_facts(report, args, changed)
    if args.since:
        stale_facts(report, args.since, head, changed)
    for line in report.lines:
        print(line)
    counts = {k: sum(line.startswith(f"{k}: ") for line in report.lines) for k in KINDS}
    print("facts: " + ", ".join(f"{counts[k]} {k}" for k in KINDS))
    if report.unread:
        print("facts: not read: " + "; ".join(report.unread))
        return 2
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
