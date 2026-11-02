# Morphic Architecture

Morphic turns any API specification — OpenAPI/Swagger, TypeSpec, Smithy, GraphQL, AsyncAPI,
Protobuf, Erlang/OTP module specs — into idiomatic SDKs and docs through a single spec-agnostic
intermediate representation (IR). This document defines the pipeline, the package layout, and the contracts
between stages. The IR itself is specified in the [IR design](./ir/README.md).

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
                        ┌── IR passes (IR → IR) ───┐
                        │  link · dedup · filter   │
                        │  version-slice · overlay │
                        │  validate                │
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
   is an IR document plus diagnostics; an emitter's only input is an IR document plus its own
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

One compiler per source format. Each owns its format: which documents a source's references name,
reference resolution, format-version normalization, and lowering into IR nodes. Reading those
documents is not a compiler's. It does no I/O, and reads every document beyond its sources, a file
one of its options names such as an overlay among them, through the `compilers.DocumentReader` the
engine hands it in `compilers.Options.Reader`, in the compile, under the compile's context and
budgets: decoding options reads nothing. The compiler says how far a read may go, as a directory it
must stay inside, and the reader enforces it, symbolic links included: the OpenAPI compiler keeps
every read inside the spec's directory, following relative references alone, unless
`allow-external-refs` lifts that.

Contract, abridged from `compilers.Compiler`, whose doc comments state each method's duties:

```go
type Compiler interface {
    Formats() []SourceFormat // e.g. openapi@3.0, openapi@3.1
    // Detect reports the format src declares and whether this compiler recognizes it.
    Detect(ctx context.Context, src Source, opts Options) (Recognition, []ir.Diagnostic, bool)
    // DecodeOptions turns textual settings into this compiler's own options value.
    DecodeOptions(set OptionSet) (any, error)
    // SourceLimit is the most bytes of a source this compiler reads under opts.
    SourceLimit(opts Options) int
    // Compile lowers sources into a Document, or refuses them with a nil one.
    Compile(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
    // SourceTable names the inputs its arguments name: the table a refusal's diagnostics index.
    SourceTable(sources []Source, opts Options) []ir.SourceInfo
}
```

A returned `Document`'s `Sources` begins with the table `SourceTable` gives, in order and filled
in, and may go on to name each further document the compile read, such as one a reference names.
`SourceTable` reads no input, so it names only what the caller supplies; a refusal reads no other
document, so the table is what its diagnostics index.

Internal phases every compiler follows (each format implements them its own way):

1. **Load & bundle** — read every document the source's references name, through the caller's
   reader, and resolve references across them. Each document a reference reaches is a `Document.Sources` entry,
   and original positions (document + JSON pointer / line-col) are preserved for provenance, so
   what another document declares is lowered where it declares it.
2. **Normalize within the format** — collapse format-version differences before IR construction:
   Swagger 2.0 is lifted to the OpenAPI 3.x shape (`body`/`formData` params → request body,
   `host`+`basePath` → servers); OpenAPI 3.0 `nullable` and 3.1 `type: [T, "null"]` both become
   the IR's nullable bit. The IR never records which dialect a fact came from except in
   provenance.
