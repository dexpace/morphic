# Annotations

Part of the [IR design](./README.md). Documentation, deprecation and examples, how prose names a
type, and where a declaration of one lives when the position writing it resolves to a shared node.

## Docs, deprecation, examples

```go
type Docs struct {
    Summary     string
    Description string        // CommonMark, exactly as the source wrote it
    Refs        []DocRef      // the spans of Description that name a type, ascending and disjoint
    ExternalDocs []Link       // {URL, Description}
}

type DocRef struct {
    StartByte, EndByte int    // a half-open span of Description's bytes, on rune boundaries
    Type               TypeID // the type the span names
}

type Deprecation struct {
    Message, Since, RemovalVersion, RemovalDate string
    ReplacedBy []EntityRef       // what to use instead, in source order ([replacements](#replacements))
}

type EntityRef struct {          // one entity by its ID: exactly one field is set
    Type TypeID; Operation OperationID; Channel ChannelID; Message MessageID; Auth AuthID
    Service ServiceID; Group GroupID; Property PropertyID; Param ParameterID; Member EnumMemberID
    Server ServerID
}
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
    Value       *Value           // single-value examples (schemas, properties, parameters);
                                 // for message examples: the payload
    Headers     *Value           // message examples: the correlated header values (AsyncAPI message
                                 // examples are header+payload PAIRS — never split them)
    Input       *ScenarioInput // operation scenarios: paired input ↔ output/error;
                                 // nil = not an operation scenario
    Output      *Value           // (Smithy @examples, TypeSpec @opExample parameters/returnType)
    Error       *ErrorExample    // the scenario ends in this error instead of Output
    ExternalURL string
    Unmodeled   Unmodeled
}
type ScenarioInput struct {
    Params map[ParameterID]Value // each parameter's value, by the ID of a parameter of the operation
    Body   *Value                // the value of the request content; nil = the scenario sends none
}
type ErrorExample struct { Type TypeRef; Content Value }
// Field legality is contextual, and `irverify` holds it (`ir/example-arm-misplaced`): Value/Headers
// on types, properties, parameters, contents, and messages; Input/Output/Error on operations. An
// Example never mixes the two arms, and never ends in both an Output and an Error
// (`ir/example-outcome-twice`).
```

