#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Check a PR out in a worktree of its own, as it will land, and print what a first review pass
needs: the title measured as it will land, commit subjects, closing keywords, files by area, the
merge state, CI, the squash-message setting, the exported API the change breaks, every name a
skill uses that the tree lacks, and an artifact scan.

Usage: python3.14 review_setup.py PR-NUMBER [DIR]

The worktree holds the head merged into main when the two merge cleanly, the head itself when it
already contains main, and the head alone, reported, when it conflicts. DIR defaults to
<temp dir>/morphic-review-<PR>. An existing clean, detached worktree of this repository there,
as an earlier run leaves, is moved to the new commit; any other directory is left alone and the
run refused, so a checkout holding a branch is never moved. Set REMOTE when the repository the PR
belongs to is not origin,
as in a clone of a fork. Every gh call reads the repository of the remote it fetches from, not one
gh picks by its own rules. Exit 0 when set up, 2 when it cannot be.
"""

import json
import os
import re
import subprocess
import sys
import tempfile
import traceback
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "morphic-curating-skills" / "scripts"))
sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "morphic-shipping-prs" / "scripts"))
import artifact_scan  # noqa: E402  (a sibling script)
import exported_api  # noqa: E402  (a script of the shipping skill)
import skills_lint  # noqa: E402  (a script of the curating skill)

TITLE_CAP = 72
INDENT = " " * 11
CONVENTIONAL = re.compile(r"^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\([^)]+\))?!?: \S")
BREAKING = re.compile(r"^[a-z]+(\([^)]+\))?!:")
# GitHub reads a keyword in any case, a colon after it or not, before #N or OWNER/REPO#N.
CLOSING = re.compile(r"\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?):? +([\w.-]+/[\w.-]+)?#[0-9]+",
                     re.I)
# The review's merge commit needs an identity, and a fresh clone may configure none.
IDENTITY = {f"GIT_{who}_{what}": value for who in ("AUTHOR", "COMMITTER")
            for what, value in (("NAME", "review_setup"), ("EMAIL", "review_setup@localhost"))}
# The domain skill to load for the files a change touches, as each skill's description names them.
DOMAINS = [
    (re.compile(r"^(compilers/openapi/|docs/compilers/openapi\.md|docs/parser-library\.md"
                r"|testdata/([^/]+/)?openapi/)"),
     "morphic-openapi-compiler"),
    (re.compile(r"^(ir/|pass/|docs/ir-design\.md|docs/emitter-design\.md"
                r"|testdata/unwitnessed\.golden\.txt$)"),
     "morphic-changing-the-ir"),
    (re.compile(r"^\.claude/skills/"), "morphic-curating-skills"),
]
# A remote's URL as host, owner and repository: https://host/o/r, git@host:o/r or ssh://host/o/r.
REMOTE_URL = re.compile(r"^(?:[a-z+]+://)?(?:[^@/]+@)?([^/:]+)[:/]([^/]+)/([^/]+?)(?:\.git)?/?$")
# A title of this type makes a cost claim, which the performance skill measures.
PERF_TITLE = re.compile(r"^perf(\([^)]+\))?!?:")


def title_report(title: str, number: str) -> list[str]:
    landed = len(f"{title} (#{number})")
    over = "  OVER" if landed > TITLE_CAP else ""
    ok = CONVENTIONAL.match(title) and not title.endswith(".")
    form = "ok" if ok else "NOT type(scope): subject without a trailing period"
    return [f"title      {title}",
            f'{INDENT}{landed} characters with " (#{number})", cap {TITLE_CAP}{over}',
            f"{INDENT}conventional form: {form}"]


def body_report(body: str, title: str) -> list[str]:
    """Report the sections CLAUDE.md asks of a PR body, Breaking among them when the title marks a
    break with !, and every closing keyword."""
    lines = []
    breaking = BREAKING.match(title) is not None
    for section in ("Summary", "Test plan", "Breaking"):
        has = re.search(rf"^#+ +{section}", body, re.M) is not None
        if has and (breaking or section != "Breaking"):
            lines.append(f"body       has {section}")
        elif has:
            lines.append("body       has a Breaking section, but the title marks no break with !")
        elif breaking or section != "Breaking":
            lines.append(f"body       MISSING a {section} section")
    for keyword in sorted({m.group(0) for m in CLOSING.finditer(body)}):
        lines.append(f"closes     {keyword}")
    return lines


def areas(files: list[str]) -> list[tuple[int, str]]:
    def area(f: str) -> str:
        parts = f.split("/")
        if len(parts) > 2:
            return "/".join(parts[:2])
        return parts[0] if len(parts) > 1 else "(root)"
    return sorted(((n, a) for a, n in Counter(map(area, files)).items()), key=lambda x: x[1])


def changed_files(root: Path, base: str, head: str) -> list[str]:
    """Return the files head changes from base, a moved file under both its paths, so the area
    it left counts too."""
    return git(root, "diff", "--no-renames", "--name-only", base, head).stdout.split()


def domain_hints(files: list[str], title: str = "") -> list[str]:
    hints = {skill for pattern, skill in DOMAINS if any(pattern.match(f) for f in files)}
    if PERF_TITLE.match(title):
        hints.add("morphic-measuring-performance")
    return sorted(hints)


def skills_report(directory: Path) -> list[str]:
    """Report each name a skill uses that the tree under review lacks, such as a test the change
    renamed, which the change itself must retarget."""
    count, problems = skills_lint.findings(directory)
    if problems:
        return [f"skills     {p}" for p in problems]
    return [f"skills     {count} checked, every name they use exists" if count
            else "skills     none in this tree"]


def exported_report(directory: Path, base: str, title: str) -> list[str]:
    """Report the exported API the tree under review breaks against base, and whether the title
    marks the break with !, as preflight.py does for the author."""
    found = exported_api.listed(directory, base)
    if not found:
        return ["exported   no exported API removed or changed"]
    if BREAKING.match(title):
        head = "exported   breaking, as the title's ! says:"
    else:
        head = "exported   NOT marked: the title carries no ! for these breaks:"
    return [head] + [INDENT + f for f in found]


def run(*args: str, check: bool = True, env: dict | None = None) -> subprocess.CompletedProcess:
    full = {**os.environ, **env} if env else None
    return subprocess.run(args, check=check, capture_output=True, text=True, errors="replace",
                          env=full)


def git(root: Path, *args: str, check: bool = True,
        env: dict | None = None) -> subprocess.CompletedProcess:
    return run("git", "-C", str(root), *args, check=check, env=env)


def point_gh_at(root: Path, remote: str) -> None:
    """Point every gh call this process and its children make at the repository remote names,
    which gh would otherwise choose by its own rules, so the PR a script reads is the one it
    fetched."""
    url = git(root, "remote", "get-url", remote).stdout.strip()
    m = REMOTE_URL.match(url)
    if not m:
        raise RuntimeError(f"{remote} ({url}) names no GitHub repository")
    os.environ["GH_REPO"] = "/".join(m.groups())


def checkout(root: Path, directory: Path, head: str) -> None:
    if not directory.exists():
        git(root, "worktree", "add", "-q", "--detach", str(directory), head)
        return
    problem = reuse_problem(root, directory)
    if problem:
        raise RuntimeError(f"{directory} {problem}; pass another DIR")
    git(directory, "checkout", "-q", "--detach", head)


def reuse_problem(root: Path, directory: Path) -> str | None:
    """Say why an existing directory cannot take the review, or None: only a clean, detached
    worktree of this repository can, as an earlier run leaves one."""
    top = git(directory, "rev-parse", "--show-toplevel", check=False)
    if top.returncode != 0 or Path(top.stdout.strip()).resolve() != directory.resolve():
        return "is not a worktree"

    def common(d: Path) -> Path:
        out = git(d, "rev-parse", "--path-format=absolute", "--git-common-dir").stdout.strip()
        return Path(out).resolve()

    if common(directory) != common(root):
        return "is a worktree of another repository"
    if git(directory, "symbolic-ref", "-q", "HEAD", check=False).returncode == 0:
        return "has a branch checked out, which a review never moves"
    if git(directory, "status", "--porcelain").stdout.strip():
        return "has local changes"
    return None


def landing(root: Path, name: str, base: str, head: str) -> tuple[list[str], str, bool]:
    """Return the merge state's report lines, the commit to review, and whether that commit is
    the tree as it will land: head when it contains base, a merge of the two when they merge
    cleanly, head alone otherwise. Base and head are commits; name is how base is printed."""
    if git(root, "merge-base", "--is-ancestor", base, head, check=False).returncode == 0:
        return [f"merge      head contains {name}: CI's run on the head ran this tree"], head, True
    clean = git(root, "merge-tree", "--write-tree", base, head, check=False)
    if clean.returncode == 0:
        merged = git(root, "commit-tree", clean.stdout.split()[0], "-p", head, "-p", base,
                     "-m", f"Merge {name} into the head for review", env=IDENTITY).stdout.strip()
        return [f"merge      clean into {name}; the worktree holds the merge"], merged, True
    if clean.returncode != 1:
        return ["merge      not checked (this git has no merge-tree --write-tree); "
                "the worktree holds the head alone"], head, False
    named = git(root, "merge-tree", "--write-tree", "--name-only", base, head, check=False)
    files = named.stdout.split("\n\n", 1)[0].splitlines()[1:]
    lines = [f"merge      CONFLICTS with {name}; the worktree holds the head alone:"]
    return lines + [INDENT + f for f in files], head, False


def report(root: Path, number: str, directory: Path, remote: str) -> tuple[list[str], str, str]:
    """Set the worktree up and return the report's lines, the revision the scan starts from and
    the head."""
    git(root, "fetch", "-q", remote, "main")
    git(root, "fetch", "-q", remote, f"pull/{number}/head")
    head = git(root, "rev-parse", "FETCH_HEAD").stdout.strip()
    name = f"{remote}/main"
    base = git(root, "rev-parse", "--verify", f"{name}^{{commit}}").stdout.strip()
    mb = git(root, "merge-base", base, head).stdout.strip()
    fields = "title,body,state,isDraft,author,closingIssuesReferences"
    pr = json.loads(run("gh", "pr", "view", number, "--json", fields).stdout)
    merge_lines, target, lands = landing(root, name, base, head)
    checkout(root, directory, target)  # last, so a failure above leaves no worktree behind

    draft = ", draft" if pr["isDraft"] else ""
    what = "the head" if target == head else f"the head {head[:12]} merged into {name}"
    diff = f"git diff {base[:12]}" if lands else f"git diff {mb[:12]}"
    out = [f"PR #{number}  {pr['state']}{draft}, by {pr['author']['login']}",
           f"worktree   {directory} @ {target[:12]}: {what}",
           f"{INDENT}`{diff}` there shows the change as "
           + ("it will land" if lands else "the branch made it"), ""]
    out += title_report(pr["title"], number) + body_report(pr["body"], pr["title"])
    out += [f"linked     #{i['number']}" for i in pr["closingIssuesReferences"]]

    subjects = git(root, "log", "--format=%s", f"{mb}..{head}").stdout.splitlines()
    out += ["", f"commits    {len(subjects)} on the branch:"]
    for s in subjects:
        over = "  OVER 72" if len(s) > TITLE_CAP else ""
        out.append(f"{INDENT}{len(s):3}  {s}{over}")

    files = changed_files(root, mb, head)
    out += ["", "files by area:"] + [f"{INDENT}{n:4}  {a}" for n, a in areas(files)]
    out.append(INDENT + git(root, "diff", "--shortstat", mb, head).stdout.strip())
    out += [f"load       {skill}" for skill in domain_hints(files, pr["title"])]
    out += exported_report(directory, name, pr["title"])
    out += skills_report(directory)

    out += [""] + merge_lines
    squash = run("gh", "api", "repos/{owner}/{repo}", "--jq", ".squash_merge_commit_message",
                 check=False)
    out.append(f"squash     body default: {squash.stdout.strip() or 'unknown'}")
    out.append("checks:")
    for line in run("gh", "pr", "checks", number, check=False).stdout.splitlines():
        cells = line.split("\t")
        if len(cells) >= 2:
            out.append(f"{INDENT}{cells[1]:8} {cells[0]}")
    return out + [""], mb, head


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if not args or not args[0].isdigit():
        print(__doc__.split("\n\n")[1], file=sys.stderr)
        return 2
    number = args[0]
    try:
        root = Path(run("git", "rev-parse", "--show-toplevel").stdout.strip())
        default = Path(tempfile.gettempdir()) / f"morphic-review-{number}"
        directory = Path(args[1] if len(args) > 1 else default).resolve()
        remote = os.environ.get("REMOTE", "origin")
        point_gh_at(root, remote)
        lines, mb, head = report(root, number, directory, remote)
    except subprocess.CalledProcessError as e:
        print(f"review_setup: {' '.join(e.cmd)}: {e.stderr.strip()}", file=sys.stderr)
        return 2
    except (RuntimeError, FileNotFoundError) as e:
        print(f"review_setup: {e}", file=sys.stderr)
        return 2
    print("\n".join(lines))
    artifact_scan.main([mb, head, "--pr", number])
    print(f"\nnext: cd {directory}, then the review's passes; once the PR is closed,\n"
          f"      git worktree remove --force {directory}")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