3. **Hoist & identify** — every anonymous inline type is hoisted into the type registry exactly
   once, keyed by its source pointer, with a naming *hint* (not a name) that is a function of the
   documents and that pointer alone. One function derives every inline name, whichever lowering
   reaches the position first (oagen's duplicated-naming failure mode is designed out).
4. **Resolve & lower** — build IR nodes: type graph, services, operations, bindings, auth,
   channels. Declared semantics lower directly; inferred semantics (heuristic pagination
   detection, envelope unwrapping) run only if the corresponding policy is enabled and mark
   their output as inferred.

What a compiler does *not* own is in `compilers/compile`: the state and the grammars every compiler
must agree on. That is the type registry with the source-coordinate map behind stable IDs (and the
rule that a minted node takes a namespace no source coordinate addresses), diagnostic accumulation,
the canonical naming grammar, and the identifier grammar — the kind prefix and the namespace after
it, with the declaration of the namespaces a compiler mints in. The registry also holds the
decisions that read nodes other lowerings intern (`compile.Settlement`), deferred until the walk
ends, and the nodes a reference names, promised to the lowering that owns them
(`compile.Types.Promise`): a walk lowers one declaration at a time, so a decision taken mid-walk, or
a node a reference built, would follow declaration order (micro-compiler-design §6.2). They live on
the registry, though only OpenAPI uses them yet, because the registry is the one handle a lowering
holds (micro-compiler-design §4). What stays with the compiler is what only it can compute: the
path an ID derives from, since a JSON Pointer, a GraphQL structural path and a protobuf
fully-qualified name are different things.

The split is enforced rather than documented: lint rules (`internal/lintrules`, run by `make vet`)
fail a package outside the framework that builds or writes the type registry, derives a canonical
name, builds an ID out of a string, or builds a namespace out of anything but a literal.
Promoting something into the framework later is additive, while demoting it breaks every compiler,
so borderline machinery starts outside and moves in on evidence from more than one format.

A compiler is also bounded in what one compile may cost. Most bounds are constants beside the walk
they bound (schema nesting depth, how deeply a document nests, how many times its own size a
document's aliases expand it to, reference-chain length), because nothing about them is a caller's
to choose. The bounds on the *input* are, so they are options: `openapi.Options.Limits` carries a
byte budget, a parsed-node budget and an alias budget for one source document, a member budget for
one enum, and a budget on how many documents external references read in one compile, each with a
documented default and each settable — zero takes the default, negative turns the budget off.
Crossing one is a spec problem like any other, so it is an `openapi/budget-exceeded` diagnostic
rather than a Go error. Inside the OpenAPI compiler these budgets, and every other count held to a
limit, count through one type, `compilers/openapi/internal/budget`, so "unbounded" has one spelling
and no count overflows; a depth, a chain's hops or a constant compared beside the one loop it bounds
stays beside its check, and what crossing a limit does stays with the site that counts. Like other
borderline machinery, the type starts in the compiler and moves into the framework only once another
format needs it. The byte budget also bounds the read: `Compiler.SourceLimit` reports it, the
registry takes the largest among the compilers it would ask, and the engine reads the spec, a
regular file and never a FIFO or a device, no further than one byte past it, where every compiler
declines it unread. A spec past every budget costs the budget and a byte, not its size, and an
overlay is read the same way. Time is bounded by the caller instead of by a constant: the work a
compile does in proportion to the document honours the `context.Context` it is given, so a deadline
or a cancellation stops it between items and the error reaches the caller unwrapped. In the load
that is the parse of the source's bytes and every walk of its YAML tree and of the model built from
it, and in the lowering the walks over what it lowers and the settling after them. The lowering's
index of `$dynamicAnchor` declarations is the exception: built when a `$dynamicRef` is first
lowered, it walks the tree reading no context. The load also asks before and after each phase, so no
phase starts once the context is done and none cut short is built on. Within a phase it asks again
before each call into the parser library, which reads no context, so a screen or walk a cancel cut
short is never followed by the library's work on what it left;
`TestLoad_DoesNextToNothingOnceItRefusesACancel` counts that work. In the load, the one step a
cancel waits out is the model build itself, a single call
([`parser-library.md`](parser-library.md#context-unread)).

**The contract is checked, not trusted.** Each duty `compilers.Compiler`'s doc comments state is
held where its data is. `Registry.Register` refuses a format naming no dialect, and
`Registry.Detect` a claim naming none, a decline that indexes a source detection was not handed or
speaks with no error, and a claim of a source past the claimant's `SourceLimit` or a silent decline
of one: a source read no further than the limits allow may be only the first bytes of one.
`Detection.Compile`, which the engine compiles through, refuses a refusal that gives no error, a
document not stamped with the IR version this build reads, a source table that does not open with
the one `SourceTable` names, a source read as a format other than the one it was recognized as or
hashed from other bytes, and a finding indexing no source. Entries past that opening name what else
the compile read, such as a document a reference names, which `SourceTable` reads no input to know
of. Each breach is a Go error wrapping `compilers.ErrContract`: a compiler bug, never the spec's.

A panic is a breach too. Every call the `compilers` package makes of a compiler (`Formats`,
`Detect`, configuring it through `DecodeOptions`, `SourceLimit`, `Compile` and `SourceTable`) runs
behind `barrier.Catch`, and the panic comes back as an error wrapping `compilers.ErrContract` and
the `*barrier.PanicError`, whose message is one line and whose value and stack stay beside it for a
caller that wants them. The registry asks `Formats` once, when the compiler registers, and keeps a
copy, so no later call can panic outside the registration or disagree with it. A barrier recovers
the calling goroutine only: a fatal error such as stack exhaustion, a panic on a goroutine the
compiler started, and a call that never returns are not its to catch. A compiler that ignores its
context holds `Run` until it returns, because the engine cannot stop a call it did not start on a
goroutine of its own, and abandoning one would leave it reading the caller's bytes after `Run` had
returned. When it does return, the run fails with the context's error, and what the compile returned
is dropped unjudged: a stage that returns once its context is done may have stopped short, and
nothing tells a whole answer from a cut one. `Registry.Detect` treats a `Detect` the same way, and
the engine a pass (§2.2).

What a document holds whatever produced it, the namespaces, a service and a well-formed source table
among them, is `irverify`'s, and `compile.Types.Finish` hands a compiler its registry with the
diagnostics it reports, a `compile/registry-refused` error for each entry the registry refused among
them. The engine does not run `irverify` by default: it slows a run, and the harness runs every
compile of the whole corpus verified. `RunOptions.Verify` (`--verify`) runs it for a compiler under
development, over the compiler's document and over each document a pass returns, and over the one
`RunDocument` is handed, and fails the run with an `engine.VerifyError` carrying every violation,
wrapping the contract error of the stage that returned the document. It also fingerprints each
document, the SHA-256 of its canonical JSON, so a pass that changes the document it was handed is
caught: the one duty no walk of a single document can check.

**What stays in the OpenAPI compiler.** Three of its pieces read as if any format could use them:
the YAML view (`internal/nodeview`), version sniffing (`internal/detect`) and document identity
(`resolve.SameDocument`, `resolve.IsURL`). None moves into `compilers/compile`, because what each
owes is agreement with the OpenAPI parser library, not with YAML or RFC 3986. The view reads a
mapping as the library's unmarshaller does, its `<<` merges and repeated keys included, which
yaml.v3's own decoder reads differently. Sniffing reads the parse the compile lowers, through that
view, and claims a broken document by OpenAPI's discriminating keys. Identity classifies a location
as the library's resolver does. A compiler parsing through the same library is the evidence that
would move the view or identity, and one wanting the byte probe for a key of its own would move
that probe; any other needs a reading that agrees with its own parser, since a shared reading would
disagree with it as detection disagreed with the compile before it read through the view. What does
cross formats is a duty rather than code, a recognition naming the version the compile reads, and
`Detection.Compile` holds every compiler to it. The one piece every compiler spelled alike, the
digest a source table records, is `ir.SourceHash`.

Compilers are registered in a registry keyed by the formats they report, and detection belongs to
them too: the registry asks each compiler in registration order whether it recognizes a source, and
the engine dispatches to the one that does. Each is asked under the options it would compile with,
because recognizing a source is a read of it, and the bounds a caller set on reading the source
hold there as they do in the compile. A compiler also decodes its own textual options, so
registering one is the whole of adding a format — no layer above names any of them. Milestone 1
ships the OpenAPI 3.x compiler only. The compiler registry and the provenance model take any format,
and the IR is designed against all eight; §2.5 says how it changes where a compiler finds it short.

### 2.2 IR passes (IR → IR)

Small, composable, order-explicit transformations. A pass is a `pass.Pass`: a stable `Name()` and a
`Run(context.Context, *ir.Document) (*ir.Document, []ir.Diagnostic)` that never mutates its input. A
transform returns a new document, which may share whatever it left unchanged; an analysis such as
`validate` returns its input beside its diagnostics. `Run` has no error result, for the reason an
emitter's `Generate` has none ([the emitter contract](./emitters/contract/emitter.md)): whatever a
pass cannot handle in the document is a diagnostic, and anything else is a bug in the pass.

`Run` takes the run's context, though a pass works on a document already in memory, because that
work grows with the document: `validate` alone walks the whole of it, under half a second on the
golden spec (morphic @e5557ddd, spec @d04a1ddd), and the document has no bound. A pass stops soon
once the context is done, as `validate` does in its walk (`ir.WalkValuesContext`) and between its
rule families. The engine asks the context before each pass, so a cancelled run starts no other, and
after each, so a pass returning once it is done fails the run with the context's error, unjudged, as
a compile does (§2.1): a pass cut short may return what its contract forbids. Verification (§2.1)
reads no context, and the engine asks after it too, so a run cancelled anywhere returns no document.

A transform changes a document through an `ir.Editor` (`ir.NewEditor`), which is to a pass what
`compile.Types` is to a compiler: its way into the document. It reaches every part: a type, service,
operation group, operation, server, channel, message and auth scheme is read, changed and removed by
its ID, and the document's own fields, its tag definitions and versions among them, are changed
through `UpdateDocument`. A part nested in one of those, a property, a parameter, an enum member, is
changed through its owner, so `version-slice` drops a property and a parameter by one rule, as an
update of the type or operation holding it.

The edit never writes the document it edits. It copies a registry at its first write into it, and
holds the service tree apart from the document: a write replaces one node, and a slice of the tree
is built anew, only where a write reached it, when a read or `Finish` needs it. So a pass pays for
what it changes, the rest stays shared, and what a read returned stays as it was while the edit goes
on: no slice is written once built, at the price of a read of a group or service building the slices
changed since the last one. An update hands its change a deep copy of the node's own fields; the
nodes the edit keys apart, a group's groups and operations, a service's groups, the document's
services and registries, are left out of the copy and changed by their own IDs. The edit keeps each
node at the key its own ID names and adds no ID, a nested one included, so every ID stays in a
namespace the document already declares (invariant 3). A write that would break either, or that only
a bug makes, is refused, and `Finish` returns each refusal as an `ir/edit-refused` error beside the
document, so it fails the run at the default threshold instead of passing unseen. A removal removes
what its ID names, with what that holds, and nothing else: a group or a service its last member
leaves stays, for the pass to remove when its own rule says so. Whether a removal leaves a reference
dangling is not the edit's to judge: `validate` reports one. Adding a node is not an edit's either:
the first pass to mint one, `link`, brings that, with a namespace of its own (invariant 3's
corollary).

Two rules in `internal/lintrules` hold the containers to the editor: `registrywrites` refuses
building a registry, or writing one of its entries, anywhere but `ir` and the compilers (the type
registry `compilers/compile` alone builds), and `servicetreewrites` refuses building or writing a
slice of services, groups or operations anywhere but `ir` and the compilers. No rule sees a node
changed in place through what it holds by reference, which the deep copy makes needless; a pass that
writes its input that way, as a write into a part of `Finish`'s document the edit left shared does,
fails a verified run (§2.1).

A pass that takes settings, as `filter`, `version-slice` and `overlay` will, holds them on its own
value, so `Run` keeps one signature for every pass. A caller supplies them through a typed field on
`RunOptions`, and the CLI through flags of the pass's own, both added with the pass. Where the
settings have no neutral default, as those three's have none, supplying them is what turns the pass
on. The compilers' textual option map is the wrong model here: it serves a set of formats the engine
knows nothing about, while the built-in passes are a closed set the engine composes, so each setting
can be typed, checked and documented where it is declared. An embedder's own pass takes its
settings where the embedder builds it.

The order lives in the `engine` package's default list, `engine.Passes`, not in `pass/`: composing
passes is orchestration, which only the engine does. An embedder runs a list of its own,
`engine.WithPasses`, composed from that one, and `engine.New` refuses a list a run could not
address: a nil pass, an unnamed one, or two sharing a name, which turning one off would turn off
together. The engine runs the list after any compile that produced a document, errors reported or
not, and over a document already compiled that a caller hands it (`RunDocument`), and threads each
pass's output into the next. An analysis that judges the document runs after every transform, so
`validate` comes last. It then checks what the run returns, every transform's output included, and
its findings, whose `Provenance.Node` is spelled against the document it was handed, locate in that
document. Running it first would give the transforms no guarantee, since a caller can turn it off:
each pass must cope with a document that fails it whatever the order.

The engine owns `Document.Diagnostics`, writing the compiler's findings and every pass's onto a copy
of the document it returns, so a transform that builds a fresh document cannot lose any, and a
compiler that hands every run one document is neither changed nor raced. A pass leaves the field
alone: empty, or as it was given. Those findings index `Document.Sources`, as the IR's own
provenance does, so a pass may append to that table but never move or drop an entry.

A pass is held to its contract by what the engine can read off its output. It returns a document
stamped with the IR version this build reads, whose source table keeps every entry it was handed and
whose `Diagnostics` are the engine's, and findings that index that table. A pass that returns none,
breaks one of these, or panics in `Run` fails the run with a Go error naming it and wrapping
`pass.ErrContract`. The engine asks a pass for its name once, in `New`, so a name that panics is a Go
error there and a name cannot change under a run. Under `RunOptions.Verify` the document a pass
returns is also held to `irverify`'s invariants, and the one it was handed to being as it was
(§2.1).

Every pass is idempotent: run again over the document a run of it returned, it returns one that
encodes the same, `Document.Diagnostics` aside. A normalization that changes its own output on a
second run leaves the document depending on how many times it went through the list, and
`RunDocument` runs the list over a document that may have been through it already. Such a document
carries the earlier run's findings, the pass's own among them, so the second run is handed findings
the first was not and must still return the same document. What a pass reports may differ the
second time, as a filter that removed an operation finds none left to remove, so the findings are
left out of the comparison.

Idempotent passes do not by themselves make the list idempotent; their order has to as well. What
a run returns is a document each of the passes it ran returns as it is, so a second run returns it
unchanged, whichever of those passes it turns off. A pass placed before one whose output it acts on
breaks this, though each of the two is idempotent alone: a filter dropping what a later pass flags
drops more on the second run. It goes after the pass it acts on. The harness's idempotence oracle
holds the default list to both over every corpus spec: through `RunDocument`, it runs each pass over
what the passes before it returned, then again over what that run returned, and then each over what
the run of them all returned (§5). No run that turns passes off is swept on its own.

A caller turns a pass off by name (`RunOptions.DisablePasses`, `--disable-pass`); a name no pass
carries is a Go error (`engine.ErrUnknownPass`), not a diagnostic, because it names no defect in the
spec. A pass whose output a consumer relies on is not a caller's to turn off: from the first such
pass, the engine refuses its name as it refuses an unknown one. Turning a pass *on* by name is for
one that has no settings and is off by default, as the convenience simplifications of
reference-learnings.md §B0 are to be, and it lands with the first of them. `engine.WithPasses`
replaces the default list whole, so an embedder's list can leave out a pass `DisablePasses` may not
turn off, and `RunOptions` can carry settings for a pass the list does not hold. Neither can happen
yet: the first pass that cannot be turned off, and the first with settings, each decide what the
engine refuses there.

Built, and in the default list:

- **validate** — referential integrity and the well-formedness rules the IR states, each rule family
  in a `pass/validate_<family>.go` of its own. Integrity means every typed-ID reference resolves to
  something the document declares (a `TypeRef` target against the type registry, an `OpID` against
  the operations the service tree declares, and so on for every ID class), and every integer index
  and `PropID` addresses a declared entry. Well-formedness is what the IR asks of a node beyond
  that, such as discriminator mappings that point at actual variants, wire names and aliases unique
  within a model, a one-way operation that declares no response, and binding completeness: a
  parameter an HTTP binding places twice at one non-host location is an error, one it places nowhere
  is a warning, and a `host` binding is additive ([http-binding](./ir/http-binding.md)).
  Structural problems are errors, which fail the run at the default threshold (§4); style issues are
  warnings.

Designed, not yet built:

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
  heuristics consume the broken shape — and *per-target-language* naming/compat overlays are an
  emitter input keyed by IR ID, so one IR document drives different compat baselines per
  language.

  The first of those is `openapi.Options.Overlay`: bytes the caller hands over, or the document its
  path names, which the compile reads through the caller's reader as it reads any document beyond
  the source, applied to the parsed node tree before the model is built, so untouched nodes keep the
  line and column the parser read them at rather than a position in a re-serialised file nobody has.
  The overlay becomes a second `Document.Sources` entry, and every position it introduced or rewrote
  names that entry as its `Provenance.Source`. Strict application is the default: an action whose
  JSONPath matches nothing is reported and the compile refuses, because an overlay that silently
  does nothing ships an SDK missing the fix it was written to make.

Passes operate on the IR only; they know nothing about source formats or target languages.

### 2.3 Emitters (IR → artifacts)

Emitters are plugins (SDK-per-language, docs, mock servers…). Out of scope for this session
except for the boundary they impose on the IR; the internal shape mirrors what Kiota and oagen
converged on independently:

1. **Plan** — compute language-*neutral* per-operation and per-type decisions once (is-paginated,
   error taxonomy, primary-content/response selection, return-shape classification (a success body
   read as a value / void / raw bytes / a stream), pagination item-type unwrap) so templates contain
   no policy. This list is the decision set oagen's emitters demonstrably needed, computed once and
   shared across all languages, less two kinds of entry ([plan fact or refine
   fact](./emitters/plan/plan-or-refine.md)): what the IR already states, such as body presence and
   idempotency, which a refiner reads there; and what varies by language, such as oagen's
   parameter-passing shape (positional vs inside an options object), which is each target's.
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
generated SDKs — is an emitter input alongside the IR, not part of it. The IR describes the API;
policy describes the SDK. (oagen embeds both in one root; we keep the trees separate so the same
IR document can drive SDKs, docs, and mock servers without dragging SDK opinions along.)

The canonical policy-input vocabulary, taken from oagen's production `SdkBehavior` (the best
real-world enumeration available): **retry** (retryable status codes, max attempts, full backoff
strategy with jitter), **timeouts** (defaults + env override), **error taxonomy** (status →
logical exception-kind and fault, doc-URL template), **telemetry** (request-
ID and client-telemetry headers), **logging** (a closed lifecycle-event list), **user-agent**
construction (identifier template, app-info enrichment), **idempotency injection** (header name,
auto-generate rules), **pagination pacing** (auto-page delay), and **request guards** (option
keys that must not appear as params — misuse detection). Delivery model: canonical defaults +
deep-partial per-emitters/per-project overrides. Precedence rule: *declared IR facts win over
policy defaults* — `ErrorCase.Retryable/Fault` and `Operation.Idempotency` come from the spec;
policy fills in where the spec is silent, never the reverse.

### 2.5 Changing the IR

The IR is the one contract between the stages, so a change to its shape is a change to every stage
that reads it. Its capability surface is designed against every format the matrix covers
(`ir-spec-matrix.md`), but that is a goal rather than a finished state: a compiler can meet a
construct the IR has no place for, and the surface grows when one does. Two rules say how.

**A gap closes as a versioned change.** Until the IR has a home for a construct, the compiler keeps
it verbatim under `ReasonNoIRHome` rather than lowering it to a weaker shape, so nothing is lost
while the gap is open, and the gap is an issue of its own, where the shape of its home is decided.
The change that closes it adds the field, moves the construct out of `Unmodeled` and bumps
`IRVersion`, as any change to the JSON shape does ([ir-version](./ir/ir-version.md)).

**Once an emitter reads a field, a change to it lands whole.** A change to a field the plan layer
or an emitter reads — renamed, removed, or given another meaning — lands in the same change as the
plan, refiner and golden updates it forces, for every target, under one `IRVersion` bump. None of
it is left to a follow-up: an emitter is built against exactly one generation and has no mode for
reading another ([ir-version](./ir/ir-version.md)), so a `main` whose compiler writes one
generation while an emitter reads the last would drop constructs without a word. Several changes
made on one line of work are batched under one bump where they land, as
[ir-version](./ir/ir-version.md) already has it. A field no emitter reads yet is added by the
compiler change that fills it, and the emitter that first reads it adopts it in its own change.

### 2.6 IR cache

A run with the same inputs has one answer, which the determinism oracle holds it to (§5), and that
answer round-trips through JSON (principle 7), so a driver may keep it. `engine.WithCache` opts an
engine in, and `--cache-dir` opts the CLI in. A hit skips detection, which parses the spec, the
compile and the passes, and returns what they returned. Nothing is stamped on the document for this:
a run's output is the same whether it hit, which is what the corpus test in `engine` holds it to,
and a stamp would make it differ from build to build.

**What a key covers.** A key is derived before anything runs, from the build, `IRVersion`, the
working directory, the spec's path and bytes, the passes the run leaves on, in order, and the
textual compiler settings. Each is there because the document depends on it. The path is written
into `Sources`. The working directory reaches type IDs: a compile reads it to relate a relative
path to an absolute one, so one spec reaching one document can lower to two sets of IDs from two
directories (#1297). The build is the identity of the code, compilers and passes included:
`cache.BuildID` hashes the executable, since a module version names neither a dirty tree nor the
toolchain. It reads the file at the executable's path, as no portable call reads the image running,
so a build installed as a run starts, before that read, is the one named: the run keeps what the
old build compiled under the new build's key, and that entry answers the new build (#1318). The
threshold is not covered, as a hit judges the verdict afresh, and neither is the format, which
follows from the bytes and which finding is what a hit skips.

**What a key cannot cover.** Which other documents a compile reads is known only once it has run.
So a compile reads through a recorder, the entry lists each request with the hash of what came
back, and a hit makes the same reads through the run's own reader first: one document with other
bytes, or one no longer there, is a miss. A run where a read failed stores nothing, because the
missing document could appear later and the entry would have nothing to compare it with. So a hit
costs those reads, a remote document's fetch among them, and a key holds one entry, so documents
that alternate between two states compile each time.

**What does not take part.** A `Verify` run exists to run its stages, so it neither reads nor
stores. A `FormatOptions` value is opaque to the engine, which cannot tell what it would have to
key on, so such a run does the same; the textual channel is the one that caches. `RunDocument`
compiles nothing.

**A cache never fails a run.** An entry that is damaged, a store that refuses or panics, or a build
that cannot be named costs the work and a report, on `Result.CacheErr` or as the CLI's warning. An
entry is sealed by a hash over its key and all it holds, so one damaged anywhere, or handed back for
another key, is refused and replaced by what the run compiles: a hash of the bytes alone would serve
an intact entry to the wrong run. A seal tells damage from a sound entry, not a forgery from a
compile, so a cache directory is trusted as the executable is.

**The store on disk.** `cache.Dir` publishes by atomic rename, so processes can share one, creates
its directory private to its user, and removes only files it named. It keeps its entries within
`cache.MaxBytes`, those unused longest going first. Listing the directory costs what its entries
number, so a store tallies its size in a ledger and prunes only once an eighth of the limit has been
stored since the last prune, or a bounded number of stores have passed. The entries can pass the
limit by that eighth between prunes, and a ledger it cannot read prunes at once.

## 3. Go package layout

```
morphic/
├── barrier/                # Layer 0 — turns a panic in code the caller does not own into an error.
├── ir/                     # Layer 0 — IR node types, IDs, shared traversals, editing, JSON round-trip.
│   ├── irfacts/            #           Derived facts of a document, never stored (docs/ir/derived-facts.md).
│   ├── irtest/             #           Golden-snapshot and fixture helpers for IR documents.
│   └── irverify/           #           Structural-invariant oracle (dangling refs, IDs, naming).
├── compilers/              # Layer 1 — compiler contract + registry.
│   ├── compile/            #           What every compiler shares: type registry + coordinates,
│   │                       #           diagnostics, and the naming/identifier grammars.
│   ├── openapi/            #           OpenAPI 3.x → IR (milestone 1). The public face: the
│   │   │                   #           Compiler, its Options, and the run that calls the
│   │   │                   #           lowerings below in order.
│   │   ├── e2e/            #             Its end-to-end tests: test files only, compiling through
│   │   │                   #             that face, so they cover no package.
│   │   └── internal/       #             Its own packages, each below that face:
│   │       ├── diag/       #             diagnostic codes and the constructor
│   │       ├── ids/        #             pointer construction; pointer → TypeID
│   │       ├── fragment/   #             a $ref fragment read as a pointer, and spelled for the resolver
│   │       ├── navigation/ #             a pointer read through the parsed model as the library reads it
│   │       ├── spelling/   #             the primitive kinds OpenAPI spells
│   │       ├── nodeview/   #             the raw YAML as the parser reads it, and one bounded walk
│   │       ├── value/      #             scalar and BigVal lowering
│   │       ├── sourceindex/ #            what one walk of a decoded source tree answers
│   │       ├── scan/       #             pre-parse refusals (alias cycles and fan-out, nesting)
│   │       ├── bind/       #             every schema $ref bound where it lands, loops found
│   │       ├── annotation/ #             what a schema says about itself, not its shape
│   │       ├── oas/        #             the OpenAPI Specification's rules by minor, and their check
│   │       ├── overlay/    #             an Overlay applied, and the positions it answers for
│   │       ├── resolve/    #             what a $ref names; reference-or-inline entries
│   │       ├── load/       #             parse, validate, resolve
│   │       ├── detect/     #             a source recognized from its bytes, on load's parse
│   │       ├── merge/      #             allOf property reconciliation
│   │       ├── naming/     #             each node's name, from its coordinate alone
│   │       ├── lowering/   #             the immutable context every lowering reads
│   │       ├── schema/     #             the schema walk, composition, preservation
│   │       ├── auth/       #             security schemes and requirements
│   │       ├── operation/  #             path items, webhooks, callbacks, params, content
│   │       ├── servers/    #             the servers a document declares, one registry
│   │       ├── document/   #             info, and what the document keeps verbatim
│   │       └── canary/ openapitest/ ynode/ # tests only: library canaries, fixtures, YAML nodes
│   ├── typespec/ smithy/ graphql/ asyncapi/ protobuf/ otp/   (future)
├── pass/                   # Layer 1 — IR → IR passes (validate, dedup, filter, slice, overlay).
├── emitters/               # Layer 2 — emitter contract, plan layer, registry (future).
├── engine/                 # Layer 3 — orchestration: detect format, run compiler, passes, emitters.
│   ├── cache/              #           Keeps a run's entries on disk, and names the build.
│   ├── reader/             #           Reads what a run's references name: disk, network or memory.
│   ├── report/             #           Renders diagnostics and --explain for a person.
│   └── output/             #           Writes the IR document to a stream, or to a file atomically.
├── internal/archtest/      #           Layering, the recursion pin, reachability and the caps (tooling).
├── internal/census/        #           Counts which IR facts a set of documents sets (tooling).
├── internal/conformance/   #           Holds each registered format to its matrix column (tooling, tests only).
├── internal/decls/         #           Type-checked reads of declarations, for tests (tooling).
├── internal/harness/       #           Bug-catching oracle sweep over a spec corpus (tooling).
├── internal/jsonshape/     #           The IR's JSON shape: IRVersion's pin and the census read it (tooling).
├── internal/keydoc/        #           Reader of the keyed documents and their citations (tooling).
├── internal/leakcheck/     #           Goroutine leak check for test binaries (tooling).
├── internal/lintrules/     #           The repository's go/analysis lint rules (tooling).
├── internal/testspec/      #           Spec fixtures shared by the tooling (tooling).
├── cmd/morphic/            # Layer 4 — CLI.
├── cmd/morphic-harness/    #           CLI over internal/harness (tooling, not the pipeline).
├── cmd/morphic-census/     #           CLI over internal/census (tooling, not the pipeline).
├── cmd/morphic-covergate/  #           Judges the coverage profile (tooling, not the pipeline).
└── cmd/morphic-vet/        #           Runs internal/lintrules under go vet (tooling, not the pipeline).
```

Swagger 2.0 gets no directory of its own: the lift normalizes it to the OpenAPI 3.x shape and
belongs *inside* that compiler (§2.1, milestone 2). A sibling `compilers/swagger/` calling into
`compilers/openapi` is the one import the rules below forbid outright.

This tree is prose. `internal/archtest`'s `rules` map is the source of truth, and it is what fails
when the two disagree — every production package must appear there or in `exempt`, which takes
tooling under `internal/` alone. To read the layout off the tree rather than off this page:

```bash
git ls-files '*/*.go' | xargs -n1 dirname | sort -u
```

Dependency rules, enforced by an architecture test as in oagen:

- `ir` imports only the standard library. It contains no parsing, no generation, no I/O.
- `barrier` imports only the standard library, and sits below the packages that run code they do not
  own behind it: `compilers`, which runs a compiler, `engine`, which runs a pass, and the OpenAPI
  compiler's `diag`, behind which that compiler runs its parser, its overlay library and its own
  alias scan. No other package recovers a panic outside its tests, as `.golangci.yml` enforces.
- `compilers/compile` imports only `ir`: it is below every compiler, not beside them.
- `compilers/*` and `pass` import `ir` (and their own format libraries), and `pass` also
  `ir/irfacts` — never each other, never `emitters` or `engine`. A compiler also names the
  contract package and `compilers/compile`; each is allowed in its own right, so no sibling
  compiler rides in beside them.
- A compiler's own `internal/` packages each carry their own allowlist rather than inheriting the
  compiler's. That is what keeps the ordering among them real: `diag` reaches only `ir` and
  `barrier`, and no lowering package may reach the compiler that calls it.
- `emitters/*` imports `ir`, `ir/irfacts` and the contract package `emitters` — never a compiler.
  The rule is in place before the first emitter package: `emitters` is ruled now, and each package
  beneath it carries an entry of its own, as a compiler's packages do.
- `engine` imports everything below it; `cmd` imports `engine`.

The engine owns every read, every write and every rendering for a person, so a second driver — an
editor integration, an emitter driver, a test harness — reads, writes and reports exactly as the CLI
does without importing it. It takes a spec file (`Run`), a spec held in memory beside documents on
disk (`RunSpec`, for an edited buffer or a harness), sources already in memory (`RunSources`, which
reads nothing else), or a document already compiled (`RunDocument`, which runs the passes alone).
`engine/reader` reads what a source's references name, `engine/output` writes the document to a
stream or replaces a file atomically, `engine/report` renders diagnostics and `--explain`, and
`engine/cache` keeps what a run compiled (§2.6). The CLI parses flags, picks the streams, cancels
the run on SIGINT, SIGTERM or SIGHUP, and turns the run into an exit code.

Two caps guard the shape rather than the graph, both in `internal/archtest` and `.golangci.yml`:
no type under `compilers/` may carry more than 20 methods, and no function may exceed 70 lines or
a cognitive complexity of 20. The method cap exists because the god object that
`micro-compiler-design.md` is about grew to 159 methods without ever writing a long function.

`ir` also owns the traversals the layers above it share: the reflection walk, and the typed walks
over operations, type edges and composition. A pass or emitter calls them rather than writing its
own. That is a convention review holds, not a rule a test checks, since no test can tell a
hand-written walker from any other loop. The reflection walk shows each value to any number of
visitors in one traversal, each seeing what it would walking alone, and builds a path only for a
visitor that reads it. `irverify` reads a document in one such walk shared by all its walking
checks, and `pass.Validate` in one shared by its three reflection checks, which their tests hold
them to. Both resolve typed-ID references through one `ir.IDReferences` visitor, so a reference one
of them reports as dangling the other reports too.

## 4. Diagnostics & provenance

Every IR node carries a `Provenance`: the source file it came from, a JSON pointer into that file
or, where the construct has no pointer yet, a line and column, and an `Inferred` marker listing the
heuristics that produced it, when any did. A shared primitive, which every source reaches by kind,
names no source (`NoSource`); so does a pass reporting on the document itself, which locates its
finding by an IR-space `Node` instead. Every stage returns
`[]Diagnostic{Severity, Code, Message, Provenance}`. Codes are stable strings
(`openapi/unresolved-ref`, `ir/dangling-type-ref`, `pass/discriminator-missing-variant`) so CI
can allowlist. A mature allowlist entry is keyed by (diagnostic code × entity ID), requires a
human rationale, supports expiry tied to a release, and rejects wildcards — narrowness is
validated, so approvals cannot rot into blanket suppressions. Nothing in the pipeline writes to
stderr. The engine renders diagnostics (`engine/report`), and a driver such as the CLI chooses the
stream they go to.

**What fails a run.** A stage reports severities; whether the run fails is the engine's verdict,
taken once the passes have reported, and recorded as `Result.Failed`, so the CLI's exit status
(`--fail-on`) and any other driver read one rule rather than each reading the diagnostics its own
way. `RunOptions.FailOn` sets the threshold. At `error`, the default, an error diagnostic fails the
run; `warning` and `info` are stricter gates, each failing it on a diagnostic at or above that
severity; `none` fails only a run that produced no document, which fails at every threshold. A
diagnostic of a severity the IR does not declare is a stage breaking its contract and fails the run
with a Go error: no threshold can rank it, and ranking it below them all would let a misspelt error
through every gate. Failing withholds nothing: a failed run still returns its document and every
diagnostic, to inspect or persist. What it stops is the emitters, which the engine starts only on a
run that has not failed. The passes are not gated, since they run on every compiled document, errors
reported or not (§2.2), and the verdict reads their findings too. Error is the default threshold
because an error means the document may say something the spec does not: an SDK built on it is
something a caller asks for with `none`, never a default.

## 5. Testing strategy

- **Golden IR snapshots**: each compiler has a corpus of specs; `spec → IR → JSON` is
  snapshot-compared. IR changes show up as reviewable diffs.
- **Capability conformance corpus**: one minimal spec per row of `ir-spec-matrix.md` per format
  that can express it, asserting the IR captures it losslessly. This is the regression net that
  keeps "lossless by default" honest as compilers are added. Row and spec are tied to each other
  rather than left to prose: every matrix row and format column carries a stable key, each
  format's `testdata/conformance/<format>/matrix.md` names the specs that witness every row its
  column marks expressible or why none does yet, and `internal/conformance` holds each format the
  engine registers a compiler for to that column. What stays a reviewer's job is the claim inside
  that link — a spec naming a row has to *exercise* that capability, and no test can read a golden
  and tell you whether it does.
- **Round-trip property**: `parse → serialize → deserialize → deep-equal` for every corpus
  document.
- **Oracle sweep** (`internal/harness`): every corpus spec is run as `morphic compile --verify` runs
  it, the default passes included, in every run an oracle makes, and driven through the oracles in
  order — no panic, `irverify`'s structural invariants, no error diagnostic, JSON round-trip,
  determinism across two compiles, each pass's idempotence and the pass list's (§2.2), and
  **order-independence**. The last one compiles the same source twice with every mapping's entry
  order reversed and diffs the two documents. It is the general form of the two-order check, and it
  is what catches an interning collision: two declarations minting one node at a single pointer
  produce the same document read either way only if neither is the one that got there first. Both
  pointer collisions the compiler has had were invisible to a single-order suite.
- **Architecture test** (`internal/archtest`): import-graph assertions for the layering rules
  above, plus the rules the graph cannot express — which lowerings are mutually recursive (so a
  set that grows is noticed rather than measured), the method-per-type cap, and that no function is
  reached by tests alone, bar the test-support packages and the API kept, by name, for a caller to
  come.
- **Lint rules** (`.golangci.yml`; `internal/lintrules`, under `make vet`): what a linter can
  hold — which packages may build or write an `ir.TypeRegistry`, derive a name or build an ID, that
  no pipeline stage prints or logs, and that a switch over an IR enum names every member. Each rule
  is held to findings planted for it, since a clean tree cannot tell a rule from one that matches
  nothing.
- **Wire-conformance harness** (from milestone 3): expected request shapes (method, path,
  query, body keys) are derived *from the IR alone* — offline, deterministic, shared across
  every language emitter; generated SDKs run under HTTP interception and the requests are
  diffed. Request-side mismatches block; response-side mismatches inform. The decisive test of
  a generated SDK is the bytes it puts on the wire, not whether it compiles.

## 6. Milestones

1. **IR + OpenAPI 3.x compiler** — `ir` package, validate pass, golden corpus, JSON round-trip.
2. **Swagger 2.0 lift** — normalization into the OpenAPI compiler; proves the
   format-version-normalization seam.
3. **First emitters** — Go, Kotlin/JVM and Kotlin Multiplatform end-to-end from one plan; proves the
   plan/refine/emit boundary, the plan's neutrality across three targets, and that the IR retains
   everything a refiner needs ([the milestone plan](./emitters/milestones.md)).
4. **Second family compiler (TypeSpec or Smithy)** — proves the spec-agnostic claim: richer-than-
   OpenAPI concepts (interfaces, custom scalars, lifecycle visibility, declared pagination) flow
   through untouched IR code.
5. **Event-shaped compiler (AsyncAPI)** — proves channels/messages/bindings; then GraphQL,
   Protobuf, and Erlang/OTP (the actor-protocol compiler: behaviours → operations + channels).
