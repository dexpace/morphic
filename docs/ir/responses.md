# Responses and errors

Part of the [IR design](./README.md). An operation's declared outcomes, and how a status is classed.

```go
type Response struct {
    Name       Naming             // for formats with named outputs; Hint elsewhere
    Conditions ResponseConditions // HTTP status codes/ranges; empty for RPC single-response
    Payload    *Payload           // nil = no body
    Headers    []Header           // response metadata fields
    StatusCodeProp *PropPath      // output member populated from the runtime HTTP status line
                                  // (Smithy @httpResponseCode, TypeSpec non-literal @statusCode);
                                  // the member is suppressed from the body
    Docs       Docs
    Unmodeled  Unmodeled
}

type Header struct {             // one entry of a Headers list; encodes flat
    Property                      // what the header is: name, type, presence, docs
    Style      string             // how the value is written into the field, in
                                  // HTTPParamBinding.Style's vocabulary, as declared; "" = simple
    Explode    *bool              // as declared; nil = the style's default
}

type ResponseConditions struct {
    StatusCodes []StatusRange     // {From,To}: 200–200, 400–499 ("4XX"), 0–0 = default/catch-all
}

type ErrorCase struct {          // a response too: Name/Conditions/Payload/Headers as on Response
    Name       Naming             // for formats with named errors; Hint elsewhere (the status key, neutralized)
    Conditions ResponseConditions
    Payload    *Payload           // nil = no body; its contents' types may be of any kind
    Headers    []Header           // error metadata fields (Retry-After, rate-limit family)
    Fault      string             // "" | "client" | "server" — protocol-neutral fault classification
                                  // (Smithy @error; OpenAPI 4XX/5XX is its HTTP lowering). Drives
                                  // exception hierarchies and default status synthesis
    Retryable  *bool              // Smithy @retryable; nil = unknown
    Throttling *bool              // retryable specifically due to throttling — distinct backoff class
                                  // (Smithy @retryable(throttling: true)); nil = unknown
    Docs       Docs
    Unmodeled  Unmodeled
}
```

## Design notes

- **All responses and all content types survive** (oagen's "primary response" collapse is the
  counterexample). The plan layer picks a primary for SDK ergonomics; the IR doesn't.
- **An error's body is of any kind**, a model or a union, a list or a primitive, and
  `irfacts.Index.Usage` marks what it reaches `UsageError` ([derived-facts](./derived-facts.md)),
  with status conditions and range collapsing exactly as Kiota's error mappings (`404`, `4XX`,
  catch-all).
- **A status's class is the protocol's; a catch-all's is no one's.** An HTTP status code states its
  own class (RFC 9110 §15): a status below 400 lowers to a `Response`, and one from 400 to an
  `ErrorCase` whose `Fault` is `client` for 4xx and `server` for 5xx, since that is what the code
  means rather than a reading of the API. OpenAPI's `default` states none: it answers every status
  the map does not name, successes and failures alike, and is an operation's only description of
  success where the map names no success. So it lowers to `Default`, never to either list, and which
  it answers — the success response where the operation declares none, the error fallback beside a
  success, or both — is the plan's to decide under its policy ([plan
  policy](../emitters/plan/policy.md)), where every other choice of a primary response is made
  (invariants 2 and 6). A format whose catch-all is declared an error, as TypeSpec's `@error` model
  with no status code is, lowers it to an `ErrorCase` with the catch-all condition instead.
  `pass/validate` holds `Default.Conditions` to the catch-all alone
  (`ir/default-response-conditions`), and a `OneWay` operation to having no `Default` either.
- **A header is a `Property` with how it is written.** A response header has no binding to state
  its serialization, as a parameter's `HTTPParamBinding` does, so `Header` holds it. Two fields on
  `Property` were the alternative, rejected because they would mean nothing on a model's or a
  resource's property, and a check over a hand-named list of positions would have to keep them off
  those. `Property.Args` takes that route because a field argument can sit at any depth of a model;
  a header is a list of its own, so the type states the rule and no check has to. `Header` embeds
  `Property`, so the two encode flat as one object and a header reads as a property does. Each of
  its fields holds what the source declares, so an emitter can tell a declared `explode: false`
  from none. OpenAPI allows a header the simple style alone and lets `explode` join an object value
  two ways, as TypeSpec's `@header` option does; Swagger 2.0's response-header `collectionFormat`
  also writes a space- or pipe-delimited list, which is why `Style` is a field and not a constant.
