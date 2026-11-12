# Auth

OR-of-ANDs, a service default with an op override.

IR: `Service.Auth = [ AuthRequirement{Schemes:[apiKey]} ]`;
`Operation.Auth = [ AuthRequirement{Schemes:[oauth2(scopes:[read])]},
AuthRequirement{Schemes:[apiKey]} ]` (priority order); auth schemes in `Document.Auth`.

- **plan.** `OpPlan.Auth` = the operation's options, priority-ordered (the op overrides the service
  default, which an operation the API starts does not inherit:
  [auth](../../ir/auth.md#inbound-operations)). An empty option inside a non-empty list = "no auth
  is one acceptable choice"; an empty `Operation.Auth` slice would mean "explicitly public".
- **refine.** Auth *schemes* are structural IR, so refine emits an `Authenticator` interface, a
  per-scheme option type, and the request-time application code that satisfies the AND-within-option
  / OR-across-options-by-priority contract. Scope sets from `oauth2` flows reach the option types.
  What refine does **not** emit is any credential value or token-refresh behavior — the actual
  secrets and token acquisition are runtime configuration, not IR. SASL/x509/user_password schemes
  (Kafka/AMQP) lower the same way for a messaging target.
- **emit.** Printer emits the `Authenticator` interface, the option constructors, and the per-request
  auth selector.

## Unresolved schemes

A scheme of kind `unresolved` ([auth](../../ir/auth.md#unresolved-schemes)) names a credential the
compiler holds no mechanism for. The compiler reports an error for each, so a document holding one
reaches an emitter only where its caller lowered the threshold past that error
([what fails a run](../../architecture.md#4-diagnostics--provenance)). The emitter reports nothing
of its own: the document's diagnostics already carry it.

- **plan.** The option stays in `OpPlan.Auth`, in its place. The requirement is the operation's, so
  dropping the option would offer one way fewer to authenticate, or none, and an operation left with
  none must not fall back on the service's.
- **refine.** The scheme gets a per-scheme option like any other, taking an `Authenticator` the
  caller writes, since nothing says how the credential is applied. The option's documentation
  carries the scheme's `Docs`. Two names that render alike, such as `APIToken` beside `api_token`,
  are settled by ID as any collision is ([naming](../refine/naming.md)).
- **emit.** Left unconfigured, the option is an unsatisfied credential: the selector takes the next
  option in priority order, and an option ANDing the scheme with others needs it like any of them.
  If no option remains, the request is refused unsent, with an error naming the scheme. The refusal
  is where a request starts: a call's error, a pager's first page, a stream's open. The service's
  credentials are never sent in its place, and a request goes with none only where the list itself
  offers the empty option.

## A declared credential shape

`AuthScheme.Credentials` names the type of what a caller configures where a scheme's credential is
several fields combined into a request ([auth](../../ir/auth.md)). Nil, which every compiler
writes today, changes nothing here. Where a scheme states one, whatever its `Kind`:

- **refine.** The scheme's option takes the caller's `Authenticator`, as an unresolved scheme's
  does ([above](#unresolved-schemes)): the IR says what the fields are and nothing of where each
  goes, so the kind's own injector, which writes one value, does not apply. The type is declared
  as any registered type is, for that `Authenticator` to be built from, and no option takes the
  value itself, which nothing generated would read.
- **redaction.** A value of the type is a credential whole, so every form of it shows
  `[REDACTED]`, as a sensitive type's does ([redaction](../refine/redaction.md#a-redacted-value)),
  whatever its members mark.

T-65 holds this section.

## A parameter in a credential's slot

A document may declare as a parameter what a scheme already sends: an `X-Api-Key` header parameter
beside the `apiKey` scheme naming that header, or an `Authorization` header parameter, which the
OpenAPI compiler keeps and reports
([`reserved-header-names`](../../compilers/openapi.md#reserved-header-names)). A **slot** is a
header, its name compared without case, a query parameter or a cookie, and a scheme owns the one
its row [below](#where-a-credential-can-be-read) names. Left as declared, the request type and the
scheme's injector both write it. What a parameter in a slot lowers to follows from who else
writes there:

- **A scheme one of the operation's options names owns it** (`OpPlan.Auth`). The slot is the auth
  step's alone: `BuildRequestShaping` gives the parameter no input, whatever its `Required`, its
  default or its type say, and the request builder writes nothing there. The credential is the
  client's to configure once, and a required field would ask every call for it. An option left
  unconfigured sends nothing in the slot, and the request is refused where no option remains
  ([above](#unresolved-schemes)).
- **A scheme of the document owns it, and none of the operation's options names that scheme**, as
  on an operation the document makes public. No injector writes there for this operation, so the
  parameter is an input like any other, the one way its caller sends the value, and it is
  redacted ([redaction](../refine/redaction.md#a-redacted-value)).
- **No scheme owns it.** It is an input like any other, redacted only where its declaration says
  so, save one case: a header parameter named `Authorization` or `Proxy-Authorization` is
  redacted whatever the document's schemes, since RFC 9110 §11.6.2 and §11.7.2 define each
  field's value as credentials.

An `Authenticator` the caller wrote may write any slot, a kept parameter's among them. The auth
step runs last and sets what it writes, never adding a second value
([below](#where-a-credential-can-be-read)), so no request carries a slot twice. An operation the
API sends keeps such a parameter: its receiver reads the slot to check it
([operations the API sends](inbound.md)), and the parsed request holds it redacted. The docs
show a parameter the auth step writes with its scheme, and among no operation's inputs.

Two other rules were weighed. Dropping every parameter in a scheme's slot, as OpenAPI says of
`Authorization`, leaves an operation whose options name no scheme unable to send what it declares.
Keeping each as a field that overrides the client's credential makes a required one ask every call
for what the client holds, or relaxes a declared `Required` to make it optional. A document that
later lists the scheme among an operation's options removes the field, a break the surface differ
reports ([surface verification](../surface-verification.md)). The rule reads `Document.Auth` and
`OpPlan.Auth` and nothing a name suggests, and on the golden spec it finds nothing: no parameter
sits in a slot of its four schemes ([redaction](../refine/redaction.md#a-redacted-value)) and no
header parameter is named `Authorization` or `Proxy-Authorization` (`jq '[paths(objects) as $p |
getpath($p) | select(.in? == "header" and (.name? // "" | ascii_downcase |
IN("authorization", "proxy-authorization")))] | length'` gives 0; spec @bc6fad24). T-65 holds it.

## Where a credential can be read

A credential is what a caller configures for a scheme, and no IR fact holds one, so no `Secret` or
`Sensitive` marks it ([redaction](../refine/redaction.md)): `Credentials` types such a value and
holds none ([above](#a-declared-credential-shape)). Where it sits in a call follows from the
scheme's `Kind`:

| `AuthKind` | Where its credential sits |
|---|---|
| `apiKey`, `In` `header`, `query` or `cookie` | the header, the query parameter or the `Cookie` pair `KeyName` names, after its `Scheme` where it states one |
| `http_basic`, `http_bearer`, and a `custom` scheme stating an HTTP `Scheme`, as `digest` lowers | the `Authorization` header (RFC 9110 §11.6.2) |
| `oauth2`, `openid_connect` | the `Authorization` header, as a token the caller's token source gave: the runtime sends no token request ([above](#auth)), so it has no event and no failure describing one, a client secret or a refresh token |
| `apiKey`, `In` `user` or `password`; `user_password`, `mutual_tls`, `x509`, the SASL kinds, `symmetric_encryption`, `asymmetric_encryption` | the connection the transport opens, never a request's text |
| any other `custom`, `unresolved`, and a scheme of any kind stating `Credentials` | wherever the caller's `Authenticator` writes: any header, query parameter or cookie |

A call holds secrets no scheme put there too: the user and password, or a signed query, of a base
URL its caller gave; a URL a response supplies already signed, in a redirect's `Location`, a link
to the next page or a place to poll; a response's `Set-Cookie`; and a body's fields. So the rule
names no header and no parameter: a list would miss the header an `Authenticator` invents and the
one the next scheme adds.

**The auth step runs last, and only writes.** For each attempt the runtime builds the request from
the operation's bindings, fixes what it will show of it ([the URL as
shown](../contract/runtime-policy.md#what-an-event-holds)), and only then lets the selector's
injectors and the caller's `Authenticator`s write into it and sends it. A write sets its slot,
replacing what the request builder put there, and adds no second value. Nothing the runtime
renders as text reads the request after that step, on a retry as on the first attempt, each of
which runs the step again. So a credential reaches the transport and nothing else, wherever it was
written:

- **An event** has no member a credential fits ([what an event
  holds](../contract/runtime-policy.md#what-an-event-holds)).
- **A failure's message** quotes a status, or a method and the URL as shown, never a header or a
  body. A failure a response caused holds that response's headers and body for its caller to
  read, a `Set-Cookie` among them, and shows neither in any form, whatever reads the value
  ([redaction](../refine/redaction.md#what-the-runtime-holds-raw)). A request refused unsent
  names its scheme ([above](#unresolved-schemes)), which is the document's name for it.
- **A transport's failure** is the library's, and a library quotes what it was sent. Measured with
  Go 1.27.3, a request to a closed port with a key in its query came back from `net/http` as
  `Get "http://127.0.0.1:1/v1/things?limit=5&pw=…": dial tcp 127.0.0.1:1: connect: connection
  refused`, the value in full, in a `*url.Error` whose `URL` member holds it too. With Ktor 3.2.3
  on Kotlin 2.4.0, each of the library's timeouts, the request's, the connection's and the
  socket's, wrote `[url=http://…?limit=5&api_key=…` into its message with the key in full, the
  last as a `java.net.SocketTimeoutException`, so an exception's class does not tell the
  platform's text from the library's. So on every target the runtime returns a failure of its
  own, holding the method, the URL as shown and, of the library's failure, only what the target's
  document shows to hold nothing of the request. In Go that is the cause under the library's
  wrapper, the `Err` of a `*url.Error`, and never the wrapper: with that, the same request's
  message was `GET http://127.0.0.1:1/v1/things?limit=5&pw=[REDACTED]: dial tcp 127.0.0.1:1:
  connect: connection refused`, and `errors.As` found no `*url.Error` in the chain. That cause's
  own text stays the library's, which the runtime cannot hold by construction, so T-65 sweeps it
  for each failure the behavior server can provoke. In Kotlin it is the names of the classes in
  the library's chain and none of its exceptions, and the runtime sets none of the library's
  timeouts, so the library builds no such message
  ([Kotlin](../targets/kotlin/errors.md#a-transports-failure)).
- **No dump.** The runtime declares no rendering of a request or a response as sent, a wire log
  or a command line that would replay it. A caller who wants one wraps the `Transport`, which is
  handed each request as it goes out, credentials included, since it sends it; what such a wrapper
  prints is its author's.
- **Docs and the mock.** A sample sets a credential from a placeholder the SDK target writes
  ([docs](../docs-and-mock.md#samples-come-from-the-sdk-emitters)), a scheme having no example in
  the IR to copy, and a mock server that logs a request logs what an event holds of it, so a key a
  test sends it reaches no log line.

A redirect to another origin carries none of the client's credentials ([runtime
policy](../contract/runtime-policy.md#the-canonical-vocabulary)), which is this rule's other half:
the step does not run for a host the client was not configured for.
