# The emitter contract

Package `emitters` is the Layer-2 contract, as package `compilers` is the compilers'. It imports
`ir`, `ir/irfacts`, `barrier`, `pathfold` and the shared packages whose types its own declarations
name (`plan`, `policy`, `hints`, `symbol`, `manifest`, `verify`), never a compiler, never `engine`,
never a target package, and no shared package imports it (INV1; architecture.md §3). Enforced by
the import-graph test ([package layout](../packages.md)).

A target is two values. The `Emitter` is constant: what the registry lists and the engine looks up.
The `Generator` is that target bound to the options one configuration gave it
([below](#a-target-is-configured-once)).

```go
package emitters

// TargetKey identifies an emitter in the registry: "go", "kotlin-jvm", "kotlin-multiplatform",
// "docs", "mock/go", …
type TargetKey string

// Emitter is one target — a language SDK, a docs site, a mock server, a validator.
type Emitter interface {
    Target() TargetKey
    Capabilities() Capabilities // lets the engine reason without running (registry.md)
    // Configure decodes a configuration strictly, at the version it states, into the generator
    // that runs under it (options.md). It reads nothing but raw.
    Configure(raw []byte) (Generator, []ir.Diagnostic, error)
}

// Generator is a target bound to its decoded options, and immutable. Generate is the whole
// emitter, as one pure function. Pure and reentrant: no package-level mutable state, no clock,
// no randomness, no stderr, no filesystem I/O. Same input ⇒ byte-identical output (INV5, INV7).
type Generator interface {
    Configuration() Configuration
    Generate(context.Context, EmitterInput) EmitterOutput
}

// Configuration is what a generator was configured with, as the manifest records it
// (../write-integrate.md#what-the-manifest-records).
type Configuration struct {
    Version   int    // the options version the configuration stated, 1 or later
    Canonical []byte // the decoded options, in the current shape, as canonical JSON
}

// Router is implemented by the generator of a cross-version target. The engine calls it on every
// version snapshot before any Generate, with the input that snapshot's Generate takes but for
// Samples and Routes, and hands each run all the tables in EmitterInput.Routes
// (../docs-and-mock.md#routes-and-links). It is pure, as Generate is, and reports nothing: each
// run plans its own routes by the same function and reports what it finds. The engine asks the
// context after it returns, as after Generate, and a done one fails the run.
type Router interface {
    Routes(context.Context, EmitterInput) RouteTable
}

// SampleReader is implemented by the generator of a target that shows other targets' samples:
// the targets its options name, which the engine runs before it
// (../docs-and-mock.md#samples-come-from-the-sdk-emitters).
type SampleReader interface {
    SampleTargets() []TargetKey
}

// Checker is implemented by an Emitter whose Capabilities.Build declares a check: it reads what a
// failed check printed into its messages. It is pure, as Generate is: the engine runs the check
// and maps each message to an entity (../verification.md#the-output-builds).
type Checker interface {
    Messages(CheckOutput) []CheckMessage
}

// CheckOutput is what one failed check printed, and what its tool's paths are resolved against.
type CheckOutput struct {
    Check  string   // the check's Name
    Output []byte   // both streams, cut at the engine's cap
    Root   string   // the output root's absolute path, as the tool was run in it
    Files  []string // the paths the run wrote, from the root, forward-slashed
}

// CheckMessage is one message of a check, at the written file and the line its tool named.
type CheckMessage struct {
    Path string // one of CheckOutput.Files; "" where the tool named no file, or none written
    Line int    // 1-based; 0 where the tool gave none
    Text string
}

type EmitterInput struct {
    Doc     *ir.Document           // the ABI — the ONLY structural input (INV1)
    Facts   *irfacts.DocumentFacts // what Doc implies: subtypes, usage, recursion, correlation keys (docs/ir/derived-facts.md), indexed ONCE by the engine, shared with the plan (below)
    Plan    *plan.Plan             // language-neutral decisions, computed ONCE by the engine (../plan/overview.md)
    Policy  policy.Resolved        // runtime/SDK behavior per operation, its layers resolved —
                                   // a SEPARATE tree, never in the IR (INV10; runtime-policy.md,
                                   // embedded-policy.md)
    Hints   map[EntityID]hints.ShapeHint // SDK-shaping hints keyed by IR stable ID, their
                                   // layers resolved (shaping-hints.md)
    Scope   *Scope                 // nil = whole document; else "emit only these" — full Doc still
                                   // consumed so global decisions stay byte-identical
    Prior   *PriorState            // prior manifest + prior surface for write/integrate; nil first run
    Snapshot string                // the label Document.Versions gives the snapshot Doc is; "" for
                                   // one no pass sliced by version, a document stating none among
                                   // them (../docs-and-mock.md#routes-and-links)
    Samples map[TargetKey]map[ir.OperationID][]Sample // the usage samples of each target a docs
                                   // target's options name
                                   // (../docs-and-mock.md#samples-come-from-the-sdk-emitters)
    Routes  map[string]RouteTable  // every version snapshot's routes, keyed by the label
                                   // Document.Versions gives it, "" for one no pass sliced,
                                   // for a cross-version target
                                   // (../docs-and-mock.md#routes-and-links)
}

type EmitterOutput struct {
    Artifacts   []Artifact          // files (+ integration directives) — no I/O performed here
    Diagnostics []ir.Diagnostic     // typed, coded, provenance-bearing; engine decides fatality
    Manifest    *manifest.Manifest  // entity→symbol map keyed by IR stable IDs (../write-integrate.md)
    Surface     *verify.Surface     // neutral API-surface projection of the generated output (../surface-verification.md)
    Samples     map[ir.OperationID][]Sample // an SDK target's usage samples per operation, one for each
                                     // language its callers write, for the docs
}

// Sample is one operation's call as an SDK target spells it.
type Sample struct {
    Language string                 // the fence's language, such as "go", "kotlin" or "swift"
    Code     string                 // the call, its values the plan's examples
}

// RouteTable is one snapshot's routes: each entity's page path from the output root, the
// snapshot's directory included, and its anchor, as a link to it is written.
type RouteTable map[EntityID]string

// EntityID is the string form of any IR stable ID (TypeID, OperationID, PropertyID, ServiceID, …).
// Every IR ID is a `type X string`, so one keyspace addresses them all.
type EntityID = string

// Artifact is oagen's GeneratedFile shape, minus every SDK-policy-shaped field.
type Artifact struct {
    Path         string          // deterministic, forward-slash, relative to the output root and
                                 // held inside it (../emit/rules.md)
    Content      []byte
    Kind         ArtifactKind    // source | test | doc | manifest | dependency-manifest | scaffold
    Header       HeaderPlacement // where its generated-by line stands: "top", "after-shebang", or
                                 // "none" for a file that can hold no comment (below)
    SkipIfExists bool            // scaffolding written once (config, .gitignore) — never overwritten
    Overwrite    bool            // regenerate but preserve ignore-regions (default is additive)
    Integrate    *IntegrateHint  // path-prefix stripping / tree-shake root for live-repo merge, and
                                 // whether the file is merged as blocks (../write-integrate.md)
    Origins      []EntityID      // IR IDs whose emission produced this file (drives ID-keyed prune)
    Spans        []Span          // the lines each declaration written under an Origin holds, so
                                 // a check's message maps to an entity (../verification.md)
}

// Span is the lines of Content one declaration holds, its documentation comment among them,
// 1-based and inclusive. A declaration inside another has a span of its own, as a method in a
// class and a field in a struct do, so a line belongs to the innermost span holding it.
type Span struct {
    From, To int
    Key      string // the manifest key of what it declares: its entity's ID and, for a symbol
                    // derived from one, the derivation (../write-integrate.md)
}
```

`error` is not part of the `Generate` signature: a generator has no failure mode that is not either
a `Diagnostic` (something about the input it cannot lower) or a programmer bug: a `TypeKind` with no
dispatch, caught by the switch-completeness test in the suite ([mandatory
tests](../testing/mandatory-tests.md)) rather than at runtime, or an output that breaks a duty,
which [the checked call](#the-checked-calls) refuses. This keeps the contract pure and total.

`Generate` takes the run's context, for the reason a pass's `Run` does (architecture.md §2.2):
emitting an SDK costs time in proportion to the document, and a deadline or a cancel must reach it.
A generator asks the context between artifacts and inside its walks, and stops soon once it is
done. The checked call asks it after `Generate` returns, and a run whose context is done by then
fails with the context's error, whatever `Generate` returned: a generator cut short may have stopped
anywhere, and no artifact of it is judged or written.

## A target is configured once

`Configure` reads a target's configuration, one JSON object, and returns the `Generator` that runs
under it, so what generates is bound to the options it was given. `Generate` reads the options its
own `Configure` closed over: no options value crosses the contract, so none can reach a target of
another type, and nothing asserts one back to its type.

- **A configuration's fault is a Go error**, the invocation's, as a compiler's unknown setting is:
  an unknown member, a missing or unknown version, a misplaced `null`, a value out of range
  ([target options](options.md)). The one diagnostic `Configure` returns is the upgrade report.
- **A generator is immutable.** The engine calls one `Generate` for each snapshot it runs the
  target on, and may call several at once.
- **`Configuration` is what the manifest records** of the options: the version the configuration
  stated and the digest of the decoded options ([write /
  integrate](../write-integrate.md#what-the-manifest-records)). `Canonical` is in the current
  shape, defaults filled, so two configurations that decode alike are one generation.
- **A capability an optional interface shows is not stated a second time.** A cross-version target
  is one whose generator implements `Router`, and a target shows other targets' samples where its
  generator implements `SampleReader`. Each is the configured target's, so the engine reads it off
  a target once that target is configured, and every target a run needs is configured before any
  generates. A flag beside the interface could disagree with it
  ([capabilities](registry.md#a-capability-an-interface-shows)).

Handing every `Generate` its options as a value of a shared interface, which each target asserted
back to its own type, was the shape before this one. The assertion could fail only by a
programmer's error, which the contract then had to report, and `Configure` leaves no such state.

## The checked calls

The engine configures and generates through two functions and no other way, as
`compilers.Detection.Compile` is the one way to compile. The registry reads a target's key and
capabilities behind the same barrier ([the registry](registry.md#the-registry)).

```go
// Bound is a target configured. Only Configure builds one, so no generator runs under another
// emitter's key.
type Bound struct{ /* the emitter's key and capabilities, and the generator it returned */ }

// Configure calls emitter.Configure behind a barrier and binds what it returns to the emitter.
func Configure(emitter Emitter, raw []byte) (Bound, []ir.Diagnostic, error)

// Generation is one target's output, held to the contract.
type Generation struct {
    Target        TargetKey
    Configuration Configuration
    Artifacts     []Artifact
    Diagnostics   []ir.Diagnostic
}

// Generate runs bound's generator behind a barrier and holds its output to the contract.
func Generate(ctx context.Context, bound Bound, input EmitterInput) (Generation, error)

// ErrContract is what a target breaking a duty of the contract is reported under.
var ErrContract = errors.New("emitter broke its contract")
```

A breach is a Go error wrapping `ErrContract` and naming the target: a bug in the target, never a
finding about the user's document, as a compiler's breach is an error and no diagnostic. So it
takes no code, no triage rule accepts it, and it fails the run at any threshold. A caller's own
mistake, a zero `Bound`, a nil document or a nil context, is an error that does not wrap it, since
the target did nothing wrong. Both calls run the target behind `barrier.Catch`, so a panic in one
is a breach too.

`Configure` refuses a nil generator returned with no error, a diagnostic coded outside the
target's own namespace ([below](#the-codes-a-target-reports)), and a `Configuration` stating a
version below 1. A configuration the target itself refuses is the target's error, passed on as it
is.

`Generate` holds a generation to these duties:

- **Paths**, each a rule of the [emit rules](../emit/rules.md): the artifacts sorted by `Path`; each
  path forward-slashed, local and naming a file; no two equal under the path fold; none a directory
  of another under the fold, and no directory spelled two ways under it; none the manifest's own or
  under it; each segment one every file system writes, and a file's own name short enough for the
  writer.
- **The header.** The generated-by line stands where `Header` says
  ([below](#the-generated-by-line)).
- **Diagnostics.** Each code is in a namespace the target may report, and each provenance names a
  source the document holds, or none.
- **Something said.** A generation holding no artifact carries the warning
  `emitters/<target>/no-artifacts` ([emit rules](../emit/rules.md)).
- **The context.** One done when `Generate` returns yields the context's error, which is no
  breach, with nothing judged or kept.

A duty over a member that lands later lands with it ([below](#the-structs-grow-with-their-readers)):
spans inside their content and nested, keys and origins naming entities of the document, samples
only from a target that declares them. The checks' work grows as the artifacts and the length of
their paths do, and no faster.

## The generated-by line

The header is the target's text and the engine's check. A target writes the line in its own
comment syntax around the shared marker `emitters.GeneratedMarker`, `Code generated by morphic`,
as Go's `// Code generated by morphic. DO NOT EDIT.` holds it. `Artifact.Header` says where: `top`,
the file's first line; `after-shebang`, its second; or `none`, for a file that can hold no comment,
as a JSON file cannot. The checked call holds the marker to that line, and refuses a placement
outside the three. A file with no marker is still the generation's own, by its digest ([write /
integrate](../write-integrate.md#a-file-is-judged-by-its-digest)).

## The codes a target reports

A target reports codes under `emitters/<target>/`, its key whole, and under the namespace of a
shared package whose findings it relays, which is `emitters/naming/` alone today
([collisions](../refine/naming.md#collisions-and-the-names-a-generation-shipped)).
`emitters/plan/…` is the plan's and `engine/…` the engine's, never a target's, and the registry
refuses a key that would make a target's namespace a shared package's
([the registry](registry.md#the-registry)). The mock's documents write its codes under
`emitters/mock/…`, which a key such as `mock/go` does not give it, and which of the two yields is
open (#2266).

## The structs grow with their readers

`EmitterInput`, `EmitterOutput`, `Artifact` and `Capabilities` are structs, so a member added later
breaks no target. Each lands with the members some code fills and some target or oracle reads, since
a
member with neither is held by no test:

| Struct | At landing | Lands later, with |
|---|---|---|
| `EmitterInput` | `Doc`, `Facts`, `Plan` | `Policy` and `Hints`: the first runtime templates. `Snapshot`, `Samples`, `Routes`: the docs target. `Scope`: the first scoped run. `Prior`: the manifest's entities (M4) |
| `EmitterOutput` | `Artifacts`, `Diagnostics` | `Samples`: the docs target. `Manifest`, `Surface`: M4 |
| `Artifact` | `Path`, `Content`, `Header` | `Kind`: the first reader of a kind. `Spans`: the build check. `SkipIfExists`: the first scaffolding file. `Overwrite`, `Integrate`, `Origins`: M4 |
| `Capabilities` | `Lifecycle`, `Successor` | `Protocols`, `RPCSystems`: the first client. `WritesSamples`: the docs target. `Build`: the build check |

The optional interfaces land the same way, `Router` and `SampleReader` with the docs target and
`Checker` with the build check, and so does each refusal of the registry over a member that is not
there yet. The engine composes the
file-level manifest itself, from what it wrote, from the first write on ([write /
integrate](../write-integrate.md#what-the-manifest-records)); `EmitterOutput.Manifest` carries the
entities a target declares, which only M4 reads.

## Who computes the plan

`plan.Build(doc, facts, planPolicy)` is a pure function ([the plan](../plan/overview.md)). For a
multi-target run the **engine computes it once** and passes the same `*plan.Plan` into every
`Generate` call. This is what makes "computed once, shared, cannot drift" literally true rather than
a convention, and it lets the plan be cached and golden-tested on its own corpus, independent of any
language. Plan policy is document-level, not per-target — the plan is language-neutral by
definition, so there is nothing per-language to vary in it. A single-target invocation may compute
the plan inline; the function is the same.

The derived facts work the same way. The engine builds one `irfacts.DocumentFacts` for the document
once the last pass has run, since an index answers for the document it was built from and a pass may
change it ([derived-facts](../../ir/derived-facts.md)), and hands that index to `plan.Build` and, as
`EmitterInput.Facts`, to every `Generate`. So the strongly connected components a refiner reads to
break a recursive reference ([recursive references](../refine/models.md#recursive-references)) are
found once per document, not once per target. An index is safe for concurrent use, so targets
running side by side share it. It is not serialized: a consumer outside the process rebuilds it from
the document, which determines it.

## Contract rules the type system and tests enforce

- **Purity / determinism.** No I/O, clock, randomness, or stderr inside `Configure` or `Generate`.
  Same configuration and same `(Doc, Plan, Policy, Hints, Scope, Prior, Snapshot, Samples,
  Routes)`, under a context that stays live, ⇒ byte-identical `Artifacts`, the prior manifest
  included since the names a generation shipped are kept
  ([naming](../refine/naming.md#collisions-and-the-names-a-generation-shipped)); `Facts` is a
  function of `Doc` and adds nothing to the tuple. `Snapshot` sets two snapshots of one document
  apart, and `Samples` and `Routes` are other runs' output, so a docs target's artifacts change when
  an SDK target's options or another snapshot do; the manifest records all three ([write /
  integrate](../write-integrate.md#what-the-manifest-records)). Maps are walked in sorted-key order,
  slices in source order ([T-6](../testing/mandatory-tests.md)).
- **Diagnostics are the only input-failure channel.** A construct the emitter cannot lower emits
  `ir.Diagnostic{Severity, Code:"emitters/<target>/…", Message, Provenance}` — never a silent
  fallback, never a panic ([T-7](../testing/mandatory-tests.md)). Codes live in a stable
  `emitters/<target>/…` namespace ([above](#the-codes-a-target-reports)).
- **Full document under scope.** Scope gates *emission*, not the plan or naming; global decisions
  are computed over the whole document so a scoped run is byte-identical to the corresponding slice
  of a full run. Scope is never a filtered `Document`.
- **Snapshots only.** The engine runs `version-slice` before any emitter; the emitter receives a
  single version snapshot and never interprets `Availability`. `Snapshot` names which one, and
  `Routes` holds every snapshot's route table, its own among them, never another's document. A
  cross-version target, one whose generator implements `Router`, is run once on each snapshot. Each
  run writes its
  artifacts under the directory its label names, the label itself as one path segment, and the `""`
  snapshot's at the output root, the one exception being the pages its last label writes for what
  spans versions ([docs and mocks](../docs-and-mock.md#routes-and-links)). So no two runs share a
  path, and the runs make one generation ([write /
  integrate](../write-integrate.md#what-the-manifest-records)). A label is checked as a path only in
  a run that writes it as one, a run of at least one cross-version target: there a
  label that is no single segment, or is `.` or `..`, is an error `engine/snapshot-label-invalid`
  before any target runs. A run of other targets alone writes one snapshot at the output root
  (below) and no label's directory, so it refuses none: `2024-01-01T00:00Z`, whose `:` Windows
  refuses in a name, is a version an SDK is generated for. Two labels that fold alike, by the fold
  the engine compares artifact paths by ([emit rules](../emit/rules.md)), would name one directory
  on a file system that ignores case, as macOS's and Windows' do by default. So each label, in
  `Document.Versions` order, names its directory where that folds as no directory an earlier label
  took, and otherwise the label followed by `-` and the lowest number, `2` up, that does not: `v1`,
  `V1` and `V1-2` write under `v1`, `V1-2` and `V1-2-2`. Every run reads `Document.Versions` whole,
  so each derives the same directories; a version added after the others moves none, and one removed
  can return a later label to its own name. A set of the folded directories taken, each base holding
  the next number to try, keeps this linear in the labels. Each directory so named, its suffix
  included, is held to what every path segment is ([emit rules](../emit/rules.md)): one past 255
  bytes, a Windows device name up to its first dot, or one holding a character Windows refuses or
  ending in `.` or a space is the same error, naming the label and the rule it breaks, before any
  target runs, where the check of artifact paths would refuse it only once every target had
  generated. Such a label is refused where two that fold alike are told apart: a suffix leaves the
  label whole in its directory and in every link, and a directory escaped or cut to length would be
  a version's address under a name the API never gave it. Keeping a label's directory from the prior
  manifest while it stays free, as a shipped name keeps its symbol, was not taken either: it serves
  only two labels that fold alike once the earlier is removed, and each run would then need the
  manifest to derive what it now reads off the document. Refusing two labels that differ in letter
  case alone, as this rule once did, failed a run on a valid document whose author could part them
  only by renaming a version of the API, the reason two names that fold alike are told apart rather
  than refused ([collisions](../refine/naming.md#collisions-and-the-names-a-generation-shipped)).
  Any other target, an SDK, a mock server, a CLI or a validator, writes one snapshot: the one whose
  label `RunOptions.Version` names. A run asking such a target for its artifacts from a sliced
  document without the setting is an error `engine/target-unversioned` before any run, its message
  naming the labels. It writes the snapshot at the output root, as an unsliced run does, its
  manifest a `Manifest` whose IDs, which every snapshot shares, carry the names it shipped when the
  label changes, and it is run on every other snapshot only for its samples, which writes nothing
  and which a docs page shows under that snapshot's label ([docs and
  mocks](../docs-and-mock.md#samples-come-from-the-sdk-emitters)). A label the setting names that a
  document stating versions lacks is an error `engine/snapshot-unknown` before any run. On a
  document stating none the setting selects nothing: the run writes the one snapshot, `""`, and
  reports an info `engine/version-unused`. So a pipeline names its label before the revision at
  which the document first states versions, and that revision changes no setting and, the manifest's
  IDs being every snapshot's, no shipped name; refused there, no value would be right on both sides
  of the revision, a set one failing before it and an unset one after. A pipeline that set none
  fails at that revision with `engine/target-unversioned`, which names the labels to choose from.
  The setting is the run's, not each target's, so an SDK and the mock server its wire conformance
  runs against read one version (T-69 holds this). Writing a unit for each snapshot under the one
  module path, Gradle coordinate or framework was rejected: a Kotlin Multiplatform framework gives
  every unit one Objective-C namespace ([Swift](../targets/kotlin/swift.md)), and Kotlin/Native
  2.4.0 exports `sdk.v1.Zone` and `sdk.v2.Zone` as `Zone` and `Zone_`, so each class a second
  version keeps loses its name in Swift; each entity becomes a type per version, a value of one
  passing to no method of another; and a mock server or a CLI answering every version needs to know
  how a request names its version, which no IR field states, an `api-version` being an ordinary
  parameter of each operation. Refusing every sliced document would leave a versioned API with no
  SDK. A run wanting several versions runs once per label, each into an output root of its own.
  Defaulting to the last label, as the newest, was rejected: `Document.Versions` orders the labels
  and says of none that an SDK ships it, so a document appending a preview label would move the SDK,
  its mock server and a CLI to it on the next run, while only an SDK has a surface differ to report
  what that breaks. The pass is designed, not built (architecture.md §2.2); until it is, no pass
  slices a document, and every run's `Snapshot` is `""`, the one label a cross-version run's
  `Routes` keys.
