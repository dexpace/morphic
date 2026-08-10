# Morphic Architecture

Morphic turns any API specification — OpenAPI/Swagger, TypeSpec, Smithy, GraphQL, AsyncAPI,
Protobuf, Erlang/OTP module specs — into idiomatic SDKs and docs through a single spec-agnostic
intermediate representation (IR). This document defines the pipeline, the package layout, and the contracts
between stages. The IR itself is specified in [`ir-design.md`](./ir-design.md).

```
                 ┌────────────── compilers ──────────────┐
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
                 ┌────────────── emitters ───────────────┐
                 │  plan (language-neutral decisions)     │
                 │  refine (per-language lowering)        │──▶  SDKs · docs · …
                 │  emit (templates/writers)              │
                 └───────────────────────────────────────┘
```

## 1. Design principles

1. **The IR is the ABI.** Compilers and emitters never see each other. A compiler's only output
   is an IR document plus diagnostics; a emitter's only input is an IR document plus its own
   options. Everything either side needs must round-trip through the IR (learned from oagen's
   layering and Kiota's refiner contract — see `prior-art.md`).
2. **Lossless by default, lowered late.** The IR is designed against the *union* of all source
   capabilities (`ir-spec-matrix.md`). Compilers never eagerly flatten (no allOf merging, no
   union-to-optional-fields collapse); lowering to what a target language can express happens in
   emitter refiners, where the decision is reversible per target.
3. **Stable identity, names as presentation.** Every named IR entity has a synthetic stable ID
   derived from its source location. Names — even "canonical" ones — are metadata that passes and
   emitters may rewrite; references never break when they do.
4. **Pure, reentrant stages.** Every stage is `f(input, options) → (output, diagnostics)` with no
   package-level mutable state. Compilers for different documents can run concurrently.
5. **Typed diagnostics with provenance.** No stage prints warnings; each emits `Diagnostic`
   values (severity, code, message, source location). The engine decides what is fatal.
6. **Heuristics are policy, not semantics.** Anything inferred rather than declared (pagination
   from parameter names, envelope detection, acronym casing) lives in injectable per-compiler or
   per-emitter policy objects, is clearly marked `Inferred` in provenance, and can be disabled.
7. **The IR is serializable.** The full document round-trips through JSON. This enables golden
   snapshot tests, IR diffing between spec versions, caching, and out-of-process emitters.

## 2. Pipeline stages

### 2.1 Compilers (spec → IR)

One compiler per source format. Each owns its format completely: file loading, reference
resolution, format-version normalization, and lowering into IR nodes.

Contract (conceptually — signatures are illustrative, not implementation):

```go
type Compiler interface {
    Formats() []SourceFormat                    // e.g. openapi@3.0, openapi@3.1
    Parse(ctx, sources, Options) (*ir.Document, []ir.Diagnostic, error)
}
```

Internal phases every compiler follows (each format implements them its own way):

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

What a compiler does *not* own is in `compilers/compile`: the state and the grammars every compiler
must agree on. That is the type registry with the source-coordinate map behind stable IDs (and the
rule that a minted node takes a namespace no source coordinate addresses), diagnostic accumulation,
the canonical naming grammar, and the identifier grammar — the kind prefix and the namespace after
it. What stays with the compiler is what only it can compute: the path an ID derives from, since a
JSON Pointer, a GraphQL structural path and a protobuf fully-qualified name are different things.

The split is enforced rather than documented: architecture tests fail a package outside the
framework that writes the type registry, derives a canonical name, or builds an ID out of a string.
Promoting something into the framework later is additive, while demoting it breaks every compiler,
so borderline machinery starts outside and moves in on evidence from more than one format.

A compiler is also bounded in what one compile may cost. Most bounds are constants beside the
walk they bound (schema nesting depth, alias expansion, reference-chain length), because nothing
about them is a caller's to choose. The bounds on the *input* are, so they are options:
`openapi.Options.Limits` carries a byte budget and a parsed-node budget for one source document
and a member budget for one enum, each with a documented default and each settable — zero takes
the default, negative turns the budget off. Crossing one is a spec problem like any other, so it
is an `openapi/budget-exceeded` diagnostic rather than a Go error. Time is bounded by the caller
instead of by a constant: the two walks that do work proportional to the document honour the
`context.Context` a compile is given, so a deadline or a cancellation stops one between items and
the error reaches the caller unwrapped.

Compilers are registered in a registry keyed by the formats they report, and detection belongs to
them too: the registry asks each compiler in registration order whether it recognizes a source, and
the engine dispatches to the one that does. A compiler also decodes its own textual options, so
registering one is the whole of adding a format — no layer above names any of them. Milestone 1
ships the OpenAPI 3.x compiler only; the compiler registry, provenance model, and IR are built for
all eight from day one.

### 2.2 IR passes (IR → IR)

Small, composable, order-explicit transformations that both the engine and users (via config)
can enable:

- **validate** — referential integrity (every typed-ID reference resolves to something the document
  declares: a `TypeRef` target against the type registry, an `OpID` against the operations the
  service tree declares, and so on for every ID class), discriminator mappings point at actual
  variants, wire-name uniqueness within a model, binding completeness (every operation parameter
  is bound exactly once per binding). Structural errors here are fatal; style issues are warnings.
- **link** — resolve cross-document references when multiple specs are parsed into one document
  (multi-service, spec-stitching).
- **dedup** — structurally identical anonymous types are merged (by content hash), with ID
  aliases retained so provenance survives.
- **filter** — include/exclude operations and types by pattern (Kiota-style path filtering),
  followed by reachability trimming of orphaned types. Filtering serves *surface reduction*
  (a smaller SDK). Regenerating only one service of an existing SDK is a different problem and
  never uses a filtered document: global decisions (dedup, shared files, naming) must stay
  byte-identical across scoped runs, so the emitter consumes the full document plus a scope
  option and gates emission itself (a lesson oagen recorded after trying the filtered route).
- **version-slice** — project a document carrying availability metadata into a concrete
  per-version snapshot (the TypeSpec versioning model: timeline stored, snapshot consumed).
- **overlay** — user-supplied IR patches (rename hints, pagination declarations for specs that
  can't express them, doc overrides) applied as data, not code. IR overlays are language-neutral
  and each entry carries provenance (user-authored vs tool-inferred, mirroring the `Inferred`
  marker) so automated overlay-generation loops stay auditable. Two related hooks live
  deliberately elsewhere: *source-document* patching (e.g. the OpenAPI Overlay spec) is a
  compiler option applied before lowering — some fixes must land before naming/hoisting
  heuristics consume the broken shape — and *per-target-language* naming/compat overlays are a
  emitter input keyed by IR ID, so one IR document drives different compat baselines per
  language.

  The first of those is `openapi.Options.Overlay`: pre-read bytes, applied to the parsed node
  tree before the model is built, so untouched nodes keep the line and column the parser read
  them at rather than a position in a re-serialised file nobody has. The overlay becomes a
  second `Document.Sources` entry, and every position it introduced or rewrote names that entry
  as its `Provenance.Source`. Strict application is the default: an action whose JSONPath
  matches nothing is reported and the compile refuses, because an overlay that silently does
  nothing ships an SDK missing the fix it was written to make.

Passes operate on the IR only; they know nothing about source formats or target languages.

### 2.3 Emitters (IR → artifacts)

Emitters are plugins (SDK-per-language, docs, mock servers…). Out of scope for this session
except for the boundary they impose on the IR; the internal shape mirrors what Kiota and oagen
converged on independently:

1. **Plan** — compute language-*neutral* per-operation and per-type decisions once
   (is-paginated, body presence, idempotency, error taxonomy, primary-content/response
   selection, return-shape classification (model / void / list-of-models with elementwise
   deserialization), pagination item-type unwrap, parameter-passing shape (positional vs
   options-bag)) so templates contain no policy. This list is the decision set oagen's
   emitters demonstrably needed, computed once and shared across all languages.
2. **Refine** — per-language lowering: reserved words, casing, union representation strategy
   (native union vs sealed interface vs wrapper class), enum strategy for open enums, interface
   extraction. Everything a refiner needs must exist un-lowered in the IR — this is the IR's
   acceptance test.
3. **Emit** — writers/templates produce files.

Language-specific naming (casing, reserved words, import layout) lives here exclusively. The IR
carries source names, wire names, and naming hints — never camelCase/PascalCase renderings.

Two further emitter-side stages are named here so their obligations shape the contracts, even
though both are post-milestone scope:

- **Write/integrate** — regenerating into a live repo alongside hand-written code requires:
  deterministic file paths, a generation manifest (spec/emitter/config hashes, sorted file
  list, and an entity → generated-symbol map **keyed by IR stable IDs**, which survive path
  and name churn), file-header provenance that gates pruning (never delete a file lacking the
  generated-by header), ignore-region markers for hand-written islands, and additive-only
  merging. Additive-only writers also need a staleness check — (previous-revision entities −
  current entities) ∩ files-on-disk — to surface dead code that pruning cannot touch.
- **Surface verification** — per-language extractors project existing SDK source into a
  neutral API-surface model; the same projection of generated output is diffed against it.
  Change records are *neutral*; breaking/additive severity is an injectable per-language
  policy function (parameter names are public API in PHP, arity in Go, almost nothing in
  JS), consistent with principle 6. Behavioral changes (defaults) are a separate channel
  from structural ones. Emitters propagate IR IDs into manifests and reports so findings
  correlate across languages without name-matching heuristics.

### 2.4 Runtime/SDK policy is a separate input

Retry, timeout, telemetry, error-class taxonomy, user-agent — the *behavioral* configuration of
generated SDKs — is a emitter input alongside the IR, not part of it. The IR describes the API;
policy describes the SDK. (oagen embeds both in one root; we keep the trees separate so the same
IR document can drive SDKs, docs, and mock servers without dragging SDK opinions along.)

The canonical policy-input vocabulary, taken from oagen's production `SdkBehavior` (the best
real-world enumeration available): **retry** (retryable status codes, max attempts, full backoff
strategy with jitter), **timeouts** (defaults + env override), **error taxonomy** (status →
logical exception-kind map, client/server catch-alls, doc-URL template), **telemetry** (request-
ID and client-telemetry headers), **logging** (a closed lifecycle-event list), **user-agent**
construction (identifier template, app-info enrichment), **idempotency injection** (header name,
auto-generate rules), **pagination pacing** (auto-page delay), and **request guards** (option
keys that must not appear as params — misuse detection). Delivery model: canonical defaults +
deep-partial per-emitters/per-project overrides. Precedence rule: *declared IR facts win over
policy defaults* — `ErrorCase.Retryable/Fault` and `Operation.Idempotency` come from the spec;
policy fills in where the spec is silent, never the reverse.

## 3. Go package layout

```
morphic/
├── ir/                     # Layer 0 — IR node types, IDs, traversal, JSON round-trip.
│   ├── irtest/             #           Golden-snapshot helpers for IR documents.
│   └── irverify/           #           Structural-invariant oracle (dangling refs, IDs, naming).
├── compilers/              # Layer 1 — compiler contract + registry.
│   ├── compile/            #           What every compiler shares: type registry + coordinates,
│   │                       #           diagnostics, and the naming/identifier grammars.
│   ├── openapi/            #           OpenAPI 3.x → IR (milestone 1). The public face: the
│   │   │                   #           Compiler, its Options, document metadata, and the run that
│   │   │                   #           calls the four lowerings below in order.
│   │   └── internal/       #             Its own packages, each below that face:
│   │       ├── diag/       #             diagnostic codes and the constructor
│   │       ├── ids/        #             pointer arithmetic; pointer → TypeID
│   │       ├── value/      #             scalar and BigVal lowering
│   │       ├── nodeview/   #             the raw source as the resolver reads it
│   │       ├── scan/       #             pre-lowering refusals (ref cycles, alias fan-out)
│   │       ├── annotation/ #             what a schema says about itself, not its shape
│   │       ├── load/       #             parse, validate, resolve
│   │       ├── resolve/    #             what a $ref names; reference-or-inline entries
│   │       ├── merge/      #             allOf property reconciliation
│   │       ├── lowering/   #             the immutable context every lowering reads
│   │       ├── schema/     #             the schema walk, composition, preservation
│   │       ├── auth/       #             security schemes and requirements
│   │       └── operation/  #             path items, webhooks, callbacks, params, content
│   ├── typespec/ smithy/ graphql/ asyncapi/ protobuf/ otp/   (future)
├── pass/                   # Layer 1 — IR → IR passes (validate, dedup, filter, slice, overlay).
├── emitters/               # Layer 2 — emitter contract, plan layer, registry (future).
├── engine/                 # Layer 3 — orchestration: detect format, run compiler, passes, emitters.
├── internal/archtest/      #           Layering, grammar, recursion and method-cap rules (tooling).
├── internal/harness/       #           Bug-catching oracle sweep over a spec corpus (tooling).
├── internal/testspec/      #           Spec fixtures shared by the tooling (tooling).
├── cmd/morphic/            # Layer 4 — CLI.
└── cmd/morphic-harness/    #           CLI over internal/harness (tooling, not the pipeline).
```

Swagger 2.0 gets no directory of its own: the lift normalizes it to the OpenAPI 3.x shape and
belongs *inside* that compiler (§2.1, milestone 2). A sibling `compilers/swagger/` calling into
`compilers/openapi` is the one import the rules below forbid outright.

This tree is prose. `internal/archtest`'s `rules` map is the source of truth, and it is what fails
when the two disagree — every production package must appear there or in `exempt`. To read the
layout off the tree rather than off this page:

```bash
git ls-files '*/*.go' | xargs -n1 dirname | sort -u
```

Dependency rules, enforced by an architecture test as in oagen:

- `ir` imports only the standard library. It contains no parsing, no generation, no I/O.
- `compilers/compile` imports only `ir`: it is below every compiler, not beside them.
- `compilers/*` and `pass` import `ir` (and their own format libraries) — never each other,
  never `emitter` or `engine`. A compiler also names the contract package and
  `compilers/compile`; each is allowed in its own right, so no sibling compiler rides in beside
  them.
- A compiler's own `internal/` packages each carry their own allowlist rather than inheriting the
  compiler's. That is what keeps the ordering among them real: `diag` reaches nothing but `ir`,
  and no lowering package may reach the compiler that calls it.
- `emitters/*` imports `ir` and `emitter` (contract) — never `compiler`.
- `engine` imports everything below it; `cmd` imports `engine`.

Two caps guard the shape rather than the graph, both in `internal/archtest` and `.golangci.yml`:
no type under `compilers/` may carry more than 20 methods, and no function may exceed 70 lines or
a cognitive complexity of 20. The method cap exists because the god object that
`micro-compiler-design.md` is about grew to 159 methods without ever writing a long function.

## 4. Diagnostics & provenance

Every IR node carries a `Provenance` (source format, file, JSON pointer or line/col, original
source name, and an `Inferred` marker naming the heuristic when applicable). Every stage returns
`[]Diagnostic{Severity, Code, Message, Provenance}`. Codes are stable strings
(`openapi/unresolved-ref`, `ir/dangling-type-ref`, `pass/discriminator-missing-variant`) so CI
can allowlist. A mature allowlist entry is keyed by (diagnostic code × entity ID), requires a
human rationale, supports expiry tied to a release, and rejects wildcards — narrowness is
validated, so approvals cannot rot into blanket suppressions. Nothing in the pipeline writes to
stderr; the CLI renders diagnostics.

## 5. Testing strategy

- **Golden IR snapshots**: each compiler has a corpus of specs; `spec → IR → JSON` is
  snapshot-compared. IR changes show up as reviewable diffs.
- **Capability conformance corpus**: one minimal spec per row of `ir-spec-matrix.md` per format
  that can express it, asserting the IR captures it losslessly. This is the regression net that
  keeps "lossless by default" honest as compilers are added. Row and spec are tied to each other
  rather than left to prose: every matrix row carries a stable key, each corpus spec names the keys
  it witnesses, and a row the OpenAPI column marks expressible must be witnessed by a spec or
  listed as not-yet-covered with a reason (`compilers/openapi/conformance_matrix_test.go`). What
  stays a reviewer's job is the claim inside that link — a spec naming a row has to *exercise* that
  capability, and no test can read a golden and tell you whether it does.
- **Round-trip property**: `parse → serialize → deserialize → deep-equal` for every corpus
  document.
- **Oracle sweep** (`internal/harness`): every corpus spec is driven through the oracles in order
  — no panic, no error diagnostic, `irverify`'s structural invariants, JSON round-trip,
  determinism across two compiles, and **order-independence**. The last one compiles the same
  source twice with every mapping's entry order reversed and diffs the two documents. It is the
  general form of the two-order check, and it is what catches an interning collision: two
  declarations minting one node at a single pointer produce the same document read either way
  only if neither is the one that got there first. Both pointer collisions the compiler has had
  were invisible to a single-order suite.
- **Architecture test** (`internal/archtest`): import-graph assertions for the layering rules
  above, plus the rules the graph cannot express — which packages may write an `ir.TypeRegistry`,
  which lowerings are mutually recursive (so a set that grows is noticed rather than measured),
  and the method-per-type cap.
- **Wire-conformance harness** (from milestone 3): expected request shapes (method, path,
  query, body keys) are derived *from the IR alone* — offline, deterministic, shared across
  every language emitter; generated SDKs run under HTTP interception and the requests are
  diffed. Request-side mismatches block; response-side mismatches inform. The decisive test of
  a generated SDK is the bytes it puts on the wire, not whether it compiles.

## 6. Milestones

1. **IR + OpenAPI 3.x compiler** — `ir` package, validate pass, golden corpus, JSON round-trip.
2. **Swagger 2.0 lift** — normalization into the OpenAPI compiler; proves the
   format-version-normalization seam.
3. **First emitter** — one language end-to-end; proves the plan/refine/emit boundary and that the
   IR retains everything a refiner needs.
4. **Second family compiler (TypeSpec or Smithy)** — proves the spec-agnostic claim: richer-than-
   OpenAPI concepts (interfaces, custom scalars, lifecycle visibility, declared pagination) flow
   through untouched IR code.
5. **Event-shaped compiler (AsyncAPI)** — proves channels/messages/bindings; then GraphQL,
   Protobuf, and Erlang/OTP (the actor-protocol compiler: behaviours → operations + channels).
