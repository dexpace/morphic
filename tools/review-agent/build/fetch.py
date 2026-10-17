#!/usr/bin/env python3
"""Fetch every PR Omar Al Jarrah opened in dexpace/morphic and dexpace/styleguide before the
snapshot, with its reviews, comments and commits, into WORK/raw. Rerunnable: PRs already
fetched are skipped. Needs an authenticated gh.

Usage: fetch.py"""
import json
import subprocess
from concurrent.futures import ThreadPoolExecutor

from common import WORK

AUTHOR = "OmarAlJarrah"
REPOS = ["dexpace/morphic", "dexpace/styleguide"]
# The corpus is frozen here so its counts mean something; move it to grow the corpus.
SNAPSHOT = "2026-10-14T14:00:00Z"
OUT = WORK / "raw"


def gh(path):
    r = subprocess.run(["gh", "api", "--paginate", "--slurp", path], capture_output=True, text=True)
    if r.returncode:
        raise RuntimeError(f"{path}: {r.stderr}")
    return [x for page in json.loads(r.stdout) for x in (page if isinstance(page, list) else [page])]


def login(user):
    return user["login"] if user else None


def fetch(repo, n):
    f = OUT / f"{repo.split('/')[1]}-{n}.json"
    if f.exists():
        return
    pr = gh(f"repos/{repo}/pulls/{n}")[0]
    d = {
        "repo": repo, "number": n, "title": pr["title"], "state": pr["state"],
        "merged_at": pr["merged_at"], "created_at": pr["created_at"], "body": pr["body"],
        "author": pr["user"]["login"], "base_sha": pr["base"]["sha"], "head_sha": pr["head"]["sha"],
        "reviews": [{"user": login(r["user"]), "state": r["state"], "body": r["body"],
                     "submitted_at": r.get("submitted_at"), "commit_id": r["commit_id"]}
                    for r in gh(f"repos/{repo}/pulls/{n}/reviews")],
        "review_comments": [{"id": c["id"], "user": login(c["user"]), "path": c["path"],
                             "line": c.get("line"), "diff_hunk": c["diff_hunk"], "body": c["body"],
                             "created_at": c["created_at"], "commit_id": c["commit_id"],
                             "in_reply_to": c.get("in_reply_to_id")}
                            for c in gh(f"repos/{repo}/pulls/{n}/comments")],
        "issue_comments": [{"user": login(c["user"]), "body": c["body"], "created_at": c["created_at"]}
                           for c in gh(f"repos/{repo}/issues/{n}/comments")],
        "commits": [{"sha": c["sha"], "message": c["commit"]["message"],
                     "date": c["commit"]["committer"]["date"],
                     "author": login(c["author"]) or c["commit"]["author"]["name"]}
                    for c in gh(f"repos/{repo}/pulls/{n}/commits")],
    }
    f.write_text(json.dumps(d, indent=1))


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    jobs = []
    for repo in REPOS:
        r = subprocess.run(["gh", "pr", "list", "-R", repo, "--author", AUTHOR, "--state", "all",
                            "--limit", "1000", "--json", "number,createdAt"],
                           capture_output=True, text=True, check=True)
        jobs += [(repo, p["number"]) for p in json.loads(r.stdout) if p["createdAt"] < SNAPSHOT]
    errors = []

    def run(job):
        try:
            fetch(*job)
        except Exception as e:  # report every failure at the end rather than stop at the first
            errors.append(str(e))

    with ThreadPoolExecutor(4) as ex:
        list(ex.map(run, jobs))
    print(f"{len(jobs)} PRs, {len(errors)} errors -> {OUT}")
    print("\n".join(errors[:5]))
    raise SystemExit(1 if errors else 0)


if __name__ == "__main__":
    main()
