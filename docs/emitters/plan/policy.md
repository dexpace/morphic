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
    OpenUnions      bool               // default on: a union a client reads keeps a
                                       // variant it does not know (below)
    OpenEnums       bool               // default on: an enum a client reads keeps a value
                                       // it does not list (below)
    StrictExclusive bool               // default off: every one_of union is TypePlan.Strict
                                       // (../refine/unions.md)
    ProtocolEffects bool               // default on: an operation stating no effects takes
                                       // what its binding's protocol defines (below)
}

type PrimaryRule string                  // "lowest_2xx" | "unconditional" | "default"
```

A test asserts that disabling a heuristic removes the corresponding `Inferred` plan field — the
guard against a heuristic silently defaulting on and quietly making the plan non-neutral.

## The policy is data

Every field is a value from a closed vocabulary the plan package defines, and none is a function.
So a policy encodes, compares and hashes as what it says, and `Plan.Version` hashes it
([the plan](overview.md#the-plans-version)). A function has no bytes to hash: its printed form is
an address that changes every run, and skipping it gives two policies that pick different responses
one version. A heuristic the vocabulary lacks is added to it, in this package, so it ships in a new
build, which the plan cache keys on ([the plan](overview.md#the-plans-version)). A strategy
registered from outside by name and version was weighed and refused: a function may close over what
a run sets, so neither its name and version nor the build fixes what it does, and two runs that pick
differently would share a key. So was a caller-supplied ID hashed in the policy's place, which fails
the same way when the caller forgets to change it.

A configuration is read as a target's options are ([target options](../contract/options.md)):
strictly, and over the defaults of the version it states, which `DefaultPolicy(version)` returns,
so a default that changes is a new version and an existing configuration keeps planning what it did.
One that states no version is refused, and so is an absent one, as a target's are, since which
defaults apply must not be guessed. The comments above name the current version's defaults. The
zero `Policy` is not the default: it turns every switch off and lists nothing, so no rule picks a
primary response and every operation returns `void`. A `Policy` built in Go meets no decoder, so
`Build` reports a rule outside the vocabulary or listed twice as an error
`emitters/plan/policy-invalid`, and plans as if it were left out.

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

## A union a client reads is open

An API that adds a variant must not break every SDK already deployed on the first response carrying
it, so under `OpenUnions` a union or polymorphic base is `Open` wherever `Facts.Usage` marks it
`Output`, marked `Inferred`. A value no variant recognizes is then kept rather than refused
([unions](../refine/unions.md)). That covers a type read and written alike: a response can carry it
whatever else the type does, and a kept value a caller sends back goes out as it came. A type only
ever written stays closed, and no SDK decodes one. Turning the policy off closes every union. The IR
has no field saying a union is open or closed (#664); once it does, a declared answer wins, as
[precedence](../contract/runtime-policy.md#precedence--declared-ir-facts-win-hard-rule) has it. The
plan records the choice so that docs and a mock agree with the SDK ([docs and
mocks](../docs-and-mock.md)).

## An enum a client reads is open

The reason unions are open holds for an enum: an API that adds a member must not break every SDK
already deployed on the first response carrying it. So under `OpenEnums` a closed enum is `Open`
wherever `Facts.Usage` marks it `Output`, marked `Inferred`, and a value it does not list is kept
rather than refused ([enums](../refine/enums.md)). An enum the IR declares open, `Closed` false
through a promoted `x-extensible-enum` among others, is `Open` wherever it is used, unmarked since
the IR states it, so the SDK, the docs and a mock each read one field. A closed enum only ever
written stays closed, as does one declaring a `FallbackMember`, since the document already says what
a value it does not list decodes to, and turning the policy off leaves every enum as the IR says.
Admission and the rules proving variants disjoint read the declared `Closed`
([TypePlan](types.md#variants-that-can-share-a-value)), so opening an enum changes no union's shape:
inside a union, a value an enum variant does not list is not that variant's, and an open union keeps
it as its own unknown.

This does not override the declared `Enum.Closed`, which states that the members the document lists
are all it admits. The SDK honours that in what it sends: its constants name the listed members
alone. `Closed` says nothing of what a later version of the API will list, and that is what a client
reading a response has to survive. On the golden spec (spec @319dc756, morphic @b206dc99) all 8,314
enums are closed, since an OpenAPI `enum` is a closed value set, so without the policy each one a
response carries would fail to decode the first time the API added a member.

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
and an operation whose bindings define neither, as an RPC binding does, take neither, and one
several bindings carry takes only what every one of them states.

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
none. An `Except` or `Bases` entry naming nothing in the document, and an envelope no body matches,
are reported as a warning `emitters/plan/envelope-unused`, since one policy may serve several
documents.

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
