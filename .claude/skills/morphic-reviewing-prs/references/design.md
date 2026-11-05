# Design

The angle that attacks the change's mechanism and pushes for a better one where one exists. It loads
`morphic-designing-code`, and `morphic-changing-the-ir` for the IR, a pass or a design document. Its
criteria are `morphic-designing-code`'s, weighed only among correct options:

- **Scales** with the document: each walk reads once per run
  (`.claude/skills/morphic-designing-code/references/design-rules.md#read-once-per-run`), and each
  limit bounds what the work grows with
  (`.claude/skills/morphic-designing-code/references/design-rules.md#bound-the-product`).
- **Extends** to the next format and emitter without a rewrite. Future-proof means fitting the work
  `docs/` and the tracker plan, never a seam with no user
  (`.claude/skills/morphic-designing-code/references/design-rules.md#second-user`).
- **Simplest**: functions of explicit inputs
  (`.claude/skills/morphic-designing-code/references/design-rules.md#functional-core`), no layer
  that only forwards
  (`.claude/skills/morphic-designing-code/references/design-rules.md#no-forwarding-layer`), and each
  fix at the owner of the invariant it restores
  (`.claude/skills/morphic-verifying-changes/references/sweep-every-site.md#fix-at-the-invariants-owner`).
- **Not a rejected simplification**: `docs/prior-art.md` records those that failed elsewhere.

1. Name what the change must deliver, and weigh it against its alternatives
   (`references/design.md#weigh-against-alternatives`).
2. Hold each new type, function and package to the rows of `morphic-designing-code`'s table it
   matches.
3. For a design document, take `references/design.md#review-design-rules`.

A finding attaches the better design's prototype and what it measured. One replacing the mechanism
leaves the round running, as the other angles' findings on the current code say what the
replacement must also get right. The matrix is each alternative with what it keeps, its cost and
the run measuring it.

## Weigh a change's design against the alternatives it gave up
`weigh-against-alternatives`

- **Rule.** Before judging a mechanism's details, name what the change must deliver (a defect
  fixed, sites covered, regressions guarded) and its alternatives: the issue's proposals, a
  narrower scope and no mechanism. Probe or prototype each on `main` for what it keeps, and weigh
  lines, scope, reliance on undocumented behaviour and the planned work it constrains
  (`gh issue list --state all --limit 1000 --search "<the mechanism's vocabulary>"`). One keeping
  as much at a measured lower cost is a finding, its prototype attached.
- **Why.** #1233's rounds checked a 479-line archtest rule's completeness (#1401) while the tree
  built commands in two forms and #704 plans code it would trip; none asked what its breadth bought.
- **Probe.** The report lists each alternative, what it keeps, its cost and the run measuring it;
  a mechanism weighed against none is unreviewed.

## Give a normative document's rules something that can disagree with them
`review-design-rules`

- **Rule.** With no output to compare, read each rule against every other's cases and a format
  it was not written for; run the rules as a program
  (`references/design.md#program-from-the-document`); check each premise
  (`.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#measure-the-premise`); and run
  each sample as printed under the document's own checks.
- **Why.** Names and citations were clean while a fallback overrode a gRPC stream's framing and a
  union of event shapes was misread, and `go vet` refused a sample that compiled (#1167, #1168).
- **Probe.** The report gives the matrix of `references/design.md#design-review-matrix`.

## Write a design document's rules as a program before reading the author's
`program-from-the-document`

- **Rule.** Implement every clause of the rules from the document alone, before reading the
  author's program, and read each fact `ir` or `ir/irfacts` derives (a flattening, exposed
  properties, an alias read) through its exported function, from a scratch module whose `go.mod`
  replaces this repository's module with the tree under review. Run both programs over compiled
  specs, a plant showing each reacts, and read each item they disagree on against the document.
- **Why.** Rerunning the author's program reproduced its count, and one written from the document
  did not (#1356). A flattening written by hand kept a member's first declaration and counted 2,039
  enveloped bodies leaving `result` optional or nullable, where `ir.FlattenedProperties` gives
  1,418 (#1356).
- **Probe.** The report names each item the programs disagree on, or says they agree on every one.

## Report a design review as a matrix of each rule's inputs and consumers
`design-review-matrix`

- **Rule.** Cross each rule's inputs
  (`.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#design-rule-over-its-document`)
  with its consumers: each target's codec and type-table requirements, output units, every
  artifact the plan feeds (the SDK and those `docs/emitters/docs-and-mock.md` names), lifecycles
  and directions, later documents and other formats. Every cell gets a result and its run, or why it
  cannot change the answer; one reason may settle a row or column, such as an input no consumer
  reads. Claim nothing is left only on a full matrix.
- **Why.** Each round of #1329's review after the first found defects a rule should have caught in
  the first, unlisted inputs and consumers' requirements among them, as reports checked a few
  cases per rule, asking nothing of the rest (#1402).
- **Probe.** Count the cells and those settled; a cell short is a round still open.
