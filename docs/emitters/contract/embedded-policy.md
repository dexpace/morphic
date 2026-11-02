# Policy and hints a document embeds

A document often carries its SDK's configuration as vendor extensions: retries and timeouts at the
document and on an operation, the statuses that count as failures, method and parameter names, the
sub-client an operation belongs to, an operation exposed under a second name, a parameter every
call sends. The compiler keeps each verbatim, an `Unmodeled` entry of reason `vendor_extension`
([unmodeled](../../ir/unmodeled.md)), and left there every emitter would parse them its own way. So
one reader, `emitters/embed`, turns them into the two inputs they configure, [runtime
policy](runtime-policy.md) and [shaping hints](shaping-hints.md), each a layer below the project's.

```go
package embed

type Mapping struct {
    Keys     map[string]Target // an extension's key as Unmodeled holds it, such as
                               // "openapi:x-sdk-retries", → what it configures
    Disabled bool              // reads nothing
}

type Target struct {
    Tree string                // "policy" | "hint"
    Path []string              // the member path inside that tree's JSON form,
                               // such as ["retry", "maxAttempts"] or ["rename"]
}

type Layer struct {
    Policy policy.Layer                              // the lowest layer of runtime policy
    Hints  map[emitters.EntityID]emitters.ShapeHint  // the lowest layer of hints
}

func Read(doc *ir.Document, m Mapping) (Layer, []ir.Diagnostic)
```

The engine calls `Read` once per document, before it resolves the layers, so an emitter sees only
the resolved policy and hints and never an extension (INV1).

## What a key configures

A key configures the node holding it. A `policy` key on the document configures every operation,
and one on an operation configures that operation, `Layer.Policy.Ops` keyed by its `OpID`
([layers](runtime-policy.md#layers-and-operations)). A `hint` key configures the node's own ID, so
a key on a parameter renames that parameter, on an operation that method, and on a service that
client. A key a mapping names on any other node, such as a `policy` key on a property, configures
nothing and is reported.

The value is decoded into the field `Path` names, as the tree's own JSON form reads it, refusing an
unknown member. A value of the wrong shape is reported as a warning at the extension and never
coerced, since the document means something by the key that the field does not hold. The entry
stays in `Unmodeled` either way, as a promotion leaves it
([promotion](../../ir/unmodeled.md#promotion)), so a consumer that disagrees with the mapping reads
the entry instead.

## The mapping is a configuration

The reader runs by default, and its default mapping is empty. The compiler's promotion policy ships
keys whose reading is a convention independent of any generator, such as `x-deprecated-reason`
([`extension-promotion`](../../compilers/openapi.md#extension-promotion)). No key for a retry, a
timeout or an SDK method name has one: each is a generator's own spelling, and its value's shape is
that generator's. So a configuration names the keys its documents use and what each one sets, and a
key it does not name stays where it was.

A mapping reads a key as `Unmodeled` holds it, the format's prefix included, and only entries of
reason `vendor_extension`, whose key is the source's own spelling. That is the stability #429 asks
the IR to state for such keys. A key under any other reason, such as `no_ir_home`, names a gap the
IR may close, and a mapping naming one is refused.

## Precedence

The embedded layer sits below the project's and the emitter's, for policy
([layers](runtime-policy.md#layers-and-operations)) and for hints alike: what a document's author
embeds is a default the SDK's author can override, and the override is reported as an info
diagnostic, so the document's author can see it was overruled. A declared IR fact still beats every
layer ([precedence](runtime-policy.md#precedence--declared-ir-facts-win-hard-rule)).

## Measured on the golden spec

The golden spec (spec @319dc756, morphic @b206dc99) carries hints nearly everywhere: a method name
and a group name on 3,761 of its 3,763 operations, a name on 118 parameters and on 30 properties,
each under a generator's own key. It carries no retry or timeout key. A mapping reading those keys
gives each method the name its document chose, which an emitter would otherwise derive.
