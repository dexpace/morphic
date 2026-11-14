# Consumers that are not emitters

Part of the [IR design](./README.md). What a consumer other than an emitter can rely on: what the
document carries for a reader, how it identifies an entity across revisions, what to pin, which
`Unmodeled` keys hold still, and what a document costs to hold.

The emitter documents ([`docs/emitters/`](../emitters/README.md)) design every reader of the IR as a
generator, and each capability question there is whether a target can express a fact. A catalog, a
linter or a diff reads the same document and asks other questions: it renders the documentation as
content, compares two documents, and stores them. Nothing here is a separate surface. It is the one
document, read for what these consumers need.

## What a reader finds

| Need | Where | Status |
|---|---|---|
| documentation shown to a person | `Docs` on every node that has any: the summary, the description as CommonMark exactly as written, the external links, and the spans of the description that name a type ([annotations](./annotations.md)) | carried |
| examples per operation, parameter, property, type and media type | `Operation.Examples`, `Parameter.Examples`, `Property.Examples` (a header's among them), `TypeCommon.Examples`, `Content.Examples` | carried |
| a deprecation and when it ends | `Deprecation`: its message, the version it began in, the removal as a version and as a date, each verbatim, and what replaces it, by ID ([annotations](./annotations.md#replacements)) | carried |
| tags as the structure of a reference | `Document.TagDefs` with each tag's documentation, parent and kind; `Operation.Tags`, every membership; `Service.Groups`, the section tree the tags describe ([services](./services.md)) | carried |
| documentation a path shares among its operations | `HTTPBinding.PathDocs` on each operation the path mounts ([http-binding](./http-binding.md#path-documentation)) | carried |
| how a response's values feed another operation | `Links` on the response, error case or default ([links](./responses.md#links)) | carried |
| two revisions at once | the [correlation key](./derived-facts.md#correlation-keys) of each entity, derived on demand (`irfacts.Index.Correlation` and `Correlated`) | carried |

## Identity across revisions

An ID identifies an entity within one document and is reproducible from the same input, but it is
never, on its own, evidence that two revisions hold the same entity: a renamed position gives new
IDs, and a reordered array gives an old ID to another entity ([across
revisions](./identity.md#across-revisions)). A consumer comparing revisions matches entities by
their correlation key, and one keying stored state by ID checks the key before trusting a match.

## What to pin

No release is tagged yet (#432), so the version a consumer can name is a commit, which Go resolves
as a pseudo-version (`go get github.com/dexpace/morphic@<commit>`). What a stored document can be
read by is the generation it is stamped with, not the commit that wrote it: `IRVersion` moves with
every change to the document's shape, to the meaning of a member, to how a name is derived and to
the `Unmodeled` keys a compiler spells ([IR version](./ir-version.md)), and a build reads only its
own generation. So a consumer storing documents records the stamp each was written at, keeps the
source each was compiled from, and recompiles that source when it moves to a build of another
generation, which refuses the old document rather than reading it. Morphic ships no migration
between generations. Two builds of one generation read each other's documents, but a fix between
them can lower one source differently, so a consumer comparing two documents compiles both with
one build, which derives their [correlation keys](./derived-facts.md#correlation-keys) too.

## Unmodeled keys

A consumer reading an `Unmodeled` entry reads a key whose spelling either the source chose or the
compiler did, and the entry's `Reason` says which promise it carries ([what a key
promises](./unmodeled.md#what-a-key-promises)).

## Size

A document is several times its source. On the golden spec the IR is 7.0 times the source written
without whitespace, 95,803,984 bytes against 13,720,625, and 4.0 times it once both are compressed,
5,244,918 bytes against 1,320,820 by gzip at level 6 (spec @319dc756, morphic @b206dc99). Much of
it repeats source positions: in the type registry, the entities' own `id` values are 23% of the
bytes and their `provenance` values 20%, an ID's path being the pointer its provenance records.

The compiler has no mode that trims provenance, and slicing a document is a pass's work rather
than the compiler's, for three reasons:

- **Provenance is what a reader cites and what the verifier compares.** Every diagnostic and every
  `Unmodeled` entry locates its construct through it, and `irverify` holds each ID to the pointer
  its node records ([what irverify holds](./identity.md#what-irverify-holds)). A document stripped
  of it still verifies, as `testdata/golden/openapi/petstore.yaml` does with no finding either way,
  because a node recording no pointer has nothing to disagree with: a trimmed document passes the
  same gate while proving less, and a consumer cannot tell the two apart.
- **Compression already takes most of it.** Under gzip the ratio falls from 7.0 to 4.0, and a
  compressed document loses nothing a reader can do.
- **A slice is the filter pass.** A consumer wanting one operation and what it reaches needs the
  closure of its types, which is easy to get wrong by hand: a discriminated base reaches its
  subtypes, an alias its target, an error the service's common errors. The planned filter pass
  ([architecture](../architecture.md#22-ir-passes-ir--ir)) keeps what a pattern selects and prunes
  every type nothing it keeps reaches. That is more than the reach [usage](./derived-facts.md#usage)
  derives, which takes a polymorphic base to its subtypes but skips each reference [the type
  graph](./derived-facts.md#the-type-graph) draws no edge for, such as an encoding's wire type:
  pruning by usage alone leaves those dangling. The pass's output is a document like any other, so
  it verifies and encodes as one.
