#!/usr/bin/env python3
"""Keep only the extracted findings whose quote appears verbatim in the PR's dossier, number
them, and write data/findings.json. A quote may elide with "..." or "…"; each fragment of 12
characters or more must match, whitespace and case normalised.

Usage: verify.py"""
import json
import re

from common import DATA, WORK


def norm(s):
    return re.sub(r"\s+", " ", s).strip().lower()


def main():
    dossiers = {}

    def dossier(repo, pr):
        k = f"{repo}-{pr}.md"
        if k not in dossiers:
            p = WORK / "dossiers" / k
            dossiers[k] = norm(p.read_text()) if p.exists() else None
        return dossiers[k]

    good, report, read = [], [], set()
    for f in sorted((WORK / "findings").glob("batch-*.json")):
        i = int(re.findall(r"\d+", f.name)[0])
        try:
            d = json.loads(f.read_text())
        except ValueError as e:
            report.append(f"{f}: bad JSON: {e}")
            continue
        read |= set(d.get("prs_read", []))
        misses = []
        for x in d.get("findings", []):
            text, quote = dossier(x.get("repo", "morphic"), x.get("pr")), norm(x.get("quote", ""))
            frags = [p for p in re.split(r"\s*(?:\.\.\.|…)\s*", quote) if len(p) >= 12]
            if text and frags and all(p in text for p in frags):
                x["batch"] = i
                good.append(x)
            else:
                misses.append(f"#{x.get('pr')} {x.get('quote', '')[:70]!r}")
        if misses:
            report.append(f"batch {i:02d}: {len(misses)} unverified" + "".join("\n    x " + m for m in misses[:4]))
    for n, x in enumerate(good, 1):
        x["id"] = f"F{n:04d}"
    (DATA / "findings.json").write_text(json.dumps(good, indent=1))
    want = {n[:-3] for b in json.loads((DATA / "batches.json").read_text()) for n in b}
    print("\n".join(report))
    print(f"{len(good)} verified -> {DATA / 'findings.json'}; PRs not read: {sorted(want - read) or '-'}")


if __name__ == "__main__":
    main()
