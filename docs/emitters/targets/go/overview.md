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

The Go [merge adapter](../../write-integrate.md) (`emitters/golang/mergeadapter.go`) uses `go/ast`
to parse existing files and append only new top-level symbols, never delete.

Go's extractor ([surface verification](../../surface-verification.md)) reads the existing package's
exported API, as `apidiff` does.
