# Morphic IR — Model Design

The intermediate representation is the single contract between spec compilers and generator
emitters. This document specifies every node in the model, the semantics of the hard cases, and
how each source format lowers into it. The pipeline around it is defined in
[`architecture.md`](./architecture.md); the capability analysis it is designed against is
[`ir-spec-matrix.md`](./ir-spec-matrix.md); the evidence base is [`prior-art.md`](./prior-art.md).

Type sketches are written as Go because the implementation will be Go, but this document is the
spec — field names and shapes here are normative, receiver methods and helpers are not.

---

## 1. Design rules

1. **Union of capabilities.** The IR is designed against everything any supported spec can express
   (`ir-spec-matrix.md`). An emitter may ignore a capability; a compiler must never drop one, so a
   construct the IR has no place for yet is kept verbatim under `no_ir_home` (§12). Only compilers
   are staged over time, and the surface they are designed against is a goal rather than a finished
   state: a later compiler is meant to fill fields rather than add them, and where one meets a gap,
   closing it is a change of the schema, versioned as §2.1 says (architecture.md §2.5).
2. **Un-lowered.** Composition, unions, visibility, discriminators, encodings, and streaming stay
   in source-semantic form. Flattening and language fitting happen in emitters.
3. **Stable IDs, flat registries.** All named entities live in flat, ID-keyed registries and
   reference each other by ID. No node embeds another named node. Recursive types need no special
   handling; renames never break references.
4. **Three-plus-one naming.** Every nameable carries: the original source name, an IR canonical
   name, a wire name, and (where the format has one) a numeric wire ID. Language casing is never
   stored.
5. **Serializable.** The whole document round-trips through JSON deterministically (maps are
   emitted in sorted-key order; slices preserve source order). In memory it reads as the tree its
   JSON encodes: a pointer, slice or map may be shared, and is then written once per place that
   holds it, but none may hold itself, which has no encoding. `irverify` reports one that does
   (`ir/cyclic-structure`).
6. **Provenance everywhere.** Every node records where it came from and whether it was declared
   or inferred.

### Go representation of closed sums

Go has no sum types; the IR uses sealed interfaces with unexported marker methods, one concrete
struct per kind, and a `Kind()` accessor for switch-dispatch:

```go
type TypeDef interface {
    typeDef()          // sealed
    Kind() TypeKind
    Common() *TypeCommon
}
```

JSON encoding of sums uses an adjacent `kind` tag (`{"kind": "model", ...}`). Exhaustiveness is
enforced by a `switch`-completeness test over `TypeKind` values (the oagen `assertNever` lesson,
adapted to Go). Nothing is generated: the test iterates a hand-written kind list, and a second test
holds that list to the kinds the `ir` sources declare.

---

## 2. Document root

```go
type Document struct {
    IRVersion   string                 // version of the IR schema itself, semver
    IDSpaces    map[string][]string    // the namespaces the document's IDs live in, by kind prefix (§3.1);
                                       // a producer states it as it stamps IRVersion
    Name        string                 // API title, as declared; "" when the source declares none
    Version     string                 // API version string (source-declared)
    Docs        Docs
    Contact     *Contact               // {Name, URL, Email} (OpenAPI/AsyncAPI info.contact)
    License     *License               // {Name, Identifier, URL} (OpenAPI/AsyncAPI info.license)
    TermsOfService string
    Services    []Service              // ≥1 (irverify); multi-service documents are normal (TypeSpec,
                                       // stitching)
    Types       map[TypeID]TypeDef     // the type registry — the only owner of TypeDefs
    Channels    map[ChannelID]Channel  // event/messaging layer (AsyncAPI, webhooks, subscriptions, OTP processes)
    Messages    map[MessageID]Message  // message registry — messages are reused across channels and
                                       // referenced by identity from operations and replies (AsyncAPI 3)
    Auth        map[AuthID]AuthScheme  // auth scheme registry
    Servers     map[ServerID]Server    // server registry: the servers the source declares,
                                       // whichever object declares them; scopes name them by ID
                                       // (§10)
    TagDefs     []TagDef               // tag metadata registry: {Name, Docs, Parent, Kind}; tag
                                       // *membership* stays []string on the tagged nodes
                                       // (OpenAPI/AsyncAPI tag objects)
    Versions    []string               // ordered version labels when availability metadata is used
    Unmodeled   Unmodeled
    Diagnostics []Diagnostic           // accumulated by compiler + passes; not part of API meaning
    Sources     []SourceInfo           // ≥1 (irverify), one entry per input file
}

type Contact struct { Name, URL, Email string }
type License struct { Name, Identifier, URL string }
type TagDef  struct {
    Name   string
    Docs   Docs
    Parent string                 // nested-tag parent (OpenAPI 3.2 tag parent), as declared;
                                  // "" = top-level
    Kind   string                 // tag role recorded verbatim (OpenAPI 3.2 tag kind);
                                  // interpretation is grouping policy (§7.1), never read here
}
type SourceInfo struct {
    Format  string   // format and version the file was read as, name@version: "openapi@3.2",
                     // "overlay@1.0.0"
    Path    string   // the path the file was handed to the compiler under
    Hash    string   // lowercase hex SHA-256 of the file's bytes (ir.SourceHash)
    SelfURI string   // the URI the document declared for itself (OpenAPI `$self`), as written;
                     // "" = none declared
}
```

A `Document` is self-contained: no node references anything outside it.

### 2.1 `IRVersion` — the schema stamp and the compatibility policy

`IRVersion` names the generation of the IR *schema* — the shape of the document itself, not the
API it describes (that is `Version`) and not the commit that produced it. It is the one claim in a
document that a reader cannot recompute from the contents: everything else about a document can be
checked against the document, but which spelling of the schema its keys are in has to be declared.

**What moves it.** Any change to the JSON shape of a `Document`: a key added, renamed or removed, an
encoding changed, or the meaning of an existing key changed. An addition counts, because the decoder
refuses a member it does not know, so a build at the old version cannot read the new document. A
line of work that changes the shape several times bumps it once, where it lands on `main` — a
version that moves within an unmerged branch tells a consumer nothing. `ir.IRVersion` is the
constant; the version history below records what each past bump changed.

**What a bump implies.** Pre-1.0 (`0.MINOR.PATCH`), MINOR is the breaking position and every bump
so far has been breaking. There is no non-breaking bump in the history and nothing distinguishes
one, so PATCH carries no promise a consumer may read compatibility into. Moving off `0.` is a
decision about the project's stability rather than about any one shape change, and no policy for
MAJOR is written here until that decision is taken.

- *Compilers* stamp `ir.IRVersion` on every document they produce, and declare beside it the
  namespaces their IDs live in (§3.1). Of the version itself that is the whole obligation: a
  compiler never emits an older generation, and there is no option to ask it to.
- *Emitters* and any other consumer are built against exactly one generation. A bump is a change
  they must be updated for; there is no "read it anyway" mode, because the failure a stale
  consumer produces is silent — it finds no key it recognizes where a renamed one used to be and
  drops the construct rather than reporting it.

**What a consumer does on mismatch: refuse.** `ir.CompatibleVersion(v)` is the predicate, and it is
exact equality with the `ir.IRVersion` the consumer was compiled against. A differing patch, a
prerelease suffix, and a value that is not a version at all are all equally unreadable; accepting a
neighbouring version would mean claiming to know what changed between them, which is the knowledge
a version exists because nobody has. Morphic ships **no migration path** between generations — a
document written by another generation is re-compiled from its source spec, not converted.

**Where it is enforced.** In two places, one for each way a document reaches a consumer. Decoding
one from JSON refuses it outright: `ir.Document`'s decoder reads `irVersion` before any other member
and fails with `ir.ErrVersionAbsent` or `ir.ErrVersionIncompatible`, so another generation's
document is reported as one rather than as whichever renamed key it trips over first. The same
decoder refuses a member the schema does not define, a duplicate name and invalid UTF-8, whatever
options the caller passed, since each is a way for a document to be read differently from how it
was written. A document built in memory never meets that decoder, so `irverify`, the gate every
consumer runs before trusting a document, checks the stamp too. Two codes, because the two failures
name different writers: `ir/ir-version-absent` for a document carrying no stamp — a producer that
forgot, and the failure `omitempty` hides best, since a document without the key is byte-identical
to one that never had it — and `ir/ir-version-incompatible` for a stamp this build does not read.

**How it is held to the shape.** `TestIRVersion_PinsTheEncodedShape` (`ir/irversion_test.go`)
describes every type a `Document` reaches through `internal/jsonshape`: each member's JSON name, Go
type and tag options, the methods the encoder calls in place of reading fields, the constants each
type declares, and each sealed sum's variants with their tags. A name is in the description where
it gives a value its meaning: a member's Go type, so retyping a string member to an ID type moves
the description, and a numeric constant's name, since nothing else says what its number
means. A field's Go name and a string constant's are left out, because the JSON name and the
string say it themselves. The test requires the description's SHA-256 to be the one its
`generations` table records for `IRVersion`, so a shape change at an unmoved version fails. So
does a bump that changes no shape, unless its row's `meaning` says what changed: a change of
meaning, such as a lowering that starts filling a member, is the one reason to bump that the
description cannot see. The description reads declarations, so what a codec method's body writes
is outside it, left to the JSON contract tests in package `ir`. A type
from another package, such as `jsontext.Value`, is described by its name, underlying type and codec
methods, so a Go release that changes how one of them encodes moves the description too.

**Bumping.** A bump sets `IRVersion` to the next MINOR, appends the row the failing test prints,
and adds an entry to the history below, which `TestIRVersion_HistoryRecordsEachGeneration` requires
to end at `IRVersion`. The description itself is `ir/testdata/shape.golden.txt`, rewritten with
`go test ./ir -run TestIRVersion -update`, so a reviewer reads a shape change as its diff. No golden
moves. A row on `main` is never edited: two branches that claim one version append their rows in the
same place, so whichever merges second meets the other's in a conflict and takes the next version.
A change to how `jsonshape` spells a shape moves `jsonshape.Format` and appends a row describing the
current generation anew, with no bump.

**Goldens store no stamp.** `irtest.WriteGolden` and `irtest.CompareGolden` refuse a document not
stamped with `ir.IRVersion`, then encode it without the stamp, and `irtest.ReadGolden` puts it back.
Every golden still asserts the stamp, and a bump rewrites none of them. Confirm rather than trust;
the second count is zero:

```bash
ls testdata/*/openapi/*.golden.json | wc -l
grep -l '"irVersion"' testdata/*/openapi/*.golden.json | wc -l
```

#### Version history

What each generation changed in the JSON shape, and what a consumer pinned to the generation before
it sees. Changes made together in one bump are listed together under it.

- **0.2.0** — three shape changes made together. `Extensions` became `Preserved`, with `RawConfig`
  split out. `Content.ItemEncoding` became a single encoding rather than a sentinel-keyed map. The
  diagnostic code `pass/dangling-auth-ref` became `ir/dangling-auth-ref` (the `pass` package doc
  explains the rename).
- **0.3.0** — `Preserved` was renamed `Unmodeled` on every carrier, so the JSON key `preserved`
  became `unmodeled`. A consumer pinned to 0.2.0 finds no key it recognizes and drops every
  unmodeled construct in silence.
- **0.4.0** — six shape changes made together, all of them closing a gap a consumer had to read
  around rather than adding a capability:
  - `ErrorCase` became `Response`'s sibling: `Type` was **removed**, and `Name`, `Payload` and
    `Headers` took its place. A consumer pinned to 0.3.0 finds no `type` on an error case and cannot
    reach its models at all; one that reads the new fields gets the status spelling, the headers and
    every media type, which 0.3.0 dumped into `Unmodeled` whatever their arity.
  - `Payload` gained `Required`. Body optionality stopped being an inverted `Unmodeled` sentinel
    read by absence, so a consumer that still reads `openapi:required` now finds nothing and reads
    every body as required.
  - `Parameter` gained `Provenance`, not `omitempty`, and with it `x-sunset` promotion at the
    parameter position.
  - `Deprecation` gained `RemovalDate`. `x-sunset` promotes into it rather than into
    `RemovalVersion`, so a consumer reading a removal date off the version field now finds it
    empty.
  - `Encoding` gained `Schema`, giving `contentSchema` a home at scalar positions.
  - `Constraints.ExclusiveMin` and `ExclusiveMax` changed from bool to a decimal string carrying the
    bound itself, so the two dialects' exclusive bounds no longer lose one keyword to the other. The
    JSON type of both keys changed; a consumer decoding them as booleans fails rather than degrades.
- **0.5.0** — the IR moved onto `encoding/json/v2` and absence got one spelling:
  - `Operation.Auth`, `Service.Auth` and `Server.Auth` write nil (inherit) as an absent key rather
    than `null`. An empty list, explicitly public, is still `[]`.
  - A `Value`'s bytes, list and object payloads, and a `CtorValue`'s args, are omitted when empty
    rather than written as `null`.
  - Strings use RFC 8785's minimal escaping, so `<`, `>` and `&` are written as themselves rather
    than as `\u003c`, `\u003e` and `\u0026`.
  - Decoding refuses what it used to take in silence: a member the schema does not define, a
    duplicate name, a string that is not UTF-8, and a missing or foreign `irVersion`, which is read
    before any other member.
- **0.6.0** — each kind of `Provenance` locator got its own key. `pointer` holds only an RFC 6901
  pointer; a line and column moved to `position`, and an IR pass's location in the document itself
  moved to `node`. A consumer pinned to 0.5.0 knows neither new key and reads those findings as
  unlocated.
- **0.7.0** — two additions made together:
  - `OperationGroup` gained `ID`. A group had been a name and a list of operations, so a consumer
    could key a sub-client only by its name, which two groups can share: the declared tag `default`
    and the group untagged operations fall into render from the same words. A consumer pinned to
    0.6.0 finds no `id` on a group and has nothing else to key it by.
  - `Document` gained `IDSpaces`, the namespaces its IDs live in, by kind prefix. Without it nothing
    could see an ID that lost the separator before its path in an operation, a service or a group,
    which record no pointer to agree with. A consumer pinned to 0.6.0 finds no `idSpaces` and
    cannot tell which namespaces a producer minted in.
- **0.8.0** — parameters and enum members gained an identity:
  - `Parameter` gained `ID`. A parameter had been named by its name, which one operation can repeat:
    `id` in the path and `id` in the query are two parameters. `HTTPParamBinding.Param`,
    `ParamPath.Param` and `Idempotency.TokenParam` changed from the parameter's name to its
    `ParamID`, so the three references name one parameter instead of two. A consumer pinned to 0.7.0
    finds no `id` on a parameter and reads those three as names; it cannot tell which of two
    same-named parameters a binding places on the wire. The `param` kind joined `IDSpaces`.
  - `EnumMember` gained `ID`. A member had been a name and a value, so a consumer could key it only
    by a name that two members can render alike (`a-b` and `a_b` are the same words) or by its
    position, which reordering changes. A consumer pinned to 0.7.0 finds no `id` on a member and
    has nothing else to key it by. The `e` kind joined `IDSpaces`.
- **0.9.0** — three additions, and one member that started being filled:
  - `Payload` gained `Docs`, a body's own documentation as against any one media type's. A consumer
    pinned to 0.8.0 finds no `docs` on a request body and drops its description.
  - `TagDef` gained `Parent` and `Kind`, OpenAPI 3.2's tag hierarchy and tag role, as declared. A
    consumer pinned to 0.8.0 reads every tag as top-level and of no kind.
  - The OpenAPI compiler now nests a tag's group under its declared parent's in
    `OperationGroup.Groups`, which no OpenAPI document filled before. The shape is unchanged, but a
    consumer that read only the top level misses every operation of a nested group.
- **0.10.0** — a tuple states what may follow its elements, and its bounds:
  - `Tuple` gained `Rest`, the type of every element past `Elems`, nil when none may follow. The
    OpenAPI compiler had lowered `prefixItems` alone and kept a trailing `items` verbatim under
    `Unmodeled["openapi:items-after-prefix"]`, so a tuple writing no `items`, which JSON Schema
    leaves open, read as closed, and one writing `items: false` was reported as open. That entry is
    no longer written. A consumer pinned to 0.9.0 finds no `rest` and reads every tuple as closed.
  - `Tuple` gained `Constraints`, the field `List` already had, so `minItems`, `maxItems` and
    `uniqueItems` beside `prefixItems` reach it rather than `Unmodeled`, where a consumer pinned to
    0.9.0 looks for them.
- **0.11.0** — `HTTPBinding` gained `WebhookName`, the OpenAPI `webhooks`-map key that declared a
  webhook operation, and a webhook binding stopped writing `uriTemplate`. The key names an event, not
  an RFC 6570 template, so a `{tenant}` in it read as a variable nothing binds. A consumer pinned to
  0.10.0 finds a webhook's name in no key it reads.
- **0.12.0** — `SourceInfo` gained `SelfURI`, the URI a document declared for itself (OpenAPI 3.2
  `$self`), which the OpenAPI compiler had dropped. A consumer pinned to 0.11.0 finds no `selfURI`
  and cannot tell what URI a document gave itself.
- **0.13.0** — changes to auth (§9) and to what a provenance position means (§13):
  - `OAuthFlow.Scopes` became a list of `{name, description}` in source order, where it was a map
    from name to description that sorted them. A consumer pinned to 0.12.0 expects an object and
    cannot decode the list.
  - `OAuthFlow` gained `TokenEndpointAuthMethods`, how a client authenticates to the token
    endpoint, and `AuthScheme` gained `Credentials`, a declared credential type. A consumer pinned
    to 0.12.0 finds neither.
  - A `Position` no longer marks a finding that has no pointer: one made on a raw node carries the
    node's `Pointer` beside its line and column. A consumer that read a position as "no pointer"
    now finds both.
- **0.14.0** — a map has one shape (§4.6):
  - The OpenAPI compiler lowers an object whose one member fact is its `additionalProperties` to a
    `MapT`, where it had built a `Model` with no properties and an `AdditionalProps`. A consumer
    pinned to 0.13.0 finds a node of kind `map` where it read a model.
  - `MapT.Key` became a pointer, nil for string keys as `AdditionalProps.Key` is, and `MapT` gained
    `Constraints`, the bound on how many entries an instance may carry. A consumer pinned to 0.13.0
    decodes a map with no `key` to a reference naming no type, and reads no entry bound. A nil key
    is the one spelling of a string key on either, and `irverify` rejects one naming the `string`
    primitive.
  - `additionalProperties: true` became a catch-all typed by the top type, as `{}` already was,
    where it had lowered to nothing, like an absent keyword. A consumer pinned to 0.13.0 reads a
    model writing `true` beside its properties as stating nothing about the rest.
- **0.15.0** — a union a discriminator routes is `WireTagged` (§4.4). The shape is unchanged, but
  the OpenAPI compiler had lowered every discriminated `oneOf` and `anyOf` with `wireTagged` false
  beside its `discriminator`, a combination the tag modes do not define. A consumer pinned to
  0.14.0 that follows them reads each such union as untagged and ignores its tag. `irverify` now
  refuses the combination (`ir/union-discriminator-untagged`).
- **0.16.0** — usage is derived, not stored (§4.9): `TypeCommon` lost `Usage`, and `UsageFlags`
  moved to `ir/irfacts`, whose `Index.Usage` derives it. No compiler or pass ever wrote the member,
  so every document carried none, and a consumer pinned to 0.15.0 loses nothing it read; the
  version check refuses it a 0.16.0 document all the same.
- **0.17.0** — a spec split across files is one document (§3.1, §13). The shape is unchanged, but
  the OpenAPI compiler lists every document a reference reads in `Sources`, after the source and
  its overlay, and lowers what each declares where it declares it: its `Provenance.Source` names
  that entry, and its IDs are in namespaces qualified by the document's location
  (`t/openapi@common.yaml/...`). It had lowered no schema another document holds and placed a
  parameter, response or other object read from one at the source's use site. A consumer pinned to
  0.16.0 reads `Sources` as the source and its overlay alone.
- **0.18.0** — a reference says when its null was inferred (§3.3): `TypeRef` gained `Inferred`,
  naming the heuristic that read null in, and empty where the source declares it. The OpenAPI
  compiler sets it where its nullable-widening policy reads a null in, from `nullable: true` or
  beside a `$ref`, and the document's version does not. A consumer pinned to 0.17.0 reads every
  nullable reference as declared.
