# The type table

Which target type a primitive becomes is the setting a generator's users override most, and every
row of it carries an import and a dependency beside the type, which drift apart wherever they are
written separately. So the table has one shape, one lookup and one merge, shared by every target in
`emitters/typemap`, rather than a field of each target's options spelled its own way.

```go
package typemap

// Table maps the IR's leaf types to one target's types. A composite key is a map in a map (../plan/overview.md).
type Table struct {
    Prims     map[ir.PrimKind]map[string]Row // by primitive, then encoding name; "" = the default
    Externals map[string]Row                 // by ir.External.Identity
}

type Row struct {
    Type       string            // the type as the target spells it in a file
    Codec      string            // Kotlin: the KSerializer that reads and writes it, "" = the
                                 // type's own; Go: always "", a type codes itself (../targets/go/types.md)
    Imports    map[string]string // each imported name Type and Codec spell — a Go package's, a
                                 // Kotlin class's — → what the file imports for it; empty = none
    Dependency *Dependency       // what the SDK's build requires for it; nil = the standard library
                                 // or the SDK's own runtime
}

type Dependency struct {
    Module     string      // a Go module path, a Maven coordinate
    MinVersion string      // the lowest version the SDK is generated against
}
```

- **The key is the IR's, never a source format's.** A reference resolves to a key through the plan's
  scalar chain ([TypePlan](../plan/types.md)): the primitive at its root, and the name of the
  encoding declared nearest the reference — a property's own `Encoding`, which overrides its type's
  ([constraints](../../ir/constraints.md#encodings)), then `ScalarChain.Encoding` — or `""` where
  none is. A compiler already gives every format it recognizes a primitive of its own — OpenAPI's
  `date-time` is `datetime_offset` — so a key spells only what the IR does not, such as
  `{string, "email"}` or `{bytes, "base64"}`, compared exactly as written. An enum's `ValueType`
  resolves to its primitive's default row, an `Any` node to the `any` primitive's, and an `External`
  to its `Identity`. A property whose own encoding changes the key is typed by that key's row, not
  by the declaration its reference names.
- **Lookup.** `{prim, encoding}`, then the default row of the primitive the value travels as: the
  one its encoding's `WireType` names, else `string` where `prim` is a string-spelled kind
  ([below](#what-a-built-in-row-writes)), whose encoding names another spelling of it, else `prim`
  itself. So an encoding with no row of its own stays in the IR and the SDK types the value as the
  wire carries it, never writing it in a spelling the encoding did not name ([a row keyed by an
  encoding](#a-row-keyed-by-an-encoding)). It is reported once per key as
  `emitters/<target>/unmapped-encoding` at the first type in ID order that uses it, a warning where
  that primitive is not `prim` and an info where it is. An `External` with no row takes the opaque
  strategy of [external types](../refine/scalars.md), with a warning.
- **Built-in rows are defaults of an options version.** Every target's built-in table has a row for
  every `PrimKind` at every options version, so the fallback always lands, and for every key [a row
  keyed by an encoding](#a-row-keyed-by-an-encoding) lists; T-17 holds each table to the kinds `ir`
  declares and to those keys. Each target's lowering table lists version 1's rows
  ([Go](../targets/go/lowering.md), [Kotlin](../targets/kotlin/lowering.md)). Changing one changes
  what existing SDKs generate, so it is a new options version ([target options](options.md)). A
  built-in row that gives an arbitrary-precision primitive a width — `integer` as a 64-bit integer,
  `number` as a double — is a heuristic, and the nodes it types carry `Inferred` provenance (INV6);
  a configuration that wants every digit replaces those rows.
- **Merge.** A target's options carry the user's rows as a `typemap.Table` the shared package
  decodes strictly. A user row replaces the built-in row under the same key, a primitive's default
  included, and replaces it whole, never field by field, so a type, its codec, its imports and its
  dependency come from one source. A row under a `PrimKind` that `ir` does not declare, an empty
  `Type`, a `Dependency` without both fields, a spelling the target cannot parse, a name `Type` or
  `Codec` imports that `Imports` does not bind, and a key of `Imports` neither spells are options
  errors.
- **Where it lives.** The target's `DecodeOptions` builds the merged table — the stated version's
  built-in rows under the user's — and refine reads it as `Ctx.Types` ([the refiner
  contract](../refine/pipeline.md)). It is not an `EmitterInput` field beside `Policy`, because
  every row is one target's: a Go type means nothing to Kotlin, and options already give a
  per-target setting a strict decode, a version and defaults per version, which a separate input
  would need all over again. The naming tables live in options for the same reason
  ([naming](../refine/naming.md)).
- **What a mapped type must do.** A type some built-in row names is one the target knows: it writes
  that type's literals, checks its constraints and knows whether it has methods. Any other type is
  known only through its row, and how it codes is the target's ([Go](../targets/go/types.md),
  [Kotlin](../targets/kotlin/types.md)). A literal of a mapped type (a default, a constant, an enum
  value) is built by decoding the value's wire form through its codec, an enum's only when the
  value is read, since an enum the target's constants cannot hold is declared over its members'
  wire forms ([enums](../refine/enums.md#a-member-on-the-wire)). A constraint on a mapped type is
  not checked by the SDK, which the server still checks, reported once per key as an info
  `emitters/<target>/unchecked-constraint`. A row's codec goes with its type, so a declaration
  standing over the type keeps it by naming that type again rather than defining a new one
  ([scalars](../refine/scalars.md)), and a type only a user's row names is taken to have methods and
  no constants.
- **Imports and dependencies.** `ComputeImports` adds a row's `Imports` to every file that spells
  its type, and the dependency-manifest artifact requires each `Dependency.Module` a row in use
  names, at the highest `MinVersion` those rows name, compared as the target's build tool compares
  versions.

`Type`, `Codec` and `Imports` are written as the target writes them in a file
([Go](../targets/go/types.md), [Kotlin](../targets/kotlin/types.md)). Decoding parses `Type` and
`Codec` into the target's AST with each imported name bound through `Imports`, so a node holds what
it imports and never the name a row spelled it by, and what a file calls each import is settled with
every other name ([reserved names](../refine/reserved-names.md)): two rows that spell `decimal` for
two packages, or a row's package and a unit of the SDK's own, can meet in one file. A built-in row
may name a type of the SDK's runtime, which the target imports from the runtime's unit ([output
units](../refine/output-units.md#the-runtimes-unit)).

## An integer row reads a number by its value

A built-in row for an integer kind, `integer` and `int8` to `uint64`, reads a JSON number by its
value, in whatever spelling: `1`, `1.0`, `1e0` and `10e-1` are one value, and `-0` is zero. It
refuses, with a decode error, a number whose value has a fraction (`1.5`, `1e-400`), one outside the
row's range (`256` for a `uint8`, `9223372036854775808` and `1e400` for an `int64`) and anything
that is no number, a string of digits among them, which only a declared encoding reads. It writes
plain digits. A declaration over such a row reads the same way, as an enum over one already compares
its members ([a member on the wire](../refine/enums.md#a-member-on-the-wire)), and so does every
text position, a parameter, a header and a form field, in an SDK and in a mock ([values as
text](../refine/text.md#reading-a-text)): `120.0` in a `Retry-After` typed `integer` is 120.

A JSON number is its value and not its digits, which is how JSON Schema counts `1.0` an integer and
how the plan already compares values when it separates a union's variants
([TypePlan](../plan/types.md#variants-that-can-share-a-value)). Producers write these spellings for
whole numbers held as floats: Python's and Ruby's JSON encoders write `1.0` and `1e+16`, and
JavaScript's `1e+21`. Neither first target's library reads them unaided, and the two disagree (Go
1.27.3; kotlinx-serialization-json 1.11.0 on Kotlin 2.4.0):

| JSON | json/v2 into `int64` | kotlinx into `Long` | this rule |
|---|---|---|---|
| `1.0` | refused | refused | 1 |
| `1e0`, `10e-1` | refused | 1 | 1 |
| `-0` | 0, refused into `uint8` | 0, refused into `UByte` | 0 |
| `1.5` | refused | refused | refused |
| `1e-400` | refused | 0 | refused |
| `1e400`, `9223372036854775808` | refused | refused | refused |
| `"1"` | refused | 1 | refused |

Refusing every spelling but plain digits on every target was the alternative. It costs Go nothing,
and it fails a whole response over how a server spelled a count, leaves an `integer` enum reading
`1.0` beside a plain field that refuses it, and still needs a serializer on Kotlin to stop `1e-400`
reading as zero.

The reader decides on the literal's text, in time linear in its length, and builds no number wider
than the row: it compares the digits with the exponent, so `1e1000000000` is refused as out of range
without being expanded, and a literal a megabyte long is read in one pass. A row a configuration
maps to an exact type reads through that type's own codec, which the SDK does not wrap: Go's
`*big.Int` reads plain digits alone, and a codec that expands an exponent bounds it itself, since
thirteen bytes can name a billion digits. `number`, `float` and `decimal` rows read every spelling
already. Each target says how its rows read ([Go](../targets/go/types.md#integers),
[Kotlin](../targets/kotlin/types.md#integers)), and T-61 holds the rule.

## A map's key is text

A JSON object's member name is a string, so a key that `MapT.Key` or a catch-all's `Key` types
travels as [the text of its value](../refine/text.md#the-text-of-a-value) and is read as [a text
is](../refine/text.md#reading-a-text), whatever its row does with a value in a body: an integer key
1 is the name `"1"` and reads back from `"1"`, `"1.0"` and `"1e0"`, a boolean's is `"true"`, an
enum's its member's wire text, a number's or a string's included, and a date's its string, on every
target, in an SDK and in a mock. Two names of one object that read as one key are refused as a
duplicate name is. A key whose type has no text, a model's, a list's, a map's or an `any`, stays the
diagnosed degrade it was.

Neither first target's library does this unaided (Go 1.27.3; kotlinx-serialization-json 1.11.0 on
Kotlin/Native 2.4.0). json/v2 writes and reads an integer key in plain digits, refuses a `bool` key
both ways (`object member name must be a string`), and hands a registered reader, or a defined
type's own codec, the name as a string, which the integer reader above refuses as it refuses any
string: `map[int64]string` read `{"1":"a"}` with json/v2 alone and failed under the reader (`cannot
unmarshal into Go *int64 within "/1"`). kotlinx quotes a `Long` and a `Boolean` key, refuses the
name `"1.0"`, and a serializer reading through `decodeJsonElement()` sees a name as it sees a string
value, so the by-value serializer and an integer enum's refused the key `"1"`. Each target says how
it reads a name ([Go](../targets/go/types.md#integers),
[Kotlin](../targets/kotlin/types.md#integers)), and how a model holds a catch-all whose `Key` is set
([Go](../targets/go/models.md#user-in-go), [Kotlin](../targets/kotlin/models.md#unknown-members)),
since json/v2 embeds no map whose keys are not strings. Protobuf's `map<int64, V>` and
`map<bool, V>` write such keys in JSON, and an Erlang map type pairing typed keys with named
ones is planned as such a catch-all ([planned
lowerings](../../compilers/planned-lowerings.md#erlangotp));
the golden spec types no key (spec @bc6fad24, morphic @fa253fe19). T-61 holds the rule.

## What a built-in row writes

A built-in row's codec writes each string-spelled value in one spelling, in a body and as text alike
([values as text](../refine/text.md)), and reads every spelling its format admits of a value its
type holds, so two targets' requests for a call hold one text at each position and values JSON
compares equal in a body. A string-spelled kind's spellings:

| Kind | Writes | Reads besides |
|---|---|---|
| `url` | its text as held | — |
| `bytes` | RFC 4648 §4 base64, padded | — |
| `uuid` | RFC 9562's hex-and-dash form, lower case | upper case |
| `date` | RFC 3339 `full-date` | — |
| `time` | `partial-time`: seconds always, and a fraction only where the value has one, in as few digits as it needs | a fraction of any length, one finer than its type's precision cut to it |
| `datetime` | `full-date`, `T`, `partial-time` | as `time` |
| `datetime_offset` | RFC 3339 `date-time`: `datetime`'s, then the value's offset as `+hh:mm` or `-hh:mm`, `Z` where it is zero | as `time`, and `+00:00` or `-00:00` as `Z` |
| `duration` | ISO 8601's: `P`, then each component the value holds in ISO 8601's order, each designator upper case, seconds' fraction as `time`'s, and `PT0S` for zero | — |

`T` and `Z` are upper case in the format these rows write and read, as RFC 3339 §5.6 lets a
specification require, so a row need not read them lower case. `bool` and `string` write their JSON
values, `any` the JSON it holds, and a numeric kind a JSON number of its value in a body, which JSON
reads by value in any spelling, and plain notation as text ([values as
text](../refine/text.md#the-text-of-a-value)). A value its format cannot spell, a year outside 0000
to 9999 or an offset with seconds, is one the row refuses to write, in a body as in text, so the
call fails before anything is sent. A fraction finer than its type's precision is cut rather than
refused, as Go's `time.Time` reads one, while a value its type cannot hold at all, such as a leap
second, is one the row refuses to read, and a text its format does not admit one it may refuse or
read as its codec does.

## A row keyed by an encoding

An encoding names how a value travels where that is not its primitive's own spelling
([encodings](../../ir/constraints.md#encodings)), so a row keyed by one writes and reads that
spelling, in a body and as text alike. Every target's built-in table has these rows, each typing the
value as its primitive's default row does unless the target's lowering table says otherwise:

| Key | Writes | Reads besides |
|---|---|---|
| `{bytes, "base64"}` | as `bytes` | — |
| `{bytes, "base64url"}` | RFC 4648 §5 base64, the URL and filename safe alphabet, padded | unpadded |
| `{datetime_offset, "rfc3339"}` | as `datetime_offset` | as `datetime_offset` |
| `{duration, "iso8601"}` | as `duration` | — |
| `{datetime_offset, "http-date"}` | RFC 9110's IMF-fixdate (§5.6.7) of the value's instant: `Sun, 06 Nov 1994 08:49:37 GMT` | an rfc850-date and an asctime-date |

- **`base64url`.** RFC 4648 has an encoder pad unless the specification naming it says otherwise
  (§3.2), which neither JSON Schema's `contentEncoding` nor the IR does, and lets the pad be skipped
  where the length is known (§5), so the row writes it and reads a text with or without it. In a
  path or a query the `=` is percent-encoded as any text's is ([values as
  text](../refine/text.md#escaping)). The targets' own decoders disagree here: Go's
  `base64.URLEncoding` refuses an unpadded text that the JDK's `Base64.getUrlDecoder` and this row
  read (Go 1.27, JDK 25). The row reads nothing else: a text holding a character outside the
  alphabet, a line break among them, which RFC 4648 has a decoder reject (§3.3), or pad bits that
  are not zero, which no conforming encoder writes (§3.5), as `-_9=` beside `-_8=`, does not parse.
  The decoders disagree on both: Go's `URLEncoding` reads each, the JDK's the second alone, and
  Kotlin's `Base64.UrlSafe` neither (Go 1.27, JDK 25, Kotlin 2.4.0). Go's reader for this row is the
  runtime's own already, so refusing them costs it a check, where the `bytes` row's reading of the
  same two stays its codec's ([above](#what-a-built-in-row-writes)): Go's json/v2 reads `+/9=` into
  a `[]byte` and refuses a line break.
- **`http-date`.** The row writes the instant in GMT with the fraction of its second cut, since the
  format holds neither an offset nor a fraction and a caller's clock reading has both, so two values
  of one instant write one text and read back at a zero offset. An instant whose year in GMT is
  outside 0000 to 9999 is one it refuses to write. It reads the three forms RFC 9110 has a recipient
  accept, each name in the case RFC 9110 writes it, an HTTP-date being case sensitive (§5.6.7), so
  `sun, 06 nov 1994 08:49:37 gmt` is no date: the day name for its spelling alone, never compared
  with the date, and an rfc850-date's two-digit year as the latest year ending in those digits that
  leaves the date no more than 50 years after the reader's clock, 2105 for `05` read in 2099, so
  that reading alone depends on when it is made, and a date that year does not hold is none, as
  `29-Feb-00` read in 2060, whose year is then 2100, since the year 2000 that holds it lies outside
  the hundred years the rule reads. Whether a date is more than 50 years after is decided on its
  fields, its year less 50 and then its month, day and time against the clock's in GMT, never by
  adding years to the clock, which Go's `AddDate` and the JDK's `plusYears` do differently on 29
  February (Go 1.27, JDK 25). The reader takes the clock's instant as an argument, which the codec
  fills from the system clock, so a test reads at an instant it names. On the golden spec (spec
  @bc6fad24), the example of the `Last-Modified` header declared `date-time`, `Wed, 21 Oct 2023
  07:28:00 GMT`, names a Saturday. The targets' own readers disagree on each point: the JDK's
  `RFC_1123_DATE_TIME` refuses that example and writes the sixth as `6 Nov`, which is no
  IMF-fixdate, and Go's `http.ParseTime` reads it and reads an rfc850-date's `70` as 1970 at any
  date (Go 1.27, JDK 25). A header list of such dates splits around the comma each holds ([values as
  text](../refine/text.md#reading-a-text)).
- **No row.** The IR's `unixTimestamp` and `seconds` have none yet: TypeSpec writes whole seconds in
  an integer type and Smithy a number with a fraction, no compiler lowers either, and what each puts
  in `WireType` decides what a row writes. Until one does, the lookup above types such a value as
  its `WireType`, a `datetime_offset` under `unixTimestamp` as the `int64` of seconds its caller
  fills, which is written right. A value so typed is checked against none of its constraints, which
  speak of the value and not of its wire form, as a mapped type's are not. `varint`, `zigzag` and
  `fixed` name a binary layout, which no JSON value or text shows, so the number's own row writes
  one. A row added for an encoding at a later options version types its values anew, which is that
  version's break to state ([target options](options.md)).
- **A string under an encoding** is the encoded text already, as OpenAPI's `contentEncoding` beside
  no `format` lowers it ([`content-vocabulary`](../../compilers/openapi.md#content-vocabulary)), so
  no row encodes a `string`. A string that is the text to encode, as Smithy has a `@mediaType`
  string in a header, has no spelling in the IR apart from that one, so a compiler lowering one
  waits on the IR telling them apart.

Each target's lowering table names the type and the codec of each ([Go](../targets/go/lowering.md),
[Kotlin](../targets/kotlin/lowering.md)), and T-51 holds the rows and the lookup. The designs it was
weighed against:

- **The primitive's row with an info**, as the lookup once fell back: Go's `[]byte` row writes the
  bytes `fb ff` under `base64url` as `+/8=`, where the encoding names `-_8=`, and the plan reads
  such a scalar's JSON kinds from its `WireType` while the codec writes its primitive's
  ([TypePlan](../plan/types.md#variants-that-can-share-a-value)).
- **The same fallback at warning**: the severity changes nothing a server receives.
- **A row for every name the IR documents**: a name a compiler invents later would still fall to the
  wrong spelling, where typing by the wire form is right for a name no table knows.
