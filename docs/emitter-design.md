# Morphic Emitter — Emitter Design

The emitter is the second half of the Morphic pipeline: it turns an `ir.Document` (plus its own
options) into generated SDK source, docs, mock servers, and other artifacts. This document is the
normative contract for that half, the way `docs/ir-design.md` is normative for the IR. It extends
the emitter sketch in `docs/architecture.md §2.3`–`§2.4`; where the two disagree, `architecture.md`
wins on stage boundaries and this document wins on the shapes inside a emitter.

Type sketches are written as Go because the implementation is Go. As in `ir-design.md`, field names
and struct shapes here are the contract; receiver methods and helpers are not. The IR is consumed,
never modified — a emitter imports `ir` and reads it.

---

## 1. Purpose, pipeline relationship, and the chosen architecture

### 1.1 Where the emitter sits

```
compiler (spec → IR)  →  passes (IR → IR)  →  emitter (IR → artifacts)
                                                 │
                    ┌────────────────────────────┴─────────────────────────────┐
                    │  plan   — language-NEUTRAL decisions, computed ONCE,       │
                    │           ID-keyed, shared across every target             │
                    │  refine — per-language lowering: IR+Plan → a typed,        │
                    │           per-language target AST (the acceptance test)    │
                    │  emit   — a pure printer over the target AST + the native  │
                    │           formatter; templates only for runtime boilerplate│
                    └────────────────────────────────────────────────────────────┘
```

