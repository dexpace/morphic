# Interface extraction & request shaping

`ExtractInterfaces` derives per-language interfaces from `Implements`, plus client interfaces for
testability from the `OperationGroup` structure; a union's and a polymorphic base's interface is
`LowerUnions`' ([unions](unions.md)). `BuildRequestShaping` turns the plan's `BindingView` into
request builders, which write each input in a path, a host label, a query, a header, a cookie, a
form field or a `text/plain` body as its value's text ([values as text](text.md)), and passes an
operation's inputs positionally or inside an options object — RequestBuilder-style constructs are
generator-side, out of the IR (prior-art §2). `OperationGroup` nesting becomes sub-clients / fluent
navigation.

The parameter shape is each target's ([plan or refine](../plan/plan-or-refine.md)), with a default
rule marked `Inferred` and overridable through the target's options ([target
options](../contract/options.md)). The three first targets share one default: path parameters are
positional, in the order the URI template of the binding the target lowers names them
([bindings](../plan/bindings.md), `HTTPBinding.URITemplate`), a binding of another protocol having
none, never in the order `Operation.Params` holds them, which follows the source's declarations
rather than the URL; and every other input — query, header and cookie parameters and the body — is a
field of one request type for the operation, which it takes when it has any such input. A parameter
in the slot of a scheme the operation's options name is no input at all
([auth](../operations/auth.md#a-parameter-in-a-credentials-slot)). A parameter a client setting
fills ([client settings](../operations/client.md#client-settings)) is an optional field of the
request type, whatever its location, since a call may leave it to the client. Each target declares
that type its own way ([Go](../targets/go/operations.md#the-request-type),
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

## The body

The body is what its content's `Type` lowers to, or a string where `OpPlan.RequestText` marks that
content itself ([values as text](text.md#a-value-no-style-arranges)), as an input `ParamBind.Text`
marks is its caller's text, a string in a path, a host label or a header and a list of pairs of
strings in a query or a cookie, unless that content is a file (`Content.File`): then it
is a byte source among the request's inputs, as a `bytes` side a client writes is
([streaming](../operations/streaming.md)), whatever its `Type`. `Type` holds what the source
declares of the file ([payloads](../../ir/payloads.md)), which need not be bytes, as a type array
lowers to a union, so a target reads `File` first here as `raw` does for a response ([body
shapes](../plan/responses.md#body-shapes)).

Where `RequestText` names a field or part of the content, or its `Rest`, the request type holds the
content's fields and parts in the body's place: a field for each member the body's shape holds,
typed, optional and required as that shape declares it ([the declaration a shape
takes](../plan/types.md#the-declaration-a-shape-takes)), each one `RequestText` names a list of
pairs, and the catch-all's or a map's entries one list under `Rest`, each pair sent as a field or a
part of the body. No declaration of the model holds the pairs: a model's declaration is its shape's,
whichever contents carry it, so a content that arranges the member, as `application/json` does,
keeps it typed, and an operation first sending the model in either content changes no declaration
the other's callers build. With `Upload` sent as multipart by one operation and as JSON by another,
the first's request type holds `metadata` as pairs and the second's body is `Upload`. A field named
as another of the request type is a collision as any is
([collisions](naming.md#collisions-and-the-names-a-generation-shipped)). Two designs lost. The
member typed as pairs in the model's own declaration gives a JSON body pairs it cannot write, and a
multipart content added later would retype a member its callers build. A declaration of the model
for each such content needs a name the shape rules do not give, since which members are text follows
the content's encoding and no shape. T-48 holds it.
