# Micro-Compiler Architecture — Implementation Plan

The design is `docs/micro-compiler-design.md`; this is the order the work lands in and how the
pieces block each other. **Each unit of work is a GitHub issue carrying its own files, steps and
acceptance test** — that detail lives there rather than being repeated here, and the dependencies
below are recorded as real GitHub issue dependencies, not only as prose.

**Goal:** dissolve the `lowerer` god object into packages of pure functions over explicit positions,
and promote what every compiler must agree on into `compilers/compile`.

**Approach:** each lowering step becomes `f(ctx, ts, node, at) → (value, []ir.Diagnostic)` — an
immutable context by value, type interning as the single shared effect, diagnostics returned rather
than accumulated. Concerns become packages under `compilers/openapi/internal/` so the Go compiler
enforces the boundary. Behaviour-neutral by default, with structural fixes carved out explicitly.

## Global constraints

Every task inherits these. They are not restated per issue.

- **Golden output byte-identical**, except where an issue says otherwise. A golden diff is a
  stop-and-explain, never an `-update`.
- **Coverage stays at exactly 100%.** `./scripts/check-coverage.sh` counts statements from the
  profile; one uncovered statement fails the build.
- **The gate, in order:** `gofmt -l`, `go vet ./...`, `golangci-lint run`, `go build ./...`,
  `./scripts/check-coverage.sh`.
- **Every new package needs an `internal/archtest` rules entry**, or
  `TestImportGraph_EveryPackageIsRuledOrExempt` fails.
- **Every extracted package ships table-driven unit tests** built without calling `Compile` and
  without a document fixture. A package that cannot be tested that way has its boundary in the wrong
  place.
- **Every new guard is proven by planting a defect.** A check first observed passing has
  demonstrated nothing.
- **The compiler's public surface does not change:** `Compiler`, `New`, `Formats`, `Compile`,
  `Options`, `GroupingStrategy`.
- One logical change per PR; Conventional Commits; subject ≤72 characters including the ` (#NNN)`
  GitHub appends.

## Order of work

### Prerequisites — nothing else can start

| Issue | Work |
|---|---|
| ~~#57~~ | **Landed.** Allowlist entries are exact unless suffixed `/...`, so a `compilers` entry no longer licenses `compilers/graphql`, and a nested directory sharing a ruled sibling's basename is audited rather than skipped |
| ~~#48~~ | **Landed.** `* text=auto eol=lf`, so the comparison that proves twenty pull requests neutral cannot fail for reasons unrelated to the IR. A clone with `core.autocrlf=true` converted 346 files and failed three test functions before it |
| #159 | The general two-order oracle. Exists today only as three hand-written cases at the site where the pointer collision was found |
| #160 | Type-ID integrity: no collisions, **and** every ID agrees with its own provenance pointer. The second assertion is the only mechanical guard on provenance correctness — `irverify` validates the source index range and never the pointer |

#48 looked like hygiene and was not. Byte-identical goldens are the sole proof of neutrality across
every conformance snapshot, and a comparison that fails environmentally teaches a reader to dismiss
golden diffs at precisely the point where each one has to be treated as a finding. It blocked #162
and #165, the two entry points, so the ordering was inherited rather than remembered — and #162
landed with it, in that order, in one pull request.

### Framework promotion

| Issue | Work | Blocked by |
|---|---|---|
| ~~#162~~ | **Landed.** Identifier grammar into `compilers/compile`: `compile.TypeID` and friends over a `compile.Space`, with the minted-namespace rule refused by `compile.Types` | — |
| ~~#163~~ | **Landed.** Canonical naming grammar into `compilers/compile`, with `compile.NamingFor` beside it and a conformance suite pinning the boundaries `irverify` cannot see | — |
| ~~#164~~ | **Landed with #161**, ahead of #163: `ir/naming-not-words` rejects a lowercase but unsegmented canonical | — |
| #73 | Answered by the three above; to be closed with the reasoning that they landed in `compilers/compile` rather than in `ir` as its text proposed | — |

#163 changed no output here: #161 had already fixed the segmentation in `compilers/openapi` and
written it into `ir-design.md` §3.2, so the move was measured against a rule already in the
contract. The graphql and protobuf copies disagree with it — 8 of 13 spellings differ across the
three — so their goldens move as those drafts rebase, each with a reddening test and a deliberate
golden update.

### Tier 0 — extractions that are pure moves

Each is one PR. All wait on #57 and #48; the rest wait on `diag` because `diagf` is the single
diagnostic constructor and sits at the bottom of the import graph.

