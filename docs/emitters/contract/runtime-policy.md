# Runtime/SDK policy — a separate input

`emitters/policy` (INV10; architecture.md §2.4). The behavioral configuration of generated
SDKs is an emitter input alongside the IR, **never a field on `ir.Document`** — the divergence from
oagen, which attaches `SdkBehavior` to its spec root. Keeping the trees separate lets one IR drive
SDKs, docs, and mock servers without dragging SDK opinions along.

## The canonical vocabulary

From oagen's production `SdkBehavior`, the best real-world enumeration (architecture.md §2.4),
widened by the behaviors every SDK has to choose and a caller has to be able to set:

```go
package policy

type Policy struct {
    Retry       Retry        // when and how a request is sent again
    Redirects   Redirects    // how a redirect is followed
    Timeout     Timeout      // how long one attempt may take, and a body's next bytes
    Errors      ErrorTax     // a status's kind and fault, and the bound on a buffered body
                             // (../operations/errors.md, ../operations/returns.md)
    Telemetry   Telemetry    // the headers naming a request and its client
    Logging     Logging      // the lifecycle events a client's hook is handed
    UserAgent   UserAgent    // the User-Agent header
    Idempotency Idempotency  // the idempotency key the runtime generates
    Pagination  Pacing       // the wait between an auto-pager's pages, and how many a walk fetches
    Polling     Polling      // the wait between a poller's polls, and how many a wait makes
    Guards      RequestGuard // option names refused as parameters
    Nesting     Nesting      // how deep a JSON value may nest
}

type Retry struct {
    Statuses      []int      // statuses saying the server did not act on the request, retried
                             // where the IR states nothing; default 408, 429, 503
    Uncertain     []int      // statuses leaving unknown whether it acted, retried as WhenUncertain
                             // says where the IR states nothing; default 500, 502, 504
    Throttling    []int      // statuses read as throttling where the IR states nothing;
                             // default 429
    MaxAttempts   int        // default 3, the first attempt included; 1 to 100
    Backoff       Backoff    // the wait before each attempt after the first;
                             // default {500ms, 2, 8s, 0.25}
    Throttled     Backoff    // the wait after a failure marked throttling
                             // (../operations/errors.md); default {1s, 2, 60s, 0.25}
    WhenUncertain RetryScope // which operations send again a request that got no response or an
                             // Uncertain status (below)
    RetryAfter    bool       // default true: wait what a Retry-After header asks, up to the Max
                             // of the backoff that applies
}

type Backoff struct {        // written whole: a layer names all four members or none
    Initial    Duration      // the wait before the second attempt; 1ms to Max
    Multiplier Ratio         // each later wait over the one before it; 1 to 10
    Max        Duration      // the longest wait, a Retry-After's included; Initial to 60m
    Jitter     Ratio         // the share of each wait drawn at random, a wait w falling in
                             // [w×(1−Jitter), w]; 0 to 1
}

type RetryScope string       // "never" | "idempotent" (default) | "always"

type Redirects struct {
    Max int                  // default 10; past it, or at 0, a 3xx is returned as the response;
                             // 0 to 100
}

type Timeout struct {
    Request Duration         // one attempt, the redirects it follows included, from its first
                             // send to the end of the last response's headers and of a body the
                             // runtime buffers; default 60s; 1ms to 60m
    Idle    Duration         // the wait for the next bytes of a body the runtime does not buffer,
                             // read or written (below); default 5m; 1ms to 60m
    Env     string           // an environment variable whose value, a Duration in Request's
                             // range, replaces Request when the client is built; "" (default)
                             // names none
}

type ErrorTax struct {
    Kinds   map[int]ErrorKind // a status's row, 300 to 599; a layer adds a row or replaces one,
                              // and removes none; default
                              // ../operations/errors.md#the-kinds-are-the-taxonomys
    MaxBody int64             // the bytes the runtime buffers of a failure's body, or of another
                              // response's (../operations/returns.md); default 1048576;
                              // 1 to 67108864
}

type ErrorKind struct {      // written whole
    Kind  string             // an upper-case ASCII letter, then ASCII letters and digits, 64
                             // bytes at most; never a fault's name (below)
    Fault string             // "client" | "server" | "" (default), which takes the status's class
}

type Telemetry struct {
    RequestID string         // the response header naming a request, which an event carries
                             // (below); default "X-Request-Id"; "" reads none
    Client    string         // a request header carrying what User-Agent does, for a platform
                             // that fixes User-Agent; "" (default) sends none
}

type Logging struct {
    Events []EventKind       // the events a client's hook is handed (below); default every
                             // kind; empty declares no hook
}

type EventKind string        // "attempt" | "response" | "wait" | "redirect" | "end"

type UserAgent struct {
    Template string          // the header's value; default "{sdk} ({target})", the root unit's
                             // name and the target's key; "" writes none; 256 bytes at most
    AppInfo  bool            // default true: the client takes its caller's application name
                             // and version, sent as one more product
    AgentEnv []string        // environment variables whose being set adds the product
                             // agent/<name>, and never a value; default none; 16 at most
}

type Idempotency struct {
    Header   string           // the header a generated key goes in where the IR names no
                              // parameter for it; default "Idempotency-Key"
    Generate IdempotencyScope // which calls get a key their caller gave none
}

type IdempotencyScope string // "never" | "declared" (default) | "unsafe"

type Pacing struct {
    AutoPageDelay Duration   // the wait before each page after the first; default 0ms;
                             // 0ms to 60m
    MaxPages      int        // the pages one walk fetches before it fails (below); default
                             // 10000; 1 to 1000000
}

type Polling struct {
    Backoff  Backoff         // the wait before each poll (below); default {1s, 2, 30s, 0.25}
    MaxPolls int             // the polls one wait for a result makes before it fails; default
                             // 1000; 1 to 100000
}

type RequestGuard struct {
    Keys []string            // names a parameter map may not hold, each a client option's and
                             // 1 to 64 bytes of visible ASCII; default none; 64 at most
}

type Nesting struct {
    MaxDepth int             // the objects and arrays, one inside another, of a JSON text the
                             // runtime reads or writes; default 64; 1 to 10000; 0 is outside
                             // the range, as any other value is; a target that cannot hold to
                             // a value reports it, and none reads to another depth (below)
}

type Duration int64          // milliseconds; in JSON digits and a unit, "ms", "s" or "m": "500ms"
type Ratio int               // thousandths; in JSON a number of at most three fraction digits

func Defaults(version int) Policy // the defaults of a configuration version (below)
func Merge(base Policy, over DeepPartial) Policy // a leaf written replaces the base's (below)
```

