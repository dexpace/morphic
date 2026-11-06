---
name: morphic-reviewing-prs
description: Use when asked to review a pull request, branch or commit range in this repository, before a branch's first push, right after opening a pull request here, or for a re-review of a PR's final state after new commits or a merge of main.
---

# Reviewing morphic pull requests

A review finds every flaw in its first round and confirms the fixes in its second. A round runs
every angle below, however much it finds first, deep into the change and broad over the code living
with it (`references/rounds.md#deep-and-broad`). Each angle loads the skills holding its rules and
fills a matrix, each cell with a run or a reason. Nothing is decided on an assumption
(`references/rounds.md#decide-on-evidence`), and a finding survives an attempt to disprove it
(`references/rounds.md#disprove-each-finding`). Post nothing unless asked, then by
`references/rounds.md#posted-findings`.

## Angles

| Angle | Attacks | Loads |
|---|---|---|
| `references/correctness.md` | each altered output, against a consumer's due result and `main` | `morphic-verifying-changes`, the domain skill |
| `references/fidelity.md` | each touched construct, at every position and spelling, for loss | the domain skill |
| `references/determinism.md` | each result, in both orders, at every limit, on every run | `morphic-verifying-changes` |
| `references/robustness.md` | each limit, foreign call and read, with hostile input | `morphic-designing-code`, `morphic-measuring-performance` |
| `references/contract.md` | the IR, codes, CLI and Go API consumers read | `morphic-changing-the-ir`, `morphic-shipping-prs` |
| `references/design.md` | the mechanism, against the simplest correct one that scales and extends | `morphic-designing-code`, `morphic-changing-the-ir` |
| `references/reuse.md` | each addition, against what the tree or a maintained library could own | `morphic-designing-code` |
| `references/testing.md` | each changed function by input class, each test by planted defect | `morphic-verifying-changes` |
| `references/performance.md` | new work's growth and cost against `main` | `morphic-measuring-performance` |
| `references/developer-experience.md` | names, placement, comments, messages | `morphic-checking-style` |
| `references/consistency.md` | each construct and name, against the codebase's own | `morphic-checking-style`, `morphic-designing-code` |
| `references/claims-and-process.md` | every sentence the change writes, and how it ships | `morphic-shipping-prs` |

Run them in order, in one reader unless split or asked to fan out
(`references/readers.md#fan-out-an-angle-to-a-reader`); one the change cannot reach is settled
unread, in a line saying why.

## Rounds

1. **Setup**: the merged tree in a throwaway worktree, as `references/rounds.md` opens.
2. **Gate**: CI's run (`references/rounds.md#ci-is-the-gate`).
3. **Angles**: the whole change in the first two rounds
   (`references/rounds.md#first-round-finds-everything`), then what moved
   (`references/rounds.md#later-rounds-read-what-moved`).
4. **Standing reviews**, after the first round's angles
   (`references/rounds.md#read-standing-reviews`).
5. **Fixes**, when the review fixes (its own PR, or when asked): each designed first
   (`references/rounds.md#design-the-fix-first`) and reviewed before it leaves
   (`references/rounds.md#review-own-fixes`).
6. **Next round**, from the setup, when the branch, its base or `origin/main` moves
   (`references/rounds.md#recheck-against-final-tree`), until
   `references/rounds.md#review-ends-with-nothing-left`.

## Findings

Rank each finding `issue:`, `nit:` (a defect breaking nothing), `suggestion:` (no defect) or
`question:` (`references/rounds.md#rank-by-what-was-shown`). Every `issue:` and `nit:` is fixed or
justified in writing before merge. Each names its angle, the rule it breaks, file and line, the run
and its output, and the smallest fix removing the class, not the instance. A defect the change
introduces, exposes or makes reachable is in scope however old its root; a stated limitation is an
`issue:` when a valid input shows head worse than base. The report gives each angle's matrix and
each equivalent mutant's reason.
