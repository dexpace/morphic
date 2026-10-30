### 4.13 Interface extraction & request shaping

`ExtractInterfaces` derives per-language interfaces from `Implements`, plus client interfaces for
testability from the `OperationGroup` structure; a union's and a polymorphic base's interface is
`LowerUnions`' (§4.4).
`BuildRequestShaping` turns the plan's `BindingView` into request builders, and passes an
operation's inputs positionally or inside an options object — RequestBuilder-style constructs are
generator-side, out of the IR (prior-art §2). `OperationGroup` nesting becomes sub-clients / fluent
navigation.

The parameter shape is each target's (§3.6), with a default rule marked `Inferred` and overridable
through the target's options (§2.5). The three first targets share one default: path parameters are
positional, in the order the primary binding's URI template names them (`HTTPBinding.URITemplate`),
never in the order `Operation.Params` holds them, which follows the source's declarations rather
than the URL; and every other input — query, header and cookie parameters and the body — is a field
of one request type for the operation, which it takes when it has any such input. Go's request type
is a `<Operation>Params` struct; Kotlin's is a class built like a model (§8.8). Two properties
decide the rule:

1. **Only the wire may fix an order.** A path template orders its parameters and a parameter list
   does not: two required strings the spec lists the other way round would swap at every positional
   call site, and still compile.
2. **An input the spec adds is an additive change.** A new field of the request type is one on every
   target. A new positional parameter breaks every Go caller, a new parameter with a default changes
   the JVM signature (§12), and Swift sees no default arguments at all.

An operation that gains its first non-path input gains the request argument, a break the surface
differ reports (§12). A dynamic target's default may differ — Python's keyword arguments are
additive as they stand — which is why the shape is per-target rather than a plan fact.