- **A request whose outcome is unknown** may have reached the server and acted: one that got no
  response, or a 500, 502 or 504, after which the server or one behind a gateway may have acted
  before failing (RFC 9110 §15.6). So by default it is sent again only where the operation is
  idempotent, as `OpPlan.Effects` says ([OpPlan](../plan/operations.md)). A 408, 429 or 503 says the
  server did not act (RFC 9110 §15.5.9 and §15.6.4, RFC 6585 §4), so `Statuses` apply whatever the
  operation does. An idempotency key the runtime generates (`Idempotency`) makes an operation
  idempotent only where the IR declares its token, since only then is the server known to honor one.
  A request whose body the runtime cannot replay, such as a stream the caller gave it, is never sent
  again, whatever came back: its failure is returned. The lists name HTTP statuses; a protocol with
  statuses of its own takes lists of its own, each status placed by whether it says the server did
  not act, as `Policy.Errors` gives such a protocol rows of its own
  ([failures](../operations/errors.md#the-kinds-are-the-taxonomys)). An entry below 300, which no
  failure has, or past 599, which no status has (RFC 9110 §15), is invalid, and so is one the other
  list names for the same layer and scope, which would say at once that the server did not act and
  that whether it acted is unknown. Where the two lists resolve from different layers, or from an
  operation's setting and its layer's document-wide one ([layers](#layers-and-operations)), a
  status both name stays only in the list set higher, so a configuration moves a status between
  them by writing the list it joins alone.
- **`Retry-After`** is the server saying when to come back. Honoring it is the default, and it never
  waits past the `Max` of the backoff that applies, `Throttled`'s after a throttling failure and
  `Backoff`'s after any other, so a server cannot park a caller indefinitely; a value past it ends
  the retries and returns the response. A value shorter than the wait the backoff drew waits that,
  so the header only lengthens what a client would wait without it and a `Retry-After: 0` changes
  nothing. The floor is the wait drawn and not the schedule's: held to the schedule's, every client
  a server answered `Retry-After: 0` would come back at one instant, which `Jitter` is there to
  prevent. A value that is neither a count of seconds nor an HTTP date (RFC 9110 §10.2.3) is read as
  absent.
- **The wait before attempt *n*+1** is the *n*th wait of the schedule of the backoff that applies to
  how attempt *n* failed, a backoff's schedule being `Initial` and then each wait the one before it
  times `Multiplier`, capped at `Max`. The step is the attempt's number and never a count of one
  backoff's own failures, so under the defaults and a `MaxAttempts` of 5 a call failing 503, 503,
  429, 429 waits 500ms, 1s, 4s and 8s, less what `Jitter` draws, rather than start `Throttled` at
  its `Initial` after the third failure. `Jitter` then draws the wait, evenly over the whole
  milliseconds of its range.
- **A throttling failure** is one whose error case the IR marks `Throttling`, and where the IR
  states nothing, one whose status `Throttling` lists. Without the list `Throttled` would select
  nothing on a document that cannot state the fact: of the golden spec's operations 175 declare a
  429 and no error case states `Throttling` (`grep -c '"throttling"'` over its IR finds none; spec
  @bc6fad24, morphic @fa253fe19).
- **What a call can cost** follows from the members, each of which a server's responses push
  against: at most `MaxAttempts` attempts, each following at most `Redirects.Max` redirects and
  bounded with them by `Timeout.Request`, with no wait between attempts past the longer `Max`, a
  failure's body read no further than `Errors.MaxBody`, and a body handed over unread waiting no
  longer than `Timeout.Idle` for its next bytes. A walk fetches at most `Pagination.MaxPages` pages
  and a poller polls at most `Polling.MaxPolls` times, each a call so bounded. Every one has an
  upper bound of its own, so no configuration removes a limit by writing a large one.
- **A redirect** is followed where following it keeps what the request asks, as RFC 9110 §15.4
  allows: 307 and 308 send the method and body again, 303 sends a `GET`, or a `HEAD` for a `HEAD`,
  and 301 and 302 send the request again as it was. A 301 or 302 to a `POST` is returned as the
  response instead: the RFC lets a client turn the `POST` into a `GET`, which drops what it asked
  for, and a server may have acted on it before redirecting. So is a redirect that would send again
  a body the runtime cannot replay, one from `https` to `http`, which would send the request in the
  clear, and a 3xx the operation declares, which the document says its caller gets. A redirect to
  another origin never carries the client's credentials, which no setting changes, since a server
  cannot vouch for one it is not.

What a response may hold that the document does not list is not runtime policy, because it changes a
type or a decoder rather than the boilerplate around them, and docs and mocks have to agree with it.
A union keeps an unknown variant and an enum an unknown value under `plan.Policy`, and a `oneOf` two
variants admit decodes as `plan.Policy` says ([plan policy](../plan/policy.md)). Unknown members
follow each model's `Additional`, a declared fact, so no setting widens a model the spec closes or
closes one it leaves open. An enum and a union differ because an OpenAPI `enum`, `oneOf` or `anyOf`
can only be closed, so their `Closed` records no choice, where `additionalProperties: false` is one
its author writes ([unions](../plan/policy.md#a-union-keeps-a-variant-it-does-not-know),
[enums](../plan/policy.md#an-enum-keeps-a-value-it-does-not-list)).

## Delivery

Canonical `Defaults(version)` + deep-partial overrides ([what a layer
writes](#what-a-layer-writes)). Arrays replace entirely (not concat) — an explicit, documented rule
— so each retry list resolves on its own, and only then does `Resolve` keep a status both name in
the one set higher ([retries](#the-canonical-vocabulary)). Placing it at each merge instead would
lose one a layer moves into a list a higher layer then replaces, leaving it in neither. A layer a
configuration writes is read as a target's options are ([target options](options.md)): strictly, as
JSON naming each field in lower camel case (`retry.maxAttempts`), and over the defaults of the
version the project's configuration states, so a default that changes is a new version and an
existing configuration keeps generating what it did. Policy is delivered resolved, per operation, in
`EmitterInput.Policy`; refiners read it via `Ctx.Policy`, and it shapes only the generated *runtime
binding* (the retry loop, transport config, user-agent string, telemetry headers, pagination pacing)
behind the `Transport`/`Serializer` abstraction. It never enters the typed structural model
([runtime abstraction](../emit/templates.md#serialization-behind-a-runtime-abstraction)). The
methods hold no policy and a template holds no branch ([structure and
boilerplate](../emit/templates.md)), so what differs by operation is a table in the runtime binding,
a row per operation, which `BuildOperationSettings` fills ([steps](../refine/steps.md)): the
operation's resolved settings, whether it retries an uncertain outcome, decided there from
`WhenUncertain` and `OpPlan.Effects`, and the statuses it declares, none of which is followed as a
redirect. A template renders the rows, each beside the methods that read it, so the runtime's own
unit names no operation ([output units](../refine/output-units.md#the-runtimes-unit)), and a method
names only its own.

## What a layer writes

A layer's override is `Policy`'s JSON form with any member left out, each member named in lower
camel case and a `Kinds` row by its status, written as its three digits and no other way
(`errors.kinds.404`, never `0404` or `+404`, which name no member the record declares):

```go
type DeepPartial struct {
    Set    []string          // the leaves written, as paths ("retry.maxAttempts"), sorted
    Values Policy            // their values; a leaf Set does not name holds nothing
}
```

A **leaf** is a member holding a scalar or a list, a `Backoff`, or one row of `Kinds`. A layer
writes a leaf whole and `Merge` replaces it whole, so no value is assembled from two layers: a
`Backoff` whose `Initial` one layer wrote and whose `Max` another did could put the first wait past
the longest, with neither layer wrong. `Set` is what tells a leaf written at its zero from one left
out, `"statuses": []` from no `statuses`, which a struct of plain members cannot.

Each leaf's comment above gives its type, its default and its range, and a value is invalid
([layers](#layers-and-operations)) where it is any of these. A reader written from the record, over
`encoding/json/jsontext` in Go 1.27.3, refused each for every leaf it applies to, and T-9 holds it:

- of another JSON type than the leaf's, `null` and a number in quotes among them: no leaf reads
  `null` as its default, since a layer leaves a member out to say that;
- outside the range its comment gives, or a string outside its grammar: a header's name is an RFC
  9110 token and an environment variable's a letter or `_` followed by letters, digits and `_`, so
  neither holds a space, a line break or a `:`;
- a list holding an entry twice, or more entries than its comment allows, and a status below 300 or
  past 599;
- a `Backoff` or a `Kinds` row naming a member it does not have or leaving out one it must name,
  `fault` alone being optional, and a `Backoff` whose `Initial` is past its `Max`;
- a member the record does not declare, and a member named twice in one object.

**A duration states its unit.** `"30s"`, `"500ms"` and `"10m"` are durations; `30` is none, and
neither is `"30"`, `"1.5s"` or `"1h"`. One unit throughout, a count of milliseconds, was weighed and
reads a wrong value as a right one: the keys a document embeds are other generators' spellings
([embedded policy](embedded-policy.md#the-mapping-is-a-configuration)), some counting seconds and
some milliseconds, so a timeout of `30` meant as seconds would decode, pass every range check and
give each request 30 ms. Read as it is here, the bare number is a value of the wrong shape, which
the embedded reader reports and never coerces. A `Ratio` is a JSON number written with at most three
fraction digits, no sign and no exponent, so `-0` is none, read from its text and held in
thousandths, so a backoff's schedule is integer arithmetic and the same on every target: each wait
is the one before it times `Multiplier`, rounded down to a millisecond and capped at `Max`.

**A zero is a value only where the comment says so.** `Redirects.Max` 0 follows none,
`AutoPageDelay` `"0ms"` waits none, `Jitter` 0 draws none, and an empty list or an empty `Env`,
`RequestID`, `Client` or `Template` names none. Everywhere else zero is out of range: a
`MaxAttempts` of 0 sends nothing, a `Timeout.Request` or a `Backoff.Initial` of `"0ms"` bounds
nothing, and a `MaxBody` of 0 reads no failure's body. No member spells "no limit".

**The client's members and an operation's.** An operation's entry ([layers](#layers-and-operations))
may write `Retry`, `Redirects`, `Timeout.Request`, `Timeout.Idle`, `Errors.MaxBody`, `Idempotency`,
`Pagination` and, where it starts a long-running operation, `Polling`. `Errors.Kinds`, `Telemetry`,
`Logging`, `UserAgent`, `Timeout.Env` and `Guards` are what a client declares or sends alike on
every call, the kinds being declarations of the runtime's unit, which names no operation ([output
units](../refine/output-units.md#the-runtimes-unit)), so an operation's entry writing one is
invalid.

**A kind is one name whatever its letter case.** The runtime declares a name per kind, a class in
Kotlin, whose file a file system that ignores letter case keeps one of ([Kotlin
naming](../targets/kotlin/naming.md#reserved-names)), so `NotFound` and `Notfound` cannot both be
kinds. Kinds are therefore compared with letter case folded, as header names are. A row whose kind
the defaults or a layer below spell another way names that kind and takes their spelling, reported
as an info `emitters/policy/kind-respelled` naming both, so no layer changes a kind's letter case
alone. Two rows of one layer spelling a kind two ways that nothing below spells are both invalid,
since which was meant is no fact the layer states. The resolved table then holds one spelling of
each kind, whatever order a layer's rows are read in. Refusing the later spelling instead was
weighed: the status it leaves falls back to the row below, whose kind can differ in letter case from
one the same layer validly wrote, so the table would need resolving again. `Client` and `Server`, in
any letter case, name the two faults on every target and are no kind's name. A kind whose
declaration would take a name a target's runtime already declares, as `Api` would take Kotlin's
`ApiException`, is that target's to report ([a setting that cannot
apply](#a-setting-that-cannot-apply)).

**What each member does** where no other document says:

- `Timeout.Env` and `UserAgent.AgentEnv` name variables the client reads once, when it is built.
  `Env`'s value replaces the document's `Request` and no operation's own. A value that is no
  duration in range refuses the client, naming the variable, rather than run with a timeout nobody
  chose. Of an `AgentEnv` variable only whether it is set is read: its value is never sent, since an
  environment holds credentials.
- `UserAgent.Template` is printable ASCII in which `{sdk}` and `{target}` are the placeholders, each
  written any number of times or not at all, so `acme/1.4` is a template. Any other `{` or `}` is
  invalid, there being no escape for one, and a placeholder for the SDK's version waits for the
  options that name a version. The caller's application name and version are each an RFC 9110 token,
  refused when the client is built otherwise, so neither can add a header.
- `Idempotency.Generate` `declared` gives a call a key, where its operation's `Idempotency` names a
  token parameter and the caller gave none, in that parameter; `unsafe` also sends one in `Header`
  on each operation whose `Effects` are neither safe nor idempotent, which makes none of them
  idempotent ([above](#the-canonical-vocabulary)); `never` generates none. A key is a version 4 UUID
  (RFC 9562), drawn once for a call and sent on each of its attempts.
- `Guards.Keys` is read by a target whose request is a map its caller fills, where an option written
  among the parameters would be sent to the server, and compared there with a parameter's name as
  written. Each first target declares its request types, so none has a place for the mistake: one
  given a key reports it as a setting it cannot apply ([below](#a-setting-that-cannot-apply)),
  rather than read none in silence.
- `Timeout.Idle` bounds what `Request` leaves: a body the runtime hands its caller unread, a
  stream's frames or a file's bytes ([streaming](../operations/streaming.md)), and a stream the
  caller writes. A read that gets no byte for `Idle`, or a write of which the transport takes none,
  fails with a timeout. The count starts again with each byte moved, not each frame, so a server's
  keep-alive, the comment line of an event stream among them, holds a quiet stream open and a peer
  gone silent ends it. While its caller writes a stream `Request` does not run: it counts from the
  stream's close to the response's headers. Each target times both itself and sets no timeout of its
  HTTP library, whose own would end a stream with its request and whose messages quote the request
  ([Go](../targets/go/operations.md#streaming), [Kotlin](../targets/kotlin/operations.md#timeouts)).
  No document says how long its streams stay silent, so the default is a choice, and 60m is the
  longest silence a configuration admits.
- `Pagination.MaxPages` is the pages one walk fetches. The fetch after the last is refused unsent
  and the walk fails naming the member; it never ends as a walk that reached the end does, since a
  caller told so would act on part of a collection as the whole. It bounds a server whose
  continuations never end, which the walk's own rules cannot always tell from one with more to give
  ([pagination](../operations/pagination.md)).
- `Polling` paces a poller ([long-running operations](../operations/long-running.md)), and the entry
  of the operation that starts it sets it. `Backoff` is read as `Retry.Backoff` is, its first wait
  preceding the first poll and its *n*th the *n*th. Under `Retry.RetryAfter`, a poll's response
  stating `Retry-After` waits that where it is longer than the wait drawn, and `Max` where it is
  past `Max`: polling early costs a request and no result. `MaxPolls` is the polls one wait for the
  result makes; past it the wait fails naming the member, and the poller is still its caller's to
  poll. Each poll is a call, with attempts of its own under the polled operation's settings. The
  golden spec states no long-running operation (`grep -c '"longRunning"'` over its IR finds none;
  spec @bc6fad24, morphic @fa253fe19), so these defaults rest on no measurement of it.

A documentation link per failure, which the vocabulary this began from carries, is left out: no rule
reads one, and a failure's page is the docs emitter's to link ([docs and
mocks](../docs-and-mock.md)).

## What an event holds

The hook a client takes is handed one value per step of a call, of the kinds `Logging.Events` lists.
A hook is a client setting ([the client](../operations/client.md)): Go's
`WithHook(func(context.Context, runtime.Event))`, handed the context its caller gave the method, and
the Kotlin builder's `hook { event -> … }`, called in the call's coroutine. So a tracer finds there
the span the call belongs under, and `Call` pairs a call's events where calls overlap: measured with
Go 1.27.3, a hook keeping a span per `Call` parented each of two calls' spans under the value its
own context held. The runtime calls the hook where the step happens and waits for it. A hook changes
nothing of the request, so a header a tracer propagates is a `Transport` wrapper's to write
([auth](../operations/auth.md#where-a-credential-can-be-read)). What a hook is handed is the same on
each target:

```go
type Event struct {          // the runtime's: one step of one call
    Kind      EventKind
    Call      int64          // counts the client's calls from 1; one call's events share it
    Operation string         // the method called, as the SDK names it: "Users.GetUser"
    Attempt   int            // 1 for the first send
    Method    string         // the request's method
    URL       string         // the request's URL as shown (below)
    Status    int            // the response's status; 0 where none has arrived
    Wait      time.Duration  // "wait": how long until the next attempt
    Elapsed   time.Duration  // since the call began
    RequestID string         // what the Telemetry.RequestID header held (below); "" = nothing
    Failure   FailureClass   // "end": how the call failed; "" = it returned
    Cause     string         // "end": the type of what failed beneath the runtime's failure, as
                             // the target names a type; "" = nothing did
}

type FailureClass string     // "status" | "transport" | "timeout" | "canceled" | "decode" |
                             // "refused"
```

`attempt` precedes each send, `response` follows a response's status and headers, `wait` precedes
the wait before another attempt, `redirect` precedes a redirect's following and carries where it
leads, and `end` reports what the call returned. A page of a pager, a poll of a poller and the
opening of a stream are each a call; a stream's elements draw no event. An `end` names the class of
its cause and never its text: `Cause` is `*net.OpError` or `java.net.ConnectException` under a
`transport` failure and the decoder's error type under a `decode`, so a metric tells a refused
connection from a reset one, and a type's name is the program's text, never the request's.

**An event holds no value it would have to hide.** It has no member for a header, a body, a request,
a response or an error value, so nothing printing, encoding or copying one can show a credential:
there is none in it. The alternative, an event holding the request with each credential in a type
that prints as the marker, was prototyped and leaks. In Go 1.27.3 such a type's `String` and
`GoString` are not asked under `%d`, nor under any verb once the value sits in an unexported field
or behind a pointer to its holder, where `fmt` and `log/slog`'s text handler read the field itself;
in Kotlin 2.4.0 a data class's `toString` showed the marker while its `copy`, its `componentN` and a
reader of its fields each handed on the credential. An event of plain members leaked in no form
either language has. And a list of headers to hide fails the header it does not name: a caller's
`Authenticator` may write any ([auth](../operations/auth.md#where-a-credential-can-be-read)).

**The URL as shown** is text the runtime builds, never a URL it sends:

- for a request the operation's bindings built, the scheme, host and port it goes to, the path, and
  the operation's declared query parameters, with what a redacted parameter expanded to, in the path
  as in the query, as `[REDACTED]` ([redaction](../refine/redaction.md#a-redacted-value)). It is
  built before the auth step runs ([auth](../operations/auth.md#where-a-credential-can-be-read)), so
  a key an injector or an `Authenticator` writes into the query is not in it, and neither is the
  user and password, the query or the fragment of a base URL the caller gave, which may hold a
  proxy's or a signature's;
- for a URL a response supplied, a redirect's `Location`, a link to the next page or a place to
  poll, the scheme, host and port, then `/[REDACTED]`. A server signs such a URL in its query, as a
  link to stored bytes is, or in its path, and nothing the document declares says which part is the
  signature.

`RequestID` is text a server wrote, so it is kept only where it is at most 128 bytes of visible
ASCII, and is empty otherwise: a log line takes no line break and no megabyte from a response. T-65
holds an event to all of this.

## Layers and operations

An API's operations rarely want one setting each: an export wants a longer timeout, a `POST` no
retry, a rate-limited endpoint its own backoff. So every source of policy is a layer holding a
document-wide override and overrides keyed by operation, merged by the same `Merge` rules:

```go
type Layer struct {
    Document DeepPartial                // what the layer sets for every operation
    Ops      map[ir.OpID]DeepPartial    // what it sets for one operation, over its Document
}

type Resolved struct {
    Document Policy                     // what an operation no layer names is given
    Ops      map[ir.OpID]Policy         // each operation some layer names, resolved whole
}

// Resolve merges the layers, lowest first, over Defaults(version), and then keeps a status
// both retry lists name only in the one set higher (above).
func Resolve(doc *ir.Document, version int,
    embedded, project, emitter Layer) (Resolved, []ir.Diagnostic)
```

The layers, lowest first, are the policy a document embeds ([embedded policy](embedded-policy.md)),
the project's, and the one the project sets for one emitter alone. A setting takes the value of the
highest layer that sets it, for the operation or for the whole document, and within that layer the
operation's value over the document's. So a project that turns retries off turns them off
everywhere, whatever a document embeds for one operation, and a document's own per-operation setting
still beats its own document-wide one. Ordering by scope first would let a document's per-operation
settings outvote a project-wide choice, which then could not be made without naming every operation.

A key naming no operation of the document configures nothing, and is never ignored, since an
operation whose ID changed would otherwise lose its settings in silence: the engine reports it
before `Resolve` runs, as a warning `emitters/unmatched-key` naming the operation that now holds its
correlation key where one does ([a key that matches
nothing](../write-integrate.md#a-key-that-matches-nothing)), a warning rather than an error since
one project's policy may serve several documents. Where a higher layer replaces what the document
embeds, or its retry list takes a status from the other list the document embeds (above), an info
`emitters/policy/embedded-overridden` names the setting, both values and the operation, so the
document's author can see it was overruled. A value a configuration writes outside a field's
vocabulary or range, such as a `RetryScope` other than the three its comment names, a `MaxAttempts`
below 1, or a retry list naming a 2xx or a status its layer's other list names at that scope, is an
error `emitters/policy/invalid`, and resolves as if its layer had not written it there: an
operation's gives way to that layer's document-wide value where it has one, and to the layers below
where it has none. One a document embeds is the reader's warning instead, as a value of the wrong
shape there is ([embedded policy](embedded-policy.md#what-a-key-configures)), since its author meant
something the field does not hold. The engine resolves the layers once for each emitter, since each
brings its own top layer, and reports a diagnostic the embedded and project layers alone draw once
for the run, as the plan's are reported.

## How deep a value nests

`Nesting.MaxDepth` is the depth to which every target reads a JSON text, a response's body, an
error's and a stream's frame, and writes a request's
([models](../refine/models.md#how-deep-a-value-nests)). Its default is what each first target was
shown to decode, encode, print, hash and compare on a small stack of its platform: a goroutine's,
which grows, 256 KB on the JVM, and on Kotlin/Native the 512 KB an Apple platform gives a thread
([Go](../targets/go/models.md#nesting), [Kotlin](../targets/kotlin/models.md#nesting)). The range
ends at 10,000, which json/v2 fixes. An operation's value bounds its own request and responses, and
what belongs to no operation, a value printed, hashed or compared, is bounded by the largest value
any operation resolves to.

A target reports a value it cannot hold to as [a setting that cannot
apply](#a-setting-that-cannot-apply) and generates no reader for another depth in its place. Go and
Kotlin/JVM hold to every value of the range. Kotlin Multiplatform holds to none above 64:
Kotlin/Native ends the process on a stack overflow, with nothing to catch, so a depth is honored
there only where a stack was shown to take it. T-63 holds it.

## Precedence — declared IR facts win (hard rule)

`ErrorCase.Retryable/Throttling/Fault` and `Operation.Idempotency` come from the spec; policy fills
in only where the spec is silent — never the reverse, and at every layer. The refiner enforces
this by reading the IR fact first and consulting policy only on `nil`, or for a `Fault`, on `""`. A
`policy-precedence` test asserts a spec `Retryable:false` beats a policy that lists that status as
retryable, at a document-wide setting and at an operation's ([T-9](../testing/mandatory-tests.md)).
When policy is overridden by a spec fact, an `Inferred`-marked diagnostic is emitted so nothing is
silent.

## A setting that cannot apply

A setting the plan or a target cannot honor is reported, never dropped and never quietly replaced by
a stricter behavior: a `Redirects.Max` a target's transport cannot bound, a per-operation `Timeout`
on a runtime that keeps one per client, a `Timeout.Env` or a `UserAgent.AgentEnv` on a platform with
no environment, as a browser is, a `UserAgent.Template` where the platform fixes the header, a
`Guards.Keys` naming a key where the target declares its request types, or a kind whose name the
target's runtime declares already. The report is a coded diagnostic of the target's, naming the
setting and the operation, and the engine decides whether it is fatal (INV5).
