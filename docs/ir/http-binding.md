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
    RequestContentTypes  []string  // the request body's media types, as written and in source
                                   // order, which states no preference among them
    ResponseBodyPath *PropPath     // HTTP response body = this sub-field of the response type
                                   // (gRPC transcoding response_body); nil = the whole payload
    SuccessStatus map[int]int      // response index → primary status (denormalized convenience; conditions are the truth)
    Compression *RequestCompression // client MUST compress the request body (Smithy @requestCompression)
    ChecksumRequired bool          // client MUST send a payload checksum (Smithy @httpChecksumRequired)
    PatchImplicitOptionality *bool // true = a PATCH body makes every member optional, false = it
                                   // does not (TypeSpec @patch implicitOptionality); nil = the
                                   // source states nothing (below)
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
    Explode    *bool
    AllowReserved bool            // written as RFC 6570's reserved expansion writes it: reserved
                                  // characters and percent-encoded triples pass unescaped, bar
                                  // those a URL cannot hold there (docs/emitters/refine/text.md#escaping)
    AllowEmptyValue bool          // the client may send an empty value in place of omitting
                                  // the param (OpenAPI/Swagger allowEmptyValue); no effect under
                                  // a Style that cannot write one (space/pipe-delimited,
                                  // deepObject)
    PathPattern string            // the path segments the param fills (gRPC transcoding
                                  // {name=shelves/*/books/*}); "" = one segment. Where it names
                                  // several, holding a / or a **, the value is percent-encoded in
                                  // all but / and RFC 3986's unreserved characters, a % among
                                  // them, so each / cuts a segment, whatever AllowReserved says
                                  // (docs/emitters/refine/text.md#escaping); a consumer that cannot
                                  // check a value against the pattern skips that check, never
                                  // the escaping
    Prefix     *string            // set: a map spread over the headers or query parameters under
                                  // the prefix, "" every one (Smithy @httpPrefixHeaders,
                                  // @httpQueryParams); nil = the one field WireName names
                                  // (below)
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

## Prefix bindings

A binding can spread a map-typed parameter over many fields rather than send it as one: Smithy's
`@httpPrefixHeaders` binds a map of strings to every request header whose name starts with a
prefix, and `@httpQueryParams` binds a map of strings, or of lists of strings, to every query
parameter. A set `Prefix` makes the binding such a spread, at a `header` or a `query` location:
each entry of the map is a header, or a query parameter, named by the prefix followed by the
entry's key, and the empty prefix, which `@httpPrefixHeaders("")` writes and `@httpQueryParams`
means, takes every name. The binding names no field of its own, so it sets no `WireName`, and a nil
`Prefix` is a binding of the one field `WireName` names. How an entry gives way to a field another
binding writes is the emitter's ([text](../emitters/refine/text.md#the-text-of-a-value)).

`Prefix` is a pointer because the empty prefix is a prefix, as `Header.Prefix` is for a response's
headers ([prefix headers](./responses.md#prefix-headers)): a `Prefix string` whose empty value means
one field spells neither catch-all. A location of its own for each catch-all, the other design
weighed, would put a second spelling of `header` and `query` beside the existing ones for every
reader of `Location` to know.

`irverify` holds a prefix binding to a `header` or `query` location (`ir/prefix-binding-location`),
to no `WireName` (`ir/prefix-binding-wire-name`) and, where it binds a whole parameter, to one whose
type reaches a map, directly or through a [decorator](./annotations.md#decorators)
(`ir/prefix-binding-not-map`); the member a `ParamPath` names is not resolved. Its [correlation
key](./derived-facts.md#correlation-keys) is its location and its prefix, a header's in lower case,
so renaming the parameter keeps it.

## Patch optionality

`PatchImplicitOptionality` says whether a `PATCH` body is a patch shape, every member optional
whatever its schema requires. `true` asks for one, as TypeSpec's `@patch(#{implicitOptionality:
true})` does, and `false` asks for none. Nil states nothing, so the body requires the members its
schema requires, as any other body does: an OpenAPI document states which members a `PATCH` body
requires as it does for any body, and has no construct that sets the field. A consumer relaxing a
body the field leaves nil does so by a policy of its own, as an emitter reads a merge patch
([TypePlan](../emitters/plan/types.md#what-a-patch-relaxes)); the IR asks it to relax nothing.

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
