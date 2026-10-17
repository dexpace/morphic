#!/usr/bin/env python3
"""Tabulate every grade-<PR>.json here. Recall counts only the defects the grader found in the
PR's later commits; the false-positive rate counts every finding the review made. A grade's
"agent" field names the agent version that wrote the review.

Usage: score.py"""
import collections
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
EXTRAS = ("valid-extra", "pre-existing", "false-positive", "unverifiable")


def main():
    by_agent = collections.defaultdict(collections.Counter)
    print(f"{'PR':>5} {'agent':>5} {'caught':>6} {'partial':>7} {'missed':>6} | "
          f"{'valid':>5} {'pre-ex':>6} {'FP':>3} {'unver':>5}")
    for f in sorted(HERE.glob("grade-*.json"), key=lambda p: int(p.stem.split("-")[1])):
        g = json.loads(f.read_text())
        d = collections.Counter(x["verdict"] for x in g["defects"])
        e = collections.Counter(x["class"] for x in g["extras"])
        agent = g.get("agent", "?")
        print(f"{'#' + str(g['pr']):>5} {agent:>5} {d['caught']:6d} {d['partial']:7d} {d['missed']:6d} | "
              f"{e['valid-extra']:5d} {e['pre-existing']:6d} {e['false-positive']:3d} {e['unverifiable']:5d}")
        by_agent[agent] += d + e
    for agent, t in sorted(by_agent.items()):
        defects = t["caught"] + t["partial"] + t["missed"]
        findings = t["caught"] + t["partial"] + sum(t[k] for k in EXTRAS)
        recall = (f"{t['caught']}/{defects} caught, {t['caught'] + t['partial']}/{defects} incl. partial"
                  if defects else "not measured (no later fixes)")
        print(f"{agent}: recall {recall}; false positives {t['false-positive']}/{findings} findings")


if __name__ == "__main__":
    main()
