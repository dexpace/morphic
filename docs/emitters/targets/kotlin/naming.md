# Naming in Kotlin

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Kotlin:

- **The units**: `buildSrc`, which Gradle refuses as a project's name wherever its directory is; the
  root project's name, since a module of that name, published under the root's Maven `group`, has
  the root's coordinates, so Gradle resolves the root's dependency on it to the root itself and
  fails the build on a cycle, which also makes a root project the options name `shared` or
  `runtime`, a module the target names and never renames, an options error (a root project `runtime`
  beside the runtime's module failed: "Circular dependency between the following tasks"); and the
  names Windows keeps for its devices, `con`, `prn`, `aux`, `nul`, and `com` or `lpt` with a digit,
  in any letter case, which Windows refuses as a directory's name, so that neither the module nor
  the directory kotlinc writes the package's classes to could be made on Windows. The names Gradle
  and its plugins keep at a build's root meet no unit, as no module sits there ([output
  units](overview.md#output-units)).
- **A unit**: what the target declares there and what a top-level declaration cannot take:
  - `Client` and each sub-client, in the unit holding it, and in every unit what the runtime's
    templates declare, though the runtime's own unit holds no spec name ([the runtime's
    unit](../../refine/output-units.md#the-runtimes-unit)): `ApiFailure`, `ApiException` and the
    exceptions under it ([failures](errors.md)), `Fault`, `OtherResponse`, `OtherResponseException`,
    `Response`, `EnvelopedResponse`, `RawBody`, `Optional`, `Duration`, `Authenticator`,
    `Transport`, and the opt-in annotations `AlphaApi` and `BetaApi`
    ([stability](operations.md#stability)) among them, and on Multiplatform `Decimal` and
    `OffsetDateTime`. A file naming one beside a spec class of its name would write it qualified
    ([imports](#imports)), which fails in an expression where a member shadows the package's first
    segment: in a class with a property `com`, `com.acme.runtime.Duration(1)` failed ("unresolved
    reference 'acme'"). Once #1496 settles that, these names need no reserving outside the
    Objective-C namespace below.
  - Each name Kotlin's default imports give that a refine step, or a template rendering beside the
    spec's names, can write, such as `List`, `Map`, `String`, `Long`, `Unit`, `Exception` and
    `Result`, since a top-level declaration shadows a default import for its whole package: with a
    model `List` in a package, `List<String>` there failed ("No type arguments expected for 'class
    List : Any'"), and with a model `Exception`, a class extending `Exception` did. Each step and
    template declares those it writes as it declares what it binds, and the set is the union of
    what the steps and the templates rendering beside the spec's names declare, for the target's
    version, never what one unit's code happens to write, so adding an operation renames no model.
    The runtime's templates render into its unit alone, so they add none, while the template
    rendering an operation's row of the runtime's settings renders beside the spec's names, in the
    operation's unit ([runtime policy](../../contract/runtime-policy.md#delivery)).
  - Each Windows device name above, in any letter case, as the JVM writes a top-level class to a
    file of its name, `Aux.class`, which Windows refuses whatever the extension.
  - On Multiplatform, in the Objective-C namespace every unit's declarations share, a nested one's
    name joined to its outer's ([Swift](swift.md)): each name above that the target declares, the
    runtime's among them, in every unit, bar `AlphaApi` and `BetaApi`, annotation classes
    Kotlin/Native 2.4.0 leaves out of the header; the joined name of each public class the target
    nests in a type, such as `UserBuilder` and `UserCompanion` beside a model `User` and
    `PaymentUnknown` beside a union `Payment`; and the names the export gives classes of its own,
    `Base`, `MutableSet`, `MutableDictionary`, `Number` and each boxed number's, such as `Int`,
    `Double` and `ULong`.
  - On Multiplatform, among the Swift names a caller sees: every type name Swift's standard library
    and Foundation declare, which a Swift caller has in scope, a table of the options version as the
    naming tables are ([naming policy](../../refine/naming.md)), the target's own names included, so
    the runtime's `Optional`, `Duration` and `Decimal` take Swift names of their own; and the
    framework's own name, `Swift` and `Foundation`, which a caller writes to qualify a name
    (`Probe.Client`). A module a caller imports by choice, such as SwiftUI, reserves nothing, since
    a file importing it and the framework qualifies a name the two share. A declaration that meets a
    name of this bullet or the last only there keeps its Kotlin name and takes an explicit
    Objective-C and Swift name, so no JVM name moves for Swift's sake.
- **A type**: `equals`, `hashCode`, `toString`, `Builder` and `Companion`.

The names a unit declares and a union's variants are compared case-folded
([collisions](../../refine/naming.md#collisions-and-the-names-a-generation-shipped)), since the JVM
writes each class to a class file of its name, a nested one's joined to its outer's by `$`, and a
case-insensitive file system makes two that differ in letter case alone one file. With classes
`Setup` and `SetUp` in a package `p`, Kotlin 2.4.0 wrote one `p/Setup.class`, and with variants
`Card` and `CARD` of a sealed interface `Payment` one `p/Payment$Card.class`; each program built
without a diagnostic and failed to load the class it had lost (`NoClassDefFoundError: p/Payment$Card
(wrong name: p/Payment$CARD)`). So one of each pair takes a suffix, as one of two names that render
alike does, and a source file named for its class is held apart with it. A template writing a file
in a unit, as the one holding each operation's row of the runtime's settings does ([runtime
policy](../../contract/runtime-policy.md#delivery)), names it for a name it declares there, which
the unit reserves, so no spec class's file meets it. An inclusive union's variants are properties,
not classes, and compare folded all the same, so a union whose kind a revision changes keeps the
comparison its variants' names shipped under. A model's properties are no class, so they keep names
apart by case. A failure kind's exception is a class too, named for its kind in `Policy.Errors`
([failures](errors.md)), and a name a template declares never yields, so two kinds that differ in
letter case alone would share a class file; whether a configuration may give two such kinds is
#1471's.

A group's package segment is its words in lowercase, joined with no separator (`useraccounts`), as
Go's package names are, and a partition's is the name the options give it. So two groups whose words
break differently render alike and collide ([output units](../../refine/output-units.md)), and no
two units differ in letter case alone, which a case-insensitive file system would make one
directory: two partitions' names that do are an options error, and a group whose name differs from a
partition's in letter case alone yields to it.

None of a hard keyword, `kotlin` and `java` is a units name. A hard keyword as a package segment
takes the escape below, which kotlinc accepts in a `package` and an `import` alike: every hard
keyword, backticked, compiles on the JVM, and `fun`, `object`, `when`, `is`, `in` and `typealias`
also compile for JS and macOS Native and link into a framework. A soft or modifier keyword (`value`,
`data`, `import`, `internal`) needs no escape there. kotlinc refuses `kotlin` as a package's first
segment ("Only the Kotlin standard library is allowed to use the 'kotlin' package"), and the JVM
refuses to load a class whose package begins with `java` (`SecurityException: Prohibited package
name`). Only a name the options give a unit can begin a package, the root's above all, so one that
begins with either is an options error ([target options](../../contract/options.md)), and so is a
root with a Windows device name for any segment, since a name the options give never yields. Each
behaviour this section states was measured with Kotlin 2.4.0, Gradle 9.8.2 and Corretto 25 on
darwin/arm64, but for the Windows device names, which Windows' own file-naming rules give.

## The escape

A Kotlin hard keyword used as a name (`class`, `object`, `true`) is escaped with backticks, which
Kotlin allows on both platforms and which leaves the wire name alone; a name a scope reserves for
what the generated code binds ([reserved names](../../refine/reserved-names.md)), which backticks
would not set apart, takes a trailing underscore.

## Imports

Where a Kotlin file refers to a declaration with the simple name of a class it would import, it
writes that class qualified rather than importing it.
