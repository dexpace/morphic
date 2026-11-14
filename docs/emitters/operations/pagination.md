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

A pager's next request is the one before it with the continuation in place, every other input as the
caller set it, so a filter and a limit hold across the walk:

- **`cursor` and `token`**: the value `CursorOut` read, at `CursorIn`.
- **`offset`**: the offset the last request sent, 0 where it sent none, plus the items its page
  held, at `CursorIn`.
- **`page`**: the number the last request sent plus one, at `CursorIn`. Where the caller sent none,
  the first page is `Page.FirstPage`: the page input's declared default, which is the page a request
  without one gets, else `plan.Policy.FirstPage`, 1 by default, marked `Inferred` ([plan
  policy](../plan/policy.md#a-page-walks-first-page)). A declared minimum is not read, since it
  bounds what the API admits rather than where its pages start. With the policy set to null, an
  operation whose page input declares no default has no `Page` and is lowered as a plain method,
  which the plan reports once as a warning `emitters/plan/first-page-unknown`. The number is sent as
  [below](#the-numbers-a-walk-reads-and-sends); an input of a type holding no integer, or a default
  that is none, gives no first page, and is planned as with the policy null. The golden spec has 254
  `GET`s taking a query parameter named `page` (spec @bc6fad24, morphic @fa253fe19): 202 declare a
  default, 200 the number 1 and 2 the string `"1"`, 4 of them beside an inclusive minimum of 0, and
  52 none, 7 of these only a minimum, 0 in three; 5 of the 254 are strings. Two `POST`s take one
  too, each defaulting to 1.
- **`next_link` and `link_header`**: a `GET` of the link, whatever the operation's method, resolved
  against the URL of the page that gave it as a redirect's `Location` is ([runtime
  policy](../contract/runtime-policy.md#the-canonical-vocabulary)). The link carries the inputs
  itself, so no body is sent and each header input the caller set is sent again, since a link
  carries none. It is sent with credentials only where a redirect to it would carry them ([runtime
  policy](../contract/runtime-policy.md)), since the server chose where it points.

The mock answers each of these as the page after ([a page walk on the
mock](../docs-and-mock.md#a-page-walk-on-the-mock)), which T-67 holds.

### The numbers a walk reads and sends

A page number, an offset, a limit and a total are integers, each read and sent in its own input's or
member's type: an `integer` or a `number` as a JSON number, or as its decimal text where a query or
a header carries it, and a `string` as its decimal text, an optional `-` and digits. The golden
spec's 37 `GET`s taking an `offset` query parameter beside a `limit` or `per_page` type 4 offsets
and 4 limits as strings and 6 and 8 as `number`s, which admit a fraction (spec @bc6fad24, morphic
@fa253fe19). A value read that is no integer, such as `2.5` or `"ten"`, or one below 0, is none: a
total so read is a page stating no total, at which `total` ends nothing. A pager adds and compares
at full precision, never through a fixed-width integer or a float, so a total no target integer
holds still reads, and a next offset or page number past what the type its input lowers to holds,
`int32` under that `format`, fails the walk naming the input rather than send a value that wrapped;
a caller's own offset or page number that is no integer fails the walk the same way when the next
request is built. A pager sends nothing below what the caller or the first page set: an offset
starts at the caller's or 0 and grows, so no declared minimum is read. T-45 holds this section.

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
under `link_header` the `Link` header's `next` link is it. Under `offset` or `page` it is a page
holding no items. Every condition a source can state ends the walk there or sooner, so the pager
reads no item fewer than the source's own rule would.

A continuation that would send the request just sent repeats: under `cursor` and `token` one that
encodes at `CursorIn` as the value the request sent there, and under a link one that resolves (RFC
3986 §5.2) to the URL of the page giving it, as an empty one does, the two compared after the
syntax-based normalization of RFC 3986 §6.2.2, which changes no URL's meaning. A pager follows a
repeat while its page brings items, whatever `Stop` lists, since a cursor naming state the server
holds repeats while its pages advance: against such a server, 6 items in 3 pages and then an empty
one, a pager ending on the first repeat read 4 items in 2 requests and reported the walk complete,
under an empty `Stop` as under `[empty]`, and one following it read all 6 in 4. A repeat ends the
walk only where `Stop` is empty and the page holds no items: no rule then says where the walk ends,
and a page that brings nothing and asks for itself again would be fetched until the bound below.
Where `Stop` lists a condition, the pager follows every repeat and waits for the condition. A server
that repeats and never ends a walk so, as one handing back the cursor it was sent beside the same
items does, meets the bound and fails the walk, unless `total` ends it: the items it hands back
again count toward the total, so one stating a total of 6 beside the items 0 and 1 ends a walk under
`[total]` after three pages holding `0 1 0 1 0 1`, which no rule here tells from a collection
holding them so. T-45 holds this section.

A server whose continuations never end, whether they differ or repeat, meets none of these, so a
walk is bounded as well: it fetches at most the runtime policy's `Pagination.MaxPages` pages and
then fails, never ending as a walk that reached its end does ([runtime
policy](../contract/runtime-policy.md)). T-37 holds the bound.