- **0.19.0** — two members, each a home for something the OpenAPI compiler had classed or dropped:
  - `Operation` gained `Default`, the response that answers every status no entry of `Responses` or
    `Errors` names (§7.2). The compiler had lowered OpenAPI's `default` to `Errors`, classing it a
    failure, which the document does not say: an operation whose only response is `default` had no
    success response. A consumer pinned to 0.18.0 finds no `default` member and no longer finds the
    response among the errors.
  - `Model` gained `Requires`, the members an instance must carry that no property of its own
    declares: one a `Base` or `Mixin` declares, which a composing schema makes required, or one only
    its catch-all governs (§4.3). The compiler had dropped such a name, reporting it only on an
    `allOf`, and kept a map's verbatim, so an object writing a catch-all and a `required` name is
    now a `Model` where it had been a `MapT`. A consumer pinned to 0.18.0 reads those members as
    optional.
- **0.20.0** — a member a model's composition declares more than once is addressed by one
  declaration (§4.3). The shape is unchanged, but a `PropID` that names a model's member, an
  encoding key in `Content.Encoding` (§7.2) or a discriminator's `Property`, now names the
  declaration that model's flattening takes. The OpenAPI compiler had keyed a part two composed
  models declare by the first it found: the body's own, then its `Base`, then each `Mixin` in turn.
  It keys the later conjunct's now, the one the flattening takes, so `allOf: [{$ref: A}, {$ref: B}]`
  keys `B`'s. A consumer pinned to 0.19.0 that finds a part's entry by that old lookup finds none
  for such a part.
- **0.21.0** — an inferred-marker list has a type of its own (§12): `Provenance.Inferred` and
  `TypeRef.Inferred` are `Inferred`, the list `JoinInferred` writes, where each was a `string`. The
  JSON is unchanged, but a member's Go type is part of the shape, and `irverify` now holds every
  stored list to that form (`ir/inferred-not-canonical`), so a list that names a heuristic twice,
  or holds an empty name, is reported where it passed. A consumer pinned to 0.20.0 reads the same
  bytes; the version check refuses it a 0.21.0 document all the same.
- **0.22.0** — servers became entities, and serialization facts kept verbatim gained homes:
  - `Server` gained `ID` and `Provenance`, and `Document.Servers` became a registry keyed by
    `ServerID` that holds the servers the source declares, whichever object declares them (§10).
    `Service.Servers` and `Channel.Servers` name servers by ID where they had indexed a list, and
    an empty one names no server where it had meant every server. `Operation` gained `Servers`, the
    servers it is sent to in place of its service's. The OpenAPI compiler had kept a path item's
    and an operation's `servers` verbatim under `Unmodeled`, left `Service.Servers` unset, and
    lowered the `/` OpenAPI supplies for a document declaring none as if declared. A consumer
    pinned to 0.21.0 cannot read the registry, and sends every operation to the document's servers.
  - `HTTPParamBinding` gained `AllowEmptyValue`, `PartEncoding` gained `AllowReserved`, and
    `Response`, `ErrorCase` and `PartEncoding` hold `Headers` as `[]Header`, a `Property` with
    `Style` and `Explode` beside its members, which encode flat (§7.2). The compiler had kept each
    verbatim under `Unmodeled` as `no_ir_home`, where a consumer pinned to 0.21.0 that reads those
    entries no longer finds them.
- **0.23.0** — what these facts say changed:
  - `StreamingNone` is `""`, where it was `"none"`, the state every operation that does not stream
    already encoded by having no `streaming` member, and `irverify` holds `Streaming` to the
    declared modes (`ir/unknown-streaming-mode`) (§7.3). No compiler wrote `"none"`, so no compiled
    document changes; one that writes it, or a consumer comparing against it, is out of date.
  - A file body's `Content.Type` holds what its schema lowered to (§7.2). The OpenAPI compiler had
    written `bytes` over it, which left the schema on a node nothing referenced. `File` still says
    the body is a file, so a consumer pinned to 0.22.0 that reads `File` first is unaffected, and
    one that expects `Type` to be `bytes` finds a reference to the schema instead.
  - `Enum.ValueType` holds the primitive a declared type and format select, where every member is
    a value of it (§4.5): `int64` where the compiler had written `integer` and kept the format
    verbatim under `Unmodeled`, an entry such a consumer no longer finds.
  - `Union.Exclusive` is false on a union whose variants can match one instance, which the source
    did not ask to exclude (§4.4): a type array naming `integer` and `number` or repeating a type,
    and an enum whose entry repeats or lowers to the top type. The compiler had set it, leaving such
    a union to refuse what its schema admits.

---

## 3. Identity, names, references

### 3.1 IDs

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

The shape around the pointer is one grammar every compiler shares: a kind prefix (`t`, `op`, `p`,
`s`, `g`, `auth`, `param`, `e`, `server`), then the namespace, then the path. Only the path is the
format's, because a JSON Pointer, a GraphQL structural path and a protobuf fully-qualified name are
different things and nothing outside the format can compute one. Whatever the format,
`Provenance.Pointer` records the path as an RFC 6901 pointer, and the ID's path is that pointer
without its leading `/`: a protobuf full name is one segment, so `acme.v1.UserId` is recorded as
`/acme.v1.UserId`. A node a lowering *mints* rather than finds takes a namespace of its own, so no
pointer a reference can spell ever reaches it — the general form of the rule §4.3 states for
distributed unions.

