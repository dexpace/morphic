# Provenance and diagnostics

Part of the [IR design](./README.md). Where a node or a finding came from, and whether it was
declared or inferred.

```go
type Provenance struct {
    Source   int              // index into Document.Sources, or NoSource (-1)
    Pointer  jsontext.Pointer // RFC 6901 pointer into that source; "" locates nothing finer than the source
    Position Position         // 1-based line and column into that source, for a finding on a raw node
    Node     string           // IR-space location: a stable ID or a path through the document's own fields
    Inferred Inferred         // "" = declared; else the heuristics that produced this node, comma-separated (docs/ir/unmodeled.md)
}

type Position struct {
    Line   int // 1-based; 0 = no position
    Column int // 1-based; 0 = the producer knows the line only
}

const NoSource = -1 // the node addresses no input file

type Diagnostic struct {
    Severity   Severity  // error | warning | info
    Code       string    // stable: "openapi/unresolved-ref", "ir/dangling-type-ref"
    Message    string
    Provenance Provenance
}
```

Everything heuristic is auditable; everything broken is reportable with an exact source location.
A reference has no `Provenance`, so a heuristic that sets a reference's `Nullable` names itself in
the reference's own `TypeRef.Inferred` ([type-graph](./type-graph.md#type-references)).

The one exception is what `NoSource` exists for. A pass reporting on the document it was handed has
no input file to name, and every real index — `0` included — names a file the document loaded, so
reusing one would make a renderer fabricate a location. A shared primitive has no one file to name
either, since every source reaches it by kind ([identity](./identity.md)). `NoSource` is therefore
the **only** out-of-table `Source` value the IR declares: a verifier accepts it and reports every
other index that addresses no declared source, so a producer inventing a second sentinel is caught
rather than tolerated.

**A refusal still has a table.** A compile that refuses returns no `Document`, yet its diagnostics
index the table that document's `Sources` would have begun with, the inputs the compile was handed,
which a document a reference names follows. The compiler names it without reading anything
(`compilers.Compiler.SourceTable`), and the engine returns it beside the findings as
`engine.Result.Sources`. A finding about a whole source carries its index and no locator, even one
made before any compiler claimed the spec, which names it as source `0`: it is about a file the
table names, and only a finding about no file at all takes `NoSource`.

**One field per kind of locator.** A consumer cannot tell a locator's kind from its spelling, so
each kind has its own field. `Pointer` is the structural locator into a source and holds nothing
but an RFC 6901 pointer, which is what lets a consumer walk it with `jsontext.Pointer`'s methods.
`Position` is for a finding made on a raw node the parser hands back, and is the form editors turn
into a link. Such a finding carries the node's `Pointer` beside it whenever the parsed tree holds
the node, so it is placed where a lowering finding about the same construct is. The position stands
alone for a part of the source no pointer reaches, such as a later document of a YAML stream or a
pair a `<<` merge key brings in, which the merged document holds at another pointer; and for the
document's top-level mapping, whose pointer is the empty one, which beside a position reads as none.
`Node` is where an IR pass reports a finding about the document itself; its spelling is the
producer's, and nothing parses it. A verifier holds a `Pointer` to RFC 6901 and a `Position` to
1-based values, and refuses either one on `NoSource`, where there is no file for it to be inside.
Nothing requires exactly one locator: a finding may carry a `Node` beside a source location, and a
node the lowering reached may carry both a pointer and a position.
