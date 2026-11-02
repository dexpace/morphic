# Responses and errors

Part of the [IR design](./README.md). An operation's declared outcomes, and how a status is classed.

```go
type Response struct {
    Name       Naming             // for formats with named outputs; Hint elsewhere
    Conditions ResponseConditions // HTTP status codes/ranges; empty for RPC single-response
    Payload    *Payload           // nil = no body
    Headers    []Header           // response metadata fields
    Links      []OperationLink    // the requests the response leads to, in source order
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
    Links      []OperationLink    // as on Response
    Fault      string             // "" | "client" | "server" — protocol-neutral fault classification
                                  // (Smithy @error; OpenAPI 4XX/5XX is its HTTP lowering). Drives
                                  // exception hierarchies and default status synthesis
    Retryable  *bool              // Smithy @retryable; nil = unknown
    Throttling *bool              // retryable specifically due to throttling — distinct backoff class
                                  // (Smithy @retryable(throttling: true)); nil = unknown
    Docs       Docs
    Unmodeled  Unmodeled
    Inferred   Inferred           // the heuristics that read a fact out of Unmodeled
                                  // ([promotion](./unmodeled.md#promotion))
}

type OperationLink struct {      // how a response leads to another request
    Name       Naming
    Operation  OpID               // the operation it leads to
    Params     []LinkParam        // values for Operation's parameters, each once, in source order
    Body       *LinkValue         // the value of Operation's request body; nil = none
    Server     *ServerID          // the server Operation is called on; nil = its own
    Docs       Docs
    Provenance Provenance
    Unmodeled  Unmodeled
}

type LinkParam struct { Param ParamID; Value LinkValue } // Param is one of Operation's

type LinkValue struct {          // Kind says which one field is set
    Kind     LinkValueKind        // constant | request | response | url | method | status
    Constant *Value               // constant: the value the link states
    Request  *InputPath           // request: a parameter or the body of the request answered
    Response *PropPath            // response: a header or the body of this response; Root nil
}
```

## Links

A link says how a response leads to another request: which operation a caller can call next, and
where each value that request needs comes from, so a docs target can say "follow with `getOrder`,
passing the `id` this response returns" and an SDK can offer the follow call. It sits on the
`Response`, the `ErrorCase` or the `Default` whose exchange it continues, since its values are read
from that exchange, and an error response can lead somewhere as a success can.

**A value is resolved to what it reads, not kept as an expression.** A value read from the request
is an `InputPath`, the parameter of the operation answered or the request body and members within
it, and one read from the response a `PropPath` into its body, or `In: "header"` and the header's
ID. That is the IR's spelling of a value of an exchange wherever one is read, as pagination reads
its cursor ([interaction patterns](./interaction-patterns.md#pagination)), and it names members by
ID, so a renamed property renames nothing here (invariant 3). An `InputPath` with neither a
parameter nor a segment is the request body whole, and a `PropPath` with no segment the response
body whole. The two alternatives were rejected. Keeping OpenAPI's runtime expression as text would
have every consumer reimplement that grammar and resolve it against the types, which is a
compiler's work done once per emitter (invariant 1). A structured expression with a JSON pointer
would still name each member by its wire name, where an ID names the member itself.

**A link lowers whole or not at all.** A link whose target, parameters or values a compiler cannot
resolve to this shape is kept verbatim instead, so a consumer reading `Params` reads every value
the source states, and none that it guessed. `pass/validate` holds each `LinkParam.Param` to the
link's `Operation` and each request value to the operation answered
(`pass/param-binding-mismatch`), and `irverify` holds a value's field to its kind
(`ir/link-value-missing-payload`, `ir/link-value-stray-payload`, `ir/unknown-link-value-kind`).

**A link has no ID**, as a response has none: nothing in the IR refers to one, and its name
presents it. `LinkValue` is a kind beside the one field it selects, as `Value` is, rather than a
sealed sum: it is a value of the exchange, and the kinds that carry no payload (`url`, `method`,
`status`) would be empty structs.

**A reference from a member to a resource is not a link.** Smithy's `@references` trait and
Protobuf's `google.api.resource_reference` say a member holds a resource's identifier, which leads
to that resource's operations by its lifecycle rather than to one operation by name, so their home
is the resource the `resource-hierarchy` row plans, not this shape.

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
