# Stability

`Operation.Stability` is how far a source commits to an operation, and `OperationGroup.Stability`
the tier of every member stating none, nearest group first, never copied down
([stability](../../ir/operations.md#stability)). A tier shapes how an SDK presents a call and
nothing it sends: an operation of any tier is called alike.

The plan resolves it once, `OpPlan.Stability` ([OpPlan](../plan/operations.md)): the operation's own
tier, else the nearest group's that states one, else none. An unstated tier is never read as
`stable`, so nothing renders a badge or an annotation for it.

What a tier lowers to is each target's, with one floor every target keeps: an `alpha` or `beta`
operation's documentation says its tier and what the tier means, in the generated code and in the
docs, which badge every stated tier, `stable` included. A target that can make a caller acknowledge
a tier may ([Go](../targets/go/operations.md#stability),
[Kotlin](../targets/kotlin/operations.md#stability)). Each method an operation lowers to carries
it, a `WithResponse` method ([return shapes](returns.md)) as much as the one returning the value,
since either calls the operation. No target leaves an `alpha` operation out: hiding operations is
the filter pass's (architecture.md §2.2), so the SDK and its docs hold the same ones.
