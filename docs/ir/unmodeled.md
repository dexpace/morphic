# Unmodeled constructs

Part of the [IR design](./README.md). The lossless escape hatch: what an entry holds, how its key is
scoped, why it is there, and how a heuristic may read one into a typed field.

```go
type UnmodeledReason string     // vendor_extension | validation_only | degraded_lowering |
                               // no_ir_home | out_of_scope

type UnmodeledEntry struct {
    Reason     UnmodeledReason    // why this is here rather than modeled
    Value      RawValue          // the source construct, verbatim
    Provenance Provenance        // where the construct itself was written
}

type Unmodeled map[string]UnmodeledEntry
// keys are namespaced by origin: "openapi:x-rate-limit", "smithy:aws.api#arn",
// "graphql:@key", "typespec:@myOrg/decorator", "asyncapi:bindings.kafka", "erlang:opaque"
// RawValue is the source JSON preserved verbatim.

type RawConfig map[string]RawValue
// declared protocol configuration the IR models a field for but leaves untyped inside:
// Server/Channel/Message/MessageBinding `Bindings`, `ProtocolDecl.Options`
```

`Unmodeled` is the lossless escape hatch: any source construct without a first-class IR node
survives, namespaced so two formats never collide, and typed passes can promote well-known
entries (`x-ms-pagination` → `Pagination`) without losing the original. The name states the one
property every member shares — the IR does not model it — rather than borrowing one format's
word for the concept, and rather than naming the guarantee: surviving verbatim is what the field
promises about an entry, but being unmodeled is why the entry is there at all, and `Reason`
records which flavour of unmodeled it is. OpenAPI/AsyncAPI say
*extensions*, Protobuf *options*, GraphQL *directives*, Smithy *traits*, TypeSpec *decorators*,
Erlang *module attributes* — and Protobuf's own `extensions` means something else entirely
(reserved field-number ranges), which the IR models as `Model.ExtensionRanges` and
`Property.ExtensionOf`.

## What an entry carries

