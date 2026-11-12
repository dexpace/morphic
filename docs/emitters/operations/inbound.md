# Operations the API sends

An operation whose HTTP binding is `Inbound`, a `webhook` or a `callback` ([inbound
operations](../../ir/http-binding.md#inbound-operations)), is a request the API sends and the SDK's
user receives. The other documents under `operations/` read an operation from the side of a caller
sending it; this one reads it from the side receiving it. The OpenAPI compiler lowers both kinds
(its `webhooks` and `callbacks` cases); the golden spec declares neither, 0 of its 3,794 operations
being inbound (spec @bc6fad24, morphic @fa253fe19).

## What an SDK generates

No client method: the caller does not send the request, and a webhook has no URL a method could send
it to. A target generates instead, for each service holding an inbound operation:

- **A receiver**, one type beside the service's client and in its unit, configured with the checks
  below, holding one parse method for each inbound operation of the service. It needs none of the
  client's transport, servers or credentials, since a credential the API presents to the receiver is
  not the one the client presents to the API ([auth](../../ir/auth.md#inbound-operations)).
- **The operation's request type**, as [request shaping](../refine/requests.md) builds it, in the
  `read` projection the API's side gives it
  ([shapes](../plan/types.md#a-shape-reaches-what-it-holds)), holding its parameters and its body.
  It lands in the operation's group's unit ([output units](../refine/output-units.md)), which the
  receiver imports as the client imports its sub-clients.
- **A response type for each response it declares**, `Return`, each of `Others` and each of `Errors`
  alike, since the receiver chooses which to answer: its status, its declared headers and its body,
  with a method writing them, landing beside the request type. A range's type holds a status the
  range admits, and a `Default`'s one no other response of the operation declares, and each refuses
  any other. Nothing raises on a failure status, and no envelope wraps a body
  ([envelopes](../plan/responses.md#envelopes)), since the receiver writes the response rather than
  reads it.

Each target's spelling is in its document
([Go](../targets/go/operations.md#operations-the-api-sends),
[Kotlin](../targets/kotlin/operations.md#operations-the-api-sends)). T-71 holds this section.

## Reading the request

A parse method reads one request as it arrived, in an order a signature needs:

1. **The method** is the binding's, or the request is refused.
2. **The body** is read once, as bytes, to the bound runtime policy gives a buffered body
   (`Policy.Errors`, [failures](errors.md)), and a body past it is refused with no more than one
   byte past the bound read or held. A receiver handed the request unread, as Go's is, reads that
   far itself. One handed the bytes, as Kotlin's is by whichever server its caller runs, cannot stop
   that server reading, so it states the largest of its operations' bounds for the server to read
   to, and each parse method refuses a body longer than its own operation's. Nothing decodes the
   body before the checks, which read the bytes as they arrived: a signature covers those, and
   neither a decoded value nor its encoding again reproduces them.
3. **The checks.** The operation's own `Auth`, as `OpPlan.Auth` holds it, says how the API
   authenticates to the receiver ([auth](auth.md)), and is read as the IR defines it
   ([auth](../../ir/auth.md)): an alternative is satisfied when each of its schemes is, and the
   request passes on the first alternative, in order, that is. The receiver satisfies a scheme
   itself, comparing in constant time, where the credential sits in the request's text and the
   receiver was given its value: an `apiKey` with `In` `header`, `query` or `cookie`, after its
   `Scheme` where it states one, an `http_basic` and an `http_bearer` ([where a credential can be
   read](auth.md#where-a-credential-can-be-read)). The caller's check stands in for exactly what the
   receiver has no means to compare: any other scheme, one of those whose value the receiver was not
   given, and an alternative naming no scheme, which an operation stating no alternative counts as
   holding. The check is told what it stands in for, the operation and the scheme, or that the
   alternative names none, and is handed the request: its method and target, the query in it, its
   headers, the body's bytes as they arrived, and the certificates the connection presented where
   the caller's server exposes them, which a `mutual_tls` or `x509` scheme is checked against. It is
   the place to verify a signature, for which no format the IR is designed against defines a scheme;
   a caller verifying one beside a scheme the receiver could compare gives the receiver no value for
   that scheme, and the check answers for both. With no check configured, what it would stand in for
   is unsatisfied: comparing a key the receiver was never given with an absent one would pass a
   request carrying none. An alternative naming no scheme the IR reads as admitting a sender that
   authenticates nothing, but a document has no scheme to state a signature with, so one stating
   none may still sign: the receiver takes such an alternative as satisfied without the check only
   where it was built stating that its operations admit an unauthenticated sender, and one built
   with no setting parses no forged body. A request no alternative admits is a verification failure,
   a value apart from a decode failure, so a handler answers it as such.
4. **The parameters** the operation declares are read from their text as their locations' rules
   say, as the mock server reads a request's ([docs and mocks](../docs-and-mock.md)), a required one
   absent refused. One no style arranges is read as its reader is given it: the string, or every
   pair of its location that its binding owns, in the order they came ([values as
   text](../refine/text.md#a-value-no-style-arranges)).
5. **The body** decodes through the content whose media type the request's `Content-Type` names
   among those the operation declares, compared as [one election](../plan/responses.md#one-election)
   compares media types, since the API chooses which it sends. One the operation does not declare is
   refused, never decoded as the elected one. Unknown members, variants and enum values are read as
   a client reads a response's ([plan policy](../plan/policy.md)).

What the request's sender controls is its body, its headers and its query. The body is held to the
bound above and decodes only past the checks, a form field or a part arriving within it. Of the
headers and the query the receiver reads only the parameters the operation declares, each once, but
an input no style arranges holds as many pairs as the sender wrote. What bounds those is the request
line and the headers the server the receiver runs in admits, Go's `net/http` 1 MB of the two
together by default, which the receiver cannot set: its caller configures that server. The caller's
check runs at most once for each scheme and each alternative naming none in the operation's `Auth`,
a count the document sets. T-71 holds this section.

## What the plan holds for one

`OpPlan` keeps its fields' meanings. `Return`, `Others` and `Errors` each plan a response the
receiver may write, its content elected as for one a client reads, which is the content the response
is written in, and `Errors` holds no `CommonErrors`, which are what the API answers its client.
`RequestContent` is the content the mock server sends. `Auth` is the operation's own. `Effects` and
the retry settings decide nothing, since nothing sends it again, so the operation's row of the
runtime's settings holds only the bound its body is read to. The plan infers no `Page` and no `LRO`
for an inbound operation, and a `Stream` on one is a warning
`emitters/<target>/unsupported-inbound-stream` with nothing generated for it, until a target designs
a receiver reading frames.

`Inbound` is the HTTP binding's alone. An AsyncAPI `receive` operation is a message the client
receives, a messaging target's through its `Message` ([bindings](../plan/bindings.md)); a GraphQL
subscription is a call the client starts; an OTP binding names a process the client calls. None is a
request the API sends to a URL, so the receiver reads HTTP bindings alone. T-71 holds this section.

## The mock server and the docs

The mock server stands in for the API, so it sends these requests rather than routing them. After
answering a request of the operation that declares a callback with a success, it resolves the
callback's expression against that request and its answer, as OpenAPI defines runtime expressions,
and sends the callback's request once to the URL it names, after the answer is written, its values
the callback's first success scenario's `Input` ([one set of
examples](../docs-and-mock.md#one-set-of-examples)). It sends at most one request for each callback
of each request it answers, within the bounds [its options](../docs-and-mock.md#the-mocks-options)
set on where it may send, how many at once and for how long, and discards the answer, so a request
naming a URL costs it at most one bounded request and nothing more. A webhook has no URL in the
document, so the mock sends one only when its control route, which listens apart from the API, asks
it to, naming the webhook and the URL.

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
- **The caller's check on every request**, beside the receiver's own comparison, made a conjunction
  of two alternatives: with `[{http_basic}, {mutual_tls}]` and a check verifying the certificate,
  correct basic credentials over a connection presenting none were refused. A check told neither
  the operation nor the scheme could not tell which of the two it was asked about.
- **Checking nothing where nothing is declared or configured.** A receiver built with no setting
  answered a forged body 200 for an operation stating no security, and a request with no key for an
  `apiKey` scheme the receiver held no key for, since an absent key equals an unset one; a check
  handed the headers and body alone could not read a client certificate or a key in the query.
