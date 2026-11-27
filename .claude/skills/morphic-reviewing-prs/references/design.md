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
- **Maintainable**: a maintained tool's feature before a hand-rolled mechanism
  (`.claude/skills/morphic-designing-code/references/design-rules.md#maintained-mechanism-first`).
- **Not a rejected simplification**: `docs/prior-art.md` records those that failed elsewhere.

1. Name what the change must deliver, and weigh it against its alternatives
   (`references/design.md#weigh-against-alternatives`).
2. Hold each new type, function and package to the rows of `morphic-designing-code`'s table it
   matches.
3. For a design document, take `references/design.md#review-design-rules`.
4. For a design document, also take `references/design.md#design-rule-names-its-end`,
   `references/design.md#wire-rule-through-every-library`,
   `references/design.md#toolchain-derived-names`,
   `references/design.md#refusal-has-a-witness` and
   `references/design.md#filesystem-rule-against-each-volume`.

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

## Give every documented refusal a witness and test built-in defaults against it
`refusal-has-a-witness`

- **Rule.** For each refusal a normative document states, (a) write an input that triggers it alone
  past every earlier guard, which the 100% coverage gate needs; and (b) run every built-in default,
  table entry and example the document gives through the refusal's exact predicate.
- **Why.** On #2267 (review head `a22080caa`), a refusal for a suffix that "folds empty" had no
  witness among 1,112,064 scalars, and `LeadingWord` defaults (`type`, `field`, `call`, `value`)
  failed `Start` (#2273).
- **Probe.** List each refusal's witness input and every built-in default's predicate verdict in
  the report.

## Cross filesystem rules against volume semantics and host APIs
`filesystem-rule-against-each-volume`

- **Rule.** Cross a document stating path or filesystem rules with volume semantics (case-folding
  or not; normalization-folding as APFS, or not, as ext4) and with the `go doc` of each filesystem
  API it names or relies on, against every step that calls it.
- **Why.** On #2267 (review head `a22080caa`), NFC/NFD names one directory on APFS and two on ext4,
  `os.Root` has `Remove` with no `Rmdir` and follows symlinks, and `renameio.WithRoot` places
  temporary files at the root (#2274).
- **Probe.** `go doc os.Root | grep -E 'Remove|Rmdir|follow symbolic links'`, and an NFC/NFD pair
  created on each supported volume with `os.SameFile`.

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

## Require an explicit end for any self-applying design rule
`design-rule-names-its-end`

- **Rule.** A design rule that applies itself to what it produces names what ends it. Feed its
  output back in: a node listed by what it composes, or a cycle of the construct through itself.
  Write that input as a minimal spec where no corpus holds one.
- **Why.** At `4662b1c94`, `docs/emitters/refine/models.md#inheritance` recursed without end on two
  valid specs (`Circle` inside `Shape`, mutual `U`/`U2`) that `compile --verify` exited 0 on
  (#1718, #1721).
- **Probe.** Read #1721's two specs against the rule's text; a rule naming no end is a finding.

## Send each special wire character class through every target's HTTP library
`wire-rule-through-every-library`

- **Rule.** For a document stating what an SDK sends, list the classes each location's grammar
  treats specially: each delimiter, leading and trailing whitespace, an empty element, `.` and `..`
  segments, and characters past ASCII. Send each through every first target's HTTP writer against a
  listener printing the raw request, holding each line to the document; for the receiving side,
  take `references/design.md#wire-rule-read-back`.
- **Why.** On #1743, a raw listener found four sending defects across Go, `fetch` and the JDK:
  trimmed header ends, `fetch` normalizing `..`, `?#` under `allowReserved`, and the JDK refusing
  `[` (#1757).
- **Probe.** Place the listener's lines beside the document's rules; a class with no line is
  unreached.

## Read each wire class back through every target's HTTP reader
`wire-rule-read-back`

- **Rule.** For a document stating what an SDK reads, send each special class (header-name case,
  `charset`, BOM, empty elements, multi-value headers) through every first target's HTTP server and
  client reader, holding each decoded value to the document. Read every section a rule cites whole,
  beside its neighbours.
- **Why.** On #1743 (`5db3e8bb4`), probing readers found three divergences (header-name case,
  ISO-8859-1 `charset`, UTF-8 BOM), and reading cited sections whole found split-after-decode and
  `Set-Cookie` joining (#1757).
- **Probe.** List each reader's output beside the document's rule and each cited section read
  whole.

## Include every name a target's toolchain derives in naming reviews
`toolchain-derived-names`

- **Rule.** A naming rule's consumers include each name a target's toolchain derives from the
  emitter's. Per declaration: Kotlin/Native's Objective-C and Swift exports, and JVM getters. Per
  source file: Kotlin's `<File>Kt` facade class, including files holding only a `typealias`.
- **Why.** #1758 missed `VALUE_1_0` and `VALUE_10` exporting as `value10` and `value10_` in Swift,
  and #1502 missed a `typealias` file generating a duplicate `TimestampKt` JVM class (#1766).
- **Probe.** Compile a declaration of each role holding the rule's case through each first target's
  toolchain and read what it exports (`<Name>.framework/Headers/<Name>.h`, `javap`, and the class
  files a unit's sources produce).
