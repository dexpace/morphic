# Naming in Kotlin

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Kotlin:

- **The units**: `build`, `buildsrc` in any letter case, `gradle` and `src`, the names Gradle gives
  a meaning at a build's root, where a group's module sits ([output
  units](overview.md#output-units)). `build` is the root project's output, so a `clean` of the root
  deletes a module there, sources and all. `buildSrc` is Gradle's build-logic build, found without
  regard to case on a case-insensitive file system such as macOS's, so a module named `buildsrc`
  fails the build there. `gradle` holds the wrapper and the version catalog, and `src` the root
  project's sources, so a module named either would share their directory.
- **A unit**: `Client` and, in a group's unit, its sub-client; and in the root unit every runtime
  name the target declares there, which each runtime template declares beside it, as a function's
  names are ([reserved names](../../refine/reserved-names.md)): `ApiFailure`, `ApiException`,
  `ClientException`, `ServerException`, the exception of each kind the error taxonomy names
  (`NotFoundException`), `Fault`, `OtherResponse`, `OtherResponseException`, `Response`, `RawBody`,
  `Optional`, `Duration`, `Authenticator` and `Transport`, and on Multiplatform `Decimal` and
  `OffsetDateTime`.
- **A type**: `equals`, `hashCode`, `toString`, `Builder` and `Companion`.

Neither a hard keyword nor `kotlin` is a units name. A hard keyword as a package segment takes the
escape below, which kotlinc accepts in a `package` and an `import` alike: `fun`, `object`, `when`,
`is`, `in` and `typealias`, backticked, compile for the JVM, JS and macOS Native and link into a
framework, and those with `class`, `val`, `interface`, `as`, `true`, `null`, `this`, `super`,
`package`, `return`, `throw` and `try` on the JVM. kotlinc refuses `kotlin` only as a package's
first segment ("Only the Kotlin standard library is allowed to use the 'kotlin' package"), which is
the root's and so the options' to name. Each of these was measured with Kotlin 2.4.0 and Gradle
9.8.2 on darwin/arm64.

## The escape

A Kotlin hard keyword used as a name (`class`, `object`, `true`) is escaped with backticks, which
Kotlin allows on both platforms and which leaves the wire name alone; a name a scope reserves for
what the generated code binds ([reserved names](../../refine/reserved-names.md)), which backticks
would not set apart, takes a trailing underscore.

## Imports

Where a Kotlin file refers to a declaration with the simple name of a class it would import, it
writes that class qualified rather than importing it.
