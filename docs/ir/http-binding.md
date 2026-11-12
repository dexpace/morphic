# HTTP binding

Part of the [IR design](./README.md). An operation's HTTP mapping: its route, where each parameter
travels and how it is written.

```go
type HTTPBinding struct {
    Method      string             // as sent on the wire (OpenAPI 3.2 additionalOperations keys
                                   // carry exact capitalization; QUERY and custom methods are legal)
    URITemplate string             // RFC 6570 — the one true path representation (Kiota + TypeSpec agree);
                                   // empty on a webhook binding, which has no path
    HostPrefix  string             // endpoint host prefix, may contain {param} labels (Smithy @endpoint)
    SharedRoute bool               // multiple operations legally share method+path, disambiguated by
                                   // request content (TypeSpec @sharedRoute) — validate must not
                                   // reject the duplicate, and single-route emitters must merge
    ParamBindings []HTTPParamBinding
    RequestContentTypes  []string  // priority-ordered
    ResponseBodyPath *PropPath     // HTTP response body = this sub-field of the response type
                                   // (gRPC transcoding response_body); nil = the whole payload
    SuccessStatus map[int]int      // response index → primary status (denormalized convenience; conditions are the truth)
    Compression *RequestCompression // client MUST compress the request body (Smithy @requestCompression)
    ChecksumRequired bool          // client MUST send a payload checksum (Smithy @httpChecksumRequired)
    PatchImplicitOptionality *bool // nil = protocol default (PATCH projections make properties
                                   // optional); false = disabled (TypeSpec @patch implicitOptionality)
    Inbound     HTTPInbound        // "" = the client sends | "webhook" | "callback": the API sends
                                   // the request and the client receives it
    WebhookName string             // OpenAPI 3.1 webhooks: the webhooks-map key, verbatim — an
                                   // event's name, not a path; only a webhook binding carries one
    PathDocs    Docs               // the path's own documentation, shared by every operation on it
                                   // (OpenAPI Path Item summary and description)
    Callbacks   []Callback         // out-of-band operations keyed by runtime expressions
    Unmodeled   Unmodeled
}

type RequestCompression struct { Encodings []string } // priority-ordered ("gzip", …)

type HTTPParamBinding struct {
    Param      ParamID            // this operation's own Operation.Params entry it binds, by ID
    ParamPath  []PropID           // nested source field within the logical param, when the binding
                                  // targets a sub-field of a message-typed param (gRPC transcoding
                                  // {book.name}, dotted query params); empty = the whole param
    Location   HTTPLocation       // path | query | querystring | header | cookie | body |
                                  // body_property | host
                                  // host = param fills a HostPrefix label (Smithy @hostLabel)
                                  // querystring = the whole query string serialized from one schema
                                  //   (OpenAPI 3.2 in: querystring, combined with ContentType)
    WireName   string
    Style      string             // simple | form | label | matrix | deepObject | pipe/space-delimited
    Explode    *bool              // as declared; nil = the style's default: true under form and
                                  // cookie, false under any other; querystring has no style,
                                  // so nil there = no explode
    AllowReserved bool
    AllowEmptyValue bool          // the client may send an empty value in place of omitting
                                  // the param (OpenAPI/Swagger allowEmptyValue); no effect under
                                  // a Style that cannot write one (space/pipe-delimited,
                                  // deepObject)
    PathPattern string            // multi-segment path pattern constraint for this param
                                  // (gRPC transcoding {name=shelves/*/books/*}); "" = single segment.
                                  // The URI template uses reserved expansion; emitters that cannot
                                  // validate the pattern drop it with a diagnostic
    Prefix     string             // map-typed param spread as prefixed wire entries:
                                  // prefixed headers (Smithy @httpPrefixHeaders) or catch-all
                                  // query maps (@httpQueryParams with Prefix "")
    ContentType string            // param serialized as a media type (OpenAPI content-style params)
    BodyPath   []PropID           // for body_property: where in the body model it lands (TypeSpec HttpProperty)
}

type Callback struct { Name, Expression string; Operations []OpID } // Name: the callbacks-map key
```

`AllowEmptyValue` sits on the binding with a parameter's other serialization facts, the
per-parameter wire detail `prior-art.md` records the client IR keeping there. OpenAPI calls the
field NOT RECOMMENDED, but a document that writes it still states it, Swagger 2.0 writes it too, and
a request validator or a mock server needs it to accept `name=`. It is a plain bool beside
`AllowReserved`, since a declared `false` and an absent one mean the same.

Every logical parameter — identified by its `ParamID`, not its name — is bound exactly once **per
non-host location; a `host` binding is additive** — Smithy `@hostLabel` members expand into the host
prefix *and* still serialize at their modeled location, by spec. TypeSpec's `HttpProperty` role+path
flattening is the model here: nested `@header`/`@body` annotations resolve to explicit
`(role, wire name, path)` bindings rather than restructured models.

## Inbound operations

Most operations are requests the client sends. Some are requests the API sends and the client
receives, and every consumer reads the two the opposite way round: a request the API sends is what
an SDK decodes and a server would encode, so read-only and write-only projections flip, and the
usage a type has (`irfacts`, [usage](./derived-facts.md#usage)) swaps input for output. `Inbound`
states which an operation is, on the binding, since what routes the request differs by kind:

- **A webhook** (`webhook`) is a request the API sends on an event, to a URL arranged outside the
  document, by an out-of-band registration for instance. An OpenAPI `webhooks` entry is keyed by
  the event's name, not by a URL, so its operations' bindings carry that key in `WebhookName` and
  leave `URITemplate` empty: a `{tenant}` in the key is part of a name rather than a variable
  nothing binds. The key stays verbatim rather than becoming a `Naming`, because the operation's
  `Name` is what an emitter renders, synthesized from the method and this key when no
  `operationId` is declared, and an empty key is legal and would name nothing.
- **A callback** (`callback`) is a request the API sends to a URL an operation the client sent
  supplies. Its binding keeps the runtime expression resolving that URL in `URITemplate`, and the
  operation that declares it lists it among its binding's `Callbacks`, under the name the source
  maps it by (`Callback.Name`, an OpenAPI callbacks-map key) and the expression.

`irverify` reports a route in the field `Inbound` does not select (`ir/webhook-route`), a kind it
does not declare (`ir/unknown-http-inbound`), and a callback whose two halves disagree: one marked
that no binding lists (`ir/callback-unlisted`), or one listed that is not marked
(`ir/callback-unmarked`). An inbound operation inherits no security requirement
([auth](./auth.md#inbound-operations)).

## Path documentation

A path can be documented apart from any operation on it: an OpenAPI Path Item's `summary` and
`description` apply to every operation the item mounts, and each operation may document itself
too. `PathDocs` holds the first and `Operation.Docs` the second, so neither displaces the other and
nothing is attributed to an operation its author did not write about. It sits on the binding beside
the route it documents, written on every operation the path mounts as `URITemplate` is; a path
mounting no operation has no binding to hold it. A format whose grouping construct carries the
documentation, a TypeSpec interface or a Smithy resource, writes it on the `OperationGroup`
([services](./services.md)), and an AsyncAPI channel's on the `Channel`
([messaging](./messaging.md)); the IR has no path entity, and adding one for a pair of strings
would make every route a node.
