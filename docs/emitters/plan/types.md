# TypePlan — per-type projections computed once

```go
type TypePlan struct {
    Flattened  []ir.PropID          // ir.FlattenedProperties over Base+Implements+Mixins+own: the
                                    // declaration each member takes, in layout order — COMPUTED
    Required   []ir.PropID          // the flattened properties an instance must carry: each one
                                    // a declaration marks Required or a Requires names
    Undeclared []string             // the names Requires lists that no flattened property declares,
                                    // in order: members only the catch-all governs
    Redacted   []ir.PropID          // the flattened members a value is redacted at: each whose
                                    // ir.FlatProperty.Secret is set, as one of its declarations is
                                    // Secret or of a sensitive type (../refine/redaction.md)
    Sensitive  bool                 // the type, or one its Base chain reaches, is Sensitive
    Unflattened []ir.TypeID         // the composition entries naming no model, which Flattened
                                    // cannot hold: each lowered beside the properties, or degraded
    Composed   []ComposedVariant    // a model narrowing a union: each shape an instance takes, the
                                    // model with a variant in the union's place; empty otherwise,
                                    // for one adding nothing to its union, and where no
                                    // distribution serves (below)
    Union      UnionShape           // a union's wire shape, one of four (../refine/unions.md); "" = not a union
    Discrim    *DiscrimFacts        // a union's or hierarchy's discriminator, as variant IDs
    Open       bool                 // a union, polymorphic base, model narrowing a union or enum
                                    // keeps a value no variant or member recognizes rather than
                                    // refusing it (policy.md, ../refine/unions.md,
                                    // ../refine/enums.md)
    Inclusive  bool                 // an any_of union two of whose variants can admit one value in
                                    // the declaration no shape projects (below); false otherwise
    Strict     bool                 // a one_of union refuses a value two variants admit unless its
                                    // constraints single one out (policy.md, ../refine/unions.md)
    Scalar     *ScalarChain         // a scalar's Base chain, walked once; nil = not a scalar
    Shapes     []ModelShape         // the shapes the document exercises it in, sorted (below)
    Alias      ir.TypeID            // the type every reference to this one is emitted as, this
                                    // one emitting nothing: a decorator's end (../refine/decorators.md), or a dedup
                                    // alias's target once that pass (architecture.md §2.2) and its
                                    // record (docs/ir/open-questions.md, question 2) exist; "" = itself
    Reach      []ir.GroupID         // the top-level operation groups whose operations reach this
                                    // type, sorted; empty = none does (../refine/output-units.md)
    Inferred   map[string]ir.Provenance
}

// ComposedVariant is one shape an instance of a model narrowing a union takes: the composition
// with a variant in the union's place, and in that of each union it narrows in turn (below).
type ComposedVariant struct {
    Over        []ir.TypeID         // the variant taken in each union's place, outermost first
    Flattened   []ir.PropID         // these five as TypePlan's own, read over the composition
    Required    []ir.PropID         // with Over's variants in their unions' places
    Undeclared  []string
    Redacted    []ir.PropID
    Unflattened []ir.TypeID
    Shapes      []ModelShape        // one for each of the model's, holding what Flattened shows
                                    // there
}

// UnionShape is the wire shape a union's serializer reads, from Exclusive, WireTagged and
// Discriminator together (docs/ir/unions.md#tag-modes).
type UnionShape string              // "one_of" | "any_of" | "discriminated" | "key_tagged"

type ScalarChain struct {           // a Scalar's Base chain walked to its root (../refine/scalars.md)
    Root        ir.TypeID           // the chain's first node that is not a Scalar: a primitive, or
                                    // what an alias stands over (docs/ir/scalars.md); "" = opaque
    Constraints *ir.Constraints     // merged along the chain, decimal throughout (../refine/constraints.md)
    Encoding    *ir.Encoding        // the encoding declared nearest this scalar; nil = the root's
}

type DiscrimFacts struct {          // Mapping/Default resolved to TypeIDs, never names (INV3)
    Locator  DiscrimLocator         // property | property_name | index
    WireKey  string                 // wire name of the tag property ("" when Locator == index)
    Index    *int                   // positional tag element (Erlang tagged tuples); nil otherwise
    Mapping  map[string]ir.TypeID   // wire tag value → variant ID
    Default  ir.TypeID              // variant for absent/unrecognized tag; "" = none
    Envelope string                 // "" = inline tag | "object" = {kind,value} wrapper
    EnvelopeValueName string        // wire name of the envelope value property (Envelope=="object")
}
type DiscrimLocator string          // "property" | "property_name" | "index"
```

