# Morphic Architecture

Morphic turns any API specification — OpenAPI/Swagger, TypeSpec, Smithy, GraphQL, AsyncAPI,
Protobuf, Erlang/OTP module specs — into idiomatic SDKs and docs through a single spec-agnostic
intermediate representation (IR). This document defines the pipeline, the package layout, and the contracts
between stages. The IR itself is specified in [`ir-design.md`](./ir-design.md).

```
                 ┌────────────── frontends ──────────────┐
 OpenAPI 3.x ──▶ │                                       │
 Swagger 2.0 ──▶ │  parse → normalize → hoist → resolve  │──▶  IR document
 TypeSpec    ──▶ │      (per-format, isolated)           │      + diagnostics
 Smithy      ──▶ │                                       │
 GraphQL     ──▶ └───────────────────────────────────────┘
 AsyncAPI                          │
 Protobuf                          ▼
 Erlang/OTP
                        ┌── IR passes (IR → IR) ──┐
                        │  validate · link · dedup │
                        │  filter · version-slice  │
                        └──────────┬───────────────┘
                                   ▼
                 ┌────────────── backends ───────────────┐
                 │  plan (language-neutral decisions)     │
                 │  refine (per-language lowering)        │──▶  SDKs · docs · …
                 │  emit (templates/writers)              │
                 └───────────────────────────────────────┘
```

## 1. Design principles

