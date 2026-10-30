# Naming in Kotlin

## Reserved names

What each scope (§4.12) reserves in Kotlin:

- **A type**: `equals`, `hashCode`, `toString`, `Builder` and `Companion`.

## The escape

A Kotlin hard keyword used as a name (`class`, `object`, `true`) is escaped with
backticks, which Kotlin allows on both platforms and which leaves the wire name alone; a name a
scope reserves for what the generated code binds (§4.12), which backticks would not set apart, takes
a trailing underscore.

## Imports

Where a Kotlin file refers
to a declaration with the simple name of a class it would import, it writes that class qualified
rather than importing it.
