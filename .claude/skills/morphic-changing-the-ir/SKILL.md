---
name: morphic-changing-the-ir
description: Use when changing anything under ir/ or pass/ (types, codecs, walks, irfacts, irverify, the passes), docs/ir-design.md or docs/emitter-design.md, the IRVersion constant, or a lowering that starts or stops filling an IR field.
---

# Changing the IR

The IR is the ABI (`CLAUDE.md`, invariant 1): a change to it is a change to every stage that reads
it. `docs/ir-design.md` is normative for its shapes, `CLAUDE.md` gives the Go representation they
take, and architecture §2.5 says how a change lands.

## Before writing it

1. Find the recorded decision: ir-design §15 (deliberate exclusions) and ir-design §16 (open
   questions), and the tracker, closed issues included
   (`gh issue list --state all --limit 1000 --search "<field or type>"`).
2. Name each `CLAUDE.md` invariant the change touches, and check the shape against every format
   `docs/ir-spec-matrix.md` covers, not only OpenAPI.
3. Name the job a consumer does with the shape and the fact about real input it rests on, and
   count that fact (`references/ir-lessons.md#measure-the-premise`).
4. Weigh designs that differ in that fact, one of them outside the issue's framing
   (`references/ir-lessons.md#weigh-the-framing`), and write each loser's reason in ir-design beside
   the winner.
5. Name the check that will hold it: an `irverify` rule, a codec test or a lint rule.

Passes follow architecture §2.2: where validate runs, how a pass takes settings, and which passes
cannot be turned off.

## What changes with it

- **Version.** Bump `IRVersion` as ir-design §2.1 says. `TestIRVersion_PinsTheEncodedShape` and
  `TestIRVersion_HistoryRecordsEachGeneration` catch a shape change. A change of meaning with no
  change of shape, such as a lowering that starts filling a member, is the bump no test sees: make
  it, with the row's `meaning`.
- **Other PRs.** Before claiming a version, and after each merge of `main`, list the open PRs
  that claim one (`references/ir-lessons.md#version-collisions`).
- **Hand lists.** A new field, ID class or sum kind meets classification tests in `ir` and
  `ir/irverify`; `references/ir-lessons.md` says which test reaches each, and which one lets a stale
  entry for a deleted field pass.
- **Census.** A fact no committed spec sets is a line of `testdata/unwitnessed.golden.txt`, which
  `go test ./cmd/morphic-census -update` rewrites; a case that sets the fact takes its line out.
- **Emitters.** Once an emitter reads the field, architecture §2.5 says the change lands whole.
- **Breaking.** A version bump breaks every consumer, as ir-design §2.1 says: the title carries
  `!`, and the commit and PR bodies say what an older consumer sees (`morphic-shipping-prs`).

## References

- `references/ir-lessons.md`: version collisions, the hand lists a new field meets, invariants a
  type held, design rules over their own document, and what the verifier and codecs must reach.
