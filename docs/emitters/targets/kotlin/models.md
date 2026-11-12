# Models in Kotlin

## Models and request types are classes, not data classes

A data class's constructor and `copy` change signature with every property it gains, so a property
the spec adds would break every JVM binary compiled against the SDK, and Swift, which sees no
default arguments, would make every caller pass it. Kotlin's guidance for library authors keeps data
classes out of a public API for the first reason. Each is a final class instead:

1. A model's properties are public `val`s and its constructor is not public. A nested `Builder`
   takes the required properties in its constructor and the optional ones through setters. A Kotlin
   caller writes `User(name = "Ada") { email = "ada@example.com" }` over it; a Swift caller uses the
   builder itself.
2. `equals`, `hashCode` and `toString` are generated. `equals` and `hashCode` read each member
   through its type's own, which every built-in row's type defines over its value, `bytes` among
   them ([bytes](types.md#bytes)), and a list, a map and a union's wrapper in turn through their
   elements'; `toString` shows a member the model redacts as `[REDACTED]`
   ([redaction](../../refine/redaction.md#a-redacted-value), [Go's
   `User`](../go/models.md#user-in-go)), which a data class's would print.
3. An operation's request type ([request shaping](../../refine/requests.md)) is built the same way,
   so an optional property or input the spec adds is a new setter on both platforms.
4. A builder copies each list and map it is given, at every level, and an `any` member's
   `JsonElement`, by a walk that keeps its own stack. So a built model holds no collection its
   caller can change: its `hashCode` is fixed, and it cannot hold itself. Measured with Kotlin
   2.4.0, a model keeping the caller's list changed its hash when the list grew and was no longer
   found in the set that held it, and one whose list was then given the model itself overflowed the
   stack in `toString`; with the copy neither happened. A decoded value's collections are the
   decoder's own, which nothing else holds, and are not copied again. T-63 holds it.

A required property the spec adds changes the builder's constructor, as it changes the API.

## Optionality

The states' forms are in the [lowering table](lowering.md).

## Unknown members

Unknown members follow [the shared rule](../../refine/models.md#unknown-members): the SDK's `Json`
is strict, and each model the spec leaves open carries `@JsonIgnoreUnknownKeys`, so a closed model
nested in an open one stays strict and the reverse stays open. A model with a catch-all
(`AdditionalProps`) is the exception: the annotation would drop the undeclared members before its
serializer saw them, so its serializer reads the object whole through
`JsonDecoder.decodeJsonElement` and folds them into the map, as Go's embedded fallback receives them
whatever the options say.

## Recursive references

Kotlin holds every class by reference, so a cycle asks nothing of either Kotlin target.

## A shape that is an alias

A shape whose projection coincides with the read one
([TypePlan](../../plan/types.md#the-declaration-a-shape-takes)) is a `typealias` of the type's
class, in that class's file, beside a factory function of the alias's name: `typealias UserCreate =
User` and `fun UserCreate(name: String, block: User.Builder.() -> Unit = {}): UserCreate`. A caller
builds a request through that function and names its type by the alias, and a caller in another
module doing both compiled unchanged once `UserCreate` was a class of its own (Kotlin 2.4.0). An
alias does not reach the builder class (`UserCreate.Builder`: `unresolved reference 'Builder'`), so
a caller naming it writes `User.Builder` and changes that line then. A binary built before fails
against the SDK after (`NoSuchMethodError`), as with any changed signature. The Objective-C export
shows no alias: a framework declaring one named the class alone in its header, a function taking the
alias taking the class, so a Swift caller names `User` until `UserCreate` is a class (Kotlin/Native
2.4.0). T-60 holds it.

## Nesting

kotlinx decodes by recursion and bounds none of it, so a body nested deeply enough overflows the
stack, and how deep is enough depends on the stack of the thread the caller chose. The runtime's
serializer therefore scans each JSON text before it decodes it, a response's body, an error's and a
stream's frame, counting the objects and arrays open outside a string, and refuses one nested past
`MaxDepth` as [a body that does not decode](../../refine/models.md#how-deep-a-value-nests). The scan
keeps no state but the count and took 1.2 ms on a 1.2 MB text, where decoding a 173 KB body of
models took 0.95 ms.

A model nested through a member decoded 2,000 levels deep and overflowed at 3,000 on the JVM's 2 MB
default stack here, decoded 400 and overflowed at 500 on a 512 KB one, and decoded 100 and
overflowed at 200 on a 256 KB one; nested through a list of itself, two levels a model, it
overflowed at 400 levels on 512 KB and at 128 on 256 KB, where 64 decoded, printed, hashed, compared
and encoded (Kotlin 2.4.0, kotlinx-serialization-json 1.11.0, JVM 25, darwin/arm64). On the JVM an
overflow is a `StackOverflowError`, which the runtime catches around a decode and an encode and
reports as a body that does not decode or as the encode's error: on a 256 KB stack the overflow of a
text of 3,000 levels was caught and the next call decoded. So Kotlin/JVM holds to every `MaxDepth`
of the range, a body within the limit that a small stack cannot take failing as one past it does.

Kotlin/Native has nothing to catch: an overflow ends the process (`SIGBUS`). In a debug build, the
same two models decoded, encoded, printed, hashed and compared 96 levels deep on a 512 KB stack, the
size Apple's platforms give a thread a program starts, and the one nested through a list ended the
process at 112; on 256 KB both passed 48 levels and ended it at 64; an optimized build passed 384
levels on 512 KB (Kotlin/Native 2.4.0, macOS arm64, through Gradle). So the default of 64 is what a
512 KB stack was shown to take, Multiplatform reports a `MaxDepth` above it ([runtime
policy](../../contract/runtime-policy.md#how-deep-a-value-nests)), and a caller who starts a Native
thread with a smaller stack decodes on it at a risk the SDK cannot see.

Two other bounds lost. A count kept in the serializers has nowhere to live: the plugin generates a
model's, and a `Decoder` carries no state of the SDK's. Reading a `JsonElement` first bounds nothing
by itself: `parseToJsonElement` read 100,000 levels, and `decodeFromJsonElement` overflowed between
1,500 and 2,000 on the default stack. A text a caller hands their own `Json` is not scanned: the
call and the text are theirs.

A value a caller builds can nest past the limit, and on Native printing one would end the process as
decoding one would. So every model, union wrapper and positional class records, when it is
constructed, how deep its JSON form nests, one more than its deepest member's, in `jsonDepth`, a
member marked `@InternalSdkApi` ([the runtime](overview.md#what-a-caller-reaches-of-the-runtime));
the record holds because a built value never changes (above). Nothing recurses into a value past the
limit: the client refuses to encode it, with an error and nothing sent, `toString` shows its class
and its depth alone, `equals` compares the depths first and holds such a value equal only to itself,
and `hashCode` is then its depth. Measured on Kotlin/Native 2.4.0, a value 200,002 levels deep
printed, hashed, compared and was found in a set on a 64 KB stack, where the same nesting with no
record ended the process on 512 KB. Both Kotlin targets do this. On the JVM a `MaxDepth` raised past
what a thread's stack takes still leaves a value within it the `StackOverflowError` any deep
structure meets there, which its caller can catch. A count kept in a thread-local as each method
recurses would bound them too, at two reads of it for every nested value hashed. T-63 holds these.
