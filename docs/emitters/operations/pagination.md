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
stops reading. The pager *runtime*, the fetch-and-pace loop, is boilerplate parameterised by
`Policy.Pagination.AutoPageDelay` — the legitimate boilerplate seam ([structure and
boilerplate](../emit/templates.md)).
