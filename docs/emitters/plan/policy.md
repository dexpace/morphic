# plan.Policy — the plan's injectable heuristics

Everything the plan *infers* rather than reads is a knob here; every inferred output is marked
`Inferred` (INV6):

```go
type Policy struct {
    ContentRanking  []string           // default: json > form > multipart > binary
    PrimaryResponse []PrimaryRule      // default: lowest_2xx, unconditional, default; the first
                                       // rule an operation meets picks its primary (below)
    DefaultFails    bool               // default on: a Default no rule takes is the catch-all
                                       // of Errors (below)
    Envelopes       []Envelope         // default none: the envelopes a success body is read
                                       // through (below)
    Framings        map[string]Framing // media type or +suffix → how it frames a stream
                                       // (streams.md)
    FrameMembers    bool               // default on: an SSE schema naming only frame
                                       // fields describes the frame (streams.md)
    OpenUnions      bool               // default on: a closed union keeps a variant it
                                       // does not know (below)
    OpenEnums       bool               // default on: a closed enum keeps a value it does
                                       // not list (below)
    StrictExclusive bool               // default off: every one_of union is TypePlan.Strict
                                       // (../refine/unions.md)
    ProtocolEffects bool               // default on: an operation stating no effects takes
                                       // what its binding's protocol defines (below)
    MergePatch      bool               // default on: a PATCH body sent as a merge patch relaxes
                                       // the members its schema requires, and is the content
                                       // elected among those it ties with (below)
    Examples        ExamplePolicy      // default both on, depth 3, 2 items: what may be made where
                                       // no example is declared (../docs-and-mock.md)
    FirstPage       *int               // default 1: a page walk's first page where its page input
                                       // declares no default; nil guesses none (below)
}

type ExamplePolicy struct {
    Synthesize bool                    // off leaves an entity with none declared without one
    Assemble   bool                    // off leaves an operation declaring no success scenario
                                       // without one built from its inputs' sets, a message's
                                       // payload unpaired and an example Shown unfilled
    MaxDepth   int                     // how many models deep a value fills optional members,
                                       // its own the first; required ones go as deep as needed
    MaxItems   int                     // how many entries a list or map holds, raised to its
                                       // minItems or minProps, lowered to its maxItems or maxProps;
                                       // deeper than MaxDepth or on a recurrence, its minimum
}

type PrimaryRule string                  // "lowest_2xx" | "unconditional" | "default"
```

A test asserts that disabling a heuristic removes the corresponding `Inferred` plan field — the
guard against a heuristic silently defaulting on and quietly making the plan non-neutral.

## The policy is data

