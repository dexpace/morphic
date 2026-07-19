# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

Morphic is at the **design stage**. The repository currently contains only design documents
under `docs/` — there is no Go code, no `go.mod`, and no build/test tooling yet. Milestone 1
(the `ir` package + OpenAPI 3.x frontend) is the first code to be written. When you add code,
create the module (`go mod init`) and the package layout described in `docs/architecture.md §3`.

Standard Go tooling applies once code exists: `go build ./...`, `go test ./...`,
`go test ./ir -run TestName` for a single test, `go vet ./...`. None of these work today.

## What Morphic is

A spec-to-SDK compiler: any API spec (OpenAPI, Swagger 2.0, TypeSpec, Smithy, GraphQL, AsyncAPI,
Protobuf) → **one spec-agnostic intermediate representation (IR)** → idiomatic SDKs and docs.
Pipeline: **frontends** (spec → IR) → **IR passes** (IR → IR) → **backends** (IR → artifacts).

## The documents are the spec — read them first

- **`docs/ir-design.md` is normative.** Field names and struct shapes in it are the contract;
  receiver methods and helpers are not. When implementing the IR, match its shapes exactly.
- `docs/architecture.md` — pipeline stages, package layout, layering rules, milestones.
- `docs/ir-spec-matrix.md` — the union of source-format capabilities the IR is designed against.
- `docs/prior-art.md` — the evidence base (oagen, Kiota, TypeSpec/TCGC) and the specific mistakes
  each Morphic decision is designed to avoid. Read this before proposing IR changes; most
  "simplifications" that come to mind are failure modes already rejected here.

## Invariants that must not be violated

These are load-bearing design decisions, not preferences. Breaking one defeats the project's core
claim (lossless, spec-agnostic, many-target). Before changing any of them, re-read the rationale
in the docs.

1. **The IR is the ABI.** Frontends and backends never see each other. A frontend's only output is
   an IR document + diagnostics; a backend's only input is an IR document + its own options.
2. **Lossless by default, lowered late.** Frontends never flatten (no `allOf` merging, no
   union-to-optional-fields collapse, no primary-response selection). Composition, unions,
   visibility, discriminators, encodings, streaming stay in source-semantic form. Lowering to what
   a target language can express happens only in backend refiners. The one documented carve-out is
   validation-only JSON Schema (`not`/`if-then-else`/`dependentSchemas`), preserved verbatim in
   `Extensions` — see `ir-design.md §4.7`.
3. **Stable IDs; names are presentation.** Every named entity has a synthetic ID derived from its
   source pointer (never from a display name, never rewritten by renames). Entities live in flat,
   ID-keyed registries and reference each other by ID; no node embeds another named node.
4. **Names are neutral, never cased.** `Naming` stores source name + neutral canonical word
   sequence + wire name (+ numeric wire ID where applicable). The IR never stores camelCase /
   PascalCase — backends own all identifier rendering, casing, and reserved-word escaping.
5. **Pure, reentrant stages.** Every stage is `f(input, options) → (output, diagnostics)` with no
   package-level mutable state. No stage writes to stderr; each emits typed `Diagnostic` values
   (severity, stable string code, message, provenance). The engine/CLI decides what is fatal.
6. **Heuristics are policy, not semantics.** Anything inferred rather than declared (pagination
   from param names, envelope detection, acronym casing) lives in injectable per-frontend/backend
   policy, is marked `Inferred` in provenance, and can be disabled.
7. **Serializable & deterministic.** The whole `Document` round-trips through JSON (maps emitted in
   sorted-key order, slices in source order). This underpins golden snapshots, IR diffing, caching.
8. **Optionality ≠ nullability.** `Property.Required` (wire presence) is orthogonal to
   `TypeRef.Nullable` (this usage admits null). Both are needed for the four distinct states.
9. **The IR capability surface is complete from day one.** Only frontends are staged over time;
   shipping OpenAPI first must never force an IR schema change when later frontends land.
