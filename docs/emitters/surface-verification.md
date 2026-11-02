# Surface verification / compat

Post-milestone. `EmitterOutput.Surface` is the neutral API-surface projection — classes, interfaces,
enums, methods, fields as neutral records — built by the **same walk** over `Origin`-tagged AST nodes
that emit performs.

- **Neutral projection with IR IDs.** Both the freshly-generated SDK and an extracted existing SDK
  (via a per-language extractor) project into one `verify.Surface`; each record carries its **IR
  stable ID** (threaded from `Origin`), so cross-language findings correlate without name-matching,
  and a rename that keeps the entity's pointer is an ID lookup rather than oagen's
  Jaccard-similarity heuristics. One that moves the pointer, such as a path renamed, is found by the
  correlation key (#679) the manifest records. `Generate` returns the generated side directly; only
  the extractor parses source.
- **Injectable per-language severity.** The differ emits **neutral change records**; a
  per-target `verify.Severity` function decides breaking/additive (parameter names are public API in
  PHP, arity in Go, ~nothing in JS) — heuristics-as-policy (INV6).
- **An extractor per target.** Each first target's extractor and where its severity differs are in
  its document ([Go](targets/go/overview.md#merging-and-surface-extraction),
  [Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)).
- **Behavioral channel separate from structural.** A default-value change is behavioral,
  tracked in a distinct channel from a signature change — detectable without re-parsing because the
  plan already carries defaults as typed `Value`s. Every finding carries drift provenance (which
  stage caused it).

## What a change record carries

Some classifications turn on facts beyond the symbol that changed, so a neutral record carries them,
and a target's severity reads them rather than guessing:

- **Direction.** Each record names the usages of the entity it concerns, from `Facts.Usage`
  ([derived facts](../ir/derived-facts.md#usage)): an enum or a union gaining a member is additive
  for a value the SDK sends and changes what a deployed SDK reading a response meets, unless the
  type is `Open` ([plan policy](plan/policy.md)). A type used both ways is judged both ways, and the
  more severe verdict counts.
- **Optionality in a response.** A response member going from required to optional breaks a typed
  target, Go's `string` becoming `*string`, and is easy to miss, since the same change in a request
  is additive. The record says which side the member is read on.
- **Bindings.** A changed method, path, parameter location or serialization style breaks a call
  without changing a type, so it is a record of the behavioral channel, beside a changed default.

Two channels answer two questions. The structural channel asks whether a caller's code still
builds against the regenerated SDK. The behavioral channel asks whether a deployed SDK, built
against the old document, still works against the API the new one describes, which a new member of
a closed response enum does not.

## Severity, seeded

Each target's `verify.Severity` starts from the rules every typed target shares, one verdict per
channel, and states where it departs ([Go](targets/go/overview.md#merging-and-surface-extraction),
[Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)):

| Change | Structural: callers' code | Behavioral: deployed SDKs |
|---|---|---|
| An entity removed, or renamed with no correlation key carrying it | breaking | breaking |
| A request member or parameter added | additive if optional, breaking if required | breaking if required |
| A request member made required | breaking | breaking |
| A response member added | additive | additive |
| A response member made optional | breaking on a typed target | breaking |
| A union or enum gaining a member, read in a response | the target's: breaking where a match over it must be exhaustive, as a Kotlin `when` over an `enum class` | breaking unless `Open` |
| A union or enum gaining a member, only sent | additive | additive |
| A success status added | breaking where it changes a return ([return shapes](operations/returns.md)) | breaking |
| A failure status added | additive | additive |
| A media type removed | breaking | breaking |
| A binding changed: method, path, parameter location or style | none | breaking |
| A default changed | none | reported |

## The version a regeneration takes

A regeneration's version bump follows from what changed, never from a digest of the spec's text.
The structural channel sets it, since an SDK's version is a promise to its callers' code: a
breaking record makes it major, an additive one minor. A behavioral record makes it at least minor,
since the regenerated SDK behaves differently from the last. A regeneration with none of those that
still changed an artifact, through a step whose version moved
([verification](verification.md#during-steps-carry-versions)) or a document whose wording did,
takes a patch, and one that changed no artifact takes none, which the manifest's digests show
([write / integrate](write-integrate.md#what-the-manifest-records)).