Note what `TypePlan` does *not* do: it does not choose Go's sealed interface vs a native union, does
not case anything, does not resolve reserved words, and does not copy a fact the IR states outright
([plan or refine](plan-or-refine.md)). `Flattened` is the *computed* flattening; the IR keeps
`Base`, `Implements` and `Mixins` distinct beside it, so a class-based target can map `Base→extends`
/ `Implements→interfaces` / `Mixins→inline` where the three first targets flatten
([inheritance](../refine/models.md#inheritance)). The plan does not decide which — that is
per-language ([refine](../refine/pipeline.md)).

`Flattened`, `Required`, `Undeclared` and `Unflattened` are one `ir.FlattenedProperties` walk, by
the rules [models](../../ir/models.md#the-flattening) states: its order, which declaration of a
redeclared member is taken, and when a member is required. A flattened property is required where
any of its declarations says so, an overridden one included, or where this model or one it composes
lists its wire name in `Requires`, as `allOf: [{$ref: User}, {required: [id]}]` makes `User`'s `id`
required on the composed model. A name `Requires` lists that no flattened property declares is
`Undeclared`: a member only the catch-all governs, whose value the catch-all types —
`AdditionalProps.Value`, or the top type where the model has no catch-all or scopes values by
`Patterns`, since which pattern governs a name is ECMA-262's to say and no target's matcher is held
to it. A conjunct that is no model, as `Tags` is in `allOf: [{$ref: Pet}, {$ref: Tags}]` when
`Tags` is a list, is in `Unflattened` as the entry names it: the declaration over `Flattened` does
not hold what it admits, so each refiner lowers it or degrades with a code of its own; the three
targets lower a union there as [inheritance](../refine/models.md#inheritance) says.

## A model narrowing a union

A model whose `Unflattened` holds a union that does not list it narrows that union, a union its
composition reaches at any distance: each of its instances is an instance of one of the union's
variants with the rest of the composition beside it
([models](../../ir/models.md#a-union-a-composition-names)). `Unflattened` lists an entry's target as
written, so where it names an alias scalar, as a 3.1 `$ref` beside a `description` hoists one
([composition entries](../../ir/models.md#composition-entries)), the union is the one the alias
stands for, read through as the flattening reads every entry. `Composed` holds an entry for each
shape that gives: the composition with a variant in the union's place, flattened by `ir`'s walk
reading that variant where it meets the union ([the flattening](../../ir/models.md#the-flattening),
#1734), never by a walk of the plan's own, so the variant's members are laid out, overridden and
required as any conjunct's are. Where that composition narrows a union in turn, as a variant that is
a union or narrows one makes it, the entry gives way to one for each of that union's variants, its
`Over` one longer, so no entry narrows a union, and the entries follow each union's variants in
source order, depth first. An entry's `Shapes` are the model's, each with the model's filter and
patch and the `Props` the entry's `Flattened` shows there. In each shape the model is inclusive and
strict where its union is ([inheritance](../refine/models.md#inheritance)), which the union's own
plan says, so no entry repeats it.

`Composed` is empty for a model adding nothing to its union, which is that union, and for one no
distribution serves: a model narrowing two unions, itself or through an entry; one whose union has a
variant composing the model in turn, or reaches itself through the unions among its variants, either
of which no distribution ends; one over a key-tagged or enveloped union, whose variants are not the
values it admits; one over a union tagged by a tuple index, whose values are no objects; and one
whose entries, counted with what each one's walk reads, would number more than
`plan.MaxComposedSize`, a fixed limit the plan package states ([overview](overview.md)), which
`Build` reports as a warning `emitters/plan/composed-limit` naming the model. A walk reads each
variant it takes in a union's place, each model and composition entry it passes, each alias scalar
it reads through, each property and `Requires` name, and each type it reads a redaction off, which
is what it costs ([the flattening](../../ir/models.md#the-flattening)) and what `ir`'s walk is to
report beside the entry it flattens (#1734). Nested unions multiply the entries and each walk reads
the whole composition again, so counting only the members the entries hold would leave that cost
free: a 124 KB spec composing a chain of 1,000 models, each redeclaring one member, beside unions
nested 14 deep holds 81,918 entries and members where its walks take 83 million steps. Counting each
variant read keeps the entries' `Over` lists within the limit too: a 193 KB spec nesting unions
1,000 deep, each beside one leaf, gives 1,000 entries whose `Over` lists hold 500,500 variants,
while the entries and the rest of what their walks read count 10,000. On the golden spec the
largest, `rulesets_ResponseRule`, counts 1,143 (spec @bc6fad24, morphic @ea70eba79). The SDK targets
alias a model adding nothing, and degrade the others
([inheritance](../refine/models.md#inheritance)). An entry whose declarations of one member admit no
value in common, as one over a variant pinning a tag the model pins otherwise does, is an entry like
any, its member as the flattening takes it; whether to leave it out is #1739's.

The plan holds the entries because four artifacts read them: an SDK target declares a variant for
each, the docs render a table for it, and the samples and a mock show values of it, which the
example set holds ([one set of examples](../docs-and-mock.md#one-set-of-examples)). Two of them
reading the shapes apart would show members another lacks, which is the third test of [plan or
refine](plan-or-refine.md), and the example set is the plan's, which imports only `ir` and
`ir/irfacts` ([packages](../packages.md)), so no refine helper can serve it. A target with
intersection types writes the composition as it stands and need not read `Composed`, as a
class-based one need not read `Flattened`. On the golden spec (spec @bc6fad24, morphic
@ea70eba79), 23 models narrow a union beside a member of their own, in 158 entries:
`rulesets_ResponseRule` over the 21 variants of `rulesets_RequestRule`, and
`dns-records_dns-record-batch-patch` over a union of two variants that narrow unions of 8 and 13,
so 21 entries. The 26 others add nothing to their union, and none is a case no distribution serves.

## Variants that can share a value

An `any_of` union is inclusive in a shape when two of its variants can admit one value there, and a
union of any other wire shape is inclusive nowhere. The plan decides it for each shape the union is
projected in ([below](#a-shape-reaches-what-it-holds)), as that shape's `ModelShape.Inclusive`, and
for the declaration no shape projects, as `TypePlan.Inclusive`. What admitting a value means is in
[unions](../refine/unions.md#a-value-several-variants-admit). The plan proves two variants disjoint,
admitting no value in common, by these rules, and a pair none of them separates can share one:

- **Kinds.** Their JSON kinds have none in common. A variant's kinds are its type's: object for a
  model or a map, array for a list, a tuple or a `Positional` model, its members' wire forms' for a
  closed enum ([enums](../refine/enums.md#a-member-on-the-wire)), its values' for a literal, its
  `ValueType`'s and its members' wire forms' for an open enum, its variants' for a union, its base's
  for a model that only stands over one, and every kind for the `Any` type, the `any` primitive,
  which `{}` compiles to, and an `External`, whose wire form is its library type's. Each other
  primitive's is boolean for `bool`, a number for a numeric kind and a string for the rest. A
  scalar's are those of the `WireType` its encoding names (`ScalarChain.Encoding`), the type its
  values travel as, and its root's where none does. A position admitting `null`
  ([unions](../refine/unions.md#a-null-a-variant-admits)) adds `null`, but a variant's own `null` is
  its union's, so the rules leave it out, and a variant admitting only `null` takes no part. Kinds
  are JSON's, so they separate nothing in a union that text carries, at any depth: a parameter's, a
  header's, a form field's or a decoded body's, unless its content is JSON. `since=-10` is the text
  of an integer and of a string alike, so there only values separate variants, compared by their
  texts, with every spelling of a number its one value: `1.0` is also the text of `1`.
- **Values.** Each admits finitely many values, as a closed enum (its members' wire forms), a
  literal or a union of them does, and no value is in both, compared as JSON Schema compares
  instances: `1` and `1.0` are one. A `Flags` enum's values are its members' combinations, and one
  with a `FallbackMember` takes every value of its `ValueType`, substituting that member for one it
  does not list.
- **Members.** Both are models laid out by name, read through `Flattened` as the JSON wire lays them
  out: each member under its JSON name (`WireNameByFormat`'s entry for JSON, else `WireName`), a
  `Flatten` property's members at its model's own level, and a position's kinds read from the
  property's own `Encoding` before its type's. One requires a member the other cannot hold: one it
  does not declare while it is closed and no model in its flattening has a catch-all
  (`AdditionalProps`), or one it declares at a position disjoint from the first's by these rules.
  Both are read in the shape the union is decided in
  ([ModelShape](#modelshape--visibility-projection)): a member that shape leaves out is undeclared
  there, and one separates only where the shape holds and requires it, as `Required` and
  `Undeclared` say, an overridden declaration counting, so a base requiring `id` separates subtypes
  that redeclare `id` as different one-value enums. A `readOnly` member separates in a read shape
  and not in a create one, none separates in a patch shape, whose members are optional, and none
  `ClientOptional` marks, which a client treats as optional. The declaration no shape projects is
  decided for every filter at once: a member separates only where no filter can leave it out, its
  `Visibility` the zero value, and a declaration some filter leaves out separates only where its
  model is closed as well. A catch-all beside a closure holds the members it governs, as
  `patternProperties` beside `additionalProperties: false` does
  ([catch-alls](../../ir/models.md#catch-alls)), so the closure alone separates nothing, and a
  polymorphic base, which holds its subtypes' values and the members they add, counts as open.

Disjointness is the least relation these rules close over, so two models are disjoint only through a
finite chain of separated members, and a cycle of models separates nothing by itself. The plan
computes it once for the document, over the pairs of positions its `any_of` unions reach in each
shape, adding each pair a rule separates until a round adds none. That costs a polynomial in those
pairs, and each union's answer is the same in whatever order the unions are planned. A walk per
union that counts a pair it is still comparing as sharing finds the same pairs, but it compares a
pair once per path to it, exponentially many as required members nest, and caching what it finds
while another pair is pending lets the union planned first decide a later one's answer.

The rules are sound, not complete. A pair they leave sharing may still admit no common value, as a
`date` and a `uuid` string do, and is then inclusive: it holds every value correctly, though more
loosely than a closed sum would. `Constraints` take no part, since admitting a value leaves them to
validation, bar a tuple's `MinItems`, which these rules leave unread too, so two variants that
differ only in a bound do share values.

Whether a union is inclusive shapes its declaration, so a document that changes it breaks that
declaration's callers: a variant added or widened, a member no longer required or newly left out of
the shape, a catch-all beside a closure, or text newly carrying the union turns a closed sum
inclusive, and the reverse narrows an inclusive one. Each shape is decided apart, so a new place a
union travels adds a declaration rather than changing one: an operation that first sends a union in
a `PATCH` body adds its patch shape, inclusive where members alone separated its variants, and
leaves the closed sum read elsewhere as it was. Text is the one distant read the rules keep, since
it drops the kinds that separate variants elsewhere, so a parameter or header first carrying a union
changes every declaration whose projection that position shares.

On the golden spec (spec @319dc756, morphic @438bebe2), 515 unions are untagged, not exclusive, and
have two or more variants. Operations reach 484 of them, in 486 declarations, and none reaches the
other 31. These rules prove 383 disjoint in every declaration they take, and those lower as a closed
sum; no union is a closed sum in one shape and inclusive in another. A parameter reaches 53, 52 of
them in a query string, and values separate one, so 38 the rules would separate in JSON are
inclusive, such as `since: anyOf [string, integer]`, whose `-10` both variants read. The zone
settings stay a closed sum in the `PATCH` body that reaches them, a list of them whose elements keep
the `id` separating them. Of the 132 left, 72 have two variants that both admit an object, such as
the 17 identity providers an account's Access listing returns. Each refines one base whose `type`
enum lists all 17, and each `config` is open with no required member, so nothing separates them: one
provider's value is admitted by every other whose `config` declares none of its members at another
type.

## ModelShape — visibility projection

One logical model produces N wire shapes across the lifecycle. A shape is a visibility filter, the
lifecycles a member must be visible in one of, and whether a `PATCH` body relaxes the members it
holds. `ModelShape(type, shape)` is a **computed traversal in the plan layer**; the IR stores only
the single logical model plus visibility facts and never the projected variants (INV2;
[properties](../../ir/properties.md#visibility)). The plan keeps one `ModelShape` for each shape the
document exercises a type in, in the type's `TypePlan.Shapes`, sorted by filter, lifecycle by
lifecycle, and then by patch, so a refiner knows exactly which shapes to build: those entries are
the projections the type needs, and nothing else lists them. They are a list, since a filter is a
set of lifecycles, which no string key holds without an escaping of its own ([the
plan](overview.md)).

```go
type ModelShape struct {
    Filter    []ir.Lifecycle // the lifecycles a member must be visible in one of, sorted
    Patch     bool           // a PATCH body's: its members optional unless the binding disables it
    Props     []ir.PropID    // a model's properties visible in this shape
    Inclusive bool           // a union's: two variants can admit one value in this shape (above)
    Inferred  map[string]ir.Provenance
}
```

`Props` are drawn from `TypePlan.Flattened`, so a composed model's shapes hold what it inherits and
mixes in, each member visible as the declaration it takes says
([models](../../ir/models.md#the-flattening)). A member is visible when its `Only` names a lifecycle
of the filter, or none of the filter's class: `readOnly → Only:[read, delete, query]`;
`writeOnly → Only:[create, update]` ([properties](../../ir/properties.md#visibility));
`Visibility.None` → excluded from every projection (distinct from the zero value). A patch shape
makes properties optional unless `HTTPBinding.PatchImplicitOptionality` points at `false` (nil is
the protocol default, which flips them), and never carries a property's default onto the wire
([values](../refine/values.md)). Unknown visibility classes are opaque filters evaluated per class
([properties](../../ir/properties.md#visibility)).

## A shape reaches what it holds

Each root the [usage table](../../ir/derived-facts.md#usage) lists takes a shape. Its filter is the
one the operation names for its view, `ParameterVisibility` for the request and
`ReturnTypeVisibility` for the response, and otherwise follows the side the table puts the root on.
What the client reads takes `read`. What it writes takes its HTTP method's lifecycles: `query` for
`GET` and `HEAD`, `create` for `POST`, `create` and `update` for `PUT`, `update` for `PATCH`,
`delete` for `DELETE`, and `create` and `update` for any other method or protocol. So the request of
a webhook or a callback, which the API starts, takes `read`, and a message takes the side its
binding's direction gives it. A `PATCH` body's shape is a patch shape unless its binding sets
`PatchImplicitOptionality` to `false`. A `readOnly` member, visible in `read`, `delete` and `query`,
thus reaches a `GET`'s parameters and a `DELETE`'s body, and never the body of a `POST` or a `PUT`
the client sends.

A shape reaches every type a position of it holds, in that same shape: each member the shape holds,
a catch-all's values, a union's variants, a polymorphic base's subtypes, a map's values, and a
decorator's or alias's base. A list's or a tuple's elements take the same filter without the patch,
since a patch replaces an array whole (RFC 7396 §2): the golden spec's zone-settings `PATCH` sends a
list of settings, each carrying the `id` that names it. So every type holding a model a shape
projects is projected with it, `Target` beside `TargetCreate` and `TargetPatch`, each holding its
variants' projections in that shape and each decided apart. Shapes whose projections of a type
coincide, every model it reaches holding and requiring the same members in both, may share one
declaration ([models](../refine/models.md#visibility-projections--n-wire-shapes)), so they take one
answer, inclusive where either is. A type no shape reaches is declared once, unprojected: it holds
every member but those `Visibility.None` hides, requires only the required members no filter can
leave out, and `TypePlan.Inclusive` decides it. A shape reaching a model reaches each union it
narrows, too, every value of the model being a value of one of that union's variants
([inheritance](../refine/models.md#inheritance)).

## A model with lifecycle visibility

IR: `Model{ Properties:[id (readOnly), name (required), email, password (writeOnly, Secret)],
Additional:"closed" }`.

`ModelShape(User, {read})` = {id,name,email}; `ModelShape(User, {create})` = {name,email,password}.
`Flattened` walks `Base`, `Implements` and `Mixins` (none here). That `User` is both written and
read is the derived-fact index's, `Facts.Usage(User)` = Input|Output, which no plan field copies
([plan or refine](plan-or-refine.md)).

What refine and emit build from it: [Go](../targets/go/models.md#user-in-go),
[Kotlin](../targets/kotlin/models.md).
