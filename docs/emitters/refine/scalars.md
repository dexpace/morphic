# Containers, scalars, external types

- **Containers**: `List`/`MapT`/`Tuple` are real IR nodes with IDs, so `List<Map<K,List<T>>>`
  composes cleanly; constrained lists and maps keep their bounds; `List.Encoding` (packed/expanded)
  reaches the serializer; a nil `MapT.Key` is a string key, and a non-string one is handled or
  diagnosed; tuples become positional structs
  whose positions past `Constraints.MinItems` are optional and whose `Rest`, when present, is a
  trailing slice.
- **Scalars**: the plan walks `Base` transitively once (`TypePlan.Scalar`), merging
  `Constraints` and settling `Encoding`, so the SDK, its docs and its mock read one chain;
  `ResolveScalarChains` lowers what it found, the root typed by the type table (§2.6). Nil-base
  scalars (GraphQL custom) go to the opaque-scalar strategy (a Go newtype), never a fabricated
  chain.
- **A declaration over a root keeps the root's codec.** A named scalar, an anonymous one that is no
  decorator (§4.15), and a `Model` whose `Base` is no model (§9) each declare a type standing over
  their chain's root, and it must code as the root does. So a target defines a new type only over a
  root whose type has no methods to lose, and declares an alias over any other
  ([Go](../targets/go/types.md#a-declaration-over-a-root), [Kotlin](../targets/kotlin/types.md#a-declaration-over-a-root)). An alias declares no method, so its constraints
  are checked where it is used, as a field's are (§4.11), and a projection of it (§4.6) is its
  root's. The target knows which of its built-in types have methods, and takes a type only a user's
  row names (§2.6) to have some. An enum is outside this line: it declares its own codec (§4.5),
  which reads and writes its value type through that type's row, so it is a defined type whatever
  its value type maps to.
- **External** (R4 §2.8): `External{Identity, Package, MinVersion}` becomes the type its type-table
  row names for `Identity` (§2.6), imported with the row's `Imports` and required at its
  `Dependency` — imported, not generated. `Package` and `MinVersion` name what the source format
  imports, which a built-in row may follow; they cannot name a target's type, so a row is what maps
  an identity. An identity with no row (`erlang:pid`, `erlang:fun`) gets the passthrough/opaque
  strategy and a `warning` diagnostic, never a silent `any`.
