# Scalars

Part of the [IR design](./README.md). Named restricted or extended primitives, and the alias edge a
position hoists over a shared target.

```go
type Scalar struct {
    TypeCommon
    Base        *TypeRef     // primitive or another scalar — extension chains preserved.
                             // nil = opaque scalar with implementation-defined representation
                             // (GraphQL custom scalars declare no base; emitters map nil-base
                             // scalars to their opaque-scalar strategy, not a fabricated chain)
    Constraints *Constraints
    Encoding    *Encoding
}
```

Covers TypeSpec `scalar X extends Y`, GraphQL custom scalars, OpenAPI `type+format` pairs that a
compiler chooses to name, Smithy simple shapes with traits. Emitters resolve the chain to the
nearest representable base and accumulate constraints/encoding along the way.

## Aliases

`Base` is also the edge an *alias* uses, and there it names a node of any kind: the [one-home
rule](./annotations.md#one-structural-home-per-declaration) gives a position that declares something
a node of its own, and where that position resolved to a shared target — a `$ref` to a model, a
union, an enum — the node it owns is a `Scalar` standing over it. So a consumer following `Base`
must expect a non-scalar and stop there rather than assume a restriction chain; `Constraints` and
annotations on such a node bind the position, not the target. `pass/validate` reads composition and
discriminator subtyping through exactly this edge.
