You grade a blind code review against ground truth. Read-only: do not touch GitHub, the network, or any file except your output.

Inputs:
- REVIEW: a reviewer's report on the INITIAL commit of a dexpace/morphic PR. The reviewer saw nothing after that commit.
- TRUTH: every non-merge commit pushed to that PR after the initial one, with full messages. The repo is the checkout this brief sits in; you may run `git show <sha>` on any truth commit to see its diff.

Step 1 — build the defect list. For each truth commit decide whether it fixes a defect present in the initial commit (a bug, a test that did not test what it claimed, an inaccurate comment/doc, a missed site, a hygiene problem) or is something else (planned continuation, new scope, a fix for a defect introduced by a later commit, a reaction to main moving). Split a commit that fixes several distinct defects into several. Give each defect an id D1, D2… with a one-line description. Exclude non-defects, but list them with the reason.

Step 2 — match. For each defect, did the review report it? "caught" = the review identifies the same defect (same site or same mechanism, and the same wrongness), even if it proposes a different fix. "partial" = the review points at the right site or mechanism but misses what is actually wrong. "missed" otherwise. Quote the review's sentence for caught/partial.

Step 3 — the rest of the review. For each review finding not matched to a defect, classify: "valid-extra" (a real defect the truth never fixed — verify it against the code at the initial commit, by running it if you can), "pre-existing" (real but not introduced by this commit), "false-positive" (wrong, or a preference no rule backs), or "unverifiable". Be strict: the repo's bar is that a false positive costs a reviewer's time.

Machine load: run only package-scoped tests with bounded parallelism (on Linux, `taskset -c 0-3`), never `make gate` or `go test ./...`, and do any checkout in your own worktree under ${XDG_CACHE_HOME:-~/.cache}/morphic-grade/<pr> (remove it when done).

Write OUTPUT with the Write tool as JSON:
{"pr": N, "defects": [{"id","desc","truth_sha","verdict":"caught|partial|missed","review_quote"}], "non_defects": [{"truth_sha","reason"}],
 "extras": [{"finding","class":"valid-extra|pre-existing|false-positive|unverifiable","why"}],
 "missed_patterns": "2-4 sentences: what kind of thing the reviewer failed to look for, phrased as a change to its instructions"}
Do not compute rates; they are derived by script. Reply with the output path only.
