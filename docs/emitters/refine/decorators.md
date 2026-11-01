# Decorators collapse into their references

OpenAPI 3.0 admits nothing beside a `$ref`, so a document annotates one use of a shared schema by
wrapping it: `state: {allOf: [{$ref: Status}], description: the widget state}`. The compiler keeps
that losslessly, as an anonymous `Model` whose `Base` is `Status` and which carries the description
(ir-design §4.3). 3.1's spelling of the same use, `{$ref: Status, description: …}`, hoists an
anonymous `Scalar` alias over `Status` wherever no property or parameter holds the annotations
(ir-design §12.1), and so does an annotation alone on a non-carrier position,
`items: {type: string, example: x}`. None of these nodes says anything about a value that its `Base`
does not. Lowered as written, each would be a type of its own — a struct repeating a wrapped model's
fields, or a second name for a wrapped enum ([scalars](scalars.md)), named from a hint — where every
user expects the type it wraps.

**What a decorator is** is the IR's rule, stated by `ir.Decorated` (ir-design §4.10): an anonymous
`Model` or `Scalar` whose `Base` is set and which sets nothing beyond its identity, the annotations
a use site can hold, `Availability`, which no emitter interprets ([contract
rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), and `Unmodeled`,
whose entries an SDK acts on by reason, none of them by changing a type ([unmodeled
entries](unmodeled.md)) — T-15 requires as much, since an entry that kept a node from collapsing
would change the artifacts. The plan reads it through that function, so the compiler, which names a
union variant holding one by it, and the plan cannot disagree about which nodes collapse. Anything
else a node sets — a property, a required member, a constraint, `Sensitive`, an XML name, a
media-type hint — says something about values at its position, and the node lowers as it would
otherwise. A named type is never a decorator: its name is public surface, lowered by each target's
lowering table ([Go](../targets/go/lowering.md), [Kotlin](../targets/kotlin/lowering.md)).

**What the plan records.** A decorator's `TypePlan.Alias` is the first type along its `Base` chain
that is not one, found by a walk bounded by the size of the registry. A chain that returns to a
decorator it passed collapses nowhere, and each of its nodes lowers as it would otherwise. A dedup
alias, once that pass exists, is followed the same way.

**What refine does.** `CollapseAliases` runs before every other step. An aliased type emits no
declaration, and each reference to it — a `TypeRef`, or a `TypeID` a discriminator's mapping or
default names — is lowered as a reference to its `Alias`, keeping the referring reference's
`Nullable`, which answers for the whole decorator (ir-design §3.3). The decorators' annotations fold
onto the carrier holding the reference — a property, a parameter, a header or a union variant, each
of which has a field for all three — so the reference documents its input exactly as the 3.1 `$ref`
it stands for does:

- **`Docs` and `Deprecation`**, each field from the first that sets it: the carrier's own, then each
  decorator's along the chain; a carrier is deprecated where any of them is. At a property,
  parameter or header the annotations of the type `Alias` names come last, merged as ir-design
  §12.2 merges a `$ref` target's onto the carrier naming it; a union variant takes none of that
  type's, as the compiler gives a variant none.
- **`Examples`** whole: the carrier's own if it has any, else the nearest decorator's. Those of the
  type `Alias` names stay on it, since an example beside a `$ref` describes the position and never
  the target ([`schema-examples`](../../compilers/openapi.md#schema-examples)).
- **Anywhere else** — a list element, a map value, a content, a `Base` — nothing holds them. They
  stay in the IR, where the docs emitter reads them, and the plan reports each decorator some
  reference reaches whose `Docs` or `Deprecation` reach no carrier, once rather than once per
  target, as an info `emitters/plan/decorator-annotations-unplaced` at the decorator. A decorator
  nothing references generates nothing and is not reported, and neither is an unplaced example: no
  SDK renders a schema example.

The fold is a shared refine helper in `emitters/refine`, reading only the IR facts `Alias` points
along. T-16 holds it to the compiler's merge: a 3.0 decorator and the 3.1 `$ref` carrying the same
annotations beside it generate identical artifacts at every kind of position, so moving a document
from one spelling to the other changes no SDK. A union variant is one such position: the compiler
names a variant whose branch is a decorator over a `$ref` as it names the `$ref`'s
([`union`](../../compilers/openapi.md#union)).
