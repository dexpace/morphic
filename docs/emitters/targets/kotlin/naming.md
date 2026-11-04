# Naming in Kotlin

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Kotlin:

- **The units**: `buildSrc`, which Gradle refuses as a project's name wherever its directory is,
  and the root project's name: a module of that name, published under the root's Maven `group`, has
  the root's coordinates, so Gradle resolves the root's dependency on it to the root itself and
  fails the build on a cycle. The names Gradle and its plugins keep at a build's root meet no unit,
  as no module sits there ([output units](overview.md#output-units)).
- **A unit**: `Client` and each sub-client, in the unit holding it, and what a runtime template
  declares, in a unit it renders into beside the spec's names ([template
  guards](../../emit/templates.md#two-guards-on-the-boilerplate-templates)): `ApiFailure`,
  `ApiException` and the exceptions under it ([failures](errors.md)), `Fault`, `OtherResponse`,
  `OtherResponseException`, `Response`, `EnvelopedResponse`, `RawBody`, `Optional`, `Duration`,
  `Authenticator` and `Transport` among them, and on Multiplatform `Decimal` and `OffsetDateTime`.
  On Multiplatform, where every unit's declarations share one Objective-C namespace and a nested
  one's name is joined to its outer's ([Swift](swift.md)), each of these is reserved in every unit,
  whichever unit declares it, and so is the joined name of each public class the target nests in a
  type, such as `UserBuilder` and `UserCompanion` beside a model `User` and `PaymentUnknown` beside
  a union `Payment`. Which unit the runtime renders into is #1489's to decide.
- **A type**: `equals`, `hashCode`, `toString`, `Builder` and `Companion`.

None of a hard keyword, `kotlin` and `java` is a units name. A hard keyword as a package segment
takes the escape below, which kotlinc accepts in a `package` and an `import` alike: `fun`, `object`,
`when`, `is`, `in` and `typealias`, backticked, compile for the JVM, JS and macOS Native and link
into a framework, and those with `class`, `val`, `interface`, `as`, `true`, `null`, `this`, `super`,
`package`, `return`, `throw` and `try` on the JVM. kotlinc refuses `kotlin` as a package's first
segment ("Only the Kotlin standard library is allowed to use the 'kotlin' package"), and the JVM
refuses to load a class whose package begins with `java` (`SecurityException: Prohibited package
name`). Only a name the options give a unit can begin a package, the root's above all, so one that
begins with either is an options error ([target options](../../contract/options.md)). Each behaviour
this section states was measured with Kotlin 2.4.0, Gradle 9.8.2 and Corretto 25 on darwin/arm64.

## The escape

A Kotlin hard keyword used as a name (`class`, `object`, `true`) is escaped with backticks, which
Kotlin allows on both platforms and which leaves the wire name alone; a name a scope reserves for
what the generated code binds ([reserved names](../../refine/reserved-names.md)), which backticks
would not set apart, takes a trailing underscore.

## Imports

Where a Kotlin file refers to a declaration with the simple name of a class it would import, it
writes that class qualified rather than importing it.
