# Redaction

`Property.Secret`, `Parameter.Secret` and `TypeCommon.Sensitive` shape what an SDK prints and
nothing it sends: a redacted value is sent and decoded as any other.

## A redacted value

The IR asks for redaction in two places. `Property.Secret` and `Parameter.Secret` ask it of one use,
and `TypeCommon.Sensitive` of every value of a type and of every type whose `Base` reaches it
([properties](../../ir/properties.md), [operations](../../ir/operations.md)). An emitter that read
only the first would print in the clear every value the IR marks through its type: a property whose
type is a sensitive component, a list of `format: password` strings, a parameter or a header whose
type is sensitive. So the plan reads both once:

- `TypePlan.Sensitive` is true of a type that is `Sensitive` or whose `Base`, a scalar's or a
  model's, reaches one, read as the IR's flattening reads it for `FlatProperty.Secret`: a `Base`
  cycle the IR permits ends at a type already on the chain. Collapsing a decorator never hides one,
  as a `Sensitive` node is no decorator ([decorators](decorators.md)).
- `TypePlan.Redacted` lists the flattened members of a model a value is redacted at: each whose
  `FlatProperty.Secret` is set, which the IR accumulates over every declaration of the member, an
  `Overridden` one included, as it does requiredness ([the
  flattening](../../ir/models.md#the-flattening)). A member a later conjunct redeclares without
  `Secret` stays redacted, which a reader of the one declaration a member takes would miss.

A value is redacted where its position is `Secret` or `Redacted`, or its type sensitive. A list's
and a tuple's elements, and the keys and values of a map or of a model's catch-all, are redacted
each on its own when their type is, so a map whose values alone are redacted shows its keys. A union
variant is redacted when its type is. The docs and a command-line target read what is redacted here
as an SDK does, so none shows a value another hides.

A parameter is redacted as well where it sits in the slot a scheme of the document writes its
credential to, whatever its declaration marks: the header, query parameter or cookie an `apiKey`
scheme's `In` and `KeyName` name, a header's name compared without case, and the `Authorization`
header where `Document.Auth` holds an `http_basic`, an `http_bearer`, an `oauth2` or an
`openid_connect` scheme, or a `custom` one stating an HTTP `Scheme`, as OpenAPI's `scheme: digest`
lowers. No other kind's credential sits where a parameter can
([auth](../operations/auth.md#where-a-credential-can-be-read)). A document may declare such a
parameter beside its scheme, and the compiler keeps an `Authorization` header parameter OpenAPI says
to ignore ([`reserved-header-names`](../../compilers/openapi.md#reserved-header-names)), so the
request type holding it would print a credential. Where one of the operation's own options names the
scheme the parameter gets no input at all, and a header parameter named `Authorization` or
`Proxy-Authorization` is redacted whatever the schemes are
([auth](../operations/auth.md#a-parameter-in-a-credentials-slot)). The rule reads every scheme of
the document, not those an operation requires, since a caller may send one an operation does not ask
for. It marks nothing on the golden spec, none of whose 11,994 parameters is `Secret` or sits in a
slot of its four declared schemes; what it does not reach is a credential only a name suggests, the
`R2-Secret-Access-Key` header two of its operations declare, which stays the document's to mark
(spec @bc6fad24, morphic @fa253fe19).

Redacting changes how a value is shown and nothing else. Every rendering an SDK makes of a value as
text, other than its wire form, shows a redacted one as `[REDACTED]`: each form the target's
printing and logging read of every type the SDK or its runtime declares that holds one, a model, a
request type, a union, each variant's wrapper and a response's headers type among them, and of a
sensitive type the SDK declares itself, where the target can give it one; the lifecycle events the
logging hooks emit ([runtime policy](../contract/runtime-policy.md)); a URL's path segment or query
value from a redacted parameter; a header's value; and every error message, a transport failure's
included, which holds no text a library wrote ([below](#nothing-a-library-wrote-is-shown)). A
rendering that asks the value nothing, as Go's `fmt` asks no method under a verb it reports as bad,
`%w` outside `Errorf` and `%p` of a struct, nor below an unexported field, shows nothing of a
redacted one either, since a target holds it where such a rendering does not reach
([Go](../targets/go/models.md#a-redacted-member)). The wire form is written from the value, never
through those forms. A value a caller takes out of every holder, as a string or a list of its own,
renders as the target renders it. So does a value a renderer reaches without asking it, which shows
what the value holds: one encoding it as JSON, as logrus's JSON formatter and zap's `Reflect` do, or
reading its fields, as Node's `console.log` and `JSON.stringify` do a JavaScript object's, a
Kotlin/JS one's among them, since no form a type declares reaches either. A credential an auth
injector writes is no IR fact this reads: it is kept out of every rendering by where the auth step
runs, not by a mark ([auth](../operations/auth.md#where-a-credential-can-be-read)). A redacted
collection shows the marker alone, not its length, since how many tokens or keys a value holds can
be what a reader must not learn. A body kept as it arrived, a failure's `RawBody` or a fallback's
untyped JSON, holds the wire's bytes, so no rendering above prints a raw body whose declared type
holds a redacted position, showing its decoded, redacted form instead. The docs emitter marks a
redacted parameter or property secret and shows a placeholder for its example
([docs](../docs-and-mock.md#what-the-docs-render)).

## What the runtime holds raw

What the runtime keeps as it arrived or as it is sent has no mark to read, and any of it may be a
credential ([auth](../operations/auth.md#where-a-credential-can-be-read)): a response's headers and
body, a request, a URL a response supplied, the client's base URL and what it was configured with.
So no type the runtime declares, and no generated type holding a client, a pager's, a poller's or a
stream's state, shows any of it in any form, whichever positions the document redacts. The rule is
on where the value is held, not on a form the type declares, since a form answers only the reader
that asks for it: such a type holds the value where neither the target's formatting nor an encoder
walking a value's members reaches it, and hands it to a caller through a function the caller calls
([Go](../targets/go/returns.md#what-a-response-prints),
[Kotlin](../targets/kotlin/errors.md#what-a-failure-prints)). A form the type does declare shows its
status and its decoded `Body` and `Headers`, each through its own form, and no raw header, one its
entry does not redact included, since a response carries headers no document lists. What a caller
takes out, the headers `RawHeader` returns or the bytes `RawBody` does, is the caller's value and
renders as the target renders it. A debugger, or a library that reads memory past what the language
exports, shows what the process holds, and no declaration answers it. T-65 holds the rule.

## Nothing a library wrote is shown

A library builds its failures from what it was handed, and the runtime hands it a request, a
response's bytes and what the caller configured, any of which may hold a credential. So nothing the
runtime shows, a failure and each of its members, an event, a log line or a string form, holds text
a library wrote, and where the runtime catches a library's failure it keeps only what the program or
the document spells:

- a decoder's failure, of a response's or an error's body, a frame or a request a receiver parses,
  and an encoder's, of a request: the type the value was read into or written from, and the types in
  the library's chain;
- a transport's failure, a refused connection's, a TLS handshake's and a name lookup's among them,
  and an `Authenticator`'s: the method, the URL as shown ([what an event
  holds](../contract/runtime-policy.md#what-an-event-holds)) and the types in the chain;
- a URL that does not parse or has no [shown
  origin](../contract/runtime-policy.md#what-an-event-holds), a base URL the caller gave, a
  redirect's `Location`, a link to the next page or a place to poll: the setting or the member it
  came from, never its text. Such a redirect is returned as the response, and such a link or place
  fails the walk or the wait;
- a receiver's verification failure ([inbound](../operations/inbound.md)): the scheme, or the user's
  check, that refused, and nothing the request carried;
- a client refused when built: the setting, never its value.

The types are the program's text, and a caller still branches on what failed: in Go the failure
answers `errors.Is` through the library's chain, which it holds behind a function as `Raw` holds a
response, and in Kotlin each link of its `cause` is the runtime's exception naming the library's
class and carrying, on the JVM, that exception's stack frames
([Go](../targets/go/errors.md#a-transports-failure),
[Kotlin](../targets/kotlin/errors.md#a-transports-failure)). A JSON pointer and a byte offset were
weighed for a decoder's failure and are left out: a pointer's tokens are the body's names, a map's
keys among them (json/v2 wrote `within "/labels/<key>"`), and an offset or an index counts what the
body held before the failure, a redacted value's length among it, which no rendering of that value
shows ([above](#a-redacted-value)). A pointer whose every key and index is written `*`, leaving
names the document declares, was weighed too and needs a decoder of the runtime's own: kotlinx
states a path only in its message, a map's key unescaped in it, so one key `a']['b` reads as two do,
`$.labels['a']['b'].n`, and none under a union read as a `JsonElement`, and json/v2's pointer under
a Go union starts at the union. A caller who wants the library's account decodes `RawBody` itself,
its own value, which holds a body whole only up to `Policy.Errors`' bound
([failures](../operations/errors.md)): past it the bytes kept are cut, and the caller raises the
bound, a client option, before calling. Measured with Go 1.27.3, none of the 1,073 renderings of
each of a refused connection, a refused TLS handshake, bodies failing at a member and at a map key
holding the value, an `Authenticator`'s error quoting it and a base URL that does not parse held the
value, where `url.Parse` wrote the URL whole into its error; with Kotlin 2.4.0, Ktor 3.2.3 and
kotlinx.serialization 1.8.1, none of the 28 renderings of each such failure did, where Ktor's
timeout wrote the URL and kotlinx the body (`JSON input: {"id":"u1","token":…`), and a decoder's
chain kept 9 of kotlinx.serialization's frames. T-65 holds this section.
