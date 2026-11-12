# The Kotlin targets

Both Kotlin targets build the constructs the shared documents describe from the same plan facts;
what differs is the runtime they target and the language's own rules.

## Two targets, one package

Kotlin/JVM and Kotlin Multiplatform are two targets and one package. They share a language, so one
AST, printer and refine pipeline serve both; they differ in a runtime, and that difference is data:
a `Platform` value carrying its built-in type table ([type table](../../contract/type-table.md)),
whose rows for the runtime-dependent primitives differ between the two, the identifier rules the
Objective-C/Swift export adds on Multiplatform, and the HTTP engine each platform's transport
defaults to. A lowering that branches on the platform reads that value, never the target key. They
are two targets rather than one with an option because their artifacts, surfaces and toolchains
differ: a JVM SDK exposes `java.time` and `BigDecimal`, which Multiplatform common code does not
have ([Multiplatform](multiplatform.md)).

## The language floor

**Generated Kotlin SDKs require Kotlin 2.4 or newer**, on both targets, and their build files say
so. 2.4 is the first release whose standard library's `kotlin.uuid.Uuid` is stable, and
`kotlin.time.Instant` became stable in 2.3, so no type an SDK exposes needs an opt-in. That is the
rule the floor serves: an opt-in API in a public signature makes every caller opt in too. The floor,
and the `kotlinx.serialization` and `kotlinx-datetime` releases beside it, move only with an options
version ([target options](../../contract/options.md)).

## Serialization

