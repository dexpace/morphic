#!/usr/bin/env python3
"""Render each fetched PR as one Markdown dossier an extraction agent reads: the body, every
review comment, and every commit with its message and file list, marked INITIAL or FOLLOW-UP.

Usage: dossier.py <styleguide-checkout>"""
import json
import subprocess
import sys

from common import REPO, WORK

RAW, OUT = WORK / "raw", WORK / "dossiers"


def files(checkout, sha):
    r = subprocess.run(["git", "-C", str(checkout), "show", "--format=", "--stat=120", sha],
                       capture_output=True, text=True)
    return r.stdout.strip() if r.returncode == 0 else "(commit not available locally)"


def render(d, checkout):
    L = [f"# {d['repo']}#{d['number']} — {d['title']}\n"
         f"state: {d['state']} merged_at: {d['merged_at']} commits: {len(d['commits'])}\n",
         "## PR body\n" + (d["body"] or "(empty)").strip() + "\n"]
    if d["reviews"] or d["review_comments"] or d["issue_comments"]:
        L.append("## Review activity")
        for r in d["reviews"]:
            if r["body"]:
                L.append(f"### review by {r['user']} ({r['state']}) on {r['commit_id'][:8]}\n{r['body']}\n")
        for c in d["review_comments"]:
            reply = ", reply" if c["in_reply_to"] else ""
            L.append(f"### inline by {c['user']} on {c['path']}:{c['line']} (commit {c['commit_id'][:8]}{reply})\n"
                     f"```diff\n{c['diff_hunk'][-800:]}\n```\n{c['body']}\n")
        for c in d["issue_comments"]:
            L.append(f"### comment by {c['user']} at {c['created_at']}\n{c['body']}\n")
    L.append("## Commits (first = initial change; the rest were pushed after it)")
    for i, c in enumerate(d["commits"]):
        tag = "INITIAL" if i == 0 else f"FOLLOW-UP {i}"
        L.append(f"### [{tag}] {c['sha'][:8]} by {c['author']} at {c['date']}\n{c['message'].strip()}\n"
                 f"```\n{files(checkout, c['sha'])}\n```\n")
    return "\n".join(L)


def main():
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    checkouts = {"dexpace/morphic": REPO, "dexpace/styleguide": sys.argv[1]}
    OUT.mkdir(parents=True, exist_ok=True)
    sizes = {}
    for f in sorted(RAW.glob("*.json")):
        d = json.loads(f.read_text())
        text = render(d, checkouts[d["repo"]])
        name = f.stem + ".md"
        (OUT / name).write_text(text)
        sizes[name] = len(text)
    (OUT / "_sizes.json").write_text(json.dumps(sizes))
    print(f"{len(sizes)} dossiers, {sum(sizes.values()) // 1024} KB -> {OUT}")


if __name__ == "__main__":
    main()
