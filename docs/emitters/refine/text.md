# Values as text

Over HTTP, a body whose media type is JSON carries a value as JSON, while a path segment, a host
label, a query, header or cookie value, a `text/plain` body, an `application/x-www-form-urlencoded`
field and a `multipart/form-data` part whose content type is `text/plain` carry it as text. OpenAPI
says how `style` and `explode` arrange texts, but leaves how a number, a boolean or a date becomes
one to each implementation (OpenAPI 3.2, Appendix B), and the targets' own conversions disagree: the
JVM's `OffsetDateTime.toString()` writes `2020-01-01T00:00Z`, which RFC 3339 does not admit, and its
`Double.toString()` writes `1.0` and `1.0E21` where Go's json/v2 writes `1` and `1e+21` (JDK 25, Go
1.27). So one rule decides a value's text, both ways, on every target. A message's headers and an
RPC's metadata are their own protocols' to spell ([open question 4](../open-questions.md)).

## The text of a value

A value travels as the text of its wire form, the JSON value its codec writes, a type-table row's
for a leaf type ([type table](../contract/type-table.md#what-a-built-in-row-writes)) and an enum's
own for a member ([enums](enums.md#a-member-on-the-wire)): a string's characters, `true` or `false`,
and a number in plain notation. Plain notation is an optional `-`, the integer part as JSON writes
it, and, only where the value is not whole, a `.` and as few digits as give back the value in its
type: `1`, `0.1`, `1000000000000000000000`, `0.0000001`, and `0.1` for a `float32` `0.1`, not the
`0.10000000149011612` its widening to a double holds. It writes no exponent, no `+`, no trailing
zero and no `-0`, so two numbers JSON compares equal write one text, as an enum's members compare,
and a whole value is written as an integer is. A target never writes a value through a conversion of
its own, a Kotlin value class's or `enum class` entry's `toString()` or Go's `fmt`, which spell no
wire form.

A value its wire form cannot spell has no text, and the call fails before anything is sent, naming
the input: NaN and the infinities, which no JSON number holds, and a year outside 0000 to 9999 or
an offset with seconds, which RFC 3339 cannot write. So does a number whose plain notation would add
more than 6,177 characters to its significant digits, the most a `decimal128` adds, which only an
arbitrary-precision value's exponent passes: `1E+999999999`, twelve characters in a body, would be a
billion digits.

An array or an object is arranged from its elements' and its members' texts as the binding's `Style`
and `Explode` say in OpenAPI's terms (§4.12.3, Appendix C): a model's members in the order
`TypePlan.Flattened` holds them, each under its `WireName`, or the name `WireNameByFormat` gives it
for a form body's media type, and a map's entries in the order of their keys' texts, compared by
UTF-8 bytes, so a Go map's unordered iteration never reaches a request. In a cookie, a `form` style
that explodes a list or an object joins its pairs with `; `, as the `cookie` style does, since the
`&` it writes in a query would leave one cookie holding them all (Appendix D.1). What a list of
objects or of lists sends, which OpenAPI leaves undefined, is
[below](#a-value-no-style-arranges). A member holding its style's
own delimiter where expansion writes it as it writes the delimiter, a `.` under an exploded `label`,
a space under `spaceDelimited`, a `|` under `pipeDelimited` or a `[` in a `deepObject` key, reads
back as that delimiter, which OpenAPI leaves to an escaping of the API's own (Appendix E.6). A
`Prefix` binding, and a response header whose `Prefix` is set, writes each entry of its map, in that
order, as a field of its own, named by the prefix followed by the entry's key, its value the entry's
text, and an entry naming a field another binding writes, in any case for a header, gives way to it,
as Smithy has a client and a server do. Two entries naming one header field, as keys `A` and `a` do,
fail the call before anything is sent, as a reader would hold one of them. An empty prefix on a
request's binding, Smithy's catch-all for every header or query parameter, has no spelling yet
(#1748). A map a `form` or `cookie` style explodes into parameters of its own is a catch-all the
same way: an entry naming a parameter another binding writes gives way to it, and its reader holds
every parameter its location carries, as Smithy's protocol tests have a client and a server treat
its query map. A parameter whose `ContentType` names a media type ([bindings](../plan/bindings.md)),
and a form field or part of another content type, carries that media type's serialization of the
value instead, so as JSON a string keeps its quotes.

## Null and empty

A null has no text. OpenAPI calls one RFC 6570's undefined (§4.12.6), which RFC 6570's expansion
leaves out (§3.2.1), as OpenAPI's own example of an undefined value does (Appendix C.4.3), so a
query, header, cookie or form input holding null is written as an absent one is, and an optional
nullable input's null and its absence send one request. OpenAPI's style table spells an undefined
value as RFC 6570 spells an empty string, `color=` under `form` and `;color` under `matrix`, which
would send a null as `""` is sent. An empty list or object is undefined too (RFC 6570 §2.3), and a
null member of a list or an object is left out of it, since the expansion appends only defined
members (RFC 6570, Appendix A).

A path parameter has no absent form: one holding null, or whose expansion leaves its segment empty,
as an empty string's does under `simple`, would send a path naming another resource, and an empty
last segment one a server ignoring a trailing slash routes to the collection above it. On the golden
spec (spec @bc6fad24, morphic @ea70eba79), 23 `DELETE`s of an item sit under a `DELETE` of their
collection, as `DELETE /accounts/{account_id}/cloudforce-one/rules/{id}` sits under one its summary
calls "Delete all rules". So such a call fails before anything is sent, as one does whose `host`
binding's label holds null or an empty text, or fills a host that is no RFC 3986 host, which
Smithy's endpoint rules require of a client. So does one whose required query, header, cookie or
form input holds null, or a list or an object with no member left: written as absent, it would leave
out what the document requires, as an empty `id` would send `DELETE /zones/{zone_id}/filters` with
no `id` at all, one of the golden spec's 11 required query lists. Under `AllowReserved` a slash in
the text is its binding's own, so an empty segment one leaves beside a segment that is not, as in
`a//b` and `photos/`, is sent as it is, while a text of slashes alone names no segment of its own
and fails as an empty one does.

A query input or a form field that arrives empty, as `page=` does, is present and holds no value
where its binding sets `AllowEmptyValue`, under which a client may send an empty value in place of
leaving the input out ([HTTP bindings](../../ir/http-binding.md)), so it meets a required input's
presence and leaves its value unset. Otherwise it holds the empty text it is, which a string reads
and an integer does not. At the revisions above, the golden spec sets `AllowEmptyValue` on 11 query
bindings.

## Escaping

A text is percent-encoded as OpenAPI says for its location (§4.12.4, Appendix E): as RFC 6570's
expansion for its style encodes it, and a space always as `%20`. Every decoder those rules name
reads `%20` as a space, while the `+` a form encoder writes, as Go's `url.Values` does, reads as a
plus under RFC 3986's rules (Appendix E). A parameter's name is percent-encoded as a text without
`AllowReserved` is, as OpenAPI requires of one outside RFC 6570's variable names (Appendix C.3): at
the revisions above, the golden spec names a query parameter `meta.<field>[<operator>]`, which Go
sends as written, `fetch` with its `<` and `>` encoded, and the JDK's `URI` refuses. `AllowReserved`
passes through, as RFC 6570's reserved expansion does, each percent-encoded triple and each of
RFC 3986's reserved characters, bar those a URL cannot hold there: a `#`, `[` or `]` in a path or a
query, and a `?` in a path, is still encoded, and a `&`, `=`, `+` or `,` it passes is read for what
it means where it lands, as OpenAPI has a caller encode one meant as data (§4.12.4). RFC 3986 ends a
path at a `?` and a query at a `#` (§3), so a path holding `a?b` would reach `a`, and admits a `[`
or `]` in neither, which the JDK's `URI` refuses in a path while Go and `fetch` send it. A `/` still
passes in a path, though OpenAPI forbids one unescaped in a path parameter's value (§4.8.2), as
gRPC's multi-segment variables and Smithy's greedy labels pass it, and as the golden spec's three R2
`object_key` parameters ask at the revisions above.

A `PathPattern` binding whose pattern names more than one segment, one holding a `/` or a `**`,
writes its text as google.api.http has a client write such a variable (`google/api/http.proto`,
"Path template syntax"): every character percent-encoded but a `/` and RFC 3986's unreserved ones
(§2.3), a `%` among them, so a triple in the value is encoded again and `a:b%41?c` travels as
`a%3Ab%2541%3Fc`, each `/` cutting a segment. Smithy writes a greedy label by the same rule
(`httpLabel`, "Serialization rules"), so one is planned to lower to a pattern of `**` ([planned
lowerings](../../compilers/planned-lowerings.md)) and never to `AllowReserved`, which passes the
`%41` and the `?` that Smithy encodes. `AllowReserved` beside such a pattern changes nothing,
since the pattern's format gives the rule, and what `AllowReserved` says of an empty segment and
this section of a dot segment hold for it as for any path parameter. A pattern of one segment, `*`
or a literal, is written as a binding with none is. A reader takes the longest run of segments the
template leaves the parameter ([below](#reading-a-text)), which needs no reading of the pattern,
and decodes each triple but `%2F` and `%2f`, which it keeps as
the three characters they are, as google.api.http has a server do, so every text written reads back
and a `%2F` another client sent is never taken for a separator. A `PathPattern` is carried to a
refiner as `ParamBind.PathPattern` ([bindings](../plan/bindings.md)). No emitter checks a value
against its pattern before it is sent, which the server still does, and each reports the binding as
an info `emitters/<target>/unchecked-constraint`. That check is all an emitter drops: the IR
has one that cannot validate a pattern drop it with a diagnostic ([HTTP
bindings](../../ir/http-binding.md)), and a pattern dropped whole would send `a/b` as `a%2Fb`, one
segment where the pattern names two, so the value is still escaped by this rule. The IR's comment on
the field says the template uses reserved expansion, which this rule departs from and
#TBD-pathpattern corrects. T-50 holds it.

A path parameter whose expansion leaves a segment `.` or `..`, or one WHATWG URL reads as either,
such as `%2e`, fails the call before anything is sent: `fetch` removes it, a `..` with the segment
before it, as RFC 3986's dot-segment removal does (§5.2.4), while Go and the JDK send it as it is,
so one call would send two paths, and `fetch`'s would name the collection above the item.

A path parameter whose text holds the literal its template sets after it in the same segment fails
before anything is sent too, and a reader ends the parameter's text where that literal first follows
it, so every text sent reads back into its parts: at the revisions above, the golden spec's
`{event_t}.{event_n}` binds two `number`s, and `1.5` and `3` would send `1.5.3`, which reads as `1`
and `5.3`.

A header's text, and a cookie's under the `cookie` style, is sent as it is, never percent-encoded
(§4.12.2.2), and OpenAPI leaves escaping it to the caller (Appendix D). One holding a control
character other than a tab, or a space or a tab at either end, which no field value carries
(RFC 9110 §5.5), a character past ASCII, or named by a `Prefix` key that is no field name (§5.1),
fails the call before anything is sent, never trimmed or escaped. So does a list's or an object's
text in a header holding an empty element, which a sender must not write, or one with a space or a
tab at either end, which a reader takes for the whitespace beside a comma (§5.6.1). A CR LF would
begin a header of its own, the HTTP libraries the targets can run on each trim the whitespace at a
text's ends, and they disagree past ASCII: Go sends `é` as its UTF-8 octets and JavaScript's
`fetch` as one ISO-8859-1 octet, while `fetch` and the JDK's `HttpClient` refuse the `€` Go sends
(Go 1.27, Node 26, JDK 25).

A `text/plain` body or part is its text's UTF-8 octets, with no byte-order mark, and is read as
UTF-8 with one leading mark dropped, as RFC 3629 §6 suggests and WHATWG's UTF-8 decode does. One
whose content type names a charset other than UTF-8 or US-ASCII, or whose octets are no UTF-8, does
not decode ([responses](../plan/responses.md#a-body-that-does-not-decode)), and a request whose
binding's media type names another fails before anything is sent. For a Latin-1 `é`, on the same
versions, Go's body holds a byte that is no UTF-8, `fetch`'s `text()` gives U+FFFD and the JDK's
`ofString` gives `é`, and Go and the JDK keep a leading mark that `fetch` drops, so a body of `42`
after one would parse on one target alone. What such a body holding a list or an object sends, which
OpenAPI leaves undefined (Appendix B), is [below](#a-value-no-style-arranges).

## Reading a text

Whatever reads a text reads it by the same rule backwards: an SDK reading a response's header or
`text/plain` body, the mock server reading a request ([docs and mocks](../docs-and-mock.md)), and an
SDK reading a request the API sends it ([inbound
operations](../../ir/http-binding.md#inbound-operations)). A list's or an object's text is split as
its `Style` and `Explode` arrange it before each part is percent-decoded, so a `%2C` stays in its
element (Appendix C.2). A path parameter's text is what its expression in the template matches, read
from the path cut at its unencoded `/`s before anything is percent-decoded, so a `%2F` stays in its
segment: within one segment, ending where a literal after it there first follows, or under
`AllowReserved`, or a `PathPattern` of several segments,
across the longest run of segments the template leaves it, as Smithy matches a
greedy label. A text is percent-decoded as its location's rules say (Appendix E.5): a `+` as a space
in a query or a form body, which OpenAPI has parse as WHATWG URL does (§4.12.4), as itself in a
path, and a header's and a `cookie`-style cookie's not at all (§4.12.2.2). A parameter a query or a
cookie repeats where its `Style` and `Explode` write it once, as a scalar's or an unexploded list's
is written, is a text that does not parse, since no one value writes two. A header's text is every
line it arrived on, joined with `, ` as RFC 9110 §5.3 joins them, bar `Set-Cookie`'s, each of whose
lines is one element since its values hold commas, as OpenAPI's Header Object has it (§4.21.3), and
only a list's or an object's split drops anything, the whitespace beside a comma and an empty
element (RFC 9110 §5.6.1), so a string header keeps its own whitespace. A `Prefix` binding's map,
and a response header's whose `Prefix` is set ([prefix
headers](../../ir/responses.md#prefix-headers)), holds every field whose name starts with the
prefix, a header's in any case (RFC 9110 §5.1), a field another binding reads included, as Smithy's
protocol tests read one, keyed by the rest of the name in lower case, as HTTP/2 carries every field
name (RFC 9113 §8.2) and as `X-Meta-fOO` reaches Go's server as `X-Meta-Foo`, the JDK's as
`X-meta-foo` and Node's as `x-meta-foo`.

Each text is read as the wire form it spells, through its codec as a body's value is, so a text and
a body read a spelling alike. A built-in row reads every spelling its format admits of a value its
type holds ([type table](../contract/type-table.md#what-a-built-in-row-writes)), and a number as its
row reads one in a body, an integer by its value in whatever spelling, `1.0` among them ([type
table](../contract/type-table.md#an-integer-row-reads-a-number-by-its-value)). A union's
text is read as [unions](unions.md#a-value-several-variants-admit) reads a value text carries, which
its variants' JSON kinds never separate, so `123` is admitted by both variants of a
`string | integer`, and an `any` holds a text as the JSON string it is, since a text carries no JSON
kind to read it by. A text its codec does not read is a value that does not parse, and a response
header's leaves its field unset and `RawHeader` holding what arrived
([T-26](../testing/mandatory-tests.md)). At the revisions above, the golden spec declares one
`Last-Modified` header `date-time` and gives an HTTP-date as its example, which the next rule reads.

A header's text read as a `datetime_offset` that no encoding spells is read by that row and, where
the row does not read it, as an HTTP-date, as the `{datetime_offset, "http-date"}` row reads one
([type table](../contract/type-table.md#a-row-keyed-by-an-encoding)): the instant it names, at a
zero offset. The two never read one text, since an RFC 3339 `date-time` opens with a digit and an
HTTP-date with a letter, so a text gains a value only where it had none. That makes it a reading
and no inference of the kind INV6 keeps in policy: it states no fact about the API, changes no
declaration and no request, and gives no text the declared format reads another value. RFC 9110
spells every date
its own fields carry so, `Last-Modified` among them (§8.8.2), and encourages a recipient to be
robust in parsing a timestamp (§5.6.7). Nothing but that reading changes: the field's type stays the
document's, and a `datetime_offset` header an SDK or the mock writes is the RFC 3339 text its
document declares, though RFC 9110 has a server ignore an `If-Modified-Since` or an
`If-Unmodified-Since` that is no HTTP-date (§13.1.3, §13.1.4), which only a compiler lowering those
fields under `http-date` changes; the golden spec's one `If-Modified-Since` is a `string` (spec
@bc6fad24, morphic @fa253fe19). A `date`, a `time` and a `datetime` read none, since an HTTP-date
names an instant and they hold none.

A header list whose elements read an HTTP-date, by that rule or under `http-date`, is split at its
commas as any list is, and then a part that is a day name alone, `Sun` or `Sunday`, takes the part
after it, joined by `, `: an IMF-fixdate and an rfc850-date each hold one comma, after the day name,
an asctime-date holds none (§5.6.7), and no date in any form a header reads is a day name alone. So
`Sun, 06 Nov 1994 08:49:37 GMT, Sun Nov  6 08:49:37 1994, 1994-11-06T08:49:37Z` is three elements,
on one line or on three. In an object only a part read as the value of a member of such a type takes
the next, so a key spelled `Sun`, and a string member holding it, stay whole. The split and each
date's reading are one pass over the field, so a server controls no more than the field's length. At
those revisions the golden spec declares no header list at all. T-49 holds both rules.

## A value no style arranges

A style arranges a leaf, a list of leaves and an object of leaves, and nothing deeper: OpenAPI's
styles take a primitive, an array and an object of primitives (§4.12.3), as RFC 6570's composite
values are a list of strings and an associative array of them (§2.3), and OpenAPI gives nothing
else a text (§4.12.6, §4.12.7). A leaf is a type whose value travels as one string, number or
boolean: a primitive, `any` and the `Any` type among them, an enum, a literal, an `External`, a
scalar over one of these, and a union of them. So whether an input has an arrangement is a fact of
its type, the member's its `ParamPath` names where it names one, read through `TypePlan.Alias` and
its scalar chain, and of where it is bound:

- under a `Style`, a parameter's, a header's or a form field's, it has one where its type is a leaf,
  a list or a tuple of leaves, a model whose `TypePlan.Flattened` members and catch-all are leaves,
  a map of leaves, or a union each of whose variants is one of these, and OpenAPI's table gives that
  shape a text under that `Style` and `Explode` (§4.12.6): it gives a leaf none under
  `spaceDelimited`, `pipeDelimited` or `deepObject`, a list none under `deepObject`, and a list or
  an object none where either delimited style explodes it;
- a form field, or a multipart part that is no file (`Filename`), whose `PartEncoding` is absent
  or sets no `Style`, no `Explode` and no content type has one where its type is a leaf or a list
  of leaves: a field is written as an exploded `form` writes it, and a part is its text with no
  `Content-Type` of its own, which RFC 7578 reads as `text/plain` (§4.4), each element of a list a
  part of that name (`Multi`). An object there has none. OpenAPI gives one a default media type,
  `application/json` (§4.15.1), but the default is OpenAPI's and no other format's, so it is the
  compiler's to lower into `ContentTypes` (#TBD-part-content-type) and never an emitter's to assume
  of a document whose format it does not read (INV1);
- a `Prefix` binding, and a header whose `Prefix` is set, has one where its type is a map of leaves;
- a `host` label and a `text/plain` body or part have one where their type is a leaf.

An input with none is its caller's text, written as the API documents it. Nothing is guessed, since
nothing in the IR says which of the spellings in use a server reads. What the caller writes depends
on what the position carries:

- a path parameter, a `host` label, a header and a `text/plain` body carry one text, so the SDK
  types the input a string and sends it as a string's text is sent, in its segment or its label, as
  its header's value or as the body's octets;
- a query, a cookie, a form body and a multipart body carry named texts, so the SDK types the input
  a list of pairs of strings, a name and a value, and sends each as a parameter, a cookie, a field
  or a part of its own, in the caller's order, its name written as a parameter's is and its value
  as a string's text is there. One string under the binding's name cannot spell what an API
  documents as `deep[a][b]=x` for a `deepObject` object holding an object, as `a=1&b[c]=2` for an
  exploded one, or as `ids[]=1&ids[]=2` for a `deepObject` list: each sends a name the binding does
  not have, or one name twice. A pair with an empty name, or one naming another parameter, field or
  part its location declares, fails the call before anything is sent, and an input holding no pair
  is an empty list ([above](#null-and-empty)).

Whatever reads one reads what arrived, so a mock checks nothing of what it holds: the string, or,
in the order they came, every pair of the location whose name nothing else there declares. The plan
decides it once for every target, as `ParamBind.Text` for a parameter
([bindings](../plan/bindings.md)) and by the same test for a header, a form field, a part and a
content, whose reader's `Type` is then `string` ([body shapes](../plan/responses.md#body-shapes)),
and reports each as a warning `emitters/plan/unarranged-value` at its binding, its header or its
content. A content no election picks is not reported, and no election passes over one. The test is
the content's own, however it is chosen: an inbound request is read through the content its
`Content-Type` names ([operations the API sends](../operations/inbound.md)), so each such content
of its request is reported, and one that arrives is the string it held, as a response a receiver
writes in one is the string it is given. Such an input or content has no example: a declared one
holds its value, not its text, so it is refused as one its type refuses is ([docs and
mocks](../docs-and-mock.md)), and none is synthesized, a text made up for it being a guess, so the
mock answers in such a content with no body and a warning `emitters/mock/example-missing`.

A document that means JSON says so: a parameter declaring `content` with `application/json` in
place of `schema` (§4.12.2.3), as an overlay can make it, lowers to `ContentType`, which types the
input as the list it is and sends its JSON text, and a body does by offering `application/json`,
which the default ranking elects. A leaf's own value can still hold a list or an object, as an
`any`'s and an `External`'s can: one that does has no text, and the call fails before anything is
sent ([above](#the-text-of-a-value)). A type that gains a list or an object where it held leaves
turns its input from typed to text, a break the surface differ reports ([surface
verification](../surface-verification.md)).

At spec @bc6fad24, morphic @fa253fe19, the golden spec has 12 such parameters, each a query list
under an exploded `form` whose elements are objects or lists, three such multipart parts, and no
other parameter, header or form field. The parts are the `metadata` of `POST
/accounts/{account_id}/images/v1`, of `POST /accounts/{account_id}/images/v2/direct_upload` and of
`PUT /zones/{zone_id}/snippets/{snippet_name}`, each an object no Encoding Object names, and they
stay their caller's text until the compiler lowers OpenAPI's default for them
(#TBD-part-content-type), which types each as its object and sends it as JSON. The descriptions of
ten of the parameters call theirs JSON, one array in one parameter, as in
`search=[{"field":"value","op":"in","value":["evil.com","bad.org"]}]`, which a caller's one pair
sends and an overlay declaring `content` types. The other two, the `filters` of `GET` and `DELETE
/accounts/{account_id}/ai-gateway/gateways/{gateway_id}/logs`, say nothing of their text, and a
`DELETE` whose filter the server did not read would delete as one given none. It has two such
bodies, the `text/plain;charset=UTF-8` requests of `POST /accounts/{account_id}/one/integrations`
and its `PATCH`, each an object beside an `application/json` content of the same schema. T-48 holds
the rule.

## Other formats

A compiler lowers the text a format's binding gives a position by default, as Smithy and TypeSpec
give a timestamp in a header an HTTP-date, as an `Encoding` at that position, a header's own or that
of a scalar hoisted to type a parameter ([planned lowerings](../../compilers/planned-lowerings.md)),
under the name the IR documents for it and not the format's own, `http-date` for TypeSpec's
`rfc7231` as for Smithy's `http-date`,
which keys its row as any encoding does: an emitter never reads which format a document came from
(INV1). An encoding with no row of its own types the value as the wire carries it, never in its
primitive's spelling ([type table](../contract/type-table.md#a-row-keyed-by-an-encoding)).

## The designs it was weighed against

- **Each target's own conversion**: one call would send two texts, one of them no RFC 3339 time, as
  above.
- **A number as its codec writes it in a body**: JSON reads `1` and `1.0` as one value, and a server
  parsing text need not. At the revisions above, 171 of the golden spec's 267 `number` query
  parameters have a whole default, `page`, `per_page` and `limit` among them, and the JVM spells a
  `Double` 1 as `1.0`, sending `page=1.0`.
- **ECMAScript's number form**, which `JSON.stringify` and RFC 8785 write: it turns to an exponent
  from 10²¹ and below 10⁻⁶, which an integer parser refuses, and has no spelling for a `decimal`
  past a double's digits, where plain notation is one rule for every numeric kind.
- **A decimal's scale kept**, `0.10` as written: the text would follow the representation, not the
  value JSON compares, and Go's decimal and Kotlin's `BigDecimal` would have to agree on the scale
  of every value a caller computes.
- **Null as `null` or as an empty text**: the first is also the string `"null"`'s text, the second
  the empty string's.
- **Reading only the spelling written**: it refuses `+00:00`, and a fraction with trailing zeros,
  which the JVM's own `OffsetDateTime.toString()` writes (`.500`).
- **Refusing every text outside a format alike**: it would take Go's `uuid.UUID` row away, whose
  parser reads a UUID in braces or with no hyphens, for agreement only on what a server should not
  send.
- **A header's text past ASCII sent as its UTF-8 octets**, as passing a value through unchanged
  would read: RFC 9110 has a recipient treat such octets as opaque and recalls that HTTP once read
  them as ISO-8859-1 (§5.5), as `fetch` still writes `é`, so a server need not read what was meant,
  and `fetch` and the JDK's `HttpClient` refuse `€` outright.
- **A map's entries in the map's own order**: a Go map has none, so one call would send them in a
  different order on each run.
- **`AllowReserved` passing every reserved character, or only a `/`**: the first sends a path
  holding `a?b` to `a`, and the second encodes the percent-encoded triples OpenAPI has a caller
  write before reserved expansion (§4.12.4), sending a key written `100%25` as `100%2525`.
- **A dot segment percent-encoded**: WHATWG URL reads `%2e` as `.`, so `fetch` still removes it.
- **A parameter's text holding its template's next literal sent as it is**, as a member holding its
  style's delimiter is: OpenAPI gives no escaping for it, and a `number` has no spelling of its
  fraction without a `.`.
- **Refusing a segment an `AllowReserved` slash leaves empty**: a path-like key ending in `/` would
  be unreachable, where a dot segment is refused because the client itself rewrites it.
- **A header's text trimmed**, as each target's HTTP library trims it: two texts differing only in
  the whitespace at their ends would send one.
- **A `Prefix` header map keyed as each server reports a name**: one header would key `Foo` on Go
  and `foo` on Node and the JDK, and HTTP/2 keeps no case to report.
- **A `text/plain` body read by the charset it names, or as UTF-8 whatever it names**: the targets
  decode different sets of charsets, and the second reads a Latin-1 `é` as U+FFFD with nothing
  failing.
- **A list of objects sent as JSON**, as a parameter declaring `content` sends one: OpenAPI defines
  no such text for a `schema` parameter, a server reading brackets or a format of its own would be
  sent a filter it does not read, and the golden spec's two undescribed `filters` sit on a `DELETE`.
  Where the document means JSON, declaring `content` says so for that parameter alone.
- **Each object as JSON under the list's own `Style`**, as OpenAPI's Encoding Object defaults a form
  field's array items to `application/json` (§4.15.1): it sends `search={…}&search={…}`, where each
  of the golden spec's ten descriptions asks for one array.
- **Brackets**, `filters[0][key]=id`: OpenAPI leaves such syntaxes out because they "are not
  standards, and often contradict each other" (§4.12.7), so choosing one is a guess too.
- **A policy switch choosing a spelling**: it would turn every such parameter of a document at
  once, the undescribed with the described, where an overlay names the parameters it is true of.
- **The list kept, each element taken as text**: no description on the golden spec gives an element
  a pair of its own, and the type would promise a structure the wire need not have.
- **A `text/plain` body of an object sent as JSON**: it labels JSON as plain text on a guess, where
  the document offering `application/json` already says it.
- **Failing the call, or passing over such a content in the election**: the first fails every call
  for a fact fixed at generation, and the second overrides a ranking its user wrote.
- **A `datetime_offset` header reading only RFC 3339**: the golden spec's one such header would
  never be read, for a text that names its instant exactly.
- **An HTTP-date read by a field's name**, `Last-Modified` and the others RFC 9110 defines: a list
  of names to keep, which leaves a field of an API's own unread, where the texts tell themselves
  apart.
- **A list of HTTP-dates split at every second comma**: an asctime-date and an RFC 3339 text hold
  no comma, so a list mixing forms would pair the wrong parts.
- **Each target's own HTTP-date reader**: at Go 1.27 and JDK 25, the JDK's `RFC_1123_DATE_TIME`
  refuses the golden spec's example, whose day name is not its date's, and writes `6 Nov` with one
  digit, and Go's `http.ParseTime` reads an rfc850-date's year `70` as 1970 at any date.
- **A `PathPattern` text under RFC 6570's reserved expansion**, as the IR's comment on the field
  describes it (#TBD-pathpattern): `a:b%41?c` would travel as written and read as `a:bA` with a
  query of `c`.
- **A Smithy greedy label under `AllowReserved`**: it passes a triple and the reserved characters,
  so `a%41?c` would travel as written where Smithy sends `a%2541%3Fc`.
- **An input no style arranges as one string wherever it is bound**: sent once under its binding's
  name, it reaches a server reading one text there, as the golden spec's ten descriptions ask, and
  can spell no request whose names are not the binding's, `deep[a][b]=x` among them.
- **A string for such a query input and pairs only under `deepObject` or an exploded object**: a
  server may read a list of objects under `form` as `filters[0][key]=id` too, one of the syntaxes
  OpenAPI names as in use (§4.12.7), which one string under `filters` cannot send.
- **An object-valued part with no content type sent as JSON**: right for OpenAPI and a guess for a
  format whose default is another, which an emitter cannot tell apart.