10. **SDK runtime policy is not IR.** Retry/timeout/telemetry/error-class taxonomy is a separate
    backend input. The IR describes the API; policy describes the SDK.

## Go representation conventions the design mandates

- **Closed sums = sealed interfaces**: unexported marker method (`typeDef()`), one concrete struct
  per kind, a `Kind()` accessor for switch-dispatch, and a generated switch-completeness test over
  the kind enum (the `assertNever` lesson). JSON encodes sums with an adjacent `kind` tag.
- **No `float64` anywhere in the IR.** Numeric values, defaults, and constraints use arbitrary-
  precision decimal strings (`BigVal`). This is a hard rule (the TypeSpec `Numeric` lesson).
- **Values are a separate channel from types** (`Value`/`ValueKind`), per the TypeSpec Type-vs-Value
  split. Defaults, consts, literals, enum values, examples are typed data, not type nodes.
- **Containers (list/map/tuple) are real type nodes with IDs**, hoisted like any anonymous type —
  never flags on a reference.

## Package layout & layering (enforced by an architecture test)

```
ir/          Layer 0 — IR nodes, IDs, traversal, JSON round-trip. Imports ONLY the stdlib.
frontend/*   Layer 1 — one frontend per format. Imports ir (+ own format libs); never each
             other, never backend/engine.
pass/        Layer 1 — IR → IR passes. Imports ir only.
backend/*    Layer 2 — imports ir + backend contract; never frontend.
engine/      Layer 3 — orchestration; imports everything below.
cmd/morphic/ Layer 4 — CLI; imports engine.
```

Write the import-graph assertion test alongside the first packages — it is part of the design, not
an afterthought.

## Testing strategy (build these as the code lands)

- **Golden IR snapshots**: `spec → IR → JSON` snapshot-compared per frontend corpus.
- **Capability conformance corpus**: one minimal spec per `ir-spec-matrix.md` row per format that
  can express it, asserting lossless capture. This is what keeps "lossless by default" honest.
- **Round-trip property**: `parse → serialize → deserialize → deep-equal` for every corpus doc.
- **Architecture test**: the import-graph assertions above.

## Repository rules

These match the conventions already in force across the other dexpace SDK repos
(`dexpace/java-sdk`, `dexpace/dexpace-react`) — kept consistent so contributors don't have to
context-switch between repos.

- Branch from `main`: `type/short-desc` (e.g. `feat/openapi-frontend-skeleton`,
  `fix/ir-nullable-defaulting`, `docs/architecture-milestone-2`).
- No CODEOWNERS, PR/issue templates, or CODE_OF_CONDUCT.md exist in this repo — don't assume them
  or invent content for files that aren't there.
- Once CI exists, keep it a small number of gating jobs (build, vet, test, the architecture
  import-graph test) required on PR and on push to `main`, rather than many fragmented workflows —
  both reference repos converge on one bundled "gate" job over granular per-check pipelines.

## Commits & pull requests

- **Conventional Commits**: `type(scope): subject`, imperative mood, subject line only (no period,
  ≤72 chars). Common types: `feat`, `fix`, `refactor`, `docs`, `test`, `build`, `chore`, `ci`,
  `perf`. Scope is the touched package (`ir`, `frontend/openapi`, `pass`, `backend/go`, `engine`)
  when it narrows things down — omit it when the change is repo-wide. The existing
  `chore: initial ir spec draft` commit already follows this.
- **Breaking changes** mark the type with `!` (`feat!:`, `refactor!:`) and explain the break in the
  commit body — don't bury it in the subject line alone.
- PRs are squash-merged, and GitHub appends the PR number (`(#NNN)`) to the squashed commit
  automatically — don't add it yourself.
- PR description: Summary / Test plan (/ Breaking, when applicable). Keep PRs scoped to one
  logical change; split unrelated changes into separate PRs.
- Write self-contained, human-framed titles/descriptions. No LLM/session artifacts, no internal
  audit/finding IDs, no "remediation"/"audit sweep" framing. State problem, change, and rationale
  on their own terms.
