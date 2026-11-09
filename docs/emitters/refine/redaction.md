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

- `TypePlan.Sensitive` is true of a type that is `Sensitive` or whose `Base` chain reaches one, read
  through `TypePlan.Alias` ([decorators](decorators.md)).
- `TypePlan.Redacted` lists the flattened members of a model a value is redacted at: each whose
  `FlatProperty.Secret` is set, which the IR accumulates over every declaration of the member, an
  `Overridden` one included, as it does requiredness ([the
  flattening](../../ir/models.md#the-flattening)). A member a later conjunct redeclares without
  `Secret` stays redacted, which a reader of the one declaration a member takes would miss.

A value is redacted where its position is `Secret` or `Redacted`, or its type sensitive. A list's,
a tuple's and a map's contents are redacted each on its own when their type is, and a map's keys
only when the map's own position or type is. A union variant is redacted when its type is.

Redacting changes how a value is shown and nothing else. Every rendering an SDK makes of a value as
text, other than its wire form, shows a redacted one as `[REDACTED]`: a model's and a request
type's string form, the lifecycle events the logging hooks emit ([runtime
policy](../contract/runtime-policy.md)), a URL's path segment or query value from a redacted
parameter, a header's value, and every error message. A redacted collection shows the marker
alone, not its length, since how many tokens or keys a value holds can be what a reader must not
learn. A body kept as it arrived, a failure's `RawBody` or a fallback's untyped JSON, holds the
wire's bytes, so the logging hooks never print a raw body whose declared type holds a redacted
position, printing its decoded, redacted form instead. The docs emitter marks a redacted parameter
or property secret and shows its example as the marker.

