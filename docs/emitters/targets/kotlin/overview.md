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
what its modules declare ([scale](scale.md)). A group's unit is `<root>.<group>`.

Kotlin's limit counts a Gradle module's declarations, and its default is set when the Kotlin targets
are first measured ([open questions](../../open-questions.md)).

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
dumps the Kotlin Gradle plugin's ABI validation writes: the JVM's class-file signatures, and for
Multiplatform the KLib ABI plus the generated Objective-C header, since a change Swift sees can
break callers the Kotlin ABI does not show. Kotlin's severity differs from Go's in two places: a
parameter added with a default leaves Kotlin callers compiling but changes the JVM signature, which
is binary-breaking, and on Multiplatform a rename only the Swift header shows still breaks Swift
callers.
