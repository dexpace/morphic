You are clustering verified review findings from the Morphic repo (Go spec-to-SDK compiler, author Omar Al Jarrah) into a rule catalog for a future code-review agent. Each finding is a defect that Omar's local reviews caught after a PR's first push (fix commits, his review comments, PR-body review notes). Quotes are verified verbatim.

Read CLAUDE_MD first. Then read your input file INPUT in full.

Task: group the findings into clusters, where each cluster is ONE reusable review rule. Merge findings that are the same rule in different words, even if they were tagged with different defect classes; split a "rule" that is really two different checks. Aim for clusters a reviewer can apply mechanically to a fresh diff -- not vague virtues. Every finding id must land in exactly one cluster (a cluster "misc-unclusterable" is allowed for genuine one-offs, but keep it small).

For each cluster write:
- "key": short kebab-case slug
- "title": one line, imperative
- "rule": 1-3 sentences, the general rule as a reviewer would apply it to any PR
- "why": 1-2 sentences on the failure it prevents, in this repo's terms
- "probe": the concrete check or experiment that would catch it BEFORE push (Omar's standard is execution, not reading: plant the mutation, swap the order, compile the spec and read the full diagnostic list, grep every site of the mechanism, measure)
- "claude_md": verbatim phrase from CLAUDE.md that already states this rule, or "" if none does. Must be an exact substring of CLAUDE.md; check it.
- "finding_ids": every finding id in the cluster
- "exemplars": 2-3 finding ids whose quotes best illustrate it

Do NOT write counts anywhere -- counts are derived from finding_ids by script.

Write exactly one file, OUTPUT, as JSON: {"group": "G", "clusters": [ ... ]}. Keep any helper scripts under SCRATCH, never in the repository. Then verify with a short script that the union of finding_ids equals the set of ids in INPUT, with no duplicates, and that each non-empty claude_md is a substring of CLAUDE.md (whitespace-normalised). Fix and re-verify until it passes. Do not touch GitHub, the network, or any other file. Reply with one line: output path and number of clusters.
