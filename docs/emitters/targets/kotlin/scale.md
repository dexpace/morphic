# Scale

What a Kotlin target must stay inside, beside the [size
check](../../refine/output-units.md#the-size-check).

The JVM caps a method at 255 parameter slots and 64 KiB of bytecode, and a class at 65,535
constant-pool entries. A refiner checks each declaration against its target's limits and lowers past
them to a shape that stays inside, or diagnoses what it cannot: the decoder of a model with hundreds
of properties is split across methods, and a builder whose constructor would pass the parameter cap
is a diagnostic. Since every model is built through a builder, none changes shape when a property it
gains crosses a limit. On Multiplatform, Kotlin/Native compile time and the size of the iOS
framework grow with every declaration the SDK exports, so the runtime's helpers are `internal`, and
the same split, whose units are Gradle modules, bounds what one module exports.
