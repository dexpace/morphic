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
  value) is built by decoding the value's wire form through its codec, an enum declared over the
  wire form where the target's constants cannot hold the type
  ([enums](../refine/enums.md#a-member-on-the-wire)), and a constraint on it is not checked by the
  SDK, which the server still checks, reported once per key as an info
  `emitters/<target>/unchecked-constraint`. A row's codec goes with its type, so a declaration
  standing over the type keeps it by naming that type again rather than defining a new one
  ([scalars](../refine/scalars.md)), and a type only a user's row names is taken to have methods.
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
may name a type of the SDK's runtime, which the target imports from wherever it puts the runtime.
