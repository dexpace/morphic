### 8.8 The Kotlin targets

Both Kotlin targets build the walkthrough's constructs from the same plan facts; what differs is
the runtime they target and the language's own rules.

**Generated Kotlin SDKs require Kotlin 2.4 or newer**, on both targets, and their build files say
so. 2.4 is the first release whose standard library's `kotlin.uuid.Uuid` is stable, and
`kotlin.time.Instant` became stable in 2.3, so no type an SDK exposes needs an opt-in. That is the
rule the floor serves: an opt-in API in a public signature makes every caller opt in too. The floor,
and the `kotlinx.serialization` and `kotlinx-datetime` releases beside it, move only with an options
version (§2.5).

**Models and request types are classes, not data classes.** A data class's constructor and `copy`
change signature with every property it gains, so a property the spec adds would break every JVM
binary compiled against the SDK, and Swift, which sees no default arguments, would make every
caller pass it. Kotlin's guidance for library authors keeps data classes out of a public API for
the first reason. Each is a final class instead:

1. A model's properties are public `val`s and its constructor is not public. A nested `Builder`
   takes the required properties in its constructor and the optional ones through setters. A Kotlin
   caller writes `User(name = "Ada") { email = "ada@example.com" }` over it; a Swift caller uses the
   builder itself.
2. `equals`, `hashCode` and `toString` are generated, and `toString` redacts a `Secret` property or
   a `Sensitive` type (§8.1), which a data class's would print.
3. An operation's request type (§4.13) is built the same way, so an optional property or input the
   spec adds is a new setter on both platforms.

A required property the spec adds changes the builder's constructor, as it changes the API.

**Serialization and calls.**

- Serialization is `kotlinx.serialization` on both. Multiplatform has no reflection, which rules
  out a reflective JSON library, and the JVM target sharing it is what lets one refine pipeline
  serve both. Codecs are generated `KSerializer`s, the counterpart of §4.8's
  `MarshalJSONTo`/`UnmarshalJSONFrom`.
- Unknown members follow §8.1's per-model rule: the SDK's `Json` is strict, and each model the spec
  leaves open carries `@JsonIgnoreUnknownKeys`, so a closed model nested in an open one stays strict
  and the reverse stays open. A model with a catch-all (`AdditionalProps`) is the exception: the
  annotation would drop the undeclared members before its serializer saw them, so its serializer
  reads the object whole through `JsonDecoder.decodeJsonElement` and folds them into the map, as
  Go's embedded fallback receives them whatever the options say.
- Operations are `suspend` functions, pages and server streams are `Flow`s, and the transport sits
  behind the runtime's `Transport` (§5.4), a Ktor client by default on both. Java callers are not a
  goal of the JVM target at M3: its surface is Kotlin's.
- A discriminator's tag is the wire string the mapping names, a numeric-looking one included
  (`"10016"`): a `KSerializer`, like Go's `UnmarshalJSONFrom`, compares it as a string and never
  parses it as a number.
- A merge-patch body (`application/merge-patch+json`) is where all three of an optional field's
  states change the request: absent leaves the member as it is and `null` removes it. Every target
  sends both through the presence box (§9: `Optional<T?>` on Kotlin, `OptNil[T]` on Go), never
  through a nullable alone.

**Multiplatform common code** has no `java.time`, no `BigDecimal`, no reflection and no `java.io`.

- An exact number — `decimal`, `decimal128`, or an `integer` or `number` whose type-table row keeps
  it exact (§2.6) — is the runtime's `Decimal`, a value class over the decimal text: every digit
  round-trips, it does no arithmetic, and a caller on the JVM converts it to `BigDecimal`.
- A `datetime_offset` is the runtime's `OffsetDateTime`, a `kotlinx-datetime` `LocalDateTime`
  beside the `UtcOffset` it was written with, which converts to an `Instant`. The offset survives,
  as it does in Go's `time.Time` and the JVM's `OffsetDateTime`, and re-encoding writes it back.

**Swift.** A Multiplatform SDK reaches iOS through the Objective-C export, which shapes these rules.

- A sealed interface is not exhaustive in a Swift `switch`, so Swift callers need a `default` arm,
  and the generated docs say so.
- A name the export would mangle — `description` and `hash` (`NSObject` members), a name in the
  `init`, `new`, `copy` or `alloc` method families, a Swift keyword — gets an explicit Objective-C
  and Swift name from the naming policy (`@ObjCName`), so the Swift name is the policy's choice
  rather than the exporter's.
- Default arguments do not reach Swift, which is one reason models and request types are built
  through a builder rather than a constructor with defaults.
- An exception reaches Swift only as `@Throws` declares it: any other terminates the app, and a
  `suspend` function with no `@Throws` propagates cancellation alone. So every operation carries
  `@Throws(ApiException::class, OtherResponseException::class, CancellationException::class)`, which
  covers every class of the tree (§8.5) and a response its method cannot return (§8.9), and a Swift
  caller receives an `NSError` holding the exception, and reads its `failure` or `response`.

**Formatting.** Go formats in process with `go/format` (§5.1). Go can call no Kotlin formatter, and
`Generate` does no I/O, so the Kotlin printer lays out canonical code itself, and the Kotlin
formatter runs as a check in the verify stage (#704) rather than inside the emitter.

**Identifiers.** A Kotlin hard keyword used as a name (`class`, `object`, `true`) is escaped with
backticks, which Kotlin allows on both platforms and which leaves the wire name alone; a name a
scope reserves for what the generated code binds (§4.12), which backticks would not set apart, takes
a trailing underscore. Go's casing lifts exported names clear of its keywords, so only locals and
parameters can meet one there: a keyword (`type`, `default`), a predeclared identifier a parameter
would shadow (`string`, `error`), a package its file imports (`time`, `url`) or a name the method's
body binds (`ctx`, `params`) takes a trailing underscore, and so does an exported name the target
declares itself (`Client`). Names that collide once cased are `Ctx.Syms`'s on every target (§4.12).

**Inheritance.** Go has only embedding. Kotlin's single class inheritance cannot hold several
`Mixins`, and extending a base would make it an open class the SDK must keep compatible for every
subclass. So on all three targets a model's `Base` and `Mixins` are flattened into it
(`TypePlan.Flattened`), which holds the members its `Implements` declare as well, since a type
conforming to an interface carries them. Subtyping survives as interfaces: `Implements` becomes an
interface, and a discriminated or abstract base a sealed interface of its subtypes, which Go holds
in a struct (§4.4).

**Scale.** The JVM caps a method at 255 parameter slots and 64 KiB of bytecode, and a class at
65,535 constant-pool entries. A refiner checks each declaration against its target's limits and
lowers past them to a shape that stays inside, or diagnoses what it cannot: the decoder of a model
with hundreds of properties is split across methods, and a builder whose constructor would pass the
parameter cap is a diagnostic. Since every model is built through a builder, none changes shape
when a property it gains crosses a limit. Go has no such caps, but one package of tens of thousands
of types builds slowly and in gigabytes of memory, which §4.17's size check reports and its split by
group bounds. On Multiplatform, Kotlin/Native compile time and the size of the iOS framework grow
with every declaration the SDK exports, so the runtime's helpers are `internal`, and the same split,
whose units are Gradle modules, bounds what one module exports.
