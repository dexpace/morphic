### 4.2 The canonical step list

| Step | Reads (un-lowered IR fact) | Lowers to (Go example) | IR ref |
|---|---|---|---|
| `CollapseAliases` | `TypePlan.Alias`; the decorator chain's `Docs`, `Deprecation`, `Examples` | no declaration for an aliased type; each reference to it lowered as one to its `Alias`, its annotations folded onto the carrier (§4.15) | §4.3; §12.2 |
| `ResolveScalarChains` | `plan.ScalarChain` (the `Base` chain walked once, constraints merged), `Ctx.Types` | a defined type over a root with no methods, an alias over any other (§4.9), the root typed by the type table (§2.6), + merged constraints; nil-base → newtype (opaque-scalar strategy) | §4.2 |
| `LowerContainers` | `List/MapT/Tuple` nodes (with IDs) | `[]T` / `map[K]V` / positional struct, its positions past `MinItems` optional and a `Rest` as a trailing slice; a nil `MapT.Key` is `string`, and a non-string key → custom-key map or diagnostic | §4.6 |
| `LowerLiterals` | `Literal{Value}` | typed constant / single-value newtype (incl. `symbol`) | §4.6 |
| `LowerModels` | `TypePlan.Flattened`, `Required`, `Undeclared`, `Unflattened`, `Base`/`Implements`/`Mixins`, `AdditionalProps`, `Additional`, `Positional` | struct over the flattened properties, an `Undeclared` member a required field typed as the catch-all types it, catch-all map as the embedded fallback, unknown members rejected on `closed` (§8.1), an `Unflattened` conjunct lowered or degraded with a code | §4.3 |
| `ProjectVisibilityShapes` | `plan.ModelShape` per lifecycle | `User` / `UserCreate` / `UserPatch` structs | §5.2 |
| `LowerOptionality` | `TypePlan.Required` (a member's wire presence, §3.2) ⊥ `TypeRef.Nullable` ⊥ `Property.Presence`; `Facts.Component` | the four+ states → `T` / `*T` / `Opt[T]`/`Nil[T]`/`OptNil[T]` / `omitzero` + presence bit; a reference on a cycle the target holds by value held through a pointer (§4.16) | §5.1; INV8 |
| `LowerConstraints` | `Constraints` on properties/scalars/lists/maps | field-level `validate` tags + a refine-built `Validate()` method structure | §5.3 |
| `LowerUnions` | `Union{Variants, Exclusive, WireTagged, Discriminator}`, a polymorphic base's `Discriminator` or `Abstract` with `Facts.Subtypes`, `TypePlan.Union`, `TypePlan.Open` | a struct holding a sealed interface + marker method + a wrapper per variant or subtype + a fallback on an open one | §4.4; INV2 |
| `LowerDiscriminator` | `Discriminator{Property/PropertyName/Index, Mapping→TypeID, Default, Envelope}` | tag-dispatched `UnmarshalJSONFrom` on the union's struct + typed factories | §4.3–§4.4 |
| `LowerOpenEnums` | `Enum.Closed`, `ValueType`, `Members`, `Flags`, `FallbackMember` | closed → typed consts; open → `string`+consts+`Unknown(raw)`; flags → bitset | §4.5 |
| `ExtractInterfaces` | `Implements`, `OperationGroup` | Go interfaces / client interfaces for testability | §4.3 |
| `MapExternalTypes` | `External{Identity, Package, MinVersion}`, `Ctx.Types` | the type table's row for the identity (§2.6): imported library type + dependency-manifest entry; no row → opaque strategy + diagnostic | §4.6 |
| `MaterializeValues` | `Value{Kind, Num BigVal, Ref, Ctor}` | literals; `Num` → bignum/decimal, never float64; `Ctor` → call; `symbol` → string explicitly; a default documented, never written onto a request (§4.10) | §6 |
| `BuildRequestShaping` | `plan.BindingView`, `RequestContent`, the target's parameter shape (§4.13) | request builders, per-protocol param binding, each operation's request type | §8 |
| `LowerHeaders` | `ResponsePlan.Headers` | a struct with a field per declared header, each optional, read from the raw headers by wire name; a `WithResponse` method on a success (§8.9), the failure value's and an `OtherResponse`'s `Headers` (§8.5, §8.9) | §7.2 |
| `LowerPagination` | `plan.PagePlan` (item unwrap, cursor paths) | auto-pager yielding `iter.Seq2[T, error]`; pacing left to policy | §7.3 |
| `LowerStreaming` | `plan.StreamPlan` (direction; each side's content, framing and element) | a method per shape the operation offers; `Stream[T]` with `Recv()`/`Send()` over an element of any kind; a `bytes` side lowered as a file; terminal ends | §7.3 |
| `LowerLRO` | `plan.LROPlan` (final-state-via, poll/final ops, result path) | poller type + `Poll()/PollUntilDone()` returning the final type | §7.3 |
| `LowerErrors` | `plan.PlannedError` | no error type: each entry's conditions, body decoder and declared facts in the operation's response decoder, which fills the runtime's failure value (§8.5) | §7.2 |
| `LowerOtherResponses` | `OpPlan.Others` | each entry's conditions in the operation's response decoder: `AsReturn` and a 2xx no entry answers read as `Return`, no body a nullable return or an ended pager or stream, any other a body decoder filling the runtime's `OtherResponse`, as a body that does not decode does (§8.9) | §7.2 |
| `PlaceDeclarations` | `Provenance.Source`, `TypePlan.Reach`, the target's placement options | the output unit each declaration lands in: one package unless a partition or a group split moves it (§4.17) | §13 |
| `ComputeImports` | what each file's declarations reference: another unit's declarations, a type-table row's `Imports`, the runtime | per-file import sets, each import by its path; the name a file gives it is settled with every other name (§4.12) | — |
| `RenderCasing` | neutral `Ident` (= `naming.Words`: the canonical words beside their source spellings; a name with none named by its characters) + the options' naming tables | cased identifiers, the units' among them — the ONLY place names get cased (§4.12) | §3.2; INV4 |
| `EscapeReservedWords` | cased `Ident` + each scope's reserved set: keywords, what the generated code binds there, what its file imports, what a unit cannot be named (§4.12) | keyword-safe identifiers; NEVER touches `WireName`/`WireID` | §3.2; INV4 |
| `ResolveCollisions` | `Ctx.Syms` (ID-keyed symbol table) | deterministic disambiguation in every scope, the units and each file's imports among them, an import yielding to a declaration (§4.12); never mutates IR identity | §3.1; INV3 |

Every `ir.TypeKind` has a dispatch here: `primitive` and `any` map through the type table (§2.6) in
`ResolveScalarChains`/type-reference lowering, `literal` in `LowerLiterals`, the rest in the named
steps above. The switch-completeness test (§13) proves the coverage in the suite. A kind no compiler
produces yet dispatches to a coded degrade instead of a lowering (§14.1), which keeps the coverage
without a lowering nothing could test. The concrete lowering strategies for the load-bearing axes
follow.
