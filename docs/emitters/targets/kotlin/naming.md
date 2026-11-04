# Naming in Kotlin

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Kotlin:

- **The units**: `buildSrc`, which Gradle refuses as a project's name wherever its directory is;
  the root project's name, since a module of that name, published under the root's Maven `group`,
  has the root's coordinates, so Gradle resolves the root's dependency on it to the root itself and
  fails the build on a cycle; and the names Windows keeps for its devices, `con`, `prn`, `aux`,
  `nul`, and `com` or `lpt` with a digit, in any letter case, which Windows refuses as a directory's
  name, so that neither the module nor the directory kotlinc writes the package's classes to could
  be made on Windows. The names Gradle and its plugins keep at a build's root meet no unit, as no
  module sits there ([output units](overview.md#output-units)).
- **A unit**: `Client` and each sub-client, in the unit holding it, and what a runtime template
  declares, in a unit it renders into beside the spec's names ([template
  guards](../../emit/templates.md#two-guards-on-the-boilerplate-templates)): `ApiFailure`,
  `ApiException` and the exceptions under it ([failures](errors.md)), `Fault`, `OtherResponse`,
  `OtherResponseException`, `Response`, `EnvelopedResponse`, `RawBody`, `Optional`, `Duration`,
  `Authenticator` and `Transport` among them, and on Multiplatform `Decimal` and `OffsetDateTime`.
  Each Windows device name above is reserved too, in any letter case, as the JVM writes a top-level
  class to a file of its name, `Aux.class`, which Windows refuses whatever the extension. On
  Multiplatform, where every unit's declarations share one Objective-C namespace and a nested one's
  name is joined to its outer's ([Swift](swift.md)), each name above that the target declares is
  reserved in that namespace too, in every unit, and so is the joined name of each public class the
  target nests in a type, such as `UserBuilder` and `UserCompanion` beside a model `User` and
  `PaymentUnknown` beside a union `Payment`. A declaration that meets one only there keeps its
  Kotlin name and takes an explicit Objective-C and Swift name, so no JVM name moves for Swift's
  sake. Which unit the runtime renders into is #1489's to decide.
- **A type**: `equals`, `hashCode`, `toString`, `Builder` and `Companion`.

A unit's package segment is its words in lowercase, joined with no separator (`useraccounts`), as
Go's package names are. So two units whose words break differently render alike and collide ([output
units](../../refine/output-units.md)), and no two differ in letter case alone, which a
case-insensitive file system would make one directory.

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
