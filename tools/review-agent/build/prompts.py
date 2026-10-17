#!/usr/bin/env python3
"""Write one extraction prompt per batch, from extract-brief.md, into WORK/prompts. Each
prompt goes to its own agent; the agent writes WORK/findings/batch-NN.json.

Usage: prompts.py"""
import json

from common import DATA, HERE, REPO, WORK


def main():
    brief = (HERE / "extract-brief.md").read_text()
    out = WORK / "prompts"
    out.mkdir(parents=True, exist_ok=True)
    (WORK / "findings").mkdir(exist_ok=True)
    batches = json.loads((DATA / "batches.json").read_text())
    for i, names in enumerate(batches, 1):
        text = (brief.replace("CLAUDE_MD", str(REPO / "CLAUDE.md"))
                .replace("OUTPUT_PATH", str(WORK / "findings" / f"batch-{i:02d}.json"))
                .replace("BATCH_NO", str(i))
                .replace("DOSSIER_DIR", str(WORK / "dossiers")))
        (out / f"batch-{i:02d}.md").write_text(text.rstrip("\n") + "".join(f"\n- {n}" for n in names) + "\n")
    print(f"{len(batches)} prompts -> {out}")


if __name__ == "__main__":
    main()
