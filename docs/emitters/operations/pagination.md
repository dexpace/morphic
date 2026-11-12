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

## What a pager sends next

A pager's next request is the one before it with the continuation in place, every other input as
the caller set it, so a filter and a limit hold across the walk:

- **`cursor` and `token`**: the value `CursorOut` read, at `CursorIn`.
- **`offset`**: the offset the last request sent, 0 where it sent none, plus the items its page
  held, at `CursorIn`.
- **`page`**: the number the last request sent plus one, at `CursorIn`. Where the caller sent none,
  the first page is `Page.FirstPage`: the page input's declared default, which is the page a
  request without one gets, else its declared minimum, else `plan.Policy.FirstPage`, 1 unless
  configured, marked `Inferred`. With that unset, `FirstPage` is nil and a pager whose caller sent
  no number reads the first page alone, which the plan reports once as a warning
  `emitters/plan/first-page-unknown`. Of the golden spec's 254 operations taking a `page` query
  parameter (spec @bc6fad24, morphic @fa253fe19), 199 declare a default, each 1, 12 only a minimum,
  9 of them 0, and 43 neither.
- **`next_link` and `link_header`**: a `GET` of the link, resolved against the URL of the page that
  gave it (RFC 3986 §5), which carries the inputs itself, sent with credentials only where a
  redirect to it would carry them ([runtime policy](../contract/runtime-policy.md)), since the
  server chose where it points.

The mock answers each of these as the page after ([a page walk on the
mock](../docs-and-mock.md#a-page-walk-on-the-mock)), which T-83 holds.

## Where a page walk ends

A pager fetches no page after one meeting a condition `Page.Stop` lists, each read as the IR states
it ([pagination](../../ir/interaction-patterns.md#pagination)): `empty` and `short` count the items
`ItemPath` reads, `short` against the limit the call sent through `LimitIn`, `no_next` reads the
page's continuation, and `total` compares the items or pages read so far with what `TotalCount` or
`TotalPages` reads. A condition a page gives nothing to read for, `short` on a call sending no limit
or `total` on a page stating no total, ends nothing there.

Whatever `Stop` lists, the walk also ends at the last page any condition could end it at. Under a
strategy reading its continuation from a page, every one but `offset` and `page`, that is a page
giving none, absent or null, since no next request can be built from it: `CursorOut` reads it, and
under `link_header` the `Link` header's `next` link is it. A link that resolves to the URL of the
page giving it, as an empty one does (RFC 3986 §5.2), is none too, since following it asks for that
page again. Under `offset` or `page` it is a page
holding no items. Every condition a source can state ends the walk there or sooner, so the pager
reads no item fewer than the source's own rule would, and an empty `Stop`, which states no rule,
ends there alone: the pager reads none into the shape.
