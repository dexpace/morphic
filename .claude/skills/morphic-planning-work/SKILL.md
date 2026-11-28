---
name: morphic-planning-work
description: "Use when scoping or designing a change that closes several issues or builds a whole feature, in one pull request or several (a tracking issue, a milestone, a handoff): cutting it into slices or pull requests, writing a design or brief that another session builds from, building parts in parallel and joining them, deciding where an issue found during that work goes, or updating a tracking issue's state."
---

# Planning large morphic changes

Most work here lands as one large pull request that closes several issues or builds a feature in
slices; some is spread over several pull requests at once. Each pull request stays `CLAUDE.md`'s
one logical change, one mechanism family, with no cap on its size, and a large one is reviewed
slice by slice instead. The rules are in `references/large-changes.md`, in the order of the
sections below.

## Any change that closes several issues

- **Before designing**, measure the family the issues belong to
  (`references/large-changes.md#measure-the-family-first`), read every consumer against every
  mechanism (`references/large-changes.md#sweep-consumers-before-recommending`), and follow what
  the tracker has decided
  (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#follow-recorded-decisions`).
- **The design** is written when its change is next, against a recorded base
  (`references/large-changes.md#design-against-its-base`), with every decision made in it
  (`references/large-changes.md#decisions-made-before-the-build`). It decides each fact once and
  implements each rule once across its spellings
  (`.claude/skills/morphic-designing-code/references/design-rules.md#decide-once`,
  `.claude/skills/morphic-designing-code/references/design-rules.md#one-rule-one-implementation`).
- **Handing work on** points at the design and caps every count
  (`references/large-changes.md#handoff-points-at-the-design`); a changed decision changes every
  record of it (`references/large-changes.md#changed-decision-updates-every-record`).
- **What the work finds** goes to its mechanism's owner with a number
  (`references/large-changes.md#found-issue-goes-to-its-mechanism`); a deferred finding is recorded
  by symbol (`references/large-changes.md#deferred-finding-by-symbol`). Scope settles by the second
  review round
  (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#scope-settled-by-the-second-round`).
- **A tracker** keeps its state in its body
  (`references/large-changes.md#tracker-state-in-its-body`), and each lesson it records goes to the
  skills before its state is updated (`morphic-curating-skills`).

## A change built in slices or in parallel parts

- Each slice is reviewed as it lands, the foundation first
  (`references/large-changes.md#review-each-slice-as-it-lands`).
- Parallel parts settle in the design what they share, and the join diffs it
  (`references/large-changes.md#parts-settle-what-they-share`).
- The join takes one `IRVersion` row for every part
  (`references/large-changes.md#one-version-row-at-the-join`).

## Several pull requests at once

- Each field, code, option and file two of them touch has one owner
  (`references/large-changes.md#one-owner-per-shared-fact`).
- Those touching one fact are reviewed on their trial merge, or become one
  (`references/large-changes.md#review-together-what-touches-one-fact`).
- No version is assigned ahead of the merge
  (`references/large-changes.md#no-version-ahead-of-the-merge`).

## References

- `references/large-changes.md`: scoping, designing, handing on, slicing and joining a change that
  closes several issues, and running several pull requests at once.
