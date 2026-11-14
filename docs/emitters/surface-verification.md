# Surface verification / compat

Post-milestone. `EmitterOutput.Surface` is the neutral API-surface projection — classes, interfaces,
enums, methods, fields as neutral records — built by the **same walk** over `Origin`-tagged AST nodes
that emit performs.

- **Neutral projection with IR IDs.** Both the freshly-generated SDK and an extracted existing SDK
  (via a per-language extractor) project into one `verify.Surface`; each record carries its **IR
  stable ID** (threaded from `Origin`), so cross-language findings correlate without name-matching,
  and a rename that keeps the entity's pointer is an ID lookup rather than oagen's
  Jaccard-similarity heuristics. One that moves the pointer, such as a renamed path variable, is
  found by the [correlation key](../ir/derived-facts.md#correlation-keys) the manifest records,
  where the key's own source holds; a path or a component renamed changes its key too, and nothing
  derived carries it ([open question 5](../ir/open-questions.md)), so it reads as one entity
  removed and another added. `Generate` returns the generated
  side directly; only the extractor parses source.
- **Injectable per-language severity.** The differ emits **neutral change records**; a
  per-target `verify.Severity` function decides breaking/additive (parameter names are public API in
  PHP, arity in Go, ~nothing in JS) — heuristics-as-policy (INV6).
- **An extractor per target.** Each first target's extractor and where its severity differs are in
  its document ([Go](targets/go/overview.md#merging-and-surface-extraction),
  [Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)).
- **Behavioral channel separate from structural.** A change of the API that an SDK already
  deployed meets, a changed default among them, is behavioral, tracked in a distinct channel from a
  signature change ([below](#what-a-change-record-carries)) — detectable without re-parsing because
  the document and the plan already carry it as typed facts. Every finding carries drift provenance
  (which stage caused it).

## What a change record carries

Two channels answer two questions, and a record belongs to the channel whose question it answers:

- **Structural: does code written against the last generation still work with this one?** A
  structural record is a change of the surface: a declaration, a member, a parameter or a result
  added, removed or retyped. It is keyed as the manifest keys a symbol, by its entity's ID and the
  derivation that follows it ([write / integrate](write-integrate.md)), so a member's record names
  the shape declaring it, as `User.ID#create` does. What working means is each target's: for Go,
  that a caller's code builds, and for a JVM target also that a caller compiled against the last
  generation still links ([Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)).
- **Behavioral: does an SDK already deployed, generated from the old document, still work against
  the API the new one describes?** A behavioral record is a change of the API, whatever it does to
  the surface, so this channel reads two documents and no surface. The old one is the prior
  revision of the spec, which the run names and this build compiles beside the new: a manifest
  holds its document's digest alone, and an IR an older build stored could be one this build
  refuses, or carry keys of another derivation ([correlation
  keys](../ir/derived-facts.md#correlation-keys)). The old revision is compiled, passed, sliced and
  planned as the new one is, under this run's options, and a sliced document is compared label by
  label. So a verdict reading a plan fact, whether an enum is `Open`, describes an SDK deployed
  under the configuration this run has, and where the prior manifest's `Config` shows another the
  report says so. The channel reports nothing, under an info `engine/no-prior-revision`, for a run
  naming no prior revision, for one whose prior revision does not compile under these options, as
  an overlay written for the new one can refuse the old, and for a label only one side has. A
  record carries the facts its verdict turns on:
  - **Direction.** For an enum or a union, the usages `Facts.Usage` gives its type
    ([derived facts](../ir/derived-facts.md#usage)): a member gained matters to a deployed SDK
    reading the type and not to one only sending it. For a model's member, its lifecycles too
    (`Visibility`): a `readOnly` member of a model used both ways is only read, where the type's
    usage alone would judge it as sent.
  - **The side an optionality change is read on.** A response member going from required to
    optional is easy to miss, since the same change in a request only relaxes what a caller gives.
  - **Bindings.** A changed method, path, parameter location, serialization style or wire name
    breaks a call without changing a type, so it is a record of this channel, beside a changed
    default.

  Its rules speak of what an SDK sends and reads, so for an operation the API sends, a webhook or
  a callback, the request is what the SDK reads and the response what it sends
  ([inbound](operations/inbound.md)).

One change of the API can leave a record in each channel, and the two need not agree. A member
added to an enum a response carries is a constant added on Go, additive for a caller's code, while
an SDK deployed with that enum closed fails on the first response carrying the member.

## Severity, seeded

Each target's `verify.Severity` starts from the rules the first targets share and states where it
departs ([Go](targets/go/overview.md#merging-and-surface-extraction),
[Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)). A structural verdict is
breaking, additive or none, and a behavioral one breaking or none. A severity is total over the
differ's record kinds, so no change goes unjudged.

**Structural**, by what changed in the surface. What a change of the API does to the surface is
each target's lowering, so these rules read the declarations and never the API: a response member
made optional retypes a Go field from `string` to `Opt[string]` and leaves as it was one held
through a pointer on a recursive reference ([Go](targets/go/lowering.md)), and an operation gaining
its first input beyond the path gains a request argument ([request shaping](refine/requests.md)).

| Surface change | Verdict |
|---|---|
| A declaration, a member, a method or a constant removed; a rename is a removal unless the entity's correlation key carries the symbol it shipped ([naming](refine/naming.md#collisions-and-the-names-a-generation-shipped)) | breaking |
| The declared type of a member, a parameter or a result changed | breaking |
| A declaration's own kind or underlying type changed, an alias made a type of its own among them | breaking |
| A parameter added to a method or removed from one, or an argument a constructor requires added | breaking |
| A parameter renamed | the target's: breaking where a caller can name an argument |
| A member added to a sum a caller's match must exhaust | breaking |
| A declaration added; a member added to a type; a member added to a sum no match need exhaust; a deprecation mark added, where the target's extractor reads one | additive |
| Documentation changed | none |

**Behavioral**, by what changed in the API:

| API change | Verdict |
|---|---|
| An operation removed, or a media type the SDK sends or reads | breaking |
| A binding changed: method, path, a parameter's location, style or wire name | breaking |
| A request member or parameter added, or made required | breaking if required, none if optional |
| A response member added | breaking where its model is closed, which decodes strictly ([unknown members](refine/models.md#unknown-members)), or holds a typed catch-all the member's value does not fit; none otherwise |
| A response member's type changed | breaking |
| A response member removed, or made optional or nullable | the target's: breaking where the deployed declaration cannot hold the member's absence or its null, which follows from how the target declares a required member ([Go](targets/go/models.md#optionality), [Kotlin](targets/kotlin/models.md#optionality)) |
| An enum or a union gaining a member, read in a response | breaking where the type is closed and, for an enum, declares no `FallbackMember`; none where it is `Open`, as the default policy makes each one it can ([plan policy](plan/policy.md)) |
| An enum or a union gaining a member, only sent | none |
| A success status added | none where it carries the body the operation returns, since a 2xx the document does not list is read as the return ([return shapes](operations/returns.md)); breaking where it carries another |
| A failure status added | none: a status no declared case matches is the error taxonomy's to classify ([failures](operations/errors.md)) |
| A default changed | none: a default is documented and never sent ([values](refine/values.md#a-default-is-documented-never-sent)), so the record is shown and breaks nothing |

The tables are seeds. Which further changes of the API the behavioral channel reports is [open
question 6](open-questions.md), decided against real cross-revision diffs.

## The level a regeneration reports

A regeneration's version level follows from what changed, never from a digest of the spec's text.
The structural channel sets it, since an SDK's version is a promise to its callers' code: a
breaking record makes it major, else an additive one minor, else a file written, pruned or whose
digest moved ([what the manifest records](write-integrate.md#what-the-manifest-records)) a patch,
and a regeneration that changed no file has none. A deprecation is additive, and so a minor, as
semantic versioning asks, on a target whose extractor reads the mark, as Go's does and Kotlin's
ABI dumps are not shown to ([Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)).
Where the surface was not compared, a target with no extractor (`Capabilities.SurfaceVerify`
unset), there is no level to report, which an info `emitters/<target>/level-unknown` says: a patch
read off the digests alone could be a break. A behavioral record never raises the level. It says
what the SDKs already deployed will meet, which no version of the new one changes, so those records
lead the report and leave the level alone.

The engine reports the level and does not apply it, because an SDK's version is no input of a
generation. Its owner chooses it after reading the level, so a generation that wrote the version
into a file would have to run twice for each release, and where the version lives is each
ecosystem's release tooling already: a Go module's is the tag its repository gives it, which no file
of the module states, and a Gradle build's is the `version` its owner sets
([Kotlin](targets/kotlin/overview.md#what-a-caller-reaches-of-the-runtime)). So no option names one
and no generated file states one, a README's installation block among them, which names what to
install and leaves the version to its reader, and a release changes no generated byte, unless its
owner writes the version into the `User-Agent` template, the one place a configuration can spell it
([runtime policy](contract/runtime-policy.md)). What a level becomes is its owner's to say as well:
below `1.0.0` a breaking change need not be a major one, and on Go a major from `v2` up is a new
module path, an option only its owner changes ([Go's names](targets/go/naming.md)).

The level is measured from the prior manifest, the last generation written, and not from the last
release, which the engine does not know. An owner who regenerates several times between two releases
takes at most the highest level those generations reported, a change made and undone between them
needing none. A first generation has no prior, and so no level.