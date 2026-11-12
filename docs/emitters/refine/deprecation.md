# Deprecations

A `Deprecation` ([annotations](../../ir/annotations.md#replacements)) marks a type, a property, a
parameter, an operation, an enum member, a union variant, an auth scheme or a channel. Each
declaration the entity lowers to carries it in the mechanism its language's tools read: every
method an operation lowers to, its `WithResponse` method, a pager and each method `AlsoMount` gives
it among them, every shape a model projects, the field a property or parameter becomes in each
shape or request type holding it, an enum's constant, a variant's wrapper. `MarkDeprecation` adds
it once the declarations exist ([step list](steps.md)). Nothing deprecated changes what is sent or
read, so the mock server reads none of it.

A mark counts only where the target's tools show it at a use in another output unit, the caller's.
Where a target's mechanism marks a declaration partly or not at all, its document says so and puts
the text where the caller reads it instead: Kotlin 2.4.0 shows no use in another module of an enum
entry's `@Deprecated`, on the JVM or on Native ([Kotlin](../targets/kotlin/lowering.md)), and the
Swift export marks a class on its initializers alone and an enum entry nowhere
([Swift](../targets/kotlin/swift.md)). No target marks a parameter, so a path parameter passed
positionally ([request shaping](requests.md)), which is no field, has its text in its method's
documentation of it: a Go doc comment has no place on a parameter, and Kotlin 2.4.0 refuses
`@Deprecated` on one, `this annotation is not applicable to target 'value parameter'`.

## The text

The deprecation's text is its fields in one order: `Message` as written, escaped for where it is
written ([emit rules](../emit/rules.md)); a sentence naming each replacement (below); and a sentence
saying since when and until when, from `Since`, `RemovalVersion` and `RemovalDate`, each as the
source wrote it and each only where it is set. A deprecation setting none of them, as an OpenAPI
`deprecated: true` alone does, reads `The API no longer recommends it.` on every target: gopls
v0.24.0 reports a use only where text follows `Deprecated:`, so a bare marker would mark nothing in
Go. The message keeps whatever replacement its prose names, as the IR keeps it there. On the golden
spec (spec @bc6fad24, morphic @fa253fe19), 465 of 778 deprecations set no field and 308 carry a
message, none names a replacement in `ReplacedBy`, and of the 187 operations'
`x-stainless-deprecation-message` values, 175 name a replacement operation: 147 by a Markdown link
to its page, 26 by its method and path, as `Use GET /events/event-categories/by-id/{category_id}
before 2026-11-28.` does, and 2 by its path alone. T-70 holds this section.

## Each replacement

Each `ReplacedBy` entry, in order, is named through the declaration its entity lowers to, as a
reference by ID that the printer resolves once `ResolveCollisions` has run, as it resolves a
[`Docs.Refs`](../../ir/annotations.md#references-in-prose) span, so the name is the one the
replacement renders as wherever it lands. An operation names its method in the group it is placed
in, after any `Remount`, an inbound one its receiver's parse method, a type its declaration, a
property or parameter its field, a member its constant, a variant its wrapper, a group its
sub-client and a service its client. A replacement in another unit is named by a reference the
target can resolve from there, which each target's document spells. A docs target links the
replacement's page through the route plan, by its ID, and names one no route reaches as text, as it
does a span ([routes and links](../docs-and-mock.md#routes-and-links)).

- **No declaration.** An entity the target declares nothing for, such as a channel on an HTTP
  target, an operation it covers no binding of ([bindings](../plan/bindings.md)) or a server or
  scheme it renders no name for, is named by its source name as text, never as a link that resolves
  to nothing: Go keeps an unresolved `[Zone.Missing]` as bracketed text.
- **Itself deprecated.** The replacement is named as the IR gives it, and its own deprecation names
  its own. No target follows the chain, so two deprecations naming each other, which `irverify`
  admits, cost one name each.
- **Another signature.** A replacement whose parameters or result differ is still named, and a
  target that also rewrites a call mechanically, as Kotlin's `ReplaceWith` does, writes one only
  where the rewritten call compiles at every use the deprecated declaration has (below).

A mechanical rewrite is written only where it holds whatever the caller wrote: the deprecation names
one replacement; an operation's is a method of the same client taking the same parameters, by name
and type, in order, and returning the same type; and a property's or parameter's is a field of the
same holder of the same type. Under any other, and for a type or an enum member, the text names the
replacement and nothing rewrites the use. With Kotlin 2.4.0, `getOrder(request: GetOrderRequest)`
rewritten as `getOrderV2(request)` fails to compile, `argument type mismatch`, since each
operation's request type is its own; `val o: Order = client.getOrder(id)` rewritten to a method
returning `OrderV2` fails, `initializer type mismatch`, as does a type's rewrite wherever a caller
declares a value the API returns, `val z: ZoneV2 = client.getZone(id)`; and a closed enum's entry
rewritten in an exhaustive `when` leaves no branch for it, `'when' expression must be exhaustive`.
T-70 holds these rules on every target.

## Rejected

- **A rewrite wherever the names line up.** An expression naming the replacement with the
  deprecated call's arguments compiles for one operation and not its neighbour, and an IDE applying
  one that does not compile leaves the caller a broken build where it had a warning.
- **Following a chain to its last replacement.** It names what the document did not, and a chain
  that loops needs a bound for no gain over naming the next one.
- **Only the message, no replacement.** A replacement the IR states would then reach a caller only
  where a message happens to repeat it, and never as a link that follows a rename.
