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

A Go [unit](../../refine/output-units.md) is a package, a group's is `<root>/<group>`, and the
[runtime's](../../refine/output-units.md#the-runtimes-unit) is `<root>/runtime`, with what generated
code alone calls beneath `<root>/internal` ([below](#what-a-caller-reaches-of-the-runtime)). Go's
default [limit](../../refine/output-units.md#the-size-check) is 10,000 declarations. A synthetic
package of that many models, each with its codec and a `Validate`, peaked at about 1.6 GB of memory
to build with Go 1.27.3 on darwin/arm64, and one of 30,000 at about 4.6 GB.

Go has no cap on a method or a class, as the JVM has, but one package of tens of thousands of types
builds slowly and in gigabytes of memory, which the [size
check](../../refine/output-units.md#the-size-check) reports and its split by group bounds.

## What a caller reaches of the runtime

A Go package reaches no other's unexported names, so what generated code calls in another package is
exported, and what keeps an exported name from a caller is where its package sits: Go refuses an
import of a package under `internal/` from outside the tree rooted at that directory's parent. The
runtime is three packages:

```
<root>/runtime           what a caller names: APIError, Stream, Response, the options, …
<root>/internal/runtime  what generated code alone calls: the send loop, the decoders, the
                         checks, the URL expansion, an operation's settings type
<root>/internal/state    the structs the opaque types of the first are declared over
```

`internal/runtime` imports `runtime`, whose failure and stream values it builds, and both import
`internal/state`, which imports neither. Every unit sits under `<root>`, so each may import all
three, and a caller's program only the first: measured with Go 1.27.3, a program in another module
importing `<root>/internal/runtime` failed to build (`use of internal package … not allowed`).

A type a caller names takes one of two shapes, so that `internal/runtime`, a package other than its
own, can build it:

- **A struct of exported fields alone**, built by a literal: `APIError`, `OtherResponse`
  ([failures](errors.md), [return shapes](returns.md)).
- **A type declared over a struct of `internal/state`**, `type Stream[T, H any] state.Stream[T, H]`.
  Its methods are declared in `<root>/runtime`, each converting to the struct and calling a function
  of `internal/state`; `internal/runtime` builds one by converting the struct to it, which only a
  package able to name the struct can write. Where a method needs what `internal/runtime` declares,
  as a stream's read does, failing with an `*APIError` its state's package cannot name, the struct
  holds a function its builder gave it. A group's sub-client is declared so in its own unit, `type
  Client state.Sub[Client]`, and the root's client builds it by that conversion, so no unit exports
  a function that builds one.

A type declared over a struct takes the struct's fields, the methods of what it embeds and its
conversions, so the struct decides what a caller reaches through the type. Three rules keep that to
nothing, each measured with Go 1.27.3 by breaking it:

- **A field of a state struct is unexported, named, and a function.** With an exported field, a
  caller's program read it through the sub-client declared over the struct (`c.Users.Core`
  compiled), and passed a struct literal of its own where an exported function took the type. With
  an embedded one, the embedded type's methods were the sub-client's, and `c.Users.Send(…)` called a
  helper of `internal/state`. A field holding its value itself is printed, since `fmt` reads an
  unexported field, and so is one holding a pointer to it, which `fmt` follows under a verb it has
  no form for: `%O` of a sub-client showed the base URL its client held, password included. A
  function prints as an address under every verb, and no encoder reads what one closes over ([what a
  response prints](returns.md#what-a-response-prints)).
- **A struct several types are declared over takes the type as a parameter**, `type Sub[T any]
  struct { …; _ [0]*T }`. Go converts between two types of one underlying type, so over a plain
  `state.Sub` a caller's `(*billing.Client)(c.Users)` compiled and called the other group's
  operations; over `state.Sub[users.Client]` and `state.Sub[billing.Client]` it does not (`cannot
  convert c.Users`).
- **An exported signature names no type of a package under `internal/`**, and spells a type of the
  first shape or the second by its name in `<root>/runtime`, as its caller writes it. A caller
  cannot name an internal type and can still hold, pass and call a value of one.

An operation's row of the runtime's settings is an unexported variable of each unit holding a method
that reads it ([output units](../../refine/output-units.md)). Exported from the shared unit, where
two groups' methods would read one row, a caller's program wrote it
(`shared.GetZoneSettings.MaxAttempts = 100`, and the next call made 100 attempts), `go doc` listed
it, and `apidiff` would report its removal when the second mount goes.

So the SDK's API is what `go doc` prints of its packages outside `internal/`: each caller-facing
type with its fields and methods, and no helper. `apidiff` compares the same. Measured with
`golang.org/x/exp/cmd/apidiff` (x/exp at `a52e3aef5920`) over the module: a helper gaining a
parameter, an operation's settings type gaining a field and two state structs gaining one each
reported nothing, while `APIError.StatusCode` changing type, `Stream.Recv` gaining a result and a
sub-client's method gaining a parameter were each reported incompatible.

The SDK is one module, so the three packages are at one version with every unit, and nothing has to
hold a unit to its runtime.

Four other layouts were built, and each fails a caller's program or the tools reading the API:

- **Everything exported from `<root>/runtime`.** A caller calls the send loop and builds an
  operation's settings, and `apidiff` reports each helper's change as a break of the SDK.
- **The helpers under `internal/`, called by the methods of the types above them.** `Stream.Recv`
  calling a decoder that builds an `*APIError` failed to build (`import cycle not allowed`), which
  is why the state sits in a package below `<root>/runtime` and the helpers in one above it.
- **The whole runtime under `internal/`, its caller-facing names declared again as aliases.** It
  builds, and a caller's `errors.As` and `runtime.Body` work. But `go doc` prints `type APIError =
  irt.APIError` with no field and no method, and `apidiff` reported nothing when `StatusCode`
  changed type and `Recv` its results, since it compares an alias's target by name and skips
  internal packages: the one change that breaks every caller would ship as compatible.
- **An exported function sealed by a parameter of an internal type.** A caller cannot name the type,
  and can still call the function: `users.New(nil)` failed, while a generic function inferring the
  type from `users.New`'s own signature returned its zero value, which `users.New` then took.

A caller can still write the zero value of any exported type, `users.Client{}` among them, as Go
allows of every struct. A sub-client or a stream built so holds no client, and each of its methods
returns an error saying so rather than dereference one. T-64 holds the three rules by reading the
packages' types: the method set of each exported type holds no method an `internal/` package
declares, no two exported struct types convert to each other, and no exported signature names an
internal type.

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
exported API with `go/types`, as `apidiff` does, of each package outside `internal/`
([above](#what-a-caller-reaches-of-the-runtime)). Source carries no IR ID, so it joins each
declaration to its entity through the prior manifest, whose entries name the symbol each entity
shipped under ([write / integrate](../../write-integrate.md#what-the-manifest-records)). An SDK
with no manifest, one Morphic did not generate, joins nothing: the differ compares one generation
with the next. A deprecation is a comment in Go, which `go/types` does not hold, so the extractor
reads each declaration's `Deprecated:` paragraph from the syntax it type-checks.

`golang.org/x/exp/apidiff` compares two Go packages and labels each change compatible or not, which
is the structural channel's question as Go asks it. It is the test oracle for Go's severity, and
neither the differ nor the severity function:

- **Not the differ.** A `Change` of its report is a message and a flag, with no symbol or ID to
  join an entity on.
- **Not the severity.** It answers only whether a caller's Go code builds. Given an enum gaining a
  constant it says compatible, which it is for a caller's code, while the behavioral channel
  reports what an SDK deployed with that enum closed meets ([surface
  verification](../../surface-verification.md#what-a-change-record-carries)).
- **The oracle.** Go's severity is Go's own compatibility rules, the ones apidiff applies, of which
  the shared rows are the part every typed target has. So for each pair of generated packages a
  fixture makes by one change, a test holds Go's structural verdict to apidiff's: breaking exactly
  where apidiff reports an incompatible change. The comparison is per pair, since a message names a
  declaration and no entity. A pair on which they differ is a bug in the severity unless this
  section lists it with its reason, and it lists one (T-75).

The one divergence is a method added to a client's interface, which each operation added brings.
`apidiff` reports it incompatible, since a type declaring the interface's methods itself stops
implementing it, and Go's severity calls it additive: the interface is declared for a caller's
tests, its documentation says it gains a method with each operation, and a fake that embeds it
keeps building. Sealing the interface with an unexported method, which makes embedding the only
way to implement it and `apidiff` report the addition compatible, was measured and not taken: a
double a tool generates declares every method itself, and is refused (`missing method sealed`).

A struct a caller holds is never comparable ([models](models.md#nesting)), so a member added takes
no `==` from a caller, and there the two agree with no divergence to list.

Measured at x/exp `a52e3aef5920`, a revision of 9 November 2026, over two type-checked packages: a
field's type changed, a method gaining a parameter, a constant's value changed and a declaration's
underlying type changed were each incompatible, and a constant added and a struct field added each
compatible. The role is settled before the milestone that builds the differ because it shapes the
extractor: one built on apidiff's report would have nothing to key a record by. As a test-only
dependency it needs no compatibility promise, which `x/exp` gives none of; its licence, BSD-3-Clause
at that revision, and its maintenance are confirmed again when the test is written (M4).
