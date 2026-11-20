# Per-target milestone plan

Extends architecture.md §6 milestone 3 (the first emitters) and beyond.

1. **M3a — plan layer + the three first targets' skeletons.** `emitters/plan` with its
   language-independent golden corpus; `emitters/golang` and both Kotlin targets (`emitters/kotlin`)
   emitting models, enums, and one service client end-to-end. Proves the plan/refine/emit boundary
   and that the IR retains what a refiner needs (architecture.md §6 M3). Three targets at once make
   the plan's neutrality a test from the first model: a plan field only one of them can use fails
   the [plan-or-refine test](plan/plan-or-refine.md) at M3a, not when a later language lands. The
   [build check](verification.md#the-output-builds) lands with the first golden, here or on [the
   TypeScript track](#the-typescript-track), so every golden builds from the first one.
2. **M3b — the hard axes.** Unions (all four wire shapes), discriminators, visibility projections,
   the four optionality states, containers, external types, values, on each first target. Each is a
   refine step tied to a specific un-lowered IR fact — the acceptance test cashed.
3. **M3c — operations & runtime.** Pagination, streaming, LRO, error taxonomy, auth; the runtime
   policy tree, its layers and the reader of embedded policy, and the boilerplate templates; the
   wire-conformance harness; the behavior server, its workflows and its coverage gate (T-14).
4. **M3d — docs + mock emitters.** Prove reuse of `Doc + Plan` over a second and third target AST,
   and that docs/mock agree with the SDK by construction.
5. **M4 — write/integrate + surface verification.** Manifest, header-gated pruning, ignore regions,
   additive merge (`dst` for Go), regions that follow their entity, README blocks, staleness;
   neutral surface diff with injectable per-language severity; the [manifest's
   checks](verification.md#the-manifest-says-what-the-output-holds). It needs the [correlation
   key](../ir/derived-facts.md#correlation-keys), which shipped names, regions, files written once
   and unmatched keys all match on.
6. **M5 — a further language family (such as Python).** Proves the plan holds beyond the languages
   that read it while it was built: a new target is a new target AST + printer + refine pipeline
   reusing the shared plan and step contract, forcing **no** IR or plan change. TypeScript is not
   that family, since it reads the plan from [a track of its own](#the-typescript-track). The
   shared plan first pays back at M3, where three targets read it.

Beyond the milestones, a command-line client is a documented extension point rather than a
scheduled target: a registry entry over `Doc + Plan`, as docs and mocks are, whose inputs and flag
rules [docs and mocks](docs-and-mock.md#a-command-line-target) sets out. No milestone depends on it.

## The TypeScript track

A TypeScript target is built on a track of its own, beside M3a to M3c, and waits for neither Go nor
Kotlin. It shares the emitter contract, the registry and the plan with them. Its documents under
`targets/` come first ([how the documents divide](README.md#how-the-documents-divide)), and it then
follows the first targets' order: M3a's models, enums and one service client end to end, every
golden passing the [build check](verification.md#the-output-builds), then M3b's axes and M3c's
operations and runtime. [What M3 lowers](#what-m3-lowers) scopes it as it scopes them.

A target's cost is its own: a target AST, a printer, a refine pipeline and a toolchain in the gate.
M3a pays it for Go and for Kotlin together. This track pays it for one language, so a generated SDK
that builds, and the engine and the command line running an emitter end to end, do not wait for
three targets.

The track changes who may read the plan first, and so what tests the plan's neutrality. M3a's
three targets test it in code from the first model. A target reading a plan field before they do
is one reader, so until Go or Kotlin reads the field, the [plan-or-refine
test](plan/plan-or-refine.md) is held on paper, by [the rule it states for a lone
reader](plan/plan-or-refine.md#one-target-reading-the-plan). M5's claim is not asked of this track:
the plan is still being built while TypeScript reads it.

Two other orders were weighed:

- **TypeScript at M5, after the first targets.** The plan keeps three readers from its first model,
  and a TypeScript SDK waits behind M3 and M4.
- **TypeScript in place of the first targets.** The plan would have one reader and no second one
  scheduled, so nothing in code would test the line the plan-or-refine test draws.

## What M3 lowers

M3a to M3c lower the IR facts the compilers produce, and degrade the rest with a code. This scopes
what the first emitters implement, not what the IR models, which stays as designed (INV9). The
scope is derived, never kept by hand: `testdata/unwitnessed.golden.txt` lists every fact no
committed spec sets. A fact is a member of one of the IR's structs, the emptiness of a member whose
empty value differs from its absence (`Operation.auth`'s `[]`), or a value of one of its
enumerations, a `TypeKind` among them. `go test ./cmd/morphic-census` holds the file to a census of
`testdata/`, and `-update` rewrites it; `go run ./cmd/morphic-census <path>...` counts any specs.

- **A listed fact degrades with a code.** Its dispatch reports a coded
  `emitters/<target>/unsupported-…` diagnostic and takes the nearest lowering it can (T-7), never a
  silent fallback, so T-1's dispatch over every kind stays complete. Where an item of M3b or M3c
  above names listed facts, such as external types, pagination or LRO, the item is that degrade
  until a compiler produces them.
- **A fact enters when a compiler first produces it.** The change that makes a compiler produce one
  adds the conformance case that witnesses it, which takes its line out of the file in that change,
  and its lowering lands tested against that compiler's output. A fact only a non-default compiler
  option produces stays listed, since the census compiles every spec with the defaults.
- **Real specs check the corpus.** A fact a compiler produces belongs in `testdata/`, so a census of
  a real spec that sets a listed fact names a conformance case to add. The golden spec's come first:
  M3 must lower every fact it sets, and lowers none the corpus does not witness.

Compiled at morphic `af283ea1`, the golden spec (`cloudflare/api-schemas` `openapi.json` @
`843eae2f`) set eight listed facts: the `datetime_offset`, `float32`, `float64`, `int32`, `int64`
and `uint64` primitives, the `client` streaming mode and the `null` value. In outline, and the file
is the list, the rest were: the `external` kind; the `datetime`, `decimal`, `decimal128`,
`duration`, `float`, `int16`, `time`, `uint16` and `uint8` primitives; constructor, reference and
symbol values; every protocol but HTTP, and the auth schemes only those use; pagination,
long-running, one-way and overloaded operations, idempotency, resources and stream details;
versioning, from `Availability` to `Document.versions`; and facts no OpenAPI construct sets, such
as `Model.abstract`, `Property.presence`, `Enum.flags` and the `host` parameter location.
