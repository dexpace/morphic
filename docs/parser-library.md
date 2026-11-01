# Parser library behaviours

The OpenAPI compiler reads a document through `github.com/speakeasy-api/openapi`, the parser
library. Its decoder builds the typed model from the YAML tree, its resolver follows `$ref`s, and
its overlay applier patches a YAML tree. This page records what the compiler depends on the library
doing, defects included: each behaviour the compiler refuses input over or works around. Each entry
gives the behaviour, the smallest input that shows it, the compiler code that exists because of it,
and the test that pins it.

Not every reading the compiler shares with the library is an entry. Where the compiler reproduces
one, such as how the resolver reads a JSON pointer, a test beside the reproduction asks the library
the same question. The resolver oracle in `internal/harness` pins, in a golden of its own, what the
resolver does with each reference shape it generates. The binding oracle in
`compilers/openapi/internal/canary` holds what the compiler binds each schema `$ref` to against what
the resolver resolves it to, and lists every disagreement with its reason. The validation oracle
beside it does the same for the library's validator, which the compiler does not call: it holds the
validator's findings to the compiler's own table of the specification's rules
([`validation`](compilers/openapi.md#validation)).

Code that depends on a behaviour cites its entry, as `docs/parser-library.md#<key>`, instead of
restating it. It never names a library version: the entry says what holds at the version it was
verified at, and the entry's pin says when that stops being true. A test refuses a version named in
a comment under `compilers/`, `internal/harness/` or `testdata/`.

## How this page stays true

Every entry is a heading and three fields, and the tests in `compilers/openapi/internal/canary`
read them all:

- **Key.** The heading, a stable slug. Every citation of this page in the repository must name an
  entry, so renaming or removing one fails until its citations follow.
- **Verified at.** The library version the entry was last checked against. It must be the version
  the module builds with, so a bump cannot land until every entry has been read again.
- **Pinned by.** The subtest of `TestLibraryBehaviour` named for the key. Each is a canary: it
  asserts what the library does today, so it goes red when a library change alters what the entry
  records, whether the change breaks something the compiler relies on or fixes a defect the
  compiler guards against. A call that could end the process or never return runs in a child
  process: a stack overflow, a panic on a goroutine the library started, memory that grows until it
  is stopped, or a hang.
- **Code.** The declarations that exist because of the behaviour, as `package.Name` or
  `package.Type.Member`, where `package` is `openapi` or a package under
  `compilers/openapi/internal`. Each must exist, and its doc comment must cite the entry.

To bump the library:

1. Run `go test ./...`. Each red subtest of `TestLibraryBehaviour` names an entry whose behaviour
   changed, and its message says what the library did instead. A red test beside a reading the
   compiler reproduces names a reading that changed, the resolver oracle names a reference shape
   whose outcome moved, and the resolution pins in `internal/archtest` name a change to what in the
   library resolves a reference.
2. For each entry, measure the new behaviour, then rewrite the entry and its pin. Decide what the
   code it names still needs: a guard against a defect the library fixed can go, and code relying on
   a behaviour the library dropped has to change. Settle each other red test where it stands: the
   oracle's golden is regenerated with `-update` once the guard it questions is decided.
3. Set every entry's **Verified at** to the new version.

## Resolution

### `schema-cycle-overflow`

A cycle of schema `$ref`s overflows the stack. Only the first reference from outside the cycle, met
by the walk before any member, returns an error instead (`circular reference detected`). A member
met first overflows, as does a second reference from outside, to any member. A `$ref` whose pointer
passes through a schema's `$ref` reads that schema's own members, so it lands where the pointer
ends: `#/components/schemas/A/properties/p` in `A` lands on `p`, and a `p` leading back to `A`
closes a cycle that overflows like any other. One ending in an empty token, such as
`#/components/schemas/A/` in `A`, names no member of a schema, and fails as naming nothing
(`not found`). In Go a stack overflow ends the process whatever recovers, cancels or times out
around it, so no schema `$ref` reaches the resolver: the compiler binds each one itself, reads a
loop of them as a loop, reports it once and compiles the document (#558).

```yaml
components:
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {$ref: '#/components/schemas/A'}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/schema-cycle-overflow`
- Code:
  - `load.bindSchemas` binds every schema `$ref` in the compiler, never through the resolver.
  - `load.resolvable` hands the resolver reference objects alone, never a schema.

### `resolution-unbounded`

Nothing in `references.ResolveOptions` bounds a resolution's depth, work, reads or size, and the
resolver never checks its context itself: handed a cancelled one, it resolves as usual. The
context reaches only the HTTP requests it makes, so a client that honours it can stop there; a
`VirtualFS` read takes none. It reads every document a chain of external references names, until
the chain ends or a read fails, and it reads each through the `VirtualFS` or `HTTPClient` its
options give. Those two reads are the only place the compiler can bound a resolution while it
runs.

```yaml
# d0.yaml, d1.yaml, ... each naming the next
x: {$ref: 'd1.yaml#/x'}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/resolution-unbounded`
- Code:
  - `load.externalReads.once` charges each document's first read to the document budget, and
    refuses a read past it.
  - `openapi.Limits.MaxExternalDocuments` is that budget.
  - `load.external.prepare` asks the context before handing the resolver a document it read, so a
    screen of it a cancel cut short admits nothing.

### `defs-order`

Whether a `#/$defs/...` reference resolves can depend on declaration order. The resolver looks for
the definition from the schema the current chain carried it to. Below, with `H` declared first,
every reference resolves. With `R` first, the walk reaches `H/properties/p` through `R`, and the
resolver fails with `definition not found: #/$defs/A`. The compiler finds a definition from the
position the reference is written at, so no order changes the answer.

```yaml
components:
  schemas:
    R: {$ref: '#/components/schemas/H/properties/p'}
    H: {$defs: {A: {type: string}}, properties: {p: {$ref: '#/$defs/A'}}}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/defs-order`
- Code:
  - `bind.definitions.definition` finds the definition from the reference's own position.

### `mapping-unresolved`

The library never reads a discriminator's mapping values: neither resolution nor validation reports
a value that names nothing. Resolved as a `$ref` to it would be, a value recurses like any schema
chain: one value into a loop of `$ref`s returns `circular reference detected`, and a second into
the same loop overflows the stack, as a second reference from outside does in
[`schema-cycle-overflow`](#schema-cycle-overflow). So the compiler reads each value itself, as a
`$ref` written at the discriminator, and binds it as it binds any other.

```yaml
components:
  schemas:
    P: {discriminator: {propertyName: k, mapping: {a: '#/x-lib/a', b: '#/x-lib/b'}}}
x-lib:
  a: {$ref: '#/x-lib/b'}
  b: {$ref: '#/x-lib/a'}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/mapping-unresolved`
- Code:
  - `bind.finder.mappingsOf` notes each mapping value that can name a position, to be bound.

### `reference-object-cycle`

A cycle of `$ref`s among reference objects returns `circular reference detected`. That holds for
parameters, responses, request bodies, headers, path items, callbacks, examples, links and
security schemes, in cycles of one to three components, and for a path item, webhook or response
naming itself by position. A `$ref` into the reference object it is reached through, such as a path
item naming a key beneath itself, returns `unresolved reference`. Neither hangs nor overflows, so
the compile leaves reference objects to the resolver and reports each failure at the reference, as
an unresolved reference. An earlier resolver held a reference's lock while following it, so a chain
re-entering one deadlocked, and it overflowed its stack on a cycle spelled by position; the
pre-parse refusals written against it are retired (#574).

```yaml
components:
  parameters:
    A: {$ref: '#/components/parameters/B'}
    B: {$ref: '#/components/parameters/A'}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/reference-object-cycle`
- Code:
  - `load.resolvable` hands the resolver every reference object, cycles included.

### `trailing-empty-token`

Through raw YAML, a pointer whose last token is empty names the node the walk has reached, not a
member keyed `""` as RFC 6901 reads it: `jsonpointer.GetTarget` on a `*yaml.Node` returns the node
before the token, and for a lone `/` the root. The resolver reads raw YAML that way wherever the
model holds none, an extension's value or another document, so a reference object spelling
`#/x-params/q/` resolves to `q`. The compiler reads every pointer as RFC 6901 does, so it places
what a reference object resolved to by the node the chain reached rather than by its pointer.

```yaml
components:
  parameters:
    P: {$ref: '#/x-params/q/'}   # resolves to q
x-params:
  q: {name: q, in: query}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/trailing-empty-token`
- Code:
  - `bind.docs.through` places a reference object's end at the first position on its pointer's read
    that holds the node the chain reached, which `bind.ReferenceEnd` carries.
  - `load.resolution.ends` hands the binder the node each chain reached, in whichever document writes
    the reference.

### `root-defs`

A schema `$ref` to `#/$defs/X`, where `$defs` is written at the document's root, resolves to the
schema there, though the OpenAPI object defines no `$defs`. The resolver reads the pointer from the
document's root before it tries any ancestor of the reference, so a root `$defs` answers even where
an ancestor of the reference holds an `X`. The compiler reads it the same way (#910), as it reads a
pointer to any other position the OpenAPI object leaves as raw YAML, and still warns that the key
is unknown.

The schema spelling the reference is the exception in the resolver when it has no `$id` and holds
an `X` in its own `$defs`. Resolved first, it reads the root's `X`. Once a chain has reached it, as
`C` does below, it reads its own, so the answer turns on what was resolved before. The compiler
reads the root's in every order, as OAS 3.1.1 reads a reference in a schema with no `$id` at or
above it: against the referring document's base URI.

```yaml
$defs:
  X: {type: string}
components:
  schemas:
    A: {$ref: '#/$defs/X'}
    B: {$ref: '#/$defs/X', $defs: {X: {type: integer}}}
    C: {$ref: '#/components/schemas/B'}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/root-defs`
- Code:
  - `bind.definitions.definition` reads the pointer from the document's root first, raw YAML
    included, in every order.

## Decoding

### `alias-cycle`

An alias to its own ancestor overflows the stack inside `soa.Unmarshal`, before the typed model
exists. Decoding first samples the quoting style of the first five string keys and five string
values it meets, depth first through aliases and with no record of where it has been, so a cycle
met before the sample fills recurses until the stack runs out (in `yml.getGlobalStringStyle`). A
cycle the sample gets past is harmless where the model keeps raw YAML, as under an extension. At a
schema position, typed decoding expands it without end, a goroutine per node, until memory runs
out.

```yaml
x-a: &a [*a]
components:
  schemas:
    A: &s {type: object, properties: {p: *s}}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/alias-cycle`
- Code:
  - `scan.anchorCycle` refuses a recursive anchor before the document is decoded.
  - `load.refusals` runs it on the source, on the tree an overlay patched and on every external
    document.

### `alias-expansion`

Decoding builds the model afresh for every path to a node through aliases: two aliases of one anchor
decode into two separate copies, in the core model it decodes and again in the model it populates
from that, and nothing is shared. So aliases that double at each level cost the doubling, and a
document of a few hundred bytes can decode into billions of nodes.

```yaml
components:
  schemas:
    L0: &l0 {type: string}
    L1: &l1 {allOf: [*l0, *l0]}
    L2: &l2 {allOf: [*l1, *l1]}   # ... each level doubling the last
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/alias-expansion`
- Code:
  - `scan.aliasAmplification` refuses a document whose aliases expand past a multiple of its own
    size, or past the alias budget.
  - `scan.aliasWeigher` weighs a node as the decoder pays for it: once for every path to it.
  - `openapi.Limits.MaxAliasSurplus` is that budget.

### `nesting-cost`

Decoding and walking a node cost in proportion to its depth. `soa.Walk` hands each item the path
to it, built afresh from its parent's, and decoding walks the whole model too. So a schema nested
four times as deep, with four times the nodes, costs more than six times as much to decode and to
walk, where a fixed cost per node would cost four times as much. A schema nested a few thousand
levels deep cost the compiler seconds and gigabytes before any bound of its own applied (#905).

```yaml
components:
  schemas:
    A: {type: array, items: {type: array, items: {type: string}}}   # ... nested d levels deep
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/nesting-cost`
- Code:
  - `scan.MaxNesting` bounds how deeply a document may nest.
  - `scan.Nesting` refuses a document nested past it before the parser builds it.

### `goroutine-per-node`

Decoding starts a goroutine for every key written on an object, every entry of a map and every
element of a sequence. So a panic decoding a node happens on a goroutine the caller's `recover` does
not reach (see [`tagged-mapping-panic`](#tagged-mapping-panic)), and what decoding collects across
those goroutines, such as the unknown keys a model records, comes back in no fixed order once it
runs on more than one processor. Nothing caps how many of a map's or a sequence's are alive at once:
decoding a thousand schemas, or a thousand required names, on one processor, a fifth of them or more
are alive together.

```yaml
components:
  schemas:
    S0: {}
    S1: {}   # ... one goroutine each, and one for each key inside
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/goroutine-per-node`
- Code:
  - `annotation.census` sorts the unknown keys it records.

### `tagged-mapping-panic`

A mapping tagged with anything but `!!map`, where the model reads a request body, response,
parameter, header, example, link or security scheme, panics with a nil pointer dereference. The
panic happens on a goroutine decoding started, so no `recover` of the caller's reaches it, and it
ends the process. Now and then, as those goroutines happen to be scheduled, the decode fails with
an error instead (`expected value to be of type ..., got <nil>`). The same tag on a schema, a path
item or a callback decodes.

```yaml
components:
  responses:
    A: !thing {description: d}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/tagged-mapping-panic`
- Code:
  - `load.taggedMappingRefusal` refuses every such mapping, wherever it is, before the document is
    decoded.
  - `sourceindex.Index.TaggedMappings` finds them.
  - `load.buildRecovered` recovers a panic on its own goroutine only.

### `recoverable-panic`

Decoding and resolution panic on some documents the YAML parser accepts, on the goroutine that
called them, where the caller's `recover` reaches the panic, unlike
[`tagged-mapping-panic`](#tagged-mapping-panic). `soa.Unmarshal` panics with `unknown node kind` on
a document of nothing but whitespace, before it decodes a node, and resolving a reference to a
mapping whose `$ref` has no value dereferences nil.

```yaml
components:
  responses:
    A: {$ref: '#/B'}
B: {$ref}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/recoverable-panic`
- Code:
  - `load.runRecovered` turns a panic in resolution, in a walk, or in building the fields beside a
    path item's `$ref` into an error naming what was running.
  - `load.buildRecovered` turns a panic in decoding into an `ErrParse` error.
  - `bind/typed.Builder.decodeRecovered` turns a panic in decoding raw YAML as a schema into a
    failure to build it.

### `anchored-entry-dropped`

An anchored entry of a map the model embeds is left out of the model, with no finding: a path in
`paths`, a method in a path item, a response in `responses`, an expression in a callback, and a
scheme in a security requirement. The decoder takes an anchored value there for an alias
definition. An anchored entry of a named map, such as a component, is kept.

```yaml
paths:
  /x: &item
    get: {responses: {'200': {description: ok}}}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/anchored-entry-dropped`
- Code:
  - `load.releaseAnchors` clears every anchor's name before the source is decoded.
  - `load.resolveExternal` rebuilds the model when a document the resolver decoded itself holds an
    anchor.

### `embedded-map-keys`

An object whose model embeds a map reads a key it does not define as an entry of that map, so it
records no unknown key, where any other object records one. A path item takes `other: {...}` for
an operation, and `responses` takes it for a response. `paths` and a callback, whose keys are all
entries anyway, record none either.

```yaml
paths:
  /a:
    other: {responses: {'200': {description: ok}}}   # an operation named other
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/embedded-map-keys`
- Code:
  - `annotation.UnknownKeysNamed` grades keys the caller found itself, for a path item.
  - `operation.undeclaredPathItemKeys` finds them in the operations map and the raw mapping.

### `requirement-scopes`

A Security Requirement Object's model holds a member only when its value reads as a list of strings.
A name written with `null`, a scalar or a mapping is gone from `All`, `Keys` and `Get`, and so is
one whose list holds a `null`, a non-scalar or a scalar the model does not decode
([`undecoded-scalar`](#undecoded-scalar)), so `{key: null}` reads as `{}`, the empty option. The
core still holds the name, with `Present` false, and a `<<` merge is already applied there.

```yaml
security:
  - key:                     # the model holds no member, the core holds key
  - key: 5                   # the same
  - key: [read, null]        # the same
  - key: [read, {}]          # the same
  - key: [read, 2021-01-01]  # the same
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/requirement-scopes`
- Code:
  - `auth.Registry.requirement` reads an option's names and scopes from the core.

### `duplicate-key`

A key written twice is read as its last occurrence through the model, which skips the earlier ones
and warns (`validation-duplicate-key`). Through raw YAML it is read as its first: by
`jsonpointer.GetTarget` on a `*yaml.Node`, and by the model's own fallback to raw YAML for what it
does not hold, such as an extension's value. The compiler's schema binder reads every mapping as the
model does, raw YAML included, so a schema `$ref` into raw YAML lands on the last copy where the
resolver lands on the first (`duplicate-key` in the binding oracle's disagreements). A reference
object the resolver resolves to a first copy has its content read at no position, so each schema
`$ref` in it is reported unresolved, saying so.

```yaml
components:
  schemas:
    A: {type: string}
    A: {type: integer}   # the model's A
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/duplicate-key`
- Code:
  - `nodeview.flatten` reads a mapping's repeated key as the model does.
  - `nodeview.Child` reads one at the copy the model does, a merge source's at its first.
  - `load.mappingKeys` records each key's first pair, as a raw read finds it.

### `merge-keys`

The decoder reads a mapping through every `<<` merge key it writes (`yml.ResolveMergeKeys`). The
pairs merged in come first, in the order their sources are written, an earlier source winning a key
two share and none overriding a key the mapping writes itself; the mapping's own pairs follow. A
source that writes a key twice gives its first copy, where the mapping itself gives its last
([`duplicate-key`](#duplicate-key)). A `<<` naming anything but a mapping or a list of mappings
merges nothing, and nothing reports it.

```yaml
x-base: &base {k: {type: string}, z: {type: string}}
components:
  schemas:
    S:
      properties: {a: {type: integer}, <<: *base, k: {type: boolean}}   # z, a, then k
    T:
      properties: {<<: 5, q: {type: string}}                            # q alone
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/merge-keys`
- Code:
  - `nodeview.flatten` orders and settles a mapping's pairs as the decoder does.
  - `nodeview.View.mergeSource` reads each source's pairs as a merge reads them.
  - `nodeview.Child` looks one key up through the merges by the same order and copies.
  - `oas.CheckFormat` reports a `<<` naming no mapping.

### `non-scalar-key`

A mapping key that is no scalar, a mapping or a list written as a key, is read as the empty string,
and nothing reports it. The Format section of the OpenAPI Specification limits a key to a string.

```yaml
components:
  responses:
    ? {k: v}
    : {description: x}   # read as the response keyed ""
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/non-scalar-key`
- Code:
  - `nodeview.KeyOf` reads such a key as the empty string.
  - `oas.CheckFormat` reports it.

### `scalar-resolution`

The YAML parser tags a plain scalar by neither YAML version alone, and the compiler keeps the tag it
gives ([`values`](compilers/openapi.md#values)). As in YAML 1.1, a leading `0` is octal, `0b` is
binary, `_` separates digits and a date is a `!!timestamp`. As in YAML 1.2, `0o` is octal and `yes`,
`on` and the base-60 `1:30` are strings. So `0644` is 420, where 1.2's core schema reads 644, and
`on` under `type: boolean` stays a string, where 1.1 reads true. OpenAPI recommends 1.2, and the
compiler reports no scalar the two versions read apart.

```yaml
components:
  schemas:
    S:
      properties:
        a: {type: integer, default: 0644}        # 420
        b: {type: integer, default: 0o17}        # 15
        c: {type: integer, default: 1_000}       # 1000
        d: {type: boolean, default: on}          # "on"
        e: {type: string, default: 2021-01-01}   # a timestamp, kept as written
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/scalar-resolution`
- Code:
  - `value.Classify` reads a scalar by the tag the parser gave it.
  - `value.NumericLiteral` gives an `!!int` the number its prefix means.

### `undecoded-scalar`

The model decodes a string from a scalar the YAML decoder takes for one: a string, or a number or
boolean, as its text. A `!!timestamp`, which any plain date is
([`scalar-resolution`](#scalar-resolution)), a `!!binary` and a local tag are none of these. A string
field so written reads as empty, and a Reference Object's `$ref` as no reference, so the load phase
meets none, and the model holds no `summary` or `description` beside it either. The validator, which
the compiler does not call, reports a type mismatch at each; the compiler's table reads any scalar
as a string and reports neither. The core keeps the node, and what is written beside it.

```yaml
info: {title: 2021-01-01}    # GetTitle is empty
components:
  securitySchemes:
    s: {$ref: 2021-01-01}    # IsReference is false, and the core holds the node
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/undecoded-scalar`
- Code:
  - `nodeview.Strings` reads a list's items by their text.
  - `auth.unresolvableSchemeDiags` reads a broken `$ref` from its node.
  - `auth.docsBeside` reads what is written beside it from the core.

### `unmodeled-keywords`

The schema model has no field for `$dynamicRef`, `$dynamicAnchor`, `$comment`, `$vocabulary` or
`dependentRequired`. It records each, like any key it does not know, by name alone, and leaves the
value in the schema's raw node.

```yaml
components:
  schemas:
    A: {$dynamicAnchor: n, $comment: c}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/unmodeled-keywords`
- Code:
  - `anchors.Build` reads every `$dynamicAnchor` from the raw tree.
  - `annotation.DecidedKeywords` names `$comment`, `$dynamicAnchor` and `$dynamicRef`, which the
    unknown-key census leaves to other readers.
  - `annotation.UnknownKeywordsIn` reads every other unknown keyword's value from the raw node.

### `null-schema-is-false`

A `null` written where a schema belongs decodes as the boolean `false` schema: `IsBool` reports
true and `GetBool` false, at a keyword such as `additionalProperties` or `items` and at an entry
such as a property or a component alike. Only the schema's node, a scalar tagged `!!null`, tells it
from a written `false`. A `null` states nothing about the values there, where `false` admits none.

```yaml
components:
  schemas:
    A: {properties: {p: null}, additionalProperties: null}
    C: null
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/null-schema-is-false`
- Code:
  - `annotation.IsNullLiteral` tells a `null` from `false` by its node.
  - `annotation.IsFalseSchema` excludes a `null`, so no reader takes one for `false`.

### `context-unread`

Neither the model build nor a walk over a model reads its context. Handed a cancelled one,
`marshaller.UnmarshalNode` builds the whole model, and `openapi.Walk` and `oas3.Walk` yield every
item, as they do handed a live one. The model build is one call the compiler cannot stop partway, so
a cancel landing in it waits for it to return. A walk the compiler stops itself, by asking for no
further item, as each walk of a model the load phase makes does, and the binding's walk of a schema
it builds.

```yaml
openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {type: object, properties: {p: {type: string}}}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/context-unread`
- Code:
  - `load.walk` asks the context before each item of a walk of a model, and every walk the load
    phase makes goes through it.
  - `load.unmarshal` builds the model in the one call a cancel waits out, and the model phase asks
    the context just before it, so a cancel that cut short the walks preparing the tree is not
    waited out on a tree half prepared.

## Overlays

### `overlay-alias-clone`

Applying an overlay clones each update into what its target selects, and the clone follows aliases
with no record of where it has been. So an update whose anchor names its own ancestor overflows the
stack, and one whose aliases multiply expands until memory runs out. A cloned alias names a copy of
its target outside the tree, one in no `Content` list, rather than the anchored node the clone
placed.

```yaml
actions:
  - target: $.info
    update: {x-a: &a [*a]}
```

- Verified at: v1.26.0
- Pinned by: `TestLibraryBehaviour/overlay-alias-clone`
- Code:
  - `scan.Aliases` refuses an overlay document whose aliases recurse or expand past its size.
  - `load.overlayRefusals` runs it before the overlay is applied.
  - `overlay.repairGrafts` replaces each alias the application leaves naming a copy with what it
    stands for.
