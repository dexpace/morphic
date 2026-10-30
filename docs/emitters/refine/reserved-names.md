**Reserved names belong to a scope.** `Reserved()` holds the language's keywords and predeclared
names, reserved everywhere. Generated code binds names of its own too, and a spec name rendered
equal to one of them shadows it or fails to compile, so each scope reserves what the code generated
into it binds as well, and the units' scope what a unit cannot be named:

- **The units** (§4.17), whose names are a scope of their own: `shared`, the shared unit's, and the
  names a target's toolchain treats apart — Go's `internal`, which no caller outside the SDK may
  import, `testdata`, which `./...` skips, and `vendor`, which the `go` command reads at a module's
  root as vendored dependencies.
- **A unit**: the names the target declares at its top level beside the spec's — Go's `Client`, its
  option type and their constructors, a group unit's sub-client, and any runtime name it declares
  there (`APIError`, `Body`, `OtherResponse`, `Response`, `Stream`).
- **A type**: the members the target gives every model — Go's `MarshalJSONTo`, `UnmarshalJSONFrom`,
  `Validate` and a redacting `String`, and the field holding the catch-all (§8.1); Kotlin's
  `equals`, `hashCode`, `toString`, `Builder` and `Companion`. The struct Go gives a union declares
  those codecs, `Validate` and `Value` (§4.4), and a wrapper those codecs and `Value`. No spec name
  lands in either, so none yields there.
- **A union's variants**: `Unknown`, the fallback's name (§4.4), which a variant the spec names so
  yields to.
- **A function**: its receiver, its context and request parameters, every local its body binds, and
  the package names its file imports. Whatever builds a body declares the names it binds beside it —
  the refine step that builds it as typed nodes, or the template that renders it, whose declaration
  the template lint holds to what the rendering binds (§5.3) — so no list is kept apart from the
  code it describes.

A spec name that meets a reserved one takes the target's escape (§8.8). Only spec names yield, since
the target's own names are what its runtime and templates are written against. A declaration a
caller names never yields to an import either: an import yields to it. Where a Kotlin file refers
to a declaration with the simple name of a class it would import, it writes that class qualified
rather than importing it; Go's exported names cannot meet its lowercase package names, and where two
packages one Go file imports share a name — the standard library's `time` and a group unit named
`time`, or two rows' `decimal` — one takes a name of its own in that file. So only a parameter or a
local yields to an import, and no caller depends on its name. That is why `ComputeImports` runs
before the naming steps (§4.1): what a file imports is fixed by where declarations landed, and what
the file calls each import is a name like any other. T-19 holds every scope's set, the names with no
words and the names that begin with a digit to code that compiles.
