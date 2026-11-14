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
  where the key's own source holds; a path or a component renamed changes its key too, and is the
  structural tier's ([open question 5](../ir/open-questions.md)). `Generate` returns the generated
  side directly; only the extractor parses source.
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
  the surface, and carries the facts its verdict turns on:
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
| A parameter added to a method or removed from one, or an argument a constructor requires added | breaking |
| A member added to a sum a caller's match must exhaust | breaking |
| A declaration added; a member added to a type; a member added to a sum no match need exhaust | additive |
| Documentation or a deprecation mark changed | none |

**Behavioral**, by what changed in the API:

| API change | Verdict |
|---|---|
| An operation removed, or a media type the SDK sends or reads | breaking |
| A binding changed: method, path, a parameter's location, style or wire name | breaking |
| A request member or parameter added, or made required | breaking if required, none if optional |
| A response member added | breaking where its model is closed, which decodes strictly ([unknown members](refine/models.md#unknown-members)); none otherwise |
| A response member made optional | the target's: breaking where its decoder refuses a body lacking a member its type requires |
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
breaking record makes it major, else an additive one minor, else a file whose digest moved
([what the manifest records](write-integrate.md#what-the-manifest-records)) a patch, and a
regeneration that changed no file has none. A behavioral record never raises it. It says what the
SDKs already deployed will meet, which no version of the new one changes, so those records lead
the report and leave the level alone.

The engine reports the level and does not apply it. No option names an SDK's version, the one the
user agent's placeholder waits on too ([runtime policy](contract/runtime-policy.md)), and what a
level becomes is its owner's to say: below `1.0.0` a breaking change need not be a major one, and
on Go a major from `v2` up is a new module path, an option only its owner changes
([Go's names](targets/go/naming.md)). A first generation has no prior, and so no level.
