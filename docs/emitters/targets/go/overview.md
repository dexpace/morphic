# The Go target

The Go target (`"go"`) lives in `emitters/golang`. These documents state what Go decides, and link
the shared rules those decisions answer.

## The language floor

**Generated SDKs require Go 1.27 or newer.** Their `go.mod` declares `go 1.27`, and this target's
lowerings use what that release provides:

- **JSON is `encoding/json/v2` and `encoding/json/jsontext`.** Go 1.27 builds them by default; Go
  1.26 builds them only under `GOEXPERIMENT=jsonv2`, a setting of the importing program's build
  environment that a library cannot make for it, and whose use outside toolchain development the
  `go` command documents as unsupported. Codecs are `MarshalJSONTo`/`UnmarshalJSONFrom`
  ([discriminators](unions.md#discriminators)), presence is `omitzero`
  ([optionality](models.md#optionality)), and a union's fallback is `jsontext.Value` ([the
  fallback](unions.md#the-fallback)).
- **`ir.PrimUUID` maps to the standard library's `uuid.UUID`**, new in Go 1.27, so an SDK takes no
  third-party dependency for it. It encodes as the canonical string, through its `MarshalText`.
- **The auto-pager is an `iter.Seq2[T, error]`** ([pagination](operations.md#pagination)), ranged
  over with `for item, err := range`.
- **Runtime templates are tested with `testing/synctest` and `httptest.NewTestServer`**
  ([T-13](../../testing/mandatory-tests.md)), and the transport template leaves draining unread
  bodies to `net/http` ([runtime templates](emit.md#runtime-templates)).

The cost is the SDK users still on Go 1.26. Go supports each major release until two newer ones
exist, so 1.26 leaves support when 1.28 ships; from then on every supported release meets the floor.

## Output units

A Go [unit](../../refine/output-units.md) is a package, and a group's is `<root>/<group>`. Go's
default [limit](../../refine/output-units.md#the-size-check) is 10,000 declarations. A synthetic
package of that many models, each with its codec and a `Validate`, peaked at about 1.6 GB of memory
to build with Go 1.27.2 on darwin/arm64, and one of 30,000 at about 4.6 GB.

Go has no cap on a method or a class, as the JVM has, but one package of tens of thousands of types
builds slowly and in gigabytes of memory, which the [size
check](../../refine/output-units.md#the-size-check) reports and its split by group bounds.

## Merging and surface extraction

The Go [merge adapter](../../write-integrate.md) (`emitters/golang/mergeadapter.go`) parses an
existing file with `github.com/dave/dst`, a decorated syntax tree, and appends only new top-level
symbols, never deleting one. `dst` keeps each comment on the node it describes, so a declaration
moved from the new generation's file into the old one carries its doc comment and a
`@morphic:keep` marker stays where its region is; `go/ast` keeps comments in a list of their own by
byte offset, so a node moved between two files leaves them behind.

A spike settled it, at Go 1.27.3 with `dst` v0.28.0. A generated file holding a keep region gained
a type and a method from the next generation, three ways:

- **`go/ast` nodes moved, printed by `go/printer`:** the moved declarations lost their doc
  comments, which landed above `GetZone`, and one of them inside `GetZone`'s body.
- **`go/ast` to find the declarations, their source text spliced in, `astutil.AddImport`:** correct,
  the region untouched and the import added. It reaches top-level declarations alone, so adding a
  field to a struct, which a merge at property level would need ([open
  questions](../../open-questions.md), 5), would be a splice inside one with no tree to check it.
- **`dst`:** correct, imports managed by its restorer, and a file using generics, a range-over-func
  iterator and `min` round-trips byte for byte.

`dst` is MIT-licensed, at v0 and maintained in bursts, its last release in 2026. So its version is
pinned exactly, `emitters/golang` alone may import it ([package layout](../../packages.md)), and an
upgrade reruns the spike's cases as tests of the adapter.

Go's extractor ([surface verification](../../surface-verification.md)) reads the existing package's
exported API with `go/types`, and each record it builds carries the IR ID its declaration's
`Origin` names, which no Go tool records.

`golang.org/x/exp/apidiff` compares two Go packages and labels each change compatible or not, which
is the Go half of the structural channel's question. It is the test oracle for Go's severity, not
the differ and not the severity function: over the surface corpus, a differential test holds each
structural verdict Go's `verify.Severity` gives to apidiff's on the same two generated packages, and
a disagreement is either a severity bug or a divergence the severity's documentation states. It
cannot be the differ, since its records carry no IR ID, nor the severity, since it answers only
whether a caller's Go code builds: given an enum gaining a constant it says compatible, which it is
for a caller's code, while the behavioral channel still reports what a deployed SDK reading a closed
response enum meets ([surface
verification](../../surface-verification.md#what-a-change-record-carries)). As a test-only
dependency it needs no compatibility promise from `x/exp`, which gives none; it was under active
development, BSD-3-Clause, at the October 2026 revision measured.
