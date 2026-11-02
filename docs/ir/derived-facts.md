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

## Capability rows

`Rows()` answers which rows of the [capability matrix](../ir-spec-matrix.md) a document exercises:
for each row's stable key, the locations of the nodes that exercise it, sorted, such as every
operation whose `Pagination` is set for `pagination`. One detector per row reads the facts the row
names, and a test holds the detectors to the matrix's keys, as the conformance corpus is held to
them: a row added to the matrix has a detector, or a reason no IR fact shows it. The engine reads
`Rows` to tell a user, before generation, which rows a target does not honor
([verification](../emitters/verification.md#before-what-a-target-honors)).


## Correlation keys

`Correlation(id)` answers what an entity of any class is called across revisions of its document,
and `Correlated(key)` answers the reverse: a key built from what the document names the entity by,
never from an array position or a file's location, so it survives the edits that move an ID
([across revisions](./identity.md#across-revisions)). An entity keeps its key through a reordered
array, a schema moved within its file or to another file, and an `allOf` branch inserted before
the one declaring a property; an operation keeps its own through a renamed path variable, and
through a renamed path where it names itself. A rename of the name the key is built from moves the
key too, and nothing derived from a document alone follows it; that is the structural tier [open
question 5](./open-questions.md) leaves open.

An entity named for itself takes a key of its own:

| Entity | Key |
|---|---|
| operation | `op:` and its declared name (`Naming.Source`, an OpenAPI `operationId`); one the source leaves unnamed takes its method and route instead, each path variable written `{}`, a webhook's event name standing for the route |
| named type | its namespace, then `type:` and its declared name; a primitive, its kind |
| service, auth scheme | its declared name |
| group | its ID, which a rule mints from a name and never from a position ([groups](./identity.md#operation-groups)) |

Every other entity is keyed by a position that names it, its holder's key extended by one segment:

| Entity | Position |
|---|---|
| parameter | its operation, where the parameter travels and its wire name, a header's in lower case; one no binding places, its name |
| property | the model declaring it, and its wire name. The anonymous models a model composes in place are part of it: their properties are keyed as its own, each wire name keying the declaration the model's [flattening](./models.md#the-flattening) takes and none it overrides |
| field argument | its property, and its name |
| header | the response, error, default or stream item declaring it, and its name in lower case |
| enum member | its enum, and the member's key ([member identity](./enums.md#member-identity)) |
| server | the service or operation naming it, and its URL template |
| callback operation with no name of its own | the operation declaring it, the method and the runtime expression keying it |
| anonymous type | a parameter's schema; the content of a request, a response, an error or a default, by the conditions and media type, and that content's stream item and file contents; a stream's events and initial message; an RPC input; a long-running operation's polling and final types; an auth scheme's credentials; a property's or a header's type, and what its encoding decodes to; a catch-all's value, key and patterns; a list's element, a map's key and value, a tuple's element by its position and its rest, and a scalar's base; an anonymous model another composes in place, by the composing model's key and `allOf`; and a variant's type, by the tag a discriminator maps to it, its wire name in a key-tagged union, else its declared name |

An entity several positions name takes the least of their keys, so its key is the same whatever
order the document declares them in. A key no two entities of a document share identifies one. Where
two share one, as two components of one name in two documents do, or two anonymous models one model
composes in place, the key identifies neither and `Correlation` reports none for both, and they key
nothing through it, because a wrong match is the failure the key exists to prevent. An entity with
no key is matched by nothing, so a consumer reads it as removed in one revision and added in the
other. The key is derived and never stored, as every fact here is: it needs only what the document
already holds, and a stored copy would add to every node a document that is already several times
its source ([consumers](./consumers.md#size)). Not built yet (#679).
