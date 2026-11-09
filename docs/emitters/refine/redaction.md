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

Redacting changes how a value is shown and nothing else. Every rendering an SDK makes of a value as
text, other than its wire form, shows a redacted one as `[REDACTED]`: each form the target's
printing and logging read of every type the SDK or its runtime declares that holds one, a model, a
request type, a union, each variant's wrapper and a response's headers type among them, and of a
sensitive type the SDK declares itself, where the target can give it one; the lifecycle events the
logging hooks emit ([runtime policy](../contract/runtime-policy.md)); a URL's path segment or query
value from a redacted parameter; a header's value; and every error message, a transport failure's
included. An HTTP library can write the URL it was sent into its own failures, as Go's `net/http`
and Ktor's timeouts do, so the runtime quotes a request's URL only in its redacted form, in a
failure as in a log line. The wire form is written from the value, never through those forms. A
value a caller takes out of every holder, as a string or a list of its own, renders as the target
renders it. A credential an auth injector writes is no IR fact this reads, so keeping it out of the
logging hooks' events is #1633's. A redacted collection shows the marker alone, not its length,
since how many tokens or keys a value holds can be what a reader must not learn. A body kept as it
arrived, a failure's `RawBody` or a fallback's untyped JSON, holds the wire's bytes, so no rendering
above prints a raw body whose declared type holds a redacted position, showing its decoded, redacted
form instead. The docs emitter marks a redacted parameter or property secret and shows a placeholder
for its example ([docs](../docs-and-mock.md#what-the-docs-render)).
