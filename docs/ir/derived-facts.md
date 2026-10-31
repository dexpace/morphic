# Derived facts

Part of the [IR design](./README.md). What `ir/irfacts` derives from a document and never stores.

What a document implies and does not state is derived by one per-document index,
`irfacts.Index` (`irfacts.New(doc)`), and never stored: nothing orders the derivations, and none
goes stale after a transform. Each of its indexes is built whole the first time one of its facts
needs it and never written again, so an `Index` is safe for concurrent use; a caller that changes
the document builds a new one. The package imports only `ir`, which keeps the definitions it
indexes: `Supertypes`, `IsSubtype`, `ExposedProps` and `AliasBase`.

The two relations `ir` walks, subtypes and exposed properties, are walked rather than indexed while
that is cheaper. An `Index` answers a relation's first questions by `ir`'s own walk, limited in
steps (`ir.IsSubtypeWithin`, `ir.ExposesWithin`), until its walks have taken as many steps as the
registry holds types, and indexes the relation then, or once another fact has. A caller asking a
few cheap questions, as `pass.Validate` does of the golden spec, pays only for the walks, and one
asking many pays for one index.

## Subtypes

`IsSubtype(t, b)` answers exactly what `ir.IsSubtype` answers ([models](./models.md#composition)),
and `Subtypes(b)` lists every declared type it holds below `b`. The index condenses the relation
into its strongly connected components and numbers them in post-order, searching depth-first down
the condensation; each component's interval runs from the least number below it to its own. A pair
whose intervals do not nest is unrelated, and a type numbered after the search reached `b`, and
before `b` itself, is below `b`. Any other pair is confirmed by `ir.IsSubtype`'s walk. There is one
only when a subtype of `b` has a supertype that is neither `b` nor below it, so a model naming two
supertypes leaves open only pairs whose base lies above it, and in a forest every question is a few
comparisons.

## Exposed properties

`Exposes(t, p)` answers exactly what `ir.ExposedProps(t)` answers about `p`
([models](./models.md)). The relation `ExposedProps` walks, a model leading to its `Base`,
`Implements` and `Mixins` and an alias scalar to its `Base`, is numbered the way the subtype
relation is, and a property declared by one model is exposed by that model and every type that
reaches it. A pair the numbering leaves open, and a property several models declare, are confirmed
by the walk. A mixin is a second parent, so this relation leaves pairs open more often than the
subtype relation does.

## The type graph

Usage and recursion read one graph with two vertices per declared type: its
*shape*, what a value of exactly that type holds, and its *range*, every shape a value declared as
that type may take. A type is *polymorphic* when the model it stands for, read through its alias
scalars, has a `Discriminator` or is `Abstract`; a value declared as any other type is of exactly
its shape.

| From | To | Through |
|---|---|---|
| a shape | the target's range when it is polymorphic, its shape otherwise | what a value holds: a model's properties and catch-all (value, key, patterns), a union's variants, a list's element, a map's key and value, a tuple's elements and rest, and what an encoded value holds once decoded (`Encoding.Schema` on a property, a scalar or a list, [constraints](./constraints.md)) |
| a shape | the target's shape | what a model composes: `Base`, `Implements`, `Mixins` |
| a shape, a range | the target's shape, its range | what a scalar stands for: its `Base` |
| a range | its own shape, and each direct subtype's range | the shapes a declared value may take |

Composition reaches a shape and never a range, so a subtype that includes its discriminated base's
members does not thereby reach the base's other subtypes, and a base routing to its own subtype is
no cycle. A discriminator's mapping and default, a default or example value, an encoding's wire
type, a template argument and an availability record name types too, but none puts a value of one
type in a value of another, so none is an edge.

## Usage

`Usage(id)` flags a type as the client's side of the wire sees it. `Input` and `Output`
say which way a value travels — the client writes it, or reads it — and `Error` and `Multipart` in
what role, so a type in an error payload carries the side it travels on beside `Error`.

| Root | Flags |
|---|---|
| an operation's parameters, request, request stream and RPC input type | `Input` |
| its responses' and its default's payloads and headers, its response stream, its long-running polling and final types | `Output` |
| its errors' and each service's common errors' payloads and headers | `Output` and `Error` |
| the messages a messaging binding sends, or receives; its reply's travel the other way | `Input`, or `Output`; both for a direction neither send nor receive |
| each channel's parameters, each auth scheme's credentials | `Input` |

A payload names its contents' types, stream items and file contents, and the headers of their
parts; a header names its type and what its encoding decodes to. An operation the API starts — a
webhook, or an operation an HTTP binding names among its callbacks — swaps `Input` and `Output`,
its errors included. A flag spreads from a root's vertex along every edge, and a property's field
arguments (`Property.Args`) make the types they name `Input`, since a client writes them to select
the field whatever it reads back. `Multipart` marks only a multipart body's own type and the types
its alias scalars stand for, not the types nested in it. An operation's `Default` is classed neither
a success nor a failure ([responses](./responses.md)), so its types are not marked `Error`. An
operation nested past `ir.MaxGroupDepth` is not read, and a type no root reaches has usage 0.

## Recursion

`Recursive(id)` reports whether a type's shape lies on a cycle of the graph — a
value of exactly that type can hold, at any depth, one of the same shape — and `Component(id)` lists
the types whose shapes share its strongly connected component. A reference to a polymorphic base
counts every subtype it may hold, so a subtype holding its own discriminated base is recursive. A
cycle through composition alone, which the IR permits, is reported too.
