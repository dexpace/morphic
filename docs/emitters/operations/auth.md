# Auth

OR-of-ANDs, a service default with an op override.

IR: `Service.Auth = [ AuthRequirement{Schemes:[apiKey]} ]`;
`Operation.Auth = [ AuthRequirement{Schemes:[oauth2(scopes:[read])]},
AuthRequirement{Schemes:[apiKey]} ]` (priority order); auth schemes in `Document.Auth`.

- **plan.** `OpPlan.Auth` = the operation's options, priority-ordered (the op overrides the service
  default). An empty option inside a non-empty list = "no auth is one acceptable choice"; an empty
  `Operation.Auth` slice would mean "explicitly public".
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
  credentials are never sent in its place, and neither is a request with none.
