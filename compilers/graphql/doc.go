// Package graphql lowers GraphQL SDL schemas — including Apollo Federation v1
// and v2 subgraphs — into the Morphic IR. It implements compilers.Compiler.
//
// Parsing is delegated to github.com/vektah/gqlparser/v2 at the SDL level
// (parser.ParseSchemas), so the compiler sees exactly what is written: no
// introspection built-ins are injected, undefined federation directives do not
// abort the parse, and type extensions arrive as their own occurrences. This
// package owns identity (structural pointer-derived IDs), type-extension
// assembly, the object/interface/union/enum/scalar/input lowering, and the
// query/mutation/subscription operation surface.
//
// Mapping highlights (ir-design §8.4): object and interface types become models
// (interfaces are Abstract; implements A & B populates Model.Implements); input
// objects become InputOnly models, and @oneOf inputs become tagged exclusive
// unions; union types become __typename-tagged unions; enums become closed
// enums; built-in scalars map to primitives while custom scalars become opaque
// nil-base scalars; field arguments become Property.Args at any depth; the three
// root types become one service with a query, mutation, and subscription group,
// with subscriptions carrying server-streaming semantics. Federation directives
// are preserved losslessly under the "federation:" extension namespace, and all
// other directive applications under "graphql:".
package graphql