The engine (Layer 3) runs the compiler, then the passes, then — **once per document** — the
derived-fact index and the shared plan, and finally each requested emitter. The passes designed are
`link · dedup · filter · version-slice · overlay · validate`, and only `validate` is built
(architecture.md §2.2). An emitter never sees a source spec, a compiler, another emitter, or the
engine (INV1). Its only structural input is an `ir.Document`; everything else it needs is either
derived from the IR in the plan/refine stages or supplied as a separate, non-IR input (runtime
policy, shaping hints, the target's own options).

### 1.2 The architecture, and why this synthesis

The industry spans a spectrum, from logic-in-templates (OpenAPI Generator) through a mutable
shared CodeDOM with per-language refiners (Kiota) to a typed-AST-plus-pretty-printer (Stainless).
Morphic lands deliberately at the typed end, because it fails least on exactly the axes Morphic
cares most about — unions, composition, cross-language consistency (prior-art.md §2–§3).

OpenAPI Generator is the reference for the template pole, and its concrete anti-patterns motivate
every decision below. Its `CodegenModel`/`CodegenProperty`/`CodegenOperation` plan objects express
type kind as ~120 mutually-entangled booleans (`isString isArray isMap isEnum isNullable
isDiscriminator …`) with no exhaustiveness guarantee; they are *mutated in place* by each of ~204
per-language `*Codegen` subclasses; every casing is pre-rendered and stored on the shared object
(`nameInCamelCase`/`nameInPascalCase`/`nameInSnakeCase`); and the result is handed to logic-heavy
Mustache templates as an untyped `Map<String,Object>` that branch on the boolean bag. The costs are
exactly the ones Morphic's design negates: casing and reserved-word bugs re-litigated in every
subclass, no compiler check that kind-flags are exclusive, cross-language drift as each subclass
re-derives decisions, and unions/composition stringified (`oneOf<A,B>`, a single merged `parent`)
because there is no first-class node to carry them. Morphic answers each with a specific structural
choice — sealed-sum plan facts and a sealed-sum target AST (not a flag bag), a shared immutable plan
(not per-language mutation), neutral `Naming` cased only in refine (not stored casings), and a typed
AST printer (not an untyped template context).

The definitive design is a **strict three-stage pipeline** with these decisions:

1. **The plan layer is shared and computed once by the engine, keyed by IR stable IDs, and passed
   into every target.** This is the single most important structural choice: it is what makes the
   docs emitter, the mock emitter, and every language SDK agree *by construction* rather than
   re-derive decisions and drift — the OpenAPI Generator failure mode set out above. The
   plan is JSON-serializable and has its own language-independent golden-test corpus.

2. **Refine is an ordered pipeline of small, named, idempotent lowerings — but each one is a *pure
   function that constructs typed target-AST nodes*, not an in-place mutation of a shared tree.**
   This grafts the Kiota refiner *discipline* (an ordered, named step list is the IR acceptance
   test — every step is legal only because the un-lowered IR fact it consumes survived) onto an
   immutable-IR, build-don't-mutate model. There is **no separate neutral code model**: the only
   code models are the per-language target ASTs. That deliberately drops the "second sealed sum
   parallel to the IR" that a shared neutral CodeDOM would require, and with it Kiota's
   "minimize refiner changes or maintenance explodes" hazard and its HTTP-shaped-core ceiling.

3. **Emit is a pure printer over the typed target AST, canonicalised by the language's native
   formatter (`go/format`, `black`, `prettier`).** Structure — every declaration, field, method,
   type reference — is a compiler-checked node, so a malformed struct or a union variant missing
   its wire tag is a *Go type error inside Morphic*, not a `go build` failure in the customer's
   repo. String templates are permitted **only** for spec-invariant runtime boilerplate (the retry
   loop, the transport core, the pagination iterator runtime), parameterised by runtime policy —
   and even there they are guarded by a template lint and a mandatory format pass (the one idea
   worth keeping from the template-first stance).

4. **Every target-AST node carries its originating IR ID (`Origin`).** The AST therefore does
   triple duty: it is the code model, the source of the ID-keyed generation manifest and import
   graph, and the neutral surface model for compat verification. Write/integrate and surface
   verification fall out of a single node walk instead of being separate subsystems.

The trade this accepts, honestly: a new language costs a target-AST node set + a printer + a refine
pipeline before it emits a line — heavier than dropping in a template pack. The payoff (compiler-
enforced correctness, a free surface model, cross-language uniformity, identity-based merge) is
real but back-loaded onto the first targets, which is precisely milestone 3's charter. §5.5 and §14
address the velocity cost directly, and the `Emit` seam is per-emitter, so a future target that
genuinely wants templates for structure can implement `Emit` differently while reusing the same
`plan` and refine step library.

### 1.3 Emitter design rules (in addition to the ten CLAUDE.md invariants)

1. **The IR is read-only.** No stage of a emitter mutates the `ir.Document`. Lowering lands on the
   target AST, which is freshly built per (target × document).
2. **Decisions live in Go, never in templates.** Any branch that depends on an IR fact or a policy
   value is resolved in plan or refine and reaches emit as a decided node. A template that inspects
   the IR is a design bug.
3. **The un-lowered IR is the acceptance test.** If a refine step cannot make its decision from
   `IR + Plan + Options`, either the IR is under-designed (file a bug against `ir-design.md`) or the
   fact is runtime policy (§6) — never a compiler change (INV2, INV9).
4. **Wire truth is a separate channel from identifiers.** Serialization always reads `WireName` /
   `WireID` / `WireNameByFormat`; it never reads a rendered identifier (INV4).
5. **Nothing heuristic is silent, nothing un-lowerable is silent.** Inferred outputs are marked
   `Inferred`; un-lowerable constructs emit a coded `Diagnostic`, never a fabricated `any` and never
   a panic (INV5, INV6).

---

## 2. The emitter contract

Package `emitters` is the Layer-2 contract, as package `compilers` is the compilers'. It imports
**only** `ir` and `ir/irfacts` (Layer 0) plus its own shared sub-packages (`plan`, `policy`,
`naming`, `typemap`, `manifest`, `verify`) — never a compiler, never `engine`, never a sibling
target package (INV1; architecture.md §3). Enforced by the import-graph test (§7).

```go
package emitters

// TargetKey identifies a emitter in the registry: "go", "kotlin-jvm", "kotlin-multiplatform",
// "docs", "mock/go", …
type TargetKey string

// Emitter is one target — a language SDK, a docs site, a mock server, a validator.
// Pure and reentrant: no package-level mutable state, no clock, no randomness, no stderr,
// no filesystem I/O. Same input ⇒ byte-identical output (INV5, INV7).
type Emitter interface {
    Target() TargetKey
    Capabilities() Capabilities          // lets the engine reason without running (§2.3)
    DecodeOptions(raw []byte) (TargetOptions, []ir.Diagnostic, error) // strict, versioned (§2.5)
    Generate(EmitterInput) EmitterOutput // the whole emitter, as one pure function
}

type EmitterInput struct {
    Doc     *ir.Document          // the ABI — the ONLY structural input (INV1)
    Facts   *irfacts.Index        // what Doc implies: subtypes, usage, recursion (ir-design
                                  // §4.9), indexed ONCE by the engine, shared with the plan (§2.2)
    Plan    *plan.Plan            // language-neutral decisions, computed ONCE by the engine (§3)
    Policy  policy.Policy         // runtime/SDK behavior — a SEPARATE tree, never in the IR (INV10; §6)
    Hints   map[EntityID]ShapeHint// SDK-shaping hints keyed by IR stable ID (§6.4)
    Scope   *Scope                // nil = whole document; else "emit only these" — full Doc still
                                  // consumed so global decisions stay byte-identical
    Prior   *PriorState           // prior manifest + prior surface for write/integrate; nil first run
    Options TargetOptions         // the target's own typed options, decoded and validated before
                                  // Generate runs, its type table and naming tables among them
                                  // (§2.5, §2.6, §4.12)
}

type EmitterOutput struct {
    Artifacts   []Artifact          // files (+ integration directives) — no I/O performed here
    Diagnostics []ir.Diagnostic     // typed, coded, provenance-bearing; engine decides fatality
    Manifest    *manifest.Manifest  // entity→symbol map keyed by IR stable IDs (§11)
    Surface     *verify.Surface     // neutral API-surface projection of the generated output (§12)
}

// EntityID is the string form of any IR stable ID (TypeID, OpID, PropID, ServiceID, …).
// Every IR ID is a `type X string`, so one keyspace addresses them all.
type EntityID = string

// Artifact is oagen's GeneratedFile shape, minus every SDK-policy-shaped field.
type Artifact struct {
    Path         string          // deterministic, forward-slash, relative to output root
    Content      []byte
    Kind         ArtifactKind    // source | test | doc | manifest | dependency-manifest | scaffold
    Header       HeaderPlacement // where the generated-by provenance header goes (top/after-shebang/none)
    SkipIfExists bool            // scaffolding written once (config, .gitignore) — never overwritten
    Overwrite    bool            // regenerate but preserve ignore-regions (default is additive)
    Integrate    *IntegrateHint  // path-prefix stripping / tree-shake root for live-repo merge (§11)
    Origins      []EntityID      // IR IDs whose emission produced this file (drives ID-keyed prune)
}
```

`error` is not part of the `Generate` signature: a emitter has no failure mode that is not either a
`Diagnostic` (something about the input it cannot lower) or a programmer bug: a `TypeKind` with no
dispatch, caught by the switch-completeness test in the suite (§13) rather than at runtime, or an
options value of another target's type, reported as an error diagnostic (§2.5). This keeps the
contract pure and total.

### 2.1 The registry

Keyed by `TargetKey`, and composed by the engine exactly as the compiler registry is
(`compilers.Registry`, architecture.md §2.1): a plain value, with no package-level default and no
`init()`-time self-registration, either of which would be the mutable global INV5 rules out. The
engine's default constructor registers every built-in target in one place, beside the built-in
compilers, and a test or an embedder composes a set of its own, as `engine.NewWith` does for
compilers. Registration finishes before the engine exists, so concurrent runs only read it.

```go
type Registry struct{ /* by target, and registration order */ }

func NewRegistry() *Registry
func (r *Registry) Register(e Emitter) error           // refuses nil, a repeated key, no Lifecycle
func (r *Registry) Lookup(t TargetKey) (Emitter, bool)
func (r *Registry) Targets() []TargetKey               // sorted — determinism (INV7)
```

The engine is the only caller of `Lookup`, the only thing that runs `Generate`, and the only thing
that touches the filesystem, prunes, and renders diagnostics. Emitters are I/O-free. Reading,
rendering and writing the IR document have homes already: `engine/reader` reads what a run's
references name, `engine/report` renders, and `engine/output` writes, replacing a file through the
atomic replace in `engine/internal/atomicfile`. Writing artifacts and pruning land with §11. The
engine runs `Generate` only for a run that has not failed (`engine.Result.Failed`, architecture.md
§4): at the default threshold a document carrying an error diagnostic reaches no emitter, and a
caller that wants one built from it anyway says so with the `none` threshold.

### 2.2 Who computes the plan

`plan.Build(doc, facts, planPolicy)` is a pure function (§3). For a multi-target run the **engine
computes it once** and passes the same `*plan.Plan` into every `Generate` call. This is what makes
"computed once, shared, cannot drift" literally true rather than a convention, and it lets the plan
be cached and golden-tested on its own corpus, independent of any language. Plan policy is
document-level, not per-target — the plan is language-neutral by definition, so there is nothing
per-language to vary in it. A single-target invocation may compute the plan inline; the function is
the same.

The derived facts work the same way. The engine builds one `irfacts.Index` for the document once
the last pass has run, since an index answers for the document it was built from and a pass may
change it (ir-design §4.9), and hands that index to `plan.Build` and, as `EmitterInput.Facts`, to
every `Generate`. So the strongly connected components a refiner reads to break a recursive
reference (§4.16) are found once per document, not once per target. An index is safe for concurrent
use, so targets running side by side share it. It is not serialized: a consumer outside the process
rebuilds it from the document, which determines it.

### 2.3 Capabilities

`Capabilities()` lets the engine reason about a emitter without running it — which IR `TypeKind`s
it lowers, whether it supports write/integrate, whether it produces a surface for compat. It is
advisory metadata, not a second decision channel; it never changes what `Generate` emits.

```go
type Capabilities struct {
    LowersKinds     []ir.TypeKind // the kinds this emitter has an emit path for
    WriteIntegrate  bool          // supports additive merge into a live repo (§11)
    SurfaceVerify   bool          // produces a verify.Surface (§12)
    Protocols       []string      // "http", "grpc", "message", "graphql", "otp"
    Lifecycle       Lifecycle     // "hidden" | "experimental" | "stable" | "sunset"; required
    Successor       TargetKey     // the target replacing a sunset one; "" when none does
}
```

`Lifecycle` is the one field the engine acts on, and only to tell the user, never to change an
artifact. The states are a target's life in order. A `hidden` target is under construction: it is
left out of every listing — help, completion, the targets a misspelt name is answered with — and
runs only when named, with the `experimental` warning. An `experimental` target runs with a warning
diagnostic saying its output may change in any release. A `sunset` target runs with a warning that
names its `Successor` when it has one. The registry refuses a target that states no lifecycle, so
none is stable by omission.

### 2.4 Contract rules the type system and tests enforce

- **Purity / determinism.** No I/O, clock, randomness, or stderr inside `Generate`. Same
  `(Doc, Plan, Policy, Hints, Scope, Options)` ⇒ byte-identical `Artifacts`; `Facts` is a
  function of `Doc` and adds nothing to the tuple. Maps are walked in sorted-key order, slices in
  source order (§13 test T-6).
- **Diagnostics are the only input-failure channel.** A construct the emitter cannot lower emits
  `ir.Diagnostic{Severity, Code:"emitters/<target>/…", Message, Provenance}` — never a silent
  fallback, never a panic (§13 test T-7). Codes live in a stable `emitters/<target>/…` namespace.
- **Full document under scope.** Scope gates *emission*, not the plan or naming; global decisions
  are computed over the whole document so a scoped run is byte-identical to the corresponding slice
  of a full run. Scope is never a filtered `Document`.
- **Snapshots only.** The engine runs `version-slice` before any emitter; the emitter receives a
  single version snapshot and never interprets `Availability`. The pass is designed, not built
  (architecture.md §2.2), and needs a way for a pass to replace types (#966); until it lands, no
  pass slices a document.

### 2.5 Target options

A target's options are a typed Go struct of its own, read by its `DecodeOptions` before `Generate`
runs, so a mistake in a configuration is reported before anything is generated rather than
discovered in the output. `raw` is the target's configuration as one JSON object; the engine reads
it from wherever the user wrote it. `TargetOptions` is the interface every target's options type
meets. The engine hands `Generate` only the value the same target's `DecodeOptions` returned, and
`Generate` asserts it back to its own type; a value of another type is a programmer error, which
`Generate` reports as an error diagnostic rather than a panic.

```go
type TargetOptions interface {
    OptionsVersion() int // the version the configuration stated; the value is in the current shape
}
```

- **Strict.** An unknown key, a missing required field and an invalid value are an error, as a
  compiler's unknown setting is (`compilers.Compiler.DecodeOptions`): a setting silently ignored
  leaves its author believing they configured something.
- **Versioned.** A configuration states the options `version` it was written against. One that
  states none is refused, and so is an absent configuration, because the version decides which
  defaults apply and that must not be guessed. A version newer than the target knows is refused: it
  was written for a later Morphic.
- **Defaults belong to a version.** A target declares each optional setting's default per version,
  and fills what a configuration leaves unset with the defaults of the version it states. A change
  that would alter what existing configurations generate is a new version whose defaults differ, so
  an existing configuration keeps generating what it did until its owner moves it. New
  configurations start at the current version. A golden test per version decodes a configuration
  that states only that version and pins every default it fills, so a default changed in place
  fails a test rather than reaching existing SDKs.
- **Migrations change the shape, never a default.** A setting renamed, retyped or removed between
  versions has one pure migration per version step, applied in order to the raw configuration, so
  `Generate` sees only the current shape. A migration writes no default: those come from the stated
  version, so no migration can carry the wrong ones forward.
- **The upgrade report.** Decoding a configuration behind the current version reports, as info
  diagnostics coded `emitters/<target>/options-behind`, each setting it leaves unset whose default
  differs between its version and the current one, so an owner sees exactly what moving changes.
  The manifest (§11) records the stated version, `OptionsVersion()`.

Per-target heuristics live here too, beside the target's other choices, rather than in the plan: the
parameter-passing shape (§4.13), the type-table rows that give an unbounded number a width (§2.6),
and the naming tables (§4.12) — the acronyms, the character names that give words to a name with
none, and the leading words an identifier that would begin with a digit takes. The target-AST nodes
a default shapes carry `Inferred` provenance (§4.3), as INV6 requires.

### 2.6 The type table

Which target type a primitive becomes is the setting a generator's users override most, and every
row of it carries an import and a dependency beside the type, which drift apart wherever they are
written separately. So the table has one shape, one lookup and one merge, shared by every target in
`emitters/typemap`, rather than a field of each target's options spelled its own way.

```go
package typemap

// Table maps the IR's leaf types to one target's types. A composite key is a map in a map (§3).
type Table struct {
    Prims     map[ir.PrimKind]map[string]Row // by primitive, then encoding name; "" = the default
    Externals map[string]Row                 // by ir.External.Identity
}

type Row struct {
    Type       string            // the type as the target spells it in a file
    Codec      string            // Kotlin: the KSerializer that reads and writes it, "" = the
                                 // type's own; Go: always "", a type codes itself (below)
    Imports    map[string]string // each imported name Type and Codec spell — a Go package's, a
                                 // Kotlin class's — → what the file imports for it; empty = none
    Dependency *Dependency       // what the SDK's build requires for it; nil = the standard library
                                 // or the SDK's own runtime
}

type Dependency struct {
    Module     string      // a Go module path, a Maven coordinate
    MinVersion string      // the lowest version the SDK is generated against
}
```

- **The key is the IR's, never a source format's.** A reference resolves to a key through the plan's
  scalar chain (§3.2): the primitive at its root, and the name of the encoding declared nearest the
  reference — a property's own `Encoding`, which overrides its type's (ir-design §5.3), then
  `ScalarChain.Encoding` — or `""` where none is. A compiler already gives every format it
  recognizes a primitive of its own — OpenAPI's `date-time` is `datetime_offset` — so a key spells
  only what the IR does not, such as `{string, "email"}` or `{bytes, "base64"}`, compared exactly as
  written. An enum's `ValueType` resolves to its primitive's default row, an `Any` node to the `any`
  primitive's, and an `External` to its `Identity`. A property whose own encoding changes the key is
  typed by that key's row, not by the declaration its reference names.
- **Lookup.** `{prim, encoding}`, then `{prim, ""}`. An encoding with no row of its own falls back
  to its primitive's: the encoding stays in the IR and the SDK types it as the primitive, reported
  once per key as an info `emitters/<target>/unmapped-encoding` at the first type in ID order that
  uses it. An `External` with no row takes the opaque strategy of §4.9, with a warning.
- **Built-in rows are defaults of an options version.** Every target's built-in table has a row for
  every `PrimKind` at every options version, so the fallback always lands; T-17 holds each table to
  the kinds `ir` declares. §9 lists version 1's rows. Changing one changes what existing SDKs
  generate, so it is a new options version (§2.5). A built-in row that gives an arbitrary-precision
  primitive a width — `integer` as a 64-bit integer, `number` as a double — is a heuristic, and the
  nodes it types carry `Inferred` provenance (INV6); a configuration that wants every digit replaces
  those rows.
- **Merge.** A target's options carry the user's rows as a `typemap.Table` the shared package
  decodes strictly. A user row replaces the built-in row under the same key, a primitive's default
  included, and replaces it whole, never field by field, so a type, its codec, its imports and its
  dependency come from one source. A row under a `PrimKind` that `ir` does not declare, an empty
  `Type`, a `Dependency` without both fields, a spelling the target cannot parse, a name `Type` or
  `Codec` imports that `Imports` does not bind, and a key of `Imports` neither spells are options
  errors.
- **Where it lives.** The target's `DecodeOptions` builds the merged table — the stated version's
  built-in rows under the user's — and refine reads it as `Ctx.Types` (§4.1). It is not an
  `EmitterInput` field beside `Policy`, because every row is one target's: a Go type means nothing
  to Kotlin, and options already give a per-target setting a strict decode, a version and defaults
  per version, which a separate input would need all over again. The naming tables live in options
  for the same reason (§4.12).
- **What a mapped type must do.** A type some built-in row names is one the target knows: it writes
  that type's literals, checks its constraints and knows whether it has methods. Any other type is
  known only through its row. A Go type codes itself, through whichever of the pairs json/v2 calls
  it declares — `MarshalJSONTo`/`UnmarshalJSONFrom`, `MarshalJSON`/`UnmarshalJSON` or
  `MarshalText`/`UnmarshalText` — because json/v2 calls a type's methods wherever its value sits, a
  caller's own `json.Marshal` included, where a codec passed as an option reaches only the values
  encoded under it. So a Go row's `Codec` is an options error, and a type without those methods is
  mapped through a type the user declares with them. A Kotlin type codes through its row's `Codec`,
  a `KSerializer` the row names unless the type is `@Serializable`. A literal of a mapped type (a
  default, a constant, an enum value) is built by decoding the value's wire form through its codec,
  and a constraint on it is not checked by the SDK, which the server still checks, reported once
  per key as an info `emitters/<target>/unchecked-constraint`. A row's codec goes with its type, so
  a declaration standing over the type keeps it by naming that type again rather than defining a
  new one (§4.9), and a type only a user's row names is taken to have methods.
- **Imports and dependencies.** `ComputeImports` adds a row's `Imports` to every file that spells
  its type, and the dependency-manifest artifact requires each `Dependency.Module` a row in use
  names, at the highest `MinVersion` those rows name, compared as the target's build tool compares
  versions.

`Type`, `Codec` and `Imports` are written as the target writes them in a file. A Go row is
`{Type: "decimal.Decimal", Imports: {"decimal": "github.com/shopspring/decimal"}}`, keyed by the
name `Type` gives the package, so a package whose name is not its path's last element
(`gopkg.in/yaml.v3` is `yaml`) is bound as surely as one whose name is. A Kotlin row is
`{Type: "BigDecimal", Codec: "BigDecimalSerializer", Imports: {"BigDecimal": "java.math.BigDecimal",
"BigDecimalSerializer": "com.example.BigDecimalSerializer"}}`. Decoding parses `Type` and `Codec`
into the target's AST with each imported name bound through `Imports`, so a node holds what it
imports and never the name a row spelled it by, and what a file calls each import is settled with
every other name (§4.12): two rows that spell `decimal` for two packages, or a row's package and a
unit of the SDK's own, can meet in one file. A built-in row may name a type of the SDK's runtime,
which the target imports from wherever it puts the runtime. Each Kotlin target's built-in table is
its `Platform`'s (§7), which is where the JVM and Multiplatform differ.

---

## 3. The plan layer

`emitters/plan` (Layer 2, imports `ir` and `ir/irfacts` only). `Build` is pure, deterministic, and JSON-serializable
(INV7) so it can be cached, shared out-of-process, and golden-tested independently of any language.
Every map in the plan is keyed by a string type, since JSON names an object's members by strings
alone. `encoding/json/v2` refuses a key that does not encode as a string, as a plain struct does, so
a composite key is a map nested in a map: a struct given a text form would need an escaping of its
own, since a `TypeID` may hold any separator the text form could choose.

```go
package plan

func Build(doc *ir.Document, facts *irfacts.Index, pp Policy) (*Plan, []ir.Diagnostic)

type Plan struct {
    Ops     map[ir.OpID]OpPlan          // one entry per operation
    Types   map[ir.TypeID]TypePlan      // one entry per registry type
    Version string                      // hash(doc content-hash + PlanPolicy); feeds the manifest
}
```

Every key is an IR stable ID — never `"METHOD /path"`, never a rendered name (INV3; the oagen
fragility, prior-art §1). A spec rename never orphans a plan entry: it is "same ID, new symbol",
a lookup. Every inferred field carries `Inferred` provenance, propagated into diagnostics so it is
auditable and disable-able (INV6). The plan's own diagnostics are coded `emitters/plan/…`, reported
once for every target rather than once by each.

The plan **classifies; it does not pick target types and it never cases anything.** It records what
it derives from the IR ("this union is discriminated, and its tag values select the variants with
IDs X/Y/Z") so every refiner chooses a strategy from the same derivation, and it leaves what the IR
states outright to be read from the IR (§3.6). It contains no casing, no reserved-word handling, no
target types, and no runtime or target policy.

A reference the plan records — `ReturnType`, a page's item type, an error's type, a variant a
discriminator maps a tag to — names the type the IR names there. Where a derivation reads a type's
contents, such as the property a page's items are found under, it reads them through
`TypePlan.Alias` (§4.15), and every consumer lowers a reference it reads through `Alias`, the plan's
as the IR's, so no field holds a reference already lowered beside others that are not.

### 3.1 OpPlan — the fixed per-operation decision set

Adopted from oagen's production `OperationPlan` (prior-art §1), the decision set the emitters
demonstrably needed:

```go
type OpPlan struct {
    Return          ReturnShape      // how the success body is read: value | void | raw | stream
    ReturnType      *ir.TypeRef      // what a value body decodes into; nil unless Return is value
    PrimaryResponse *ir.Response     // SDK-ergonomic pick; IR keeps ALL responses
    PrimaryContent  *ir.Content      // json>form>multipart>binary — a SHARED plan helper
    Errors          []PlannedError   // status→error, ranges collapsed (404/4XX/catch-all)
    Page            *PagePlan        // nil = not paginated
    Stream          *StreamPlan      // nil = not streaming (ir-design.md §7.3)
    LRO             *LROPlan         // nil = not long-running
    Auth            []AuthOption     // OR-of-ANDs, priority-ordered (§8.6)
    Bindings        BindingView      // per-protocol param/request/response view from OpBindings
    Inferred        map[string]ir.Provenance // which fields above were heuristic (INV6)
}

type PagePlan struct {                // item-type unwrap is a PLAN decision
    Strategy   ir.PageStrategy
    ItemType   *ir.TypeRef            // element type items resolve to, unwrapped from the envelope
    ItemPath   *ir.PropPath           // where items live — a PATH (may be nested / in headers)
    CursorIn   *ir.ParamPath
    CursorOut  *ir.PropPath
    Inferred   bool                   // heuristic pagination (may be disabled by policy)
}

type PlannedError struct {
    Conditions ir.ResponseConditions  // status ranges, collapsed
    Type       *ir.TypeRef            // an Error-flagged model
    Fault      string                 // "" | "client" | "server" — declared
    Retryable  *bool                  // declared (nil = unknown) → wins over policy (§6.3)
    Throttling *bool                  // declared throttling class (nil = unknown)
}

// The remaining operation-facet plans. Each is nil when the facet is absent, so a refiner's
// presence check is a nil check, and each carries only classified facts — never a target type.
type StreamPlan struct {
    Direction  ir.StreamingMode       // client | server | bidi (from the operation core)
    EventUnion ir.TypeID              // the WireTagged event union (Variant.Event carries per-event
                                      // content-type + terminal bit); resolved by ID
    Initial    *ir.TypeRef            // initial-request/response message preceding the stream; nil = none
    RequiresLength bool               // streamed content needs a known finite length up front
}

type LROPlan struct {                 // maps ir.LongRunning (ir-design.md §7.3)
    FinalStateVia string              // "operation-location" | "status-monitor" | "original-uri" | …
    PollOp     *ir.OpID               // declared polling operation; nil = poll the start op's monitor
    FinalOp    *ir.OpID               // declared final-result operation; nil = result is inline
    PollType   *ir.TypeRef            // status-monitor type
    FinalType  *ir.TypeRef            // what PollUntilDone resolves to
    ResultPath *ir.PropPath           // where the final result lives in the terminal response
}

type AuthOption struct {              // one option in the OR-of-ANDs; slice order is PRIORITY order
    Schemes []ir.SchemeUse            // ALL must be satisfied together; empty = "no auth is acceptable"
}

// ReturnShape is how an operation's success body reaches its caller, a small closed enum:
type ReturnShape string              // "value" | "void" | "raw" | "stream"
```

**Return shapes.** `Return` says how the body is read, not what it holds: what it holds is
`ReturnType`, and every type kind already has a lowering (§4.2), so a shape per kind would repeat
the type in a second channel. The first rule that applies decides:

1. **`stream`** — the operation streams its response (`Streaming` is `server` or `bidi`): events are
   read one at a time, as `Stream` describes (§8.4).
2. **`void`** — nothing is read: the operation has no success response, or its primary response
   carries no payload. An operation with no success response returns only an error, and which
   statuses fail it is the error taxonomy's (`Errors`, §8.5). The IR does not yet say whether a
   lone `default` response is the success response (#608); once it does, this rule reads that
   response as any other.
3. **`raw`** — the primary content is a file (`Content.File`, ir-design §7.2): the body reaches the
   caller unread, a byte stream beside the media type it arrived as, and nothing decodes it.
4. **`value`** — otherwise: the primary content's codec decodes the body into `ReturnType`, whatever
   its kind — a model, a list, a map, a union, an enum, a scalar, `any`. A paginated operation's
   `ReturnType` is one page, from which `Page` takes the items.

`ReturnType` is the primary content's type, lowered through `TypePlan.Alias` as any reference is
(§3, §4.15), so an anonymous `allOf` decorator around a model returns the model. Its `Nullable` is
the content's, and a nullable return takes the target's nullable form (§4.7). Unwrapping an
envelope, which `plan.Policy` may enable (§3.5), would return a value found inside the content
instead; what it records and where is #695's. Which codec reads a media type is the target's, and a
media type it has none for is a coded degrade of that target's that reads the body as `raw`. Every
operation of every document takes one of the four, since the rules end in a default; §8.9 traces
each through the first targets.

Some facts oagen's plan carries are not here, by §3.6. Whether there is a body, whether the
operation is one-way and its idempotency are stated by the IR, so a refiner reads them there:
`Operation.Request`, `Operation.OneWay` and `Operation.Idempotency`, the last of which also names
the idempotency-token parameter. And whether an operation's inputs are passed positionally or
inside an options object differs by language, so it is each target's (§4.13); the neutral facts it
is decided from are already here — `BindingView` says where each input binds, and
`Parameter.Required` and `Payload.Required` which are required — so moving the decision out added
no field.

`PrimaryResponse` / `PrimaryContent` are the *one legitimate home* for the collapse oagen performs
destructively in its compiler and Morphic forbids there (INV2): the IR keeps every response and
content type whole; "pick a primary for the ergonomic method" happens here, lowered late. `Content
negotiation` default (`json > form > multipart > binary`) is a shared, overridable plan helper
(resolves `ir-design.md` open Q3).

### 3.2 TypePlan — per-type projections computed once

```go
type TypePlan struct {
    Flattened  []ir.PropID          // FlattenedProperties() over Base+Implements+Mixins+own — COMPUTED
    Union      UnionShape           // a union's wire shape, one of four (§4.4); "" = not a union
    Discrim    *DiscrimFacts        // a union's or hierarchy's discriminator, as variant IDs
    Scalar     *ScalarChain         // a scalar's Base chain, walked once; nil = not a scalar
    Shapes     map[ir.Lifecycle]ModelShape // the wire shapes the document exercises (§3.3)
    Alias      ir.TypeID            // the type every reference to this one is emitted as, this
                                    // one emitting nothing: a decorator's end (§4.15), or a dedup
                                    // alias's target once that pass (architecture.md §2.2) and its
                                    // record (ir-design §16, question 2) exist; "" = itself
    Reach      []ir.GroupID         // the top-level operation groups whose operations reach this
                                    // type, sorted; empty = none does (§4.17)
    Inferred   map[string]ir.Provenance
}

// UnionShape is the wire shape a union's serializer reads, from Exclusive, WireTagged and
// Discriminator together (ir-design §4.4's tag-mode conventions).
type UnionShape string              // "one_of" | "any_of" | "discriminated" | "key_tagged"

type ScalarChain struct {           // a Scalar's Base chain walked to its root (§4.9)
    Root        ir.TypeID           // the chain's first node that is not a Scalar: a primitive, or
                                    // what an alias stands over (ir-design §4.2); "" = opaque
    Constraints *ir.Constraints     // merged along the chain, decimal throughout (§4.11)
    Encoding    *ir.Encoding        // the encoding declared nearest this scalar; nil = the root's
}

type DiscrimFacts struct {          // Mapping/Default resolved to TypeIDs, never names (INV3)
    Locator  DiscrimLocator         // property | property_name | index
    WireKey  string                 // wire name of the tag property ("" when Locator == index)
    Index    *int                   // positional tag element (Erlang tagged tuples); nil otherwise
    Mapping  map[string]ir.TypeID   // wire tag value → variant ID
    Default  ir.TypeID              // variant for absent/unrecognized tag; "" = none
    Envelope string                 // "" = inline tag | "object" = {kind,value} wrapper
    EnvelopeValueName string        // wire name of the envelope value property (Envelope=="object")
}
type DiscrimLocator string          // "property" | "property_name" | "index"
```

Note what `TypePlan` does *not* do: it does not choose Go's sealed interface vs a native union, does
not case anything, does not resolve reserved words, and does not copy a fact the IR states outright
(§3.6). `Flattened` is the *computed* flattening; the IR keeps `Base`, `Implements` and `Mixins`
distinct beside it, so a class-based target can map `Base→extends` / `Implements→interfaces` /
`Mixins→inline` where the three first targets flatten (§8.8). The plan does not decide which — that
is per-language (§4).

### 3.3 ModelShape — visibility projection

One logical model produces N wire shapes across the lifecycle. `ModelShape(model, lifecycle)` is a
**computed traversal in the plan layer**; the IR stores only the single logical model plus visibility
facts and never the projected variants (INV2; `ir-design.md §5.2`). The plan emits one `ModelShape`
per lifecycle the document actually exercises, in the type's `TypePlan.Shapes`, so a refiner knows
exactly which shapes to build: those keys are the projections the type needs, and nothing else
lists them.

```go
type ModelShape struct {
    Props   []ir.PropID  // properties visible in this lifecycle
    Patch   bool         // PATCH shape: properties are implicitly optional unless the binding disables it
    Inferred map[string]ir.Provenance
}
```

`readOnly → Only:[read, delete, query]`; `writeOnly → Only:[create, update]` (ir-design §5.2);
`Visibility.None` → excluded from every projection (distinct from the zero value); PATCH flips
properties optional unless `HTTPBinding.PatchImplicitOptionality` points at `false` (nil is the
protocol default, which flips them), and a patch shape never carries a property's default onto the
wire (§4.10); operation-level `ParameterVisibility` /
`ReturnTypeVisibility` override which filter applies. Unknown visibility classes are opaque
filters evaluated per class (`ir-design.md §5.2`).

### 3.4 BindingView — the per-protocol request/response projection

`Parameter`s carry no protocol location (`ir-design.md §7.2`). The plan derives, from `OpBindings`,
a neutral per-protocol view a refiner turns into a request builder: which params are path/query/
header/cookie/body, their style/explode/prefix, RPC message folding, message channel + reply, the
GraphQL entry point, the OTP call/cast shape. One operation may carry several bindings (gRPC
transcoding `additional_bindings`, HTTP+RPC); all are represented, primary first. This resolves
`ir-design.md` open Q6 on the emitter side: status conditions and `StatusCodeProp` are read through
the HTTP `BindingView`, so RPC/OTP single-response ops with empty conditions are handled uniformly.

```go
type BindingView struct {
    Primary  ProtoBinding    // the binding a single-protocol SDK uses
    All      []ProtoBinding  // every binding, primary first (transcoding, dual HTTP+RPC)
}
type ProtoBinding struct {
    Protocol string          // "http" | "grpc" | "message" | "graphql" | "otp"
    Params   []ParamBind     // neutralized param placement (one per bound param, per location)
    ResponseBodyPath *ir.PropPath // HTTPBinding.ResponseBodyPath: the response body is this
                                  // sub-field of the payload; nil = the whole payload
    // Protocol-specific handles kept by ID/reference, not restructured:
    HTTP     *ir.HTTPBinding // method/URITemplate/status map, when Protocol == http
    RPC      *ir.RPCBinding
    Message  *MsgView        // channel + direction + reply, when Protocol == message
    Raw      ir.RawConfig    // GraphQL entry point / OTP call-cast tag / deployment bindings, verbatim
}
type ParamBind struct {
    Param    ir.ParamID      // the bound Operation.Params entry; one name may be two params
    Where    string          // path | query | querystring | header | cookie | body |
                             // body_property | host
    WireName string          // never a rendered identifier (INV4)
    Style    string; Explode *bool; Prefix string   // form/label/matrix/deepObject serialization
    BodyPath []ir.PropID     // body_property: where in the body model the param lands
}
type MsgView struct { Channel ir.ChannelID; Direction ir.MsgDirection; Reply *ir.Reply; Messages []ir.MessageID }
```

The Go-first walkthrough (§8) exercises `Protocol == "http"`; the RPC/message/OTP arms exist in the
type so the second-protocol emitter (open Q4) does not force a plan schema change (INV9). Channels
and messages (`ir-design.md §8.3`) reach a emitter through `MsgView`; a messaging target consumes
them the same way an HTTP target consumes `HTTP`.

`Raw` is `ir.RawConfig`, not `ir.Unmodeled`, and the distinction is not cosmetic. It carries
*declared* protocol configuration the plan chooses to pass through unstructured — the GraphQL
entry point, the OTP call/cast tag, AsyncAPI deployment bindings — all of which the IR models
fields for; there is no unmodeled construct to give a reason for, and the source position a
consumer would want is the owning binding's. The `Unmodeled` map each binding struct carries
(`ir-design.md §8.1`–`§8.5`) is the other thing entirely: the escape hatch for constructs the IR
does *not* model — a directive applied to a GraphQL entry-point field, say — where `Reason` and
`Provenance` are exactly what a policy layer selects on. A refiner reads both, for different
purposes.

Sub-clients and fluent navigation are built from the `OperationGroup` tree itself — its nesting,
names, operations and Smithy `Resource` — which the IR states, so the plan mirrors none of it
(§3.6).

### 3.5 plan.Policy — the plan's injectable heuristics

Everything the plan *infers* rather than reads is a knob here; every inferred output is marked
`Inferred` (INV6):

```go
type Policy struct {
    ContentRanking      []string                        // default: json > form > multipart > binary
    PrimaryResponse     func([]ir.Response) *ir.Response // default: lowest 2xx
    EnvelopeDetect      EnvelopePolicy                   // off unless explicitly enabled
}
```

A test asserts that disabling a heuristic removes the corresponding `Inferred` plan field — the
guard against a heuristic silently defaulting on and quietly making the plan non-neutral.

### 3.6 Plan fact or refine fact

The plan classifies and refine decides (§3), and three first targets (§14) test that line from the
first model. A candidate field belongs in the plan only when all three of these hold:

1. **Neutral.** Its value is the same for every target. Ask it concretely: would Go, Kotlin/JVM
   or Kotlin Multiplatform ever want a different value, or would a class-based or dynamic target?
   If any would, it is a refine decision, made by each target with its default injectable through
   the target's options (§2.5).
2. **Closed over the document and plan policy.** It is a function of the IR and `plan.Policy`
   alone, and reads no naming policy, runtime policy, hint or target option. Needing one of those
   is the clearest sign the fact is per-target.
3. **Derived, and exposed by more than one artifact.** Its value is derived rather than read — a
   choice under `plan.Policy`, or a classification or walk over more than one IR fact — and more
   than one artifact generated from the document exposes it, so two of them disagreeing would be a
   defect a user can see: docs describing a response the SDK does not return, a mock paging where
   the SDK's pager does not, an SDK sending a tag value the docs do not list.

A fact the IR states outright fails the third test and gets no field: every consumer reads it from
the IR, and a copy would be a second home for it. A record the plan derives as a whole is not such a
copy, though it holds declared facts: a `PagePlan` the policy may infer has no IR field to read
instead, `Errors` collapse several cases into one entry, and the stream and long-running plans
gather facts spread over more than one node — the event union, the poll operation's monitor type —
so each reads the same however its facet was written. What the test removes is a field or record
that derives nothing. Any other fact that fails it shapes only the code of the artifact that uses it
— how an untagged union's variant is recognized on decode (§4.4), which symbols a file imports — and
is a shared refine helper in `emitters/refine` when more than one target needs it: reused code, not
a plan field.

The procedure decided every existing field:

1. **Choices under `plan.Policy`** stay: `PrimaryResponse`, `PrimaryContent`, the unwrap behind
   `ReturnType`, and `PagePlan`, an inferred one included.
2. **Derivations several artifacts expose** stay: `Return`, `Errors`, `Auth`, `Bindings`, `Stream`
   and `LRO`; and `Flattened`, `Union`, `Discrim`, `Scalar`, `Shapes`, `Alias` and `Reach`. `Alias`
   classifies a decorator over several of its fields and resolves its chain (§4.15), and `Reach`
   walks every operation (§4.17); SDKs and docs that disagreed on either would name a field's type
   or a type's home differently.
3. **Copies of what the IR states** are gone: whether there is a body, one-way-ness and idempotency
   (§3.1), a model's `Base`, `Implements` and `Mixins`, an enum's openness, value type, flags and
   fallback member, a union's variant list, and the operation-group tree (§3.4). `Union` replaces a
   classification that copied the variant list and a second discriminator beside the tag mode it
   derived, and `Scalar` replaces a bare root that left the constraints it merged nowhere.
   `NeedShapes` is gone too: it listed again the lifecycles `Shapes` is keyed by (§3.3). A client's
   servers are stated by the IR as well, so they get no field (§8.10).
4. **The parameter-passing shape** fails the first test and moved to refine (§4.13): a dynamic
   target's keyword arguments are additive as they stand, where a positional parameter added to a Go
   or Kotlin function breaks its callers, so the best default differs by language.
5. **What the derived-fact index states** gets no field either. How a type is used, whether it is
   recursive and which types share its strongly connected component are `irfacts.Index` facts,
   read through `EmitterInput.Facts`, and a plan copy would be their second home, which is why
   `Usage`, copied into the plan before the index derived it, is gone. Which references of a
   component a target must break fails the first test, since what breaks a cycle — a slice, a
   pointer, a class reference — is the target's (§4.16).

What holds the line beyond review: `emitters/plan` imports only `ir` and `ir/irfacts`, so it cannot
read a naming or runtime policy or a target (§7); the plan is golden-tested once, with no target in
the loop; and a plan field's doc comment names what it derives and which artifacts expose it, which
a reviewer checks against the third test.

---

## 4. The refine layer

`emitters/<target>/refine`. This is where the "lossless, lowered late" bet is cashed (INV2) and
the **only** place names get cased (INV4). It reads the immutable IR and the shared Plan and
**constructs typed nodes of a per-language target AST**. It never mutates the IR and never mutates a
shared cross-language tree.

### 4.1 The refiner contract — an ordered pipeline of pure lowerings

Following Kiota's concrete contract — an ordered list of small, named, idempotent steps, each valid
only because the un-lowered fact it needs is still present (prior-art.md §2) — but each step
is a **pure function `IR+Plan → nodes`**, not an in-place edit:

```go
package refine

type Refiner interface {
    Target() emitter.TargetKey
    Steps()  []Step             // ORDERED — composition order is part of the contract
}

type Step interface {
    Name() string               // stable, for diagnostics + golden step-trace tests
    Apply(*Ctx) []ir.Diagnostic // appends typed nodes to the target AST under construction; idempotent
}

type Ctx struct {
    Doc     *ir.Document        // read-only source of un-lowered facts
    Facts   *irfacts.Index      // read-only derived facts, the engine's one index (§2.2)
    Plan    *plan.Plan          // read-only shared decisions
    Types   typemap.Table       // the merged type table (§2.6), built from Options
    Policy  policy.Policy        // read-only SDK runtime policy (§6)
    Naming  naming.Policy        // the target's casing engine over its options' naming tables
                                 // (INV4; §4.12)
    Hints   map[emitter.EntityID]emitter.ShapeHint
    Options emitter.TargetOptions // decoded options (§2.5); a step asserts its own target's type
    Out     TargetAST            // the per-language AST being built (e.g. *goast.Module)
    Syms    *SymbolTable         // ID-keyed; collision resolution never touches IR identity
}

func Run(r Refiner, c *Ctx) []ir.Diagnostic  // runs Steps in order

// TargetAST is the per-language AST root marker; each target supplies a concrete type
// (e.g. *goast.Module). It is an interface here only so the shared refiner contract is language-
// neutral; every real step type-asserts to its own AST and appends typed nodes.
type TargetAST interface{ targetAST() }
```

**This ordered, named step list IS the IR acceptance test.** Each step below is only implementable
because a specific un-lowered IR fact reached it; a missing IR capability shows up as an
unimplementable step, not a silent degrade. Ordering is asserted by a golden *step-trace* test:
collapsing aliases runs first, since every other step lowers references through it; placement
follows every structural lowering, and import computation follows placement, since a file imports
what its declarations reference from wherever those landed; the naming steps run last — casing,
then reserved-word escaping, whose scopes reserve what a file imports (§4.12), then collision
resolution — so every name, a unit's and an import's among them, is settled once, after everything
that names it exists. Because each step is idempotent and reads only its `Ctx`, a step-trace
snapshot pins the whole lowering deterministically.

### 4.2 The canonical step list

| Step | Reads (un-lowered IR fact) | Lowers to (Go example) | IR ref |
|---|---|---|---|
| `CollapseAliases` | `TypePlan.Alias`; the decorator chain's `Docs`, `Deprecation`, `Examples` | no declaration for an aliased type; each reference to it lowered as one to its `Alias`, its annotations folded onto the carrier (§4.15) | §4.3; §12.2 |
| `ResolveScalarChains` | `plan.ScalarChain` (the `Base` chain walked once, constraints merged), `Ctx.Types` | a defined type over a root with no methods, an alias over any other (§4.9), the root typed by the type table (§2.6), + merged constraints; nil-base → newtype (opaque-scalar strategy) | §4.2 |
| `LowerContainers` | `List/MapT/Tuple` nodes (with IDs) | `[]T` / `map[K]V` / positional struct, its positions past `MinItems` optional and a `Rest` as a trailing slice; a nil `MapT.Key` is `string`, and a non-string key → custom-key map or diagnostic | §4.6 |
| `LowerLiterals` | `Literal{Value}` | typed constant / single-value newtype (incl. `symbol`) | §4.6 |
| `LowerModels` | `TypePlan.Flattened`, `Base`/`Implements`/`Mixins`, `AdditionalProps`, `Additional`, `Positional` | struct over the flattened properties, catch-all map as the embedded fallback, unknown members rejected on `closed` (§8.1) | §4.3 |
| `ProjectVisibilityShapes` | `plan.ModelShape` per lifecycle | `User` / `UserCreate` / `UserPatch` structs | §5.2 |
| `LowerOptionality` | `Property.Required` ⊥ `TypeRef.Nullable` ⊥ `Property.Presence`; `Facts.Component` | the four+ states → `T` / `*T` / `Opt[T]`/`Nil[T]`/`OptNil[T]` / `omitzero` + presence bit; a reference on a cycle the target holds by value held through a pointer (§4.16) | §5.1; INV8 |
| `LowerConstraints` | `Constraints` on properties/scalars/lists/maps | field-level `validate` tags + a refine-built `Validate()` method structure | §5.3 |
| `LowerUnions` | `Union{Variants, Exclusive, WireTagged, Discriminator}`, `TypePlan.Union` | sealed interface + marker method + concrete variants | §4.4; INV2 |
| `LowerDiscriminator` | `Discriminator{Property/PropertyName/Index, Mapping→TypeID, Default, Envelope}` | tag-dispatched `UnmarshalJSONFrom` + typed factories | §4.3–§4.4 |
| `LowerOpenEnums` | `Enum.Closed`, `ValueType`, `Members`, `Flags`, `FallbackMember` | closed → typed consts; open → `string`+consts+`Unknown(raw)`; flags → bitset | §4.5 |
| `ExtractInterfaces` | `Model.Abstract`, `Implements`, union membership | Go interfaces / client interfaces for testability | §4.3 |
| `MapExternalTypes` | `External{Identity, Package, MinVersion}`, `Ctx.Types` | the type table's row for the identity (§2.6): imported library type + dependency-manifest entry; no row → opaque strategy + diagnostic | §4.6 |
| `MaterializeValues` | `Value{Kind, Num BigVal, Ref, Ctor}` | literals; `Num` → bignum/decimal, never float64; `Ctor` → call; `symbol` → string explicitly; a default documented, never written onto a request (§4.10) | §6 |
| `BuildRequestShaping` | `plan.BindingView`, the target's parameter shape (§4.13) | request builders, per-protocol param binding, each operation's request type | §8 |
| `LowerPagination` | `plan.PagePlan` (item unwrap, cursor paths) | auto-pager yielding `iter.Seq2[T, error]`; pacing left to policy | §7.3 |
| `LowerStreaming` | `plan.StreamPlan` (direction, event union) | `EventStream.Recv()`/`Send()` over the event union; terminal ends | §7.3 |
| `LowerLRO` | `plan.LROPlan` (final-state-via, poll/final ops, result path) | poller type + `Poll()/PollUntilDone()` returning the final type | §7.3 |
| `LowerErrors` | `plan.PlannedError`, `irfacts.UsageError`, `Fault` | error type tree (`APIError` → client/server → concrete) | §7.2 |
| `PlaceDeclarations` | `Provenance.Source`, `TypePlan.Reach`, the target's placement options | the output unit each declaration lands in: one package unless a partition or a group split moves it (§4.17) | §13 |
| `ComputeImports` | what each file's declarations reference: another unit's declarations, a type-table row's `Imports`, the runtime | per-file import sets, each import by its path; the name a file gives it is settled with every other name (§4.12) | — |
| `RenderCasing` | neutral `Ident` (= `naming.Words`: the canonical words beside their source spellings; a name with none named by its characters) + the options' naming tables | cased identifiers, the units' among them — the ONLY place names get cased (§4.12) | §3.2; INV4 |
| `EscapeReservedWords` | cased `Ident` + each scope's reserved set: keywords, what the generated code binds there, what its file imports, what a unit cannot be named (§4.12) | keyword-safe identifiers; NEVER touches `WireName`/`WireID` | §3.2; INV4 |
| `ResolveCollisions` | `Ctx.Syms` (ID-keyed symbol table) | deterministic disambiguation in every scope, the units and each file's imports among them, an import yielding to a declaration (§4.12); never mutates IR identity | §3.1; INV3 |

Every `ir.TypeKind` has a dispatch here: `primitive` and `any` map through the type table (§2.6) in
`ResolveScalarChains`/type-reference lowering, `literal` in `LowerLiterals`, the rest in the named
steps above. The switch-completeness test (§13) proves the coverage in the suite. A kind no compiler
produces yet dispatches to a coded degrade instead of a lowering (§14.1), which keeps the coverage
without a lowering nothing could test. The concrete lowering strategies for the load-bearing axes
follow.

### 4.3 The target AST substrate

The target AST (`emitters/golang/goast` for Go, `emitters/kotlin/ktast` for both Kotlin targets) is
a small, purpose-built, sealed-sum syntax layer — the exact code the SDK needs, at
declaration/signature granularity — modelled with the same discipline the IR uses (INV
Go-conventions): an unexported marker method, one struct per node kind, a `Kind()` accessor, a
switch-completeness test over the node enum, its kind list held to what the package declares
(`internal/decls`), as the IR's is.

```go
package goast

type Node interface { node(); Kind() NodeKind; Origin() emitter.EntityID }

type Module struct { Packages []*Package }
type StructType struct {
    Common
    Fields  []*Field
    Embeds  []Expr           // the catch-all map (§8.1); Base and Mixins are flattened into Fields
}
type Field struct {
    Common
    Name Ident               // cased identifier
    Type Expr                // a typed expression node — cannot be a raw string
    Tag  StructTag           // built from WIRE name/ID/format, NEVER the identifier
    Opt  Optionality
}
type InterfaceType struct { Common; Methods []MethodSig; Marker string } // union base / abstract model
type Method struct { Common; Name Ident; Params []*Param; Returns Expr; Kind MethodKind }
type ErrorType struct { Common; Fault string; Retryable *bool; Throttling *bool }

type Common struct {
    origin emitter.EntityID   // ← the IR ID this node came from (manifest / surface / prune; INV3)
    prov   ir.Provenance      // passthrough for diagnostics + Inferred marking
    ident  Ident              // NEUTRAL until RenderCasing runs: naming.Words(Naming), never cased
}

type Expr interface{ isExpr() }
type Named   struct { Pkg, Name string }   // time.Time, uuid.UUID — both standard library (§8)
type Pointer struct { Elem Expr }
type Slice   struct { Elem Expr }
type MapExpr struct { Key, Value Expr }
type Iface   struct{}                       // any
```

Two properties make this the right substrate: **illegal code is unrepresentable** (a field's type is
an `Expr`, not a string; a union variant that forgot its wire tag is a missing struct-literal field
the Go compiler flags inside Morphic — the antidote to the Mustache `oneOf`/`allOf` failure class),
and **every node carries its `Origin`**, so the manifest, import graph, and surface model are one
walk (§11, §12).

### 4.4 Unions (the hardest axis)

`LowerUnions` reads the plan's wire shape (`TypePlan.Union`) and the target's capability. The
tag-mode matrix (each a distinct serializer) is normative (`ir-design.md §4.4`):

- `WireTagged=false` → untagged; variant inferred by validation (JSON oneOf/anyOf).
  `Exclusive=true` = exactly-one; `false` = one-or-more (anyOf, open). An untagged union carries
  no `Discriminator`.
- `WireTagged=true` + `Discriminator` → the tag travels in the payload, where the discriminator
  locates it: an OpenAPI `discriminator`, GraphQL `__typename`, an Erlang tagged tuple. It sits
  inline among the variant's own members (the property `PropertyName` names, or the tuple element
  at `Index`), or, with `Discriminator.Envelope == "object"`, in a `{kind, value}` wrapper around
  them.
- `WireTagged=true` + nil `Discriminator` → key-tagged: a single-key object keyed by the variant's
  wire name (Smithy unions, proto3-JSON oneof, GraphQL `@oneOf`).

Go has no sum type, so the Go refiner picks the **sealed-interface** strategy — the same closed-sum
idiom the IR itself mandates: `type Payment interface { isPayment() }`, one concrete struct per
variant with an unexported marker method, a generated switch-completeness test over the variant set.
Variant identity (`Variant.Name`) survives for named accessors / constructors; `WireName`/`WireID`
survive for (de)serialization. **Unions never degrade to optional-field merges** — flattening a
*body* union into method arguments is an opt-in `ShapeHint` (§6.4), never a silent loss (the oagen
counterexample, prior-art §1). A future TS/Python target lowers the *same* facts to a native
`A | B | C`; the decision differs, the fact base is identical.

Within the untagged case, choosing *how* a variant is recognized on decode is refine's job, and
ogen's `schema_gen_sum.go` is the battle-tested enumeration to lift: try, in order, explicit
discriminator → implicit discriminator (variant const/name) → JSON-type discrimination (variants
distinguishable by JSON kind) → unique-field discrimination → value-based. The decisive divergence:
ogen computes this *while building the Go model*, so a union it cannot discriminate aborts the whole
generation. Because Morphic keeps `Union` lossless in the IR and only a emitter picks a strategy, a
Go refiner that finds no strategy emits a coded diagnostic and falls back to `jsontext.Value` per
variant, which keeps the variant's bytes as they arrived — a degrade, never a whole-document failure
(§13 test T-7). That graceful-degradation margin is exactly what the ABI seam buys and the
fused-pipeline generator cannot have. Whatever the strategy, the union's codec is a
`MarshalJSONTo`/`UnmarshalJSONFrom` pair, for the reason §4.8 gives. Which Go type declares the
pair is open: an interface type cannot declare a method, so the sealed interface itself cannot
(#1022).

### 4.5 Open enums

`Enum.Closed` tells the refiner whether extensibility must survive. Go cannot express open enums, so
`Closed=false` lowers to a named string type + typed consts + a passthrough that preserves unknown
values and honors `FallbackMember` on decode; `Closed=true` lowers to typed consts with strict
decode; `Flags=true` → bitset; numeric enums render `Value` through the Values channel (§4.10).
Duplicate member values (`allow_alias`) must not be rejected; slice order is the canonical name for
serialization. The open/closed bit, value type, member values, and flags bit all survived from the
IR to this point — Kiota's string-only-closed-enum mistake is designed out because the fact reached
refine.

A Go constant is declared at package level, where every enum's constants meet, so Go names a
member's constant by its enum's identifier, as rendered, followed by the member's name
(`StatusActive`); a Kotlin entry lives in its enum class and takes the member's name alone
(`ACTIVE`, as the naming policy cases it).

### 4.6 Visibility projections → N wire shapes

`ProjectVisibilityShapes` consumes the precomputed `ModelShape` map and emits the concrete
create/read/update/patch structs the target needs — Speakeasy's "public type vs wire (de)serialize
shape" split made explicit. `None` properties are excluded from every projection; PATCH shapes make
properties optional unless the binding disables it. A model used as both input and output
(`Facts.Usage` = Input|Output, `InputOnly` distinct identity for GraphQL inputs) may emit two
shapes.

### 4.7 Optionality × Nullability × Presence (INV8)

`LowerOptionality` renders the four states — `Property.Required` (wire presence) ⊥ `TypeRef.Nullable`
(admits null) — plus protobuf `Property.Presence` (implicit/explicit/required) as a *third* axis that
is not nullability (protobuf has no null). The reference Go lowering is ogen's generic-box scheme
(`gen/ir/generics.go`), which independently arrives at exactly these four states: required-nonnull →
bare `T`; optional → `Opt[T]` (a `{value T; set bool}` box, so presence is distinct from the zero
value); nullable → `Nil[T]`; both → `OptNil[T]`. A mature real-world generator needing all four
first-class boxes is the strongest external confirmation of INV8, and — decisively — ogen makes this
decision in its *emitter* (`boxType`), keeping `Required` and `Nullable` orthogonal in the schema
model right up to that point. That is Morphic's split precisely: orthogonal bits in the IR, boxed in
refine. Two Go-specific optimizations to inherit: reference types (slice/map/pointer) can encode one
of the four states in their own `nil` (ogen's `NilSemantic: invalid | optional | null`), saving a
wrapper; `explicit` protobuf presence → pointer/hazzer, `implicit` → `omitzero,omitempty`, so a
zero is not serialized. `ClientOptional` relaxes the client-side type even when `Required=true`;
`DefaultAdded` may be suppressed for back-compat. Where Go's zero value would force a *deliberate*
collapse of two states, a `Diagnostic` is emitted — never a silent conflation (§13 test T-3).

Presence is stated in `encoding/json/v2` terms, and `omitempty` never keeps a presence state. v2
defines `omitempty` by the encoded JSON — a field is dropped when it would encode as `null`, `""`,
`{}` or `[]` — so it keeps a zero number or `false` and drops a pointer to an empty struct: a set
but empty value turns into an absent one, the silent conflation just forbidden. `omitzero` is
defined by the Go value and asks the type's `IsZero` method when it has one. The boxes implement
`IsZero` as "not set" beside their own `MarshalJSONTo`/`UnmarshalJSONFrom`, so a box field tagged
`omitzero` is omitted exactly when unset, and an `Opt[int]` set to `0` is written. The `implicit`
tag carries both options because each alone misses a zero: `omitempty` keeps `0` and `false`, and
`omitzero` keeps an empty non-nil slice or map.

### 4.8 Discriminators

`LowerDiscriminator` reads a `Discriminator` whose `Mapping` points at **TypeIDs, not names**, so a
presentation rename never breaks the wiring. It emits a tag-dispatched `UnmarshalJSONFrom` that
peeks the discriminator **wire** key (never the Go field name), typed factory functions, and a
`MarshalJSONTo` that writes the tag. `Property` vs `PropertyName` vs `Index` (positional),
`Envelope` vs inline, and the `Default` variant on an absent/unrecognized tag all survive
un-lowered; the choice of factory vs native vs wrapper is the refiner's.

The pair is `encoding/json/v2`'s `MarshalerTo`/`UnmarshalerFrom`, not v1's `MarshalJSON`/
`UnmarshalJSON`, because only the v2 methods see the caller's encoder and decoder. `MarshalJSONTo`
writes the variant straight to the caller's `jsontext.Encoder`. A tag may sit anywhere in its
object, so `UnmarshalJSONFrom` buffers the one value it dispatches (`dec.ReadValue()`), scans it
for the tag without decoding the rest, and decodes the variant from that buffer under
`dec.Options()`: the options the caller decoded under reach the variant's body. A v1
`UnmarshalJSON` receives only bytes, so it decodes the variant under options of its own choosing,
never the caller's, and a type-specific decoder the caller registered with `json.WithUnmarshalers`
stops applying at the first union. The one option a model sets over its caller's is
`RejectUnknownMembers`, which each model pins for itself (§8.1). `ir/json.go` is the worked
example: `(*TypeRegistry).UnmarshalJSONFrom` buffers each entry, `stringMember` finds the `kind`
tag by token scan, and `decodeTypeDef` decodes the concrete kind under the decoder's options.

### 4.9 Containers, scalars, external types

- **Containers**: `List`/`MapT`/`Tuple` are real IR nodes with IDs, so `List<Map<K,List<T>>>`
  composes cleanly; constrained lists and maps keep their bounds; `List.Encoding` (packed/expanded)
  reaches the serializer; a nil `MapT.Key` is a string key, and a non-string one is handled or
  diagnosed; tuples become positional structs
  whose positions past `Constraints.MinItems` are optional and whose `Rest`, when present, is a
  trailing slice.
- **Scalars**: the plan walks `Base` transitively once (`TypePlan.Scalar`), merging
  `Constraints` and settling `Encoding`, so the SDK, its docs and its mock read one chain;
  `ResolveScalarChains` lowers what it found, the root typed by the type table (§2.6). Nil-base
  scalars (GraphQL custom) go to the opaque-scalar strategy (a Go newtype), never a fabricated
  chain.
- **A declaration over a root keeps the root's codec.** A named scalar, an anonymous one that is no
  decorator (§4.15), and a `Model` whose `Base` is no model (§9) each declare a type standing over
  their chain's root. Go defines a new type, `type Name string`, only where the root's type has no
  methods of its own — a predeclared type, a slice, a map — so the new type codes as the root does
  and can carry a `Validate` of its own. Over any other root it declares an alias,
  `type Created = time.Time` or `type Owner = User`. A defined type has none of its root's methods:
  json/v2 refuses `type Created time.Time` outright, writes `type Key uuid.UUID` and
  `type Blob jsontext.Value` as base64, and never runs a closed model's strict decoder or an open
  enum's keeper of unknown values for one. An alias declares no method, so its constraints are
  checked where it is used, as a field's are (§4.11), and a projection of it (§4.6) is its root's.
  The target knows which of its built-in types have methods, and takes a type only a user's row
  names (§2.6) to have some. Kotlin draws the same line at its own types: a value class over
  `Boolean`, a number, `String` or `ByteArray`, and a `typealias` over any other root, which leaves
  the root's type, and its serializer, in every signature. An enum is outside this line: it
  declares its own codec (§4.5), which reads and writes its value type through that type's row, so
  it is a defined type whatever its value type maps to.
- **External** (R4 §2.8): `External{Identity, Package, MinVersion}` becomes the type its type-table
  row names for `Identity` (§2.6), imported with the row's `Imports` and required at its
  `Dependency` — imported, not generated. `Package` and `MinVersion` name what the source format
  imports, which a built-in row may follow; they cannot name a target's type, so a row is what maps
  an identity. An identity with no row (`erlang:pid`, `erlang:fun`) gets the passthrough/opaque
  strategy and a `warning` diagnostic, never a silent `any`.

### 4.10 Values

`MaterializeValues` renders defaults, consts, enum values, literals, and examples as language
literals. `Num` (`BigVal` decimal string) parses straight to the target numeric/decimal type —
**never through float64** (§13 test T-5). `symbol` renders as a string only *explicitly* and
diagnosed (Erlang atoms: `ok ≠ <<"ok">>`); `Ref` renders as an `Enum.Member` reference, not an
inlined copy; `Ctor` renders as a constructor *call* (`plainDate.fromISO(...)`), never folded to a
frozen literal. A value of a type only a user's type-table row knows is decoded from its wire form
through the row's codec (§2.6).

**A default is documented, never sent.** A property's or parameter's `Default` is what the API
assumes when the input is absent, so an SDK writes it into the documentation of the input and into
no request: an optional input the caller leaves unset is absent from the request whatever its
default, in every shape. Filling it in would turn "not set" into "set to the default", which on a
patch shape (`ModelShape.Patch`, §3.3) resets the field on every update, and on any other shape
overrides a server whose default has since moved. A required input with a default stays required.
A response member that arrives absent stays absent and is never read as its default, since presence
is a state the SDK keeps apart (INV8, §4.7).

### 4.11 Constraints and validation

`LowerConstraints` reads `Constraints` on properties, scalars, and lists (§5.3 of `ir-design.md`)
and does two things: it stamps field-level validation metadata (a `validate:"…"` struct tag or an
equivalent registration) and it *builds the structure* of a per-type `Validate()` method — which
checks apply to which fields — as typed AST. Validation is therefore neither pure boilerplate (the
checks depend on the spec) nor free-form template logic: refine decides the set of checks, and the
individual check primitives (`checkMinLength`, `checkMultipleOf` on `big.Rat`) are runtime helpers.
This is ogen's `oas_validators_gen.go` split, relocated to honor the structure-vs-boilerplate line
(§5.2). Numeric bounds flow through `BigVal`/`big.Rat`, **never float64** — ogen itself gets storage
right yet leaks `Float64()` in its `minNum`/`maxNum` merge path, so any constraint intersection the
plan or a refiner performs (a scalar chain's bounds, `allOf` bounds) must stay on decimal. A
`Pattern` a target's regex engine cannot honor is dropped with a `warning` diagnostic, never
silently mistranslated, and a constraint on a type only a user's type-table row knows is left to the
server with an info diagnostic (§2.6).

### 4.12 Naming, casing, reserved words, collisions (INV4)

Identifier rendering is owned **exclusively** by refine, through a `naming.Policy` each target
builds from its options. Its casing functions are the target's code. The tables they read — the
acronyms, the character names and the leading words below — are heuristics whose every change
renames identifiers, so each is a default of the target's options version, under the entries a
configuration gives it (§2.5), which `DecodeOptions` merges key by key as it merges the type table
(§2.6). They are not an `EmitterInput` field, for the reason the type table is not: each is one
target's.

```go
package naming

type Policy interface {
    Case(words []Word, role Role) string  // Role: TypeName | Field | Method | Const | Param | Package
    Acronyms() AcronymTable               // from the options
    Reserved() ReservedSet                // the language's keywords and predeclared names
    CharacterNames() CharacterNameTable   // from the options: character sequences → words, for a
                                          // name with none
    LeadingWord(role Role) string         // from the options: the word before an identifier that
                                          // would begin with a digit, one per role (below)
}

type Word struct {
    Canonical string // one word of Naming.Canonical: the boundaries, never re-derived
    Source    string // that word as Naming.Source spells it, letter case kept; "" when it has none
}

func Words(n ir.Naming) []Word // the one alignment every target uses
```

The `Ident` on every AST node starts as the neutral `Naming.Canonical` word sequence — a *word
sequence, not an identifier*. `Canonical` holds those words as one lower_snake string, and
`naming.Words` splits them on `_`, the one separator its grammar admits (ir-design §3.2), for `Case`
to take each beside its source spelling (below). `RenderCasing` is the only step that cases it;
because the IR stores the pre-split neutral words, there is no lossy runtime re-splitting of a cased
string (oagen's `splitWords` counterexample). `EscapeReservedWords` runs after casing and **never
touches `WireName`/`WireID`** — struct tags are built from the wire channel, so wire correctness
never depends on the rendered identifier (§13 test T-4). Collisions are resolved deterministically
in `Ctx.Syms`, which is keyed by IR ID: adding one endpoint never renames an existing method (the
oagen collision-cascade counterexample; INV3). Anonymous hoisted types get an emitter-chosen name
from `Naming.Hint`. Per-service presentation renames (`Service.Renames`) change how a shape is
presented in a service without changing its `TypeID` or its own `Naming`. Enum members are keyed by
`EnumMember.ID`, which is derived from the value a member holds, so two members whose names render
alike are two entries to disambiguate, and reordering members of distinct values renames none.

**The source spelling, for letter case only.** `Case` receives each canonical word beside the
spelling the source gave it. ir-design §3.2 derives `Canonical` from `Source` by one grammar, which
lives in `ir` (`ir.CanonicalWords`), and `irverify` holds every `Naming` that has a `Source` to it
(`ir/naming-not-derived`). `naming.Words` takes the spellings from that same grammar: `ir` gains a
sibling of `CanonicalWords` that returns the words as written, and `CanonicalWords` becomes their
lowercased join, so the words align one to one and no emitter carries a second copy of the grammar.
A name with no `Source` — one built from a hint — has no spellings, and neither has a `Naming` the
grammar does not derive, which `Words` leaves unspelled rather than guess. A policy may read a
spelling to recover the letter case of a word whose boundaries the canonical form already fixes:
`iOSVersion` is `i_os_version` and `HTTPServer2FA` is `http_server_2_fa`, and the spellings `i`,
`OS`, `Version` and `HTTP`, `Server`, `2`, `FA` let a policy render `IOSVersion` and
`HTTPServer2FA`, where casing the words gives `IOsVersion` and `HttpServer2Fa` and an acronym table
would have to capitalize `os` and `fa` in every name. It never reads one to find a boundary — the
words are fixed before any spelling is read — so the rule against re-splitting a cased string holds.
Ignoring the spellings is conforming, and the acronym table applies either way.

**A name with no words takes its characters' names.** A `Source` with no letter or digit in it — an
enum value `>=`, an `operationId` `***`, a property `<` — has neither `Canonical` nor `Hint`
(ir-design §3.2), and still needs an identifier, wherever it is written. `RenderCasing` names it:
from each position, the longest sequence the character-name table holds becomes that entry's words,
and a character it does not hold becomes one word, `u` and its code point in lowercase hex (`u2192`
for `→`). The words have no source spelling, so they are cased as a hint's are, and the node they
name carries `Inferred` provenance, `character-names` (INV6). The words read nothing but the name,
as `EnumMember.ID` reads nothing but the value, so reordering an enum renames none of its members.
Two names that meet — `=` and `==` are both `equal` — are `Ctx.Syms`'s, like any other collision.

The table is one of the naming tables, so a configuration may replace any entry of it, and a
target's is a default of its options version (§2.5). The first targets share version 1's, in which
a sequence is named by what it means where it has one meaning, and each other character by its own
name:

```text
<=  less_than_or_equal      >=  greater_than_or_equal   !=  not_equal       ==  equal
<   less_than               >   greater_than            =   equal           !   exclamation_mark
*   asterisk                +   plus                    -   minus           /   slash
\   backslash               %   percent                 &   ampersand       |   pipe
^   caret                   ~   tilde                   @   at              #   hash
$   dollar                  ?   question_mark           .   period          ,   comma
:   colon                   ;   semicolon               _   underscore      '   apostrophe
"   quotation_mark          `   backtick                (   left_parenthesis
)   right_parenthesis       [   left_bracket            ]   right_bracket
{   left_brace              }   right_brace             U+0020   space
```

**An identifier that would begin with a digit takes a leading word.** Canonical words may begin with
a digit — a property `1000`, a schema `10_gbps_to_100_gbps`, an enum value `10002` — and no first
target accepts an identifier that does. Go takes one only from a letter, and exports one only from
an upper-case letter, so `1000` cannot be a field at all; Kotlin takes one only in backticks, which
the Objective-C export a Multiplatform SDK reaches iOS through cannot carry (§8.8). Where the
identifier a role composes would begin with a digit, `RenderCasing` puts the policy's leading word
for that role before its words, and the node it names carries `Inferred` provenance, `leading-word`
(INV6). The test reads the composed identifier, not the name: a Go enum constant begins with its
enum's name (§4.5), so `Code` and `1000` make `Code1000` and need no word, where a Kotlin entry is
the member's name alone and takes one. A hint is held to the same test.

The words are a naming table too, one per role, and like the character names a default of the
target's options version (§2.5). Version 1's are `type` for a type, `field` for a field, `call` for
a method, `value` for a constant, `arg` for a parameter and `pkg` for a package, so the examples
render `Field1000`, `Type10GbpsTo100Gbps` and Kotlin's `VALUE_10002`. A word that makes one name
meet another — `Field1000` beside a property spelled `field_1000` — is `Ctx.Syms`'s, like any
collision.

**Reserved names belong to a scope.** `Reserved()` holds the language's keywords and predeclared
names, reserved everywhere. Generated code binds names of its own too, and a spec name rendered
equal to one of them shadows it or fails to compile, so each scope reserves what the code generated
into it binds as well, and the units' scope what a unit cannot be named:

- **The units** (§4.17), whose names are a scope of their own: `shared`, the shared unit's, and the
  names a target's toolchain treats apart — Go's `internal`, which no caller outside the SDK may
  import, `testdata`, which `./...` skips, and `vendor`, which the `go` command reads at a module's
  root as vendored dependencies.
- **A unit**: the names the target declares at its top level beside the spec's — Go's `Client`, its
  option type and their constructors, and a group unit's sub-client.
- **A type**: the members the target gives every model — Go's `MarshalJSONTo`, `UnmarshalJSONFrom`,
  `Validate` and a redacting `String`, and the field holding the catch-all (§8.1); Kotlin's
  `equals`, `hashCode`, `toString`, `Builder` and `Companion`.
- **A function**: its receiver, its context and request parameters, every local its body binds, and
  the package names its file imports. Whatever builds a body declares the names it binds beside it —
  the refine step that builds it as typed nodes, or the template that renders it, whose declaration
  the template lint holds to what the rendering binds (§5.3) — so no list is kept apart from the
  code it describes.

A spec name that meets a reserved one takes the target's escape (§8.8). Only spec names yield, since
the target's own names are what its runtime and templates are written against. A declaration a
caller names never yields to an import either: an import yields to it. Where a Kotlin file refers
to a declaration with the simple name of a class it would import, it writes that class qualified
rather than importing it; Go's exported names cannot meet its lowercase package names, and where two
packages one Go file imports share a name — the standard library's `time` and a group unit named
`time`, or two rows' `decimal` — one takes a name of its own in that file. So only a parameter or a
local yields to an import, and no caller depends on its name. That is why `ComputeImports` runs
before the naming steps (§4.1): what a file imports is fixed by where declarations landed, and what
the file calls each import is a name like any other. T-19 holds every scope's set, the names with no
words and the names that begin with a digit to code that compiles.

### 4.13 Interface extraction & request shaping

`ExtractInterfaces` derives per-language interfaces from `Abstract` / `Implements` / union
membership, plus client interfaces for testability from the `OperationGroup` structure.
`BuildRequestShaping` turns the plan's `BindingView` into request builders, and passes an
operation's inputs positionally or inside an options object — RequestBuilder-style constructs are
generator-side, out of the IR (prior-art §2). `OperationGroup` nesting becomes sub-clients / fluent
navigation.

The parameter shape is each target's (§3.6), with a default rule marked `Inferred` and overridable
through the target's options (§2.5). The three first targets share one default: path parameters are
positional, in the order the primary binding's URI template names them (`HTTPBinding.URITemplate`),
never in the order `Operation.Params` holds them, which follows the source's declarations rather
than the URL; and every other input — query, header and cookie parameters and the body — is a field
of one request type for the operation, which it takes when it has any such input. Go's request type
is a `<Operation>Params` struct; Kotlin's is a class built like a model (§8.8). Two properties
decide the rule:

1. **Only the wire may fix an order.** A path template orders its parameters and a parameter list
   does not: two required strings the spec lists the other way round would swap at every positional
   call site, and still compile.
2. **An input the spec adds is an additive change.** A new field of the request type is one on every
   target. A new positional parameter breaks every Go caller, a new parameter with a default changes
   the JVM signature (§12), and Swift sees no default arguments at all.

An operation that gains its first non-path input gains the request argument, a break the surface
differ reports (§12). A dynamic target's default may differ — Python's keyword arguments are
additive as they stand — which is why the shape is per-target rather than a plan fact.

### 4.14 Unmodeled entries

An `Unmodeled` entry is a construct the IR keeps verbatim rather than modeling it (ir-design §12).
An SDK emitter acts on an entry's `Reason`, never on its key, and reports none of them:

| Reason | What an SDK emitter does |
|---|---|
| `vendor_extension` | nothing, unless a policy names the key (§6) |
| `out_of_scope` | nothing, unless a policy names the key: emitter policy is the consumer the IR keeps these for |
| `validation_only` | nothing; a validation emitter reads them (§10) |
| `degraded_lowering` | lowers the weaker shape the IR holds beside the entry, never the entry |
| `no_ir_home` | nothing: honouring the construct waits for the field the IR lacks, never a key read by name |

**Reporting is the compiler's.** The compiler is the stage that chose to keep a construct rather
than model it, and its specification states the diagnostic each kept construct draws, or that it
draws none (ir-design §12). `Document.Diagnostics` carries those reports to every consumer, so a
report from the emitter would repeat each one once per target and add only "this target ignores
it", which the table already says of every SDK target.

**Keys are read by name only through a policy.** An SDK emitter reads an entry's key only where a
policy names it, and only `vendor_extension` and `out_of_scope` entries are kept for policies
(INV6), so the stability of key names (#429) reaches an SDK through those two reasons alone. The
validation emitter reads `validation_only` keys as the keywords they are (§10), which #429 covers
too.

The copies of `readOnly` and `default` that the OpenAPI compiler keeps where they were declared,
after applying them at every use
([`use-bound-annotations`](./compilers/openapi.md#use-bound-annotations)), need no rule of their
own: every use site already carries the fact, and an SDK reads no `no_ir_home` entry. T-15 (§13)
holds the table.

### 4.15 Decorators collapse into their references

OpenAPI 3.0 admits nothing beside a `$ref`, so a document annotates one use of a shared schema by
wrapping it: `state: {allOf: [{$ref: Status}], description: the widget state}`. The compiler keeps
that losslessly, as an anonymous `Model` whose `Base` is `Status` and which carries the description
(ir-design §4.3). 3.1's spelling of the same use, `{$ref: Status, description: …}`, hoists an
anonymous `Scalar` alias over `Status` wherever no property or parameter holds the annotations
(ir-design §12.1), and so does an annotation alone on a non-carrier position, `items: {type: string,
example: x}`. None of these nodes says anything about a value that its `Base` does not. Lowered as
written, each would be a type of its own — a struct repeating a wrapped model's fields, or a second
name for a wrapped enum (§4.9), named from a hint — where every user expects the type it wraps.

**What a decorator is.** An anonymous type whose `Base` is set, and which is

- a `Model` with every other field of its own zero, or a `Scalar` with no `Constraints` and no
  `Encoding`; and
- sets nothing in `TypeCommon` beyond its identity (`ID`, `Name`, `Namespace`, `Anonymous`,
  `Provenance`), the annotations a use site can hold (`Docs`, `Deprecation`, `Examples`),
  `Availability`, which no emitter interprets (§2.4), and `Unmodeled`, whose entries an SDK acts on
  by reason, none of them by changing a type (§4.14) — T-15 requires as much, since an entry that
  kept a node from collapsing would change the artifacts.

The rule lists what may be set rather than what may not, so a field the IR gains keeps a node from
collapsing until a change decides otherwise; the plan's test enumerates the fields by reflection,
which turns that decision into a failing test rather than a review catch. Anything else a node sets
— a property, a constraint, `Sensitive`, an XML name, a media-type hint — says something about
values at its position, and the node lowers as it would without this section. A named type is never
a decorator: its name is public surface, lowered by §9's rows.

**What the plan records.** A decorator's `TypePlan.Alias` is the first type along its `Base` chain
that is not one, found by a walk bounded by the size of the registry. A chain that returns to a
decorator it passed collapses nowhere, and each of its nodes lowers as it would otherwise. A dedup
alias, once that pass exists, is followed the same way.

**What refine does.** `CollapseAliases` runs before every other step. An aliased type emits no
declaration, and each reference to it — a `TypeRef`, or a `TypeID` a discriminator's mapping or
default names — is lowered as a reference to its `Alias`, keeping the referring reference's
`Nullable`, which answers for the whole decorator (ir-design §3.3). The decorators' annotations fold
onto the carrier holding the reference — a property, a parameter, a header or a union variant, each
of which has a field for all three — so the reference documents its input exactly as the 3.1 `$ref`
it stands for does:

- **`Docs` and `Deprecation`**, each field from the first that sets it: the carrier's own, then each
  decorator's along the chain; a carrier is deprecated where any of them is. At a property,
  parameter or header the annotations of the type `Alias` names come last, merged as ir-design
  §12.2 merges a `$ref` target's onto the carrier naming it; a union variant takes none of that
  type's, as the compiler gives a variant none.
- **`Examples`** whole: the carrier's own if it has any, else the nearest decorator's. Those of the
  type `Alias` names stay on it, since an example beside a `$ref` describes the position and never
  the target ([`schema-examples`](./compilers/openapi.md#schema-examples)).
- **Anywhere else** — a list element, a map value, a content, a `Base` — nothing holds them. They
  stay in the IR, where the docs emitter reads them, and the plan reports each decorator some
  reference reaches whose `Docs` or `Deprecation` reach no carrier, once rather than once per
  target, as an info `emitters/plan/decorator-annotations-unplaced` at the decorator. A decorator
  nothing references generates nothing and is not reported, and neither is an unplaced example: no
  SDK renders a schema example.

The fold is a shared refine helper in `emitters/refine`, reading only the IR facts `Alias` points
along. T-16 holds it to the compiler's merge: a 3.0 decorator and the 3.1 `$ref` carrying the same
annotations beside it generate identical artifacts at every kind of position, so moving a document
from one spelling to the other changes no SDK. A union variant is the exception until the compiler
hints a decorated branch as it hints a bare `$ref` (#1018).

### 4.16 Recursive references

`Facts.Recursive` says whether a value of a type can hold, at any depth, a value of its own shape,
and `Facts.Component` which types share that cycle (ir-design §4.9); the engine derives both once
per document (§2.2). What a target must do about a cycle is its own, since what breaks one is a
property of its type system (§3.6).

- **Go** holds a struct field by value, so a cycle of struct fields is an invalid recursive type. A
  slice, a map, a pointer and an interface each break a cycle, so a list, a map, a union and a
  polymorphic base — the last two sealed interfaces (§4.4, §8.8) — already do; a model, a tuple's
  positional struct and the boxes of §4.7 hold their contents by value, and do not. So a reference
  is held through a pointer when it lies on a cycle of references Go holds by value, read after
  `Base` and `Mixins` are flattened into the declarations they land in. Such a cycle lies within one
  of `Facts.Component`'s components, and a shared refine helper finds the cycles a target's
  by-value references close within each. The pointer stands for a box's state where it can (§4.7):
  a required reference is `*T`, which `Validate` reports when nil; an optional or a nullable one is
  `*T`, nil meaning absent or null; and an optional nullable one is `Opt[*T]`.
- **Kotlin** holds every class by reference, so a cycle asks nothing of either Kotlin target.

Go breaks every reference on such a cycle rather than the fewest that would do. The fewest depend
on the whole graph — one type added elsewhere can change which reference is picked — and each pick
that moves changes a field's type for every caller. Nor does it break every by-value reference whose
two ends merely share a component: a cycle that passes through a list, a map or a union already
compiles, and a pointer on it would ask every caller to check for nil where nothing requires it.
A reference is a pointer exactly when a by-value cycle passes through it, which only a change to
that cycle moves. T-20 holds the rule.

### 4.17 Output units

Every declaration a target emits lands in one output unit, and a reference across units is an import
`ComputeImports` adds. A Go unit is a package; a Kotlin unit is a package, and also a Gradle module
of its own unless it is the root, since kotlinc compiles a module whole and a Multiplatform
framework exports what its modules declare (§8.8). `PlaceDeclarations` decides each declaration's
unit as a function of the document and the target's options alone, never of how many declarations
there are: moving a declaration to another unit renames it for every caller, so a document that
grows must move none. Placement reads the whole document, so a scoped run places each declaration
as a full run does (§2.4).

Two rules move a declaration out of the root unit. Both are off by default, and the first that
applies decides.

1. **Partition by source document.** `Partition` maps a `SourceInfo.Path` to a unit, for an API
   split across files that wants each file's types as a package of its own, a cross-file `$ref`
   becoming a cross-package reference. A type declaration goes to the unit its own
   `Provenance.Source` maps to, and one an overlay introduced or rewrote belongs to the document the
   overlay patched, the entry document ([`overlays`](./compilers/openapi.md#overlays)). Operations,
   sub-clients, the client and request types stay where rule 2 or the root puts them, whichever file
   declared them: an operation is a method of its group's client type, and Go declares a method in
   its receiver's package. What the document cannot honour is reported:
   - a key with a `#fragment` is an options error, since a partition is a whole document;
   - a key naming the entry document, `Sources[0]`, or an overlay is an error diagnostic: the root
     unit is the options' to name already, and an overlay's declarations are its document's;
   - a key naming no source of the document is a warning, since it partitions nothing;
   - an import cycle between units is an error, since neither a Go package nor a Gradle module
     admits one, naming the units and one reference that closes the cycle.

   Each document a spec's references read is a `Sources` entry after the entry document and its
   overlay, and a type it declares, inline in a parameter or response it holds included, names that
   entry (ir-design §3.1), so a key names a file the references read.
2. **Split by group.** With `SplitByGroup`, each top-level operation group is a unit, holding the
   group's operations, its sub-client and every type only its operations reach, as `TypePlan.Reach`
   says; `Reach` spreads from each operation's roots along the type graph as usage does (ir-design
   §4.9), a service's `CommonErrors` counting as every one of its groups' roots, since each of its
   operations can return them. A type several groups reach goes to a shared unit, and a type none
   reaches stays in the root with the client. No cycle can arise: whatever a reached type holds is
   reached by every group that reaches it, so a group unit imports at most the shared unit, the
   shared unit imports none, and only the root, which nothing imports, holds a type that may reach
   into a group. A type moves between a group's unit and the shared unit only when the set of groups
   reaching it changes, which takes a change to the operations that reach it.

The root unit's name is the options'. A group's unit is the group's name rendered for a package
under the root (`<root>/<group>` in Go, `<root>.<group>` in Kotlin), the shared unit is `shared`
under the root, and a partition's unit is the name its key maps to. The units' names are a scope of
their own (§4.12): a group named `shared`, or `internal` in Go, meets a reserved name, and two
groups whose names render alike — tags `Webhooks` and `webhooks`, both the Go package `webhooks` —
collide, and are told apart by group ID as any collision is. A name the options give a unit never
yields. A declaration a target derives from another lands in that one's unit — a visibility shape
(§4.6) or a Kotlin variant class in its type's, an operation's request type in the operation's —
and a unit is a namespace of its own, so two declarations collide only within one (§4.12). T-18
holds placement to these rules.

**The size check.** Each target states a limit on the declarations one unit holds, a default of its
options version (§2.5), and reports each unit over it once, as a warning
`emitters/<target>/unit-over-limit` naming the unit, its count and the limit, and `SplitByGroup`
while it is off. The limit decides nothing: a count that moved declarations when it crossed a
threshold would move them as a document grew, the instability placement exists to prevent. Go's
default is 10,000 declarations. A synthetic package of that many models, each with its codec and a
`Validate`, peaked at about 1.6 GB of memory to build with Go 1.27.2 on darwin/arm64, and one of
30,000 at about 4.6 GB. Kotlin's limit counts a Gradle module's declarations, and its default is set
when the Kotlin targets are first measured (§16).

---

## 5. The emit layer

`emitters/<target>/emit` plus per-target printer. Emit is a **pure fold** `print(TargetAST) → []byte`,
then the language's native formatter canonicalises the result.

### 5.1 Typed AST → text → native formatter (the rationale)

The Stainless model, adapted to Go: the printer emits syntactically complete text and hands it to
`go/format.Source` (Python `black`, TS `prettier`), which is the canonical authority on formatting.
Indentation, spacing, and import ordering stop being the emitter's problem — an entire class of
whitespace/import-order bugs is eliminated by delegation. A language whose formatter Go cannot call
in process, as Kotlin's, takes the other route: its printer lays out canonical code itself, and the
formatter runs as a check in the verify stage, outside `Generate` (§8.8).

**Why a domain AST → text → `go/format`, not `go/ast` directly.** `go/ast` is awkward to *synthesize*
(it wants a `token.FileSet` and real positions and cannot carry generator intent like a field's
`Origin` IR ID or a `{t:TypeID}` doc token). The domain AST is smaller, holds provenance and wire
metadata on its nodes, and prints to text that `go/format` then makes canonical. `go/ast` is used in
exactly one place — re-parsing *existing* files for additive merge (§11) — where its parser is the
right tool. The two Go trees are kept aligned by ID-keying: "a symbol I generated" ↔ "a symbol I
parsed" is a lookup.

### 5.2 The structure-vs-boilerplate line (the honest boundary)

| Content | Representation | Why |
|---|---|---|
| Models, enums, unions, discriminators, operations, clients, error types, serializers | **Typed AST nodes** | Spec-derived *structure*; correctness must be compiler-checked; must be uniform across languages |
| Runtime core: HTTP transport, retry loop, backoff+jitter, pagination iterator runtime, auth injectors, logging hooks | **Templates**, one small file each, parameterised by `Policy` | Policy-driven *boilerplate* whose shape does not vary with the spec |
| Package docs, README, per-op usage examples | **Docs emitter** (`docast`, §10) | Prose with holes |

A retry loop is the same code for every API; only numbers and header names vary. A typed AST there
buys nothing and costs readability. But a template with an `if` that inspects the IR is a design bug
— that `if` belongs in refine, producing a different node. The boilerplate templates therefore
contain **no policy branching and no structural branching**: all decisions were made in plan/refine;
the template only interpolates scalar policy values (retry count, header name). So templates cannot
diverge across languages the way Mustache *structure* templates do.

The Go templates lean on the standard library wherever Go 1.27 does the work (§8). In particular the
transport template does not drain an unread response body by hand so that its connection can be
reused: `net/http` does that itself on `Close`, up to a bound it sets, and past the bound closes the
connection rather than read an arbitrarily large body.

### 5.3 Two guards on the boilerplate templates

Grafted from the template-first stance, applied only to the narrow boilerplate surface:

- **A template lint** (a unit test per pack) parses every `.tmpl` and rejects kind-discriminating
  conditionals, arithmetic, and comparisons beyond boolean presence — only `range`, field access,
  whitelisted pure funcs, and `{{template}}` are allowed. This turns "a template *could* smuggle a
  decision" from a discipline problem into a failing test. A template that renders into a scope
  where spec names live also declares the names it binds there (§4.12), and the lint renders it and
  holds that declaration to what the rendering binds — for Go, as `go/parser` reads it — so a local
  added to a template cannot shadow a parameter the declaration left out.
- **A mandatory format pass** (`Formatter.Format(path, src)`), run on *all* emitted files. Any
  syntactic slip a template produces is caught here as a located, hard `Diagnostic` (file + the
  formatter's message), never shipped. Because typed-AST structure is already syntactically valid by
  construction, in practice the format pass only ever catches boilerplate-template mistakes.

### 5.4 Serialization behind a runtime abstraction

The generated *typed veneer* references a `Transport` / `Serializer` interface (Kiota's
`IParseNode`/`ISerializationWriter` seam); the concrete transport is a runtime-policy-configured
library. This is the seam that keeps runtime policy (INV10) out of the typed structural model:
the veneer of models/unions/methods is policy-free, and `Policy` only ever parameterises the
boilerplate that backs the interface.

### 5.5 Determinism, and the velocity answer

Emit walks the AST in source order and any map in sorted-key order (INV7); identical `IR + options`
⇒ byte-identical output (§13 test T-6), which is what makes golden snapshots and the ID-keyed
manifest meaningful. On velocity: a new *language* is expensive (node set + printer + pipeline), but
a new *non-SDK emitter* (docs, mock) is cheap because it reuses the shared plan and most of the
refine step library (§10); and the per-language cost buys compiler-enforced correctness, a free
surface model, and identity-based merge that a template pack cannot. The `Emit` seam remaining
per-emitter means a target may opt into templates-for-structure if it ever needs to — the design
does not mandate typed-AST emit, it defaults to it.

---

## 6. Runtime/SDK policy — a separate input

`emitters/policy` (INV10; architecture.md §2.4). The behavioral configuration of generated
SDKs is a emitter input alongside the IR, **never a field on `ir.Document`** — the divergence from
oagen, which attaches `SdkBehavior` to its spec root. Keeping the trees separate lets one IR drive
SDKs, docs, and mock servers without dragging SDK opinions along.

### 6.1 The canonical vocabulary

From oagen's production `SdkBehavior`, the best real-world enumeration (architecture.md §2.4):

```go
package policy

type Policy struct {
    Retry       Retry        // retryable status codes, max attempts, Backoff{initial, multiplier, max, jitter}
    Timeout     Timeout      // default seconds + env override
    Errors      ErrorTax     // status→logical exception-kind, client/server catch-all, doc-URL template
    Telemetry   Telemetry    // request-ID header, client-telemetry header
    Logging     Logging      // enabled + a CLOSED lifecycle-event list
    UserAgent   UserAgent    // identifier template, app-info enrichment, ai-agent env vars
    Idempotency Idempotency  // header name, auto-generate rules
    Pagination  Pacing       // auto-page delay
    Guards      RequestGuard // option keys that must NOT appear as params (misuse detection)
}

func Defaults() Policy
func Merge(base Policy, over DeepPartial) Policy // scalars override; ARRAYS REPLACE (the oagen rule)
```

### 6.2 Delivery

Canonical `Defaults()` + deep-partial per-emitters/per-project overrides. Arrays replace
entirely (not concat) — an explicit, documented rule. Policy is delivered in `EmitterInput.Policy`;
refiners read it via `Ctx.Policy`, and it shapes only the generated *runtime binding* (the retry
loop, transport config, user-agent string, telemetry headers, pagination pacing) behind the
`Transport`/`Serializer` abstraction. It never enters the typed structural model (§5.4).

### 6.3 Precedence — declared IR facts win (hard rule)

`ErrorCase.Retryable/Throttling/Fault` and `Operation.Idempotency` come from the spec; policy fills
in only where the spec is silent — never the reverse. The refiner enforces this by reading the IR
fact first and consulting policy only on `nil`. A `policy-precedence` test asserts a spec
`Retryable:false` beats a policy that lists that status as retryable (§13 test T-9). When policy is
overridden by a spec fact, an `Inferred`-marked diagnostic is emitted so nothing is silent.

### 6.4 SDK-shaping hints — a third, ID-keyed input

oagen's `OperationHint` / rename / remount / split-union-body / url-builder machinery is *SDK
ergonomics, not API semantics* — the same category as runtime policy. It lives in
`EmitterInput.Hints`, keyed by **IR stable ID** (never `"METHOD /path"`), so a path rename never
drops a hint (INV3):

```go
type ShapeHint struct {
    Rename         string      // presentation rename for the entity's symbol; "" = none
    Remount        *ir.GroupID // move an operation under a different sub-client; nil = keep
    SplitUnionBody bool        // flatten a request-body union into typed wrapper methods — OPT-IN,
                               // the one sanctioned union→arguments collapse (§4.4); default off
    URLBuilder     bool        // expose a URL-builder variant of the operation
    Extra          map[string]ir.RawValue // free-form, forward-compatible per-target ergonomics
}
```

`Extra` is a bare raw-JSON map rather than `ir.RawConfig`: `RawConfig` names *declared protocol
configuration inside the IR* (`ir-design.md §12`), and a hint is neither — it is emitter input
authored alongside the SDK, and the IR never sees it.

Method-name *derivation* is itself an injectable policy marked `Inferred`, disable-able and
overridable. Per-target naming/compat overlays are the same shape: a emitter input keyed by IR ID,
so one IR document drives different compat baselines per language.

---

## 7. Package layout & layering

Consistent with architecture.md §3 (`emitters/*` is Layer 2: imports `ir`, `ir/irfacts` + the emitter contract,
never `compiler`, never `engine`, never a sibling target):

```
emitters/
├── contract.go        # Emitter, EmitterInput/Output, Artifact, Capabilities, TargetKey, registry
├── plan/              # SHARED, language-neutral — imports ir, ir/irfacts. Build(), Plan, OpPlan, TypePlan, ModelShape
│   └── plantest/      #   golden snapshots of Plan (language-independent corpus)
├── policy/            # runtime SDK-policy vocabulary + Defaults() + Merge(DeepPartial) (INV10)
├── naming/            # SHARED naming engine: acronym/compound tables, the character-name table,
│                      #   reserved-word set type, ID-keyed collision resolver. Casing FUNCTIONS are
│                      #   per-target (naming.Policy impl)
├── typemap/           # SHARED type table: Table, Row, lookup, merge, strict decoding (§2.6)
├── refine/            # SHARED refiner contract + code-shaping helpers more than one target uses
│                      #   (untagged-union recognition, the decorator fold, import-set harness);
│                      #   never a plan fact
├── manifest/          # generation manifest (ID-keyed), header provenance, additive-merge driver (§11)
├── verify/            # neutral Surface projection, differ, injectable per-language severity (§12)
├── golang/            # ◀ FIRST TARGET (TargetKey "go")
│   ├── emitter.go     #   New() returns the target the engine registers; plan→refine→emit
│   ├── goast/         #   typed target AST (sealed sum) + printer
│   ├── refine/        #   IR+Plan → goast — the ordered lowering pipeline (§4)
│   ├── emit/          #   goast → []byte via printer + go/format; templates/ (boilerplate only)
│   ├── naming/        #   Go reserved words + default acronym table (naming.Policy impl)
│   └── mergeadapter.go#   go/ast additive-merge adapter (registers into manifest driver)
├── kotlin/            # ◀ FIRST TARGETS (TargetKeys "kotlin-jvm" and "kotlin-multiplatform")
│   ├── emitter.go     #   NewJVM() and NewMultiplatform(): one pipeline, two Platform profiles
│   ├── platform.go    #   Platform: the built-in type table, identifier rules, transport
│   ├── ktast/         #   typed Kotlin AST (sealed sum) + printer
│   ├── refine/        #   IR+Plan+Platform → ktast — the ordered lowering pipeline (§4)
│   ├── emit/          #   ktast → []byte via a printer laying out canonical code; templates/
│   └── naming/        #   Kotlin hard keywords, the Objective-C/Swift export rules (Multiplatform)
├── docs/              # docs emitter: reuses plan + refine step library; docast + Markdown/HTML printer (§10)
└── mock/              # mock-server emitter: reuses plan; server-routing AST + printer (§10)
```

`goast` lives *under* `emitters/golang`, not in the shared contract, because a target AST is
per-language by definition. `plan`, `policy`, `naming`, `typemap`, `refine` (the contract + shared
helpers), `manifest`, and `verify` are shared because they are language-neutral: `typemap` holds a
table's shape and rules, and each target its rows. This section lays out Morphic's own packages; how
a generated SDK divides into packages of its own, by source document or by group, is §4.17's.

Kotlin/JVM and Kotlin Multiplatform are two targets and one package. They share a language, so
one AST, printer and refine pipeline serve both; they differ in a runtime, and that difference is
data: a `Platform` value carrying its built-in type table (§2.6), whose rows for the
runtime-dependent primitives differ between the two, the identifier rules the Objective-C/Swift
export adds on Multiplatform, and the HTTP engine each platform's transport defaults to. A lowering
that branches on the platform reads that value, never the target key. They are two targets rather
than one with an option because their artifacts, surfaces and toolchains differ: a JVM SDK exposes
`java.time` and `BigDecimal`, which Multiplatform common code does not have (§8.8).

**Architecture test.** The import-graph assertion (written alongside the first emitter packages, not
after) asserts: `emitters/golang` and `emitters/kotlin` may each import `emitters`, `emitters/plan`,
`emitters/policy`, `emitters/naming`, `emitters/typemap`, `emitters/refine`, `emitters/manifest`,
`emitters/verify`, `ir` and `ir/irfacts`; neither may import `compilers/*`, `engine`, or the other.
**Switch-completeness tests** live beside `goast` and `ktast` (over each one's `NodeKind`) and
beside every refiner (over every `ir.TypeKind`): adding an IR kind fails the test of every dispatch
that must handle it (the `assertNever` obligation).

---

## 8. The first targets — a Go walkthrough, and what the Kotlin targets change

The first milestone ships three targets from one plan: Go (`"go"`), Kotlin/JVM (`"kotlin-jvm"`) and
Kotlin Multiplatform (`"kotlin-multiplatform"`: JVM, Native/iOS and JS/Wasm from common code).
§8.1–§8.7 trace IR constructs through `plan → refine → emit` for Go, one per subsection, with IR
field names per `ir-design.md`. The Kotlin targets read the same plan and lower the same facts;
§8.8 gives what they decide differently, §8.9 and §8.10 trace return shapes and the client's base
URL through all three, and §9 every construct per target.

**Generated SDKs require Go 1.27 or newer.** Their `go.mod` declares `go 1.27`, and the lowerings
below use what that release provides:

- **JSON is `encoding/json/v2` and `encoding/json/jsontext`.** Go 1.27 builds them by default; Go
  1.26 builds them only under `GOEXPERIMENT=jsonv2`, a setting of the importing program's build
  environment that a library cannot make for it, and whose use outside toolchain development the
  `go` command documents as unsupported. Codecs are `MarshalJSONTo`/`UnmarshalJSONFrom` (§4.8),
  presence is `omitzero` (§4.7), and a union's fallback is `jsontext.Value` (§4.4).
- **`ir.PrimUUID` maps to the standard library's `uuid.UUID`**, new in Go 1.27, so an SDK takes no
  third-party dependency for it. It encodes as the canonical string, through its `MarshalText`.
- **The auto-pager is an `iter.Seq2[T, error]`** (§8.3), ranged over with `for item, err := range`.
- **Runtime templates are tested with `testing/synctest` and `httptest.NewTestServer`** (§13 test
  T-13), and the transport template leaves draining unread bodies to `net/http` (§5.2).

The cost is the SDK users still on Go 1.26. Go supports each major release until two newer ones
exist, so 1.26 leaves support when 1.28 ships; from then on every supported release meets the floor.

### 8.1 A model with lifecycle visibility

IR: `Model{ Properties:[id (readOnly), name (required), email, password (writeOnly, Secret)],
Additional:"closed" }`.

- **plan.** `ModelShape(User, read)` = {id,name,email}; `ModelShape(User, create)` =
  {name,email,password}. `Flattened` walks `Base`, `Implements` and `Mixins` (none here). That
  `User` is both written and read is the derived-fact index's, `Facts.Usage(User)` = Input|Output,
  which no plan field copies (§3.6).
- **refine.** `ProjectVisibilityShapes` emits `StructType{Name:"User"}` (read) and
  `StructType{Name:"UserCreate"}` (create). `LowerOptionality`: `name`, required and non-null, is a
  bare `string`, and the optional `id`, `email` and `password` are `Opt[T]` boxes (§4.7), so an
  absent email stays apart from an empty one. `RenderCasing` turns `["email"]`→`Email`, and the tag
  is `Tag:json:"email,omitzero"`: the name from `WireName`, never the identifier, and `omitzero`
  because an unset box written without it would send `"email":null`. `password` (`Secret`) gets a
  redacting `String()` and is excluded from the read shape. `Additional:"closed"` → the model's
  `UnmarshalJSONFrom` decodes its members under `json.RejectUnknownMembers(true)`. Every model pins
  that option for itself, `false` unless closed: a v2 option reaches every value below the call that
  sets it, so a closed model would otherwise make an open model nested in it strict, and a strict
  caller would reach a model the spec leaves open. A catch-all (`AdditionalProps`) is the struct's
  one embedded fallback, a `map[string]T` tagged `json:",embed"`, which receives the undeclared
  members whatever the options say.
- **emit.** Printer folds the two structs, prints through `go/format`. File `models/user.go`.
  Manifest records `User.ID → "User"` and `User.ID#create → "UserCreate"`.

### 8.2 A discriminated union (`Payment = Card | BankTransfer`, tag `type`)

IR: `Union{ Variants:[Card, BankTransfer], Exclusive:true, WireTagged:true,
Discriminator{PropertyName:"type", Mapping:{"card":Card.ID, "bank":BankTransfer.ID}} }`.

- **plan.** `TypePlan.Union` = `discriminated`, and `Discrim` maps `card` and `bank` to the two
  variants' IDs. No primary-variant collapse — the union stays whole (INV2).
- **refine.** Go has no sum type → `LowerUnions` picks the sealed-interface strategy:
  `type Payment interface { isPayment() }`, `Card`/`BankTransfer` each implement an unexported
  marker. `LowerDiscriminator` reads `Mapping` (keyed by **TypeID**) and generates
  `UnmarshalJSONFrom` that peeks the `"type"` wire key and dispatches; an unrecognized tag with no
  `Default` becomes a coded decode error, not a silent nil. Variant identity survives →
  `NewPaymentCard(...)`.
- **emit.** Printer emits the interface, markers, concrete structs, and the
  `UnmarshalJSONFrom`/`MarshalJSONTo` pair keyed on the **wire** name `"type"` (§4.8). The
  switch-completeness test guarantees every variant is handled.

### 8.3 A paginated operation (`ListUsers`, cursor pagination)

IR: `Operation{ Params:[limit, cursor], Responses:[200 → {items:[]User, next_cursor}],
Pagination{Strategy:cursor, InputCursor:{cursor}, Items:{body→items}, NextCursor:{body→next_cursor}} }`,
bound `HTTPBinding{GET /users, query limit+cursor}`.

- **plan.** `OpPlan{ Return:value, ReturnType:ListUsersResponse, Page:&PagePlan{Strategy:cursor,
  ItemType:User, ItemPath:body→items, CursorIn:cursor, CursorOut:next_cursor} }`: each page decodes
  as the response's own type, and `Page` takes the items from it. Item-type unwrap is a plan
  decision. Pacing delay is **not** here — it is runtime policy.
- **refine.** `BuildRequestShaping` builds the request from the query bindings. By the default
  parameter shape (§4.13), marked `Inferred`, `limit` and `cursor` are fields of `ListUsersParams`.
  `func (c *Client) ListUsers(ctx, *ListUsersParams) iter.Seq2[User, error]` is a range-over-func
  auto-pager: it walks pages using `CursorOut` to read `next_cursor` and `CursorIn` to feed it back,
  yields each item deserialized elementwise over `ItemPath`, yields one non-nil error and stops when
  a fetch fails, and fetches no further page once the caller's loop breaks.
- **emit.** Printer emits `ListUsers` and `ListUsersParams`; the pager needs no named iterator type.
  The pager *runtime* (fetch-and-pace loop) is `pagination.go.tmpl` parameterised by
  `Policy.Pagination.AutoPageDelay` — the legitimate boilerplate seam.

### 8.4 A streaming operation (`StreamEvents`, server-sent events)

IR: `Operation{ Streaming:server, ResponseStream:&StreamDetail{ Events: &TypeRef→EventUnion } }`
where `EventUnion = Union{ WireTagged:true, Variants:[Message{Event}, Heartbeat{Event, Terminal:false},
Done{Event, Terminal:true}] }`, bound `HTTPBinding{GET /events}` with the SSE wire mechanism on the
binding.

- **plan.** `OpPlan{ Return:stream, Stream:&StreamPlan{Direction:server, EventUnion:EventUnion.ID} }`.
  Per-event content-types and the terminal bit come from `Variant.Event`.
- **refine.** The event union lowers exactly like §8.2 (sealed interface `Event` with concrete
  `MessageEvent`/`HeartbeatEvent`/`DoneEvent`). Server streaming lowers to
  `func (c *Client) StreamEvents(ctx) (*EventStream, error)` — no request type, since the operation
  has no input (§4.13) — where
  `EventStream.Recv() (Event, error)` returns the sealed-interface event and returns `io.EOF` after a
  variant whose `Variant.Event.Terminal` is true. Direction (server) is read from the *core*; the SSE
  framing is read from the *binding* — a bidi op (`Streaming:bidi`, both `RequestStream` and
  `ResponseStream`) would additionally emit a `Send(...)` method, and a gRPC binding of the same core
  would reuse the same `EventStream` shape over length-prefixed frames.
- **emit.** Printer emits `StreamEvents`, `EventStream`, and the event union.
  The stream *runtime* (SSE frame reader) is `stream.go.tmpl` boilerplate behind the `Transport`
  interface.

### 8.5 An error taxonomy (`RateLimited`, 429, retryable-throttling)

IR: `ErrorCase{ Name:{Hint:"429"}, Conditions:{429}, Payload:{Contents:[{application/json →
RateLimited (Model, reached from this error case)}]}, Headers:[Retry-After], Fault:"client",
Retryable:&true, Throttling:&true }`; policy default maps `429 → "TooManyRequests"`.

- **plan.** `OpPlan.Errors` = `[PlannedError{ Conditions:{429}, Type:RateLimited, Fault:"client",
  Retryable:true, Throttling:true }]` — declared IR facts, carried as such. **Known gap (#447):**
  `PlannedError` has no `Headers` field, so the `Retry-After` on the IR line above has no home in
  the plan, and its single `Type` names no election where `ErrorCase.Payload.Contents` is plural —
  the negotiation `PrimaryContent` states for the success side has no error-side counterpart here.
- **refine.** `LowerErrors` builds the error tree: `APIError` interface → a `ClientError`/`ServerError`
  split from `Fault` → concrete `RateLimited` implementing `error`. The retry wiring reads
  `Retryable`/`Throttling` **from the IR fact** and falls back to `Policy.Retry` only where the spec
  is silent (§6.3); throttling selects the throttling backoff class. The `Error` suffix is a
  `naming.Policy` rendering of the neutral taxonomy — Python would render `TooManyRequestsError` from
  the same neutral kind.
- **emit.** Printer emits the error type + `Error() string`, registers `429 → *RateLimited` in the
  response decoder, and the retry runtime consults `IsRetryable()`/`IsThrottling()` which return the
  spec-declared values.

### 8.6 Auth (OR-of-ANDs, service default with an op override)

IR: `Service.Auth = [ AuthRequirement{Schemes:[apiKey]} ]`;
`Operation.Auth = [ AuthRequirement{Schemes:[oauth2(scopes:[read])]},
AuthRequirement{Schemes:[apiKey]} ]` (priority order); auth schemes in `Document.Auth`.

- **plan.** `OpPlan.Auth` = the operation's options, priority-ordered (the op overrides the service
  default). An empty option inside a non-empty list = "no auth is one acceptable choice"; an empty
  `Operation.Auth` slice would mean "explicitly public".
- **refine.** Auth *schemes* are structural IR, so refine emits an `Authenticator` interface, a
  per-scheme option type (`WithAPIKey(...)`, `WithOAuth2(...)`), and the request-time application
  code that satisfies the AND-within-option / OR-across-options-by-priority contract. Scope sets from
  `oauth2` flows reach the option types. What refine does **not** emit is any credential value or
  token-refresh behavior — the actual secrets and token acquisition are runtime configuration, not
  IR. SASL/x509/user_password schemes (Kafka/AMQP) lower the same way for a messaging target.
- **emit.** Printer emits the `Authenticator` interface, the option constructors, and the per-request
  auth selector.

### 8.7 A long-running operation (`StartExport`, status-monitor polling)

IR: `Operation{ LongRunning:&LongRunning{ FinalStateVia:"status-monitor", PollingOperation:GetExport.ID,
FinalType:&TypeRef→Export, ResultPath:body→result }, Request:{ExportRequest} }`, bound
`HTTPBinding{POST /exports}`; the
declared poll op `GetExport` is `HTTPBinding{GET /exports/{id}}` returning an `ExportStatus` monitor.

- **plan.** `OpPlan{ Return:value, LRO:&LROPlan{ FinalStateVia:"status-monitor",
  PollOp:GetExport.ID, FinalType:Export, PollType:ExportStatus, ResultPath:body→result } }`. Which
  response signals terminal state, and where the result lives, are plan facts unwrapped from the
  monitor type — the same "classify, don't decide" split pagination uses. Poll *pacing* is **not**
  here; it is runtime policy, exactly like `Pagination` pacing (§8.3).
- **refine.** `LowerLRO` emits a poller:
  `func (c *Client) StartExport(ctx, *StartExportParams) (*ExportPoller, error)`, where
  `ExportPoller.Poll(ctx) (bool, error)` performs one poll via the `PollOp` binding and
  `ExportPoller.PollUntilDone(ctx) (*Export, error)` loops until terminal and extracts `Export` from
  `ResultPath`. `FinalStateVia` selects how the poller finds the monitor URL (status-monitor vs
  `operation-location` header vs original-URI). The poller *type* and result extraction are typed AST;
  the poll loop's backoff between attempts is `lro.go.tmpl` boilerplate parameterised by policy — the
  same structure/runtime line as the iterator (§8.3) and the stream reader (§8.4). Smithy waiters are
  *not* folded in here: their acceptor/JMESPath lists arrive verbatim in `Operation.Unmodeled`
  (`ir-design.md §7.3`) and a waiter-aware refiner is a separate, opt-in step.
- **emit.** Printer emits `StartExport`, `StartExportParams`, and `ExportPoller` +
  `Poll()/PollUntilDone()`. `OneWay` operations take the opposite path — no poller, no response
  binding, a fire-and-forget method — read straight from `Operation.OneWay`.

### 8.8 The Kotlin targets

Both Kotlin targets build the walkthrough's constructs from the same plan facts; what differs is
the runtime they target and the language's own rules.

**Generated Kotlin SDKs require Kotlin 2.4 or newer**, on both targets, and their build files say
so. 2.4 is the first release whose standard library's `kotlin.uuid.Uuid` is stable, and
`kotlin.time.Instant` became stable in 2.3, so no type an SDK exposes needs an opt-in. That is the
rule the floor serves: an opt-in API in a public signature makes every caller opt in too. The floor,
and the `kotlinx.serialization` and `kotlinx-datetime` releases beside it, move only with an options
version (§2.5).

**Models and request types are classes, not data classes.** A data class's constructor and `copy`
change signature with every property it gains, so a property the spec adds would break every JVM
binary compiled against the SDK, and Swift, which sees no default arguments, would make every
caller pass it. Kotlin's guidance for library authors keeps data classes out of a public API for
the first reason. Each is a final class instead:

1. A model's properties are public `val`s and its constructor is not public. A nested `Builder`
   takes the required properties in its constructor and the optional ones through setters. A Kotlin
   caller writes `User(name = "Ada") { email = "ada@example.com" }` over it; a Swift caller uses the
   builder itself.
2. `equals`, `hashCode` and `toString` are generated, and `toString` redacts a `Secret` property or
   a `Sensitive` type (§8.1), which a data class's would print.
3. An operation's request type (§4.13) is built the same way, so an optional property or input the
   spec adds is a new setter on both platforms.

A required property the spec adds changes the builder's constructor, as it changes the API.

**Serialization and calls.**

- Serialization is `kotlinx.serialization` on both. Multiplatform has no reflection, which rules
  out a reflective JSON library, and the JVM target sharing it is what lets one refine pipeline
  serve both. Codecs are generated `KSerializer`s, the counterpart of §4.8's
  `MarshalJSONTo`/`UnmarshalJSONFrom`.
- Unknown members follow §8.1's per-model rule: the SDK's `Json` is strict, and each model the spec
  leaves open carries `@JsonIgnoreUnknownKeys`, so a closed model nested in an open one stays strict
  and the reverse stays open. A model with a catch-all (`AdditionalProps`) is the exception: the
  annotation would drop the undeclared members before its serializer saw them, so its serializer
  reads the object whole through `JsonDecoder.decodeJsonElement` and folds them into the map, as
  Go's embedded fallback receives them whatever the options say.
- Operations are `suspend` functions, pages and server streams are `Flow`s, and the transport sits
  behind the runtime's `Transport` (§5.4), a Ktor client by default on both. Java callers are not a
  goal of the JVM target at M3: its surface is Kotlin's.
- A discriminator's tag is the wire string the mapping names, a numeric-looking one included
  (`"10016"`): a `KSerializer`, like Go's `UnmarshalJSONFrom`, compares it as a string and never
  parses it as a number.
- A merge-patch body (`application/merge-patch+json`) is where all three of an optional field's
  states change the request: absent leaves the member as it is and `null` removes it. Every target
  sends both through the presence box (§9: `Optional<T?>` on Kotlin, `OptNil[T]` on Go), never
  through a nullable alone.

**Multiplatform common code** has no `java.time`, no `BigDecimal`, no reflection and no `java.io`.

- An exact number — `decimal`, `decimal128`, or an `integer` or `number` whose type-table row keeps
  it exact (§2.6) — is the runtime's `Decimal`, a value class over the decimal text: every digit
  round-trips, it does no arithmetic, and a caller on the JVM converts it to `BigDecimal`.
- A `datetime_offset` is the runtime's `OffsetDateTime`, a `kotlinx-datetime` `LocalDateTime`
  beside the `UtcOffset` it was written with, which converts to an `Instant`. The offset survives,
  as it does in Go's `time.Time` and the JVM's `OffsetDateTime`, and re-encoding writes it back.

**Swift.** A Multiplatform SDK reaches iOS through the Objective-C export, which shapes three rules.

- A sealed interface is not exhaustive in a Swift `switch`, so Swift callers need a `default` arm,
  and the generated docs say so.
- A name the export would mangle — `description` and `hash` (`NSObject` members), a name in the
  `init`, `new`, `copy` or `alloc` method families, a Swift keyword — gets an explicit Objective-C
  and Swift name from the naming policy (`@ObjCName`), so the Swift name is the policy's choice
  rather than the exporter's.
- Default arguments do not reach Swift, which is one reason models and request types are built
  through a builder rather than a constructor with defaults.

**Formatting.** Go formats in process with `go/format` (§5.1). Go can call no Kotlin formatter, and
`Generate` does no I/O, so the Kotlin printer lays out canonical code itself, and the Kotlin
formatter runs as a check in the verify stage (#704) rather than inside the emitter.

**Identifiers.** A Kotlin hard keyword used as a name (`class`, `object`, `true`) is escaped with
backticks, which Kotlin allows on both platforms and which leaves the wire name alone; a name a
scope reserves for what the generated code binds (§4.12), which backticks would not set apart, takes
a trailing underscore. Go's casing lifts exported names clear of its keywords, so only locals and
parameters can meet one there: a keyword (`type`, `default`), a predeclared identifier a parameter
would shadow (`string`, `error`), a package its file imports (`time`, `url`) or a name the method's
body binds (`ctx`, `params`) takes a trailing underscore, and so does an exported name the target
declares itself (`Client`). Names that collide once cased are `Ctx.Syms`'s on every target (§4.12).

**Inheritance.** Go has only embedding. Kotlin's single class inheritance cannot hold several
`Mixins`, and extending a base would make it an open class the SDK must keep compatible for every
subclass. So on all three targets a model's `Base` and `Mixins` are flattened into it
(`TypePlan.Flattened`). Subtyping survives as interfaces: `Implements` becomes an interface, and a
discriminated or abstract base a sealed interface its subtypes implement.

**Scale.** The JVM caps a method at 255 parameter slots and 64 KiB of bytecode, and a class at
65,535 constant-pool entries. A refiner checks each declaration against its target's limits and
lowers past them to a shape that stays inside, or diagnoses what it cannot: the decoder of a model
with hundreds of properties is split across methods, and a builder whose constructor would pass the
parameter cap is a diagnostic. Since every model is built through a builder, none changes shape
when a property it gains crosses a limit. Go has no such caps, but one package of tens of thousands
of types builds slowly and in gigabytes of memory, which §4.17's size check reports and its split by
group bounds. On Multiplatform, Kotlin/Native compile time and the size of the iOS framework grow
with every declaration the SDK exports, so the runtime's helpers are `internal`, and the same split,
whose units are Gradle modules, bounds what one module exports.

### 8.9 Return shapes

What a success body is read as is `OpPlan.Return` (§3.1). §8.3 and §8.7 read a model as a `value`,
behind a pager and a poller, and §8.4 a `stream`; the kinds a `value` holds besides a model, and the
`raw` and `void` shapes, lower on the three targets as follows.

- **A scalar, a list, a map or a union** (`GET /count` → `integer`, `GET /labels` → a map of
  strings, `GET /pet` → a `oneOf`). **plan.** `Return:value`, `ReturnType` the content's type.
  **refine.** The method returns that type as any field of it lowers (§9): Go
  `func (c *Client) Count(ctx) (int64, error)`, `Labels` returning `map[string]string`, `GetPet`
  returning the sealed interface `Pet`; Kotlin `suspend fun count(): Long`, `Map<String, String>`,
  the sealed interface `Pet`. A nullable return takes the target's nullable form.
- **A file** (`GET /file`, `application/octet-stream`, `format: binary`). **plan.** `Return:raw`,
  since the compiler marks the content a file (`Content.File`). **refine.** Go
  `func (c *Client) Download(ctx) (*RawBody, error)`, where the runtime's `RawBody` holds the
  response's `io.ReadCloser` and the media type it arrived as, and the caller closes it; Kotlin a
  `suspend fun download(): RawBody` over the transport's byte channel. Nothing decodes or buffers
  the body, so a large download streams.
- **No body** (`DELETE /pet/{id}` → `204`). **plan.** `Return:void`. **refine.** Go returns `error`
  alone, Kotlin `Unit`.
- **No success response** (`POST /purge`, whose only response is `default`). **plan.**
  `Return:void`: the method returns only an error, and a status no declared response or error case
  matches is the error taxonomy's to classify (§8.5). Once the IR says whether a lone `default` is a
  success response (#608), this line reads it as any other.
- **An anonymous decorator** (`200 → {allOf: [{$ref: Zone}], description: …}`). **plan.**
  `Return:value`, `ReturnType` the decorator, as the content names it, whose `TypePlan.Alias` is
  `Zone` (§4.15). **refine.** `CollapseAliases` lowers the reference to `Zone`, so the method
  returns `*Zone`; the description, which no return position holds, stays in the IR for the docs
  emitter and is reported once (§4.15).

### 8.10 The client and its base URL

IR: `Document.Servers = [ {URLTemplate:"https://{region}.api.example.com/v1",
Variables:[{Name:region, Default:"us", Enum:[us, eu]}]} ]`, and a service whose `Servers` is empty.

- **Which servers.** A service's servers are those its `Servers` indexes, or, where it indexes
  none, every server in `Document.Servers`, in order (ir-design §10). The client's default is the
  first of them a client can reach (below). The IR states both, so the plan copies neither (§3.6).
- **refine.** The client takes three settings beside its runtime policy: which declared server to
  use, the default unless the caller names another; a value for each of that server's variables, its
  `Default` unless the caller gives one, and one of its `Enum` when it has one, each value of which
  is a constant the caller can name; and a base URL, which replaces the server's URL entirely, for a
  proxy, a test server or a deployment the document does not list. Go's are functional options —
  `NewClient(WithRegion(RegionEU))`, `WithServer(…)`, `WithBaseURL(u)` — and Kotlin's are setters on
  the client's builder. A value outside a variable's `Enum` is refused when the client is built,
  never sent. An empty `Default` substitutes nothing, the one reading OpenAPI admits, since it
  requires a default; telling an empty default from none, which AsyncAPI allows, is #883's.
- **A server a client cannot reach.** A server whose URL, its variables at their defaults, has no
  scheme and host — the `/` the compiler lowers for a document that declares no server (#672)
  among them — cannot be the default: the document's location, which OpenAPI resolves a relative
  URL against, is not something an SDK knows. The default is the first server that has both, so a
  relative server listed first passes the default to the next. A client refuses to be built while
  the server it would use has neither and no base URL replaces it, as when a caller names a
  relative server, and refine reports a client none of whose servers has both once, as an info
  `emitters/<target>/no-default-server`.
- **An operation's own servers.** OpenAPI lets a path item or an operation name servers of its own,
  which the IR keeps under `no_ir_home` until an operation gets a field for them (#285), so an SDK
  reads none of them (§4.14) and sends those operations to the client's base URL; the compiler
  reports each. Once the field exists, an operation's servers replace its service's for that
  operation, by the same rules.
- **Auth.** An HTTP target authenticates as §8.6 says, from the operation and the service.
  `Server.Auth` is the placement the messaging formats use, where a broker connection authenticates
  per server, and a messaging target reads it.
- **emit.** The URL template is expanded at request time from the chosen values; the template and
  each variable's constants are typed AST, the expansion a runtime helper.

---

## 9. The lowering table (normative, per first target)

The complete emitter acceptance surface is `ir-design.md §4–§9`; this table is the condensed
normative mapping for the three first targets. Every row must have an emit path or a diagnosed
degrade on each; a missing row is an IR bug (INV9), not a compiler change. The Kotlin column holds
for both Kotlin targets, and "KMP:" marks where Multiplatform differs from the JVM (§8.8).

| IR construct | Go | Kotlin (JVM and KMP) | Wire truth preserved |
|---|---|---|---|
| fixed-width `Primitive` (`int8`…`uint64`, `float32`, `float64`) | the same-named Go type | `Byte`…`Long`, `UByte`…`ULong`, `Float`, `Double` | — |
| unbounded `Primitive` (`integer`, `number`, `float`) | `int64`, `float64` by the built-in type-table rows (§2.6), marked `Inferred`; exact where a row maps them so: `*big.Int`, the runtime decimal | `Long`, `Double` by the same rows; exact where a row maps them so: JVM `BigInteger`/`BigDecimal`, KMP the runtime `Decimal` | every digit, when exact |
| `decimal`, `decimal128` | the runtime decimal type, over `math/big` | JVM `BigDecimal`; KMP the runtime `Decimal` | every digit |
| `bool`, `string`, `bytes`, `url` | `bool`, `string`, `[]byte`, which json/v2 writes as base64, `string` | `Boolean`, `String`, `ByteArray` through the runtime's base64 serializer, `String` | bytes as base64 text, where `kotlinx.serialization`'s own `ByteArray` serializer writes an array of numbers; a URL's text, never re-serialized from a parsed form |
| `uuid` | the standard library's `uuid.UUID` | the standard library's `kotlin.uuid.Uuid` on both (§8.8) | canonical text |
| `date`, `time`, `datetime`, `datetime_offset`, `duration` | runtime `Date`, `TimeOfDay`, `DateTime`; `time.Time`; a runtime ISO 8601 `Duration` | JVM `LocalDate`, `LocalTime`, `LocalDateTime`, `OffsetDateTime`; KMP `kotlinx-datetime`'s `LocalDate`, `LocalTime`, `LocalDateTime`, the runtime `OffsetDateTime` (§8.8); a runtime ISO 8601 `Duration` on both | the offset a timestamp was written with; a duration's years and months, which neither language's own duration holds |
| `any`, `Any` | `jsontext.Value`, which keeps the bytes, distinct from a diagnosed unknown | `JsonElement` | every byte; decoding into Go's `any` would turn each number into a `float64` |
| nil `*TypeRef` | void | `Unit` | — |
| `Scalar{Base}` chain | over the chain's root (`TypePlan.Scalar`), typed by the type table's row for the root and the chain's encoding (§2.6): a defined type with a `Validate` for merged constraints where the root's type has no methods (a predeclared type, a slice, a map), an alias (`type X = Root`) over any other, its constraints checked where it is used (§4.9); nil base → newtype | `@JvmInline value class` over a `Boolean`, number, `String` or `ByteArray` root, a `typealias` over any other (§4.9); nil base → value class over the wire form | encoding triple; the root's codec |
| `Model` (Base/Implements/Mixins) | struct with `Base` and `Mixins` flattened in; `Implements` → interfaces; `Abstract` → interface; `Positional` → index-ordered struct | final class with `Base` and `Mixins` flattened in, built through a builder, never a `data class` (§8.8); `Implements` → interface; discriminated or abstract base → sealed interface | `WireID` order |
| anonymous decorator: a `Model` with only a `Base`, or a `Scalar` with neither `Constraints` nor `Encoding`, beside annotations (§4.15) | no declaration: each reference lowered as one to the first type along its `Base` that is not a decorator, its docs, deprecation and examples folded onto the carrier | the same | — |
| `Model` whose `Base` is no model, not a decorator | with no property, catch-all or discriminator of its own, lowered as the `Scalar` row lowers an alias over that base, its constraints, docs and examples binding the position (ir-design §4.2); a union base beside properties distributed over the variants as ir-design §4.3 distributes a union co-declared with a composition; anything else a diagnosed degrade | the same | — |
| `AdditionalProps` / `Additional` | catch-all `map` as the embedded fallback; `closed`→`RejectUnknownMembers(true)`, pinned per model (§8.1) | catch-all `Map` filled by a serializer that reads the whole object (§8.8); strict `Json`, `@JsonIgnoreUnknownKeys` on each other open model | pattern channel |
| `Union` (four wire shapes, §4.4) | sealed interface + marker + concrete variants + tag-dispatch (de)serializer | sealed interface + one wrapper class per variant + a `KSerializer` per wire shape; KMP: not exhaustive in Swift | `WireName`/`WireID` per variant |
| `Enum` open/closed/flags | consts named by enum then member (`StatusActive`, §4.5): closed→typed consts; open→string+consts+`Unknown`; flags→bitset; `FallbackMember` on decode | closed → `enum class` with `@SerialName` members, `FallbackMember` on decode; open → value class over the value type with a constant per member, which keeps an unknown value; flags → value class over an integer | member `WireName` |
| `List`/`MapT`/`Tuple` | `[]T`/`map[K]V`/positional struct (optional past `MinItems`, `Rest`→trailing slice); `List.Encoding`→serializer mode | `List<T>`/`Map<K, V>`/a class with a positional serializer (nullable past `MinItems`, `Rest` → trailing `List`) | packed/expanded |
| `Literal` | typed constant (incl. `symbol`) | `const val`, or an `object` for a non-primitive value | — |
| `External` | the type-table row for its `Identity` (§2.6): imported library type + dep-manifest; no row → opaque handle, passthrough + diagnostic | the type-table row for its `Identity`, each platform's own; no row → diagnostic | — |
| `Visibility` sets | N structs from `ModelShape`; PATCH → optional | N classes from `ModelShape`; PATCH → optional | — |
| `Required` ⊥ `Nullable` ⊥ `Presence` | `T`/`Opt[T]`/`Nil[T]`/`OptNil[T]`/`omitzero`+presence; collapse→diagnostic; a reference on a cycle of references held by value `*T`, or `Opt[*T]` when optional and nullable (§4.16) | `T` / `T?`, left out when null / `T?`, written as null / runtime `Optional<T?>`, absent or null or a value; collapse → diagnostic | — |
| `Constraints` | field `validate` tags + refine-built `Validate()`; unrepresentable regex→diagnostic | a generated `validate()`, never an `init` check, which would refuse a response; unrepresentable regex → diagnostic | numeric bounds as decimal |
| `Discriminator{Mapping→ID}` | tag-dispatch `UnmarshalJSONFrom` + factories | tag-dispatch `KSerializer` that reads the wire key first | discriminator wire key |
| `Pagination` (PropPaths) | `iter.Seq2[T, error]` auto-pager; item unwrap from plan; pacing from policy | `Flow<T>`; item unwrap from plan; pacing from policy | path-based item/cursor read |
| `Streaming` + `StreamDetail` | `EventStream.Recv()`/`Send()` over event union; terminal ends; direction from core, framing from binding | `Flow` of events received, a send channel for a client stream; the same sources | per-event content-type |
| `LongRunning` | poller type + `Poll()`/`PollUntilDone()`; result from `ResultPath`; pacing from policy | poller with `suspend` `poll()`/`pollUntilDone()`; the same sources | monitor/final-state channel |
| `OneWay` | fire-and-forget method, no response binding | `suspend` function returning `Unit`, no response binding | — |
| `Channel`/`Message`/`MessageBinding` | `MsgView` → channel client + send/receive + reply routing (messaging target) | the same, over `Flow` | address/correlation paths |
| `ErrorCase{Fault, Retryable}` | error tree; declared facts win over policy | exception hierarchy (`ApiException` → client/server → concrete); declared facts win over policy | status→error map |
| `Auth` OR-of-ANDs | `Authenticator` + per-scheme options; priority order | `Authenticator` + per-scheme configuration; priority order | — |
| `Value{Num BigVal, Ctor, Ref, symbol}` | literals via decimal (never float64); `Ctor`→call; `symbol`→string explicitly; a `Default` documented, never written onto a request (§4.10) | literals built from the decimal text, never a `Double` literal; `Ctor` → call; `symbol` → string explicitly; a `Default` documented, never sent | — |
| `Naming{Canonical}` | cased via `naming.Policy`; a name with no words named by its characters, and one that would begin with a digit led by its role's word (§4.12); a name a scope reserves escaped with a trailing underscore; collisions ID-keyed | cased via `naming.Policy`, characters named and leading digits led the same; hard keywords in backticks, other reserved names a trailing underscore; KMP: explicit Objective-C/Swift names where the export would mangle; collisions ID-keyed | `WireName`/`WireID` never cased |

---

## 10. Docs and mock-server emitters reuse the contract

Both are `Emitter` implementations consuming the **same `Doc + Plan`** — the payoff of computing the
plan once: docs and mocks agree with the SDK by construction, not by re-derivation.

- **Docs emitter (`emitters/docs`).** Reuses the shared plan verbatim (primary response/content,
  pagination, return-shape, error taxonomy — exactly what docs must describe) and most of the refine
  step library, but lowers into a `docast` (pages, operation blocks, schema tables, examples) and
  runs a **reduced pipeline**: no `EscapeReservedWords`, and casing is a presentation choice (SDK-style
  identifiers or spec-faithful source names, set in its options). `Docs.Description`
  `{t:TypeID}` cross-reference tokens resolve to intra-doc links via the *same* IR-ID lookup the Go
  emitter uses for doc comments — links survive renames. Examples use the schema arm (`Value/Headers`)
  vs the operation arm (`Input/Output/Error`) correctly. No runtime `Policy` is consulted — proving
  the IR/policy split pays off.
- **Mock-server emitter (`emitters/mock`).** Reuses `PrimaryResponse`/`PrimaryContent` and the IR
  `Examples` to synthesize canned responses, the error taxonomy to simulate declared failures, the
  `BindingView` to route requests, and the visibility projections to honor the same wire shapes an
  SDK sends/receives. Refine lowers into a server-routing AST (routes from `HTTPBinding.URITemplate`,
  handlers validating against constraints). Because the same plan feeds SDK and mock, the two are
  **wire-consistent by construction** — the mock can *be* the interception target the SDK's
  wire-conformance harness (§13) is diffed against.
- **Validation emitter (future).** Additionally consumes the validation-only constructs kept
  verbatim in `Unmodeled` (`not`/`if-then-else`/`dependentSchemas`, …) that SDK/docs emitters
  ignore. It selects them by `UnmodeledEntry.Reason == ir.ReasonValidationOnly` rather than by key
  prefix, so vendor metadata sharing the same map never reaches it, and reports each finding at
  `UnmodeledEntry.Provenance` — the keyword's own position, not the enclosing schema's. The
  contract already carries all of that; no IR change is needed (INV9).

Adding any of these is a registry entry over one `Doc + Plan` — no change to the ABI, no change to
the IR.

---

## 11. Write / integrate

Post-milestone (architecture.md §2.3), but the contract is designed so it bolts on with **no IR
change** (INV9). The `manifest.Manifest` and `Artifact` fields returned from `Generate` are the
seams; the `Origin`-tagged AST makes the manifest a byproduct of the emit walk.

- **Generation manifest keyed by IR stable IDs.** Records spec/plan/emitter/config **hashes**,
  a **sorted** file list (deterministic diffs), and an **entity → generated-symbol map keyed by
  `TypeID`/`OpID`/`PropID`** — assembled from the `Origin` on every emitted node. This is the biggest
  win of stable-ID keying: a spec rename is "same ID, new symbol" — a lookup, not oagen's structural
  rename-inference machinery. Write/integrate and surface verification (architecture.md §2.3) both
  rest on it.
- **File-header provenance gates pruning.** `Artifact.Header` writes a generated-by header; the
  writer **never deletes a file lacking it** (hand-edited). First adoption (no prior manifest) skips
  pruning, writes a baseline, emits a notice.
- **Ignore regions & additive-only merge.** Two provenance markers — whole-file
  (`@morphic:ignore-file`) and region (`@morphic:keep`/`:end`) — with append-only merge. The
  structural merge is **language-parameterised** by an adapter registered into the manifest driver;
  the Go adapter (`emitters/golang/mergeadapter.go`) uses `go/ast` to parse existing files and append
  only new top-level symbols, never delete. No adapter ⇒ skip, never clobber. Case-only renames go
  via a temp name (APFS/NTFS hazard). The parser Kotlin's own tools rely on is the compiler's front
  end, which runs on the JVM, so the Kotlin targets' adapter is a JVM helper the engine runs, since
  the engine owns all I/O. Until it exists the Kotlin targets merge nothing and report
  `Capabilities.WriteIntegrate` false: pruning stays header-gated and no hand-written file is
  touched.
- **Staleness check.** `(prior-manifest entities − current entities) ∩ files-on-disk` surfaces
  dead code additive writers cannot prune — cheap because both manifests are ID-keyed and the prior
  IR/plan snapshot is serializable (INV7).

---

## 12. Surface verification / compat

Post-milestone. `EmitterOutput.Surface` is the neutral API-surface projection — classes, interfaces,
enums, methods, fields as neutral records — built by the **same walk** over `Origin`-tagged AST nodes
that emit performs.

- **Neutral projection with IR IDs.** Both the freshly-generated SDK and an extracted
  existing SDK (via a per-language extractor) project into one `verify.Surface`; each record carries
  its **IR stable ID** (threaded from `Origin`), so cross-language findings correlate without
  name-matching, and rename detection is an ID lookup rather than oagen's Jaccard-similarity
  heuristics. `Generate` returns the generated side directly; only the extractor parses source.
- **Injectable per-language severity.** The differ emits **neutral change records**; a
  per-target `verify.Severity` function decides breaking/additive (parameter names are public API in
  PHP, arity in Go, ~nothing in JS) — heuristics-as-policy (INV6).
- **Extractors for the first targets.** Go's reads the existing package's exported API, as
  `apidiff` does. The Kotlin targets read the ABI dumps the Kotlin Gradle plugin's ABI validation
  writes: the JVM's class-file signatures, and for Multiplatform the KLib ABI plus the generated
  Objective-C header, since a change Swift sees can break callers the Kotlin ABI does not show.
  Kotlin's severity differs from Go's in two places: a parameter added with a default leaves
  Kotlin callers compiling but changes the JVM signature, which is binary-breaking, and on
  Multiplatform a rename only the Swift header shows still breaks Swift callers.
- **Behavioral channel separate from structural.** A default-value change is behavioral,
  tracked in a distinct channel from a signature change — detectable without re-parsing because the
  plan already carries defaults as typed `Value`s. Every finding carries drift provenance (which
  stage caused it).

---

## 13. Mandatory emitter tests

Built as the code lands, extending the testing strategy in `architecture.md §5`. Cited as `T-N`
throughout this document.

1. **T-1 · TypeKind switch-completeness** — a test over every `ir.TypeKind` and every target-AST
   `NodeKind`, its kind lists held to what the packages declare; adding a kind fails every dispatch
   that misses it (`assertNever`).
2. **T-2 · Union tag-mode coverage** — all four wire shapes emit distinct (de)serializers.
3. **T-3 · Four-state field matrix** — required×nullable×presence produce distinguishable
   declarations, or a diagnosed collapse.
4. **T-4 · Wire fidelity** — serialized output uses wire names/IDs, never rendered identifiers;
   positional models serialize by index; `WireID==0` ≠ absent.
5. **T-5 · No-float64** — numeric defaults/constraints/enum values round-trip through `BigVal` with
   no float parsing, including any constraint-merge path.
6. **T-6 · Determinism / golden IR→artifact** — identical IR ⇒ identical bytes; a scoped run equals
   the corresponding slice of a full run.
7. **T-7 · Diagnostic-not-silence** — every un-lowerable construct emits a coded diagnostic, never a
   silent `any` and never a panic, a fact outside M3's scope included (§14.1).
8. **T-8 · Manifest ID-keying** — the entity→symbol map survives path/name churn.
9. **T-9 · Policy precedence** — declared `Retryable`/`Fault`/`Idempotency` override policy defaults.
10. **T-10 · Step-trace golden** — the ordered refine step list produces a pinned, deterministic
    trace (guards implicit inter-step coupling).
11. **T-11 · Architecture import-graph** — the layering in §7.
12. **T-12 · Wire-conformance (milestone 3)** — expected request shapes derived from `Doc + Plan`
    alone, diffed against the generated SDK under HTTP interception; request-side mismatches block,
    response-side inform.
13. **T-13 · Runtime determinism** — the retry, backoff and timeout templates run inside a
    `testing/synctest` bubble against `httptest.NewTestServer`, whose in-memory network works within
    the bubble: a backoff schedule is asserted exactly under the fake clock and completes instantly,
    and `synctest.Sleep` advances the clock and waits for the runtime under test to settle.
14. **T-14 · Runtime behavior against a shared server, with a coverage gate (milestone 3)** — every
    target's generated SDK runs one hand-written suite of behaviors against one behavior server,
    and a gate fails a target that silently skips one (§13.1).
15. **T-15 · Unmodeled routing** — a document carrying an `Unmodeled` entry of each reason, on a
    type an operation reaches, on the operation and on the document, generates byte-identical
    artifacts and the same diagnostics as the document without them, unless a policy names an
    entry's key (§4.14).
16. **T-16 · Decorator spellings agree** — a 3.0 `allOf` decorator and the 3.1 `$ref` carrying the
    same annotations beside it, at a property, a parameter, a header, a union variant, a list
    element, a map value and a content, generate byte-identical artifacts (§4.15), a variant's name
    excepted until #1018 lands. Beside it, the plan's test enumerates the fields of `Model`,
    `Scalar` and `TypeCommon` by reflection and fails on one §4.15 has not classified.
17. **T-17 · Type table** — each target's built-in table has, at every options version, a row for
    every `PrimKind` `ir` declares, read through `internal/decls`; a user row replaces the built-in
    row under its key whole; an encoding with no row falls back to its primitive's and is reported
    once; a row naming an import `Imports` does not bind, an `Imports` key it does not use, or a Go
    `Codec` is an options error; and two rows that spell one name for two packages generate code
    that compiles in a file using both (§2.6).
18. **T-18 · Placement** — adding a type no operation reaches, an operation that reaches no existing
    type, or declarations past a unit's size limit moves no existing declaration to another unit,
    while an operation in another group moves to the shared unit exactly the types it makes a second
    group reach; under `SplitByGroup` the units' imports form no cycle, and groups whose names
    render alike, or as a name the units' scope reserves, get distinct units; and every partition
    error and warning of §4.17 has a fixture that draws it.
19. **T-19 · Reserved scopes** — a fixture naming its properties, parameters, path parameters and
    enum members after every name each scope of §4.12 reserves, after characters alone and after
    names that begin with a digit, and its groups after the standard-library packages its files
    import, generates code that compiles on every target, with every template's declared names held
    to what it binds.
20. **T-20 · Recursive types** — a model that holds itself directly, through a base, through a
    tuple and through another model, and one whose cycle passes through a list or a union, generate
    code that compiles on every target, and exactly the references on a cycle Go holds by value are
    held through a pointer (§4.16).
21. **T-21 · Declarations keep their root's codec** — a named scalar over every primitive, and over
    a model, an enum and a union, encodes and decodes each value byte for byte as its root does on
    every target, and a constraint the scalar declares is still checked (§4.9).

### 13.1 The behavior server (T-14)

T-12 checks the requests an SDK builds and T-13 the timing of its runtime, both inside the process.
Much of what an SDK does shows only against a live server: honoring `Retry-After`, an event stream
split across chunk boundaries or carrying one very large event, JSON Lines framing, each pagination
strategy, status-monitor polling, OAuth token acquisition and refresh, an error body that does not
parse, and the four combinations of required and nullable in a response (§4.7). Writing those
tests once per target does not scale, and nothing would notice a target that left one out.

- **One server, shared by every target.** It has one route per behavior and is written by hand,
  together with the OpenAPI document that describes it, from which each target generates the SDK
  under test. It is deliberately not the mock-server emitter's output (§10): that mock is
  wire-consistent with the SDK by construction, so it would share any misreading of the spec the
  generator makes, and a test against it could not disagree with the code it tests. Each test gets
  a session of its own, so tests run in parallel, and the server records the requests each session
  received.
- **Target-neutral definitions.** Each behavior is an Arazzo workflow: the operation, the request,
  and the success criteria, written by hand. Expectations are not derived from the spec's own
  examples, which the generator under test also reads, for the same reason the server is not
  generated. A criterion about what the client sent — a second attempt no sooner than
  `Retry-After` allowed, a token refreshed once — is checked against the session's recorded
  requests. Each target supplies the runner that turns a workflow step into a call through its
  generated SDK; exact timing stays with T-13, under a fake clock.
- **A coverage gate.** Every workflow ID is registered in one place, and every target declares,
  for each, that it runs it or skips it with a reason. The gate fails when a registered ID is
  neither, when an ID a target names is not registered, and when a skipped test passes: skipped
  tests still run, and one that would pass has outlived its reason.

---

## 14. Per-target milestone plan

Extends architecture.md §6 milestone 3 (the first emitters) and beyond.

1. **M3a — plan layer + the three first targets' skeletons.** `emitters/plan` with its
   language-independent golden corpus; `emitters/golang` and both Kotlin targets
   (`emitters/kotlin`) emitting models, enums, and one service client end-to-end. Proves the
   plan/refine/emit boundary and that the IR retains what a refiner needs (architecture.md §6 M3).
   Three targets at once make the plan's neutrality a test from the first model: a plan field only
   one of them can use fails §3.6 at M3a, not when a later language lands.
2. **M3b — the hard axes.** Unions (all four wire shapes), discriminators, visibility projections,
   the four optionality states, containers, external types, values, on each first target. Each is a
   refine step tied to a specific un-lowered IR fact — the acceptance test cashed.
3. **M3c — operations & runtime.** Pagination, streaming, LRO, error taxonomy, auth; the runtime
   policy tree and the boilerplate templates; the wire-conformance harness; the behavior server, its
   workflows and its coverage gate (T-14).
4. **M3d — docs + mock emitters.** Prove reuse of `Doc + Plan` over a second and third target AST,
   and that docs/mock agree with the SDK by construction.
5. **M4 — write/integrate + surface verification.** Manifest, header-gated pruning, ignore regions,
   additive `go/ast` merge, staleness; neutral surface diff with injectable per-language severity.
6. **M5 — a further language family (TS or Python).** Proves the plan holds beyond the first
   targets' statically typed languages: a new target is a new target AST + printer + refine pipeline
   reusing the shared plan and step contract, forcing **no** IR or plan change. The shared plan
   first pays back at M3, where three targets read it.

### 14.1 What M3 lowers

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
versioning, from `Availability` to `Document.versions`; the `none` streaming mode, which the
compiler writes as `""` (#978); and facts no OpenAPI construct sets, such as `Model.abstract`,
`Property.presence`, `Enum.flags` and the `host` parameter location.

---

## 15. Deliberate exclusions

- **A separate neutral code model.** Rejected in favor of per-language target ASTs. A shared
  neutral CodeDOM would add a second sealed sum parallel to the IR, duplicate the plan's traversal,
  and re-import Kiota's HTTP-shaped-core ceiling and its "minimize refiner changes" hazard. The plan
  *is* the shared neutral layer; below it, code is per-language.
- **Logic in templates.** Templates are confined to spec-invariant runtime boilerplate, guarded by a
  lint and a format pass. All structure is typed AST. A template that inspects the IR is a bug.
- **Runtime SDK policy in the IR.** Retry/timeout/telemetry/error-taxonomy/user-agent/idempotency-
  injection/pagination-pacing/request-guards are a separate `policy.Policy` tree; declared IR facts
  win over its defaults (§6).
- **Generator plan artifacts in the IR.** Request builders, executor/generator method pairs,
  per-visibility model variants, primary-response/primary-content selection — all computed views in
  the plan/refine layers, never stored.
- **Credential values / token acquisition.** Auth *schemes* are structural IR; secrets and refresh
  behavior are runtime configuration.
- **Name-keyed manifests or `"METHOD /path"` keys.** Everything cross-run correlates by IR stable ID.

---

## 16. Open questions (tracked for implementation)

1. **Plan-fact vs refine-fact boundary.** Settled in §3.6, before the plan layer is built, since
   three first targets test it from M3a. It moved the parameter-passing shape out of the plan and
   removed the fields that copied what the IR states.
2. **Target-AST granularity.** The line is "declarations and signatures = typed AST; behavior and
   boilerplate = template". Method *bodies* that are pure boilerplate (the pager's fetch loop, the
   retry loop) stay templated; an over-fine AST that models statement-level Go grammar is a
   non-goal. The exact granularity is a per-feature judgment that will be litigated as features land.
3. **Where content-negotiation ties break.** The `json > form > multipart > binary` default is a
   shared plan helper (resolves `ir-design.md` Q3), but a target that wants a different primary needs
   a clean override path that does not fork the plan. Leaning: an override on `plan.Policy`, since the
   choice is document-neutral.
4. **Protocol-view neutrality.** The `BindingView` and target-AST `MethodKind` must be designed
   against the full binding matrix (HTTP/RPC/message/GraphQL/OTP) from day one, or they ossify around
   HTTP the way Kiota's model did. The Go-first walkthrough is HTTP-heavy; the RPC/messaging targets
   are the real test. Revisit when the second-protocol emitter lands (tracks `ir-design.md` Q6).
5. **Manifest granularity for merge.** Whether the entity→symbol map keys at property level (not just
   type/op level) to support field-level additive merge, or leaves sub-type merge to the `go/ast`
   adapter. Decide when write/integrate (M4) is built.
6. **Surface behavioral channel scope.** Which behavioral changes beyond default-value changes
   (e.g. a `Retryable` flip, an idempotency change) belong in the separate behavioral channel vs the
   structural one. Decide when surface verification (M4) is built against real cross-revision diffs.

7. **Behavior server distribution.** The server T-14 runs against is Go in this repository, and a
   target in another language needs it running beside its own test suite. Whether that is a
   released binary, a container image or a build at a pinned revision is decided when the Kotlin
   targets' T-14 suite is built (M3c).
8. **Kotlin's unit size.** §4.17's size check needs a default limit on a Gradle module's
   declarations for each Kotlin target, as Go's is set from a measured build. It is set by measuring
   kotlinc and the Kotlin/Native framework build when the Kotlin targets are built (M3a), and moves
   only with an options version, as every default does (§2.5).

---

## 17. Invariant compliance

| INV | Where satisfied |
|---|---|
| 1 IR is the ABI | `EmitterInput.Doc` is the only structural input; `emitters/*` imports `ir`, `ir/irfacts` + shared contract, never `compiler`/`engine`/a sibling target (§2, §7) |
| 2 Lossless, lowered late | all lowering is in `refine` steps building a fresh target AST; the IR is never mutated; unions/enums/discriminators/visibility/pagination/streaming/LRO arrive un-lowered and the step list is the acceptance test (§4, §8) |
| 3 Stable IDs, names presentation | plan, AST `Origin`, manifest, surface, hints, and the collision table are all keyed by IR stable ID; renames are lookups; collisions never touch identity; a name with no words is named from its own characters alone, and placement reads no count, so neither moves when something else changes (§3, §4.12, §4.17, §11) |
| 4 Names neutral, emitter cases | `Ident` starts as the `Naming.Canonical` word sequence, the source spelling beside each word for its letter case only; `RenderCasing`/`EscapeReservedWords` are the only casing steps; struct tags come from `WireName`/`WireID`, a separate channel (§4.12, §5) |
| 5 Pure, typed diagnostics | `Generate` is pure/reentrant/no-stderr/no-I/O; every un-lowerable construct → coded `ir.Diagnostic`; the engine decides fatality (§2, §2.4) |
| 6 Heuristics are policy | `plan.Policy`, each target's versioned options (its type table and naming tables among them), `verify.Severity`, method-name derivation, and shaping hints are injectable and disable-able; the type table's width-picking rows, the acronyms, the character names and the leading words are defaults a configuration replaces; inferred outputs marked `Inferred` (§2.5, §2.6, §3.5, §4.12, §6.4, §12) |
| 7 Serializable/deterministic | `Plan` + manifest are JSON-serializable, and `Facts` is rebuilt from the document; maps walked sorted, slices source order; identical input ⇒ identical bytes (§2.2, §3, §5.5, §13) |
| 8 Optionality ≠ nullability | `LowerOptionality` renders all four states plus protobuf `Presence` as a third axis; collapses are diagnosed (§4.7) |
| 9 Capability surface designed against every format | every `ir.TypeKind` and every operation facet (pagination/streaming/LRO/one-way/messaging) has a lowering path or diagnosed degrade (§4.2, §9); docs/mock/validation are registry entries over the same `Doc + Plan`; a new language forces no IR or plan change (§10, §14 M5) |
| 10 Runtime policy separate | `policy.Policy` tree in `EmitterInput.Policy`, never on `ir.Document`; it parameterises only boilerplate; declared IR facts win over its defaults (§6) |
