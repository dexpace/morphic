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
2. `equals`, `hashCode` and `toString` are generated, and `toString` redacts a `Secret` property or
   a `Sensitive` type ([Go's `User`](../go/models.md#user-in-go)), which a data class's would print.
3. An operation's request type ([request shaping](../../refine/requests.md)) is built the same way,
   so an optional property or input the spec adds is a new setter on both platforms.

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
