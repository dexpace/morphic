# Pagination

A paginated operation is a `PagePlan` ([OpPlan](../plan/operations.md)) and a pager each target
builds from it ([Go](../targets/go/operations.md#pagination),
[Kotlin](../targets/kotlin/operations.md)).

## A paginated operation

`ListUsers`, cursor pagination.

IR: `Operation{ Params:[limit, cursor], Responses:[200 → {items:[]User, next_cursor}],
Pagination{Strategy:cursor, InputCursor:{cursor}, Items:{body→items}, NextCursor:{body→next_cursor}} }`,
bound `HTTPBinding{GET /users, query limit+cursor}`.

`OpPlan{ Return:{Shape:value, Type:ListUsersResponse}, Page:&PagePlan{Strategy:cursor,
ItemType:User, ItemPath:body→items, CursorIn:cursor, CursorOut:next_cursor}
}`: each page decodes as the response's own type, and `Page` takes the items from it. Item-type
unwrap is a plan decision. Pacing delay is **not** here — it is runtime policy.

## What a target generates

`LowerPagination` emits an auto-pager that follows `Page`: for `ListUsers` it reads `next_cursor`
through `CursorOut` and feeds it back through `CursorIn`. Every pager yields each item deserialized
elementwise over `ItemPath`, stops when a fetch fails, and fetches no further page once the caller
stops reading or the walk ends ([below](#where-a-page-walk-ends)). The pager *runtime*, the
fetch-and-pace loop, is boilerplate parameterised by `Policy.Pagination.AutoPageDelay` — the
legitimate boilerplate seam ([structure and boilerplate](../emit/templates.md)).

## Where a page walk ends

A pager fetches no page after one meeting a condition `Page.Stop` lists, each read as the IR states
it ([pagination](../../ir/interaction-patterns.md#pagination)): `empty` and `short` count the items
`ItemPath` reads, `short` against the limit the call sent through `LimitIn`, `no_next` reads
`CursorOut`, and `total` compares the items or pages read so far with what `TotalCount` or
`TotalPages` reads. A condition a page gives nothing to read for, `short` on a call sending no limit
or `total` on a page stating no total, ends nothing there.

Whatever `Stop` lists, the walk also ends at the last page any condition could end it at: where
`CursorOut` is set, a page from which it reads no continuation, absent or null, since no next
request can be built from it, and otherwise, as under `offset` or `page`, a page holding no items.
Every condition a source can state ends the walk there or sooner, so the pager reads no item fewer
than the source's own rule would, and an empty `Stop`, which states no rule, ends there alone: the
pager reads none into the shape.
