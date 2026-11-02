# Unions

Part of the [IR design](./README.md). One node for every union a source can write, and the tag modes
that say how a payload names its variant.

```go
type Union struct {
    TypeCommon
    Variants      []Variant
    Exclusive     bool           // exactly one variant matches (oneOf); false = one or more (anyOf,
                                 // and a union whose variants overlap though no source asked to
                                 // exclude them, as a type array naming integer and number)
    WireTagged    bool           // the payload names its variant: by the Discriminator's tag, or by
                                 // a single key (Smithy union, protobuf oneof); false = untagged
    Discriminator *Discriminator // where the tag sits in the payload; set only when WireTagged
    Closed        bool           // false = open: a payload may hold a variant this does not list
    InputOnly     bool           // only a request carries it (GraphQL @oneOf input objects)
}

type Variant struct {
    Name       Naming    // named variants (TypeSpec, Smithy, protobuf oneof fields); Hint-only for bare oneOf members
    Type       TypeRef
    WireName   string    // serialized tag when it differs from Name.Source
                         // (Smithy @jsonName on union members, protobuf oneof json_name)
    WireID     *int      // protobuf oneof field number, Cap'n Proto/Avro ordinal; nil = none
                         // (pointer because 0 is a legal ordinal in several formats)
    XML        *XMLHints // @xmlName/@xmlNamespace on union members
    Event      *EventInfo // event-stream metadata when the union is a stream's event set
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Examples   []Example
    Unmodeled  Unmodeled
}

type EventInfo struct {
    ContentType string   // per-event content type (TypeSpec @Events.contentType)
    Terminal    bool     // receiving this event ends the stream (TypeSpec @SSE.terminalEvent)
}
```

One node covers the whole design space from the capability matrix: untagged `anyOf`
(`Exclusive=false`), untagged `oneOf` (`Exclusive=true`), unions a tag in the payload routes
(`WireTagged=true` with a `Discriminator`), and key-tagged unions (`WireTagged=true` alone).
Variant identity survives so emitters can generate named accessors, sealed interfaces, or wrapper
types per their refiner strategy. Unions never degrade to optional-field merges in the IR — that
includes GraphQL `@oneOf` input objects, which are spec-level tagged input unions and lower here,
not to models.

## Tag modes

**Tag-mode conventions** (normative): `WireTagged=true` with a `Discriminator` means the tag
travels in the payload, where the discriminator locates it: inline among the variant's own
members, as a property (an OpenAPI `discriminator`, GraphQL `__typename`) or a tuple element
(`Index`, an Erlang tagged tuple), or in the `{kind, value}` `Envelope` around them.
`WireTagged=true` with **nil** `Discriminator` means the union is *key-tagged*: the wire shape is
a single-key object whose key is the variant's wire name (Smithy unions, proto3-JSON oneof,
GraphQL `@oneOf` inputs). `WireTagged=false` means the variant is inferred by validation (JSON
oneOf/anyOf), and such a union carries no `Discriminator`; `irverify` refuses one that does
(`ir/union-discriminator-untagged`). `Exclusive` is independent of the mode: it records what
validation admits, exactly one variant or one or more.

An OpenAPI discriminator is the payload-tagged mode, not a fourth one. The tag property's value
names the schema the payload is expected to validate against. A value no mapping names selects
none, and OpenAPI says validation should then fail, unless 3.2's `defaultMapping` names one, which
`Discriminator.Default` holds and which also answers for a payload carrying no tag. OpenAPI also
says the discriminator must not change the validation outcome, but that binds validators, not the
wire: what validation admits stays in `Exclusive`, and on an `anyOf` the tag picks one of the
several schemas a payload may satisfy. A mode for "tag plus validation" would spell the same wire
shape twice.

## Open unions

`Closed` says whether a payload can hold a variant the union does not list, as `Enum.Closed` says
of a value no member names ([enums](./enums.md)). A closed union admits its variants and nothing
else; an open one may meet a variant added after the consumer was built, which the consumer must be
able to represent rather than refuse, as an unknown-variant case beside the known ones. The default
follows each format's semantics, never one target's: a JSON Schema `oneOf` or `anyOf` admits only
what matches a listed schema, so every OpenAPI and AsyncAPI union is closed; a Smithy union is
open, since a client must tolerate a member it does not know; a protobuf `oneof` is open, since an
unknown field leaves no case set rather than failing the message; a GraphQL output union gains
members as a schema evolves, and is open. A `@oneOf` input object is closed, since the server
validates what a client sends. A document that marks an OpenAPI union open can only do it through
an extension, which a promotion mapping may read into `Closed`
([promotion](./unmodeled.md#promotion)); no spelling is common enough for the OpenAPI compiler's
default mapping to name one. Whether a target tolerates an unknown variant of a closed union is
its policy, never the IR's.

`InputOnly` marks a union only a request may carry, with an identity distinct from every output
type, as `Model.InputOnly` marks an input object ([models](./models.md)). GraphQL is the source: a
`@oneOf` input object is such a union, and an emitter projecting separate input and output types
reads the flag rather than a format-named marker.
