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
whatever the options say. Where the catch-all's `Key` is set, it reads each name through the key's
text serializer ([types](types.md#integers)), refuses two that read as one key, and writes each
key's text back: with a `Long` key, `{"1": …, "2.0": …}` read as 1 and 2, which were written `{"1":
…, "2": …}`, and `{"1": …, "1e0": …}` was refused (Kotlin 2.4.0).

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

An alias, as any declaration of a file that is no class, gives its file a class of kotlinc's on the
JVM, `UserKt` for `User.kt`. No spec name meets one, since a unit reserves every name ending in `Kt`
([naming](naming.md#reserved-names)), so an alias sits in the file of the class it names. The
Objective-C header shows such a class only for a function or a property at a file's top level, and
one class for every unit's file of that name: with Kotlin/Native 2.4.0, a `User.kt` holding a class
and an alias gave the header no `UserKt`, while two units' `Kind.kt`, each declaring `const val
KIND`, gave one class `KindKt` holding `KIND` and `KIND_`, the second the constant of the unit whose
package sorts last, so a unit added could rename a shipped constant. So no file holding the spec's
declarations declares a function or a property at its top level: a factory is a companion's member,
as above, and a literal's constant a member of an `object` named for the literal, `object Kind {
const val VALUE = "user" }`, which a Swift caller read as `Kind.shared.VALUE` in both orders of the
units, the object told apart from another unit's `Kind` as any class is there ([Swift](swift.md)).
T-60 holds this section.

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
@fa253fe19). So no depth is safe on a stack the runtime does not measure, and it measures each.

The emitter writes, for each type the client decodes or encodes, the most one level of it can cost:
8 KB, 512 bytes for each member of the widest model the type reaches, and 6 KB for each union or
model with a catch-all a value can pass between two levels. That is about twice what those runs
measured, 4.3 KB, 235 bytes and 2.1 to 2.6 KB, and the three numbers are a table of the options
version, measured on each Kotlin release. Before the runtime decodes a text it multiplies that cost
by the depth its scan counted, and before it encodes, prints, hashes, compares or validates a value
by the depth the value records (below). Where the product and 32 KB more fit in what the current
thread has left, from the current frame to the end of its stack, which `pthread_get_stackaddr_np`
and `pthread_get_stacksize_np` give on Apple's platforms, the work runs there; otherwise the runtime
runs it on a thread it starts with a stack of that sum, joins it, and hands its caller the work's
result or the failure it raised. Each generated `toString`, `hashCode`, `equals` and `validate`
checks on entry, and a check nested in one that passed passes too, a level spending less than its
cost. Measured so (Kotlin/Native 2.4.0, through Gradle on macOS arm64, a debug and an optimized
build), each shape above decoded, encoded, printed, hashed and compared at every depth from 1 to 64
from threads of 48 KB to 1 MB; a caller that had left itself 59 KB of a 512 KB thread decoded 14
levels of the member shape on a thread started for it; and on the main thread, 8 MB left, a hash of
a value of the 200-member model two levels deep started none. With what is left read as unbounded, a
64 KB thread ended the process in the debug build at 17 levels of the member shape, which the
optimized build decoded to 64. Reading what is left took 8 ns optimized and 17 in debug, and
starting and joining a thread 12 µs and 30 µs.

One cost serves every operation, a decode's, since each of the others took less stack a level at
every depth measured: the 200-member model printed on 10,160 bytes a level in the debug build and
decoded on 31,152. A `validate`, which walks as `equals` does, was not run and is taken to as well.
So on a thread short of stack they move at depths it could take: on a 512 KB thread, 1,461 of the
3,072 calls of that run started a thread. A cost for each operation would spare those, at four more
figures to measure on each release. A budget fixed at 128 KB, a quarter of a 512 KB stack, was the
alternative, and reads nothing of the thread, so it is wrong both ways: it decoded those 14 levels
in place with 59 KB left, which ended the process in the debug build, and on the main thread it
started a thread for that hash, 23 µs where the hash took 0.43 µs (optimized).

Each platform Multiplatform is built for ([the first targets](../../README.md#the-first-targets))
has what it gives. Its JVM is the JVM above. Native gives the reading on Apple's platforms, the ones
named, and was run on macOS arm64 alone, iOS not. JS and Wasm give no reading and no thread to
start, so every call runs where it is made. On JS an overflow is a `RangeError`, which the runtime
catches as it does the JVM's. On Wasm `catch (e: Throwable)` did not catch it and it ended the
program, so there the limit is the one bound, and a model wide enough overflows within it. Built for
production and run under node 26.13.0 (Kotlin 2.4.0, kotlinx-serialization-json 1.11.0), a model
nested through a member, a list, a map and an `any`, one whose member is a union read through a
`JsonElement` and one of 200 members each decoded, encoded, printed, hashed and compared at every
depth from 1 to 64 on both; the 200-member model overflowed first, at 229 levels on JS and 266 on
Wasm, each depth in a process of its own, and a JS decode overflowing below a caller deep in its own
stack was caught, the next call decoding. A browser's stack was not run. Multiplatform still reports
a `MaxDepth` above 64 ([runtime policy](../../contract/runtime-policy.md#how-deep-a-value-nests)),
no deeper value having been run on Native and Wasm having nothing to catch. A default lowered to
what the costliest measured shape takes was the alternative, and the next wider model would pass it.

Two other bounds lost. A count kept in the serializers has nowhere to live: the plugin generates a
model's, and a `Decoder` carries no state of the SDK's. Reading a `JsonElement` first bounds nothing
by itself: `parseToJsonElement` read 100,000 levels, and `decodeFromJsonElement` overflowed between
1,500 and 2,000 on the default stack. A text a caller hands their own `Json` is not scanned: the
call and the text are theirs.

A value a caller builds can nest past the limit, and on Native printing one would end the process as
decoding one would. So every model, union wrapper, inclusive union's class and positional class
records, when it is constructed, how deep its JSON form nests, one more than its deepest member's,
a wrapper and an inclusive union's class adding no level of their own, in `jsonDepth`, a member
marked `@InternalSdkApi` ([the runtime](overview.md#what-a-caller-reaches-of-the-runtime));
the record holds because a built value never changes (above). Nothing recurses into a value past the
limit: the client refuses to encode it, with an error and nothing sent, `toString` shows its class
and its depth alone, `equals` compares the depths first and holds such a value equal only to itself,
`hashCode` is then its depth, and `validate()` reports it as nested past the limit: it returns its
failures, none for a value that passes, the first 1,000 and then one naming the check `maxFailures`
where it found more (one sharing each child at 22 levels, its every node failing two constraints,
returned 1,001 in 105 ms, where keeping all held 8,388,606 in 1.2 GB, Kotlin 2.4.0, JVM 25), each a
`ValidationFailure` of the runtime's naming the member and the check, whose `nesting` is true for a
value past either limit and false for a constraint's and for that last, so a caller tells them apart
by no message. Throwing them was the alternative, and reaches a Swift caller only through `@Throws`,
any other exception ending its app ([Swift](swift.md)); a Swift caller read `nesting` from the list
returned (Kotlin/Native 2.4.0). Each records in `jsonValues`, beside it, how many values its JSON
form holds, the sum of its members' and one for itself, none for a wrapper or an inclusive union's
class, held at the greatest `Int` once past it, and a value past `MaxValues` is treated the same
way, its count shown beside its depth: one sharing each child at 22 levels, 16,777,214 values,
printed, hashed, compared and was found in a set in 0.9 s, and one at 30 levels answered all four in
68 µs, equal only to itself (Kotlin 2.4.0, JVM 25). Measured on Kotlin/Native
2.4.0, a value 200,002 levels deep printed, hashed, compared and was found in a set on a 64 KB
stack, where the same nesting with no record ended the process on 512 KB. Both Kotlin targets do
this. On the JVM a `MaxDepth` raised past what a thread's stack takes still leaves a value within it
the `StackOverflowError` any deep structure meets there, which its caller can catch. A count kept in
a thread-local as each method recurses would bound them too, at two reads of it for every nested
value hashed. T-63 holds these.
