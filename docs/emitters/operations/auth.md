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
