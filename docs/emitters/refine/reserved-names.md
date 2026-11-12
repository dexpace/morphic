# Reserved names and collisions

`Reserved()` holds the language's keywords and predeclared names, reserved everywhere. Generated
code binds names of its own too, and a spec name rendered equal to one of them shadows it or fails
to compile, so each scope reserves what the code generated into it binds as well, and the units'
scope and a unit's what a unit or a top-level declaration cannot be named:

- **The units** ([output units](output-units.md)), whose names are a scope of their own: `shared`
  and `runtime`, the shared unit's and the runtime's, and the names a target's toolchain, or a file
  system it writes to, treats apart.
- **A unit**: the names the target declares at its top level beside the spec's, and those a
  top-level declaration cannot take, as when a file system refuses the file it is written to or the
  declaration would shadow a name the language imports by default. No spec name lands in the
  runtime's unit ([the runtime's unit](output-units.md#the-runtimes-unit)), so a file can always
  write one of its names beside a spec name of its own, Go qualified and Kotlin through an import of
  a name of its own ([Kotlin](../targets/kotlin/naming.md#imports)). Go reserves none, while Kotlin
  reserves them in every unit for its callers' sake
  ([Kotlin](../targets/kotlin/naming.md#reserved-names)), as a namespace every unit shares does
  ([Swift](../targets/kotlin/swift.md)).
- **A type**: the members the target gives every model.
- **A union's variants**: `Unknown`, the fallback's name ([unions](unions.md)), which a variant the
  spec names so yields to.
- **A function**: its receiver, its context and request parameters, every local its body binds, and
  the package names its file imports. Whatever builds a body declares the names it binds beside it —
  the refine step that builds it as typed nodes, or the template that renders it, whose declaration
  the template lint holds to what the rendering binds ([template
  guards](../emit/templates.md#two-guards-on-the-boilerplate-templates)) — so no list is kept apart
  from the code it describes.

Each target's sets are in its naming document ([Go](../targets/go/naming.md#reserved-names),
[Kotlin](../targets/kotlin/naming.md#reserved-names)).

A spec name that meets a reserved one takes the target's escape
([Go](../targets/go/naming.md#the-escape), [Kotlin](../targets/kotlin/naming.md#the-escape)). Only
spec names yield, since the target's own names are what its runtime and templates are written
against. A declaration a caller names never yields to an import either: an import yields to it, so
every reference a file writes begins with a name no nearer declaration takes, as each target says
([Go](../targets/go/naming.md#imports), [Kotlin](../targets/kotlin/naming.md#imports)). A Go caller
names no parameter, so there a parameter or a local yields to an import instead. A Kotlin caller
names one in a named argument, and a Swift caller as its label, so there an import yields to every
declaration of its file, at any depth. That is why `ComputeImports` runs before the naming steps
([the refiner contract](pipeline.md)): what a file imports is fixed by where declarations landed,
and what the file calls each import is a name like any other. Names that collide once cased are
`Ctx.Syms`'s on every target, and so, in a scope a target writes as paths, are names that differ in
letter case alone ([naming](naming.md#collisions-and-the-names-a-generation-shipped)). T-19 holds
every scope's set, the names with no words and the names that begin with a digit to code that
compiles, and names that fold alike to paths that do not; T-55 holds names holding a character a
target cannot write, or beginning with one its role cannot begin with, to code that compiles and a
caller in another unit that reaches each; and T-60 holds each reference to the declaration it means.