A source can span documents: an OpenAPI description split across files is one. A node another
document declares lives in that document's namespaces, which are the source's own qualified by the
document, so one pointer in two documents is two IDs and each ID's path is still the pointer its
node records; its `Provenance.Source` names that document's `Sources` entry (§13). The qualifier is
an `@` and the document's location from the source's directory, as one segment, escaped as RFC 6901
escapes a token: `t/openapi@schemas~1pet.yaml/components/schemas/Pet` is the `Pet` component that
`schemas/pet.yaml` declares. The location, not the entry's index, qualifies it, so an ID moves only
when its document does. Operations, parameters and security schemes are named by where the source
mounts or names them, so they take the source's namespace whichever document declares them.

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
`NoSource` (§13). An index beside the empty pointer would say that file's whole document declared a
node every source reaches by kind. `irverify` holds no producer to this so far (#590).

Every named entity has an ID — including services (Thrift `service B extends A`, WSDL 2.0
interface extension, and Cap'n Proto interface inheritance all reference services by identity),
messages (AsyncAPI reuses one named message across channels, operations, and replies) and servers
(a service, a channel and an operation each name the servers serving it; §10).

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
that collision is a rendering decision it makes by ID (emitter-design §4.12).

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

An enum member is identified by the enum that declares it and the value it holds:
`e/<the enum's namespace>/<the enum's path>/<key>`. The member lives in the namespace of its
enum's own `TypeID` (`openapi` for a component schema, `anon` for an inline one) and beneath its
path, so it needs no namespace of its own: only enums hold members. `irverify` holds it there
(`ir/member-id-scope`); a member records no pointer for the agreement below to compare, so that
scope is what ties the two IDs together. The key, and the number a repeated value appends after it,
are described in §4.5.

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

An ID held as a reference — a field, a slice element, a map key or value, anywhere but the
declaring entity's own `ID` — names an entity, so it is never empty. A reference a position may
omit is a nil pointer (`Operation.OverloadOf`, `Reply.Channel`), as an optional type is a nil
`*TypeRef` (§3.3). Two positions document an empty value as meaningful, and they are the only ones:
`Discriminator.Default`, whose zero value means no default, and `Discriminator.Property`, empty when
`PropertyName` or `Index` locates the tag instead (§4.3). `irverify` reports an empty reference
anywhere else as `ir/empty-<noun>-ref`, except an empty `TypeRef.Target`, which is
`ir/type-ref-no-target` (§3.3). `pass.Validate` reports an empty reference only in a discriminator
mapping, as a missing variant, so far (#575).

This is the direct answer to oagen's name-keyed registry (silent collision merging, string-rewrite
ref fixing) and Kiota's name-keyed children (collision reconciliation logic), and adopts the
intent of TCGC's `crossLanguageDefinitionId`.

### 3.2 Names

```go
type Naming struct {
    Source    string   // exactly as written in the spec ("user_id", "$ref name", GraphQL field)
    Canonical string   // IR-normalized identifier in neutral form: lower_snake words, no casing opinions
    Hint      string   // context-derived suggestion where there is no source name to render: an
                       // anonymous type ("connection_domain"), or one named with the empty string
    Aliases   []string // alternate names for schema-resolution matching (Avro aliases). Versionless —
                       // rename *history* tied to version labels lives in Availability.RenamedFrom.
}
```

`Canonical` is a cleaned word sequence, not a cased identifier — emitters apply casing, acronym
policy, and reserved-word escaping. An emitter may read `Source` to recover the letter case of the
words `Canonical` fixes (`iOS` in `iOSVersion`), segmenting it with this section's grammar rather
than one of its own, and never to find where a word ends (emitter-design §4.12). Anonymous
(hoisted) types have empty `Source` and a `Hint`; whether an emitter inlines them or names them is
its choice.

**`Hint` is the same word sequence in the same grammar.** It is not a presentation affordance
sitting outside the neutrality rule: a hoisted type has no other name, so `Hint` is exactly what an
emitter renders that type's identifier from, which is the job `Canonical` does for a declared one.
That matters because a hint is nearly always derived from something a source *did* spell — a
component key, an `operationId`, a header name, a `$ref` target — so a compiler that passes the
context through puts precisely the casing and punctuation this section rules out into the one
channel that carries a name and no spelling (GitHub #54). What `Hint` is not held to is the
recomputation below: it has no `Source` beside it to be derived from, which is why it exists.

The segmentation is part of the contract, not each compiler's dialect: a word is a run of letters
and digits (with the combining marks that belong to them), a camel-case or letter/digit boundary
starts a new one, and **every other character separates** — `.`, `/`, `[`, `-`, a space alike. So
`com.example.User` and `com-example-user` canonicalize the same, and an emitter reading `Canonical`
never has to ask which compiler produced it. A source name with no word character in it
canonicalizes to the empty string; `Source` keeps the spelling.

At least one of `Source`, `Canonical` and `Hint` is set. A `Naming` with all three empty names the
entity to nobody, and every rule above is vacuously true of it, so `irverify` reports it
(`ir/naming-absent`). Where a source *does* name an entity with the empty string — a legal enum
value, property key, tag or component name — the compiler mints a hint rather than passing the
emptiness through; nothing is lost, because there was no spelling to keep.

**A camel-case boundary is where lowercasing changes the rune**, not where Unicode reports an
uppercase category. The two differ: double-struck `ℤ` and GREEK UPSILON WITH HOOK `ϒ` are
uppercase with no lowercase form, so they survive lowercasing unchanged and are not
boundaries — `COUNTℤ` is one word. The titlecase letters are boundaries and are not uppercase —
`xǅy` is two. Defining it by the effect rather than the category is what makes the grammar a fixed
point: a rune it split on but lowercasing left alone would still look like a boundary in the output,
and re-canonicalizing would split it again (GitHub #187). It is also the definition `irverify`
already applied to a whole canonical, so the producer and the checker agree by construction.

The **acronym tail** — the last capital of a run, before a lowercase letter — asks a different
question, and takes the category rather than the effect: `ℤServer` is `ℤ_server` because `ℤ` belongs
to a run of capitals whether or not lowercasing would change it, and `ǅBc` is `ǆ_bc` because a
titlecase letter belongs to one too. A transition test there would lose the first; the uppercase
category alone would lose the second.

`irverify` holds every `Naming` to this: the shape rules run over `Canonical` and `Hint` alike, and
it recomputes the canonical from the `Source` beside it, so a boundary in the wrong place is a
compiler bug rather than a variant reading. What no check can say is that the grammar itself is
right — a check that recomputes moves with what it recomputes through — so the answers are pinned
by a conformance table and the properties every answer must satisfy by a fuzz target beside it
(GitHub #186).

One rule sits under all of those and under `Aliases` too, because it is about the encoding rather
than the spelling: **every channel's bytes must decode**. It is not a naming rule at all: every
string a document holds must be well-formed UTF-8, because a `Document` refuses to encode one that
is not, so a single ill-formed name, ID or description fails the whole artifact. `irverify` holds
every string to it under one code, `ir/invalid-utf8`, at the path of the string that broke it.

**`Aliases` is held to none of the shape rules, and to rules of its own instead.** An alias is
matched against a name *another* schema wrote — an Avro type alias is a full name such as
`com.example.User`, or one relative to its type's namespace, and resolution compares it against the
writer schema's full name — so the separators and the casing are what the match is made of rather
than a spelling the IR gets to decide.
Neutralizing one would throw away precisely that, which is the lossy direction
lossless-by-default rules out. `Source` is the internal precedent: it carries `UserID` today and
no content rule touches it, because it records what the spec said rather than deciding a spelling.
What is left is decidable without any grammar, and `irverify` holds an alias to exactly that much:

- **Every entry names something** (`ir/naming-alias-blank`). An entry whose every rune is one
  Unicode classifies as invisible — a space, a control, a format character, or a default-ignorable
  one — matches nothing under any grammar. `""`, `" "`, `"\u200b"` and `"\u3164"` are alike here.
  An invisible rune sitting *beside* a visible one is a different question and is not asked:
  whether `com.example.<ZWSP>User` is a legal name is decidable only under the grammar of the
  format it will be matched against, which the IR does not know.
- **Every entry decodes** — the shared byte rule above, which reaches an alias the same way it
  reaches the other three channels.
- **No entry repeats another, or the entity's own `Source`** (`ir/naming-alias-duplicate`,
  `ir/naming-alias-redundant`), reported at the later entry and naming the earlier, so the message
  says which to delete and which to keep. Either admits no name that was not already admitted, so a
  producer that wrote one built the list wrong. Only `Source` is compared against: `Canonical` and
  `Hint` are names the IR derived for an emitter to render, never names a writer schema could have
  spelled.

That such an entry is inert is also why a *source* declaring one is recorded once with a diagnostic
rather than carried through: dropping it is not the lossy direction, since the same set of names
resolves to the entity either way.

An alias can also collide with *another* entity's name, and that is not a rule of the list:

- **Within a record** it is a problem in the spec. An alias that another property of the same model
  answers to — by its wire name or by an alias of its own — leaves a reader resolving that name two
  fields to choose from, which is why Avro holds an alias to the same uniqueness as a name. A source
  can write one, and no lowering can drop either claim without choosing between them, so
  `pass/validate` reports it (`pass/ambiguous-alias`) beside the duplicate wire names of the same
  scope, and `irverify` does not.
- **Across the type registry** nothing reports it, because nothing in the IR decides it. A relative
  alias is qualified by its type's namespace under the format's grammar, so one string on two types
  in two namespaces is two names; and one document can hold several independent schemas — each
  message payload of an AsyncAPI document is one — that may each declare the same type and alias.
  A collision between named types is the compiler's to report, within the schema it parsed.

### 3.3 Type references

```go
type TypeRef struct {
    Target   TypeID
    Nullable bool     // this *usage* admits null on the wire
    Inferred Inferred // "" = the source declares the null; else the heuristics that read it in (§12)
}
```

`Target` is never empty. A position that admits no type — a model with no base, a stream with no
element type — holds a `nil *TypeRef` (§4.1), so a `TypeRef` naming nothing is one a lowering left
unfilled, and `irverify` reports it (`ir/type-ref-no-target`).

Nullability lives on the reference, not the target type, because the same type is nullable in one
position and not another. Combined with `Property.Required` this yields the four distinct states
(required/optional × nullable/non-null) that OpenAPI 3.1, TypeSpec, and GraphQL all distinguish.
Compilers normalize every source spelling to this one bit: OAS 3.0 `nullable: true`, OAS 3.1
`type: [T, "null"]`, TypeSpec `T | null`, GraphQL's absence-of-`!`.
A oneOf/anyOf/union whose only distinction is a null variant becomes a plain nullable `TypeRef`,
never a union node.

The bit describes the whole schema, not the keyword that spells it. Where a source format conjoins
keywords (JSON Schema), a compiler reads them together: a value set that omits `null` beside a type
that names it — `{type: [string, "null"], enum: [red, green]}` — does not admit null, one that lists
`null` with no type beside it does, and a composition admits what its conjuncts jointly admit. So
every spelling of one constraint reaches the same bit, which is the only thing an emitter reads.

A bit a heuristic set rather than the source declared names that heuristic in `Inferred` (invariant
6). It is the reference's own marker because a reference has no `Provenance` (§13) and the node
holding one may hold several, so marking the holder would not say which bit was guessed. The marker
is set only where the heuristic admits null and the source's own reading does not, so `Nullable &&
Inferred == ""` is the bit that reading sets. `irverify` reports a marker on a reference that admits
no null (`ir/type-ref-inferred-not-nullable`). The OpenAPI compiler's one such heuristic is its
nullable-widening policy ([`nullable`](./compilers/openapi.md#nullable)).

Protobuf field *presence* is **not** nullability — protobuf has no null. Presence disciplines
lower to `Property.Presence` (§5.1), keeping `Nullable` strictly about wire-null.

---

## 4. The type graph

```go
type TypeKind string // "primitive" | "scalar" | "model" | "union" | "enum" | "list" | "map" |
                     // "tuple" | "literal" | "external" | "any"

type TypeCommon struct {
    ID         TypeID
    Name       Naming
    Namespace  []string     // the type's declared logical namespace (proto package, Avro namespace,
                            // Thrift/XSD/Cap'n Proto scopes); independent of Service.Namespace
    Anonymous  bool         // hoisted inline type
    Docs       Docs
    Tags       []string     // free-form labels (Smithy @tags, OpenAPI tag membership on types via policy);
                            // tag metadata lives once in Document.TagDefs
    Sensitive  bool         // whole-type redaction (Smithy @sensitive on shapes, OpenAPI format:password),
                            // shared by every type whose Base reaches this one; Property.Secret is the per-use form
    Access     string       // "" = public; "internal" = not part of the exported SDK surface
                            // (protobuf editions export/local, TCGC @access(internal), Smithy @internal via policy)
    Deprecation *Deprecation
    Availability *Availability
    WireNameByFormat map[string]string // type-level serialized-name overrides per mime
                                       // (TypeSpec @encodedName on models/enums/scalars)
    MediaTypeHint string    // declared default content type when the type is a body
                            // (TypeSpec @mediaTypeHint, Smithy @mediaType on string/blob shapes)
    XML        *XMLHints    // type-level XML wire shape: root element name/namespace
                            // (OpenAPI xml object, Smithy @xmlName/@xmlNamespace on shapes, TypeSpec @Xml.*)
    Examples   []Example    // typed example values attached to the type
                            // (TypeSpec @example, OpenAPI schema-level examples)
    Instantiation *TemplateInstantiation // provenance for monomorphized generics (TypeSpec templates)
    Unmodeled  Unmodeled
    Provenance Provenance
}

type TemplateInstantiation struct {
    Template string
    Args     []TemplateArg  // instances are identified by type AND value arguments
}
type TemplateArg struct {
    Type  *TypeRef          // exactly one of Type/Value is set
    Value *Value            // TypeSpec `valueof` template parameters
}
```

`Namespace` is a path, one segment per level, as `Service.Namespace` and `GraphQLBinding.FieldPath`
(§8.4) are. **No segment is blank** (`ir/path-segment-blank`): no namespace or field grammar has an
empty level, so a source that spells one has written an invalid name, which its compiler reports
rather than carries. A null or global namespace is the empty list, not one blank segment. A segment
may repeat — `com.acme.acme` has three levels — so a path is not held to unique entries.

**Derived facts** — the subtypes of a model, the properties a type exposes, how each type is used,
which types can hold themselves — are not stored on a type; `ir/irfacts` derives them from the
document (§4.9).

### 4.1 Primitives

```go
type Primitive struct {
    TypeCommon
    Prim PrimKind
}

type PrimKind string
// bool string bytes
// int8 int16 int32 int64 uint8 uint16 uint32 uint64
// integer  (arbitrary precision — JSON Schema "integer", TypeSpec "integer")
// float32 float64
// float    (arbitrary precision *binary* floating point — TypeSpec "float", supertype of float32/64)
// number   (arbitrary precision — JSON Schema "number", TypeSpec "numeric")
// decimal decimal128
// date time datetime datetime_offset duration
// url uuid
// any      (unknown/JSON any — schemaless)
```

The set is the union of TypeSpec's intrinsic scalars, JSON Schema's types, and Protobuf's needs.
Protobuf's `fixed32`/`sfixed64`/`sint*` are **encodings of** `uint32`/`int64`/…, not distinct
primitives — they lower to `Encoding` (§5.3). There is no `void` type: absence of a body/return
is a `nil *TypeRef`. TypeSpec `safeint` is not a primitive either — its canonical lowering is
`Scalar{Base: integer, Constraints: ±(2^53−1)}`.

**Containment.** `PrimKind.StrictlyWithin` relates two kinds by the values they hold: `a` is
strictly within `b` when every finite value of `a` is a value of `b` and `b` lies above `a` in the
edges below. NaN and the infinities, which only the IEEE-754 kinds hold and no JSON instance
carries, are outside the domain; that is what puts `float` below `number` and `decimal128` below
`decimal`. The edges (`domainParents`) follow TypeSpec's declared numeric hierarchy — each integer
width extends the next (`int8` up to `int64`, `uint8` up to `uint64`), `int64` and `uint64` extend
`integer`, `float32` extends `float64` extends `float`, `decimal128` extends `decimal`, and
`numeric` (`PrimNumber`) is their top — with two of its edges refined to exact containments it does
not declare: `integer` lies within `float` and `float` within `decimal`, since every finite binary
float is a finite decimal. To these they add the cross-signed widths (`uint8` within `int16`,
`uint16` within `int32`, `uint32` within `int64`), the widths a float holds exactly (`int16` and
`uint16` within `float32`, `int32` and `uint32` within `float64`), and `int64` and `uint64` within
`decimal128`. `PrimAny` is the top: every other kind is strictly within it, which is a rule of the
walk and not an edge.

Every edge holds both of a kind's finite values and of the JSON instances its spellings admit.
OpenAPI floats round, so `format: float` admits every finite number in its range; a bounded integer
width sits inside either float width by rounding, while `integer` merely shares the values a float
width holds. That holds of JSON instances and not of values, so `int32`, `uint32`, `int64`, `uint64`
and `integer` against `float32`, and `int64`, `uint64` and `integer` against `float64`, are not
edges. The string-spelled kinds (`date`, `time`, `datetime_offset`, `duration`, `url`, `uuid`,
`bytes`) are **not** within `string` here, although TypeSpec declares `url extends string`: they are
distinct kinds in this domain, and a format's refinement of `string` is a fact about an OpenAPI
spelling, which a compiler holds.

### 4.2 Scalars (named restricted/extended primitives)

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

`Base` is also the edge an *alias* uses, and there it names a node of any kind: §12.1's rule gives a
position that declares something a node of its own, and where that position resolved to a shared
target — a `$ref` to a model, a union, an enum — the node it owns is a `Scalar` standing over it.
So a consumer following `Base` must expect a non-scalar and stop there rather than assume a
restriction chain; `Constraints` and annotations on such a node bind the position, not the target.
`pass/validate` reads composition and discriminator subtyping through exactly this edge.

### 4.3 Models (structs / objects / messages)

```go
type Model struct {
    TypeCommon
    Properties     []Property
    Requires       []string        // wire names of members an instance must carry that no own
                                   // property declares: one Base, Implements or Mixins declare,
                                   // which this model makes required, or one only the catch-all
                                   // governs (JSON Schema `required`); none names an own property
    Base           *TypeRef        // declared single inheritance (TypeSpec extends, allOf-as-inheritance)
    Implements     []TypeRef       // interface conformance, N-ary (GraphQL `implements A & B`); targets are Abstract models
    Mixins         []TypeRef       // composition without subtyping (Smithy mixins, TypeSpec spread provenance, extra allOf entries)
    AdditionalProps *AdditionalProps // catch-all beside the declared structure; a catch-all
                                     // with nothing beside it is a MapT, never a Model (§4.6)
    Additional     AdditionalMode  // openness of the property set beyond declared + AdditionalProps
    Constraints    *Constraints    // cardinality of the property set (minProperties/maxProperties);
                                   // Additional says whether undeclared properties are allowed at all,
                                   // this says how many an instance may carry
    Abstract       bool            // cannot be instantiated directly; fields may be typed by it and
                                   // resolve to a conforming concrete model (GraphQL interface types)
    Positional     bool            // properties serialize positionally as a tuple ordered by WireID
                                   // (Erlang records: tag at element 1, fields at 2..N; WireID is the
                                   // 1-based element index, matching element/2)
    ExtensionRanges []WireIDRange  // wire-ID ranges reserved for third-party extension fields
                                   // (protobuf `extensions 100 to 199`)
    Discriminator  *Discriminator  // set on the polymorphic base
    DiscriminatorValue string      // set on each subtype: its wire tag value
    InputOnly      bool            // GraphQL input types; distinct identity from output types
}

type AdditionalMode string
// ""                         — unspecified (open by JSON Schema default)
// "closed"                   — no properties beyond the declared set (additionalProperties: false,
//                              closed-by-construction records)
// "closed_after_composition" — closed once composition is resolved (unevaluatedProperties: false)

type WireIDRange struct { From, To int }

type AdditionalProps struct {
    Value    TypeRef
    Key      *TypeRef       // nil = string keys, the one spelling of them (§4.6)
    Patterns []PatternProps // key-pattern-scoped value schemas (JSON Schema patternProperties)
}
type PatternProps struct { Pattern string; Value TypeRef }

type Discriminator struct {
    // Exactly one of Property / PropertyName / Index locates the tag:
    Property PropID                // model hierarchies: the property that carries the tag, the
                                   // declaration this model's flattening takes for it
    PropertyName string            // unions: wire name of the tag property (it exists on no single
                                   // model, so there is no PropID to point at; TypeSpec @discriminated
                                   // discriminatorPropertyName, OpenAPI discriminator on oneOf)
    Index    *int                  // positional: 0-based tuple element carrying the tag Literal
                                   // (Erlang tagged tuples {ok, V} | {error, R}; JSON arrays with a
                                   // const head via prefixItems)
    Mapping  map[string]TypeID     // wire value → subtype; nil mapping = infer by type name
    Default  TypeID                // variant to use when the tag is absent/unrecognized
                                   // (OpenAPI 3.2 defaultMapping); zero = none
    Envelope string                // "" = tag inline in the variant object;
                                   // "object" = {kind, value} wrapper (TypeSpec @discriminated envelope)
    EnvelopeValueName string       // wire name of the envelope's value property (default "value";
                                   // TypeSpec envelopePropertyName); meaningful only when Envelope=="object"
    Inferred bool                  // discovered heuristically (const-property detection), not declared
}
```

**Composition semantics.** `Properties` contains only the model's *own* properties. Consumers walk
`Base`, `Implements`, and `Mixins` for the full shape; the flattening is computed, never stored.
`ir.FlattenedProperties` computes it, by the rules under *The flattening* below; `ir.ExposedProps`
computes the flat set of property IDs it reads, and `ir.Supertypes` and `ir.IsSubtype` the subtype
relation, which excludes mixins; `irfacts.Index` answers the exposed set and the subtype relation
for many questions at once (§4.9). This preserves what oagen and Kiota lose: the difference between
"inherits from", "conforms to", "mixes in", and "declares". `Base` is single (subtype identity);
`Implements` is N-ary conformance to `Abstract` models — the distinction matters because GraphQL
allows multiple interfaces while every class-based target language allows at most one base class.

**Required members beyond the own properties.** Whether an own property must be present is its
`Required` (§5.1). A member the model requires that none of its own properties declares is in
`Requires`, by wire name and once (`irverify` refuses a repeat as `ir/requires-duplicate`), in the
order the source's `required` lists name it: an inherited property the model makes required, as
`allOf: [{$ref: User}, {required: [id]}]` makes `User`'s `id` required on the composed model, or a
member only the catch-all governs, which JSON Schema requires whether or not a schema declares it.
The flattening resolves each name over the flattened shape (below), and the IR asserts nothing
about which of these a name is, since saying so needs the hierarchy walked. A name an own property
declares is never listed (`irverify` refuses it as `ir/requires-own-property`), so required-ness
has one home per member. A model that requires a member is neither a map (§4.6) nor a decorator
(§4.10), since a map requires no key and a decorator says nothing its `Base` does not.

**The flattening.** `ir.FlattenedProperties(types, id)` returns a model's full shape: its members,
one per wire name, each with the declaration it takes, the declarations that one overrides and
whether an instance must carry it, beside the names a `Requires` lists that no member declares and
the composition entries that name no model. Its rules:

- *What it reads.* The declarations `ExposedProps` reaches and no others: the model's own, and those
  of every model its `Base`, `Implements` and `Mixins` lead to at any distance. Every reference, the
  root's included, is read through alias scalars, since a `$ref` carrying siblings composes the
  alias it hoists rather than the schema it names (below). A root naming no model has an empty
  shape.
- *Members.* Declarations are one member when their wire names are equal: `WireName`, else
  `Name.Source` (`Property.EffectiveWireName`, §5.1). The empty name is a name, the JSON member
  keyed by the empty string. A per-format name (`WireNameByFormat`) and an alias a property answers
  to (`Name.Aliases`) neither join members nor split one. A property several models declare under
  one `PropID`, as the variants of a distributed union do (below), is one declaration however often
  it is read.
- *Layout order.* The walk goes depth first from the root, following each model's `Base`, then its
  `Implements`, then its `Mixins`, each in order, and reads a model's own properties, in source
  order, once it has read every model the model composes that does not compose it in turn. Models
  that compose each other, a cycle the IR permits, are read together, in the order the walk
  finished them. So what a model composes comes before what it declares, conjuncts in the order the
  model lists them.
- *Override.* A later declaration of a wire name overrides an earlier one, and the member stays
  where its name was first read. So a model's declaration overrides that of every model it composes,
  at any distance, unless that model composes it too; and of two declarations whose models do not
  compose each other, the later conjunct's wins: a `Mixin`'s over the `Base`'s, a later `Mixin`'s
  over an earlier one's. A subtype narrowing its base's `petType` to `{const: dog}` keeps the base's
  slot with its own type. The member takes every facet but whether it is required — type,
  `Presence`, visibility, default, docs — from the declaration it takes, and lists the ones it
  overrides, which is where a consumer conjoining more finds them. Nothing is reported: redeclaring
  is how a subtype narrows, and whether two declarations agree is a question about their types,
  which the flattening does not ask.
- *Requiredness.* A member is required where any of its declarations is `Required`, or any model
  the walk reads lists its wire name in `Requires`: JSON Schema requires what any conjunct
  requires, so requiredness only accumulates. A name a `Requires` lists that no member declares is
  undeclared, a member only the catch-all governs.
- *Conjuncts that are no model.* A composition entry naming no model once read through alias
  scalars — a scalar, an enum, a union, a map, a list, or a type the registry does not hold —
  contributes no property, so the flattening lists the entry's target as written, once, for its
  consumer to lower (`Base` and `Mixins` name whatever the source composed with, below).
- *What it leaves.* A catch-all, `AdditionalProps` and `Additional`, is not flattened: a consumer
  reads the model's own (emitter-design §3.2), so a conjunct closing its property set is the
  consumer's to weigh. A property marked `Flatten` is one member like any other, the hoisting of its
  fields being its wire's (§5.1). Layout order is no wire order: a `Positional` model's elements
  follow their `WireID`.
- *Addressing a member.* A `PropID` naming a model's member, an encoding key in `Content.Encoding`
  (§7.2) or a discriminator's `Property`, names the declaration the flattening of that model takes,
  the content's type for an encoding key, so a consumer finds it on the member itself. The OpenAPI
  compiler keys a part through the flattening, and tags a discriminator by an own property, which
  the flattening takes. `pass.Validate` holds each only to a property the model exposes, not yet to
  the one it takes, since a flattening per model it asks about grows quadratically down a deep
  hierarchy, which its cost tests refuse.
- *Cycles and cost.* Each model and alias scalar is read once, so a cyclic composition is read to
  its fixpoint, each model of a cycle holding the members of all of them, and nothing is cut short:
  the walk needs no step budget and no recursion. It costs the declarations it returns plus the
  models, entries and aliases it passes, so an `irfacts` index could answer no faster, and it stays
  a function of `ir`.

Why the later conjunct wins: JSON Schema conjoins every `allOf` entry, so neither of two unrelated
declarations is wrong on the wire, and the later conjunct is where a spec writes the specialization.
On the golden spec (`cloudflare/api-schemas` `openapi.json` at `e2b8c60`, compiled at `efafa0d0`),
1,197 of the 3,866 composed models redeclare a wire name. Of the 1,811 overrides between
declarations whose models do not compose each other and whose types are different nodes, 1,788 are
made by a failure schema. They fall on 1,572 members, every one in a 4XX, 5XX or default response
that composes a success shape and, after it, the failure schema (`success: false`, `result: null`),
which an earlier-conjunct rule would type as the success. 774 members are required only through a
declaration they override, no `Requires` naming them, which a rule reading requiredness off the
declaration taken alone would make optional. Visibility is the declaration taken's because both
directions occur: of the 41 members whose declarations disagree on it, 28 take one that makes them
read-only, and 13 one that lifts the read-only an overridden declaration states, as a batch patch
body redeclares the `id` it must send, which a conjunction of the two would forbid writing.

Which `allOf` entry becomes `Base` and which a `Mixin` is the OpenAPI compiler's rule
[`allof-classification`](./compilers/openapi.md#allof-classification).

A `$ref` entry that also writes keywords beside the `$ref` composes the *branch*, not the schema
the branch names: those keywords bind this use of the target and cannot go on the shared node, so
the branch position takes a node of its own — an alias over the target carrying them — and
`Base`/`Mixins` point at that. It is the same alias any other `$ref`-with-siblings position hoists
when it carries no `Property` or `Parameter` of its own to hold them — `items`, a union branch, a
component — so nothing about composition is special here; a `$ref` entry writing nothing beside
itself composes straight to the target and hoists no node. The alias's name hint is the hint the
node the `$ref` resolves to carries — a component's name, or, for a position inside a schema, the
hint its position gives it, an operation's or a response's included. A hint is a function of the
documents and the position alone, so an outside `$ref` naming the branch pointer gives the same one,
whichever lowering interns the node.

`Base` and `Mixins` name whatever the source composed with, which in JSON Schema is any schema:
`allOf: [{$ref: SomeScalar}]` is legal and lowers to a `Base` pointing at a `Scalar`, and the same
holds for an `Enum`, a `Union`, a `MapT`, or the top type. They are typed `TypeRef` rather than
"reference to a `Model`" for exactly that reason — narrowing them would mean dropping a branch the
source wrote, against invariant 2. What is *shaped* like `allOf` is the composition itself: one
referent per conjunct, resolved by reference. Emitters that can only render model inheritance are
the ones that must lower a non-`Model` conjunct to something their target can express.

**Composition co-declared with a union.** JSON Schema conjoins sibling keywords, so
`{allOf: […], oneOf: [{$ref: A}, {$ref: B}]}` is a composition *and* a union at once. A compiler
must never return early on the combinator and drop the composition. It distributes instead —
`S ∧ (X | Y)` is lowered as `(S ∧ X) | (S ∧ Y)`: the node is a `Union` whose every `Variant.Type`
is a `Model` carrying `S`'s own `Base`/`Mixins`/`Properties`/`required`/`Additional`, with that
branch's referent conjoined by the same rule as an `allOf` entry (`Base` when the composition
named none, else a `Mixin`). The composition rides on every variant rather than being merged into
one or dropped, so nothing degrades and no `allOf` branch is flattened. A property `S` declares
appears on each variant under its single source-derived `PropID`, since the source declares it
once.

The variant models are anonymous and **synthesized**: no schema in the source occupies them, so
each takes an ID in a namespace of its own, derived from — but not equal to — the ID the branch
pointer would yield. Provenance still points at the branch, the nearest thing the source wrote.
Giving them the branch pointer's own ID would put two different types on one pointer: a `$ref` to
`…/oneOf/N` denotes the *branch schema*, and whichever of the two lowered first would take the
pointer from the other. That makes the IR depend on declaration order and can produce exactly the
`Union` the next paragraph forbids.

Distribution needs a referent per branch, because `Base`/`Mixins` conjoin by reference — a branch
must be a `$ref`, and that `$ref` must resolve. A union that cannot supply a referent for every
branch is not distributed *at all* — never halfway, which would leave a `Union` whose variants
disagree about whether they carry `S` with nothing in the IR saying which. Those cases degrade
instead, enumerated by the OpenAPI compiler's rule
[`union-beside-structure`](./compilers/openapi.md#union-beside-structure).

### 4.4 Unions

```go
type Union struct {
    TypeCommon
    Variants      []Variant
    Exclusive     bool           // exactly one variant matches (oneOf); false = one or more (anyOf,
                                 // and a union whose variants overlap though no source asked to
                                 // exclude them, as a type array naming integer and number)
    WireTagged    bool           // the payload names its variant: by the Discriminator's tag, or by
                                 // a single key (Smithy union, protobuf oneof); false = untagged
    Discriminator *Discriminator // where the tag sits in the payload; set only when WireTagged
}

type Variant struct {
    Name       Naming    // named variants (TypeSpec, Smithy, protobuf oneof fields); Hint-only for bare oneOf members
    Type       TypeRef
    WireName   string    // serialized tag when it differs from Name.Source
                         // (Smithy @jsonName on union members, protobuf oneof json_name)
    WireID     *int      // protobuf oneof field number, Cap'n Proto/Avro ordinal; nil = none
                         // (pointer because 0 is a legal ordinal in several formats)
    XML        *XMLHints // @xmlName/@xmlNamespace on union members
    Event      *EventInfo // event-stream metadata when the union is a stream's event set
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Examples   []Example
    Unmodeled  Unmodeled
}

type EventInfo struct {
    ContentType string   // per-event content type (TypeSpec @Events.contentType)
    Terminal    bool     // receiving this event ends the stream (TypeSpec @SSE.terminalEvent)
}
```

One node covers the whole design space from the capability matrix: untagged `anyOf`
(`Exclusive=false`), untagged `oneOf` (`Exclusive=true`), unions a tag in the payload routes
(`WireTagged=true` with a `Discriminator`), and key-tagged unions (`WireTagged=true` alone).
Variant identity survives so emitters can generate named accessors, sealed interfaces, or wrapper
types per their refiner strategy. Unions never degrade to optional-field merges in the IR — that
includes GraphQL `@oneOf` input objects, which are spec-level tagged input unions and lower here,
not to models.

**Tag-mode conventions** (normative): `WireTagged=true` with a `Discriminator` means the tag
travels in the payload, where the discriminator locates it: inline among the variant's own
members, as a property (an OpenAPI `discriminator`, GraphQL `__typename`) or a tuple element
(`Index`, an Erlang tagged tuple), or in the `{kind, value}` `Envelope` around them.
`WireTagged=true` with **nil** `Discriminator` means the union is *key-tagged*: the wire shape is
a single-key object whose key is the variant's wire name (Smithy unions, proto3-JSON oneof,
GraphQL `@oneOf` inputs). `WireTagged=false` means the variant is inferred by validation (JSON
oneOf/anyOf), and such a union carries no `Discriminator`; `irverify` refuses one that does
(`ir/union-discriminator-untagged`). `Exclusive` is independent of the mode: it records what
validation admits, exactly one variant or one or more.

An OpenAPI discriminator is the payload-tagged mode, not a fourth one. The tag property's value
names the schema the payload is expected to validate against. A value no mapping names selects
none, and OpenAPI says validation should then fail, unless 3.2's `defaultMapping` names one, which
`Discriminator.Default` holds and which also answers for a payload carrying no tag. OpenAPI also
says the discriminator must not change the validation outcome, but that binds validators, not the
wire: what validation admits stays in `Exclusive`, and on an `anyOf` the tag picks one of the
several schemas a payload may satisfy. A mode for "tag plus validation" would spell the same wire
shape twice.

### 4.5 Enums

```go
type Enum struct {
    TypeCommon
    ValueType PrimKind      // string | int32 | int64 | float64 …
    Members   []EnumMember
    Closed    bool          // false = open/extensible: unknown values must be representable
    Flags     bool          // bitfield semantics
    FallbackMember string   // wire name of the member to substitute when an unknown value is read
                            // (Avro enum `default` symbol); "" = none
}

type EnumMember struct {
    ID         EnumMemberID // e/<enum's namespace>/<enum's path>/<key>, keyed by the value held,
                            // and /<n> after it on the n-th holder of one value
    Name       Naming
    Value      Value        // typed value, matches ValueType
    WireName   string       // serialized form when it differs from Value (rare)
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability // members appear/disappear across versions (TypeSpec @added on EnumMember)
    Examples   []Example
    Unmodeled  Unmodeled
}
```

`Closed` defaults per source semantics: JSON Schema enums are closed; Smithy enums are open;
protobuf enums are open or closed **per file syntax / editions feature** (proto2 closed, proto3
open, editions `enum_type` per enum) — the compiler lowers the *resolved* value. Emitters that
can't express open enums (plain Go consts, TS string literals) lower via their refiners — the
bit must survive to that point (Kiota's string-only closed enums are the counterexample).
Duplicate member values are legal (protobuf `allow_alias`); slice order preserves which name is
canonical for serialization, and the validate pass must not reject them.

A member's `ID` is keyed by the value it holds, never by its name or its position: `a-b` and `a_b`
render from the same words and are still two members, and reordering a list that holds each value
once renames none. The key
is one RFC 6901 token, `<tag>:<payload>`, where the tag names the kind of value — `s` string, `y`
symbol, `n` number, `b` bool, `x` bytes as unpadded URL-safe base64, `z` null — so the string `1`
and the number `1` are two members. A number's payload is its canonical decimal
(`ir.Decimal.String`): the leading significant digit, any others after a point, and the power of ten
that digit carries, so `1500`, `1500.0` and `15e2` are all `1.5e3` and every zero is `0`. Only the
key is canonical: the member's `Value` is unchanged. A list, object, reference or constructor value
has no key, and an enum holding one is not an `Enum`. A value held twice keeps every member: the
first has the bare key, and each repeat appends its occurrence number as one more token
(`…/<key>/2`), as a repeated parameter does (§3.1), which no key can be, since every key opens with
its tag and a colon. The OpenAPI compiler reports each repeat in the list as written
(`openapi/duplicate-enum-value`), whether the list lowers to an `Enum` or degrades to a `Union`.
Which repeat holds the bare ID follows source order, so a reorder keeps the set of IDs and may move
that one.
`pass.Validate` reports an ID repeated within one enum as `pass/duplicate-enum-member-id`.

A **closed** Enum with **no members** is the empty value space — it admits its members and has
none — so it is how a compiler states a position that accepts no instance at all (JSON Schema's
`enum: []`) without a bottom `TypeKind`. Emitters render it as their uninhabited type where they
have one (TypeScript `never`); none may widen it to the top type.

**The member count never decides an enumeration's kind.** An enumeration whose members an `Enum` can
hold is an `Enum`, with one member as with many. One it cannot hold — a list or object member as
above, members of different kinds, or only `null` — lowers to an exclusive `Union` of `Literal`s
whatever its count, with a diagnostic. In particular, one member does not make an enumeration a
`Literal` (§4.6), which is for a source's *constant*: JSON Schema `const`, a TypeSpec literal type,
an Erlang atom. The count is the wrong thing to decide the kind on:

- A member has an identity, a name, docs and a deprecation of its own (`EnumMember`), none of
  which a `Literal` has a field for.
- `Closed` belongs to the enum, and a one-member enum can be open — an `x-extensible-enum`, a
  Smithy enum, a proto3 enum — which no single value can state.
- Adding a member to an enum is ordinary evolution of an API, and it must not change the kind
  of the node, which is what every consumer dispatches on.
- Every other format's one-member enum is an `Enum`, as the memberless one above already is.

So OpenAPI 3.0, which has no `const` and writes a pinned value as a one-member `enum`, lowers it to
an `Enum`. A consumer that reads a pinned value — discriminator inference (#659), a target that
types a tag as a literal type — reads a closed one-member `Enum` the way it reads a `Literal`, and
that reading is its policy rather than a lowering.

### 4.6 Containers and the rest

```go
type List struct    { TypeCommon; Elem TypeRef; Constraints *Constraints; Encoding *Encoding }
                    // Constraints: minItems/maxItems/uniqueItems.
                    // Encoding: container-level wire encoding — protobuf packed vs expanded repeated
                    // fields ("packed"/"expanded"); stacks with the element's own encoding
                    // (repeated sint64 [packed=false] = zigzag element + expanded list)
type MapT struct    { TypeCommon; Key *TypeRef; Value TypeRef; Constraints *Constraints }
                    // a map and nothing else: TypeSpec Record, a protobuf, Smithy or Avro map, an
                    // object whose one statement about its members is additionalProperties.
                    // Key: nil = string keys, the one spelling of them, as for AdditionalProps.Key.
                    // Constraints: minProperties/maxProperties (Smithy @length), the entry count
type Tuple struct   { TypeCommon; Elems []TypeRef; Rest *TypeRef; Constraints *Constraints }
                    // prefixItems + items, TypeSpec tuples, Erlang tuples
                    // Rest: the type of every element past Elems; nil = none may follow
                    // Constraints: minItems/maxItems/uniqueItems over the whole instance
type Literal struct { TypeCommon; Value Value }
                    // const, a source's literal types, Erlang atoms;
                    // a one-member enum is an Enum (§4.5)
type External struct{ TypeCommon; Identity string; Package string; MinVersion string } // resolve to a well-known library type (TCGC external)
type Any struct     { TypeCommon }                                         // schemaless
```

Containers are real type nodes with IDs (hoisted like all anonymous types), not flags on
references — Kiota's `CollectionKind` flag couldn't express nested collections or constrained
lists cleanly. `External` also carries opaque runtime handles that no target can structurally
model (`erlang:pid`, `erlang:fun` — see §4.8).

**A map and nothing else is a `MapT`.** `Model.AdditionalProps` and `MapT` both type a value under
arbitrary keys, and what tells them apart is what else the source says about the members. A
catch-all beside any other member fact — a declared property, a pattern-scoped value, composition
(`Base`, `Implements`, `Mixins`), a closure (`Additional`), a discriminator or a subtype's tag, or
any other field only a `Model` has — is that model's `AdditionalProps`. A catch-all with nothing
beside it is a `MapT`, never a `Model` with no properties. TypeSpec's `Record<T>`, a protobuf map
and an OpenAPI object writing only `additionalProperties` are one construct, and a document that
held some of them as models would leave every emitter to recognize the map itself, the work a
dictionary node exists to spare it (prior-art: Kiota has no map type, TCGC has one). Both spell a
string key one way, as a nil `Key`. A key naming the `string` primitive states that type a second
way, so `irverify` rejects it (`ir/map-key-string`), and a `Key` that is set names some other type.

`ir.AsMap` states the rule field by field, and it is the only statement of it. A compiler builds the
`Model` its source describes and converts it through `ir.AsMap`, so a lowering that comes to fill
another `Model` field keeps the node a model with no second predicate to update, and `irverify`
holds every document to the same function (`ir/map-as-model`). The conversion keeps the model's
`TypeCommon` — its ID, name, docs and unmodeled entries — and takes the catch-all's key and value
and the model's `Constraints`, which on a map bound how many entries an instance may carry. A map
requires no key, so an object whose `required` names one is a `Model` whose `Requires` lists it
(§4.3), never a `MapT`, however it is composed. What a map has no field for is kept the way any
keyword without a home is: `propertyNames` stays the validation-only entry §4.7 makes it, since it
constrains keys without being a key type.

A `Tuple` states three independent things, one field each. `Elems` types the leading positions.
`Rest` types every element past them, as `List.Elem` types every element of a list; nil means no
element may follow, so an open rest that constrains nothing targets the top type rather than being
absent. `Constraints` bounds the whole instance, `Elems` and `Rest` together, under the same rules
as a `List`'s (§12.2).

`Elems` does not require its positions. JSON Schema's `prefixItems` requires none of them, and an
array has no holes, so the required positions are always a prefix, and `Constraints.MinItems` is how
long it is. A consumer reading a `Tuple` with no `MinItems` must therefore treat every position as
optional, and a source whose tuples are exact — TypeSpec's and Erlang's — lowers each with
`MinItems` equal to `len(Elems)` and no `Rest`. No field is derived from another: a compiler neither
trims `Elems` to `MaxItems` nor restates a closed tuple as a `MaxItems`.

### 4.7 Validation-only schema constructs — a documented boundary

JSON Schema's `not`, `if`/`then`/`else`, `dependentSchemas`, `dependentRequired`,
`contains`/`minContains`/`maxContains`, `propertyNames`, `unevaluatedItems`, and
`unevaluatedProperties` express *validation logic*, not data shape; no target language's type
system represents them, and none of the other source formats has an equivalent. The IR
deliberately does not model them structurally. Compilers keep them verbatim in `Unmodeled`
(`openapi:not`, `openapi:if-then-else`, `openapi:contains`, …) under `ReasonValidationOnly` and
emit an `info` diagnostic, so the information is never silently lost and a future
validation-oriented emitter (request validators, mock servers) can still consume them — selecting
on the reason rather than sifting vendor metadata out of the same bag (§12).
The one structural carve-back: `unevaluatedProperties: false` is *shape* (a closed model after
composition) and lowers to `Model.Additional = closed_after_composition`. Every other
`unevaluated*` form stays verbatim, `unevaluatedProperties` with a schema value included.
`propertyNames` is not a second carve-back onto `AdditionalProps.Key`: it
constrains *every* key of the object, including the ones `properties` declares — where there is no
`AdditionalProps` to hold a key type at all — and it is a whole schema rather than a type, so only
its degenerate `{type: string}` form names one.

Two neighbouring keyword groups are *not* part of this boundary — the content vocabulary
(`contentEncoding`, `contentMediaType`, `contentSchema`) and the keywords that identify a schema
resource (`$id`, `$schema`, `$vocabulary`) — and neither is `$dynamicRef`. How the OpenAPI compiler
lowers each is stated with its rules:
[`content-vocabulary`](./compilers/openapi.md#content-vocabulary),
[`dialect-keywords`](./compilers/openapi.md#dialect-keywords) and
[`dynamic-ref`](./compilers/openapi.md#dynamic-ref).

This is the one intentional carve-out from "lossless means structural": losslessness is satisfied by
verbatim preservation, and the carve-out is explicit rather than accidental. The OpenAPI compiler
holds every 2020-12 keyword to being lowered or kept
([`vocabulary-coverage`](./compilers/openapi.md#vocabulary-coverage)).

### 4.8 Degraded source constructs — documented lowerings

A few source constructs have no faithful target — either because the IR has no combinator for the
shape, or because no target language's type system holds it. Each gets a *normative* weaker
lowering, so the degradation is a decision rather than an accident, and — wherever that weaker
shape actually loses something — verbatim preservation under `ReasonDegradedLowering` beside it,
so the loss stays recoverable. A construct kept raw merely for want of an IR field at that
position is a different situation and carries `ReasonNoIRHome` instead (§12).

The list is the set of *source-construct* degradations: one entry per distinctive source shape the
IR declines to model as written. It is not a list of every lowering that can weaken a type: the
whole-schema fallbacks a compiler takes at its own limits are its rules, as the OpenAPI compiler's
are ([`whole-schema-fallbacks`](./compilers/openapi.md#whole-schema-fallbacks), and for a `false`
`allOf` branch [`allof-false-branch`](./compilers/openapi.md#allof-false-branch)).

- **JSON Schema shapes** — `oneOf`/`anyOf` co-declared with structural keywords that cannot be
  distributed, competing keywords at one position, an inline `allOf` branch declaring more than the
  merge consumes, a redeclared property the merge cannot fold whole, and a shape applicator the
  lowered node cannot carry. Each is a rule of the OpenAPI compiler:
  [`union-beside-structure`](./compilers/openapi.md#union-beside-structure),
  [`competing-keywords`](./compilers/openapi.md#competing-keywords),
  [`allof-inline-residue`](./compilers/openapi.md#allof-inline-residue),
  [`allof-redeclared-property`](./compilers/openapi.md#allof-redeclared-property) and
  [`shape-applicator`](./compilers/openapi.md#shape-applicator).
- **TypeSpec `never`-typed properties/variants** — no `never` node exists, so compilers delete
  the member and emit an `info` diagnostic. Nothing is preserved beside it: a `never` member
  admits no value, so there is no content the deletion could lose.
- **TypeSpec `StringTemplate` types** (interpolating non-literal types) — degrade to `string` +
  `Unmodeled["typespec:string-template"]` + `info` diagnostic.
- **Erlang bit-sized binaries** (`<<_:M, _:_*N>>`) — `bytes` + `Min/MaxLength` when
  byte-aligned, plus `Unmodeled["erlang:bit-size"] = {base, unit}` + `info` diagnostic.
- **Erlang `fun()` types** — `External{Identity: "erlang:fun"}` + full spec text in
  `Unmodeled["erlang:fun-spec"]` + `warning` diagnostic (non-portable member). No function
  type node exists.
- **Erlang heterogeneous map association lists** (`#{atom() => a(), integer() => b()}`) —
  lower to a `MapT` whose key and value are unions (§4.6), or to union-typed `AdditionalProps`
  beside a map's `:=` associations, + `Unmodeled["erlang:map-assocs"]` verbatim, since a union on
  each side loses which key type pairs with which value type.
- **Erlang `-opaque`** — the shape lowers structurally and losslessly; what degrades is the
  abstraction boundary the attribute declares, which no IR node states.
  `Unmodeled["erlang:opaque"] = true` records it, and emitter policy decides whether to expose
  or wrap.

### 4.9 Derived facts — `ir/irfacts`

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

**Subtypes.** `IsSubtype(t, b)` answers exactly what `ir.IsSubtype` answers (§4.3), and
`Subtypes(b)` lists every declared type it holds below `b`. The index condenses the relation into
its strongly connected components and numbers them in post-order, searching depth-first down the
condensation; each component's interval runs from the least number below it to its own. A pair
whose intervals do not nest is unrelated, and a type numbered after the search reached `b`, and
before `b` itself, is below `b`. Any other pair is confirmed by `ir.IsSubtype`'s walk. There is one
only when a subtype of `b` has a supertype that is neither `b` nor below it, so a model naming two
supertypes leaves open only pairs whose base lies above it, and in a forest every question is a few
comparisons.

**Exposed properties.** `Exposes(t, p)` answers exactly what `ir.ExposedProps(t)` answers about `p`
(§4.3). The relation `ExposedProps` walks, a model leading to its `Base`, `Implements` and `Mixins`
and an alias scalar to its `Base`, is numbered the way the subtype relation is, and a property
declared by one model is exposed by that model and every type that reaches it. A pair the numbering
leaves open, and a property several models declare, are confirmed by the walk. A mixin is a second
parent, so this relation leaves pairs open more often than the subtype relation does.

**The type graph.** Usage and recursion read one graph with two vertices per declared type: its
*shape*, what a value of exactly that type holds, and its *range*, every shape a value declared as
that type may take. A type is *polymorphic* when the model it stands for, read through its alias
scalars, has a `Discriminator` or is `Abstract`; a value declared as any other type is of exactly
its shape.

| From | To | Through |
|---|---|---|
| a shape | the target's range when it is polymorphic, its shape otherwise | what a value holds: a model's properties and catch-all (value, key, patterns), a union's variants, a list's element, a map's key and value, a tuple's elements and rest, and what an encoded value holds once decoded (`Encoding.Schema` on a property, a scalar or a list, §5.3) |
| a shape | the target's shape | what a model composes: `Base`, `Implements`, `Mixins` |
| a shape, a range | the target's shape, its range | what a scalar stands for: its `Base` |
| a range | its own shape, and each direct subtype's range | the shapes a declared value may take |

Composition reaches a shape and never a range, so a subtype that includes its discriminated base's
members does not thereby reach the base's other subtypes, and a base routing to its own subtype is
no cycle. A discriminator's mapping and default, a default or example value, an encoding's wire
type, a template argument and an availability record name types too, but none puts a value of one
type in a value of another, so none is an edge.

**Usage.** `Usage(id)` flags a type as the client's side of the wire sees it. `Input` and `Output`
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
a success nor a failure (§7.2), so its types are not marked `Error`. An operation nested past
`ir.MaxGroupDepth` is not read, and a type no root reaches has usage 0.

**Recursion.** `Recursive(id)` reports whether a type's shape lies on a cycle of the graph — a
value of exactly that type can hold, at any depth, one of the same shape — and `Component(id)` lists
the types whose shapes share its strongly connected component. A reference to a polymorphic base
counts every subtype it may hold, so a subtype holding its own discriminated base is recursive. A
cycle through composition alone, which the IR permits, is reported too.

### 4.10 Decorators — a reference with annotations

A format that admits nothing beside a reference annotates one use of a shared type by wrapping it:
OpenAPI 3.0 writes `state: {allOf: [{$ref: Status}], description: the widget state}`, and keeps it
losslessly as an anonymous `Model` whose `Base` is `Status`. 3.1 writes the same use `{$ref: Status,
description: …}`, which hoists an anonymous `Scalar` alias over `Status` wherever no carrier holds
the annotations (§12.1). Neither node says anything about a value that its `Base` does not. Such a
node is a **decorator**, and `ir.Decorated` is the one statement of the rule: an anonymous type
whose `Base` is set, and which is

- a `Model` with every other field of its own zero, or a `Scalar` with no `Constraints` and no
  `Encoding`; and
- sets nothing in `TypeCommon` beyond its identity (`ID`, `Name`, `Namespace`, `Anonymous`,
  `Provenance`), the annotations a use site can hold (`Docs`, `Deprecation`, `Examples`),
  `Availability`, which no emitter interprets, and `Unmodeled`, whose entries an SDK acts on by
  reason, none of them by changing a type.

The rule lists what may be set rather than what may not, so a field the IR gains keeps a node from
being a decorator until a change decides otherwise; `ir`'s test sets every other field in turn by
reflection, which turns that decision into a failing test rather than a review catch. A named type
is never a decorator: its name is public surface. An emitter collapses a decorator into the
reference it decorates (emitter-design §4.15), and a compiler names what holds one as it names what
holds that reference, so the two spellings of one use name it alike
([`union`](./compilers/openapi.md#union)).

---

## 5. Properties, constraints, encodings, visibility

### 5.1 Property

```go
type Property struct {
    ID         PropID
    Name       Naming
    WireName   string              // serialized name; defaults to Name.Source
    WireNameByFormat map[string]string // per-mime overrides (TypeSpec @encodedName json/xml)
    WireID     *int                // protobuf field number / thrift id / tuple element index
                                   // (1-based when Model.Positional); nil = none — pointer because
                                   // 0 is a legal ordinal in Cap'n Proto/FlatBuffers/Avro
    ExtensionOf string             // "" = the model's own field; else the fully-qualified declaring
                                   // scope of a third-party extension field (protobuf `extend`) —
                                   // directs qualified JSON naming and registry-based accessors
    Type       TypeRef
    Required   bool                // presence on the wire; orthogonal to Type.Nullable
    Presence   PresenceKind        // wire-presence discipline where the format distinguishes more
                                   // than required/optional (protobuf)
    ClientOptional bool            // wire-required but clients MUST treat as optional
                                   // (Smithy @clientOptional; implicit for @input structure members)
    DefaultAdded bool              // default was added post-publication; generators may ignore it
                                   // for backward compatibility (Smithy @addedDefault)
    Visibility Visibility          // lifecycle set; zero value = visible in all
    Default    *Value
    Constraints *Constraints
    Encoding   *Encoding
    Args       []Parameter         // parameterized fields: GraphQL field arguments on any property,
                                   // at any depth — not just operation entry points. Empty elsewhere.
    Flatten    bool                // property's fields hoisted into parent on the wire (Smithy/TCGC
                                   // flatten; also set on the synthetic property wrapping a hoisted
                                   // protobuf oneof — oneof members are wire-level top-level fields)
    EventHeader bool               // in an event-stream event model: member travels in the frame
                                   // header, not the event payload (Smithy @eventHeader)
    EventPayload bool              // member is the *raw* frame payload — blob/string as bytes,
                                   // structure as protocol document (Smithy @eventPayload).
                                   // Mutually exclusive with EventHeader; at most one per model.
    Secret     bool                // redact in logs/docs (TypeSpec @secret, format:password); a value whose
                                   // type is TypeCommon.Sensitive is redacted whether or not it is set
    XML        *XMLHints           // XML wire shape when it diverges from the JSON-implied shape
    Examples   []Example           // property-level examples (TypeSpec @example, OpenAPI)
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Unmodeled  Unmodeled
    Provenance Provenance
}

type PresenceKind string
// ""         — format default (JSON world: Required/Nullable say everything)
// "implicit" — absence ⇔ default value; unset is unobservable; zero values are not serialized
//              (proto3 no-label, editions IMPLICIT)
// "explicit" — unset is distinguishable from default-valued (hazzers/pointers; proto2 optional,
//              proto3 `optional`, editions EXPLICIT)
// "required" — must be present on the wire (proto2 required, editions LEGACY_REQUIRED)
```

**Scope rule for `Args`:** parameterized properties are only legal on models reachable from a
GraphQL binding (the only format with per-field arguments); the validate pass rejects them
elsewhere. This keeps ordinary models pure data shapes — a model without `Args` anywhere in its
graph serializes conventionally in every emitter.

### 5.2 Visibility — lifecycle sets, not booleans

```go
type Lifecycle = string // OPEN set. Canonical values: "create" | "read" | "update" | "delete" | "query".
                        // TypeSpec's visibility system allows arbitrary user-defined visibility
                        // classes; compilers lower custom classes as "<class>:<member>" strings so
                        // nothing is dropped. Filters evaluate per class, not across the flat union;
                        // emitters treat unknown classes as opaque filters.

type Visibility struct {
    Only []Lifecycle   // empty = visible in all lifecycles (unless None)
    None bool          // visible in NO lifecycle: excluded from every projection
                       // (TypeSpec @invisible) — distinct from the zero value
}
```

`Only`, like an operation's `ParameterVisibility` and `ReturnTypeVisibility` (§7.2), is a set: no
entry is blank and none repeats (`ir/set-entry-blank`, `ir/set-entry-duplicate`), since a blank
names no class and a repeat admits nothing more.

`readOnly` lowers to `Only: [read]` (plus delete/query per OpenAPI semantics); `writeOnly` to
`Only: [create, update]`; GraphQL input-vs-output types to `create/update` vs `read`; TypeSpec
`@visibility` maps directly. One logical model therefore produces N wire shapes; the projection
(`ModelShape(model, lifecycle)`, with PATCH additionally making properties optional *unless the
binding disables it* — §8.1 `PatchImplicitOptionality`) is a computed traversal in emitters'
plan layer — the IR stores the single logical model plus the visibility facts, never the
projected variants. Operations can override which filter applies to their request/response
(§7.2 `ParameterVisibility`/`ReturnTypeVisibility`). This is TypeSpec's `MetadataInfo` design
with storage and computation split.

### 5.3 Constraints and encodings

```go
type Constraints struct {
    // numeric — arbitrary-precision decimal strings, never float64 (TypeSpec Numeric lesson)
    Min, Max           *BigVal    // inclusive bounds (minimum / maximum)
    ExclusiveMin, ExclusiveMax *BigVal // exclusive bounds (exclusiveMinimum / exclusiveMaximum);
                                  // independent of Min/Max, not flags on them — a schema may
                                  // declare both per side and both apply
    MultipleOf         *BigVal
    Precision, Scale   *int64     // decimal digit bounds (Avro decimal, XSD totalDigits/fractionDigits,
                                  // OData Edm.Decimal)
    // string & bytes — length constraints apply to both (Avro fixed(N) = bytes, MinLength=MaxLength=N)
    MinLength, MaxLength *int64
    Pattern            string      // ECMA-262 regex as written; emitters translate or drop with a diagnostic
    PatternMessage     string      // human-readable validation message (TypeSpec @pattern's second arg)
    // collections
    MinItems, MaxItems *int64
    UniqueItems        bool
    MinProps, MaxProps *int64
}

type Encoding struct {
    Name     string    // "rfc3339" | "unixTimestamp" | "base64" | "base64url" | "iso8601" |
                       // "seconds" | "http-date" | "varint" | "zigzag" | "fixed" |
                       // "packed" | "expanded" | "delimited" | format strings
                       // (packed/expanded are container encodings on List.Encoding; "delimited" =
                       // protobuf group / editions DELIMITED message encoding)
    WireType *TypeRef  // the on-wire primitive when it differs from the logical type
                       // (utcDateTime encoded as int32; bytes as base64 string)
    MediaType string   // content media type of the value itself (Smithy @mediaType on string/blob,
                       // JSON Schema contentMediaType); "" = none
    Schema   *TypeRef  // the shape the encoded value has once decoded (JSON Schema contentSchema):
                       // what a base64 blob or an application/json-typed string holds; nil = unstated
}
```

The logical-type / encoding-name / wire-type triple is TCGC's reification of TypeSpec `@encode`
and also absorbs OpenAPI `format` and Protobuf's `sint*/fixed*` wire variants. `MediaType` and
`Schema` sit beside it because they answer the same question one level in: what the encoded value
*is*. `Schema` is a `TypeRef` like any other schema position — the decoded shape is hoisted into
the registry at its own source pointer, never carried here as a raw payload. Encoding attaches
at the scalar definition or overrides at the property — property wins. Protobuf editions features
lower here per element after the compiler resolves the feature cascade (descriptors expose
resolved values): `field_presence` → `Presence`, `enum_type` → `Closed`,
`repeated_field_encoding` → `List.Encoding`, `message_encoding` → `"delimited"`; remaining axes
(`utf8_validation`, `json_format`) → namespaced `Unmodeled` entries.

### 5.4 XML hints

XML-capable formats (OpenAPI's `xml` object, Smithy's `xmlName`/`xmlAttribute`/`xmlNamespace`/
`xmlFlattened` traits, TypeSpec's `@Xml.*` decorators) describe an XML wire shape that diverges
from the JSON-implied one. This is typed, not an extension, because multiple formats express it
and emitters must act on it. Hints attach at both levels: `TypeCommon.XML` (root element
name/namespace of the shape itself) and `Property.XML` (per-use overrides; property wins).

```go
type XMLHints struct {
    Name      string   // element/attribute name override
    Namespace string   // namespace URI
    Prefix    string
    NodeType  string   // "" | "element" | "attribute" | "text" | "cdata" | "none"
                       // (OpenAPI 3.2 nodeType; "text" covers Smithy httpPayload text)
    Wrapped   bool     // list items wrapped in a container element
}
```

---

## 6. The Values channel

Defaults, constants, literal types, enum member values, and examples are *typed data*, kept
separate from the type graph (TypeSpec's Type-vs-Value split):

```go
type Value struct {
    Kind ValueKind        // "null" | "bool" | "string" | "number" | "bytes" | "symbol" |
                          // "list" | "object" | "ref" | "ctor"
    Bool   bool
    Str    string         // payload for "string" AND "symbol"
    Num    BigVal         // arbitrary precision, decimal string form
    Bytes  []byte         // base64 in JSON form
    List   []Value
    Object []Field        // ordered; Field{Name string; Value Value}
    Ref    *ValueRef      // reference to a declared constant: {Type TypeID; Member string}
                          // (TypeSpec enum-member defaults, references to named consts)
    Ctor   *CtorValue     // constructor-built value (TypeSpec scalar constructors)
}

type CtorValue struct {
    Scalar TypeID         // the scalar whose constructor is invoked
    Name   string         // constructor name ("fromISO", "now", custom inits)
    Args   []Value
}
```

`BigVal` is a decimal-string wrapper: no float64 round-tripping anywhere in the IR. Its text is a
JSON number: `NewBigVal` drops a leading `+`, gives the integer part one digit, drops a trailing
point, and keeps every significant digit and the exponent as written. Its leading significant digit
carries a power of ten within ±(10¹⁸ − 1) (`ir.MaxDecimalExp`), and zero is held at any exponent.
`ir.Decimal` is the one reading of that text (sign, significant digits, that power of ten), never
the value materialized. Order, equality, integrality and §4.5's canonical decimal
(`ir.Decimal.String`) all derive from it.

`symbol` is an interned-symbol value distinct from `string` (Erlang atoms: on the native wire
`ok` ≠ `<<"ok">>`). Emitters without a symbol concept render symbols as strings — explicitly,
not accidentally. `ctor` captures values built by named scalar constructors
(`utcDateTime.now()`, `plainDate.fromISO("2024-05-06")`) — inherently non-literal, so compilers
must not fold them.

---

## 7. Service layer

### 7.1 Services and operation groups

```go
type Service struct {
    ID         ServiceID
    Name       Naming
    Docs       Docs
    Version    string             // per-service version string (Smithy service version); the
                                  // document-level Version remains the API title's version
    Namespace  []string           // source namespace path (TypeSpec/Smithy/proto package)
    Extends    []ServiceID        // client-visible service inheritance (Thrift service extends,
                                  // WSDL 2.0 interface extension, Cap'n Proto interface inheritance);
                                  // inherited operations are walked, never copied
    Groups     []OperationGroup   // hierarchical; a group ≈ TypeSpec interface / Smithy resource / tag
    Auth       []AuthRequirement  // service-level default (OR-of-ANDs, §9)
    CommonErrors []ErrorCase      // errors every operation can return (Smithy service-level errors)
    Protocols  []ProtocolDecl     // declared serde/protocol conventions the service speaks
                                  // (Smithy @protocolDefinition traits like aws.protocols#restJson1)
    Renames    map[TypeID]Naming  // per-service shape presentation names (Smithy service `rename`);
                                  // the TypeID — and Naming on the type — are unchanged
    Servers    []ServerID         // the servers serving the service, most preferred first; empty =
                                  // no declared server does (§10)
    Unmodeled  Unmodeled
    Provenance Provenance
}

type ProtocolDecl struct {
    Name    string                // e.g. "aws.restJson1", "grpc"
    Options RawConfig             // per-protocol options kept raw (the Channel.Bindings pattern)
}

type OperationGroup struct {
    ID         GroupID            // minted by the rule that formed the group (§3.1); document-wide unique
    Name       Naming
    Docs       Docs
    Groups     []OperationGroup   // nesting: Smithy resources, sub-clients
    Operations []Operation
    Resource   *ResourceInfo      // Smithy resource semantics when declared
    Availability *Availability    // groups (TypeSpec interfaces) are versionable
    Unmodeled  Unmodeled
}

type ResourceInfo struct {
    Identifiers []Property                  // resource identity fields
    Properties  []Property                  // resource state fields (Smithy 2.0 resource properties)
    Lifecycle   map[string]OpID             // "create"|"put"|"read"|"update"|"delete"|"list" → op
                                            // (put = create-or-replace with client-provided identifier)
    NoReplace   bool                        // put may create but not replace (Smithy @noReplace)
    InstanceOps   []OpID                    // declared non-lifecycle instance operations (require identifiers)
    CollectionOps []OpID                    // declared collectionOperations — the split drives
                                            // sub-client shape and is a declared fact, not a heuristic
}
```

OpenAPI compilers build groups from tags (policy-controllable: tag-based vs path-prefix-based);
TypeSpec from interfaces/namespaces; Smithy from resources; GraphQL yields three groups
(query/mutation/subscription); Protobuf one group per `service`; Erlang/OTP one group per module.
How a tag builds a group, and nests it under a declared parent, is the OpenAPI compiler's rule
[`tag-groups`](./compilers/openapi.md#tag-groups).

### 7.2 Operation — the protocol-neutral core

```go
type Operation struct {
    ID         OpID
    Name       Naming
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability

    Params     []Parameter        // all logical inputs, protocol-unbound
    Request    *Payload           // body/message content, nil = none
    Responses  []Response         // ordered; success + alternative successes
    Errors     []ErrorCase        // declared failure shapes
    Default    *Response          // the response for every outcome no entry of Responses or Errors
                                  // names, which the source classes neither a success nor a failure
                                  // (OpenAPI `default`); Conditions hold the catch-all alone;
                                  // nil = none

    OneWay     bool               // fire-and-forget: no response EVER exists (OTP cast, AsyncAPI
                                  // send-without-reply, Thrift oneway, JSON-RPC notifications).
                                  // Distinct from a response with no body (an ack still exists);
                                  // validate rejects OneWay with any response, Default included
    Streaming  StreamingMode      // client | server | bidi; "" = does not stream (derived summary
                                  // of the two below)
    RequestStream  *StreamDetail  // client→server streaming semantics, when present
    ResponseStream *StreamDetail  // server→client streaming semantics, when present
    Pagination *Pagination
    LongRunning *LongRunning
    Idempotency Idempotency       // unknown | safe | idempotent | idempotency_token(param)
                                  // safe = no side effects (Smithy @readonly, HTTP GET semantics)
                                  // TokenParam is set exactly for idempotency_token; validate
                                  // rejects either half alone (pass/idempotency-token-mismatch)
    Servers    []ServerID         // the servers it is sent to, most preferred first, in place
                                  // of its service's; empty = its service's (§10)
    Auth       []AuthRequirement  // override of service default; empty slice ≠ nil (empty = explicitly public)
    Tags       []string
    ParameterVisibility  []Lifecycle // visibility filter override for the request view
                                     // (TypeSpec @parameterVisibility); nil = protocol default
    ReturnTypeVisibility []Lifecycle // filter override for the response view; nil = protocol default
    OverloadOf *OpID              // TypeSpec @overload grouping

    Bindings   OpBindings         // §8 — how the core maps onto concrete protocols
    Examples   []Example
    Unmodeled  Unmodeled
    Provenance Provenance
}

type Parameter struct {
    ID         ParamID             // param/<space>/<operation's path>/parameters/<name>/<in>, and
                                   // /<n> after it on the n-th holder of one name and location;
                                   // the bindings, pagination inputs and idempotency token name a
                                   // parameter by it, not by Name, which two locations may share
    Name       Naming
    Type       TypeRef
    Required   bool
    Default    *Value
    Constraints *Constraints
    ValueFrom  *PropPath           // the parameter's value is derived from a location in the
                                   // outgoing/incoming message (AsyncAPI parameter `location`
                                   // runtime expressions) — SDKs may auto-fill it; nil = caller-supplied
    Docs       Docs
    Deprecation *Deprecation
    Availability *Availability
    Examples   []Example
    Unmodeled  Unmodeled
    Provenance Provenance      // the parameter's own declaration; a parameter merged into several
                               // operations (an OpenAPI path-item parameter) points at that one
                               // declaration, not at the operation it was merged into. For a
                               // referenced entry that is the component it names, and the mount
                               // site is not recorded — so inherited-vs-declared is readable off
                               // the pointer only for an entry written inline
    // NOTE: no location here — path/query/header is HTTP-binding detail (§8.1); the
    // location is part of the ID, so one name in two locations is two parameters.
    // A path-item parameter is copied by value into each operation it applies to,
    // with an ID of that operation's own.
}

type Payload struct {
    Contents []Content            // one per media type / message schema — all kept; none for a
                                  // request body declaring no media type, which OpenAPI's
                                  // schemas admit
    Required *bool                // true = the body must be sent, false = it may be omitted;
                                  // nil = the source format does not express body optionality.
                                  // Three states, not two: a format that expresses it treats an
                                  // unstated body as optional, so folding that onto nil would make
                                  // "the format is silent" read as "the document says no".
                                  // Response and message payloads leave it nil — only a request
                                  // body can be omitted — and pass/validate reports one set
                                  // anywhere else (ir/payload-required-outside-request)
    Docs     Docs                 // the body's own documentation, as against any one media
                                  // type's: an OpenAPI Request Body Object's description, a
                                  // TypeSpec @doc on a @body parameter. Omitted when empty,
                                  // unlike a named node's
    Unmodeled  Unmodeled
}

type Content struct {
    MediaType string              // "application/json", "multipart/form-data", "" for non-HTTP
    SchemaFormat string           // schema *language* the type graph was lowered from, media-type
                                  // form (AsyncAPI multiFormatSchema: Avro/Protobuf/RAML/…);
                                  // "" = source-native. The verbatim source schema is preserved in
                                  // Unmodeled (e.g. "asyncapi:schema") for schema-registry workflows
    Type      TypeRef
    Item      *TypeRef            // element shape of a sequential stream declared per media type
                                  // (OpenAPI 3.2 itemSchema for SSE/JSONL/json-seq); nil = not sequential
    ItemEncoding *PartEncoding    // wire encoding of a sequential stream's item (3.2 itemEncoding);
                                  // singular like Item, because it governs every item alike.
                                  // Positional per-item encoding (3.2 prefixEncoding) has no form
                                  // here and stays in Unmodeled — see §12
    Encoding  map[PropID]PartEncoding // multipart/form: per-property (part) wire config, keyed by the
                                  // part property's PropID — a position inside the body model, so
                                  // `pass` resolves a key against that model rather than a
                                  // registry; of a part several declarations name, the one the
                                  // model's flattening takes (§4.3)
    File      *FileInfo           // body is a file upload/download (TypeSpec file bodies, binary
                                  // payloads): how it travels, while Type stays what the source
                                  // declares of it, so nothing that declaration says is lost
    Examples  []Example
    Unmodeled  Unmodeled
}

type PartEncoding struct {
    ContentTypes []string          // media type(s) of this part
    Headers      []Header          // per-part headers
    Multi        bool              // part repeats (array member → repeated parts)
    Filename     bool              // part carries a filename (file part)
    Style        string            // form-style serialization for non-file parts
    Explode      *bool
    AllowReserved bool             // reserved characters pass unescaped in the part's value, as
                                   // HTTPParamBinding.AllowReserved does in a parameter's
}
// TypeSpec tuple-form multipart lowers to a synthesized model whose properties are the parts.

type FileInfo struct {
    IsText   bool                 // textual vs binary contents
    Contents *TypeRef             // declared contents scalar chain (string/bytes extensions); nil = bytes.
                                  // A compiler's lowered schema stays on Content.Type: it need not
                                  // be such a chain, as a type array lowers to a union
    ContentTypes []string         // declared *allowed* content-type set (TypeSpec File<"image/png" |
                                  // "image/jpeg">); runtime Content-Type comes from the file value
    ContentTypeDefault string     // default when the file value carries none
    FilenameLocation string       // "content-disposition" (default) | "path" | "header"
    FilenameWireName string       // wire name when FilenameLocation is path/header
}

type Response struct {
    Name       Naming             // for formats with named outputs; Hint elsewhere
    Conditions ResponseConditions // HTTP status codes/ranges; empty for RPC single-response
    Payload    *Payload           // nil = no body
    Headers    []Header           // response metadata fields
    StatusCodeProp *PropPath      // output member populated from the runtime HTTP status line
                                  // (Smithy @httpResponseCode, TypeSpec non-literal @statusCode);
                                  // the member is suppressed from the body
    Docs       Docs
    Unmodeled  Unmodeled
}

type Header struct {             // one entry of a Headers list; encodes flat
    Property                      // what the header is: name, type, presence, docs
    Style      string             // how the value is written into the field, in
                                  // HTTPParamBinding.Style's vocabulary, as declared; "" = simple
    Explode    *bool              // as declared; nil = the style's default
}

type ResponseConditions struct {
    StatusCodes []StatusRange     // {From,To}: 200–200, 400–499 ("4XX"), 0–0 = default/catch-all
}

type ErrorCase struct {          // a response too: Name/Conditions/Payload/Headers as on Response
    Name       Naming             // for formats with named errors; Hint elsewhere (the status key, neutralized)
    Conditions ResponseConditions
    Payload    *Payload           // nil = no body; its contents' types may be of any kind
    Headers    []Header           // error metadata fields (Retry-After, rate-limit family)
    Fault      string             // "" | "client" | "server" — protocol-neutral fault classification
                                  // (Smithy @error; OpenAPI 4XX/5XX is its HTTP lowering). Drives
                                  // exception hierarchies and default status synthesis
    Retryable  *bool              // Smithy @retryable; nil = unknown
    Throttling *bool              // retryable specifically due to throttling — distinct backoff class
                                  // (Smithy @retryable(throttling: true)); nil = unknown
    Docs       Docs
    Unmodeled  Unmodeled
}
```

Design notes:

- **Parameters carry no protocol location.** The same logical operation is bindable to HTTP
  (locations assigned in the binding), to gRPC (all params fold into a request message), or to a
  channel message. This is the split TCGC makes between `ServiceMethod` and `InputOperation`,
  turned inside out: Morphic stores the neutral core and each binding, and emitters derive the
  per-protocol view.
- **All responses and all content types survive** (oagen's "primary response" collapse is the
  counterexample). The plan layer picks a primary for SDK ergonomics; the IR doesn't.
- **An error's body is of any kind**, a model or a union, a list or a primitive, and
  `irfacts.Index.Usage` marks what it reaches `UsageError` (§4.9), with status conditions and range
  collapsing exactly as Kiota's error mappings (`404`, `4XX`, catch-all).
- **A status's class is the protocol's; a catch-all's is no one's.** An HTTP status code states its
  own class (RFC 9110 §15): a status below 400 lowers to a `Response`, and one from 400 to an
  `ErrorCase` whose `Fault` is `client` for 4xx and `server` for 5xx, since that is what the code
  means rather than a reading of the API. OpenAPI's `default` states none: it answers every status
  the map does not name, successes and failures alike, and is an operation's only description of
  success where the map names no success. So it lowers to `Default`, never to either list, and which
  it answers — the success response where the operation declares none, the error fallback beside a
  success, or both — is the plan's to decide under its policy (emitter-design §3.5), where every
  other choice of a primary response is made (invariants 2 and 6). A format whose catch-all is
  declared an error, as TypeSpec's `@error` model with no status code is, lowers it to an
  `ErrorCase` with the catch-all condition instead. `pass/validate` holds `Default.Conditions` to
  the catch-all alone (`ir/default-response-conditions`), and a `OneWay` operation to having no
  `Default` either.
- **A header is a `Property` with how it is written.** A response header has no binding to state
  its serialization, as a parameter's `HTTPParamBinding` does, so `Header` holds it. Two fields on
  `Property` were the alternative, rejected because they would mean nothing on a model's or a
  resource's property, and a check over a hand-named list of positions would have to keep them off
  those. `Property.Args` takes that route because a field argument can sit at any depth of a model;
  a header is a list of its own, so the type states the rule and no check has to. `Header` embeds
  `Property`, so the two encode flat as one object and a header reads as a property does. Each of
  its fields holds what the source declares, so an emitter can tell a declared `explode: false`
  from none. OpenAPI allows a header the simple style alone and lets `explode` join an object value
  two ways, as TypeSpec's `@header` option does; Swagger 2.0's response-header `collectionFormat`
  also writes a space- or pipe-delimited list, which is why `Style` is a field and not a constant.
- **`OneWay` is on the neutral core**, like `Streaming` and `Idempotency`, because
  fire-and-forget is protocol-independent — an SDK that blocks awaiting a reply on a cast is
  wrong regardless of transport. The AsyncAPI compiler sets it for send-operations without a
  reply, rather than leaving one-way-ness inferable only from binding fields.

### 7.3 Pagination, long-running, streaming

```go
type Pagination struct {
    Strategy   PageStrategy   // cursor | offset | page | link_header | next_link | token
    Inferred   bool           // heuristic (OpenAPI name-matching policy) vs declared (Smithy/TypeSpec)
    InputCursor  *ParamPath   // which input continues iteration
    InputLimit   *ParamPath
    Items        *PropPath    // where result items live in the response — a PATH, not a name
    NextCursor   *PropPath    // continuation source in the response
    NextLink     *PropPath
    PrevLink     *PropPath    // server-driven navigation links beyond next
    FirstLink    *PropPath    // (TypeSpec @prevLink/@firstLink/@lastLink, OpenAPI links)
    LastLink     *PropPath
    TotalCount   *PropPath
}

type PropPath struct {
    Root     *TypeRef  // the type the path roots in; nil = determined by context
                       // (the enclosing response body, message payload, …)
    In       string    // "" = body/payload | "header" — continuation tokens and reply addresses
                       // can live in response/message headers, not just bodies
    Segments []PropID
}
type ParamPath struct{ Param ParamID; Segments []PropID }
// ParamPath.Param, like Idempotency.TokenParam, names a parameter of the operation
// carrying it: another operation's ParamID, or a field argument's, is a well-formed
// reference to the wrong parameter, and pass.Validate rejects it
// (pass/param-binding-mismatch).

type LongRunning struct {
    FinalStateVia string       // "operation-location" | "status-monitor" | "original-uri" | …
    PollingOperation *OpID     // declared poll op (Azure.Core @pollingOperation — a library
                               // convention, not core TypeSpec)
    FinalOperation   *OpID     // declared final-result op (Azure.Core @finalOperation)
    PollingType   *TypeRef
    FinalType     *TypeRef
    ResultPath    *PropPath
}

type StreamingMode string      // "" (none) | "client" | "server" | "bidi"

type StreamDetail struct {
    Events  *TypeRef   // stream element type, of any kind; nil where the contents that stream
                       // name different elements, which stay on the payload. For a format with
                       // event streams it is a WireTagged Union of event models: within an event
                       // model, Property.EventHeader marks frame-header members and
                       // Property.EventPayload marks a raw-payload member (Smithy
                       // @eventHeader/@eventPayload), and per-event content types and terminal
                       // events live on Variant.Event
    Initial *TypeRef   // initial-request / initial-response message preceding the stream
                       // (Smithy event stream initial messages); nil = none
    RequiresLength bool // the streamed content must have a known finite length up front
                        // (Smithy @requiresLength) — changes the generated parameter type
}
```

Streaming mode is on the operation core because it is protocol-independent (gRPC streams, SSE,
WebSocket, Smithy event streams all project onto it). An operation that does not stream holds the
zero mode, as `IdempotencyUnknown` and `AdditionalUnspecified` are their fields' absent states, so
such an operation encodes no member; a `"none"` spelling would add one to nearly every operation
and say nothing `RequestStream` and `ResponseStream` do not. `Streaming` summarizes an operation,
and a stream belongs to one content of its payload, so an OpenAPI operation can offer
`application/json` beside `application/x-ndjson`, and the plan elects among them (emitter-design
§3.1). The wire mechanism (SSE vs JSON Lines vs WS frames vs length-prefixed events) has no field of
its own: over HTTP the streaming content's `MediaType` states it, another binding frames its own
messages, and the plan derives which (emitter-design §3.1). `Content.Item` declares what one item
is; a compiler that reads a stream from a media type alone names the content's `Type` as `Events`,
or `bytes` for a file, whose `Type` describes the whole, a reading `Provenance.Inferred` marks. An
element need not be a union, and `Variant.Event` applies only where it is one. Smithy **waiters**
(`smithy.waiters#waitable`) are *not* folded into `LongRunning` —
acceptor lists and JMESPath matchers are preserved verbatim in `Operation.Unmodeled` (§15).

---

## 8. Protocol bindings

```go
type OpBindings struct {
    HTTP    []HTTPBinding     // OpenAPI/Swagger ops, TypeSpec @route, Smithy http traits.
                              // A slice: one operation may carry several HTTP mappings —
                              // gRPC transcoding's additional_bindings — with the primary first
    RPC     *RPCBinding       // Protobuf/gRPC, Smithy RPC protocols, JSON-RPC
    Message *MessageBinding   // AsyncAPI operations, webhooks
    GraphQL *GraphQLBinding   // query/mutation/subscription field binding. GraphQL subscriptions
                              // bind here + streaming fields on the core — NOT via MessageBinding
                              // (GraphQL defines no channel; synthesizing one is deployment-aware
                              // policy, marked Inferred, never a compiler default)
    OTP     *OTPBinding       // Erlang/OTP behaviour operations (§8.5)
}
```

An operation has at least one binding; more than one is legal (Smithy service exposed over both
HTTP and RPC; gRPC with HTTP transcoding).

### 8.1 HTTP

```go
type HTTPBinding struct {
    Method      string             // as sent on the wire (OpenAPI 3.2 additionalOperations keys
                                   // carry exact capitalization; QUERY and custom methods are legal)
    URITemplate string             // RFC 6570 — the one true path representation (Kiota + TypeSpec agree);
                                   // empty on a webhook binding, which has no path
    HostPrefix  string             // endpoint host prefix, may contain {param} labels (Smithy @endpoint)
    SharedRoute bool               // multiple operations legally share method+path, disambiguated by
                                   // request content (TypeSpec @sharedRoute) — validate must not
                                   // reject the duplicate, and single-route emitters must merge
    ParamBindings []HTTPParamBinding
    RequestContentTypes  []string  // priority-ordered
    ResponseBodyPath *PropPath     // HTTP response body = this sub-field of the response type
                                   // (gRPC transcoding response_body); nil = the whole payload
    SuccessStatus map[int]int      // response index → primary status (denormalized convenience; conditions are the truth)
    Compression *RequestCompression // client MUST compress the request body (Smithy @requestCompression)
    ChecksumRequired bool          // client MUST send a payload checksum (Smithy @httpChecksumRequired)
    PatchImplicitOptionality *bool // nil = protocol default (PATCH projections make properties
                                   // optional); false = disabled (TypeSpec @patch implicitOptionality)
    IsWebhook   bool               // OpenAPI 3.1 webhooks: direction is inbound
    WebhookName string             // OpenAPI 3.1 webhooks: the webhooks-map key, verbatim — an
                                   // event's name, not a path; only a webhook binding carries one
    Callbacks   []Callback         // out-of-band operations keyed by runtime expressions
    Unmodeled   Unmodeled
}

type RequestCompression struct { Encodings []string } // priority-ordered ("gzip", …)

type HTTPParamBinding struct {
    Param      ParamID            // this operation's own Operation.Params entry it binds, by ID
    ParamPath  []PropID           // nested source field within the logical param, when the binding
                                  // targets a sub-field of a message-typed param (gRPC transcoding
                                  // {book.name}, dotted query params); empty = the whole param
    Location   HTTPLocation       // path | query | querystring | header | cookie | body |
                                  // body_property | host
                                  // host = param fills a HostPrefix label (Smithy @hostLabel)
                                  // querystring = the whole query string serialized from one schema
                                  //   (OpenAPI 3.2 in: querystring, combined with ContentType)
    WireName   string
    Style      string             // simple | form | label | matrix | deepObject | pipe/space-delimited
    Explode    *bool
    AllowReserved bool
    AllowEmptyValue bool          // the client may send an empty value in place of omitting
                                  // the param (OpenAPI/Swagger allowEmptyValue); no effect under
                                  // a Style that cannot write one (space/pipe-delimited,
                                  // deepObject)
    PathPattern string            // multi-segment path pattern constraint for this param
                                  // (gRPC transcoding {name=shelves/*/books/*}); "" = single segment.
                                  // The URI template uses reserved expansion; emitters that cannot
                                  // validate the pattern drop it with a diagnostic
    Prefix     string             // map-typed param spread as prefixed wire entries:
                                  // prefixed headers (Smithy @httpPrefixHeaders) or catch-all
                                  // query maps (@httpQueryParams with Prefix "")
    ContentType string            // param serialized as a media type (OpenAPI content-style params)
    BodyPath   []PropID           // for body_property: where in the body model it lands (TypeSpec HttpProperty)
}

type Callback struct { Expression string; Operations []OpID }
```

`AllowEmptyValue` sits on the binding with a parameter's other serialization facts, the
per-parameter wire detail `prior-art.md` records the client IR keeping there. OpenAPI calls the
field NOT RECOMMENDED, but a document that writes it still states it, Swagger 2.0 writes it too, and
a request validator or a mock server needs it to accept `name=`. It is a plain bool beside
`AllowReserved`, since a declared `false` and an absent one mean the same.

Every logical parameter — identified by its `ParamID`, not its name — is bound exactly once **per
non-host location; a `host` binding is additive** — Smithy `@hostLabel` members expand into the host
prefix *and* still serialize at their modeled location, by spec. TypeSpec's `HttpProperty` role+path
flattening is the model here: nested `@header`/`@body` annotations resolve to explicit
`(role, wire name, path)` bindings rather than restructured models.

An OpenAPI `webhooks` entry is keyed by the name of an event the provider sends, not by a URL: where
the request goes is arranged outside the document, by an out-of-band registration for instance. Its
operations' bindings carry that key in `WebhookName` and leave `URITemplate` empty, so a `{tenant}`
in the key is part of a name rather than a variable nothing binds. `irverify` reports a route in the
field `IsWebhook` does not select (`ir/webhook-route`). The key stays verbatim rather than becoming
a `Naming`, because the operation's `Name` is what an emitter renders, synthesized from the method
and this key when no `operationId` is declared, and an empty key is legal and would name nothing. A
callback operation keeps its runtime expression in `URITemplate`.

### 8.2 RPC

```go
type RPCBinding struct {
    System      string     // "grpc" | "smithy-rpc" | "connect" | "jsonrpc" | …
    FullMethod  string     // "/pkg.Service/Method"
    InputType   *TypeRef   // the request message type params fold into (nil = synthesize from Params)
    ParamStructure string  // "" | "by_name" | "by_position" | "either" — how params serialize
                           // (JSON-RPC positional vs named; OpenRPC paramStructure). Param order
                           // is already source order per design rule 5; this is the mode
    IdempotencyLevel string
    Unmodeled   Unmodeled
}
```

### 8.3 Messaging / events

```go
type MessageBinding struct {
    Channel   ChannelID
    Direction MsgDirection     // send | receive (application perspective, AsyncAPI 3 semantics)
    Messages  []MessageID      // which of the channel's messages this operation uses
                               // (must be a subset — validated)
    Reply     *Reply           // request-reply semantics; nil = none. A send-op with no Reply and
                               // no Responses is one-way (set Operation.OneWay)
    Bindings  map[string]RawConfig // operation-level protocol bindings kept raw
                                    // (kafka groupId/clientId — constrain SDK client config)
    Unmodeled  Unmodeled
}

type Reply struct {
    Channel  *ChannelID    // static reply channel; nil when the address is dynamic-only.
                           // (An AsyncAPI reply channel's own address is null by spec)
    Address  *PropPath     // dynamic reply address: where in the *request* message the reply
                           // destination lives, e.g. In:"header", Segments:[replyTo]
                           // (AsyncAPI Operation Reply Address runtime expressions)
    Messages []MessageID   // reply payload message set (a subset of Channel's when set — validated)
    Docs     Docs
}

type Channel struct {
    ID        ChannelID
    Name      Naming
    Address   *string            // topic/routing key/path, may contain {params}.
                                 // nil = unknown/runtime-assigned address (load-bearing: reply
                                 // channels and dynamic topics; SDKs expose a runtime address arg)
    Docs      Docs
    Tags      []string
    Params    []Parameter
    Messages  []MessageID        // the channel's message set — messages live in Document.Messages
    Servers   []ServerID         // the servers the channel is available on, most preferred first;
                                 // empty = no declared server is (§10)
    Bindings  map[string]RawConfig // protocol-specific ("kafka", "amqp", "ws", "mqtt") kept as namespaced raw config
    Unmodeled  Unmodeled
    Provenance Provenance
}

type Message struct {
    ID          MessageID
    Name        Naming
    Payload     Payload
    Headers     *TypeRef           // header *schema* — an object-constrained model hoisted into the
                                   // type registry like any anonymous type (headers can be named,
                                   // composed, even Avro-defined; and $message.header#/… paths need
                                   // a type to resolve against). Emitters compute flat lists per §4.3
    CorrelationID *PropPath        // In: "header" | "" (payload)
    ContentType string
    Tags        []string
    Docs        Docs
    Deprecation *Deprecation
    Examples    []Example          // correlated header+payload example pairs (Example.Headers + .Value)
    Bindings    map[string]RawConfig // message-level protocol bindings (kafka message key, …)
    Unmodeled   Unmodeled
    Provenance  Provenance
}
```

Kafka/AMQP/MQTT binding minutiae stay as namespaced `RawConfig` rather than modeled
structs — they are protocol deployment detail, not API shape, and modeling them structurally
would chase every AsyncAPI binding spec revision. The raw-bindings channel exists at every
level that AsyncAPI defines bindings: server, channel, operation, and message.

### 8.4 GraphQL

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
of optional properties — that is the union-to-optional-fields collapse rule 2 forbids), custom
scalars → `Scalar` with `Base = nil`, entry-point fields → operations (field args → `Params`,
field type → response), nested field arguments → `Property.Args` at any depth, subscriptions →
`GraphQLBinding{Kind: "subscription"}` + `StreamingMode: server` + `ResponseStream`.

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

### 8.5 Erlang/OTP

```go
type OTPBinding struct {
    Behaviour  string    // "gen_server" | "gen_statem" | "gen_event"
    Kind       string    // "call" (synchronous request→reply) | "cast" (fire-and-forget; the
                         // operation also sets OneWay) | "info" (raw message send)
    Process    ChannelID // the channel modeling the target process: Address = registered name
                         // (nil Address = unregistered/runtime pid); registration kind
                         // (local/global/via) in Channel.Bindings["otp"]
    RequestTag *Value    // tag of the request tuple (a symbol Value, e.g. 'get');
                         // nil = the whole term is the request
    Unmodeled  Unmodeled  // source constructs the IR does not model, kept verbatim (§12)
}
```

An OTP compiler consumes module type information (`-spec`/`-type` on behaviour callbacks and
API functions). Lowering conventions: a gen_server ≈ one `Channel`; `handle_call`/`handle_cast`
APIs are `Operation`s with an `OTPBinding`; unsolicited `handle_info` messages are channel
`Messages` consumed via `MessageBinding{Direction: receive}`; a gen_event manager is a Channel
with one message shape per event. Tagged-tuple protocols (`{ok, V} | {error, R}`) are `WireTagged`
Unions of Tuples discriminated by `Discriminator.Index`; records are `Model{Positional: true}`
with the record tag as an index-1 `Literal`-typed property. Splitting a tagged reply union into
`Responses` vs `Errors` (`{error, Reason}` is in-band data, not transport failure) is injectable
compiler policy, marked `Inferred`. Erlang `string()` is `[char()]` and must lower as a List of
a char-ranged scalar — never as PrimKind `string`. Delayed replies (`noreply` +
`gen_server:reply/2`) are invisible at the protocol surface and need no representation; call
timeouts and `multi_call`/`send_request` machinery are SDK runtime policy, not IR.

---

## 9. Auth

```go
type AuthScheme struct {
    ID   AuthID
    Name Naming
    Kind AuthKind
    // AuthKind: apiKey | http_basic | http_bearer | oauth2 | openid_connect | mutual_tls |
    //           user_password | x509 | symmetric_encryption | asymmetric_encryption |
    //           sasl_plain | sasl_scram_sha256 | sasl_scram_sha512 | sasl_gssapi | custom
    // The SASL family, user_password, and x509 are how Kafka/AMQP clients authenticate
    // (AsyncAPI securitySchemes). AsyncAPI `httpApiKey` lowers to apiKey; AsyncAPI `apiKey`
    // (transport user/password slot) lowers to apiKey with In: user|password. X509 is distinct
    // from mutual_tls (certificate as credential vs mutual verification) — compilers must not
    // conflate them.
    Docs Docs
    Deprecation *Deprecation
    // apiKey
    In       string      // header | query | cookie | user | password
    KeyName  string
    // http
    Scheme       string  // bearer, basic, digest…; also legal for apiKey (Smithy @httpApiKeyAuth scheme)
    BearerFormat string
    // oauth2 (Flows may be empty: OAuth2MetadataURL can name the authorization server
    // instead, and a source can state neither)
    Flows []OAuthFlow
    OAuth2MetadataURL string // RFC 8414 authorization-server metadata (OpenAPI 3.2)
    // openid
    OpenIDConnectURL string
    Credentials *TypeRef // the credential a client supplies, where the source declares its shape
    Unmodeled  Unmodeled
    Provenance Provenance
}

type OAuthFlow struct {
    Kind                     string       // authorization_code | client_credentials | implicit | password | device
    AuthorizationURL         string       // the device flow's deviceAuthorizationUrl rides it
    TokenURL                 string
    RefreshURL               string
    Scopes                   []OAuthScope // source order, each name once
    TokenEndpointAuthMethods []string     // IANA "OAuth Token Endpoint Authentication Methods" values
    Unmodeled                Unmodeled
}

type OAuthScope struct { Name, Description string }

type AuthRequirement struct {          // one *option*
    Schemes []SchemeUse                // ALL must be satisfied together
}
type SchemeUse struct { Scheme AuthID; Scopes []string }
// []AuthRequirement on a service/operation/server = OR across options (TypeSpec/OpenAPI security
// semantics). Option order is PRIORITY order — clients pick the first supported option (Smithy
// @auth is priority-ordered; the Smithy compiler materializes the alphabetical default when
// @auth is absent). Compilers whose source has no ordering semantics emit source order.
// An empty option (AuthRequirement{Schemes: []}) inside a non-empty list means "no auth is one
// acceptable choice" (TypeSpec NoAuth in a union, Smithy @optionalAuth, OpenAPI's empty security
// requirement) — distinct from Operation.Auth = [] (explicitly public).
```

**Scopes keep their order.** A flow's scopes are a list of name and description, in the order the
source writes them, as a `Value.Object`'s members are (§6): documentation and consent screens show
them as the author did, and a map would sort them. A scope with no description has an empty one;
TypeSpec writes scope names alone. A name appears once per flow, as the map made it: two entries
would give one scope two descriptions, so a compiler folds a source that repeats one, and `irverify`
reports a repeat (`ir/oauth-scope-duplicate`).

**How a client authenticates to the token endpoint** is `TokenEndpointAuthMethods`, spelled as the
IANA registry RFC 8414's `token_endpoint_auth_methods_supported` draws on spells it
(`client_secret_basic`, `client_secret_post`, `private_key_jwt`, …). It is a set: the methods the
endpoint accepts, none blank and none repeated. The registry is open, so a value outside it is kept
as written, and an empty list means the source does not say, leaving the choice to the SDK's
runtime policy (`emitter-design.md` §6). No format the matrix covers states it as a field; an
OpenAPI document that does so writes a vendor extension, which stays under `Unmodeled` until a
promotion target reads it.

**A declared credential shape** is `Credentials`, a reference to a registered type, for a scheme —
typically `custom` — whose credential is several fields combined into a request rather than the
one value its kind implies. Nil means the source declares no shape. No compiler fills it yet:
building that type out of an extension would mint a node (§4.3), which is a design of its own.

## 10. Servers

```go
type ServerID string                    // server/<space>/<pointer of its Server Object>, or a
                                        // minted one: server/default

type Server struct {
    ID          ServerID
    Name        Naming                  // servers are named entities (AsyncAPI name-keyed servers,
                                        // OpenAPI 3.2 Server.name)
    URLTemplate string                  // may contain {variables}
    Description Docs
    Variables   []ServerVariable        // {Name, Default, Enum []string, Docs, Unmodeled}
    Protocol    string                  // "https" default; "kafka", "wss", … for messaging servers
    ProtocolVersion string              // e.g. Kafka "3.5", AMQP "0-9-1" (AsyncAPI)
    Tags        []string
    Auth        []AuthRequirement       // server-scoped security — AsyncAPI's primary auth placement
                                        // (broker connections authenticate per server; different
                                        // servers of one service may require different schemes)
    Bindings    map[string]RawConfig    // server-level protocol bindings kept raw
    Unmodeled   Unmodeled
    Provenance  Provenance              // where it is declared; no pointer for one the format
                                        // supplies where the document declares none
}
```

**A registry, and scopes that name it.** `Document.Servers` holds the servers the source declares,
keyed by ID, whichever object declares them: a document's own list, an OpenAPI path item's or
operation's, an AsyncAPI document's name-keyed map. Where a compiler keeps an object whole rather
than lowering it, as the OpenAPI compiler keeps a component no reference reaches, its servers stay
inside it. The scopes a server serves name it: `Service.Servers`, `Channel.Servers` and
`Operation.Servers` are each a list of IDs, most preferred first, since a consumer that needs one
server, an SDK client's default among them, takes the first it can use (emitter-design §8.10). An
operation's non-empty list replaces its service's for that operation, as its `Auth` replaces its
service's; an empty one leaves its service's. An empty service or channel list names no server. It
does not mean every server, because the registry holds servers that serve one operation alone; a
format that reads a scope naming none as served by all of them, as AsyncAPI reads a channel, lists
them all. A server no scope names is still a declaration and stays: an OpenAPI path item's list that
every one of its operations overrides is registered and named by none.

**Identity.** A server is identified by where it is declared, the pointer of its Server Object in
the document declaring it (`server/openapi/paths/~1a/servers/0`), so a path item two paths mount
declares one server that both mounts name, and the ID's path is the pointer its provenance records,
which `irverify` holds. A server the format supplies where the document declares none is minted
(§3.1): OpenAPI's `/`, for a `servers` field absent or empty, is `server/default`, in a space no
pointer reaches, and records the whole document as its position. Its missing pointer is what tells
it from a declared `/`.

**Why IDs and a map rather than indices.** Until 0.22.0 the scopes indexed an ordered
`Document.Servers`, and an empty list meant every server, which held while the only servers were the
document's own. Servers an operation declares break both halves: an index stops naming its server
once a pass removes or reorders one (invariant 3 exists for this), and "every server" would send a
service to servers only one of its operations uses. Servers are named entities, and every other
reference between named entities is by ID.

**What an operation's servers are not.** `HTTPBinding.HostPrefix` stays where it is: Smithy's
`@endpoint` prefixes whichever server is chosen with a host label, so it qualifies a server rather
than naming one. And `Operation.Servers` sits on the neutral core beside `Auth`, which an operation
overrides the same way, rather than on a binding: it says where the operation is sent, whichever
binding sends it. An operation bound to a channel leaves it empty, since its channel names its
servers.

## 11. Versioning & availability

```go
type Availability struct {
    Added      []string  // version labels from Document.Versions, ordered — multiple entries
    Removed    []string  // support add/remove/re-add cycles (v1 present, v2 removed, v3 re-added)
    Deprecated string
    RenamedFrom     []VersionedName // {Version, Name}
    TypeChangedFrom []VersionedType // {Version, Type TypeRef} — property/return type changed
                                    // across versions (TypeSpec @typeChangedFrom)
    RequiredChanged []VersionedBool // {Version, WasRequired} — optionality flipped at a version
                                    // (TypeSpec @madeOptional/@madeRequired); the slice pass
                                    // reconstructs Required for older snapshots from it
}
type VersionedBool struct { Version string; WasRequired bool }
```

The IR stores the **timeline** (TypeSpec model); the `version-slice` pass produces a concrete
snapshot document per version for consumption. Emitters always receive snapshots — they never
interpret availability themselves. Formats without versioning semantics simply leave this nil.

`Availability` attaches everywhere versioning decorators can: `TypeCommon`, `Property`,
`Operation`, `Parameter`, `EnumMember`, `Variant`, and `OperationGroup` — a versioned enum
member or union variant must disappear from pre-`Added` snapshots. Smithy `@since` (free-form,
no version registry) lowers to `Added` only when the compiler synthesizes labels; otherwise it
is kept verbatim under `Unmodeled`.

## 12. Docs, deprecation, examples, unmodeled constructs

```go
type Docs struct {
    Summary     string
    Description string        // CommonMark; may contain {t:TypeID} cross-reference tokens that
                              // emitters resolve to language-appropriate links (Kiota's doc templates)
    ExternalDocs []Link       // {URL, Description}
}

type Deprecation struct { Message, Since, RemovalVersion, RemovalDate string }
// A scheduled removal is two fields because a version and a date are two facts, not two
// spellings of one: a document may state either or both, and neither is derivable from the
// other without a release calendar the IR does not have. A consumer deciding whether removing
// a deprecated entity is breaking compares a removal date against a release date, so it must
// be able to tell which fact it holds without re-parsing the string. RemovalDate is the
// source's own text, unparsed and unnormalized — no source format defines the field, so none
// defines its format either.

type Example struct {
    Name        string
    Summary     string
    Description string
    Value       *Value        // single-value examples (schemas, properties, parameters);
                              // for message examples: the payload
    Headers     *Value        // message examples: the correlated header values (AsyncAPI message
                              // examples are header+payload PAIRS — never split them)
    Input       *Value        // operation scenarios: paired input ↔ output/error
    Output      *Value        // (Smithy @examples, TypeSpec @opExample parameters/returnType)
    Error       *ErrorExample // the scenario ends in this error instead of Output
    ExternalURL string
    Unmodeled   Unmodeled
}
type ErrorExample struct { Type TypeRef; Content Value }
// Field legality is contextual (validated): Value/Headers on types, properties, parameters,
// contents, and messages; Input/Output/Error on operations. An Example never mixes the two arms.

type UnmodeledReason string     // vendor_extension | validation_only | degraded_lowering |
                               // no_ir_home | out_of_scope

type UnmodeledEntry struct {
    Reason     UnmodeledReason    // why this is here rather than modeled
    Value      RawValue          // the source construct, verbatim
    Provenance Provenance        // where the construct itself was written
}

type Unmodeled map[string]UnmodeledEntry
// keys are namespaced by origin: "openapi:x-rate-limit", "smithy:aws.api#arn",
// "graphql:@key", "typespec:@myOrg/decorator", "asyncapi:bindings.kafka", "erlang:opaque"
// RawValue is the source JSON preserved verbatim.

type RawConfig map[string]RawValue
// declared protocol configuration the IR models a field for but leaves untyped inside:
// Server/Channel/Message/MessageBinding `Bindings`, `ProtocolDecl.Options`
```

`Unmodeled` is the lossless escape hatch: any source construct without a first-class IR node
survives, namespaced so two formats never collide, and typed passes can promote well-known
entries (`x-ms-pagination` → `Pagination`) without losing the original. The name states the one
property every member shares — the IR does not model it — rather than borrowing one format's
word for the concept, and rather than naming the guarantee: surviving verbatim is what the field
promises about an entry, but being unmodeled is why the entry is there at all, and `Reason`
records which flavour of unmodeled it is. OpenAPI/AsyncAPI say
*extensions*, Protobuf *options*, GraphQL *directives*, Smithy *traits*, TypeSpec *decorators*,
Erlang *module attributes* — and Protobuf's own `extensions` means something else entirely
(reserved field-number ranges), which the IR models as `Model.ExtensionRanges` and
`Property.ExtensionOf`.

Each entry carries two things beyond the value. **`Reason`** says why the construct is
unmodeled, so a consumer can take the subset it cares about: the validation emitter §4.7
anticipates wants `validation_only` and not vendor noise, while a spec linter wants
`degraded_lowering` and `no_ir_home` and nothing else. It is a property of the construct, never
of the diagnostic emitted beside it — one diagnostic code routinely spans preservation,
weakening, and outright dropping, so it says nothing about why. **`Provenance`** locates the
construct itself rather than the node carrying it, so an emitter can report on an entry at its
own source position instead of the enclosing schema's — falling back to the declaring position
only where the entry combines several keywords into one synthesized object and no single node
addresses it (`openapi:if-then-else`, `openapi:contains`, `openapi:unevaluated`). Such an entry's
`Source` names the input document that wrote every keyword it combines, when one did, and the
declaring position's otherwise: an overlay that wrote the whole construct is credited with it, and
one that only added to it leaves it with the document that declares it.

A key names the origin format, then the construct — and, where one carrier holds constructs from
more than one source object, the path between them. `openapi:x-rate-limit` is the extension on the
node's own object; `openapi:info/contact/x-id` is the contact block's, kept on the document;
`openapi:encoding/<part>/x-vendor` is one form part's, kept on the content. The scope is
load-bearing, not decoration. Most objects lower to a node carrying an `Unmodeled` map of their own
and take no scope, but the rest ride on the nearest node that does — an info block's on the
document, a path item's on each of its operations, an encoding's on the owning content — and
several of those reach one map, where two objects each writing `x-id` would be one key whose
surviving entry depended on which lowering ran last. That is the order-dependence §4.3 forbids for
minted node IDs, and it takes the same remedy: a namespace of its own rather than a shared one.

A scope is a fixed path the compiler chooses, and where it addresses an object the *document* names
— one form part among several, one callback among several — that name is one segment, escaped
RFC 6901 style exactly as a pointer segment is. The escaping is what makes the scope injective and
is not cosmetic: parts named `q` and `q/x-a` otherwise write `openapi:encoding/q/x-a/x-b` between
them, and which entry survives follows the order the two were declared in, silently, in both
orders. That is the failure §4.3 describes for a minted node reusing a pointer, reached by a
different route.

Scoped and unscoped keys stay apart on top of that, because every vendor-extension name begins with
`x-` and no scope does. A scope opens with a keyword the compiler writes, and where one scope on a
carrier extends another, what it adds opens with one too. A segment the document names may begin
with `x-`, as a callback named `x-hook` keeps its extensions under `openapi:callbacks/x-hook/`, but
only after such a keyword. The same gap separates both from the non-extension entries a shared
carrier holds under those scopes, such as `openapi:encoding/<part>/prefixEncoding`.

| Reason | Meaning |
|---|---|
| `vendor_extension` | The source format assigns the key no semantics at all (`x-*` and every format's equivalent). The key set is unbounded and nothing can be inferred from the value. |
| `validation_only` | Validation logic rather than data shape: the structural picture is complete without it, and no target type system or sibling source format has an equivalent (§4.7). |
| `degraded_lowering` | No faithful target for the shape as written — the IR has no combinator for it, or no target type system holds it — so it was lowered to a documented weaker shape with the original kept beside it (§4.8). |
| `no_ir_home` | Expressible in target languages, and no boundary the IR draws on purpose: the position simply has no field for it yet. A gap expected to close — each of these is a promotion candidate. |
| `out_of_scope` | A boundary the IR draws on purpose (§15): runtime machinery or per-target SDK policy rather than API shape. Kept so nothing is lost, but no node is coming — the intended consumer is an emitter policy layer, not a promotion pass. |

`no_ir_home` and `out_of_scope` are the pair worth keeping straight, because they read alike and
mean opposite things: the first is an admission (a field the IR should probably grow), the second
is a decision (§15's list, which exists precisely so those constructs are never mistaken for
oversights). A promotion pass mining the IR for its next typed node reads `no_ir_home` and must
skip `out_of_scope`; nothing about the key or the value distinguishes them, which is the whole
reason `Reason` is data rather than a comment.

**Reporting a kept construct is the compiler's.** It is the stage that chose to keep the construct
rather than model it, so its specification (§14) states, for every construct it keeps under a reason
other than `vendor_extension`, the diagnostic the construct draws, or that it draws none.
`Document.Diagnostics` carries those reports to every consumer, and an emitter acts on an entry's
`Reason` without reporting it again (emitter-design §4.14).

Sorting entries into *typed* buckets instead was considered and rejected: the defining property
of an entry is that it is unmodeled, so anything with enough known semantics to be sorted into a
bucket has enough to be modeled properly, and sorting would make the compiler guess — a
heuristic, which invariant 6 then requires to be injectable, marked `Inferred`, and disableable.
`Reason` gives the same discrimination as data, and callers already tell formats apart by key
prefix.

For the escape hatch to hold, **every node that can carry source metadata has an `Unmodeled`
field** — including `Response`, `ErrorCase`, `Payload`, `Content`, `Example`, `ServerVariable`,
`OAuthFlow`, and every binding struct (new spec revisions land fields on exactly these objects).
Per-file metadata (protobuf file options) is keyed by path in `Document.Unmodeled`.

`RawConfig` is the neighbouring map type, not a second escape hatch: it holds declared protocol
configuration the IR models a field for and deliberately leaves untyped inside (AsyncAPI
bindings at server, channel, operation and message level; `ProtocolDecl.Options`). Its values are
bare `RawValue`, not `UnmodeledEntry` — those entries are there because the source declared them
where the IR expects them, so there is no reason to record and no unmodelled construct to locate.

#### Promoting a vendor extension into the field it is the only spelling for

Several typed fields model information no source format gives a keyword for, so the only way a
document can state it is a vendor extension:
`Deprecation.Message`/`Since`/`RemovalVersion`/`RemovalDate`, `Enum.Closed`, `Pagination.*`,
`LongRunning`, `Idempotency`, `ErrorCase.Retryable`/`Throttling`, `Enum.Flags`,
`EnumMember.Name`, `Sensitive` and `Secret`. Reading such an extension into its field is
**promotion**, and because the format assigns an `x-*` key no semantics at all, promotion is a
heuristic — invariant 6 applies to it in full. Four rules, so that no emitter has to re-derive
this from `Unmodeled` and no two derive it differently:

1. **The mapping is injectable policy, default-on and disableable**, per compiler. Its default
   contents are conventions, not standards: nothing in any specification says `x-deprecated-reason`
   means what its name suggests, so a caller may replace the mapping outright.
2. **The extension stays where it was.** A promotion is a second reading of a preserved
   `Unmodeled` entry, never a move, and the entry keeps its `vendor_extension` reason. That is what
   makes it reversible: a consumer that disagrees with the guess reads the entry instead. It also
   makes losslessness independent of the policy — a disabled promotion loses nothing.
3. **The node records that it was inferred**, in its own `Provenance.Inferred`, naming the
   heuristic. `Inferred` holds one string and a node can be reached by more than one heuristic, so
   the names are listed rather than overwritten, and a name already listed is not repeated. The
   list joins the names with commas in the order the heuristics first applied, none empty; names
   compare exactly, and the IR declares no set of them. `TypeRef.Inferred` (§3.3) lists them the
   same way. The list has a type of its own, `ir.Inferred`: `ir.JoinInferred` writes the form and
   `Inferred.IsCanonical` reads it. A conversion or a decoded document skips the join, and two
   spellings of one list would compare unequal, so `irverify` reports a stored list in any other
   form (`ir/inferred-not-canonical`), wherever it sits.
4. **A node with no `Provenance` is not promoted into.** A node carrying a `Deprecation` and no
   provenance could not satisfy rule 3, and a heuristic that cannot be audited is worse than an
   empty field. Giving such a node a provenance is a change to this document, and the promotion
   follows it rather than preceding it — which is the order `Parameter` went through, and it held
   this rule's only named instance until it gained the `Provenance` §7.2 now gives it. `Variant`
   (§4.4) and `EnumMember` (§4.5) are the instances today: each carries a `Deprecation` and no
   provenance of its own, so no key maps into either until one of them gains one.

What the OpenAPI compiler's default mapping reads, and what it does with a value the field cannot
hold, is its rule [`extension-promotion`](./compilers/openapi.md#extension-promotion). Others of
its rules decide what reaches the IR from a document of a given minor —
[`unmodeled-dialect-fields`](./compilers/openapi.md#unmodeled-dialect-fields),
[`later-version-construct`](./compilers/openapi.md#later-version-construct), which reads the keys
beside a 3.0 schema's `$ref` as 3.1 does,
[`minor-required-fields`](./compilers/openapi.md#minor-required-fields), and
[`nullable`](./compilers/openapi.md#nullable), whose policy reads `nullable` wherever it is written,
though 3.0 confines it to a `type` beside it and 3.1 removed it, and keywords beside a `$ref` that
admit null as a nullable reference — and
[`unreferenced-components`](./compilers/openapi.md#unreferenced-components) keeps the component
entries no reference reaches.

### 12.1 One structural home per declaration

Documentation, deprecation, XML hints, examples, vendor extensions, validation-only keywords and
value constraints are **position-scoped**: they bind the position they were written at, not the
shape that position resolves to. A source schema that reduces to a shared node — the `string`
primitive, the schemaless top type, or another type reached through a `$ref` — must therefore
never write them onto that node, because every other position resolving to the same shape would
then read them as its own.

Each such declaration gets exactly **one** home, chosen by the position rather than by what the
body lowered to:

| the position | where the declaration's annotations live |
|---|---|
| carries an `ir.Property` or `ir.Parameter` of its own — a model property, a header, an operation parameter | that carrier |
| carries neither — a component, an `items`, `additionalProperties`, `prefixItems[n]`, a `patternProperties` value, a union branch, a media-type or item schema, a `$ref`-hoisted sub-schema | a type node the position owns, hoisting a `Scalar` alias over the shared target when the body reduced to one |

Two consequences a compiler must hold to. **A carrier position never hoists an alias**: a
documented scalar field keeps its description on `Property.Docs`, where an emitter rendering that
field reads it, rather than on an anonymous node the field points at. **A non-carrier position
hoists only when the declaration says something** one of those homes holds — a bare
`items: {type: string}` gains nothing by owning a node, so it keeps resolving straight to the
shared primitive and the type graph stays free of an anonymous node per element position.

Where a carrier holds annotations of its own alongside a schema's — a header and a parameter both
write a `description` beside their schema — the carrier's own win: they describe that input,
while the schema's describe its type.

Constraints follow the same rule with one exception on the carrier side. `Property.Constraints`
and `Parameter.Constraints` are filled whether or not the schema hoisted a node, because no node
such a schema can hoist holds them: an object lowers to a `Model` and a formatted scalar to a
`Scalar` carrying an `Encoding`, and neither has a field for the scalar bounds.

### 12.2 What a reference carries down, and what it does not

A `$ref` divides the declarations at its target in two, and the line falls between annotations and
constraints.

**Annotations merge onto the use site.** Documentation, deprecation, visibility and `default` are
read off the referent and written onto the referencing `Property`/`Parameter` with **use-site
precedence** ([`ref-use-site`](./compilers/openapi.md#ref-use-site)), field by field. Each is a
single value that one position may restate for another — a `description` beside the `$ref` says what
*this* input is, replacing the target's — so the two can never both be true at once, and any
consumer reading both would need this precedence rule anyway. Applying it once, in the compiler, is
what keeps every carrier alike.

An OpenAPI Reference Object's own `summary` and `description` fold onto the use site the same way,
by the OpenAPI compiler's rule
[`reference-object-siblings`](./compilers/openapi.md#reference-object-siblings).

**Constraints do not merge, and are never copied to a use site.** Bounds *conjoin*: `maxLength: 64` on the
referent and `maxLength: 100` beside the `$ref` are both in force, and the admitted value is the
narrower of the two. There is no precedence to apply — merging with use-site precedence would
publish `100` as the whole truth and lose the bound the document actually enforces. So
`Property.Constraints` and `Parameter.Constraints` hold what their own position declared and
nothing else, and every node reached through `TypeRef` keeps its own.

The rule a consumer needs follows from that, and it is the reason this is written down rather than
left to be inferred from an empty struct: **an absent `Constraints` at a use site means that
position declared no bound, never that the value is unbounded.** The effective bound is the
conjunction of the use site's own `Constraints` with those of every node reached from its
`TypeRef`; a consumer that wants it — a validator emitter, a differ comparing two revisions —
resolves the reference and intersects. A differ that reads use sites alone sees no change when a
shared component's `maxLength` moves, because the change is at the component, which is the one
place it was declared and the one place it is recorded.

## 13. Provenance & diagnostics

```go
type Provenance struct {
    Source   int              // index into Document.Sources, or NoSource (-1)
    Pointer  jsontext.Pointer // RFC 6901 pointer into that source; "" locates nothing finer than the source
    Position Position         // 1-based line and column into that source, for a finding on a raw node
    Node     string           // IR-space location: a stable ID or a path through the document's own fields
    Inferred Inferred         // "" = declared; else the heuristics that produced this node, comma-separated (§12)
}

type Position struct {
    Line   int // 1-based; 0 = no position
    Column int // 1-based; 0 = the producer knows the line only
}

const NoSource = -1 // the node addresses no input file

type Diagnostic struct {
    Severity   Severity  // error | warning | info
    Code       string    // stable: "openapi/unresolved-ref", "ir/dangling-type-ref"
    Message    string
    Provenance Provenance
}
```

Everything heuristic is auditable; everything broken is reportable with an exact source location.
A reference has no `Provenance`, so a heuristic that sets a reference's `Nullable` names itself in
the reference's own `TypeRef.Inferred` (§3.3).

The one exception is what `NoSource` exists for. A pass reporting on the document it was handed has
no input file to name, and every real index — `0` included — names a file the document loaded, so
reusing one would make a renderer fabricate a location. A shared primitive has no one file to name
either, since every source reaches it by kind (§3.1). `NoSource` is therefore the **only**
out-of-table `Source` value the IR declares: a verifier accepts it and reports every other index
that addresses no declared source, so a producer inventing a second sentinel is caught rather than
tolerated.

**A refusal still has a table.** A compile that refuses returns no `Document`, yet its diagnostics
index the table that document's `Sources` would have begun with, the inputs the compile was handed,
which a document a reference names follows. The compiler names it without reading anything
(`compilers.Compiler.SourceTable`), and the engine returns it beside the findings as
`engine.Result.Sources`. A finding about a whole source carries its index and no locator, even one
made before any compiler claimed the spec, which names it as source `0`: it is about a file the
table names, and only a finding about no file at all takes `NoSource`.

**One field per kind of locator.** A consumer cannot tell a locator's kind from its spelling, so
each kind has its own field. `Pointer` is the structural locator into a source and holds nothing
but an RFC 6901 pointer, which is what lets a consumer walk it with `jsontext.Pointer`'s methods.
`Position` is for a finding made on a raw node the parser hands back, and is the form editors turn
into a link. Such a finding carries the node's `Pointer` beside it whenever the parsed tree holds
the node, so it is placed where a lowering finding about the same construct is. The position stands
alone for a part of the source no pointer reaches, such as a later document of a YAML stream or a
pair a `<<` merge key brings in, which the merged document holds at another pointer; and for the
document's top-level mapping, whose pointer is the empty one, which beside a position reads as none.
`Node` is where an IR pass reports a finding about the document itself; its spelling is the
producer's, and nothing parses it. A verifier holds a `Pointer` to RFC 6901 and a `Position` to
1-based values, and refuses either one on `NoSource`, where there is no file for it to be inside.
Nothing requires exactly one locator: a finding may carry a `Node` beside a source location, and a
node the lowering reached may carry both a pointer and a position.

---

## 14. Compiler lowering summaries

A format whose compiler exists states its lowering rules in a specification beside it, under
`docs/compilers/`, one rule per entry; this document keeps the shapes they lower into. A format
whose compiler is still to come keeps its planned lowering here, as a list of highlights, and the
list moves to a specification of its own when the compiler lands.

| Format | Lowering rules |
|---|---|
| **OpenAPI 3.x** | [`compilers/openapi.md`](./compilers/openapi.md) |
| **Swagger 2.0** | lifted inside the OpenAPI compiler: [`swagger-lift`](./compilers/openapi.md#swagger-lift) |

### TypeSpec

- consumed post-check (monomorphized, `isFinished`)
- template instances → TypeCommon.Instantiation incl. value args → TemplateArg
- models → Model w/ Base + spread provenance → Mixins
- `Record<T>` → MapT, and a `Record` spread or extended beside properties → AdditionalProps
- scalars → Scalar chains, constructors in values → Value.Ctor
- `@encode`/`@format` → Encoding triple
- `@encodedName` → WireNameByFormat at property AND type level
- unions w/ named variants → Union, `@discriminated` → WireTagged +
  Discriminator.PropertyName/Envelope/EnvelopeValueName
- `| null` → Nullable
- visibility classes (incl. custom, `@invisible` → Visibility.None) → Visibility, op overrides →
  ParameterVisibility/ReturnTypeVisibility
- `@patch` implicitOptionality → HTTPBinding.PatchImplicitOptionality
- interfaces → OperationGroups (versionable)
- `@overload` → OverloadOf
- `@sharedRoute` → SharedRoute
- `@service` → Service
- versioning decorators incl. `@typeChangedFrom`/`@madeOptional`/`@madeRequired` and add/remove
  cycles → Availability timeline (on members/variants/params too)
- pagination decorators incl. prev/first/last links and header continuation tokens → Pagination
  PropPaths (In:"header")
- Azure.Core `@pollingOperation`/`@finalOperation` → LongRunning
- multipart w/ parts → Content.Encoding/PartEncoding, `Http.File` → FileInfo (content-type set,
  contents chain, filename location)
- streams/SSE → StreamDetail + Variant.Event (contentType, terminal)
- `@error` → the response becomes an `ErrorCase`, whose payload `irfacts.Index.Usage` marks
  `UsageError`
- `@example`/`@opExample` → Examples (Input/Output pairs)
- `@pattern` message → Constraints.PatternMessage
- `@mediaTypeHint` → TypeCommon.MediaTypeHint
- `never` members deleted + diagnostic per §4.8
- tuples → Tuple with `Constraints.MinItems = len(Elems)` and no Rest, since a TypeSpec tuple
  requires every position and admits nothing past them
- TCGC client-shaping decorators (`@clientName`, `@access`, `@usage`, `@scope`, `@override`, …) →
  namespaced Unmodeled (`out_of_scope`) consumed by emitter policy, never IR semantics
- values/consts incl. enum-member refs → Values channel

### Smithy 2.0

- structures → Model, mixins → Mixins (non-structure mixins flattened — spec-sanctioned)
- `document` → Any
- map shapes → MapT (key shape → Key, nil for `smithy.api#String`, `@length` → Constraints)
- unions → WireTagged Union, member `@jsonName` → Variant.WireName
- enum/intEnum → Enum (open by default)
- `@sparse` → element Nullable
- traits: constraints → Constraints, `@paginated` → Pagination (declared), `@retryable` →
  ErrorCase.Retryable + Throttling, `@error` fault → ErrorCase.Fault, `@readonly` → Idempotency
  safe, `@idempotent`/`@idempotencyToken` → Idempotency, `@sensitive` → Sensitive/Secret, `@tags` →
  Tags, `@clientOptional`/`@input` → Property.ClientOptional (+InputOnly), `@addedDefault` →
  DefaultAdded, root-shape `@default` pushed down to properties w/ provenance
- `@streaming` blob → StreamDetail (+`@requiresLength` → RequiresLength)
- event streams → StreamDetail.Events union + Property.EventHeader/EventPayload + Initial messages
- service-level errors → Service.CommonErrors
- protocol traits → Service.Protocols
- service `rename`/`version` → Service.Renames/Version
- resources → OperationGroup + ResourceInfo (identifiers, properties, lifecycle incl.
  put/@noReplace, instance vs collection ops)
- http traits → HTTPBinding incl. `@endpoint`/`@hostLabel` → HostPrefix/host location (additive
  binding), `@httpPrefixHeaders`/`@httpQueryParams` → Prefix bindings, `@httpResponseCode` →
  Response.StatusCodeProp, `@requestCompression` → Compression, `@httpChecksumRequired` →
  ChecksumRequired
- `@auth` order → priority-ordered Auth, `@optionalAuth` → empty option
- `@jsonName` → WireName
- `@mediaType` → Encoding.MediaType
- xml traits → XMLHints at type and property level
- `@examples` → Examples (Input/Output/Error)
- waiters + rules-engine traits → verbatim Unmodeled (`out_of_scope`, §15)
- `smithy.api#Unit` → nil payload / shared empty Model for tag-only variants
- other traits → namespaced Unmodeled

### GraphQL

- §8.4
- interfaces (incl. interface hierarchies) → Abstract models + Implements
- field arguments → Property.Args
- input objects → `InputOnly` models, `@oneOf` inputs → WireTagged Exclusive Union
- non-null wrapping → Required/Nullable (all `[T!]!` combinations via per-layer list nodes)
- defaults on args AND input fields → Default (list-input coercion normalized)
- custom scalars → Scalar{Base: nil}, `@specifiedBy` → Unmodeled
- deprecation w/ reason at every location incl. args/input fields
- directives → ordered-array Unmodeled convention + document-level definitions

### AsyncAPI

- servers w/ protocols/protocolVersion/security → Servers (named, w/ Auth)
- channels → Channels (Address nil = unknown), messages → Document.Messages registry referenced by
  ID; a channel's `servers` → Channel.Servers, and one naming none lists every server, which
  AsyncAPI reads it as available on (§10)
- operations send/receive → Operation + MessageBinding, no-reply sends → OneWay
- reply objects → Reply (static channel + dynamic PropPath address + message set)
- correlation IDs → PropPath (In: header|payload)
- message headers → hoisted header model (Headers *TypeRef)
- message examples → Example{Headers, Value} pairs
- multi-format payload schemas → Content.SchemaFormat + verbatim schema in Unmodeled (Avro payloads
  lower through the type graph: record→Model, map→MapT, enum→closed Enum w/ FallbackMember,
  fixed→bytes w/ length, decimal→Constraints.Precision/Scale, aliases→Naming.Aliases)
- parameter `location` → Parameter.ValueFrom
- traits applied by merge w/ provenance
- protocol bindings at server/channel/operation/message level → raw Bindings maps
- payload JSON-Schema lowering shared with OpenAPI

### Protobuf

- messages → Model w/ WireIDs
- oneof → WireTagged Exclusive Union w/ variant WireIDs + Flatten on the synthetic wrapper property
  (oneof members are top-level wire fields; synthetic oneofs from proto3 `optional` do NOT lower to
  unions — they are presence markers)
- presence disciplines → Property.Presence (never Nullable)
- enums → Enum open/closed per syntax/edition, allow_alias → duplicate member values
- map/repeated → MapT/List (a `string` map key → nil Key; packed/expanded → List.Encoding)
- groups / editions DELIMITED → nested Model + Encoding "delimited"
- scalar wire variants (sint/fixed) → Encoding
- `extend` fields → Property.ExtensionOf, extension ranges → Model.ExtensionRanges
- well-known types → External (wrapper types → External or nullable primitive per injectable policy)
- custom options → namespaced Unmodeled (proto-JSON), file options keyed by path in
  Document.Unmodeled
- services/rpcs → Service/Operation + RPCBinding
- streaming modifiers → StreamingMode
- gRPC transcoding (google.api.http) → additional HTTPBinding entries (slice), path patterns →
  PathPattern, nested bindings → ParamPath, response_body → ResponseBodyPath
- reserved ranges → Unmodeled (guarded by validate pass)

### Erlang/OTP

- §8.5
- `-type`/`-spec` type language → type graph: tuples → Tuple with
  `Constraints.MinItems = len(Elems)` and no Rest, `tuple()` of any arity → Tuple with no Elems and
  a Rest of the top type, tagged-tuple unions → WireTagged Union + Discriminator.Index, records →
  Model{Positional} w/ 1-based WireIDs, atoms → symbol Values / Literal types /
  `Scalar{Base: string, Encoding: "erlang:atom"}`, integer ranges → integer Scalar + Min/Max, maps
  `:=`/`=>` → Properties(Required)/AdditionalProps w/ typed keys, a map of `=>` associations alone →
  MapT, `pid()`/`port()`/`reference()`/`fun()` → External, `string()` → List of char scalar,
  parametrized types monomorphized w/ Instantiation
- behaviours: gen_server call/cast → Operations + OTPBinding (cast → OneWay), info → channel
  Messages received, gen_event → Channel w/ N messages, gen_statem states →
  `Unmodeled["otp:states"]`
- registered process → Channel.Address, registration kind → Channel.Bindings["otp"]
- reply-union Errors split → injectable Inferred policy
- bit-sized binaries/map assoc lists/`-opaque` → §4.8 degraded lowerings
- `-deprecated` → Deprecation
- `-doc`/EDoc → Docs

## 15. Deliberate exclusions

Constructs on this list that a source format actually declares are still kept — losslessness is
not negotiable — but they are kept under `ReasonOutOfScope` (§12), never `no_ir_home`: the
boundary is drawn on purpose, so no promotion pass should read them as a gap to close. Entries
here that no format declares (language casings, generator plan artifacts) are simply absent from
the IR and preserve nothing.

- **Language names/casings** — emitters own identifier rendering entirely (IR stores neutral
  word sequences + wire names).
- **SDK runtime policy** (retry/timeout/telemetry/error-class taxonomy) — a separate emitter
  input, never IR (§2.4 of architecture.md). OTP call timeouts and `multi_call`/`send_request`
  machinery fall here too.
- **Generator plan artifacts** (request builders, executor pairs, per-visibility model variants,
  primary-response selection) — computed views in emitters, not stored.
- **Structured modeling of transport-deployment minutiae** (Kafka partition configs, AMQP
  exchange args) — preserved as raw namespaced entries.
- **Smithy rules-engine traits** (`@endpointRuleSet`, context params) — runtime endpoint
  resolution machinery; preserved as raw namespaced entries on the service.
- **Smithy waiters** (`smithy.waiters#waitable`) — preserved verbatim in `Operation.Unmodeled`,
  never folded into `LongRunning`; promoted to a typed node only if a second format lands.
- **TCGC-style client-shaping decorators** (`@clientName`, `@access`, `@usage`, `@scope`, …) —
  per-language SDK-surface policy, preserved as namespaced entries for emitter policy layers.
- **Arbitrary GraphQL persisted queries** — the type graph and entry points are retained; query
  composition is a emitters/runtime feature.
- **Capability/interface-typed fields** (Cap'n Proto capability passing) — requires
  services-as-types; out of scope for a data-SDK compiler.
- **Function types** — Erlang funs degrade per §4.8; no target language marshals closures.
- **A general intersection combinator** (an `Intersection` node, or an `Intersect` sum arm, for the
  *struct ∧ union* shape a JSON Schema `oneOf` beside structural keywords writes) — asked and
  answered, so it does not need re-litigating. The capability is not cross-format: only OpenAPI and
  AsyncAPI can express it, and they share a compiler code path, so a node would be one format's
  artifact given IR-wide status against invariant 9. TypeSpec's `&` is *model-only*
  (`ir-spec-matrix.md`'s Intersection row records it as `⚠ & (model is)`) and so is already covered
  by §4.3's `Base`/`Mixins`; Smithy, GraphQL, Protobuf and Erlang/OTP cannot express it at all. The
  shape is instead handled where it arises: distributed across the union's variants per §4.3 when
  every branch names a referent, and kept verbatim under `ReasonDegradedLowering` in the five
  residual shapes [`union-beside-structure`](./compilers/openapi.md#union-beside-structure) lists. A
  second format that declares a genuine intersection — not a model-only `&` — is what would reopen
  this.

## 16. Open questions (tracked for implementation)

1. **BigVal representation** — settled: a decimal string in the IR (`BigVal`), read by
   `ir.Decimal` without materializing it (§6).
2. **Dedup aliasing mechanics** — alias table on Document vs ID rewriting with a redirect map;
   leaning alias table (IDs immutable, always).
3. **Content negotiation in the plan layer** — settled: the IR keeps all media types, and one
   shared plan helper elects among them for a success, a failure and a stream alike, by a ranking
   the plan's policy holds (emitter-design §3.1).
4. **Whether `UsageFlags` is stored or always recomputed** — settled: derived by `ir/irfacts`,
   not stored (§4.9). On the golden spec (`cloudflare/api-schemas` at `843eae2f`) deriving usage
   for every type, the subtype index and the type graph included, takes about 50 ms of a compile
   of about 8 s (morphic `4545c23e`), so storing it bought nothing, and a stored copy goes stale
   after any transform that changes what an operation reaches.
5. **Dual identity for cross-revision correlation** — pointer-derived `TypeID`s are stable across
   runs but churn when a schema moves within its source file. TCGC solves cross-version
   correlation with name-based `crossLanguageDefinitionId`s. Likely resolution: keep pointer IDs
   as the structural identity and add a derived, name-based correlation key computed by a pass —
   decide once IR diffing between spec revisions is actually built. Evidence from oagen's
   differ/compat stack: (a) diffing IR-document-to-IR-document is the right plane (oagen already
   does this); (b) name-based correlation alone still misses renames — plan a third *structural*
   tier (content-hash / field-set similarity, shared with the `dedup` pass) as the fallback when
   both pointer and name churn. Without stable identity, rename detection costs hundreds of
   lines of best-effort heuristics downstream (see prior-art.md §1).
6. **Response discrimination placement** — `ResponseConditions` (status ranges) is HTTP-shaped
   living on the neutral core. Fresh evidence that it sits awkwardly: RPC single-response and OTP
   call replies both use empty conditions, and `Response.StatusCodeProp` is likewise
   HTTP-flavored. Alternatives: move conditions into `HTTPBinding` alongside parameter locations,
   leaving responses as an ordered named list. Revisit when the RPC or messaging compiler lands
   and either validates or strains the current shape.
7. **Semantic nullability (GraphQL)** — the `@semanticNonNull` RFC ("null only on error") is a
   third nullability state `TypeRef.Nullable` cannot express. Pre-spec today: preserved as
   `Unmodeled["graphql:@semanticNonNull"]`. If it merges into the spec, add a `NullKind` (or a
   `NullOnlyOnError` bool) rather than churning every compiler early.
