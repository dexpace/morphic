# Runtime and SDK policy

`emitters/policy` (INV10; architecture.md §2.4). The behavioral configuration of generated
SDKs is an emitter input alongside the IR, **never a field on `ir.Document`** — the divergence from
oagen, which attaches `SdkBehavior` to its spec root. Keeping the trees separate lets one IR drive
SDKs, docs, and mock servers without dragging SDK opinions along.

## The canonical vocabulary

From oagen's production `SdkBehavior`, the best real-world enumeration (architecture.md §2.4):

```go
package policy

type Policy struct {
    Retry       Retry        // retryable status codes, max attempts, Backoff{initial, multiplier, max, jitter}
    Timeout     Timeout      // default seconds + env override
    Errors      ErrorTax     // status→logical exception-kind and fault, doc-URL template, the bound
                             // on a buffered body (§8.5, §8.9)
    Telemetry   Telemetry    // request-ID header, client-telemetry header
    Logging     Logging      // enabled + a CLOSED lifecycle-event list
    UserAgent   UserAgent    // identifier template, app-info enrichment, ai-agent env vars
    Idempotency Idempotency  // header name, auto-generate rules
    Pagination  Pacing       // auto-page delay
    Guards      RequestGuard // option keys that must NOT appear as params (misuse detection)
}

func Defaults() Policy
func Merge(base Policy, over DeepPartial) Policy // scalars override; ARRAYS REPLACE (the oagen rule)
```

## Delivery

Canonical `Defaults()` + deep-partial per-emitters/per-project overrides. Arrays replace
entirely (not concat) — an explicit, documented rule. Policy is delivered in `EmitterInput.Policy`;
refiners read it via `Ctx.Policy`, and it shapes only the generated *runtime binding* (the retry
loop, transport config, user-agent string, telemetry headers, pagination pacing) behind the
`Transport`/`Serializer` abstraction. It never enters the typed structural model (§5.4).

## Precedence — declared IR facts win (hard rule)

`ErrorCase.Retryable/Throttling/Fault` and `Operation.Idempotency` come from the spec; policy fills
in only where the spec is silent — never the reverse. The refiner enforces this by reading the IR
fact first and consulting policy only on `nil`, or for a `Fault`, on `""`. A `policy-precedence`
test asserts a spec `Retryable:false` beats a policy that lists that status as retryable (§13 test
T-9). When policy is overridden by a spec fact, an `Inferred`-marked diagnostic is emitted so
nothing is silent.
