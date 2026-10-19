# Parser library behaviours

The OpenAPI compiler reads a document through `github.com/speakeasy-api/openapi`, the parser
library. Its decoder builds the typed model from the YAML tree, its resolver follows `$ref`s, and
its validator reports what the model breaks. This page records what the compiler depends on the
library doing, defects included. Each entry gives the behaviour, the smallest input that shows it,
the compiler code that exists because of it, and the test that pins it.

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
  compiler guards against. A call that can end the process runs in a child process: a stack
  overflow, a panic on a goroutine the library started, or memory that grows until it is stopped.
- **Code.** The declarations that exist because of the behaviour, as `package.Name` or
  `package.Type.Member`, where `package` is `openapi` or a package under
  `compilers/openapi/internal`. Each must exist, and its doc comment must cite the entry.

To bump the library:

1. Run `go test ./compilers/openapi/internal/canary`. Each red subtest names an entry whose
   behaviour changed, and its message says what the library did instead.
2. For each, measure the new behaviour, then rewrite the entry and its pin. Decide what the code it
   names still needs: a guard against a defect the library fixed can go, and code relying on a
   behaviour the library dropped has to change.
3. Set every entry's **Verified at** to the new version.

## Resolution

### `schema-cycle-overflow`

A cycle of schema `$ref`s overflows the stack. Only the first reference from outside the cycle,
met by the walk before any member, returns an error instead (`circular reference detected`). A
member met first overflows, as does a second reference from outside, to any member. In Go a stack
overflow ends the process whatever recovers, cancels or times out around it, so a cycle has to be
refused before the resolver sees the document. Refusing one in every declaration order takes a
model of how the resolver reads `$defs`, `$anchor` and `$id`, which is what most of the code below
is.

```yaml
components:
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {$ref: '#/components/schemas/A'}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/schema-cycle-overflow`
- Code:
  - `scan.refCycles` refuses a cycle of root pointers before the document is decoded.
  - `load.reachCycle` refuses a cycle a `$anchor`, an `$id` or a nested `$defs` could close.
  - `load.loops` refuses a chain closing through the source's own file name, when external
    references are read.
  - `load.ends` keeps a chain that failed from being resumed into a loop.

#558 proposes resolving schema references in the compiler, which would retire these guards.

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

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/resolution-unbounded`
- Code:
  - `load.externalReads.once` charges each document's first read to the document budget, and
    refuses a read past it.
  - `openapi.Limits.MaxExternalDocuments` is that budget.

### `defs-order`

Whether a `#/$defs/...` reference resolves can depend on declaration order. The resolver looks for
the definition from the schema the current chain carried it to. Below, with `H` declared first,
every reference resolves. With `R` first, the walk reaches `H/properties/p` through `R`, and the
resolver fails with `definition not found: #/$defs/A`.

```yaml
components:
  schemas:
    R: {$ref: '#/components/schemas/H/properties/p'}
    H: {$defs: {A: {type: string}}, properties: {p: {$ref: '#/$defs/A'}}}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/defs-order`
- Code:
  - `load.withDefsHeld` takes every `#/$defs/...` reference out of the resolver's walk.
  - `load.resolution.resolveHeld` then resolves each to the definition `defs.Reader.Target`
    names.
  - `defs.Reader.Target` finds it from the reference's own position, whatever the order.

### `anchor-id-scope`

A `$anchor` or `$id` is found only by a reference inside the top-level schema that declares it.
From another component, a `#name` fragment is read as a JSON pointer, and fails with `jsonpointer
must start with /`. A reference to the `$id` is read as another document: refused with `external
reference not allowed` when external references are off, and fetched when they are on.

```yaml
components:
  schemas:
    A: {properties: {p: {$ref: '#x'}}}
    B: {$anchor: x, type: string}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/anchor-id-scope`
- Code:
  - `load.reach` scopes each `$anchor` and `$id` to the schema documents a node belongs to.

### `mapping-unresolved`

