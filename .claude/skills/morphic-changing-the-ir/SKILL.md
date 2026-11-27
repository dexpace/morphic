---
name: morphic-changing-the-ir
description: Use when changing anything under ir/ or pass/ (types, codecs, walks, irfacts, irverify, the passes), docs/ir or docs/emitters, the IRVersion constant, or a lowering that starts or stops filling an IR field or writes it a new value.
---

# Changing the IR

The IR is the ABI (`CLAUDE.md`, invariant 1): a change to it is a change to every stage that reads
it. `docs/ir/README.md` leads to what is normative for its shapes, `CLAUDE.md` gives the Go
representation they take, and architecture §2.5 says how a change lands.

## Before writing it

1. Find the recorded decision: `docs/ir/boundaries.md#deliberate-exclusions`,
   `docs/ir/open-questions.md`, and the tracker, closed issues included
   (`gh issue list --state all --limit 1000 --search "<field or type>"`).
2. Name each `CLAUDE.md` invariant the change touches, and check the shape against every format
   `docs/ir-spec-matrix.md` covers, not only OpenAPI.
3. Name the job a consumer does with the shape and the fact about real input it rests on, and
   count that fact (`references/ir-lessons.md#measure-the-premise`).
4. Weigh designs that differ in that fact, one of them outside the issue's framing
   (`references/ir-lessons.md#weigh-the-framing`), prototyping each emitter mechanism against its
   layer's requirements (`references/ir-lessons.md#prototype-against-every-requirement`), and write
   each loser's reason in `docs/ir` beside the winner.
5. Name the check that will hold it: an `irverify` rule, a codec test or a lint rule, or, for a rule
   in `docs/emitters`, a `T-<n>` in `docs/emitters/testing/mandatory-tests.md` the rule cites.

Passes: architecture §2.2.

## What changes with it

- **Version.** Bump `IRVersion` as `docs/ir/ir-version.md` says. `TestIRVersion_PinsTheEncodedShape`
  and `TestIRVersion_HistoryRecordsEachGeneration` catch a shape change, and
  `TestIRVersion_PinsTheNames` a renamed symbol. A change of meaning with no change of shape, such as
  a lowering that starts filling a member, or a hint moved at a position
  `testdata/golden/openapi/hint_positions.yaml` does not write, is the bump no test sees: make it,
  with the row's `meaning`. `git diff origin/main... -- testdata | grep -E '^-\s*"(canonical|hint)"'`
  lists the names a branch moved. A diagnostic shape change can also move `engine/report`'s
  `SchemaVersion` (`references/ir-lessons.md#report-schema-version`).
- **Other PRs.** After each merge moving a version, check no version repeats
  (`references/ir-lessons.md#version-collisions`).
- **Mentions.** A position only mentioning a type needs more than classifying
  (`references/ir-lessons.md#mention-is-no-use`).
- **Hand lists.** A new field, ID class or sum kind meets classification tests in `ir` and
  `ir/irverify`; `references/ir-lessons.md` says which list each meets.
- **Census.** Run `go test ./cmd/morphic-census -update` when a case sets a fact listed in
  `testdata/unwitnessed.golden.txt`.
- **Goldens.** A version bump rewrites none, but a new namespace in a compiler's `Namespaces`
  rewrites the `idSpaces` of every one (`references/ir-lessons.md#new-id-namespace`).
- **Values.** A lowering writing a value a field never held, one a consumer switches on, or
  withdrawing one, changes no shape, so no test sees it: bump for its meaning
  (`references/ir-lessons.md#widened-field-values`).
  A field naming another node bumps nothing (`references/ir-lessons.md#verdict-on-another-node`); a
  node changing kind at its ID bumps (`references/ir-lessons.md#kind-change-at-an-id-bumps`).
- **Emitters.** Once an emitter reads the field, architecture §2.5 says the change lands whole.
- **Breaking.** A version bump takes `!` and says what an older consumer sees
  (`docs/ir/ir-version.md`, `morphic-shipping-prs`).

## References

- `references/ir-lessons.md`: version bumps, hand lists, namespaces, invariants, field values,
  flattened members and emitter design rules.
- `references/verifier-and-codecs.md`: what the verifier, text predicates and codecs must reach.
