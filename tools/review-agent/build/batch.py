#!/usr/bin/env python3
"""Pick the back-test hold-out PRs and split the remaining dossiers into extraction batches of
about 100 KB each. Writes data/holdout.json and data/batches.json.

The hold-out is 20 merged morphic PRs with at least three fix/test/refactor follow-ups, spread
evenly by number. Rerunning on the same corpus gives the same split.

Usage: batch.py"""
import json
import re

from common import DATA, WORK

BATCH_BYTES = 100_000
HOLDOUT = 20


def fixes(d):
    return sum(1 for c in d["commits"][1:] if re.match(r"(fix|test|refactor)", c["message"]))


def main():
    sizes = json.loads((WORK / "dossiers" / "_sizes.json").read_text())
    prs = [json.loads(f.read_text()) for f in (WORK / "raw").glob("*.json")]
    cand = sorted((d for d in prs if d["repo"] == "dexpace/morphic" and d["merged_at"] and fixes(d) >= 3),
                  key=lambda d: d["number"])
    step = len(cand) / HOLDOUT
    hold = sorted(cand[int(i * step)]["number"] for i in range(HOLDOUT))
    held = {f"morphic-{n}.md" for n in hold}
    rest = sorted((n for n in sizes if n not in held),
                  key=lambda n: (n.split("-")[0], int(re.findall(r"\d+", n)[0])))
    batches, cur, size = [], [], 0
    for n in rest:
        if cur and size + sizes[n] > BATCH_BYTES:
            batches.append(cur)
            cur, size = [], 0
        cur.append(n)
        size += sizes[n]
    batches.append(cur)
    (DATA / "holdout.json").write_text(json.dumps(hold))
    (DATA / "batches.json").write_text(json.dumps(batches, indent=0))
    print("holdout", hold)
    print("batches", len(batches))


if __name__ == "__main__":
    main()
