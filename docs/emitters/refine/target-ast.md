# The target AST substrate

The target AST (`emitters/golang/goast` for Go, `emitters/kotlin/ktast` for both Kotlin targets) is
a small, purpose-built, sealed-sum syntax layer — the exact code the SDK needs, at
declaration/signature granularity — modelled with the same discipline the IR uses (INV
Go-conventions): an unexported marker method, one struct per node kind, a `Kind()` accessor, a
switch-completeness test over the node enum, its kind list held to what the package declares
(`internal/decls`), as the IR's is.

Go's node set is sketched in [its document](../targets/go/emit.md#the-target-ast).

Two properties make this the right substrate: **illegal code is unrepresentable** (a field's type is
an `Expr`, not a string; a union variant that forgot its wire tag is a missing struct-literal field
the Go compiler flags inside Morphic — the antidote to the Mustache `oneOf`/`allOf` failure class),
and **every node carries its `Origin`**, so the manifest, import graph, and surface model are one
walk ([write / integrate](../write-integrate.md), [surface
verification](../surface-verification.md)).

## Every node is data

A node holds values, nodes of its own sums, and IR stable IDs, its `Origin` and a reference's target
among them. It holds no function, channel or interface outside its sums, no pointer into the
`ir.Document` or the plan, and no state computed on first read. The nodes form a tree: a node names
another declaration by ID or by name, as Go's `Named` does a type, never by a pointer to it, so no
node is reached twice. Go's `Common` keeps its fields unexported, which a codec inside the package
writes all the same. So a target's AST can take a JSON codec, kind-tagged and deterministic as the
IR's is (INV7), when a renderer outside Morphic first reads one ([the
README](../README.md#a-renderer-outside-morphic)), with no node changing shape. Until then no codec
is written. T-78 holds this section.
