# Properties

Part of the [IR design](./README.md). One member of a model, and the lifecycle sets, not booleans,
that say when it is visible.

```go
type Property struct {
    ID         PropID
    Name       Naming
    WireName   string              // serialized name; defaults to Name.Source
    WireNameByFormat map[string]string // per-mime overrides (TypeSpec @encodedName json/xml)
    WireID     *int                // protobuf field number / thrift id / tuple element index
                                   // (1-based when Model.Positional); nil = none — pointer because
                                   // 0 is a legal ordinal in Cap'n Proto/FlatBuffers/Avro
    ExtensionOf string             // "" = the model's own field; else the fully-qualified declaring
                                   // scope of a third-party extension field (protobuf `extend`) —
                                   // directs qualified JSON naming and registry-based accessors
    Type       TypeRef
    Required   bool                // presence on the wire; orthogonal to Type.Nullable
    Presence   PresenceKind        // wire-presence discipline where the format distinguishes more
                                   // than required/optional (protobuf)
    ClientOptional bool            // wire-required but clients MUST treat as optional
                                   // (Smithy @clientOptional; implicit for @input structure members)
    DefaultAdded bool              // default was added post-publication; generators may ignore it
                                   // for backward compatibility (Smithy @addedDefault)
    Visibility Visibility          // lifecycle set; zero value = visible in all
    Default    *Value
    Constraints *Constraints
    Encoding   *Encoding
    Args       []Parameter         // parameterized fields: GraphQL field arguments on any property,
                                   // at any depth — not just operation entry points. Empty elsewhere.
    Flatten    bool                // property's fields hoisted into parent on the wire (Smithy/TCGC
                                   // flatten; also set on the synthetic property wrapping a hoisted
                                   // protobuf oneof — oneof members are wire-level top-level fields)
    EventHeader bool               // in an event-stream event model: member travels in the frame
                                   // header, not the event payload (Smithy @eventHeader)
    EventPayload bool              // member is the *raw* frame payload — blob/string as bytes,
                                   // structure as protocol document (Smithy @eventPayload).
                                   // Mutually exclusive with EventHeader; at most one per model.
    Secret     bool                // redact in logs/docs (TypeSpec @secret, format:password); a value whose
                                   // type is TypeCommon.Sensitive is redacted whether or not it is set
    XML        *XMLHints           // XML wire shape when it diverges from the JSON-implied shape
    Examples   []Example           // property-level examples (TypeSpec @example, OpenAPI)
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Unmodeled  Unmodeled
    Provenance Provenance
}

type PresenceKind string
// ""         — format default (JSON world: Required/Nullable say everything)
// "implicit" — absence ⇔ default value; unset is unobservable; zero values are not serialized
//              (proto3 no-label, editions IMPLICIT)
// "explicit" — unset is distinguishable from default-valued (hazzers/pointers; proto2 optional,
//              proto3 `optional`, editions EXPLICIT)
// "required" — must be present on the wire (proto2 required, editions LEGACY_REQUIRED)
```

**Scope rule for `Args`:** parameterized properties are only legal on models reachable from a
GraphQL binding (the only format with per-field arguments); the validate pass rejects them
elsewhere. This keeps ordinary models pure data shapes — a model without `Args` anywhere in its
graph serializes conventionally in every emitter.

## Visibility

```go
type Lifecycle = string // OPEN set. Canonical values: "create" | "read" | "update" | "delete" | "query".
                        // TypeSpec's visibility system allows arbitrary user-defined visibility
                        // classes; compilers lower custom classes as "<class>:<member>" strings so
                        // nothing is dropped. Filters evaluate per class, not across the flat union;
                        // emitters treat unknown classes as opaque filters.

type Visibility struct {
    Only []Lifecycle   // empty = visible in all lifecycles (unless None)
    None bool          // visible in NO lifecycle: excluded from every projection
                       // (TypeSpec @invisible) — distinct from the zero value
}
```

`Only`, like an operation's `ParameterVisibility` and `ReturnTypeVisibility`
([operations](./operations.md)), is a set: no entry is blank and none repeats (`ir/set-entry-blank`,
`ir/set-entry-duplicate`), since a blank names no class and a repeat admits nothing more.

`readOnly` lowers to `Only: [read]` (plus delete/query per OpenAPI semantics); `writeOnly` to
`Only: [create, update]`; GraphQL input-vs-output types to `create/update` vs `read`; TypeSpec
`@visibility` maps directly. One logical model therefore produces N wire shapes; the projection
(`ModelShape(model, lifecycle)`, with PATCH additionally making properties optional *unless the
binding disables it* — [http-binding](./http-binding.md) `PatchImplicitOptionality`) is a computed
traversal in emitters' plan layer — the IR stores the single logical model plus the visibility
facts, never the projected variants. Operations can override which filter applies to their
request/response ([operations](./operations.md) `ParameterVisibility`/`ReturnTypeVisibility`). This
is TypeSpec's `MetadataInfo` design with storage and computation split.
