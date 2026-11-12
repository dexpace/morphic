# The emitter contract

Package `emitters` is the Layer-2 contract, as package `compilers` is the compilers'. It imports
**only** `ir` and `ir/irfacts` (Layer 0) plus its own shared sub-packages (`plan`, `policy`,
`naming`, `typemap`, `manifest`, `verify`) — never a compiler, never `engine`, never a sibling
target package (INV1; architecture.md §3). Enforced by the import-graph test ([package
layout](../packages.md)).

```go
package emitters

// TargetKey identifies an emitter in the registry: "go", "kotlin-jvm", "kotlin-multiplatform",
// "docs", "mock/go", …
type TargetKey string

// Emitter is one target — a language SDK, a docs site, a mock server, a validator.
// Pure and reentrant: no package-level mutable state, no clock, no randomness, no stderr,
// no filesystem I/O. Same input ⇒ byte-identical output (INV5, INV7).
type Emitter interface {
    Target() TargetKey
    Capabilities() Capabilities          // lets the engine reason without running (registry.md)
    DecodeOptions(raw []byte) (TargetOptions, []ir.Diagnostic, error) // strict, versioned (options.md)
    Generate(context.Context, EmitterInput) EmitterOutput // the whole emitter, as one pure function
}

// Router is implemented by a target whose Capabilities.CrossVersion is set. The engine calls it
// on every version snapshot before any Generate, with the input that snapshot's Generate takes but
// for Samples and Routes, and hands each run all the tables in EmitterInput.Routes
// (../docs-and-mock.md#routes-and-links). It is pure, as Generate is, and reports nothing: each
// run plans its own routes by the same function and reports what it finds. The engine asks the
// context after it returns, as after Generate, and a done one fails the run.
type Router interface {
    Routes(context.Context, EmitterInput) RouteTable
}

// SampleReader is implemented by the options of a target whose Capabilities.ReadsSamples is set:
// the targets whose samples it shows, which the engine runs before it
// (../docs-and-mock.md#samples-come-from-the-sdk-emitters).
type SampleReader interface {
    SampleTargets() []TargetKey
}

type EmitterInput struct {
    Doc     *ir.Document          // the ABI — the ONLY structural input (INV1)
    Facts   *irfacts.Index        // what Doc implies: subtypes, usage, recursion, correlation keys (docs/ir/derived-facts.md), indexed ONCE by the engine, shared with the plan (below)
    Plan    *plan.Plan            // language-neutral decisions, computed ONCE by the engine (../plan/overview.md)
    Policy  policy.Resolved       // runtime/SDK behavior per operation, its layers resolved —
                                  // a SEPARATE tree, never in the IR (INV10; runtime-policy.md,
                                  // embedded-policy.md)
    Hints   map[EntityID]ShapeHint// SDK-shaping hints keyed by IR stable ID, their layers resolved
                                  // (shaping-hints.md)
    Scope   *Scope                // nil = whole document; else "emit only these" — full Doc still
                                  // consumed so global decisions stay byte-identical
    Prior   *PriorState           // prior manifest + prior surface for write/integrate; nil first run
    Snapshot string               // the label Document.Versions gives the snapshot Doc is; "" for
                                  // one no pass sliced by version, a document stating none among
                                  // them (../docs-and-mock.md#routes-and-links)
    Samples map[TargetKey]map[ir.OpID][]Sample // the usage samples of each target a docs
                                  // target's options name
                                  // (../docs-and-mock.md#samples-come-from-the-sdk-emitters)
    Routes  map[string]RouteTable // every version snapshot's routes, keyed by the label
                                  // Document.Versions gives it, "" for one no pass sliced,
                                  // for a target whose Capabilities.CrossVersion is set
                                  // (../docs-and-mock.md#routes-and-links)
    Options TargetOptions         // the target's own typed options, decoded and validated before
                                  // Generate runs, its type table and naming tables among them
                                  // (options.md, type-table.md, ../refine/naming.md)
}

type EmitterOutput struct {
    Artifacts   []Artifact          // files (+ integration directives) — no I/O performed here
    Diagnostics []ir.Diagnostic     // typed, coded, provenance-bearing; engine decides fatality
    Manifest    *manifest.Manifest  // entity→symbol map keyed by IR stable IDs (../write-integrate.md)
    Surface     *verify.Surface     // neutral API-surface projection of the generated output (../surface-verification.md)
    Samples     map[ir.OpID][]Sample // an SDK target's usage samples per operation, one for each
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

// EntityID is the string form of any IR stable ID (TypeID, OpID, PropID, ServiceID, …).
// Every IR ID is a `type X string`, so one keyspace addresses them all.
type EntityID = string

// Artifact is oagen's GeneratedFile shape, minus every SDK-policy-shaped field.
type Artifact struct {
    Path         string          // deterministic, forward-slash, relative to the output root and
                                 // held inside it (../emit/rules.md)
    Content      []byte
    Kind         ArtifactKind    // source | test | doc | manifest | dependency-manifest | scaffold
    Header       HeaderPlacement // where the generated-by provenance header goes (top/after-shebang/none)
    SkipIfExists bool            // scaffolding written once (config, .gitignore) — never overwritten
    Overwrite    bool            // regenerate but preserve ignore-regions (default is additive)
    Integrate    *IntegrateHint  // path-prefix stripping / tree-shake root for live-repo merge, and
                                 // whether the file is merged as blocks (../write-integrate.md)
    Origins      []EntityID      // IR IDs whose emission produced this file (drives ID-keyed prune)
}
```

