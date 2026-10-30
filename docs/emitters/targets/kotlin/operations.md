# Operations in Kotlin

Operations are `suspend` functions, pages and server streams are `Flow`s, and the transport sits
behind the runtime's `Transport` (§5.4), a Ktor client by default on both. Java callers are not a
goal of the JVM target at M3: its surface is Kotlin's.

## The request type

Kotlin's request type (§4.13) is a class built like a [model](models.md).

## The client

The client's settings (§8.10) are setters on the client's builder.
