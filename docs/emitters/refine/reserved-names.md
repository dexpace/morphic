# Reserved names and collisions

`Reserved()` holds the language's keywords and predeclared names, reserved everywhere. Generated
code binds names of its own too, and a spec name rendered equal to one of them shadows it or fails
to compile, so each scope reserves what the code generated into it binds as well, and the units'
scope and a unit's what a unit or a top-level declaration cannot be named:

- **The units** ([output units](output-units.md)), whose names are a scope of their own: `shared`
  and `runtime`, the shared unit's and the runtime's, the names a target's toolchain, or a file
  system it writes to, treats apart, and on Go each predeclared identifier, which an import of a
  unit so named would hide in a caller's file ([Go](../targets/go/naming.md#reserved-names)).
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
  spec names so yields to, and on Kotlin each class its toolchain nests where the variants are
  nested ([Kotlin](../targets/kotlin/naming.md#reserved-names)).
- **A function**: its receiver, its context and request parameters, every local its body binds,
  every name of its unit the body writes unqualified, as its operation's row of the runtime's
  settings, and on Go the package names its file imports. Whatever builds a body declares the names
  it binds beside it — the refine step that builds it as typed nodes, or the template that renders
  it, whose declaration the template lint holds to what the rendering binds ([template
  guards](../emit/templates.md#two-guards-on-the-boilerplate-templates)) — so no list is kept apart
  from the code it describes.

Each target's sets are in its naming document ([Go](../targets/go/naming.md#reserved-names),
[Kotlin](../targets/kotlin/naming.md#reserved-names)).

A spec name that meets a reserved one takes the target's escape
([Go](../targets/go/naming.md#the-escape), [Kotlin](../targets/kotlin/naming.md#the-escape)),
whatever reserved it: a keyword, a predeclared identifier, a name generated code binds in a function
or declares in a type, one the runtime or a template declares in a unit, a class the language
imports by default, a name a file system refuses, or a class the toolchain nests beside a union's
variants. In a scope compared folded
([naming](naming.md#collisions-and-the-names-a-generation-shipped)) a name that folds as a reserved
one meets it. The escape is a trailing underscore, and for a Kotlin keyword backticks, which keep
the name. The numeric suffix of the collision rule tells two spec names apart and answers no
reservation. The escape reads the name and the reserved set and nothing else its scope holds, and no
identifier casing renders ends in an underscore, so an escaped name is one no other declaration of
its scope renders to. A scope joining the names of several, as the Objective-C namespace every
Kotlin unit's declarations share, holds each name as it exports, an explicit Swift name or the
Kotlin name after its own scope's escape, and reserves the target's names as they export, so a name
escaped in one scope can meet one escaped in another: it takes the escape again there, a type
`Duration`, `Duration_` in Kotlin, exporting as `Duration__` beside the runtime's `Duration_`, and
which unit's package sorts first decides nothing
([Kotlin](../targets/kotlin/naming.md#reserved-names)). A suffix is one another can: a type `Error`
given `Error2` for a reserved `Error` would hold what a type `error_2` renders to, and their IDs
would decide which kept it. Names that meet one reserved name are escaped and then told apart as any
two are, `Result_` and `Result_2`, and a suffix that would make a reserved name is passed over, as
`com2` is. Two reservations are answered otherwise. An import that yields (below) takes the
collision rule's suffix whatever it met, `error2` or `Dog2`: it yields to its file's own
declarations too, which only that rule compares, and its name is its file's alone, so nothing a
caller writes turns on it. A Kotlin method or enum entry in an Objective-C method family takes its
role's leading word in its Swift name, since a trailing underscore leaves it in the family
([Swift](../targets/kotlin/swift.md)).

Only spec names yield, since the target's own names are what its runtime and templates are written
against, so an import yields to a name the target binds on every target. A declaration a caller
names never yields to an import either: an import yields to it, so every reference a file writes
begins with a name no nearer declaration takes, as each target says
([Go](../targets/go/naming.md#imports), [Kotlin](../targets/kotlin/naming.md#imports)). A Go caller
names no parameter, so there a parameter the spec names yields to an import instead, and the
function's scope reserves the name its file gives each import. The naming steps settle that name
after the units' names and before any other scope's ([the refiner contract](pipeline.md)), since on
Go it reads only its package's name, what the target binds,
the predeclared identifiers its file writes and its file's other imports: with Go 1.27.3, a
parameter `time2`, escaped against `time` alone, hid the unit `time` its file then imported as
`time2` (`time2.Zone is not a type`). A Kotlin caller names one in a named argument, and a Swift
caller as its label, so there an import yields to every declaration of its file, at any depth, and
no scope reserves an import. An import has no ID, so which of two keeps a name is each target's to
say from what they import. That is why `ComputeImports` runs before the naming steps ([the refiner
contract](pipeline.md)): what a file imports is fixed by where declarations landed, and what the
file calls each import is a name like any other. Names that collide once cased are `Ctx.Syms`'s on
every target, and so, in a scope a target writes as paths, are names that fold alike
([naming](naming.md#collisions-and-the-names-a-generation-shipped)). T-19 holds every scope's set,
the names with no words and the names that begin with a digit to code that compiles, and names that
fold alike to paths that do not; T-54 holds names holding a character a target cannot write, or
beginning with one its role cannot begin with, to code that compiles and a caller in another unit
that reaches each; and T-55 holds each reference to the declaration it means.
