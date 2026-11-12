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
UTF-8 bytes, so a Go map's unordered iteration never reaches a request. What a list of objects or of
lists sends, which OpenAPI leaves undefined, is #1731's. A `Prefix` binding writes each entry of its
map, in that order, as a field of its own, named by the prefix followed by the entry's key, its
value the entry's text, and an entry naming a field another binding writes gives way to it, as
Smithy's rules have a client do. An empty prefix, Smithy's catch-all for every header or query
parameter, has no spelling yet (#1748). A parameter whose `ContentType` names a media type
([bindings](../plan/bindings.md)), and a form field or part of another content type, carries that
media type's serialization of the value instead, so as JSON a string keeps its quotes.

## Null and empty

A null has no text. OpenAPI's style examples read it as RFC 6570's undefined (§4.12.6), so a query,
header, cookie or form input holding null is written as an absent one is, and an optional nullable
input's null and its absence send one request. An empty list or object is undefined too (RFC 6570
§2.3), and a null member of a list or an object is left out of it, since the expansion appends only
defined members (RFC 6570, Appendix A). A path parameter has no absent form: one holding
null, or whose expansion leaves its segment empty, as an empty string's does under `simple`, would
send a path naming another resource, and an empty last segment one a server ignoring a trailing
slash routes to the collection above it. On the golden spec (spec @bc6fad24, morphic @ea70eba79),
23 `DELETE`s of an item sit under a `DELETE` of their collection, as
`DELETE /accounts/{account_id}/cloudforce-one/rules/{id}` sits under one its summary calls "Delete
all rules". So such a call fails before anything is sent, as one does whose `host` binding's label
holds null or an empty text, or fills a host that is no RFC 3986 host, which Smithy's endpoint rules
require of a client.

## Escaping

A text is percent-encoded as OpenAPI says for its location (§4.12.4, Appendix E): as RFC 6570's
expansion for its style encodes it, and a space always as `%20`. Every decoder those rules name
reads `%20` as a space, while the `+` a form encoder writes, as Go's `url.Values` does, reads as a
plus under RFC 3986's rules (Appendix E). `AllowReserved` passes through, as RFC 6570's reserved
expansion does, each percent-encoded triple and each of RFC 3986's reserved characters, bar those a
URL cannot hold there: a `#`, `[` or `]` in a path or a query, and a `?` in a path, is still
encoded. RFC 3986 ends a path at a `?` and a query at a `#` (§3), so a path holding `a?b` would
reach `a`, and admits a `[` or `]` in neither, which the JDK's `URI` refuses in a path while Go and
`fetch` send it. A `/` still passes in a path, though OpenAPI forbids one unescaped in a path
parameter's value (§4.8.2), as gRPC's multi-segment variables and Smithy's greedy labels pass it,
and as the golden spec's three R2 `object_key` parameters ask at the revisions above. What a
`PathPattern` binding's text sends, which gRPC percent-encodes but for a `/` and RFC 3986's
unreserved characters, is #1755's.

A path parameter whose expansion leaves a segment `.` or `..`, or one WHATWG URL reads as either,
such as `%2e`, fails the call before anything is sent: `fetch` removes it, a `..` with the segment
before it, as RFC 3986's dot-segment removal does (§5.2.4), while Go and the JDK send it as it is,
so one call would send two paths, and `fetch`'s would name the collection above the item.

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

## Reading a text

Whatever reads a text reads it by the same rule backwards: an SDK reading a response header, the
mock server reading a request ([docs and mocks](../docs-and-mock.md)), and an SDK reading a request
the API sends it ([inbound operations](../../ir/http-binding.md#inbound-operations)). A text is
percent-decoded as its location's rules say (Appendix E.5), a header's and a `cookie`-style cookie's
not at all (§4.12.2.2), and a list's or an object's is split as its `Style` and `Explode` arrange
it. A header's text is every line it arrived on, joined with `, ` as RFC 9110 §5.3 joins them, and
only a list's or an object's split drops anything, the whitespace beside a comma and an empty
element (§5.6.1), so a string header keeps its own whitespace. A `Prefix` binding's map holds every
field whose name starts with the prefix and that no other binding reads, a header's in any case
(RFC 9110 §5.1), keyed by the rest of the name. A response's headers hold no such binding yet
(#1745).

Each text is read as the wire form it spells, through its codec as a body's value is, so a text and
a body read a spelling alike. A built-in row reads every spelling its format admits of a value its
type holds ([type table](../contract/type-table.md#what-a-built-in-row-writes)), and a number as its
row reads one in a body, whose integral spellings, `1.0` for an integer, #1451 decides. A text its
codec does not read is a value that does not parse, and a response header's leaves its field unset
and `RawHeader` holding what arrived ([T-26](../testing/mandatory-tests.md)). At the revisions
above, the golden spec declares one `Last-Modified` header `date-time` and gives an HTTP-date as its
example, so that field stays unset; whether such a header reads an HTTP-date is #1732's.

## Other formats

A compiler lowers the text a format's binding gives a position by default, as Smithy and TypeSpec
give a timestamp in a header an HTTP-date, as an `Encoding` at that position, a header's own or that
of a scalar hoisted to type a parameter ([planned lowerings](../../compilers/planned-lowerings.md)),
which keys its row as any encoding does: an emitter never reads which format a document came from
(INV1). What an encoding with no row of its own writes is #1733's.

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
- **A header's text trimmed**, as each target's HTTP library trims it: two texts differing only in
  the whitespace at their ends would send one.
