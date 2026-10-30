# Reserved names and collisions

`Reserved()` holds the language's keywords and predeclared
names, reserved everywhere. Generated code binds names of its own too, and a spec name rendered
equal to one of them shadows it or fails to compile, so each scope reserves what the code generated
into it binds as well, and the units' scope what a unit cannot be named:

- **The units** (§4.17), whose names are a scope of their own: `shared`, the shared unit's, and the
  names a target's toolchain treats apart.
- **A unit**: the names the target declares at its top level beside the spec's.
- **A type**: the members the target gives every model.
- **A union's variants**: `Unknown`, the fallback's name (§4.4), which a variant the spec names so
  yields to.
- **A function**: its receiver, its context and request parameters, every local its body binds, and
  the package names its file imports. Whatever builds a body declares the names it binds beside it —
  the refine step that builds it as typed nodes, or the template that renders it, whose declaration
  the template lint holds to what the rendering binds (§5.3) — so no list is kept apart from the
  code it describes.

Each target's sets are in its naming document ([Go](../targets/go/naming.md#reserved-names), [Kotlin](../targets/kotlin/naming.md#reserved-names)).

A spec name that meets a reserved one takes the target's escape ([Go](../targets/go/naming.md#the-escape), [Kotlin](../targets/kotlin/naming.md#the-escape)).
Only spec names yield, since the target's own names are what its runtime and templates are written
against. A declaration a caller names never yields to an import either: an import yields to it, as
each target says ([Go](../targets/go/naming.md#imports), [Kotlin](../targets/kotlin/naming.md#imports)). So only a parameter or a local yields to an
import, and no caller depends on its name. That is why `ComputeImports` runs before the naming steps
(§4.1): what a file imports is fixed by where declarations landed, and what the file calls each
import is a name like any other. Names that collide once cased are `Ctx.Syms`'s on every target
(§4.12). T-19 holds every scope's set, the names with no words and the names that begin with a digit
to code that compiles.
