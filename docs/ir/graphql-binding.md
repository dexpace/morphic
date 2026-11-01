# GraphQL binding

Part of the [IR design](./README.md). A GraphQL entry-point field, and the conventions the format's
lowering follows.

```go
type GraphQLBinding struct {
    Kind      string    // "query" | "mutation" | "subscription"
    FieldPath []string  // entry-point field (nesting for namespaced schemas)
    Unmodeled  Unmodeled
}
```

GraphQL lowering: schema types → models (`InputOnly` for inputs), interfaces → `Abstract`
models with implementors listing them in `Implements` (including interfaces implementing
interfaces), unions → `WireTagged` unions discriminated by `__typename`, **`@oneOf` input
objects → `Union{Exclusive: true, WireTagged: true}` with a variant per field** (never a model
of optional properties — that is the union-to-optional-fields collapse [design rule
2](./README.md#design-rules) forbids), custom scalars → `Scalar` with `Base = nil`, entry-point
fields → operations (field args → `Params`, field type → response), nested field arguments →
`Property.Args` at any depth, subscriptions → `GraphQLBinding{Kind: "subscription"}` +
`StreamingMode: server` + `ResponseStream`.

Directive conventions (normative): applications are ordered and repeatable, so
`Unmodeled["graphql:@<name>"]` holds an **ordered JSON array** of application argument objects
— a singleton array for a single application. Directive *definitions* are preserved verbatim at
document level under `Unmodeled["graphql:directive-definitions"]`. Type-extension assembly
(`extend type` across files): the node's `Provenance` points at the original definition, each
member's `Provenance` at its defining occurrence; the occurrence list goes to
`Unmodeled["graphql:extends"]` when SDL round-trip fidelity is wanted.

Arbitrary client-composed selection sets are out of scope by design: Morphic generates SDK
surface, and the full type graph — including per-field arguments — is retained so an emitter can
still offer query builders.