| Issue | Package | Blocked by |
|---|---|---|
| #165 | `internal/diag` | #57, #48 |
| #166 | `internal/load` | #57, #165 |
| #167 | `internal/scan` — cycles and alias amplification share one walk | #57, #165 |
| #168 | `internal/ids` — after the grammar moves, so no second copy lands | #57, #162 |
| #169 | `internal/value` | #57, #165 |
| #170 | `internal/annotation` — takes `site` with it, see below | #57, #165 |
| #171 | `internal/merge` | #57, #165 |

#170 is the one that does not move alone: `facets.go` reads a `site`, and `site`, `siteKind`,
`siteAt` and `siteSchema` are declared in `resolve.go` as free functions. They move together.

### Tier 1 — dissolving the god object

Strictly sequential: each conversion depends on the layer below it having moved.

| Issue | Work | Blocked by |
|---|---|---|
| #172 | `Ctx` with accessors; derive indexes at entry | all of Tier 0 |
| #173 | Convert the leaves — constraints, metadata, auth | #172, #159, #160 |
| #174 | `internal/resolve` | #173 |
| #175 | `internal/operation` | #174 |
| #176 | `internal/schema` — the recursive core; settles `hoist` and `diagnosedConstraints` | #175 |
| #177 | Remove the `lowerer` struct | #176 |

Both oracles gate the *first* Tier-1 conversion rather than the whole tier, so they are proven
against the old code before any of it moves.

### After the split

| Issue | Work | Blocked by |
|---|---|---|
| #178 | Cap methods per type in `internal/archtest` | #177 |
| #83 | Function-size and complexity caps in `golangci-lint` | #177 |
| #180 | Refresh the package layout and testing strategy in `architecture.md` and `CLAUDE.md` | #177 |

The caps land after the restructuring because they are calibrated against the finished shape;
landing them first would encode the current one. #178 exists because #83 alone would not have
prevented this: no function body in the package exceeds 50 lines against a 70-line cap, and the
failure was type surface, which nothing measured.

#180 matters more than routine doc upkeep. `CLAUDE.md` states the documents are the spec and are
read first, and both it and `architecture.md` §3 carry a package-layout tree that this work
invalidates. A layout diagram that no longer matches the tree actively misleads.

### Absorbed from the existing backlog

Work already filed that lands inside this restructuring rather than alongside it.

| Issue | Relationship | Blocked by |
|---|---|---|
| #86 | Hand-built provenance at 24 sites. Its proposed fix — a method on the lowerer — is invalidated by #177; the equivalent is a constructor in `internal/diag` taking the context. Until #160 lands this is the one change with no mechanical guard behind it; afterwards the ID-provenance agreement check covers the property and this issue removes the way to get it wrong in the first place | #172 |
| #84 | Eight copy-pasted reference-resolution helpers, in files that relocate. Sequenced after the move so the relocation and the collapse stay separate reviews | #175 |
| #87 | Mostly overtaken by #97 — the `*_edgecases_test.go` files are gone. The surviving concern, `newRawLowerer` hand-constructing its subject, resolves when there is nothing left to construct | — |

### Deferred, with reasons

| Issue | Status |
|---|---|
| #179 | Source index — blocked on `$ref` handling being correct first (#40, #141; #143 is closed) |
| ~~#161~~ | **Landed**, deliberately unblocked from #163: promotion would have fixed it, but it was a contract violation shipping today and did not wait on an architecture programme |
| #142 | The annotation matrix cannot address a carrier position. Recorded as a standing blind spot in the design §8.4; independently fixable |
| #54 | Cased `Naming.Hint` passes the neutrality check. #164 landed without reaching `Hint`, so this stays open — `neutral-naming.golden.json` shows one |
| #66 | Closed as superseded — its premise expired when the next compilers landed without it |
| #20, #21 | GraphQL and Protobuf drafts are read-only evidence here, not work items |

## Critical path

```
#57 ─┬─ #162 ─── #168 ──┐
#48 ─┤  #163 ─── #164   │                                          ┌─ #178
     └─ #165 ─┬─────────┼─ #172 ─ #173 ─ #174 ─ #175 ─ #176 ─ #177 ─┼─ #83
              ├─ #166 ──┤          │              │                 └─ #180
              ├─ #167 ──┤          └─ #86         └─ #84
              ├─ #169 ──┤
              ├─ #170 ──┤
              └─ #171 ──┘
#159, #160 ──────────────── #173
```

Twelve steps deep at its longest, with Tier 0 wide enough that six of its seven extractions can
proceed in parallel once `diag` lands.

Of the five that were unblocked at the start, #57, #161 and #48 have landed, and the framework
promotion (#162, #163) with them. **#159** and **#160** remain, and are the prerequisites the two
new oracles wait on. The graph is acyclic, and the dependency columns above are derived from the API
rather than maintained by hand — check both rather than trusting either:

```bash
gh api repos/dexpace/morphic/issues/N/dependencies/blocked_by --jq '[.[].number]'
```