1. **The IR is the ABI.** Frontends and backends never see each other. A frontend's only output
   is an IR document plus diagnostics; a backend's only input is an IR document plus its own
   options. Everything either side needs must round-trip through the IR (learned from oagen's
   layering and Kiota's refiner contract — see `prior-art.md`).
2. **Lossless by default, lowered late.** The IR is designed against the *union* of all source
   capabilities (`ir-spec-matrix.md`). Frontends never eagerly flatten (no allOf merging, no
   union-to-optional-fields collapse); lowering to what a target language can express happens in
   backend refiners, where the decision is reversible per target.
3. **Stable identity, names as presentation.** Every named IR entity has a synthetic stable ID
   derived from its source location. Names — even "canonical" ones — are metadata that passes and
   backends may rewrite; references never break when they do.
4. **Pure, reentrant stages.** Every stage is `f(input, options) → (output, diagnostics)` with no
   package-level mutable state. Frontends for different documents can run concurrently.
5. **Typed diagnostics with provenance.** No stage prints warnings; each emits `Diagnostic`
   values (severity, code, message, source location). The engine decides what is fatal.
6. **Heuristics are policy, not semantics.** Anything inferred rather than declared (pagination
   from parameter names, envelope detection, acronym casing) lives in injectable per-frontend or
   per-backend policy objects, is clearly marked `Inferred` in provenance, and can be disabled.
7. **The IR is serializable.** The full document round-trips through JSON. This enables golden
   snapshot tests, IR diffing between spec versions, caching, and out-of-process backends.

## 2. Pipeline stages

### 2.1 Frontends (spec → IR)

One frontend per source format. Each owns its format completely: file loading, reference
resolution, format-version normalization, and lowering into IR nodes.

Contract (conceptually — signatures are illustrative, not implementation):

```go
type Frontend interface {
    Formats() []SourceFormat                    // e.g. openapi@3.0, openapi@3.1
    Parse(ctx, sources, Options) (*ir.Document, []ir.Diagnostic, error)
}
```

Internal phases every frontend follows (each format implements them its own way):

1. **Load & bundle** — read all source files, resolve external references, produce one in-memory
   source document. Original pointers (file + JSON pointer / line-col) are preserved for
   provenance.
2. **Normalize within the format** — collapse format-version differences before IR construction:
   Swagger 2.0 is lifted to the OpenAPI 3.x shape (`body`/`formData` params → request body,
   `host`+`basePath` → servers); OpenAPI 3.0 `nullable` and 3.1 `type: [T, "null"]` both become
   the IR's nullable bit. The IR never records which dialect a fact came from except in
   provenance.
3. **Hoist & identify** — every anonymous inline type is hoisted into the type registry exactly
   once, keyed by its source pointer, with a naming *hint* (not a name) computed from context.
   This is a single pass; no other code derives inline names (oagen's duplicated-naming failure
   mode is designed out).
4. **Resolve & lower** — build IR nodes: type graph, services, operations, bindings, auth,
   channels. Declared semantics lower directly; inferred semantics (heuristic pagination
   detection, envelope unwrapping) run only if the corresponding policy is enabled and mark
   their output as inferred.

Frontends are registered in a registry keyed by detected format; the engine sniffs the source
format and dispatches. Milestone 1 ships the OpenAPI 3.x frontend only; the frontend registry,
provenance model, and IR are built for all eight from day one.

### 2.2 IR passes (IR → IR)

Small, composable, order-explicit transformations that both the engine and users (via config)
can enable:

- **validate** — referential integrity (every `TypeRef` targets a registered type), discriminator
  mappings point at actual variants, wire-name uniqueness within a model, binding completeness
  (every operation parameter is bound exactly once per binding). Structural errors here are
  fatal; style issues are warnings.
- **link** — resolve cross-document references when multiple specs are parsed into one document
  (multi-service, spec-stitching).
- **dedup** — structurally identical anonymous types are merged (by content hash), with ID
  aliases retained so provenance survives.
- **filter** — include/exclude operations and types by pattern (Kiota-style path filtering),
  followed by reachability trimming of orphaned types.
- **version-slice** — project a document carrying availability metadata into a concrete
  per-version snapshot (the TypeSpec versioning model: timeline stored, snapshot consumed).
- **overlay** — user-supplied IR patches (rename hints, pagination declarations for specs that
  can't express them, doc overrides) applied as data, not code.

Passes operate on the IR only; they know nothing about source formats or target languages.

### 2.3 Backends (IR → artifacts)

Backends are plugins (SDK-per-language, docs, mock servers…). Out of scope for this session
except for the boundary they impose on the IR; the internal shape mirrors what Kiota and oagen
converged on independently:

1. **Plan** — compute language-*neutral* per-operation and per-type decisions once
   (is-paginated, body presence, idempotency, error taxonomy) so templates contain no policy.
2. **Refine** — per-language lowering: reserved words, casing, union representation strategy
   (native union vs sealed interface vs wrapper class), enum strategy for open enums, interface
   extraction. Everything a refiner needs must exist un-lowered in the IR — this is the IR's
   acceptance test.
3. **Emit** — writers/templates produce files.

Language-specific naming (casing, reserved words, import layout) lives here exclusively. The IR
carries source names, wire names, and naming hints — never camelCase/PascalCase renderings.

### 2.4 Runtime/SDK policy is a separate input

Retry, timeout, telemetry, error-class taxonomy, user-agent — the *behavioral* configuration of
generated SDKs — is a backend input alongside the IR, not part of it. The IR describes the API;
policy describes the SDK. (oagen embeds both in one root; we keep the trees separate so the same
IR document can drive SDKs, docs, and mock servers without dragging SDK opinions along.)

## 3. Go package layout

```
morphic/
├── ir/                  # Layer 0 — IR node types, IDs, traversal, JSON round-trip.
│   └── irtest/          #           Golden-snapshot helpers for IR documents.
├── frontend/            # Layer 1 — frontend contract + registry.
│   ├── openapi/         #           OpenAPI 3.x → IR (milestone 1).
│   ├── swagger/         #           2.0 lift → openapi frontend (future).
│   ├── typespec/ smithy/ graphql/ asyncapi/ protobuf/ otp/   (future)
├── pass/                # Layer 1 — IR → IR passes (validate, dedup, filter, slice, overlay).
├── backend/             # Layer 2 — backend contract, plan layer, registry (future).
├── engine/              # Layer 3 — orchestration: sniff format, run frontend, passes, backends.
└── cmd/morphic/         # Layer 4 — CLI.
```

Dependency rules, enforced by an architecture test as in oagen:

- `ir` imports only the standard library. It contains no parsing, no generation, no I/O.
- `frontend/*` and `pass` import `ir` (and their own format libraries) — never each other,
  never `backend` or `engine`.
- `backend/*` imports `ir` and `backend` (contract) — never `frontend`.
- `engine` imports everything below it; `cmd` imports `engine`.

## 4. Diagnostics & provenance

Every IR node carries a `Provenance` (source format, file, JSON pointer or line/col, original
source name, and an `Inferred` marker naming the heuristic when applicable). Every stage returns
`[]Diagnostic{Severity, Code, Message, Provenance}`. Codes are stable strings
(`openapi/unresolved-ref`, `ir/dangling-type-ref`, `pass/discriminator-missing-variant`) so CI
can allowlist. Nothing in the pipeline writes to stderr; the CLI renders diagnostics.

## 5. Testing strategy

- **Golden IR snapshots**: each frontend has a corpus of specs; `spec → IR → JSON` is
  snapshot-compared. IR changes show up as reviewable diffs.
- **Capability conformance corpus**: one minimal spec per row of `ir-spec-matrix.md` per format
  that can express it, asserting the IR captures it losslessly. This is the regression net that
  keeps "lossless by default" honest as frontends are added.
- **Round-trip property**: `parse → serialize → deserialize → deep-equal` for every corpus
  document.
- **Architecture test**: import-graph assertions for the layering rules above.

## 6. Milestones

1. **IR + OpenAPI 3.x frontend** — `ir` package, validate pass, golden corpus, JSON round-trip.
2. **Swagger 2.0 lift** — normalization into the OpenAPI frontend; proves the
   format-version-normalization seam.
3. **First backend** — one language end-to-end; proves the plan/refine/emit boundary and that the
   IR retains everything a refiner needs.
4. **Second family frontend (TypeSpec or Smithy)** — proves the spec-agnostic claim: richer-than-
   OpenAPI concepts (interfaces, custom scalars, lifecycle visibility, declared pagination) flow
   through untouched IR code.
5. **Event-shaped frontend (AsyncAPI)** — proves channels/messages/bindings; then GraphQL,
   Protobuf, and Erlang/OTP (the actor-protocol frontend: behaviours → operations + channels).
