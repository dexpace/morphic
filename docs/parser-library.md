# Parser library behaviours

The OpenAPI compiler reads a document through `github.com/speakeasy-api/openapi`, the parser
library. Its decoder builds the typed model from the YAML tree, its resolver follows `$ref`s, which
the compiler never asks it to, and its overlay package reads an overlay document and the JSONPath
selectors in it, which the compiler applies itself. This page records what the compiler depends on
the library doing, defects included: each behaviour the compiler refuses input over or works
around. Each entry gives the behaviour, the smallest input that shows it, the compiler code that
exists because of it, and the test that pins it.

Not every reading the compiler shares with the library is an entry. Where the compiler reproduces
one, a test beside the reproduction asks the library the same question. The resolver oracle in
`internal/harness` pins, in a golden of its own, what the resolver does with each reference shape
it generates, and holds the compiler to survive every one. The binding oracle in
`compilers/openapi/internal/canary` holds what the compiler binds each `$ref` to, a schema's and a
reference object's, against what the resolver resolves it to, and lists every disagreement with its
reason. The validation oracle
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
   compiler reproduces names a reading that changed, the binding oracle names a `$ref` whose
   resolution moved, the resolver oracle a reference shape whose outcome moved, and the resolution
   pins in `internal/archtest` name a change to what in the library resolves a reference.
2. For each entry, measure the new behaviour, then rewrite the entry and its pin. Decide what the
   code it names still needs: a guard against a defect the library fixed can go, and code relying on
   a behaviour the library dropped has to change. Settle each other red test where it stands: an
   oracle's golden or list of disagreements is regenerated with `-update` once each is read again.
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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/schema-cycle-overflow`
- Code:
  - `load.bindAll` binds every `$ref` in the compiler, never through the resolver.

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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/mapping-unresolved`
- Code:
  - `bind.finder.mappingsOf` notes each mapping value that can name a position, to be bound.

### `reference-object-cycle`

A cycle of `$ref`s among reference objects returns `circular reference detected`. That holds for
parameters, responses, request bodies, headers, path items, callbacks, examples, links and
security schemes, in cycles of one to three components, and for a path item, webhook or response
naming itself by position. A `$ref` into the reference object it is reached through, such as a path
item naming a key beneath itself, returns `unresolved reference`. Neither hangs nor overflows, but
the error names the member the resolver met first, and a member met after it resolves to nothing
with no error, so what is reported turns on the order references are resolved in. The compiler
binds reference objects itself, as it binds schema `$ref`s, and reports such a loop once, at its
least member, as a loop of schema `$ref`s is.

```yaml
components:
  parameters:
    A: {$ref: '#/components/parameters/B'}
    B: {$ref: '#/components/parameters/A'}
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/reference-object-cycle`
- Code:
  - `load.bindAll` binds every reference object in the compiler, never through the resolver.
  - `bind.objects.closeLoop` records a loop of reference objects, to be reported once.

### `trailing-empty-token`

Through raw YAML, a pointer whose last token is empty names the node the walk has reached, not a
member keyed `""` as RFC 6901 reads it: `jsonpointer.GetTarget` on a `*yaml.Node` returns the node
before the token, and for a lone `/` the root. The resolver reads raw YAML that way wherever the
model holds none, an extension's value or another document, so a reference object spelling
`#/x-params/q/` resolves to `q`, the node its own spelling without the `/` names, and one pointer
names two things by who reads it. The compiler binds reference objects itself and reads every
pointer as RFC 6901 does, so `#/x-params/q/` names the member of `q` keyed `""`.

```yaml
components:
  parameters:
    P: {$ref: '#/x-params/q/'}   # resolves to q
x-params:
  q: {name: q, in: query}
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/trailing-empty-token`
- Code:
  - `load.bindAll` binds every reference object in the compiler, never through the resolver.
  - `bind.objects.readRef` reads a reference object's pointer as RFC 6901 does.

### `merge-list-pointer`

