# The plan layer

`emitters/plan` (Layer 2, imports `ir` and `ir/irfacts` only). `Build` is pure, deterministic, and JSON-serializable
(INV7) so it can be cached, shared out-of-process, and golden-tested independently of any language.
Every map in the plan is keyed by a string type, since JSON names an object's members by strings
alone. `encoding/json/v2` refuses a key that does not encode as a string, as a plain struct does, so
a composite key is a map nested in a map: a struct given a text form would need an escaping of its
own, since a `TypeID` may hold any separator the text form could choose.

```go
package plan

func Build(doc *ir.Document, facts *irfacts.Index, pp Policy) (*Plan, []ir.Diagnostic)

type Plan struct {
    Ops     map[ir.OpID]OpPlan          // one entry per operation
    Types   map[ir.TypeID]TypePlan      // one entry per registry type
    Examples map[string][]Example       // by the ID of each type, property, parameter and
                                        // operation: its declared examples, else one
                                        // synthesized (../docs-and-mock.md#one-set-of-examples)
    Version string                      // hashes the document and policy it was built from (below)
}

type Example struct {
    Value    ir.Example                 // the example, as the IR spells one
    Authored bool                       // declared by the document; false = synthesized, Inferred
}
```

Every key is an IR stable ID — never `"METHOD /path"`, never a rendered name (INV3; the oagen
fragility, prior-art §1). A rename of anything an ID is not built from, such as an `operationId` or
a `title`, never orphans a plan entry: it is "same ID, new symbol", a lookup. A rename of what an ID
is built from, a path or a component name, gives the entity a new ID, and only its correlation key
(#679) carries it across the revision, where the key's own source holds, as an operation's route
does when a path variable is renamed; a path or a component renamed changes its key too ([open
question 5](../../ir/open-questions.md)). Every inferred field carries `Inferred` provenance,
propagated into diagnostics so it is auditable and disable-able (INV6). The plan's own diagnostics
are coded `emitters/plan/…`, reported once for every target rather than once by each.

The plan **classifies; it does not pick target types and it never cases anything.** It records what
it derives from the IR ("this union is discriminated, and its tag values select the variants with
IDs X/Y/Z") so every refiner chooses a strategy from the same derivation, and it leaves what the IR
states outright to be read from the IR ([plan or refine](plan-or-refine.md)). It contains no casing,
no reserved-word handling, no target types, and no runtime or target policy.

A reference the plan records — `Return.Type`, a page's item type, an error's type, a stream's
element, a variant a discriminator maps a tag to — names the type the IR names there. Where a
derivation reads a type's contents, such as the property a page's items are found under, it reads
them through `TypePlan.Alias` ([decorators](../refine/decorators.md)), and every consumer lowers a
reference it reads through `Alias`, the plan's as the IR's, so no field holds a reference already
lowered beside others that are not.

## The plan's version

`Version` is a SHA-256 hash of two things: the document's digest and the policy's encoding under
the options the document's own codec pins (INV7). The digest is SHA-256 over the document's
canonical JSON (`MarshalJSONTo`) with `sources` and `diagnostics` left out, which `Build` never
reads, so a spec reformatted, which moves only where its reports point, keeps its plan's version,
as does a build that words a report anew. Two plans therefore share a `Version` exactly when they
were built from one document, `sources` and `diagnostics` aside, under one policy, which is what
the generation manifest ([write / integrate](../write-integrate.md)) records it for. It hashes the
plan's policy alone: `naming.Policy` and the runtime policy shape no plan
([plan or refine](plan-or-refine.md)), and the manifest records them in its configuration digest
([what the manifest records](../write-integrate.md#what-the-manifest-records)).

- **The policy is data** ([plan policy](policy.md#the-policy-is-data)), so its encoding is what it
  says. A policy holding a function would encode to nothing stable: json/v2 refuses the field, its
  printed form is an address that changes every run, and leaving it out gives two policies that pick
  different responses one version.
- **The code is the build's.** `Version` names no code, so the plan cache keys on it and on the
  build, as the IR cache does ([architecture](../../architecture.md#26-ir-cache)): `cache.BuildID`
  hashes the executable. A revision bumped by hand would name neither a dirty tree nor a fix to
  `Build` that no plan golden reaches, and either would then serve a stale plan. The plan holds no
  build, as the document holds none, so a plan golden is the same from build to build, and is what
  shows a new toolchain left those bytes alone, as an IR golden is for a document's: json/v2
  promises them only within one build.