`error` is not part of the `Generate` signature: an emitter has no failure mode that is not either a
`Diagnostic` (something about the input it cannot lower) or a programmer bug: a `TypeKind` with no
dispatch, caught by the switch-completeness test in the suite ([mandatory
tests](../testing/mandatory-tests.md)) rather than at runtime, or an options value of another
target's type, reported as an error diagnostic ([target options](options.md)). This keeps the
contract pure and total.

`Generate` takes the run's context, for the reason a pass's `Run` does (architecture.md §2.2):
emitting an SDK costs time in proportion to the document, and a deadline or a cancel must reach it.
An emitter asks the context between artifacts and inside its walks, and stops soon once it is done.
The engine asks it after `Generate` returns, and a run whose context is done by then fails with the
context's error, whatever `Generate` returned: an emitter cut short may have stopped anywhere, and
no artifact of it is written.

## Who computes the plan

`plan.Build(doc, facts, planPolicy)` is a pure function ([the plan](../plan/overview.md)). For a
multi-target run the **engine computes it once** and passes the same `*plan.Plan` into every
`Generate` call. This is what makes "computed once, shared, cannot drift" literally true rather than
a convention, and it lets the plan be cached and golden-tested on its own corpus, independent of any
language. Plan policy is document-level, not per-target — the plan is language-neutral by
definition, so there is nothing per-language to vary in it. A single-target invocation may compute
the plan inline; the function is the same.

