# Policy and hints a document embeds

A document often carries its SDK's configuration as vendor extensions: retries and timeouts at the
document and on an operation, the statuses that count as failures, method and parameter names, the
sub-client an operation belongs to, an operation exposed under a second name, a parameter every call
sends. The compiler keeps each verbatim, an `Unmodeled` entry of reason `vendor_extension`, or
`out_of_scope` where the IR declines to model it, as it does a key its format does not define
([unmodeled](../../ir/unmodeled.md#reasons)), and left there every emitter would parse them its own
way. So one reader, `emitters/embed`, turns them into the two inputs they configure, [runtime
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
    Path []string              // the member path inside that tree's JSON form
                               // (runtime-policy.md#delivery, shaping-hints.md),
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

A key configures the node holding it: a path item's, which each of its operations holds
([keys](../../ir/unmodeled.md#keys)), configures each of them. A `policy` key on the document
configures every operation, and one on an operation configures that operation, `Layer.Policy.Ops`
keyed by its `OpID` ([layers](runtime-policy.md#layers-and-operations)). A `hint` key configures the
node's own ID, so a key mapped to `rename` on a parameter renames that parameter, on an operation
that method, and on a service that client. A `hint` key on the document configures its service where
it has only one, as every OpenAPI document does, since the document holds what its root writes;
where it has several, the key is misplaced. A key a mapping names on any other node, such as a
`policy` key on a property, configures nothing and is reported as a warning
`emitters/embed/misplaced`. So does a key the document holds for one of several objects, such as a
tag's under `openapi:tags/<n>/`: it describes that object rather than the document, and configures
nothing until the IR carries it where the object lowers (#1221). Where two keys set one field of one
node, an operation's own key beats its path item's, and any other pair is an error
`emitters/embed/conflict` that sets the field from neither, since which to take is no fact the
document states.

The value is decoded into the field `Path` names, as the tree's own JSON form reads it, refusing an
unknown member. A value of the wrong shape is reported as a warning `emitters/embed/invalid-value`
at the extension and never coerced, since the document means something by the key that the field
does not hold: a group is named by its ID, as `Remount` takes it, never by the words a document
places it under. The entry stays in `Unmodeled` either way, as a promotion leaves it
([promotion](../../ir/unmodeled.md#promotion)), so a consumer that disagrees with the mapping reads
the entry instead.

## The mapping is a configuration

The reader runs by default, and its default mapping is empty. The compiler's promotion policy ships
keys whose reading is a convention independent of any generator, such as `x-deprecated-reason`
([`extension-promotion`](../../compilers/openapi.md#extension-promotion)). No key for a retry, a
timeout or an SDK method name has one: each is a generator's own spelling, and its value's shape is
that generator's. So a configuration names the keys its documents use and what each one sets, and a
key it does not name stays where it was.

A mapping reads a key as `Unmodeled` holds it, the format's prefix included, and only from entries
of two reasons: `vendor_extension`, whose key is the source's own spelling, and `out_of_scope`,
which the IR keeps for an emitter's policy layer to read, such as a call timeout or a client-shaping
decorator ([boundaries](../../ir/boundaries.md#deliberate-exclusions)). No promotion moves either,
which is the stability #429 asks the IR to state for them. A key under any other reason, such as
`no_ir_home`, names a gap the IR may close or a construct that configures nothing, so where a
document holds a key the mapping names under one, the key reads nothing and is reported as an error
`emitters/embed/key-refused`, as is a `Target` naming no tree or a path no field has.

## Precedence

The embedded layer sits below the project's and the one the project sets for an emitter, for policy
([layers](runtime-policy.md#layers-and-operations)) and for hints alike: what a document's author
embeds is a default the SDK's author can override, and the override is reported as an info
diagnostic, so the document's author can see it was overruled. A declared IR fact still beats every
layer ([precedence](runtime-policy.md#precedence--declared-ir-facts-win-hard-rule)).

## Measured on the golden spec

The golden spec (spec @319dc756, morphic @b206dc99) carries hints nearly everywhere, each under a
generator's own key: a method name and a sub-client on 3,761 of its 3,763 operations, aliases under
other sub-clients and method names on 44, and a name on 118 parameters and on 30 properties. It
carries no retry or timeout key. A mapping reads the names into `Rename`, but no sub-client: each is
a path of names, mostly dotted, such as `dns.domainconnect`, where `Remount` and `AlsoMount` take
the ID of a group the IR has. Placing an operation by such a path is left to a compiler grouping
that reads it, which #582 proposes, and the method names hold only beside one: read alone, 1,745 of
the 3,761 share their name with another method of the group the IR places them in, which the
collision resolver then renames ([naming](../refine/naming.md)).
