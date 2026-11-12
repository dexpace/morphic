# Models in Kotlin

## Models and request types are classes, not data classes

A data class's constructor and `copy` change signature with every property it gains, so a property
the spec adds would break every JVM binary compiled against the SDK, and Swift, which sees no
default arguments, would make every caller pass it. Kotlin's guidance for library authors keeps data
classes out of a public API for the first reason. Each is a final class instead:

1. A model's properties are public `val`s and its constructor is not public. A nested `Builder`
   takes the required properties in its constructor and the optional ones through setters. A Kotlin
   caller writes `User(name = "Ada") { email = "ada@example.com" }` over it, which is the
   companion's `operator fun invoke` and no top-level function ([below](#a-shape-that-is-an-alias));
   a Swift caller uses the builder itself.
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
class, `typealias UserCreate = User`, and nothing more. Kotlin reaches a companion through an alias,
and a companion holds each factory: a model's `invoke`, and for a union one function for each
variant bar the fallback, named for the variant as a property is, `Payment.card(value)`. So a caller
writes `UserCreate(name = "Ada") { pay = PaymentCreate.card(CardCreate(number = "1")) }` and names
each type by its alias, and a caller in another module doing so compiled unchanged once
`UserCreate`, `PaymentCreate` and `CardCreate` were classes of their own (Kotlin 2.4.0). An alias
does not reach a nested class (`UserCreate.Builder`: `unresolved reference 'Builder'`), so a caller
naming a builder, or a variant's wrapper in a `when`, writes `User.Builder` or `Payment.Card` and
changes that line then. A binary built before fails against the SDK after (`NoSuchMethodError`), as
with any changed signature. The Objective-C export shows no alias: a framework declaring one named
the class alone in its header, a function taking the alias taking the class, so a Swift caller names
`User` until `UserCreate` is a class (Kotlin/Native 2.4.0).

