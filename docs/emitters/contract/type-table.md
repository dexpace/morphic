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
- **Lookup.** `{prim, encoding}`, then `{prim, ""}`. An encoding with no row of its own falls back
  to its primitive's: the encoding stays in the IR and the SDK types it as the primitive, reported
  once per key as an info `emitters/<target>/unmapped-encoding` at the first type in ID order that
  uses it. An `External` with no row takes the opaque strategy of [external
  types](../refine/scalars.md), with a warning.
- **Built-in rows are defaults of an options version.** Every target's built-in table has a row for
  every `PrimKind` at every options version, so the fallback always lands; T-17 holds each table to
  the kinds `ir` declares. Each target's lowering table lists version 1's rows
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
refuses, with a decode error, a number whose value has a fraction (`1.5`, `1e-400`), one outside
the row's range (`256` for a `uint8`, `9223372036854775808` and `1e400` for an `int64`) and
anything that is no number, a string of digits among them, which only a declared encoding reads. It
writes plain digits. A declaration over such a row reads the same way, as an enum over one already
compares its members ([a member on the wire](../refine/enums.md#a-member-on-the-wire)), and so does
every text position, a parameter, a header and a form field, in an SDK and in a mock: `120.0` in a
`Retry-After` typed `integer` is 120.

A JSON number is its value and not its digits, which is how JSON Schema counts `1.0` an integer and
how the plan already compares values when it separates a union's variants
([TypePlan](../plan/types.md#variants-that-can-share-a-value)). Producers write these spellings for
whole numbers held as floats: Python's and Ruby's JSON encoders write `1.0` and `1e+16`, and
JavaScript's `1e+21`. Neither first target's library reads them unaided, and the two disagree
(Go 1.27.3; kotlinx-serialization-json 1.11.0 on Kotlin 2.4.0):

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
than the row: it compares the digits with the exponent, so `1e1000000000` is refused as out of
range without being expanded, and a literal a megabyte long is read in one pass. A row a
configuration maps to an exact type reads through that type's own codec, which the SDK does not
wrap: Go's `*big.Int` reads plain digits alone, and a codec that expands an exponent bounds it
itself, since thirteen bytes can name a billion digits. `number`, `float` and `decimal` rows read
every spelling already. Each target says how its rows read
([Go](../targets/go/types.md#integers), [Kotlin](../targets/kotlin/types.md#integers)), and T-70
holds the rule.

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