A scenario's input states each value once. A parameter's value sits under its ID whatever its
binding, a path, query or header parameter alike, and `Body` is the value of the request content.
`Body` is nil on an operation that binds any parameter into its body, whole or by property: there
the parameters state the body, so a `Body` beside them would state part of it twice
(`ir/input-body-twice`), and a `Body` on an operation that sends no request has no content to be the
value of (`ir/input-body-absent`). A nil `Input` is no operation scenario; an empty one sends
nothing. The input is keyed by `ParameterID`, not by a parameter's source name, because a parameter
and a body member can share a name; and it is a struct rather than an object `Value`, whose field
names would hide an ID where no reference check finds it ([IDs as
references](./identity.md#ids-as-references)).

## Replacements

A deprecation that says what to use instead names it in `ReplacedBy`, so a docs target can link
"use `getOrderV2` instead" and an SDK can name the replacement in the deprecation it emits. Each
entry is an `EntityRef`: the ID of the replacement in the field of its class, so a consumer reads
which kind of entity it is from the field and finds it in the registry that field names, and the
reference check (`ir/dangling-*-ref`) holds it like any other ID. `irverify` holds a ref to one
field set (`ir/entity-ref-empty`, `ir/entity-ref-ambiguous`) and an entity to naming another
(`ir/replaced-by-itself`). A list, because a source that splits an operation names two; an
empty one names nothing.

The replacement may be of another class than what it replaces, since a source can move a value
from a query parameter into the body, so the reference names its class rather than taking the
carrier's. A bare ID string was rejected because a consumer would have to try each registry to
learn what it names, and the reference check could not see it; a field per class was chosen
over a class tag beside a string so that the IDs stay typed, which is what the check reads. A
replacement names an entity; it is no use of it, so neither `ir.TypeEdges` nor usage follows it.

No format states a replacement in a keyword: Smithy, TypeSpec and GraphQL carry a deprecation's
message and Protobuf only its flag, so a source that names a replacement does so in that prose or
in a vendor extension. A compiler fills the field from what its policy maps
([promotion](./unmodeled.md#promotion)), or, under a policy reading prose, from a name the message
gives in a form the document's own entities resolve, as the OpenAPI compiler reads an operation's
route or a code span naming a sibling
([`replacement-inference`](../compilers/openapi.md#replacement-inference)), the node marked
inferred. `Message` keeps the prose either way, so a consumer setting the guess aside loses
nothing.

## References in prose

Some formats name a type inside their prose: a Protobuf comment writes `[Book][example.v1.Book]`,
and EDoc has `{@link}`. A link an emitter resolves through the type's ID says what the type renders
as in that SDK, where the same words copied into a doc comment go stale (Kiota's doc templates).
**`Docs.Refs` marks such a span and leaves the prose as the source wrote it.** A `DocRef` says that
a run of `Description`'s bytes names a type. An emitter replaces the span with a link to whatever
the type renders as. One that does not resolve references, or that meets a type rendering as nothing
a link can reach, such as a primitive, leaves the span as text, which still says what the author
meant.

A span counts bytes of UTF-8, the encoding every string a document holds is in
([names](./naming.md)), so a reader in another language converts from it rather than counting in its
own, which is why the fields carry the unit in their names. Spans ascend and do not overlap, so an
emitter splices them in one pass over the text. `irverify` holds a span to a non-empty run of whole
runes inside its description (`ir/doc-ref-span`), and the list to that order (`ir/doc-ref-order`).
`Type` is a `TypeID` like any other, so both checkers report one that names nothing as
`ir/dangling-type-ref` and neither reads the prose. `Refs` indexes its own `Description`, so
whatever replaces a description replaces its references: a use site's description folded over a
referent's ([what a reference carries down](#what-a-reference-carries-down)) takes the use site's
spans or none.

**A reference is a mention, not a use.** Prose naming a type holds no value of it, so a `DocRef` is
no edge of the type graph. `ir.TypeEdges`, `irfacts` and GraphQL reachability do not follow one: a
model is not recursive because its description names it, and a type only prose names stays
unreachable. A removal that leaves a span naming nothing is reported as any dangling reference is.

**Why the references are not in the text.** The design this replaces wrote a `{t:TypeID}` token into
`Description`, and a description that happens to contain text of that shape, a templating example
for one, could not be told apart from one using a token. Nothing could check a token either, since
a parser inside prose reports its false positives as dangling references. Three ways of keeping the
reference in the text were weighed, and each fails on input real documents write:

- **An escape rule**, a compiler writing `\{t:` for a literal `{t:`, fails where such text sits.
  CommonMark reads no backslash escape inside a code span, so `` `{t:1}` `` would render with a
  stray backslash wherever a consumer did not undo the rule, and braces often sit in code: of the
  253 descriptions compiled from `cloudflare/api-schemas` @ `319dc756` that hold one, 121 hold it in
  a code span. It would also stop `Description` being what the source wrote (invariant 2).
- **A CommonMark construct of the IR's own**, such as a link under a reserved scheme, is the same
  ambiguity with a rarer spelling, and telling one from a code span takes a CommonMark parser, which
  `ir` may not import.
- **A table from spelling to type** replaces every occurrence of the spelling, the one in a code
  span included, and cannot say which occurrence a source meant.

Dropping the capability was weighed too, since OpenAPI writes no such reference and its compiler
sets no span. Two formats do ([matrix](../ir-spec-matrix.md)), and `Docs.Refs` is the home their
compilers fill rather than invent (invariant 9).

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

**A carrier holds its notes whatever its schema lowered to.** Where the schema at a carried position
builds a node of its own — a `List`, a `MapT`, an `Enum`, a `Model`, a `Union`, a `Tuple`, a
`Literal`, a `Scalar` — the carrier holds its documentation, deprecation and examples, the three it
has fields for, and the node holds none of them, so a reader of a model's members meets them on the
member whichever kind its schema is. That holds of each note nothing at the position writes over: where a
parameter or a header writes its own documentation or examples beside its schema's, or a Reference
Object naming a component one writes documentation beside its `$ref`, the node keeps the schema's
for every use of that position, and each carrier holds only its own. The node keeps what speaks of the values it admits: its XML hints, which `ir.Parameter`
has no field for, a redaction, which lands on the type (#1220), the bounds below, and what is kept
verbatim, which stays where the promotion reads a key. A schema at a carrier writing these notes
beside one `$ref` to an object, or beside an `allOf` of it, and nothing the carrier has no field
for, builds no node: the carrier names the target and holds them ([decorators](#decorators)). One
writing anything else beside them, or a note on a conjunct of its `allOf`, keeps them on the node
it builds, a [decorator](#decorators) among them, which an emitter folds onto the carrier. A
carried position that a reference names keeps them on its node too, its carrier holding none, so
every reference to it reads them where it lands.

Where a carrier writes annotations of its own alongside a schema's — a header and a parameter both
write a `description` beside their schema, and an `example` — the two stay apart: the carrier's own
describe that input, and the schema's describe its type, so they stay with the node that type is.
A deprecation is no such pair: either one deprecating the carrier deprecates it, so the carrier
holds both. Where the schema hoists no node, the carrier's own still replace the schema's, which no
node holds.

Constraints follow the same rule, value bounds and collection bounds alike. `Property.Constraints`
and `Parameter.Constraints` hold the bounds their schema writes where it hoisted no node, over a
reference or a primitive every use shares, and none where it did: the node holds them, a `Scalar`,
a `Model`, a `MapT`, a `List`, a `Tuple` or a `Union`, or the variants of a union the position builds
do, or the node keeps them verbatim where its kind has no field for them, an `Enum` or a `Literal`
([unions](./unions.md#bounds-beside-a-union)), a `Scalar` carrying a number as text, whose field
speaks of the number ([constraints](./constraints.md#constraints)), or a `Model` beside a string
bound. So a reader meets a bound once: on the carrier or on the node the carrier names, never on
both, as a reference naming the position reads it on the node. Where the position owns no node and
a reference names it, the reference reads a `Scalar` hoisted there holding the bounds, which the
carrier, naming the shared primitive, keeps as well
([`constraints`](../compilers/openapi.md#constraints)).

A carrier restating the node's value bounds was weighed and withdrawn (#1988). It gave a reader of
the carrier every bound in one place, but wrote each twice, so a consumer validating the carrier
and its type checked the bound twice, and one spelling of a use built a node holding a count that
another left on its carrier. A field the redeclared-property fold builds is the one carrier that
can hold a bound a node it reaches holds too. Where the fold composes the field's model, the model
it mints stands at no position and holds none, so the field holds what its declarations' own nodes
bound, which those nodes, the model's mixins, still hold. Where the field's type is one
declaration's node, the field holds the bounds of the declarations owning no node, which that node
may state as well. A reader joining the field's bounds with its type's meets the tightest of them
either way ([`allof-redeclared-property`](../compilers/openapi.md#allof-redeclared-property)).

## What a reference carries down

A `$ref` divides the declarations at its target in two, and the line falls between annotations and
constraints.

**Annotations merge onto the use site.** Documentation, deprecation, redaction, visibility and
`default` are read off the referent and written onto the referencing `Property`/`Parameter` with
**use-site precedence** ([`ref-use-site`](../compilers/openapi.md#ref-use-site)), field by field, a
description and its `Refs` counting as one ([references in prose](#references-in-prose)). Each is a
single value that one position may restate for another — a `description` beside the `$ref` says what
*this* input is, replacing the target's — so the two can never both be true at once, and any
consumer reading both would need this precedence rule anyway. Applying it once, in the compiler, is
what keeps every carrier alike. Redaction, deprecation and visibility have a single side: a position
can state each and none can withdraw one another states, so a use holds it where either position
does. That is JSON Schema's own reading of `deprecated`, `readOnly` and `writeOnly`, each in force
where any occurrence applying to a location is true and absent where one is false, and it is the
reading a [decorator](#decorators)'s fold makes too, so the two spellings of one use agree.

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
OpenAPI 3.0 writes `items: {allOf: [{$ref: Status}], description: the widget state}`, and keeps it
losslessly as an anonymous `Model` whose `Base` is `Status`, where `Status` can be an object, and as
the use it is where it cannot ([models](./models.md#an-allof-no-object-satisfies)). 3.1 writes the
same use `{$ref: Status, description: …}`, which hoists an anonymous `Scalar` alias over `Status`
wherever no carrier holds the annotations
([one structural home per declaration](#one-structural-home-per-declaration)).
A keyword that binds only the use, `readOnly`, `writeOnly`, `default` or a `nullable` a policy reads
as widening the reference, makes no decorator where a carrier holds the use: the carrier holds it
and names `Status` itself. Nor does a note the carrier holds, its documentation, deprecation or
examples, so `state: {allOf: [{$ref: Status}], description: …}` at a property names `Status` and
holds the description, in either spelling. A note no carrier holds makes the decorator: one at an
`items`, a union's branch or a component, one at a carried position a reference names, and one a
parameter's or header's own note, or a Reference Object's beside its `$ref`, writes over, as do a
keyword beside it that only a node keeps and a note written on a conjunct of the `allOf`. A decorator holding a carrier's note as well was
weighed and withdrawn (#2071): the two spellings agreed either way, but a reader of the carrier
alone found the note there over a string target and not over an object one, and a target that
became an object moved every user's note onto a node.
Neither the `Model` nor the alias says anything about a value that its `Base` does not. Such a node is a **decorator**,
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
