## 8. The first targets — a Go walkthrough, and what the Kotlin targets change

The first milestone ships three targets from one plan: Go (`"go"`), Kotlin/JVM (`"kotlin-jvm"`) and
Kotlin Multiplatform (`"kotlin-multiplatform"`: JVM, Native/iOS and JS/Wasm from common code).
§8.1–§8.7 trace IR constructs through `plan → refine → emit` for Go, one per subsection, with IR
field names per `ir-design.md`. The Kotlin targets read the same plan and lower the same facts;
§8.8 gives what they decide differently, §8.9 and §8.10 trace return shapes and the client's base
URL through all three, and §9 every construct per target.

**Generated SDKs require Go 1.27 or newer.** Their `go.mod` declares `go 1.27`, and the lowerings
below use what that release provides:

- **JSON is `encoding/json/v2` and `encoding/json/jsontext`.** Go 1.27 builds them by default; Go
  1.26 builds them only under `GOEXPERIMENT=jsonv2`, a setting of the importing program's build
  environment that a library cannot make for it, and whose use outside toolchain development the
  `go` command documents as unsupported. Codecs are `MarshalJSONTo`/`UnmarshalJSONFrom` (§4.8),
  presence is `omitzero` (§4.7), and a union's fallback is `jsontext.Value` (§4.4).
- **`ir.PrimUUID` maps to the standard library's `uuid.UUID`**, new in Go 1.27, so an SDK takes no
  third-party dependency for it. It encodes as the canonical string, through its `MarshalText`.
- **The auto-pager is an `iter.Seq2[T, error]`** (§8.3), ranged over with `for item, err := range`.
- **Runtime templates are tested with `testing/synctest` and `httptest.NewTestServer`** (§13 test
  T-13), and the transport template leaves draining unread bodies to `net/http` (§5.2).

The cost is the SDK users still on Go 1.26. Go supports each major release until two newer ones
exist, so 1.26 leaves support when 1.28 ships; from then on every supported release meets the floor.
