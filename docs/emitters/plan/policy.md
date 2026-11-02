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
    Envelopes       []Envelope         // default none: what unwraps a success body (below)
    Framings        map[string]Framing // media type or +suffix → how it frames a stream
                                       // (streams.md)
    FrameMembers    bool               // default on: an SSE schema naming only frame
                                       // fields describes the frame (streams.md)
    OpenUnions      bool               // default on: a union a client reads keeps a
                                       // variant it does not know (below)
    OpenEnums       bool               // default on: an enum a client reads keeps a value
                                       // it does not list (below)
    ExclusiveMatch  string             // "first" (default) | "only": what a oneOf value two
                                       // variants admit decodes to (../refine/unions.md)
    MethodSemantics bool               // default on: an operation stating no effects takes
                                       // its method's (below)
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
one version. A heuristic the vocabulary lacks is added to it, in this package and at a new
`plan.Revision`, so a change of behaviour always changes the version. A strategy registered by name
and version from outside was weighed and refused: changing its code without its version would be a
silent cache hit, which is the failure this rule removes. So was a caller-supplied ID hashed in the
policy's place, which fails the same way when the caller forgets it.

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
already deployed on the first response carrying it. So under `OpenEnums` an enum is `Open` wherever
`Facts.Usage` marks it `Output`, marked `Inferred`, and a value it does not list is kept rather
than refused ([enums](../refine/enums.md)). An enum only ever written stays as the IR says, and
turning the policy off leaves every enum as the IR says.

This does not override the declared `Enum.Closed`, which states that the members the document
lists are all it admits. The SDK honours that in what it sends: its constants name the listed
members alone. `Closed` says nothing of what a later version of the API will list, and that is what
a client reading a response has to survive. A document declaring an enum open, through a promoted
`x-extensible-enum` among others, makes it open in both directions. On the golden spec (spec
@319dc756, morphic @b206dc99) all 8,314 enums are closed, since an OpenAPI `enum` is a closed value
set, so without the policy each one a response carries would fail to decode the first time the API
added a member.

## An operation's effects come from its method

`OpPlan.Effects` ([OpPlan](operations.md)) says whether calling an operation is safe and whether it
is idempotent. A declared `Operation.Idempotency` decides, its token kind counting as idempotent
since a retry sends the token it first sent. Where the IR states none, `MethodSemantics` takes them
from the binding's method as RFC 9110 §9.2 defines them, marked `Inferred`: `GET`, `HEAD`,
`OPTIONS` and `TRACE` are safe, and those with `PUT` and `DELETE` are idempotent. `POST` and
`PATCH` state neither, and an operation no HTTP binding carries takes neither.

A method says nothing of whether an operation destroys something: `DELETE` and `PUT` are
idempotent and may be destructive, so nothing derives a confirmation from `Effects`. On the golden
spec no operation declares its idempotency, and of its 3,763 operations 1,841 use a safe method and
2,693 an idempotent one. Without the policy no operation would be known idempotent, so no request
that got no response would ever be sent again.

## Envelopes

An envelope wraps an API's success bodies in members of its own, such as a failure flag, messages
and page information, beside the one member a caller wants. An SDK that returns the envelope makes
every call site dig. One that unwraps it has to know where the value and the failure signal live,
so a configuration names its envelopes, and the default names none.

```go
type Envelope struct {
    Name    string      // recorded in each Inferred mark this envelope sets
    Bases   []ir.TypeID // matches a body whose model reaches one of these through Base or Mixins
    Members []string    // matches a body whose model declares every one of these wire names
    Value   []string    // the wire-name path to what the reader gets in the body's place
    Flag    []string    // the path to the member saying whether the call succeeded; nil = none
    Fails   *ir.Value   // the Flag value meaning the call failed; set exactly when Flag is
    Except  []ir.OpID   // operations whose bodies this envelope never matches
}
```

A body matches an `Envelope` when its type, read through `TypePlan.Alias`, is a model that reaches
one of `Bases` through `Base` or `Mixins`, or declares every name in `Members`, read through
`Flattened`. Either way it must also declare the members `Value` and `Flag` name. A body takes the
first envelope it matches, and none of them on an operation its `Except` lists.
[Responses](responses.md#envelopes) says how a matched body is read. Each field the match sets is
marked `Inferred` with the envelope's `Name`. An `Except` or `Bases` entry naming nothing in the
document is reported, as is a `Flag` without `Fails`.

A body is matched by its members as well as by its composition, because composition alone misses
real envelopes. On the golden spec (spec @319dc756, morphic @b206dc99), the primary bodies of 2,665
of 3,763 operations declare `success`, `errors`, `messages` and `result`. 2,238 of them reach a
named component declaring the `success` flag through `Base` or `Mixins`, 113 distinct components in
all, since products declare their own; the other 427 declare it inline, and no `Bases` list reaches
them. One `Members` entry matches all 2,665, and since it names every member, a body that merely has
a `result` is never unwrapped by accident:

```go
plan.Envelope{
    Name:    "result",
    Members: []string{"success", "errors", "messages", "result"},
    Value:   []string{"result"},
    Flag:    []string{"success"},
    Fails:   &ir.Value{Kind: ir.ValueBool, Bool: false},
}
```

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
