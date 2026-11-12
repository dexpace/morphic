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
  the first page is `Page.FirstPage`: the page input's declared default, which is the page a request
  without one gets, else `plan.Policy.FirstPage`, 1 unless configured, marked `Inferred` ([plan
  policy](../plan/policy.md#a-page-walks-first-page)). A declared minimum is not read, since it
  bounds what the API admits rather than where its pages start. With the policy unset, an operation
  whose page input declares no default has no `Page` and is lowered as a plain method, which the
  plan reports once as a warning `emitters/plan/first-page-unknown`. The number is an integer, sent
  as the input's type holds one: itself where that is an integer or a number, and its decimal text
  where it is a string; an input of another type, or a default that is neither, gives no first page,
  and is planned as with the policy unset. The golden spec has 254 `GET`s taking a query parameter
  named `page` (spec @bc6fad24, morphic @fa253fe19): 202 declare a default, 200 the number 1 and 2
  the string `"1"`, 4 of them beside an inclusive minimum of 0, and 52 none, 7 of these only a
  minimum, 0 in three; 5 of the 254 are strings. Two `POST`s take one too, each defaulting to 1.
- **`next_link` and `link_header`**: a `GET` of the link, whatever the operation's method, resolved
  against the URL of the page that gave it (RFC 3986 §5), which carries the inputs itself, so no
  body is sent and each header input the caller set is sent again, since a link carries none. It
  is sent with credentials only where a redirect to it would carry them ([runtime
  policy](../contract/runtime-policy.md)), since the server chose where it points.

The mock answers each of these as the page after ([a page walk on the
mock](../docs-and-mock.md#a-page-walk-on-the-mock)), which T-67 holds.

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
under `link_header` the `Link` header's `next` link is it. A continuation that would send the
request just sent is none too, since it asks for that page again: under `cursor` and `token` one
that encodes at `CursorIn` as the value the request sent there, and under a link one that resolves
(RFC 3986 §5.2) to the URL of the page giving it, as an empty one does, the two compared after the
syntax-based normalization of RFC 3986 §6.2.2, which changes no URL's meaning. Under `offset` or
`page` it is a page holding no items. Every condition a source can state ends the walk there or
sooner, so the pager reads no item fewer than the source's own rule would, and an empty `Stop`,
which states no rule, ends there alone: the pager reads none into the shape. T-45 holds this
section.
