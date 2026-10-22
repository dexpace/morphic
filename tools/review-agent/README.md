# Review agent

A Claude Code agent that reviews a morphic PR, branch or commit range against `CLAUDE.md`, the
dexpace styleguide, and a catalog of the defects this repository's PRs shipped and fixed after
their first push. It proves each finding by running something: compiling the spec a comment
talks about, planting a mutation, diffing two declaration orders. It writes a local report and
never posts to GitHub, pushes, or edits your checkout.

`report.html` explains how it was built and how it scored, and holds the whole catalog in a
browsable form. Open it in a browser.

## Install and run

```bash
tools/review-agent/install.sh
```

That copies the agent into `~/.claude/agents/` and the catalog into `~/.claude/morphic-review/`.
`.gitignore` keeps `.claude/` out of the repository, so the agent installs per user, not per
project. Rerun it after changing either file.

Then, in a Claude Code session in the morphic checkout:

```text
Use the morphic-reviewer agent to review PR #812
Use the morphic-reviewer agent to review branch fix/null-only-union
Use the morphic-reviewer agent to review 210283f..HEAD
```

The report lands in `${XDG_CACHE_HOME:-~/.cache}/morphic-review/<label>.md`. A review runs
`make gate` in its own worktree, so run one at a time.

The agent also serves as a [relevo](https://github.com/fuad-daoud/relevo) chain's reviewer. The
agent is installed where Claude Code reads it, so give it an actor whose candidates run Claude
Code, and name that actor with `--reviewer-actor`. Under relevo it writes the report
to the round's output path and ends with the verdict the chain reads. When the chain's check ran
`make gate`, it cites that result instead of running a second gate.

## What is here

| Path | What it is |
|---|---|
| `morphic-reviewer.md` | The agent: what to run, in what order, what counts as evidence, the report format. `install.sh` fills in `CATALOG_PATH`. |
| `catalog.md` | The rules the agent applies, grouped into passes. Generated; do not edit by hand. |
| `report.html` | The write-up, with the catalog spliced in. |
| `build/` | The scripts and committed data that produce `catalog.md`. |
| `bench/` | The blind back-test: briefs, ground truth, the agent's reviews and the grades. |

## How the catalog is built

Each step reads the previous one's output. The corpus is large and refetchable, so it lives in
`${MORPHIC_REVIEW_WORK:-${XDG_CACHE_HOME:-~/.cache}/morphic-review-build}`, outside the
repository. Steps 4 and 7 are agent steps: each prompt file goes to its own agent.

1. `build/fetch.py`: every PR Omar Al Jarrah opened in dexpace/morphic and dexpace/styleguide
   before the snapshot date in the script, with reviews, comments and commits. Needs `gh`.
2. `build/dossier.py <styleguide-checkout>`: one Markdown dossier per PR.
3. `build/batch.py`: picks the back-test hold-out PRs (`build/data/holdout.json`) and splits
   the rest into extraction batches (`build/data/batches.json`).
4. `build/prompts.py`: one extraction prompt per batch from `build/extract-brief.md`. Each
   agent writes the defects its PRs' fix commits and review comments caught, with a verbatim
   quote.
5. `build/verify.py`: keeps only findings whose quote is found verbatim in the dossier, numbers
   them, and writes `build/data/findings.json`.
6. `build/group.py`: splits the findings into five groups by defect class and writes one
   clustering prompt per group from `build/cluster-brief.md`.
7. Each clustering agent writes `build/data/clusters-<G>.json`: one cluster per reusable rule.
8. `build/catalog.py`: assembles `catalog.md`. The pass layout and which clusters merge are the
   one hand-written part, a table at the top of the script. The script fails if a cluster is
   left out or placed twice, or if a finding sits in no cluster or in two.
9. `build/catalog_html.py`: splices the catalog into `report.html`.

To change a rule's wording, edit its cluster in `build/data/clusters-*.json` and rerun steps 8
and 9. To move or merge rules, edit the table in `catalog.py`. Rerunning steps 1 to 7 rebuilds
everything from GitHub, but the agent steps will not reproduce the same clusters.

## Back-test

Each hold-out PR is reviewed at its first commit, blind to everything after it. A grader agent
then reads the commits pushed afterwards and decides which of them fixed a defect the first
commit already had. That list is the answer key.

```bash
bench/prep.py 183 212          # ground truth + the reviewer and grader prompts
# run each review, one at a time, then each grader with bench/grader-brief.md
bench/score.py                 # per-PR table and per-agent recall and false positives
```

Read each `truth-<PR>.md` before spending a review on it. A PR whose later commits continue the
work instead of fixing it gives nothing to measure. That happened with #564 and #136, so the
current agent's recall is still unmeasured.

The grades record which agent wrote each review. `v1` was the first draft, and its text was not
kept. `v2` added rules for what v1 missed on its four PRs: test a claim outside the case its
author measured, mutate each part of a built pointer separately, and treat a defect the change
makes worse as part of the change. `morphic-reviewer.md` is v2 plus two rules added after its
last back-test: read the PR title before a squash-subject finding, and duplication the package
layering forces is not a finding.