Every field is a value from a closed vocabulary the plan package defines, or a count such as an
example bound, and none is a function. So a policy encodes, compares and hashes as what it says, and
`Plan.Version` hashes it ([the plan](overview.md#the-plans-version)). A function has no bytes to
hash: its printed form is an address that changes every run, and skipping it gives two policies that
pick different responses one version. A heuristic the vocabulary lacks is added to it, in this
package, so it ships in a new build, which the plan cache keys on ([the
plan](overview.md#the-plans-version)). A strategy registered from outside by name and version was
weighed and refused: a function may close over what a run sets, so neither its name and version nor
the build fixes what it does, and two runs that pick differently would share a key. So was a
caller-supplied ID hashed in the policy's place, which fails the same way when the caller forgets to
change it.

A configuration is read as a target's options are ([target options](../contract/options.md)):
strictly, and over the defaults of the version it states, which `DefaultPolicy(version)` returns,
so a default that changes is a new version and an existing configuration keeps planning what it did.
One that states no version is refused, and so is an absent one, as a target's are, since which
defaults apply must not be guessed. The comments above name the current version's defaults. The
zero `Policy` is not the default: it turns every switch off and lists nothing, so no rule picks a
primary response and every operation returns `void`. A `Policy` built in Go meets no decoder, so
`Build` reports a rule outside the vocabulary or listed twice, and a negative example bound, as an
error `emitters/plan/policy-invalid`, and plans as if the rule were left out and the bound 0.

## The default response is classed here

The IR states an operation's `Default` — OpenAPI's `default` — as neither a success nor a failure,
since it answers every status the others do not name ([responses](../../ir/responses.md)).
`PrimaryResponse` reads the whole operation for that reason, rule by rule, and the first rule the
operation meets picks: `lowest_2xx` the lowest 2xx `Response`, an exact status before a range that
starts there; `unconditional` one with no condition, which is an RPC's one response; and `default`
the `Default`, which is the only description of a success an operation declaring neither has. A
`Default` that is not the primary response is the catch-all of `Errors`, with its condition, under
`DefaultFails`. Either use is marked `Inferred`, and a policy may take it as neither or both.
Leaving out the `default` rule and `DefaultFails` leaves an operation with no success response
`void` ([body shapes](responses.md#body-shapes)) and its `Default` in no plan field. `Streaming`
summarizes only the streams an operation's success responses declare, so no `StreamDetail` covers a
`Default`, and a primary one streams only where a content of it declares an `Item`
([streams](streams.md)).

## A union keeps a variant it does not know

An API that adds a variant must not break every SDK already deployed on the first response carrying
it, so under `OpenUnions` a closed union, a polymorphic base, or a model narrowing a closed union,
which the three targets lower as that union ([inheritance](../refine/models.md#inheritance)), is
`Open`, marked `Inferred`, whichever way it travels. A value no variant recognizes is then kept
rather than refused ([unions](../refine/unions.md)). The policy leaves closed only what the IR marks
`InputOnly`, a union, a base or a narrowing model: its identity is a request's alone, so no response
carries it, and the mark is the type's own ([open unions](../../ir/unions.md#open-unions)). A union
the IR declares open, `Closed` false as a Smithy union lowers or a promoted key makes one, is `Open`
too, and so is a model narrowing one, unmarked since the IR states it, as
[precedence](../contract/runtime-policy.md#precedence--declared-ir-facts-win-hard-rule) has a
declared fact win. Turning the policy off leaves every union as the IR says, and every polymorphic
base and every model narrowing a closed union closed. Opening a closed union no more overrides
`Union.Closed` than opening an enum overrides `Enum.Closed` (below): it says what this version of
the API admits, not what a later one will add ([open unions](../../ir/unions.md#open-unions)). The
plan records the choice so that docs and a mock agree with the SDK ([docs and
mocks](../docs-and-mock.md)).

`Open` is what a client survives, never what the API admits. A request carries what the declared
`Closed` admits: the fallback has no factory
([unions](../refine/unions.md#a-value-no-variant-recognizes)), so a caller builds only the variants
the document lists, and a value kept from a response goes out as it came. A mock holds a request to
the declared `Closed` and sends the examples the plan holds, each checked against it ([docs and
mocks](../docs-and-mock.md)), so opening a type changes nothing a mock accepts or sends. T-22 and
T-58 hold this section.

## An enum keeps a value it does not list

The reason unions are open holds for an enum: an API that adds a member must not break every SDK
already deployed on the first response carrying it. So under `OpenEnums` a closed enum is `Open`,
marked `Inferred`, whichever way it travels, and a value it does not list is kept rather than
refused ([enums](../refine/enums.md)). An enum the IR declares open, `Closed` false through a
promoted `x-extensible-enum` among others, is `Open` too, unmarked since the IR states it, so the
SDK, the docs and a mock each read one field. Two closed enums stay closed, each for what it
declares itself: one declaring a `FallbackMember`, since the document already says what a value it
does not list decodes to, and one with no members, which admits no value and which no policy may
widen ([the empty enum](../../ir/enums.md#the-empty-enum),
[enums](../refine/enums.md#an-enum-with-no-members)). Turning the policy off leaves every enum as
the IR says. Admission and the rules proving variants disjoint read the declared `Closed`
([TypePlan](types.md#variants-that-can-share-a-value)), so opening an enum changes no union's shape:
inside a union, a value an enum variant does not list is not that variant's, and an open union keeps
it as its own unknown.

This does not override the declared `Enum.Closed`, which states that the members the document lists
are all it admits. The SDK honours that in what it sends: its constants name the listed members
alone, and a mock refuses a request carrying any other ([docs and mocks](../docs-and-mock.md)).
`Closed` says nothing of what a later version of the API will list, and that is what a client
reading a response has to survive. On the golden spec (spec @bc6fad24, morphic @fa253fe19) all 7,687
enums are closed, since an OpenAPI `enum` is a closed value set, so without the policy each one a
response carries would fail to decode the first time the API added a member. T-36 and T-58 hold this
section.

## Neither policy reads which way a type travels

Neither policy reads `Facts.Usage`. Whether a type is read is a fact about the operations reaching
it, not about the type, and a declaration shaped by it moves when an operation elsewhere changes. A
rule opening only what a response carries was weighed and refused on that count. On the golden spec
(spec @bc6fad24, morphic @fa253fe19) 2,885 of 7,687 enums and 327 of 1,003 unions are only written.
One response newly returning a type only written, a model or a list as well as an enum or a union,
would move at least one of those declarations to its open form for 5,178 of the 10,157 such types,
and 76 of them for the largest, a request body. The API makes that change: of the enums and unions
both the spec @149d53ea of 2026-04-30 and @bc6fad24 declare under one ID, 32 enums and 4 unions went
from only written to read, and 5 and 4 from read to only written, which closes them again. Each move
breaks callers where a target's two forms are different declarations. Measured with Kotlin 2.4.0, a
caller compiled against a closed enum's `enum class` and run against its open form fails with
`NoSuchFieldError` where it names `Status.ACTIVE`, though it only sends the member, and an
exhaustive `when` over the enum, or over a sealed interface that gained `Unknown`, no longer
compiles. Go's two forms of an enum are one named type and an open union adds a wrapper to those a
closed one declares, so there opening a type takes nothing a caller names away
([Go](../targets/go/types.md#enums)).

Opening a type nothing reads costs what the closed form gave its callers. A caller no longer gets an
exhaustive `when` over a Kotlin enum it only sends, and an open enum holds any value of its value
type, so one a caller builds from text the document does not list reaches the API, which refuses it.
A union admits nothing more on the way out, its fallback having no factory. The designs this was
weighed against:

- **Opening only what a response carries, the move stated as a break.** A surface differ would
  report each move, but the moves follow changes the API makes without breaking its own clients,
  which is what the policies exist to survive.
- **A declaration for each direction**, closed where a client writes the type and open where it
  reads it. A type travelling both ways, as 858 of the golden spec's enums and 104 of its unions do,
  needs one type for a value read and sent back, and every other would be declared twice.
- **The open declaration with a strict decoder for a type only written.** A client decodes no type
  it only writes, so this differs from opening it in nothing a caller can observe.

T-22 and T-36 hold what each policy opens, and T-58 that no operation moves a declaration.

## A merge patch relaxes what its schema requires

A `PATCH` body sent as `application/merge-patch+json` carries only the members it changes (RFC 7396
§1), and documents commonly give it the schema of the resource it edits, required members and all.
Under `MergePatch` such a body's shape is a patch shape, every member optional, where its binding's
`PatchImplicitOptionality` is nil, and the shape's `Inferred` marks its `Patch`
([TypePlan](types.md#what-a-patch-relaxes)). A binding stating the field decides for itself, as
[precedence](../contract/runtime-policy.md#precedence--declared-ir-facts-win-hard-rule) has a
declared fact win: `true` relaxes whatever the media type, unmarked, and `false` relaxes nothing.
Turning the policy off leaves every body requiring what its schema requires. Where the operation
offers that content beside others its ranking ties it with, the policy elects it too, marked
`Inferred` ([one election](responses.md#one-election)), so the body an SDK sends is the one relaxed.
No other media type is read this way: a JSON Patch (`application/json-patch+json`) is a list of
operations, and a `PATCH` sent as `application/json` says nothing about the members it leaves out.
T-59 holds this section.

## An operation's effects come from its binding

`OpPlan.Effects` ([OpPlan](operations.md)) says whether calling an operation is safe and whether it
is idempotent. A declared `Operation.Idempotency` decides, its token kind counting as idempotent
since a retry sends the token it first sent. Where the IR states none, `ProtocolEffects` takes them
from what the operation's binding defines, marked `Inferred`. A GraphQL binding's `query` is safe
and idempotent, since GraphQL defines a query as a read-only fetch (GraphQL §2.3), and a `mutation`
or `subscription` is neither. An HTTP binding's method counts as RFC 9110 §9.2 defines it: `GET`,
`HEAD`, `OPTIONS` and `TRACE` are safe, and those with `PUT` and `DELETE` are idempotent, while
`POST` and `PATCH` state neither. `QUERY`, which OpenAPI 3.2 names, is safe and idempotent, as the
HTTP QUERY method's specification defines it. Any other method a document declares, as OpenAPI 3.2's
`additionalOperations` can, takes what the IANA HTTP Method Registry (RFC 9110 §16.1) records for
it, its name compared with case as HTTP compares a method (RFC 9110 §9.1); the plan package holds
the registry as data, so a change to it ships in a new build. A method the registry does not list,
and an operation whose bindings define neither, as a message or an OTP binding does, take neither,
and one several bindings carry takes only what every one of them states. An RPC system's own level,
such as gRPC's `idempotency_level`, reaches `Operation.Idempotency` ([the
matrix](../../ir-spec-matrix.md)), the declared fact above.

A method says nothing of whether an operation destroys something: `DELETE` and `PUT` are
idempotent and may be destructive, so nothing derives a confirmation from `Effects`. On the golden
spec no operation declares its idempotency, and of its 3,763 operations 1,841 use a safe method and
2,693 an idempotent one. Without the policy no operation would be known idempotent, so no request
whose outcome is unknown would ever be sent again
([retries](../contract/runtime-policy.md#the-canonical-vocabulary)).

## Envelopes

An envelope wraps an API's success bodies in members of its own, such as a failure flag, messages
and page information, beside the one member a caller wants. An SDK that returns the envelope makes
every call site dig. One that unwraps it has to know where the value and the failure signal live,
so a configuration names its envelopes, and the default names none.

```go
type Envelope struct {
    Name     string      // recorded in each Inferred mark this envelope sets
    Bases    []ir.TypeID // matches a body whose model is or composes one of these, at any distance
    Members  []string    // matches a body whose model declares every one of these wire names
    Value    []string    // the wire-name path to what the reader gets in the body's place;
                         // nil = the body itself
    Flag     []string    // the path to the member saying whether the call succeeded; nil = none
    Succeeds []ir.Value  // the Flag values meaning it succeeded; set exactly when Flag is
    Except   []ir.OpID   // operations whose bodies this envelope never matches
}
```

An envelope reads the bodies of `Return` and `Others`, the responses that succeed. A body matches
an `Envelope` when its type, read through `TypePlan.Alias`, is a model that is one of `Bases` or
composes one through `Base`, `Implements` or `Mixins` at any distance, as the models `Flattened`
reads do, or declares every name in `Members`, read through `Flattened`. Either way it must also
declare the members `Value` and `Flag` name, and where `Flag` is set a flag admitting a value
`Succeeds` lists. A body takes the first envelope that matches it, and an envelope never matches a
body of an operation its `Except` lists, so the next one may. An operation opts out through
`Except` rather than a hint, since the plan reads none ([plan or refine](plan-or-refine.md)).
[Responses](responses.md#envelopes) says how a matched body is read, a failure's among them. Each
field the match sets is marked `Inferred` as `envelope:` followed by the envelope's `Name`, apart
from every other heuristic's mark.

An envelope is reported as an error `emitters/plan/policy-invalid`, and matches nothing, when it has
no `Name`, a `Name` holding a comma, which an `Inferred` list cannot hold, or a `Name` another
envelope shares; when it has neither `Bases` nor `Members`, or neither `Value` nor `Flag`, which
would read nothing; or when it has only one of `Flag` and `Succeeds`. A body that only its flag,
admitting no value `Succeeds` lists, keeps from matching is passed over, since reading it would fail
every call, so the next envelope may match it, and an envelope never reads a body worse than no
envelope does. Such a body that no envelope then matches is read as with none and draws a warning
`emitters/plan/envelope-flag-mismatch`, once, so a configuration whose later entry reads it draws
none. An envelope no body matches is reported as a warning `emitters/plan/envelope-unused`, since
one policy may serve several documents. An `Except` or `Bases` entry naming nothing in the document
is passed over by `Build`, since the engine reported it before `Build` ran, as a warning
`emitters/unmatched-key` for the same reason ([a key that matches
nothing](../write-integrate.md#a-key-that-matches-nothing)).

`Succeeds` lists what success looks like, not failure, because a flag may fail in more than one way:
JSend's `status` fails as `fail` and as `error`, and a numeric `code` succeeds at `0` alone. A
flag's value is compared with them as JSON Schema compares instances, so `0.0` is `0`
([TypePlan](types.md#variants-that-can-share-a-value)). A nil `Value` serves an API that puts its
data beside the flag, such as `{ok, channel}`: the body is returned whole, and the flag is still
read.

A body is matched by its members as well as by its composition, because composition alone misses
real envelopes. On the golden spec (spec @5c37ac8c, morphic @fd8b0730), the primary bodies of 2,665
of 3,763 operations declare `success`, `errors`, `messages` and `result`. 2,238 of them compose a
named component declaring the `success` flag, 113 distinct components in all, since products
declare their own, and 9 more are one of those 113. The other 418 declare it themselves: 209 in 126
named types a `Bases` list would have to add one by one, and 209 in an anonymous body, which no
`Bases` list can name. One `Members` entry matches all 2,665, and since it names every member, a
body that merely has a `result` is never unwrapped by accident:

```go
plan.Envelope{
    Name:     "result",
    Members:  []string{"success", "errors", "messages", "result"},
    Value:    []string{"result"},
    Flag:     []string{"success"},
    Succeeds: []ir.Value{{Kind: ir.ValueBool, Bool: true}},
}
```

That entry is not every envelope there: 680 more primary bodies declare `success` beside fewer
members, 500 of them with a `result` and 171 without. Entries listed after it reach them, one with
`Members: []string{"success", "result"}` and the same `Value`, then one with
`Members: []string{"success"}` and a nil `Value`, each body taking the first that matches. One of
those declares `success` as a string whose example is `"true"`, which both pass over: an entry
listed after them whose `Succeeds` is the string `"true"` reads it, and without one it is read as
with no envelope, with a warning. The other 9 are variants of a union, which no envelope matches,
since only a model does.

## Framings

Framings state what a media type means, not a guess about an API, so the table marks nothing
`Inferred`: whether a content streams is the source's to declare or the compiler's to infer, and the
table says only how it divides. Its default holds the sequential media types OpenAPI 3.2 names:
`text/event-stream` is `sse`; `application/jsonl` and `application/x-ndjson` are `json_lines`;
`application/json-seq` and the `+json-seq` structured suffix, which `application/geo+json-seq`
carries, are `json_seq`; and `multipart/mixed` is `multipart`. An entry matches a media type
compared as [one election](responses.md#one-election) says, and one starting with `+` every media
type with that suffix, as the compiler's streaming policy reads one
([`streaming-media`](../../compilers/openapi.md#streaming-media)), an entry naming the whole media
type winning over a suffix. A configuration adds one for a media type its compiler was told streams.
A framing a target has no reader for is that target's coded degrade to `raw` ([body
shapes](responses.md#body-shapes)).

## A page walk's first page

A pager whose caller sets no page number sends none on its first request, as for any input left
unset ([values](../refine/values.md#a-default-is-documented-never-sent)), and counts its next from
the page that request gets, `PagePlan.FirstPage`
([pagination](../operations/pagination.md#what-a-pager-sends-next)). The page input's declared
default is it, unmarked, since a request sending none gets that page. A page input declaring none
says nothing of where its pages start, so `FirstPage` takes the policy's number, marked `Inferred`
in `OpPlan.Inferred` under `Page.FirstPage`, since `PagePlan.Inferred` says only that the walk as a
whole was inferred. A declared minimum is not read: it says what the API admits, and 4 of the golden
spec's `GET`s taking a `page` query parameter declare a default of 1 beside an inclusive minimum of
0 (spec @bc6fad24, morphic @fa253fe19), so reading the minimum would start them at a page the API
does not number first. With the policy holding a number every page walk whose input holds one has a
pager, so a default a document gains later changes the number sent and no method. With `FirstPage`
set to null the plan guesses none: an operation whose page input declares no default has no `Page`,
so every target lowers it as a plain method taking the number as any input, the mock answers it with
its example, and the plan reports it once as a warning `emitters/plan/first-page-unknown`. A pager
reading only the page its caller names was weighed and refused: a caller sending no number would
read one page of a walk it asked to read whole. T-45 holds this section.