The derived facts work the same way. The engine builds one `irfacts.Index` for the document once the
last pass has run, since an index answers for the document it was built from and a pass may change
it ([derived-facts](../../ir/derived-facts.md)), and hands that index to `plan.Build` and, as
`EmitterInput.Facts`, to every `Generate`. So the strongly connected components a refiner reads to
break a recursive reference ([recursive references](../refine/models.md#recursive-references)) are
found once per document, not once per target. An index is safe for concurrent use, so targets
running side by side share it. It is not serialized: a consumer outside the process rebuilds it from
the document, which determines it.

## Contract rules the type system and tests enforce

- **Purity / determinism.** No I/O, clock, randomness, or stderr inside `Generate`. Same
  `(Doc, Plan, Policy, Hints, Scope, Prior, Snapshot, Samples, Routes, Options)`, under a context
  that stays live, ⇒ byte-identical `Artifacts`, the prior manifest included since the names a
  generation shipped are kept
  ([naming](../refine/naming.md#collisions-and-the-names-a-generation-shipped)); `Facts` is a
  function of `Doc` and adds nothing to the tuple. `Snapshot` sets two snapshots of one document
  apart, and `Samples` and `Routes` are other runs' output, so a docs target's artifacts change when
  an SDK target's options or another snapshot do; the manifest records all three ([write /
  integrate](../write-integrate.md#what-the-manifest-records)). Maps are walked in sorted-key order,
  slices in source order ([T-6](../testing/mandatory-tests.md)).
- **Diagnostics are the only input-failure channel.** A construct the emitter cannot lower emits
  `ir.Diagnostic{Severity, Code:"emitters/<target>/…", Message, Provenance}` — never a silent
  fallback, never a panic ([T-7](../testing/mandatory-tests.md)). Codes live in a stable
  `emitters/<target>/…` namespace.
- **Full document under scope.** Scope gates *emission*, not the plan or naming; global decisions
  are computed over the whole document so a scoped run is byte-identical to the corresponding slice
  of a full run. Scope is never a filtered `Document`.
- **Snapshots only.** The engine runs `version-slice` before any emitter; the emitter receives a
  single version snapshot and never interprets `Availability`. `Snapshot` names which one, and
  `Routes` holds every snapshot's route table, its own among them, never another's document. A
  target whose `Capabilities.CrossVersion` is set is run once on each snapshot. Each run writes its
  artifacts under the directory its label names, the label itself as one path segment, and the `""`
  snapshot's at the output root, the one exception being the pages its last label writes for what
  spans versions ([docs and mocks](../docs-and-mock.md#routes-and-links)). So no two runs share a
  path, and the runs make one generation ([write /
  integrate](../write-integrate.md#what-the-manifest-records)). A label is checked as a path only in
  a run that writes it as one, a run of at least one target whose `CrossVersion` is set: there a
  label that is no single segment, or is `.` or `..`, is an error `engine/snapshot-label-invalid`
  before any target runs. A run of other targets alone writes one snapshot at the output root
  (below) and no label's directory, so it refuses none: `2024-01-01T00:00Z`, whose `:` Windows
  refuses in a name, is a version an SDK is generated for. Two labels that
  fold alike, by the fold the engine compares artifact paths by ([emit rules](../emit/rules.md)),
  would name one directory on a file system that ignores case, as macOS's and Windows' do by
  default. So each label, in `Document.Versions` order, names its directory where that folds as no
  directory an earlier label took, and otherwise the label followed by `-` and the lowest number,
  `2` up, that does not: `v1`, `V1` and `V1-2` write under `v1`, `V1-2` and `V1-2-2`. Every run
  reads `Document.Versions` whole, so each derives the same directories; a version added after the
  others moves none, and one removed can return a later label to its own name. A set of the folded
  directories taken, each base holding the next number to try, keeps this linear in the labels. Each
  directory so named, its suffix included, is held to what every path segment is ([emit
  rules](../emit/rules.md)): one past 255 bytes, a Windows device name up to its first dot, or one
  holding a character Windows refuses or ending in `.` or a space is the same error, naming the
  label and the rule it breaks, before any target runs, where the check of artifact paths would
  refuse it
  only once every target had generated. Such a label is refused where two that fold alike are told
  apart: a suffix leaves the label whole in its directory and in every link, and a directory escaped
  or cut to length would be a version's address under a name the API never gave it. Keeping a
  label's directory from the prior manifest while it stays free, as a shipped name keeps its symbol,
  was not taken either: it serves only two labels that fold alike once the earlier is removed, and
  each run would then need the manifest to derive what it now reads off the document. Refusing two
  labels that differ in letter case alone, as this rule once did, failed a run on a valid document
  whose author could part them only by renaming a version of the API, the reason two names that fold
  alike are told apart rather than refused
  ([collisions](../refine/naming.md#collisions-and-the-names-a-generation-shipped)). Any other
  target, an SDK, a mock server, a CLI or a validator, writes one snapshot: the one whose label
  `RunOptions.Version` names. A run asking such a target for its artifacts from a sliced document
  without the setting is an error `engine/target-unversioned` before any run, its message naming the
  labels. It writes the snapshot at the output root, as an unsliced run does, its manifest a
  `Manifest` whose IDs, which every snapshot shares, carry the names it shipped when the label
  changes, and it is run on every other snapshot only for its samples, which writes nothing and
  which a docs page shows under that snapshot's label ([docs and
  mocks](../docs-and-mock.md#samples-come-from-the-sdk-emitters)). A label the setting names that a
  document stating versions lacks is an error `engine/snapshot-unknown` before any run. On a
  document stating none the setting selects nothing: the run writes the one snapshot, `""`, and
  reports an info `engine/version-unused`. So a pipeline names its label before the revision at
  which the document first states versions, and that revision changes no setting and, the manifest's
  IDs being every snapshot's, no shipped name; refused there, no value would be right on both sides
  of the revision, a set one failing before it and an unset one after. A pipeline that set none
  fails at that revision with `engine/target-unversioned`, which names the labels to choose from.
  The setting
  is the run's, not each target's, so an SDK and the mock server its wire conformance runs against
  read one version (T-69 holds this). Writing a unit for each snapshot under the one module path,
  Gradle coordinate or framework was rejected: a Kotlin Multiplatform framework gives every unit one
  Objective-C namespace ([Swift](../targets/kotlin/swift.md)), and Kotlin/Native 2.4.0 exports
  `sdk.v1.Zone` and `sdk.v2.Zone` as `Zone` and `Zone_`, so each class a second version keeps loses
  its name in Swift; each entity becomes a type per version, a value of one passing to no method of
  another; and a mock server or a CLI answering every version needs to know how a request names its
  version, which no IR field states, an `api-version` being an ordinary parameter of each operation.
  Refusing every sliced document would leave a versioned API with no SDK. A run wanting several
  versions runs once per label, each into an output root of its own. Defaulting to the last label,
  as the newest, was rejected: `Document.Versions` orders the labels and says of none that an SDK
  ships it, so a document appending a preview label would move the SDK, its mock server and a CLI to
  it on the next run, while only an SDK has a surface differ to report what that breaks. The pass is
  designed, not built (architecture.md §2.2); until it is, no pass slices a document, and every
  run's `Snapshot` is `""`, the one label a cross-version run's `Routes` keys.
