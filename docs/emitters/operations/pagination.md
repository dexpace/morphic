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
