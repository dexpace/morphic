You are extracting review lessons from pull-request history of the Morphic repo (a Go spec-to-SDK compiler; author Omar Al Jarrah, GitHub OmarAlJarrah) and its org styleguide. Goal: a catalog of the defects that Omar's reviews and fix commits caught, generalised into rules a future reviewer can apply BEFORE a PR is pushed.

Rules of engagement
- Read-only. Do NOT run gh, git push, curl, or anything that touches the network or GitHub. Do not edit any file except your one output file.
- Read every dossier listed below in full (use Read with offset/limit for large ones). Each dossier is one PR: its body, Omar's review comments (if any), and every commit. The INITIAL commit is the first change; FOLLOW-UP commits were pushed after it, mostly fixing defects found by local reviews.
- For repo rules, read CLAUDE_MD once before you start.

What counts as a finding
- A FOLLOW-UP commit that corrects something wrong in the PR's own earlier work (a bug, a test that did not test what it claimed, an inaccurate doc comment or doc, an overclaim, a missed site of the same mechanism, a hygiene problem).
- An inline/review/PR comment by OmarAlJarrah that identifies a defect.
- A PR body section that reports what a review found.
- Skip: merge commits, follow-ups that simply continue planned feature work, pure mechanical regenerations with no stated defect. If a commit fixes several distinct defects, emit one finding each.

Output: write exactly one file, OUTPUT_PATH, with the Write tool (not a script), containing a single JSON object:
{"batch": BATCH_NO, "prs_read": ["morphic-123", ...], "findings": [ {
  "repo": "morphic" | "styleguide",
  "pr": 123,
  "sha": "8-char sha or null",
  "source": "fix-commit" | "inline-comment" | "review" | "pr-comment" | "pr-body",
  "quote": "VERBATIM excerpt (<=300 chars) from the dossier that states the defect -- copy exactly, it will be machine-checked; never paraphrase inside this field",
  "defect": "one sentence: what was wrong, concretely",
  "defect_class": one of: silent-data-loss | order-dependence | id-collision | unbounded-resource | missing-diagnostic | wrong-diagnostic | spec-semantics | vacuous-test | unreachable-fixture | golden-blind-spot | test-asserts-wrong-thing | incomplete-sweep | doc-comment-inaccurate | prose-drift-or-count | overclaim | layering | api-shape | error-handling | naming | size-or-complexity | determinism | performance | concurrency | commit-or-pr-hygiene | other:<short label>,
  "rule": "a general imperative rule a reviewer can apply to ANY future PR (not specific to this PR's code)",
  "catch_how": "the concrete check/probe that would have caught it before push (e.g. 'swap declaration order and diff the IR', 'revert the fix and confirm the test reddens')",
  "covered_by": "claude-md" | "styleguide" | "archtest-or-lint" | "docs" | "none" | "unsure",
  "covered_ref": "if covered: short verbatim phrase of the existing rule; else empty",
  "severity": "correctness" | "data-loss" | "determinism" | "test-integrity" | "doc-accuracy" | "performance" | "process" | "style"
} ] }

Quality bar: precision over volume, but do not skip real findings. Every finding must have a verbatim quote. The rule must be reusable; "fix the $ref resolver" is not a rule, "when a lookup key is derived from a parsed URL, check every respelling maps to the same key" is. When done, reply with one line: the output path and the number of findings.

Dossiers for this batch (all under DOSSIER_DIR):
