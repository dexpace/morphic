# Annotations

Part of the [IR design](./README.md). Documentation, deprecation and examples, and where a
declaration of one lives when the position writing it resolves to a shared node.

## Docs, deprecation, examples

```go
type Docs struct {
    Summary     string
    Description string        // CommonMark; may contain {t:TypeID} cross-reference tokens that
                              // emitters resolve to language-appropriate links (Kiota's doc templates)
    ExternalDocs []Link       // {URL, Description}
}

type Deprecation struct { Message, Since, RemovalVersion, RemovalDate string }
// A scheduled removal is two fields because a version and a date are two facts, not two
// spellings of one: a document may state either or both, and neither is derivable from the
// other without a release calendar the IR does not have. A consumer deciding whether removing
// a deprecated entity is breaking compares a removal date against a release date, so it must
// be able to tell which fact it holds without re-parsing the string. RemovalDate is the
// source's own text, unparsed and unnormalized — no source format defines the field, so none
// defines its format either.

type Example struct {
    Name        string
    Summary     string
    Description string
    Value       *Value        // single-value examples (schemas, properties, parameters);
                              // for message examples: the payload
    Headers     *Value        // message examples: the correlated header values (AsyncAPI message
                              // examples are header+payload PAIRS — never split them)
    Input       *Value        // operation scenarios: paired input ↔ output/error
    Output      *Value        // (Smithy @examples, TypeSpec @opExample parameters/returnType)
    Error       *ErrorExample // the scenario ends in this error instead of Output
    ExternalURL string
    Unmodeled   Unmodeled
}
type ErrorExample struct { Type TypeRef; Content Value }
// Field legality is contextual (validated): Value/Headers on types, properties, parameters,
// contents, and messages; Input/Output/Error on operations. An Example never mixes the two arms.
```

## One structural home per declaration

Documentation, deprecation, XML hints, examples, vendor extensions, validation-only keywords and
value constraints are **position-scoped**: they bind the position they were written at, not the
shape that position resolves to. A source schema that reduces to a shared node — the `string`
primitive, the schemaless top type, or another type reached through a `$ref` — must therefore
never write them onto that node, because every other position resolving to the same shape would
then read them as its own.

Each such declaration gets exactly **one** home, chosen by the position rather than by what the
body lowered to:

| the position | where the declaration's annotations live |
|---|---|
| carries an `ir.Property` or `ir.Parameter` of its own — a model property, a header, an operation parameter | that carrier |
| carries neither — a component, an `items`, `additionalProperties`, `prefixItems[n]`, a `patternProperties` value, a union branch, a media-type or item schema, a `$ref`-hoisted sub-schema | a type node the position owns, hoisting a `Scalar` alias over the shared target when the body reduced to one |

Two consequences a compiler must hold to. **A carrier position never hoists an alias**: a
documented scalar field keeps its description on `Property.Docs`, where an emitter rendering that
field reads it, rather than on an anonymous node the field points at. **A non-carrier position
hoists only when the declaration says something** one of those homes holds — a bare
`items: {type: string}` gains nothing by owning a node, so it keeps resolving straight to the
shared primitive and the type graph stays free of an anonymous node per element position.

Where a carrier holds annotations of its own alongside a schema's — a header and a parameter both
write a `description` beside their schema — the carrier's own win: they describe that input,
while the schema's describe its type.

Constraints follow the same rule with one exception on the carrier side. `Property.Constraints`
and `Parameter.Constraints` are filled whether or not the schema hoisted a node, because no node
such a schema can hoist holds them: an object lowers to a `Model` and a formatted scalar to a
`Scalar` carrying an `Encoding`, and neither has a field for the scalar bounds.

## What a reference carries down

A `$ref` divides the declarations at its target in two, and the line falls between annotations and
constraints.

**Annotations merge onto the use site.** Documentation, deprecation, redaction, visibility and
`default` are read off the referent and written onto the referencing `Property`/`Parameter` with
**use-site precedence** ([`ref-use-site`](../compilers/openapi.md#ref-use-site)), field by field. Each is a
single value that one position may restate for another — a `description` beside the `$ref` says what
*this* input is, replacing the target's — so the two can never both be true at once, and any
consumer reading both would need this precedence rule anyway. Applying it once, in the compiler, is
what keeps every carrier alike.

An OpenAPI Reference Object's own `summary` and `description` fold onto the use site the same way,
by the OpenAPI compiler's rule
[`reference-object-siblings`](../compilers/openapi.md#reference-object-siblings).

**Constraints do not merge, and are never copied to a use site.** Bounds *conjoin*: `maxLength: 64`
on the referent and `maxLength: 100` beside the `$ref` are both in force, and the admitted value is
the narrower of the two. There is no precedence to apply — merging with use-site precedence would
publish `100` as the whole truth and lose the bound the document actually enforces. So
`Property.Constraints` and `Parameter.Constraints` hold what their own position declared and nothing
else, and every node reached through `TypeRef` keeps its own.

The rule a consumer needs follows from that, and it is the reason this is written down rather than
left to be inferred from an empty struct: **an absent `Constraints` at a use site means that
position declared no bound, never that the value is unbounded.** The effective bound is the
conjunction of the use site's own `Constraints` with those of every node reached from its
`TypeRef`; a consumer that wants it — a validator emitter, a differ comparing two revisions —
resolves the reference and intersects. A differ that reads use sites alone sees no change when a
shared component's `maxLength` moves, because the change is at the component, which is the one
place it was declared and the one place it is recorded.

## Decorators

A format that admits nothing beside a reference annotates one use of a shared type by wrapping it:
OpenAPI 3.0 writes `state: {allOf: [{$ref: Status}], description: the widget state}`, and keeps it
losslessly as an anonymous `Model` whose `Base` is `Status`. 3.1 writes the same use `{$ref: Status,
description: …}`, which hoists an anonymous `Scalar` alias over `Status` wherever no carrier holds
the annotations ([one structural home per declaration](#one-structural-home-per-declaration)).
Neither node says anything about a value that its `Base` does not. Such a node is a **decorator**,
and `ir.Decorated` is the one statement of the rule: an anonymous type whose `Base` is set, and
which is

- a `Model` with every other field of its own zero, or a `Scalar` with no `Constraints` and no
  `Encoding`; and
- sets nothing in `TypeCommon` beyond its identity (`ID`, `Name`, `Namespace`, `Anonymous`,
  `Provenance`), the annotations a use site can hold (`Docs`, `Deprecation`, `Examples`),
  `Availability`, which no emitter interprets, and `Unmodeled`, whose entries an SDK acts on by
  reason, none of them by changing a type.

The rule lists what may be set rather than what may not, so a field the IR gains keeps a node from
being a decorator until a change decides otherwise; `ir`'s test sets every other field in turn by
reflection, which turns that decision into a failing test rather than a review catch. A named type
is never a decorator: its name is public surface. An emitter collapses a decorator into the
reference it decorates ([decorators](../emitters/refine/decorators.md)), and a compiler names what
holds one as it names what holds that reference, so the two spellings of one use name it alike
([`union`](../compilers/openapi.md#union)).