Serialization is `kotlinx.serialization` on both. Multiplatform has no reflection, which rules out a
reflective JSON library, and the JVM target sharing it is what lets one refine pipeline serve both.
Codecs are generated `KSerializer`s, the counterpart of Go's
[`MarshalJSONTo`/`UnmarshalJSONFrom`](../go/unions.md#discriminators).

## Output units

A Kotlin [unit](../../refine/output-units.md) is a package, and also a Gradle module of its own
unless it is the root, since kotlinc compiles a module whole and a Multiplatform framework exports
what its modules declare ([scale](scale.md)). A group's unit is `<root>.<group>`, and each module is
a project named for its unit, in the directory `modules/<name>` rather than at the build's root,
where Gradle and its plugins keep names of their own, a set any plugin may add to. There, measured
with Kotlin 2.4.0 and Gradle 9.8.2, a `clean` of the root deletes a module named `build`, sources
and all; a case-insensitive file system such as macOS's finds one named `buildsrc` as Gradle's
build-logic build, which then fails; one named `gradlew` fails `gradle wrapper`; and one named
`api`, `gradle` or `src` shares its directory with the root project's ABI dump, the wrapper's files
or the root's sources. Under `modules/` each builds, and a `clean` and a `wrapper` leave every
module's sources. What a module's project still meets is in [naming](naming.md#reserved-names).

The [runtime's unit](../../refine/output-units.md#the-runtimes-unit) is `<root>.runtime`, the
module `modules/runtime`, which the root project and every other module depend on as an `api`
dependency and a Multiplatform framework exports with the rest ([Swift](swift.md)). Measured with
Kotlin 2.4.0, code depending on the root project, or on a group's module, that took the runtime as
`implementation` could not name the runtime's `ApiException` ("Unresolved reference 'runtime'"),
and a framework that did not export the runtime named its `ApiException` `RuntimeApiException` in
the header and left out the `NotFoundException` under it, which no signature names, so a Swift
caller could neither match a failure's class nor name the runtime's types as a Kotlin caller does.

Kotlin's `internal` reaches only its own module, and a call to an `internal` runtime function from a
group's module failed ("it is internal"), so what generated code calls in another module is public:
the runtime's helpers, and the function building a sub-client, which the root's client calls while
the sub-client's constructor stays `internal`. What keeps a caller from them is
[below](#what-a-caller-reaches-of-the-runtime). On Multiplatform each carries `@HiddenFromObjC` as
well, which kept it out of the header, so the framework exports no more than its callers use
([scale](scale.md)). The function is there because a constructor cannot be hidden so ("This
annotation is not applicable to target 'constructor'"), and a sub-client's public constructor was
exported as `init(settings:)` over an `id`, which a Swift caller could call with any object.
`@HiddenFromObjC` is experimental in Kotlin 2.4.0, so a file using it opts in with
`@file:OptIn(ExperimentalObjCRefinement::class)`, which no caller inherits.

Kotlin's limit counts a Gradle module's declarations, and its default is set when the Kotlin targets
are first measured ([open questions](../../open-questions.md)).

## What a caller reaches of the runtime

What generated code alone calls ([the runtime's
unit](../../refine/output-units.md#the-runtimes-unit)) is public, since its callers sit in other
modules, and each such declaration, a function, a class or the function building a sub-client,
carries `@InternalSdkApi`, which the runtime declares:

```kotlin
@RequiresOptIn(
    level = RequiresOptIn.Level.ERROR,
    message = "Only the SDK's generated code calls this.",
)
@Retention(AnnotationRetention.BINARY)
annotation class InternalSdkApi
```

Each generated file that uses one opts in with `@file:OptIn(InternalSdkApi::class)`, which no
caller inherits. Measured with Kotlin 2.4.0, across modules:

- a caller's call to a marked function, and its construction of a marked class, failed to compile
  with the annotation's message, as did a group's module whose file left the opt-in out, while a
  caller using the client, a sub-client's method and `ApiException` needed nothing;
- a caller writing `@OptIn(InternalSdkApi::class)` compiled. The marker does not make a call
  impossible; it makes the caller's own source say it calls what the SDK does not version, as
  Kotlin's libraries mark their own internals;
- the marker is Kotlin's, so Java compiled a call to a marked function. Each marked function
  carries `@JvmSynthetic` as well, which `javac` refused to resolve (`cannot find symbol`). A marked
  class and an `internal` constructor, public in the class file, stay open to Java, whose callers
  are no goal of the target ([operations](operations.md));
- the marker does not reach the Objective-C export: a marked function and a marked class without
  `@HiddenFromObjC` were both in the framework's header, and a Swift caller called them, so on
  Multiplatform each carries both ([above](#output-units)). The annotation class itself is not
  exported.

The dumps the extractor reads ([below](#merging-and-surface-extraction)) are written with the
marker excluded, by each module's build file:

```kotlin
kotlin {
    abiValidation {
        filters { exclude { annotatedWith.add("<root>.runtime.InternalSdkApi") } }
    }
}
```

Measured with the Kotlin Gradle plugin 2.4.0 and Gradle 9.8.2, the
runtime's dump listed a marked class, a marked function and a `@JvmSynthetic` one without the
filter, and a group's dump its sub-client's factory; with it each dump held none of them, and still
held `ApiException` and the sub-client's method. So a helper's change fails no `checkKotlinAbi`,
and a change to what a caller names does.

**One version.** A module is an artifact of its own, so a build can resolve two modules of one SDK
at different versions, and a module then calls a runtime it was not generated with. Measured: with
each module depending plainly on the others, an application depending on the root at 1.0 and on a
group's module at 1.1 resolved the runtime to 1.1 and failed when run (`NoSuchMethodError: 'void
p.runtime.Core.<init>(java.lang.String)'`), the root's 1.0 code calling a constructor 1.1 had
changed. So each module's build file states, beside its dependency on another module of the SDK, a
constraint on that module `strictly` at its own version. With it the same application failed when
its dependencies were resolved (`Cannot find a version of 'g:sdk-users' that satisfies the version
constraints`). The constraint is published in the Gradle module metadata; the POM Gradle writes
beside it carried the plain version, so a Maven build is held only while it depends on the root
alone.

Three other designs were weighed:

- **`internal`, with each module a friend of the runtime's.** `-Xfriend-paths` let a group's module
  call an `internal` runtime function, but it is one of the compiler's advanced options, which its
  help says "may be changed or removed without any notice", and Java still called the function,
  `internal` being public in the class file.
- **`@PublishedApi internal`.** Refused across modules as `internal` is ("it is internal"), and
  listed in the ABI dump.
- **One module for the SDK**, where `internal` would do, gives up what the modules are for
  ([scale](scale.md)).

## Formatting

Go formats in process with `go/format` ([printing](../../emit/printing.md)). Go can call no Kotlin
formatter, and `Generate` does no I/O, so the Kotlin printer lays out canonical code itself, and the
Kotlin formatter runs as a check in the verify stage (#704) rather than inside the emitter.

## Merging and surface extraction

The parser Kotlin's own tools rely on is the compiler's front end, which runs on the JVM, so the
Kotlin targets' adapter is a JVM helper the engine runs, since the engine owns all I/O. Until it
exists the Kotlin targets merge nothing and report `Capabilities.WriteIntegrate` false: pruning
stays header-gated and no hand-written file is touched.

The Kotlin targets' extractors ([surface verification](../../surface-verification.md)) read the ABI
dumps the Kotlin Gradle plugin's ABI validation writes, what generated code alone calls left out
of them ([above](#what-a-caller-reaches-of-the-runtime)): the JVM's class-file signatures, and for
Multiplatform the KLib ABI plus the generated Objective-C header, since a change Swift sees can
break callers the Kotlin ABI does not show. Kotlin's severity differs from Go's in two places: a
parameter added with a default leaves Kotlin callers compiling but changes the JVM signature, which
is binary-breaking, and on Multiplatform a rename only the Swift header shows still breaks Swift
callers.
