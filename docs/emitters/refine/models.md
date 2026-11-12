# Models

## Visibility projections → N wire shapes

`ProjectVisibilityShapes` consumes the precomputed `ModelShape` list and emits the concrete
create/read/update/patch declarations the target needs — Speakeasy's "public type vs wire
(de)serialize shape" split made explicit — for every type a shape reaches, so a union or a container
holding a projected model is projected with it, as a model holding one is
([TypePlan](../plan/types.md#a-shape-reaches-what-it-holds)). `None` properties are excluded from
every projection; a patch shape makes properties optional, and a `PATCH` body's is one only where
its binding or its media type says so ([TypePlan](../plan/types.md#what-a-patch-relaxes)). A model
used as both input and output (`Facts.Usage` = Input|Output, `InputOnly` distinct identity for
GraphQL inputs) may emit two shapes. Each shape's declaration takes the name the plan gives it,
`ModelShape.Name` after the type's own: `User` for every shape that projects what a read does, and
`UserCreate`, `UserCreateUpdate`, `UserUpdate` or `UserUpdatePatch` for one that does not
([TypePlan](../plan/types.md#the-declaration-a-shape-takes)). Shapes sharing a name share the
declaration, and no target names or merges them otherwise, since the docs and a mock name the same
declarations from the same plan.

## Optionality × Nullability × Presence (INV8)

`LowerOptionality` renders the four states — `Property.Required` (wire presence) ⊥
`TypeRef.Nullable` (admits null) — plus protobuf `Property.Presence` (implicit/explicit/required) as
a *third* axis that is not nullability (protobuf has no null). A model's member is present where
`TypePlan.Required` lists it ([TypePlan](../plan/types.md)), which counts every declaration of it
and every `Requires`, not only the `Property.Required` of the declaration the flattening takes. Each
target's forms are its own ([Go](../targets/go/models.md#optionality),
[Kotlin](../targets/kotlin/models.md#optionality)).

`ClientOptional` relaxes the client-side type even when `Required=true`; `DefaultAdded` may be
suppressed for back-compat. Where a target's representation would force a *deliberate* collapse of
two states, a `Diagnostic` is emitted — never a silent conflation
([T-3](../testing/mandatory-tests.md)).

A nullable reference whose `TypeRef.Inferred` names a heuristic renders as nullable like any
other: the compiler's policy already chose to read null in, and the marker is there so that choice
stays auditable (INV6). A configuration that wants only what the source declares reads
`Nullable && Inferred == ""` instead, which is the declared bit
([type-graph](../../ir/type-graph.md#type-references)).

A merge-patch body (`application/merge-patch+json`) is where all three of an optional field's states
change the request: absent leaves the member as it is and `null` removes it. Every target sends both
through the presence box (`Optional<T?>` on Kotlin, `OptNil[T]` on Go), never through a nullable
alone.

## Inheritance

Go has only embedding. Kotlin's single class inheritance cannot hold several `Mixins`, and extending
a base would make it an open class the SDK must keep compatible for every subclass. So on all three
targets a model's `Base` and `Mixins` are flattened into it (`TypePlan.Flattened`), which holds the
members its `Implements` declare as well, since a type conforming to an interface carries them.
Subtyping survives as interfaces: `Implements` becomes an interface, and a discriminated or abstract
base a sealed interface of its subtypes, which Go holds in a struct ([Go
unions](../targets/go/unions.md)).

A union in `TypePlan.Unflattened`, an entry along the model's composition, or one an alias scalar
there stands for ([TypePlan](../plan/types.md#a-model-narrowing-a-union)), has no members to flatten
in: each instance of the model is an instance of one of the union's variants, with the rest of the
composition beside it ([models](../../ir/models.md#a-union-a-composition-names)). So each of the
three targets lowers the model as the union it narrows, distributing the composition over the
union's variants as [models](../../ir/models.md#composition-beside-a-union) distributes a union
written beside one: a variant for each entry of `TypePlan.Composed`, the model with that variant's
type in the union's place, flattened as the entry says and lowered as any model is, and entries
whose `Over` starts with the same variants nested as a union of them, as the composition over those
variants narrows a union in turn ([TypePlan](../plan/types.md#a-model-narrowing-a-union)). A model
the union lists as a variant, as `Shape: {oneOf: [Circle, Square]}` lists
`Circle: {allOf: [Shape, …]}`, narrows nothing: each of its instances is that variant, so the union
adds no member to it and it lowers as a model, one the union's sum holds. A target with intersection
types may write the composition as it stands instead and need not read `Composed`; the plan computes
the entries because the docs and the example set read the same shapes ([plan or
refine](../plan/plan-or-refine.md)). `LowerModels` declares the variants before the steps projecting
shapes and lowering unions and discriminators reach them ([steps](steps.md)).

The model takes the union's wire shape, a discriminator on the union routing each tag, and its
default, to the variant composed over the one it names. A tag naming a variant whose entry the plan
leaves out, since no value takes it ([TypePlan](../plan/types.md#a-model-narrowing-a-union)), routes
to no variant, and a default naming one is no default, so a value carrying such a tag takes the
order [unions](unions.md#a-value-no-variant-recognizes) gives one no variant recognizes. A model
left one entry is still that union, of one variant. Each variant composed here takes its entry's
`Shapes`, the model's, each holding what its flattening shows there, as a model's `ModelShape` does
([ModelShape](../plan/types.md#modelshape--visibility-projection)); a shape reaching the model
reaches the union ([a shape reaches what it holds](../plan/types.md#a-shape-reaches-what-it-holds)),
so what the variants hold is projected with them, and a variant's `readOnly` member is in no create
or patch shape of the model's. In each shape the model is inclusive and strict where the union is,
since two variants the union keeps apart stay apart once composed, and it is open as `TypePlan.Open`
says of the model ([policy](../plan/policy.md#a-union-keeps-a-variant-it-does-not-know)). Every path
to a variant composed here passes through the model, which a target declares as a union, so it
breaks a cycle through the variant as a union does ([recursive references](#recursive-references)).
Such a variant has no `TypeID`, unlike one the compiler composes, nor has a union nesting the
entries whose `Over` starts alike, so `Ctx.Syms` keys a variant by the model's and those its entry's
`Over` lists, and such a union by the model's and those the shared start of its entries' `Over`
lists, which name each from one generation to the next
([naming](naming.md#collisions-and-the-names-a-generation-shipped)), and each goes to the model's
output unit ([output units](output-units.md)).

A model adding nothing to its union is that union, which each target's lowering table aliases
([Go](../targets/go/lowering.md), [Kotlin](../targets/kotlin/lowering.md)). Any other model whose
`Composed` the plan leaves empty, for a reason
[TypePlan](../plan/types.md#a-model-narrowing-a-union) lists, is a diagnosed degrade: one narrowing
two unions, one no distribution ends, one over a union whose variants are not the object values it
admits, and one whose entries would pass the plan's limit. T-46 holds the rule.

## Unknown members

`Additional` decides for its own model alone, whatever decodes around it: a closed model nested in
an open one stays strict, and an open one nested in a closed one stays open, under a strict caller
too. Each target says how ([Go](../targets/go/models.md#user-in-go),
[Kotlin](../targets/kotlin/models.md#unknown-members)).

## Recursive references

`Facts.Recursive` says whether a value of a type can hold, at any depth, a value of its own shape,
and `Facts.Component` which types share that cycle ([derived-facts](../../ir/derived-facts.md)); the
engine derives both once per document ([who computes the
plan](../contract/emitter.md#who-computes-the-plan)). What a target must do about a cycle is its
own, since what breaks one is a property of its type system ([plan or
refine](../plan/plan-or-refine.md)). Each says what it does
([Go](../targets/go/models.md#recursive-references),
[Kotlin](../targets/kotlin/models.md#recursive-references)). A cycle a target must break lies within
one of `Facts.Component`'s components, and a shared refine helper finds the cycles a target's
by-value references close within each. T-20 holds the rule.
