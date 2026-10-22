#!/usr/bin/env python3
"""Split the verified findings into five groups by defect class and write one clustering
prompt per group, from cluster-brief.md, into WORK/synthesis. Each prompt goes to its own
agent, which writes data/clusters-<G>.json.

Usage: group.py"""
import json

from common import DATA, HERE, REPO, WORK

GROUPS = {
    "A": "doc-comment-inaccurate prose-drift-or-count overclaim naming commit-or-pr-hygiene",
    "B": "vacuous-test test-asserts-wrong-thing golden-blind-spot unreachable-fixture",
    "C": "incomplete-sweep silent-data-loss missing-diagnostic wrong-diagnostic",
    "D": "spec-semantics order-dependence determinism id-collision performance unbounded-resource "
         "error-handling api-shape layering size-or-complexity concurrency",
}
FIELDS = ["id", "repo", "pr", "sha", "source", "quote", "defect", "defect_class", "rule", "catch_how",
          "covered_by", "covered_ref", "severity"]


def main():
    findings = json.loads((DATA / "findings.json").read_text())
    brief = (HERE / "cluster-brief.md").read_text()
    out = WORK / "synthesis"
    out.mkdir(parents=True, exist_ok=True)
    groups = {g: [] for g in "ABCDE"}
    for x in findings:
        cls = x["defect_class"].split(":")[0]
        g = next((g for g, classes in GROUPS.items() if cls in classes.split()), "E")
        groups[g].append({k: x[k] for k in FIELDS if k in x})
    for g, items in groups.items():
        src = out / f"in-{g}.json"
        src.write_text(json.dumps(items, indent=1))
        prompt = (brief.replace("CLAUDE_MD", str(REPO / "CLAUDE.md"))
                  .replace("INPUT", str(src))
                  .replace("SCRATCH", str(out))
                  .replace('"group": "G"', f'"group": "{g}"')
                  .replace("OUTPUT", str(DATA / f"clusters-{g}.json")))
        (out / f"brief-{g}.md").write_text(prompt)
        print(g, len(items))


if __name__ == "__main__":
    main()
