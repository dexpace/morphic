# The canonical step list

| Step | Reads (un-lowered IR fact) | Lowers to | ir-design section |
|---|---|---|---|
| `CollapseAliases` | `TypePlan.Alias`; the decorator chain's `Docs`, `Deprecation`, `Examples` | no declaration for an aliased type; each reference to it lowered as one to its `Alias`, its annotations folded onto the carrier (§4.15) | 4.3; 12.2 |
| `ResolveScalarChains` | `plan.ScalarChain` (the `Base` chain walked once, constraints merged), `Ctx.Types` | a declaration over the root (§4.9), the root typed by the type table (§2.6), + merged constraints; nil-base → the opaque-scalar strategy | 4.2 |
| `LowerContainers` | `List/MapT/Tuple` nodes (with IDs) | list, map and positional forms, a positional form's members past `MinItems` optional and a `Rest` trailing; a nil `MapT.Key` is a string key, and a non-string key → custom-key map or diagnostic | 4.6 |
| `LowerLiterals` | `Literal{Value}` | typed constant / single-value type (incl. `symbol`) | 4.6 |
| `LowerModels` | `TypePlan.Flattened`, `Required`, `Undeclared`, `Unflattened`, `Base`/`Implements`/`Mixins`, `AdditionalProps`, `Additional`, `Positional` | a declaration over the flattened properties, an `Undeclared` member a required field typed as the catch-all types it, the catch-all holding the undeclared members, unknown members rejected on `closed`, an `Unflattened` conjunct lowered or degraded with a code | 4.3 |
| `ProjectVisibilityShapes` | `plan.ModelShape` per lifecycle | one declaration per shape, such as `User` / `UserCreate` / `UserPatch` | 5.2 |
| `LowerOptionality` | `TypePlan.Required` (a member's wire presence, §3.2) ⊥ `TypeRef.Nullable` ⊥ `Property.Presence`; `Facts.Component` | the four+ states in the target's forms; a reference on a cycle the target holds by value broken as its type system requires (§4.16) | 5.1; INV8 |
| `LowerConstraints` | `Constraints` on properties/scalars/lists/maps | field-level validation metadata + a refine-built validation method structure | 5.3 |
| `LowerUnions` | `Union{Variants, Exclusive, WireTagged, Discriminator}`, a polymorphic base's `Discriminator` or `Abstract` with `Facts.Subtypes`, `TypePlan.Union`, `TypePlan.Open` | the target's closed sum, with a variant per variant or subtype + a fallback on an open one | 4.4; INV2 |
| `LowerDiscriminator` | `Discriminator{Property/PropertyName/Index, Mapping→TypeID, Default, Envelope}` | a tag-dispatched decoder + typed factories | 4.3–4.4 |
| `LowerOpenEnums` | `Enum.Closed`, `ValueType`, `Members`, `Flags`, `FallbackMember` | the target's closed, open and flags forms; an open one keeps an unknown value | 4.5 |
| `ExtractInterfaces` | `Implements`, `OperationGroup` | interfaces / client interfaces for testability | 4.3 |
| `MapExternalTypes` | `External{Identity, Package, MinVersion}`, `Ctx.Types` | the type table's row for the identity (§2.6): imported library type + dependency-manifest entry; no row → opaque strategy + diagnostic | 4.6 |
| `MaterializeValues` | `Value{Kind, Num BigVal, Ref, Ctor}` | literals; `Num` → bignum/decimal, never float64; `Ctor` → call; `symbol` → string explicitly; a default documented, never written onto a request (§4.10) | 6 |
| `BuildRequestShaping` | `plan.BindingView`, `RequestContent`, the target's parameter shape (§4.13) | request builders, per-protocol param binding, each operation's request type | 8 |
| `LowerHeaders` | `ResponsePlan.Headers` | a struct with a field per declared header, each optional, read from the raw headers by wire name; a `WithResponse` method on a success (§8.9), the failure value's and an `OtherResponse`'s `Headers` (§8.5, §8.9) | 7.2 |
| `LowerPagination` | `plan.PagePlan` (item unwrap, cursor paths) | an auto-pager; pacing left to policy | 7.3 |
| `LowerStreaming` | `plan.StreamPlan` (direction; each side's content, framing and element) | a method per shape the operation offers; a stream over an element of any kind; a `bytes` side lowered as a file; terminal ends | 7.3 |
| `LowerLRO` | `plan.LROPlan` (final-state-via, poll/final ops, result path) | poller type + one poll and a poll until done, which returns the final type | 7.3 |
| `LowerErrors` | `plan.PlannedError` | no error type: each entry's conditions, body decoder and declared facts in the operation's response decoder, which fills the runtime's failure value (§8.5) | 7.2 |
| `LowerOtherResponses` | `OpPlan.Others` | each entry's conditions in the operation's response decoder: `AsReturn` and a 2xx no entry answers read as `Return`, no body a nullable return or an ended pager or stream, any other a body decoder filling the runtime's `OtherResponse`, as a body that does not decode does (§8.9) | 7.2 |
| `PlaceDeclarations` | `Provenance.Source`, `TypePlan.Reach`, the target's placement options | the output unit each declaration lands in: one package unless a partition or a group split moves it (§4.17) | 13 |
| `ComputeImports` | what each file's declarations reference: another unit's declarations, a type-table row's `Imports`, the runtime | per-file import sets, each import by its path; the name a file gives it is settled with every other name (§4.12) | — |
| `RenderCasing` | neutral `Ident` (= `naming.Words`: the canonical words beside their source spellings; a name with none named by its characters) + the options' naming tables | cased identifiers, the units' among them — the ONLY place names get cased (§4.12) | 3.2; INV4 |
| `EscapeReservedWords` | cased `Ident` + each scope's reserved set: keywords, what the generated code binds there, what its file imports, what a unit cannot be named (§4.12) | keyword-safe identifiers; NEVER touches `WireName`/`WireID` | 3.2; INV4 |
| `ResolveCollisions` | `Ctx.Syms` (ID-keyed symbol table) | deterministic disambiguation in every scope, the units and each file's imports among them, an import yielding to a declaration (§4.12); never mutates IR identity | 3.1; INV3 |

A step's Go forms are in [Go's step table](../targets/go/lowering.md#the-step-list-in-go).

Every `ir.TypeKind` has a dispatch here: `primitive` and `any` map through the type table (§2.6) in
`ResolveScalarChains`/type-reference lowering, `literal` in `LowerLiterals`, the rest in the named
steps above. The switch-completeness test (§13) proves the coverage in the suite. A kind no compiler
produces yet dispatches to a coded degrade instead of a lowering (§14.1), which keeps the coverage
without a lowering nothing could test. The concrete lowering strategies for the load-bearing axes
are the documents beside this one.