The library never reads a discriminator's mapping values: neither resolution nor validation reports
a value that names nothing. The load phase resolves each value itself, as a `$ref` to it would be
resolved, and that resolution recurses like any schema chain. One value into a loop of `$ref`s
returns `circular reference detected`, and a second into the same loop overflows the stack, as a
second reference from outside does in [`schema-cycle-overflow`](#schema-cycle-overflow). The
pre-parse scan does not see a loop written where the model keeps raw YAML, such as under an
extension.

```yaml
components:
  schemas:
    P: {discriminator: {propertyName: k, mapping: {a: '#/x-lib/a', b: '#/x-lib/b'}}}
x-lib:
  a: {$ref: '#/x-lib/b'}
  b: {$ref: '#/x-lib/a'}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/mapping-unresolved`
- Code:
  - `load.mappings.resolveTarget` resolves each mapping value as a `$ref` to it would be.
  - `load.mappings.chainEnds` leaves out a value whose chain does not provably end.

### `reference-object-cycle`

A cycle of `$ref`s among reference objects returns `circular reference detected`. That holds for
parameters, responses, request bodies, headers, path items, callbacks, examples, links and
security schemes, in cycles of one to three components, and for a path item, webhook or response
naming itself by position. A `$ref` into the reference object it is reached through, such as a path
item naming a key beneath itself, returns `unresolved reference`. Neither hangs nor overflows. The
pre-parse refusals of these shapes were written when the resolver held a reference's lock while
following it, so a chain re-entering one deadlocked; #574 decides whether they stay.

```yaml
components:
  parameters:
    A: {$ref: '#/components/parameters/B'}
    B: {$ref: '#/components/parameters/A'}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/reference-object-cycle`
- Code:
  - `scan.outsideRefused` refuses a chain of reference objects that re-enters a reference, or that
    cycles once it has left components, before the document is decoded.

### `root-defs`

A schema `$ref` to `#/$defs/X`, where `$defs` is written at the document's root, resolves to the
schema there, though the OpenAPI object defines no `$defs`. The compiler reads it differently. It
warns that the key is unknown, and its `$defs` rule finds a definition only in the reference's own
schema or an ancestor of it, so it reports the reference unresolved (`openapi/unresolved-ref`).
#910 decides which reading is intended; #570 asks the same of a schema under an extension.

```yaml
$defs:
  X: {type: string}
components:
  schemas:
    A: {$ref: '#/$defs/X'}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/root-defs`
- Code:
  - `defs.Reader.Target` reads no definition from the document's root.

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

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/alias-cycle`
- Code:
  - `scan.anchorCycle` refuses a recursive anchor before the document is decoded.
  - `load.refusals` runs it on the source, on the tree an overlay patched and on every external
    document.

### `alias-expansion`

Decoding builds the model afresh for every path to a node through aliases: two aliases of one
anchor decode into two separate copies, and nothing is shared. So aliases that double at each level
cost the doubling, and a document of a few hundred bytes can decode into billions of nodes.

```yaml
components:
  schemas:
    L0: &l0 {type: string}
    L1: &l1 {allOf: [*l0, *l0]}
    L2: &l2 {allOf: [*l1, *l1]}   # ... each level doubling the last
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/alias-expansion`
- Code:
  - `scan.aliasAmplification` refuses a document whose aliases expand past a multiple of its own
    size, or past the alias budget.
  - `scan.aliasWeigher` weighs a node as the decoder pays for it: once for every path to it.
  - `openapi.Limits.MaxAliasSurplus` is that budget.

### `goroutine-per-node`

Decoding starts a goroutine for every field of an object, every entry of a map and every element
of a sequence, and nothing caps how many of a map's or a sequence's are alive at once: decoding a
thousand schemas, or a thousand required names, on one processor, a fifth of them or more are
alive together. So a panic in decoding happens on a goroutine the caller's `recover` does not
reach (see [`tagged-mapping-panic`](#tagged-mapping-panic)). And what decoding collects across
those goroutines, such as the unknown keys a model records, comes back in no fixed order once it
runs on more than one processor.

```yaml
components:
  schemas:
    S0: {}
    S1: {}   # ... one goroutine each, and one for each key inside
```

- Verified at: v1.25.5
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

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/tagged-mapping-panic`
- Code:
  - `load.taggedMappingRefusal` refuses every such mapping, wherever it is, before the document is
    decoded.
  - `sourceindex.Index.TaggedMappings` finds them.
  - `load.unmarshal` recovers a panic on its own goroutine only.

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

- Verified at: v1.25.5
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

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/embedded-map-keys`
- Code:
  - `annotation.UnknownKeysNamed` grades keys the caller found itself, for a path item.
  - `operation.undeclaredPathItemKeys` finds them in the operations map and the raw mapping.

### `duplicate-key`

A key written twice is read as its last occurrence through the model, which skips the earlier ones
and warns (`validation-duplicate-key`). Through raw YAML it is read as its first: by
`jsonpointer.GetTarget` on a `*yaml.Node`, and by the model's own fallback to raw YAML for what it
does not hold, such as an extension's value.

```yaml
components:
  schemas:
    A: {type: string}
    A: {type: integer}   # the model's A
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/duplicate-key`
- Code:
  - `nodeview.dedupeLastWins` reads a mapping's repeated key as the model does.
  - `annotation.RawChildNode` does too.
  - `load.mappingKeys` records each key's first pair, as a raw read finds it.

### `unmodeled-keywords`

The schema model has no field for `$dynamicRef`, `$dynamicAnchor`, `$comment`, `$vocabulary` or
`dependentRequired`. It records each, like any key it does not know, by name alone, and leaves the
value in the schema's raw node.

```yaml
components:
  schemas:
    A: {$dynamicAnchor: n, $comment: c}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/unmodeled-keywords`
- Code:
  - `schema.dynamicAnchors` reads every `$dynamicAnchor` from the raw tree.
  - `annotation.DecidedKeywords` names `$comment`, `$dynamicAnchor` and `$dynamicRef`, which the
    unknown-key census leaves to other readers.
  - `annotation.UnknownKeywordsIn` reads every other unknown keyword's value from the raw node.

### `bare-walk-panic`

`soa.Walk` panics with `no match handler registered` when started from a reference object that is
not wrapped in its `Referenced` type: a path item, parameter, header, request body, response,
callback, example, link or security scheme. It does the same for a `JSONSchema[Concrete]`. Each
walks once wrapped, or converted to a `JSONSchema[Referenceable]`.

```go
soa.Walk(ctx, &soa.PathItem{}) // panics
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/bare-walk-panic`
- Code:
  - `load.reachedWalk` wraps each kind before walking it.

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

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/overlay-alias-clone`
- Code:
  - `scan.Aliases` refuses an overlay document whose aliases recurse or expand past its size.
  - `load.overlayRefusals` runs it before the overlay is applied.
  - `overlay.repairGrafts` replaces each alias the application leaves naming a copy with what it
    stands for.

## Validation

### `validation-version`

Validation does not read the declared version, with one exception: it refuses
`additionalOperations` before 3.2. A construct a later minor version added passes at an earlier
one, such as `webhooks` at 3.0.3. A field a later minor stopped requiring is required even where
it is optional: an operation's `responses` (optional from 3.1) and a response's `description`
(optional from 3.2). And three constructs 3.2 added fail at 3.2.0: `style: cookie` on a cookie
parameter, `discriminator.defaultMapping` and `xml.nodeType`. The last two are findings of the 3.1
meta-schema: a schema's own `Validate` discards the version it is handed, which the package-level
`oas3.Validate` reads.

```yaml
openapi: 3.2.0
components:
  schemas:
    Q: {type: object, xml: {nodeType: element}}   # refused
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/validation-version`
- Code:
  - `load.versionCheck` reports a later minor's construct at an earlier version.
  - `load.metaSchemaVersionArtifacts` drops the 3.1 meta-schema's findings in a 3.2 document.
  - `load.findRequiredArtifacts` drops a required-field finding for a field the version does not
    require.

`style: cookie` is still reported (#389).

### `empty-is-missing`

Validation reports a required field written as the empty string, or as an empty map, as missing:
`title: ''` draws `` `info.title` is required ``, and a request body's `content: {}` draws
`` `requestBody.content` is required ``. The specification requires the field to be present, and
every minor's schema admits the empty value.

```yaml
info: {title: '', version: '1'}
```

- Verified at: v1.25.5
- Pinned by: `TestLibraryBehaviour/empty-is-missing`
- Code:
  - `load.findRequiredArtifacts` drops such a finding where the field is written.
  - `load.emptyValues` names the fields it concerns.
