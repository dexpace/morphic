# Interface extraction & request shaping

`ExtractInterfaces` derives per-language interfaces from `Implements`, plus client interfaces for
testability from the `OperationGroup` structure; a union's and a polymorphic base's interface is
`LowerUnions`' ([unions](unions.md)). `BuildRequestShaping` turns the plan's `BindingView` into
request builders, and passes an operation's inputs positionally or inside an options object —
RequestBuilder-style constructs are generator-side, out of the IR (prior-art §2). `OperationGroup`
nesting becomes sub-clients / fluent navigation.

The parameter shape is each target's ([plan or refine](../plan/plan-or-refine.md)), with a default
rule marked `Inferred` and overridable through the target's options ([target
options](../contract/options.md)). The three first targets share one default: path parameters are
positional, in the order the primary binding's URI template names them (`HTTPBinding.URITemplate`),
never in the order `Operation.Params` holds them, which follows the source's declarations rather
than the URL; and every other input — query, header and cookie parameters and the body — is a field
of one request type for the operation, which it takes when it has any such input. Each target
declares that type its own way ([Go](../targets/go/operations.md#the-request-type),
[Kotlin](../targets/kotlin/operations.md#the-request-type)). Two properties decide the rule:

1. **Only the wire may fix an order.** A path template orders its parameters and a parameter list
   does not: two required strings the spec lists the other way round would swap at every positional
   call site, and still compile.
2. **An input the spec adds is an additive change.** A new field of the request type is one on every
   target. A new positional parameter breaks every Go caller, a new parameter with a default changes
   the JVM signature ([surface verification](../surface-verification.md)), and Swift sees no default
   arguments at all.

An operation that gains its first non-path input gains the request argument, a break the surface
differ reports ([surface verification](../surface-verification.md)). A dynamic target's default may
differ — Python's keyword arguments are additive as they stand — which is why the shape is
per-target rather than a plan fact.

The body is what its content's `Type` lowers to, unless that content is a file (`Content.File`):
then it is a byte source among the request's inputs, as a `bytes` side a client writes is
([streaming](../operations/streaming.md)), whatever its `Type`. `Type` holds what the source
declares of the file ([payloads](../../ir/payloads.md)), which need not be bytes, as a type array
lowers to a union, so a target reads `File` first here as `raw` does for a response ([body
shapes](../plan/responses.md#body-shapes)).