No file generated into a unit declares a function or a property at its top level. Either gives the
file a class of kotlinc's, `UserKt` for `User.kt`, which a spec class `UserKt` meets on the JVM
(`duplicate JVM class name 'p/UserKt'`) and in the Objective-C header, where the file's class then
moves to `UserKt_` under its Swift callers. So a factory is a companion's member, as above, where a
function `UserCreate(…)` beside the alias was the alternative, and a literal's `const val` is a
member of an `object` named for the literal. A `typealias` gives its file such a class as well,
beside a class as alone, so each alias has a file of its own, which names that class for the alias
with `@file:JvmName` ([naming](naming.md#reserved-names)), common code included; the header shows no
class for it. Built so, a unit held spec classes `UserKt` and `UserCreateKt` beside `User` and the
alias `UserCreate`, on the JVM and in a framework's header, which named both as written (Kotlin
2.4.0, Kotlin/Native 2.4.0). T-60 holds this section.

## Nesting

kotlinx decodes by recursion and bounds none of it, so a body nested deeply enough overflows the
stack, and how deep is enough depends on the stack of the thread the caller chose. The runtime's
serializer therefore scans each JSON text before it decodes it, a response's body, an error's and a
stream's frame, counting the objects and arrays open outside a string and the values, and refuses
one nested past `MaxDepth` or holding more than `MaxValues` as [a body that does not
decode](../../refine/models.md#how-deep-a-value-nests). The depth scan keeps no state but the count
and took 1.2 ms on a 1.2 MB text, where decoding a 173 KB body of models took 0.95 ms.

A model nested through a member decoded 2,000 levels deep and overflowed at 3,000 on the JVM's 2 MB
default stack here, decoded 400 and overflowed at 500 on a 512 KB one, and decoded 100 and
overflowed at 200 on a 256 KB one; nested through a list of itself, two levels a model, it
overflowed at 400 levels on 512 KB and at 128 on 256 KB, where 64 decoded, printed, hashed, compared
and encoded (Kotlin 2.4.0, kotlinx-serialization-json 1.11.0, JVM 25, darwin/arm64). On the JVM an
overflow is a `StackOverflowError`, which the runtime catches around a decode and an encode and
reports as a body that does not decode or as the encode's error: on a 256 KB stack the overflow of a
text of 3,000 levels was caught and the next call decoded. So Kotlin/JVM holds to every `MaxDepth`
of the range, a body within the limit that a small stack cannot take failing as one past it does.

Kotlin/Native has nothing to catch: an overflow ends the process (`SIGBUS`). And what a level costs
there is the document's, no constant. In a debug build on a 512 KB stack, the size Apple's platforms
give a thread a program starts, a model nested through a member decoded 120 levels deep, through a
list 104, through a map 96 and an `any` 128; a model whose member is a union read through a
`JsonElement` 80, one whose union's variant is a union 54, and a model with a catch-all holding such
a union in a box 62; and a model with 50 members beside the one it recurs through 32, and with 200
members 10, the plugin's `deserialize` keeping a slot for each, where an optimized build took it 28
(Kotlin/Native 2.4.0, kotlinx-serialization-json 1.11.0, macOS arm64, through Gradle). The golden
spec's widest model has 175 members and its widest that recurs 26 (spec @bc6fad24, morphic
@fa253fe19). So no depth is safe on a stack the runtime did not size, and it sizes one.

The emitter writes, for each type the client decodes or encodes, the most one level of it can cost:
8 KB, 512 bytes for each member of the widest model the type reaches, and 6 KB for each union or
model with a catch-all a value can pass between two levels. That is about twice what those runs
measured, 4.3 KB, 235 bytes and 2.1 to 2.6 KB, and the three numbers are a table of the options
version, measured on each Kotlin release. Before the runtime decodes a text it multiplies that cost
by the depth its scan counted, and before it encodes, prints, hashes or compares a value by the
depth the value records (below). Where the product passes 128 KB, a quarter of that stack, it does
the work on a thread it starts with a stack of the product and 128 KB more, and joins it; that
thread is marked, and work on it starts no other. Measured so, each shape above decoded, encoded,
printed, hashed and compared 64 levels deep from a 512 KB thread, the 200-member model among them,
whose encoding ended the process at 32 levels on that thread itself and whose `toString` and
`equals` did at 128. A value 2,000 levels deep printed, hashed and compared from a 64 KB thread on
one thread started for each call, and on 1,987 for one `toString` with the mark unread. Starting and
joining a thread took 19 µs, where decoding six levels of the 50-member model took 2 µs.
Multiplatform still reports a `MaxDepth` above 64 ([runtime
policy](../../contract/runtime-policy.md#how-deep-a-value-nests)), no deeper value having been run,
and a caller who starts a Native thread with less than 512 KB, or calls from deep in a recursion of
their own, decodes on it at a risk the SDK cannot see. A default lowered to what the costliest
measured shape takes was the alternative, and the next wider model would pass it.

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
and `hashCode` is then its depth. Each records in `jsonValues`, beside it, how many values its JSON
form holds, the sum of its members' and one for itself, held at the greatest `Int` once past it,
and a value past `MaxValues` is treated the same way, its count shown beside its depth: one sharing
each child at 22 levels, 16,777,214 values, printed, hashed, compared and was found in a set in 0.9
s, and one at 30 levels answered all four in 68 µs, equal only to itself (Kotlin 2.4.0, JVM 25).
Measured on Kotlin/Native 2.4.0, a value 200,002 levels deep
printed, hashed, compared and was found in a set on a 64 KB stack, where the same nesting with no
record ended the process on 512 KB. Both Kotlin targets do this. On the JVM a `MaxDepth` raised past
what a thread's stack takes still leaves a value within it the `StackOverflowError` any deep
structure meets there, which its caller can catch. A count kept in a thread-local as each method
recurses would bound them too, at two reads of it for every nested value hashed. T-63 holds these.
