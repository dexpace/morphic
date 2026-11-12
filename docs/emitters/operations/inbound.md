# Operations the API sends

An operation whose HTTP binding is `Inbound`, a `webhook` or a `callback`
([inbound operations](../../ir/http-binding.md#inbound-operations)), is a request the API sends and
the SDK's user receives. The other documents under `operations/` read an operation from the side of
a caller sending it; this one reads it from the side receiving it. The OpenAPI compiler lowers both
kinds (its `webhooks` and `callbacks` cases); the golden spec declares neither, 0 of its 3,794
operations being inbound (spec @bc6fad24, morphic @fa253fe19).

## What an SDK generates

No client method: the caller does not send the request, and a webhook has no URL a method could
send it to. A target generates instead, for each service holding an inbound operation:

- **A receiver**, one type beside the service's client and in its unit, configured with the checks
  below, holding one parse method for each inbound operation of the service. It needs none of the
  client's transport, servers or credentials, since a credential the API presents to the receiver
  is not the one the client presents to the API ([auth](../../ir/auth.md#inbound-operations)).
- **The operation's request type**, as [request shaping](../refine/requests.md) builds it, in the
  `read` projection the API's side gives it
  ([shapes](../plan/types.md#a-shape-reaches-what-it-holds)), holding its parameters and its body.
  It lands in the operation's group's unit ([output units](../refine/output-units.md)), which the
  receiver imports as the client imports its sub-clients.
- **A response type for each response it declares**, `Return`, each of `Others` and each of
  `Errors` alike, since the receiver chooses which to answer: its status, its declared headers and
  its body, with a method writing them. A range's type holds a status the range admits, and refuses
  one outside it. Nothing raises on a failure status, and no envelope wraps a body
  ([envelopes](../plan/responses.md#envelopes)), since the receiver writes the response rather than
  reads it.

Each target's spelling is in its document
([Go](../targets/go/operations.md#operations-the-api-sends),
[Kotlin](../targets/kotlin/operations.md#operations-the-api-sends)).

## Reading the request

A parse method reads one request as it arrived, in an order a signature needs:

1. **The method** is the binding's, or the request is refused.
2. **The body** is read once, as bytes, up to the bound runtime policy gives a buffered body
   (`Policy.Errors`, [failures](errors.md)), and a body past it is refused unread beyond it. Nothing
   decodes it before the checks, which read the bytes as they arrived: a signature covers those, and
   neither a decoded value nor its encoding again reproduces them.
3. **The checks.** The operation's own `Auth`, as `OpPlan.Auth` holds it, says how the API
   authenticates to the receiver ([auth](auth.md)); an option passes when each of its schemes does,
   and the request when one option does, and an operation stating none checks nothing. An `apiKey`
   scheme's value, in the header, query parameter or cookie it names, and an `http` `basic` or
   `bearer` scheme's credentials are compared with the ones the receiver is configured with, in
   constant time. Any other scheme, `mutualTLS`, `oauth2`, `openIdConnect`, another `http` scheme
   or one of kind `unresolved`, is the user's to check, so the receiver calls the user's check for
   it, and refuses the request where none is configured rather than pass what it cannot check. The
   user's check then runs over the headers and the bytes, as the place to verify a signature, for
   which no format the IR is designed against defines a scheme. A request a check refuses is a
   verification failure, a value apart from a decode failure, so a handler answers it as such.
4. **The parameters** are read from their text as their locations' rules say, as the mock server
   reads a request's ([docs and mocks](../docs-and-mock.md)), a required one absent refused.
5. **The body** decodes through the content whose media type the request's `Content-Type` names
   among those the operation declares, compared as [one election](../plan/responses.md#one-election)
   compares media types, since the API chooses which it sends. One the operation does not declare is
   refused, never decoded as the elected one. Unknown members, variants and enum values are read as
   a client reads a response's ([plan policy](../plan/policy.md)).

What the request's sender controls is its body, its headers and its query. The body is held to the
bound above and decodes only past the checks; the headers and the query are what the user's HTTP
server already bounds, Go's `net/http` at 1 MB of headers by default, and the receiver reads only
the parameters the operation declares, each once.

## What the plan holds for one

`OpPlan` keeps its fields' meanings. `Return`, `Others` and `Errors` each plan a response the
receiver may write, its content elected as for one a client reads, which is the content the
response is written in. `RequestContent` is the content the mock server sends. `Auth` is the
operation's own. `Effects` and the retry settings decide nothing, since nothing sends it again, so
the operation's row of the runtime's settings holds only the bound its body is read to. The plan
infers no `Page` and no `LRO` for an inbound operation, and a `Stream` on one is a warning
`emitters/<target>/unsupported-inbound-stream` with nothing generated for it, until a target designs
a receiver reading frames.

`Inbound` is the HTTP binding's alone. An AsyncAPI `receive` operation is a message the client
receives, a messaging target's through `MsgView` ([bindings](../plan/bindings.md)); a GraphQL
subscription is a call the client starts; an OTP binding names a process the client calls. None is
a request the API sends to a URL, so the receiver reads HTTP bindings alone.

## The mock server and the docs

The mock server stands in for the API, so it sends these requests rather than routing them. After
answering a request of the operation that declares a callback with a success, it resolves the
callback's expression against that request and its answer, as OpenAPI defines runtime expressions,
and sends the callback's request once to the URL it names, after the answer is written, its values
the callback's first success scenario's `Input` ([one set of
examples](../docs-and-mock.md#one-set-of-examples)). It sends at most one request for each callback
of each request it answers, only to an `http` or `https` URL, follows no redirect, gives up past the
timeout its options set, and discards the answer, so a request naming a URL costs it one bounded
request and nothing more. A webhook has no URL in the document, so the mock sends one only when its
own control route asks it to, naming the operation and the URL.

The docs give an inbound operation a page under its group saying the API sends it: a webhook's event
by its `WebhookName`, a callback's declaring operation, linked, and its expression. The page shows
its request as the receiver reads it, the checks its `Auth` asks, and the responses the receiver may
answer with. No SDK sample is shown, since a sample spells a call to the API
([samples](../docs-and-mock.md#samples-come-from-the-sdk-emitters)).

T-71 holds this document on every target.

## Rejected

- **A client method**, as the documents read every operation before, sends the request the API
  sends, to a URL the document does not give a webhook, and decodes the answer the API reads.
- **A handler interface the SDK routes requests to.** Routing needs to know which operation a
  request is, and nothing on the wire says so: `WebhookName` is the document's key, and webhooks
  sharing one URL are told apart by a body member only the API's prose names. A parse method per
  operation lets the user's own route decide, as it does already.
- **A server the SDK runs** would tie the SDK to one HTTP server, where each target takes the
  request as its servers hand it over.
- **Decoding before the checks** acts on a body nothing has verified.
