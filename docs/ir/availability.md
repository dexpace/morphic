# Availability

Part of the [IR design](./README.md). The API's own version timeline, as against the schema's [IR
version](./ir-version.md).

```go
type Availability struct {
    Added      []string  // version labels from Document.Versions, ordered — multiple entries
    Removed    []string  // support add/remove/re-add cycles (v1 present, v2 removed, v3 re-added)
    Deprecated string
    RenamedFrom     []VersionedName // {Version, Name}
    TypeChangedFrom []VersionedType // {Version, Type TypeRef} — property/return type changed
                                    // across versions (TypeSpec @typeChangedFrom)
    RequiredChanged []VersionedBool // {Version, WasRequired} — optionality flipped at a version
                                    // (TypeSpec @madeOptional/@madeRequired); the slice pass
                                    // reconstructs Required for older snapshots from it
}
type VersionedBool struct { Version string; WasRequired bool }
```

The IR stores the **timeline** (TypeSpec model); the `version-slice` pass produces a concrete
snapshot document per version for consumption. Emitters always receive snapshots — they never
interpret availability themselves. Formats without versioning semantics simply leave this nil.

`Availability` attaches everywhere versioning decorators can: `TypeCommon`, `Property`,
`Operation`, `Parameter`, `EnumMember`, `Variant`, and `OperationGroup` — a versioned enum
member or union variant must disappear from pre-`Added` snapshots. Smithy `@since` (free-form,
no version registry) lowers to `Added` only when the compiler synthesizes labels; otherwise it
is kept verbatim under `Unmodeled`.

## Version labels

`Document.Versions` lists the labels once each, none blank, and its order is the timeline's.
`irverify` reports a blank entry (`ir/version-label-blank`) and an entry repeating an earlier one
(`ir/version-label-repeated`). Every label an `Availability` writes, in `Added`, `Removed`, a
non-empty `Deprecated`, and the `Version` of a rename, a type change or an optionality change, is
one `Versions` declares (`ir/version-label-undeclared`).

With every label of `Added` and `Removed` declared, each ascends strictly in `Versions` order, and
the two merged alternate (`ir/availability-out-of-order`). Either may come first: a `Removed`
first says the entity is there from the first version. A label both lists write is out of order,
since the entity cannot be added and removed at one version.
