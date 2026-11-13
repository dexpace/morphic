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

A variant need not declare the property a `PropertyName` tag sits at, require it, or type it as a
string. The payload carries the tag, which an open variant admits whether or not it names it:
OpenAPI asks that the payload hold the property and says only that a schema should require it, and
a variant typing it as a number owns the tags whose text is that number's literal ([the tag a
variant's value carries](../emitters/refine/unions.md#the-tag-a-variants-value-carries)). So
`pass.Validate` holds a union's discriminator to its routes, each naming a variant, and reports
nothing a variant declares of the tag, nor yet a closed variant declaring no such property, which no
tagged payload can be (#1807). A rule refusing a variant that does not declare the tag would
refuse OpenAPI's own shape, a `oneOf` of schemas a `discriminator` beside it tags, and warning where
one leaves it optional or a number would report documents the compiler lowers as written: on the
golden spec (spec @`86556f6f`, morphic @`0cd087ff6`), of the 551 variants of the 145 unions a
property name tags, 59 declare the tag optional and 34 declare it a number, every one of them an
integer error code its `mapping` routes.

A union whose source declares no discriminator may still name its variant in the payload: where
every variant requires one property and pins it to a distinct value, that value tells them apart as
validation does. A compiler policy may read such a tag in, as a `Discriminator` with `Inferred` set
([models](./models.md#discriminator)), and the union is then `WireTagged`, since the payload does
name its variant. The mark keeps the reading auditable: a consumer taking only what the source
declares reads such a union as untagged, which validation still decodes.

## A type listed twice

Two variants naming one type are told apart by nothing in a value of it, save the key of a
key-tagged union: a payload tag routes to a type, which both variants name. Such a value matches
both, so an exclusive union refuses every value of the type, and in an inclusive one neither variant
admits a value the other does not, a `null` aside where only one is nullable. A source can write
either, as `oneOf: [{$ref: A}, {$ref: A}]` or an `anyOf` listing `{type: string}` twice, so the IR
keeps both variants, as it keeps every branch, and `pass.Validate` reports the later: as a warning
`pass/exclusive-union-repeated-variant` where the union is exclusive, which then admits less than
the source seems to say, and as an info `pass/inclusive-union-redundant-variant` where it is not.
Each case has a code of its own, so a consumer selecting findings by code can take the redundant
repeat without the one that narrows what the union admits.

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
default mapping to name one, bar the key that opens an enum, which also opens the union of literals
a mixed enum lowers to. Whether a target tolerates an unknown variant of a closed union is
its policy, never the IR's.

`InputOnly` marks a union only a request may carry, with an identity distinct from every output
type, as `Model.InputOnly` marks an input object ([models](./models.md)). GraphQL is the source: a
`@oneOf` input object is such a union, and an emitter projecting separate input and output types
reads the flag rather than a format-named marker.
