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
