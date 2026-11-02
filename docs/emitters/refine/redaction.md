# Redaction and stability

Two facts the IR states about a value or an operation shape what an SDK prints and how it presents
a call, though neither changes the wire: a redacted value is sent and decoded as any other, and an
operation of any tier is called alike.

## A redacted value

The IR asks for redaction in two places. `Property.Secret` and `Parameter.Secret` ask it of one use,
and `TypeCommon.Sensitive` of every value of a type and of every type whose `Base` reaches it
([properties](../../ir/properties.md), [operations](../../ir/operations.md)). An emitter that read
only the first would print in the clear every value the IR marks through its type: a property whose
type is a sensitive component, a list of `format: password` strings, a parameter or a header whose
type is sensitive. So the plan reads both once:

- `TypePlan.Sensitive` is true of a type that is `Sensitive` or whose `Base` chain reaches one, read
  through `TypePlan.Alias` ([decorators](decorators.md)).
- `TypePlan.Redacted` lists the flattened members of a model a value is redacted at: each one any of
  its declarations marks `Secret`, an `Overridden` one included, as requiredness accumulates, or
  whose type is sensitive. A member a later conjunct redeclares without `Secret` stays redacted,
  which a reader of the one declaration a member takes would miss; the IR's flattening does not
  carry it yet (#1211), so the plan reads the declarations itself.

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

## A stability tier

`Operation.Stability` is how far a source commits to an operation, and `OperationGroup.Stability`
the tier of every member stating none, nearest group first, never copied down
([stability](../../ir/operations.md#stability)). The plan resolves it once, `OpPlan.Stability`:
the operation's own tier, else the nearest group's that states one, else none. An unstated tier is
never read as `stable`, so nothing renders a badge or an annotation for it.

What a tier lowers to is each target's, with one floor every target keeps: an `alpha` or `beta`
operation's documentation says its tier and what the tier means, in the generated code and in the
docs, which badge every stated tier, `stable` included. A target that can make a caller acknowledge
a tier may ([Go](../targets/go/operations.md#stability),
[Kotlin](../targets/kotlin/operations.md#stability)). No target leaves an `alpha` operation out:
hiding operations is the filter pass's (architecture.md §2.2), so the SDK and its docs hold the same
ones. On the golden spec (spec @319dc756) 3,190 of 3,763 operations state a tier.