Each entry carries two things beyond the value. **`Reason`** says why the construct is
unmodeled, so a consumer can take the subset it cares about: the validation emitter
[boundaries](./boundaries.md#validation-only-constructs) anticipates wants `validation_only` and not
vendor noise, while a spec linter wants `degraded_lowering` and `no_ir_home` and nothing else. It is
a property of the construct, never of the diagnostic emitted beside it — one diagnostic code
routinely spans preservation, weakening, and outright dropping, so it says nothing about why.
**`Provenance`** locates the construct itself rather than the node carrying it, so an emitter can
report on an entry at its own source position instead of the enclosing schema's — falling back to
the declaring position only where the entry combines several keywords into one synthesized object
and no single node addresses it (`openapi:if-then-else`, `openapi:contains`, `openapi:unevaluated`).
Such an entry's `Source` names the input document that wrote every keyword it combines, when one
did, and the declaring position's otherwise: an overlay that wrote the whole construct is credited
with it, and one that only added to it leaves it with the document that declares it.

## Keys

A key names the origin format, then the construct — and, where one carrier holds constructs from
more than one source object, the path between them. `openapi:x-rate-limit` is the extension on the
node's own object; `openapi:info/contact/x-id` is the contact block's, kept on the document;
`openapi:encoding/<part>/x-vendor` is one form part's, kept on the content. The scope is
load-bearing, not decoration. Most objects lower to a node carrying an `Unmodeled` map of their own
and take no scope, but the rest ride on the nearest node that does — an info block's on the
document, a path item's on each of its operations, an encoding's on the owning content — and
several of those reach one map, where two objects each writing `x-id` would be one key whose
surviving entry depended on which lowering ran last. That is the order-dependence
[models](./models.md#composition-beside-a-union) forbids for minted node IDs, and it takes the same
remedy: a namespace of its own rather than a shared one.

A scope is a fixed path the compiler chooses, and where it addresses an object the *document* names
— one form part among several, one callback among several — that name is one segment, escaped
RFC 6901 style exactly as a pointer segment is. The escaping is what makes the scope injective and
is not cosmetic: parts named `q` and `q/x-a` otherwise write `openapi:encoding/q/x-a/x-b` between
them, and which entry survives follows the order the two were declared in, silently, in both
orders. That is the failure [models](./models.md#composition-beside-a-union) describes for a minted
node reusing a pointer, reached by a different route.

Scoped and unscoped keys stay apart on top of that, because every vendor-extension name begins with
`x-` and no scope does. A scope opens with a keyword the compiler writes, and where one scope on a
carrier extends another, what it adds opens with one too. A segment the document names may begin
with `x-`, as a callback named `x-hook` keeps its extensions under `openapi:callbacks/x-hook/`, but
only after such a keyword. The same gap separates both from the non-extension entries a shared
carrier holds under those scopes, such as `openapi:encoding/<part>/prefixEncoding`.

## Reasons

| Reason | Meaning |
|---|---|
| `vendor_extension` | The source format assigns the key no semantics at all (`x-*` and every format's equivalent). The key set is unbounded and nothing can be inferred from the value. |
| `validation_only` | Validation logic rather than data shape: the structural picture is complete without it, and no target type system or sibling source format has an equivalent ([boundaries](./boundaries.md#validation-only-constructs)). |
| `degraded_lowering` | No faithful target for the shape as written — the IR has no combinator for it, or no target type system holds it — so it was lowered to a documented weaker shape with the original kept beside it ([boundaries](./boundaries.md#degraded-constructs)). |
| `no_ir_home` | Expressible in target languages, and no boundary the IR draws on purpose: the position simply has no field for it yet. A gap expected to close — each of these is a promotion candidate. |
| `out_of_scope` | A boundary the IR draws on purpose ([boundaries](./boundaries.md#deliberate-exclusions)): runtime machinery or per-target SDK policy rather than API shape. Kept so nothing is lost, but no node is coming — the intended consumer is an emitter policy layer, not a promotion pass. |

`no_ir_home` and `out_of_scope` are the pair worth keeping straight, because they read alike and
mean opposite things: the first is an admission (a field the IR should probably grow), the second
is a decision ([the list of exclusions](./boundaries.md#deliberate-exclusions), which exists
precisely so those constructs are never mistaken for oversights). A promotion pass mining the IR for
its next typed node reads `no_ir_home` and must skip `out_of_scope`; nothing about the key or the
value distinguishes them, which is the whole reason `Reason` is data rather than a comment.

## Reporting

**Reporting a kept construct is the compiler's.** It is the stage that chose to keep the construct
rather than model it, so its specification ([`docs/compilers/`](../compilers/planned-lowerings.md))
states, for every construct it keeps under a reason other than `vendor_extension`, the diagnostic
the construct draws, or that it draws none. `Document.Diagnostics` carries those reports to every
consumer, and an emitter acts on an entry's `Reason` without reporting it again ([unmodeled
entries](../emitters/refine/unmodeled.md)).

## Why not typed buckets

Sorting entries into *typed* buckets instead was considered and rejected: the defining property
of an entry is that it is unmodeled, so anything with enough known semantics to be sorted into a
bucket has enough to be modeled properly, and sorting would make the compiler guess — a
heuristic, which invariant 6 then requires to be injectable, marked `Inferred`, and disableable.
`Reason` gives the same discrimination as data, and callers already tell formats apart by key
prefix.

## Carriers

For the escape hatch to hold, **every node that can carry source metadata has an `Unmodeled`
field** — including `Response`, `ErrorCase`, `Payload`, `Content`, `Example`, `ServerVariable`,
`OAuthFlow`, and every binding struct (new spec revisions land fields on exactly these objects).
Per-file metadata (protobuf file options) is keyed by path in `Document.Unmodeled`.

## RawConfig

`RawConfig` is the neighbouring map type, not a second escape hatch: it holds declared protocol
configuration the IR models a field for and deliberately leaves untyped inside (AsyncAPI
bindings at server, channel, operation and message level; `ProtocolDecl.Options`). Its values are
bare `RawValue`, not `UnmodeledEntry` — those entries are there because the source declared them
where the IR expects them, so there is no reason to record and no unmodelled construct to locate.

## Promotion

Several typed fields model information no source format gives a keyword for, so the only way a
document can state it is a vendor extension:
`Deprecation.Message`/`Since`/`RemovalVersion`/`RemovalDate`, `Enum.Closed`, `Operation.Stability`,
`Pagination.*`, `LongRunning`, `Idempotency`, `ErrorCase.Retryable`/`Throttling`, `Enum.Flags`,
`EnumMember.Name`, `Sensitive` and `Secret`, and a format's keyword for a fact need not be its only
spelling, so `Deprecation` itself is a target too. Reading such an extension into its field is
**promotion**, and because the format assigns an `x-*` key no semantics at all, promotion is a
heuristic — invariant 6 applies to it in full. Four rules, so that no emitter has to re-derive
this from `Unmodeled` and no two derive it differently:

1. **The mapping is injectable policy, default-on and disableable**, per compiler. Its default
   contents are conventions, not standards: nothing in any specification says `x-deprecated-reason`
   means what its name suggests, so a caller may replace the mapping outright.
2. **The extension stays where it was.** A promotion is a second reading of a preserved
   `Unmodeled` entry, never a move, and the entry keeps its `vendor_extension` reason. That is what
   makes it reversible: a consumer that disagrees with the guess reads the entry instead. It also
   makes losslessness independent of the policy — a disabled promotion loses nothing.
3. **The node records that it was inferred**, in its own `Provenance.Inferred`, naming the
   heuristic. `Inferred` holds one string and a node can be reached by more than one heuristic, so
   the names are listed rather than overwritten, and a name already listed is not repeated. The
   list joins the names with commas in the order the heuristics first applied, none empty; names
   compare exactly, and the IR declares no set of them. `TypeRef.Inferred`
   ([type-graph](./type-graph.md#type-references)) lists them the same way. The list has a type of
   its own, `ir.Inferred`: `ir.JoinInferred` writes the form and `Inferred.IsCanonical` reads it. A
   conversion or a decoded document skips the join, and two spellings of one list would compare
   unequal, so `irverify` reports a stored list in any other form (`ir/inferred-not-canonical`),
   wherever it sits.
4. **A node with no `Provenance` is not promoted into.** A node carrying a `Deprecation` and no
   provenance could not satisfy rule 3, and a heuristic that cannot be audited is worse than an
   empty field. Giving such a node a provenance is a change to the IR design, and the promotion
   follows it rather than preceding it — which is the order `Parameter` went through, and it held
   this rule's only named instance until it gained the `Provenance` [operations](./operations.md)
   now gives it. `Variant` ([unions](./unions.md)) and `EnumMember` ([enums](./enums.md)) are the
   instances today: each carries a `Deprecation` and no provenance of its own, so no key maps into
   either until one of them gains one.

What the OpenAPI compiler's default mapping reads, and what it does with a value the field cannot
hold, is its rule [`extension-promotion`](../compilers/openapi.md#extension-promotion). Others of
its rules decide what reaches the IR from a document of a given minor —
[`unmodeled-dialect-fields`](../compilers/openapi.md#unmodeled-dialect-fields),
[`later-version-construct`](../compilers/openapi.md#later-version-construct), which reads the keys
beside a 3.0 schema's `$ref` as 3.1 does,
[`minor-required-fields`](../compilers/openapi.md#minor-required-fields), and
[`nullable`](../compilers/openapi.md#nullable), whose policy reads `nullable` wherever it is
written, though 3.0 confines it to a `type` beside it and 3.1 removed it, and keywords beside a
`$ref` that admit null as a nullable reference — and
[`unreferenced-components`](../compilers/openapi.md#unreferenced-components) keeps the component
entries no reference reaches.
