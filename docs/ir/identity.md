# IDs

Part of the [IR design](./README.md). How a named entity is identified: the grammar every compiler
shares, the rule for each class of ID, and what `irverify` holds them to.

```go
type TypeID string      // e.g. "t/openapi/components/schemas/User" or "t/anon/paths/~1users/get/responses/200/content/application~1json"
type OpID   string      // operation identity, same construction
type ServiceID, GroupID, ChannelID, MessageID, AuthID, PropID, ParamID, EnumMemberID string
type ServerID string
```

IDs are opaque to consumers but constructed deterministically by compilers from the source
pointer of the defining occurrence, so the same input always yields the same IDs (stable
snapshots, cachable, diffable across spec revisions). IDs are never derived from display names
and never rewritten by renames. The `dedup` pass may alias two structurally identical anonymous
types; aliases are recorded so both IDs stay resolvable.

## The ID grammar

The shape around the pointer is one grammar every compiler shares: a kind prefix (`t`, `op`, `p`,
`s`, `g`, `auth`, `param`, `e`, `server`), then the namespace, then the path. Only the path is the
format's, because a JSON Pointer, a GraphQL structural path and a protobuf fully-qualified name are
different things and nothing outside the format can compute one. Whatever the format,
`Provenance.Pointer` records the path as an RFC 6901 pointer, and the ID's path is that pointer
without its leading `/`: a protobuf full name is one segment, so `acme.v1.UserId` is recorded as
`/acme.v1.UserId`. A node a lowering *mints* rather than finds takes a namespace of its own, so no
pointer a reference can spell ever reaches it — the general form of the rule
[models](./models.md#composition-beside-a-union) states for distributed unions.

## Across documents

A source can span documents: an OpenAPI description split across files is one. A node another
document declares lives in that document's namespaces, which are the source's own qualified by the
document, so one pointer in two documents is two IDs and each ID's path is still the pointer its
node records; its `Provenance.Source` names that document's `Sources` entry
([provenance](./provenance.md)). The qualifier is an `@` and the document's location from the
source's directory, as one segment, escaped as RFC 6901 escapes a token:
`t/openapi@schemas~1pet.yaml/components/schemas/Pet` is the `Pet` component that `schemas/pet.yaml`
declares. The location, not the entry's index, qualifies it, so an ID moves only when its document
does. Operations, parameters and security schemes are named by where the source mounts or names
them, so they take the source's namespace whichever document declares them.

## Primitives

Primitives are the one exception, and only because the rule's premise does not hold for them: a
primitive occupies no source position, so there is no path for a format to own. Its identity is
its `PrimKind`, and its ID is `t/prim/<kind>` — derived by `ir.PrimTypeID` rather than by any
compiler, `ir` being the only place that can compute it. Two documents lowered from different
formats must reach that same node for the same kind, or they disagree about the identity of the
same type. The `prim` namespace is reserved for exactly those nodes: anything else addressed there
either collides with the primitive of that kind or squats the name of the next one. `irverify`
holds both halves — `ir/prim-id-not-derived` and `ir/prim-space-reserved` — for every document,
whatever produced it.

Having no source position, a primitive names no source either: its `Provenance.Source` is
`NoSource` ([provenance](./provenance.md)). An index beside the empty pointer would say that file's
whole document declared a node every source reaches by kind. `irverify` holds no producer to this so
far (#590).

## Named entities

Every named entity has an ID — including services (Thrift `service B extends A`, WSDL 2.0
interface extension, and Cap'n Proto interface inheritance all reference services by identity),
messages (AsyncAPI reuses one named message across channels, operations, and replies) and servers
(a service, a channel and an operation each name the servers serving it; [servers](./servers.md)).

## Operation groups

An operation group is the plainest minted node. OpenAPI has no construct that is a group: a Tag
Object is metadata about a name, and an operation may use a tag no Tag Object declares. The rule
that forms the group therefore mints its ID, in a namespace named for the rule — the OpenAPI
compiler's are `g/tags/<tag name>`, `g/path-prefix/<first segment>`, and the two singletons
`g/default` and `g/webhooks`. A format that declares its groups (a TypeSpec interface, a Smithy
resource, a protobuf service) takes the declaration's own coordinate as the path, as it does for
any other entity. Either way the ID is unique across the whole document, nested groups and every
service included, because a consumer keys a group by it alone; `irverify` reports a repeat as
`ir/duplicate-group-id` and an absent one as `ir/empty-group-id`. The group's `Naming` is never part
of it, so two groups that render the same words stay two entities, and what an emitter does about
that collision is a rendering decision it makes by ID ([naming](../emitters/refine/naming.md)).

## Parameters

A parameter is identified by the operation it belongs to, its name and its location. The OpenAPI
compiler's are `param/openapi/<operation mount pointer>/parameters/<name>/<in>`: the mount is the
pointer the operation is lowered at, the name is one escaped RFC 6901 token, and `<in>` is the
source's location. A path-item parameter copied into two operations is two parameters, one ID
apiece, and a path item reached from two paths gives each mount's parameters IDs rooted at that
mount. The mount rather than the declaration scopes it, which is the reason an operation is
identified by its mount (#107), and for the same reason a parameter is exempt from the agreement
below: its provenance records the declaration it was read from. No list index is used, so reordering
a parameter list renames nothing unless the list repeats a name and location. OpenAPI forbids a
repeat, but one reached through a `$ref` gets past the bundled parser's check, and both parameters
are kept: the first in list order has the bare ID, each repeat appends its occurrence number as one
more token (`…/<in>/2`), and the compiler reports it (`openapi/duplicate-parameter`), in every list
the source writes whether or not an operation lowers it. The path is injective and the only rule
minting into kind `param`, so it takes the format's own namespace, `openapi`, with no space of its
own.

Two things are left open. Nothing holds a parameter's ID beneath its own operation's: `irverify`
checks it against the grammar, and the OpenAPI compiler roots both at one mount, but a parameter
carrying another operation's path is not reported. And `Property.Args` holds `Parameter`s too, but
no compiler lowers field arguments yet, so their IDs have no grammar; the first compiler that mints
them fixes it here.

## Enum members

An enum member is identified by the enum that declares it and the value it holds:
`e/<the enum's namespace>/<the enum's path>/<key>`. The member lives in the namespace of its
enum's own `TypeID` (`openapi` for a component schema, `anon` for an inline one) and beneath its
path, so it needs no namespace of its own: only enums hold members. `irverify` holds it there
(`ir/member-id-scope`); a member records no pointer for the agreement below to compare, so that
scope is what ties the two IDs together. The key, and the number a repeated value appends after it,
are described in [enums](./enums.md#member-identity).

## What irverify holds

`irverify` holds every class of ID that has a kind prefix to the grammar, in three ways. An ID is
well-formed (`ir/id-malformed`). It carries the pointer its node records, where the node records the
pointer its path was derived from (`ir/id-provenance-disagreement`). And it lives in a namespace the
document declares (`ir/id-space-undeclared`).

The first two cannot tell every lost separator. Shape alone does not: `t/anonaddr` reads as a
namespace named `anonaddr`. Agreement does, but only where a node records its pointer. A type, a
security scheme, a server and a property are held to it. An operation is not, because its provenance
records where its body is declared and its ID where it is mounted, and the two differ for an
operation reached through a `$ref`'d path item or callback (#107); a parameter is not, for the same
reason. A service records no pointer, and a group, formed by a rule from every operation sharing a
key, has no single position to record.

The declaration is what sees them all. A producer states the namespaces it mints IDs in as
`Document.IDSpaces`, keyed by kind prefix and built from the constants it derives IDs with, as it
stamps `IRVersion`. An ID that lost its separator is then in a namespace nobody declared, whatever
its class and whether or not its node records a pointer. A type in the primitive namespace, which is
`ir`'s, needs no entry. A document declaring nothing while carrying IDs is reported once
(`ir/id-spaces-absent`). The declaration must itself be usable: keyed by real kinds, a kind with no
namespace left out, each list sorted by the byte order of its UTF-8 spelling and without repeats,
no empty namespace and none carrying the separator (`ir/id-spaces-unknown-kind`,
`ir/id-spaces-not-canonical`, `ir/id-space-invalid`). It is a vocabulary and not a usage report, so
a namespace no ID uses is not an error here; a compiler's own test holds its vocabulary to what its
corpus uses, in both directions. In this repository a lint rule also holds a compiler to building a
namespace only from a literal, which is where a path gets glued on, or through the framework's
qualifier for another document (`compile.Space.Qualified`). Channels and messages
have no prefix yet and are held to none of this.

## IDs as references

An ID held as a reference — a field, a slice element, a map key or value, anywhere but the
declaring entity's own `ID` — names an entity, so it is never empty. A reference a position may
omit is a nil pointer (`Operation.OverloadOf`, `Reply.Channel`), as an optional type is a nil
`*TypeRef` ([type-graph](./type-graph.md#type-references)). Two positions document an empty value as
meaningful, and they are the only ones: `Discriminator.Default`, whose zero value means no default,
and `Discriminator.Property`, empty when `PropertyName` or `Index` locates the tag instead
([models](./models.md#discriminator)). `irverify` reports an empty reference anywhere else as
`ir/empty-<noun>-ref`, except an empty `TypeRef.Target`, which is `ir/type-ref-no-target`
([type-graph](./type-graph.md#type-references)). `pass.Validate` reports an empty reference only in
a discriminator mapping, as a missing variant, so far (#575).

## Why IDs

This is the direct answer to oagen's name-keyed registry (silent collision merging, string-rewrite
ref fixing) and Kiota's name-keyed children (collision reconciliation logic), and adopts the
intent of TCGC's `crossLanguageDefinitionId`.
