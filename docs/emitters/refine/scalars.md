# Containers, scalars, external types

- **Containers**: `List`/`MapT`/`Tuple` are real IR nodes with IDs, so `List<Map<K,List<T>>>`
  composes cleanly; constrained lists and maps keep their bounds; `List.Encoding` (packed/expanded)
  reaches the serializer; a nil `MapT.Key` is a string key, and a non-string one is handled or
  diagnosed; tuples take a positional form whose positions past `Constraints.MinItems` are optional
  and whose `Rest`, when present, trails them.
- **Scalars**: the plan walks `Base` transitively once (`TypePlan.Scalar`), merging `Constraints`
  and settling `Encoding`, so the SDK, its docs and its mock read one chain; `ResolveScalarChains`
  lowers what it found, the root typed by the type table ([type table](../contract/type-table.md)).
  Nil-base scalars (GraphQL custom) go to the opaque-scalar strategy (a Go newtype), never a
  fabricated chain.
- **A declaration over a root keeps the root's codec.** A named scalar, an anonymous one that is no
  decorator ([decorators](decorators.md)), a named alias over the conjunct deciding an `allOf` no
  object satisfies ([models](../../ir/models.md#an-allof-no-object-satisfies)) and a `Model` whose
  `Base` is no model ([lowering table](lowering-table.md)) each declare a type standing over their
  chain's root, and it must code as the root does. So a target defines a new type only over a root
  whose codec that type keeps, and declares an alias over any other
  ([Go](../targets/go/types.md#a-declaration-over-a-root),
  [Kotlin](../targets/kotlin/types.md#a-declaration-over-a-root)). An alias declares no method, so
  its constraints are checked where it is used, as a field's are ([constraints](constraints.md)),
  and a projection of it ([visibility](models.md#visibility-projections--n-wire-shapes)) is its
  root's. The target knows which of its built-in types have methods, and takes a type only a user's
  row names ([type table](../contract/type-table.md)) to have some. An enum is outside this line: it
  declares its own codec ([enums](enums.md)), which reads and writes its value type through that
  type's row, so it is a defined type whatever its value type maps to.
- **External** ([containers](../../ir/containers.md)): `External{Identity, Package, MinVersion}`
  becomes the type its type-table row names for `Identity`
  ([type table](../contract/type-table.md)), imported with the row's `Imports` and required at its
  `Dependency` — imported, not generated. `Package` and
  `MinVersion` name what the source format imports, which a built-in row may follow; they cannot
  name a target's type, so a row is what maps an identity. An identity with no row (`erlang:pid`,
  `erlang:fun`) gets the passthrough/opaque strategy and a `warning` diagnostic, never a silent
  `any`.