Through raw YAML, a pointer walk reads past a `<<` naming one mapping, but passes over one naming a
list of mappings: `jsonpointer.GetTarget` on a `*yaml.Node` finds no key a list merges in, though
the decoder merges it ([`merge-keys`](#merge-keys)), so a reference object whose pointer reads
through one into an extension's value fails as naming nothing (#1578). The compiler binds reference
objects itself and reads every mapping as the decoder does, merges included.

```yaml
components:
  parameters:
    P: {$ref: '#/x-params/q'}   # not found
x-b: &b {q: {name: q, in: query}}
x-params:
  <<: [*b]
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/merge-list-pointer`
- Code:
  - `bind.objects.readRef` reads a reference object's pointer as the decoder reads a mapping.

### `pointer-token-range`

A pointer token holding a character above U+FFFF is refused: `jsonpointer` checks each token
against a pattern whose ranges stop at U+FFFF, so a reference object spelling one fails as no valid
token (#516). RFC 6901 admits any character but `/` and `~` unescaped, and the compiler, which binds
reference objects itself, reads such a token as the member it spells.

```yaml
components:
  parameters:
    P: {$ref: '#/x-params/😀'}   # jsonpointer part must be a valid token
x-params:
  "😀": {name: q, in: query}
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/pointer-token-range`
- Code:
  - `bind.objects.readRef` admits any character in a token.

### `media-type-references`

The library models neither OpenAPI 3.2's `components/mediaTypes` nor a content entry written as a
Reference Object: the Components Object has no field for the section, a content map holds a Media
Type Object whatever its entry writes, so a `$ref` there decodes as an undefined key, and no walk
yields a reference at either. Its generic reference over a Media Type Object, the one it names no
alias for, decodes from such an entry as a reference, as any wrapper does. Its walk panics on that
reference, a type it has no handler for, and its marshaller is told of its own kinds' core models
when it is loaded, and of no other.

```yaml
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/M'}   # an undefined key $ref
            application/xml: {schema: {type: string}}
components:
  mediaTypes:
    M: {schema: {type: string}}
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/media-type-references`
- Code:
  - `resolve.ReferencedMediaType` names the reference the library names no alias for.
  - `load.MediaTypes` models each `components/mediaTypes` entry and each content entry written as a
    `$ref`, so every pass of the load phase walks them as the source's own reference objects.
  - `load.modelItem` hands a walk a model the library's walk does not know, without asking the
    library for a match it would panic building.
  - `load.registerMediaTypeReference` tells the marshaller of the reference's core model, as the
    library tells it of its own kinds'.

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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/tagged-mapping-panic`
- Code:
  - `load.taggedMappingRefusal` refuses every such mapping, wherever it is, before the document is
    decoded.
  - `sourceindex.Index.TaggedMappings` finds them.
  - `load.buildRecovered` recovers a panic on its own goroutine only.

### `recoverable-panic`

Decoding panics on some documents the YAML parser accepts, on the goroutine that called it, where
the caller's `recover` reaches the panic, unlike [`tagged-mapping-panic`](#tagged-mapping-panic).
`soa.Unmarshal` panics with `unknown node kind` on a document of nothing but whitespace, before it
decodes a node, and decoding a mapping whose `$ref` has no value as a reference object, such as raw
YAML a reference lands on, dereferences nil.

```yaml
components:
  responses:
    A: {$ref: '#/B'}
B: {$ref}   # decoded as a response, which A names
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/recoverable-panic`
- Code:
  - `load.runRecovered` turns a panic in decoding an object a reference lands on, in a walk, or in
    building the fields beside a path item's `$ref` into an error naming what was running.
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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/anchored-entry-dropped`
- Code:
  - `load.readyForModel` clears every anchor's name before a document is decoded.

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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/requirement-scopes`
- Code:
  - `auth.Registry.requirement` reads an option's names and scopes from the core.

### `duplicate-key`

A key written twice is read as its last occurrence through the model, which skips the earlier ones
and warns (`validation-duplicate-key`). Through raw YAML it is read as its first: by
`jsonpointer.GetTarget` on a `*yaml.Node`, and by the model's own fallback to raw YAML for what it
does not hold, such as an extension's value. The compiler binds every `$ref` itself and reads every
mapping as the model does, raw YAML included, so a `$ref` into raw YAML, a schema's or a reference
object's, lands on the last copy where the resolver lands on the first (`duplicate-key` in the
binding oracle's disagreements), and one pointer names one position whoever reads it.

```yaml
components:
  schemas:
    A: {type: string}
    A: {type: integer}   # the model's A
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/duplicate-key`
- Code:
  - `nodeview.flatten` reads a mapping's repeated key as the model does.
  - `nodeview.Child` reads one at the copy the model does, a merge source's at its first.
  - `load.bindAll` binds every reference object in the compiler, never through the resolver.

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

- Verified at: v1.26.3
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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/non-scalar-key`
- Code:
  - `nodeview.KeyOf` reads such a key as the empty string.
  - `oas.CheckFormat` reports it.

### `scalar-resolution`

The YAML parser tags a plain scalar by neither YAML version alone, and the compiler keeps the tag it
gives ([`values`](compilers/openapi.md#values)) but a plain date's and a plain `<<` value's, which
it tags a string ([`undecoded-scalar`](#undecoded-scalar)). As in YAML 1.1, a leading `0` is octal,
`0b` is binary, `_` separates digits and a date is a `!!timestamp`. As in YAML 1.2, `0o` is octal
and `yes`, `on` and the base-60 `1:30` are strings. So `0644` is 420, where 1.2's core schema reads
644, and `on` under `type: boolean` stays a string, where 1.1 reads true. OpenAPI recommends 1.2,
and the compiler reports no scalar the two versions read apart.

```yaml
components:
  schemas:
    S:
      properties:
        a: {type: integer, default: 0644}        # 420
        b: {type: integer, default: 0o17}        # 15
        c: {type: integer, default: 1_000}       # 1000
        d: {type: boolean, default: on}          # "on"
        e: {type: string, default: 2021-01-01}   # a timestamp, read as written
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/scalar-resolution`
- Code:
  - `value.Classify` reads a scalar by the tag the parser gave it.
  - `value.NumericLiteral` gives an `!!int` the number its prefix means.

### `undecoded-scalar`

The model decodes a string from a scalar the YAML decoder takes for one: a string, or a number or
boolean, as its text. A `!!timestamp`, which any plain date is
([`scalar-resolution`](#scalar-resolution)), a `!!merge`, which a plain `<<` is wherever it stands,
a `!!binary` and a local tag are none of these. A string field so written reads as empty, and a
Reference Object's `$ref` as no reference, its wrapper holding no object, and the model holds no
`summary` or `description` beside it either. The compiler binds reference objects itself, reading
the `$ref` as its text, so the reference is followed all the same. The validator, which the compiler
does not call, reports a type mismatch at each; the compiler's table reads any scalar as a string
and reports neither. The core keeps the node, and what is written beside it.

YAML 1.2, which OpenAPI recommends, reads a plain date and a plain `<<` as strings, as it reads any
scalar under the non-specific `!`, so the compiler tags each such date and `<<` a string in the
source and each document it reads beside it before their models are built, but a `<<` key, which
merges ([`merge-keys`](#merge-keys)). A scalar under any other tag keeps it, and still reads as
above (#1368).

```yaml
info: {title: 2021-01-01}    # GetTitle is empty
components:
  securitySchemes:
    s: {$ref: 2021-01-01}    # IsReference is false, and the core holds the node
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/undecoded-scalar`
- Code:
  - `load.mistagged` finds a plain date or `<<` value, which `load.readyForModel` tags a string.
  - `nodeview.Strings` reads a list's items by their text.
  - `auth.docsBeside` reads what is written beside it from the core.

### `unmodeled-keywords`

The schema model has no field for `$dynamicRef`, `$dynamicAnchor`, `$comment`, `$vocabulary` or
`dependentRequired`. It records each, like any key it does not know, by name alone, and leaves the
value in the schema's raw node. `Schema.IsEqual` compares the model alone, so a schema writing such
a key compares equal to one that does not.

```yaml
components:
  schemas:
    A: {$dynamicAnchor: n, $comment: c}
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/unmodeled-keywords`
- Code:
  - `bind.identifiers` holds every `$dynamicAnchor`, read from the raw tree.
  - `annotation.DecidedKeywords` names `$comment`, `$dynamicAnchor` and `$dynamicRef`, which the
    unknown-key census leaves to other readers.
  - `annotation.UnknownKeywordsIn` reads every other unknown keyword's value from the raw node.
  - `schema.bodyReading.keysRestate` judges every key a union branch writes, and every key of a
    schema the branch writes for a property or an element, reading the raw node, which holds these
    keywords, rather than the model.

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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/null-schema-is-false`
- Code:
  - `annotation.IsNullLiteral` tells a `null` from `false` by its node.
  - `annotation.IsFalseSchema` excludes a `null`, so no reader takes one for `false`.

### `failure-beneath-itself`

A schema holding a keyword that fails to decode beneath one of its own name decodes as the boolean
`false` schema, though its node is the mapping written there: an `items` above a list of `items`,
which a JSON Schema draft writes as a tuple, or above one through `properties`, and a `not` above a
`not` written as a number. Decoded on its own, the mapping is the schema it writes, the keyword that
failed alone `false`, so in a chain of such schemas the next one down decodes as `false` in turn. A
property above a list of `items` decodes as written.

```yaml
components:
  schemas:
    A: {type: array, items: {type: array, items: [{type: string}]}}
    N: {not: {required: [a], not: 5}}
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/failure-beneath-itself`
- Code:
  - `bind/typed.Builder.mend` decodes each such schema again, in place, up to `typed.MaxMends` of
    them one below another, once `Build` has built the landings.
  - `schema.unmended` tells one left past that bound, which the lowering reports and lowers as the
    top type.

### `null-type-is-empty`

A `type` written as a YAML `null`, rather than the string `"null"`, decodes as one type spelled
`""`, as an item of a list does: `GetType` returns `[""]`, which no `SchemaType` constant names and
which reads as a type written, where the keyword states nothing.

```yaml
components:
  schemas:
    A: {type: null}
    B: {type: [string, null]}
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/null-type-is-empty`
- Code:
  - `schema.writesType` and `schema.effectiveTypes` pass over the `""`, so no reader takes it for a
    type written.

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

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/context-unread`
- Code:
  - `load.walk` asks the context before each item of a walk of a model, and every walk the load
    phase makes goes through it.
  - `load.unmarshal` builds the model in the one call a cancel waits out, and the model phase asks
    the context just before it, so a cancel that cut short the walks preparing the tree is not
    waited out on a tree half prepared.

## Overlays

### `overlay-jsonpath-dialect`

`Overlay.NewPath`, the overlay package's reader of a selector, reads one as RFC 9535 only for an
overlay declaring Overlay 1.1 or later, or one whose `x-speakeasy-jsonpath` extension says
`rfc9535`. It reads any other in an older dialect of its own, in which an RFC 9535 filter does not
parse, though both versions of the Overlay Specification make every selector RFC 9535. With the
overlay's `JSONPathVersion` field set to `rfc9535` it reads every selector as RFC 9535, the
property-name operator `~` included, which RFC 9535 does not have.

```yaml
overlay: 1.0.0
actions:
  - target: '$.paths.*[?@.operationId=="putBlob"]'   # does not parse
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/overlay-jsonpath-dialect`
- Code:
  - `overlay.read` sets every overlay it reads to RFC 9535, whatever it declares.
  - `overlay.scanSelector` finds a `~`, which `overlay.readSelector` refuses.

### `jsonpath-rfc9535`

The RFC 9535 parser the overlay package reads selectors with parts from RFC 9535. It refuses a
member name `true`, `false` or `null` written after a dot, and any `\u` escape in a string, both of
which RFC 9535 reads. It reads the escape `\r` as a line feed, so a selector naming a member by one
selects another, and it accepts a control character written unescaped in a string, which RFC 9535
refuses. Its `match()` and `search()` answer true for a node lacking the value they test, where RFC
9535 answers false, so a filter calling one selects every such node. Its comparisons read a scalar
by its YAML tag: a plain date, which the compiler reads as the string it spells, equals no string
and equals every other plain date, and `0x10` compares as 0. Two objects compare equal only with
their members in the same order.

```yaml
actions:
  - target: "$.paths.*[?match(@.summary, 'Pets.*')]"   # selects every operation with no summary too
  - target: "$.paths.*[?@['x-limit'] == 0]"           # selects x-limit: 0x10 too
```

- Verified at: v1.26.3
- Pinned by: `TestLibraryBehaviour/jsonpath-rfc9535`
- Code:
  - `overlay.scanSelector` finds each of these in a selector's text, bar the comparisons, which no
    text shows.
  - `overlay.readSelector` refuses each it finds, before any action applies. A comparison applies
    as the library reads it (#1835).
