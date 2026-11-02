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
    Retry       Retry        // when and how a request is sent again (below)
    Redirects   Redirects    // how a redirect is followed (below)
    Timeout     Timeout      // default seconds + env override
    Errors      ErrorTax     // status→logical exception-kind and fault, doc-URL template, the bound
                             // on a buffered body (../operations/errors.md, ../operations/returns.md)
    Telemetry   Telemetry    // request-ID header, client-telemetry header
    Logging     Logging      // enabled + a CLOSED lifecycle-event list
    UserAgent   UserAgent    // identifier template, app-info enrichment, ai-agent env vars
    Idempotency Idempotency  // header name, auto-generate rules
    Pagination  Pacing       // auto-page delay
    Guards      RequestGuard // option keys that must NOT appear as params (misuse detection)
}

type Retry struct {
    Statuses    []int        // statuses retried where the IR states nothing; default 408, 429,
                             // 500, 502, 503, 504
    MaxAttempts int          // default 3, the first attempt included
    Backoff     Backoff      // {Initial, Multiplier, Max, Jitter}
    Throttled   Backoff      // the backoff after a failure marked throttling
                             // (../operations/errors.md)
    Connection  string       // a request that got no response: "never" | "idempotent" (default) |
                             // "always"
    RetryAfter  bool         // default on: wait what a Retry-After header asks, up to Backoff.Max
}

type Redirects struct {
    Max int                  // default 10; 0 follows none, returning the 3xx as a response
}

func Defaults() Policy
func Merge(base Policy, over DeepPartial) Policy // scalars override; ARRAYS REPLACE (the oagen rule)
```

- **A request that got no response** may have reached the server and acted, so by default it is
  sent again only where the operation is idempotent, as `OpPlan.Effects` says
  ([OpPlan](../plan/operations.md)). A status answers that question, so `Statuses` apply whatever
  the operation does. An idempotency key the runtime generates (`Idempotency`) makes an operation
  idempotent only where the IR declares its token, since only then is the server known to honor
  one.
- **`Retry-After`** is the server saying when to come back. Honoring it is the default, and it never
  waits past `Backoff.Max`, so a server cannot park a caller indefinitely; a value past it ends the
  retries and returns the response.
- **A redirect** is followed as RFC 9110 §15.4 says: 307 and 308 send the method and body again,
  301, 302 and 303 send a `GET`. A redirect to another origin never carries the client's
  credentials, which no setting changes, since a server cannot vouch for one it is not.

What a response may hold that the document does not list is not runtime policy, because it changes a
type or a decoder rather than the boilerplate around them, and docs and mocks have to agree with it.
A union a client reads keeps an unknown variant and an enum an unknown value under `plan.Policy`,
and a `oneOf` two variants admit decodes as `plan.Policy` says ([plan policy](../plan/policy.md)).
Unknown members follow each model's `Additional`, a declared fact, so no setting widens a model the
spec closes or closes one it leaves open.

## Delivery

Canonical `Defaults()` + deep-partial overrides. Arrays replace entirely (not concat) — an
explicit, documented rule. Policy is delivered resolved, per operation, in `EmitterInput.Policy`;
refiners read it via `Ctx.Policy`, and it shapes only the generated *runtime binding* (the retry
loop, transport config, user-agent string, telemetry headers, pagination pacing) behind the
`Transport`/`Serializer` abstraction. It never enters the typed structural model ([runtime
abstraction](../emit/templates.md#serialization-behind-a-runtime-abstraction)).

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

func Resolve(layers ...Layer) (Resolved, []ir.Diagnostic) // lowest first: Defaults() is implied
```

The layers, lowest first, are the policy a document embeds ([embedded
policy](embedded-policy.md)), the project's, and the emitter's. A setting takes the value of the
highest layer that sets it, for the operation or for the whole document, and within that layer the
operation's value over the document's. So a project that turns retries off turns them off
everywhere, whatever a document embeds for one operation, and a document's own per-operation
setting still beats its own document-wide one. Ordering by scope first would let a document's
per-operation settings outvote a project-wide choice, which then could not be made without
naming every operation.

A key naming no operation of the document is reported, never ignored, since an operation whose ID
changed would otherwise lose its settings in silence. Where a higher layer replaces what the
document embeds, an info diagnostic names the setting, both values and the operation, so the
document's author can see it was overruled.

## Precedence — declared IR facts win (hard rule)

`ErrorCase.Retryable/Throttling/Fault` and `Operation.Idempotency` come from the spec; policy fills
in only where the spec is silent — never the reverse, and at every layer. The refiner enforces
this by reading the IR fact first and consulting policy only on `nil`, or for a `Fault`, on `""`. A
`policy-precedence` test asserts a spec `Retryable:false` beats a policy that lists that status as
retryable, at a document-wide setting and at an operation's ([T-9](../testing/mandatory-tests.md)).
When policy is overridden by a spec fact, an `Inferred`-marked diagnostic is emitted so nothing is
silent.

## A setting that cannot apply

A setting the plan or a target cannot honor is reported, never dropped and never quietly replaced
by a stricter behavior: a `Redirects.Max` a target's transport cannot bound, or a per-operation
`Timeout` on a runtime that keeps one per client. The report is a coded diagnostic of the target's,
naming the setting and the operation, and the engine decides whether it is fatal (INV5).
