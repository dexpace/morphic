# Multiplatform common code

Multiplatform common code has no `java.time`, no `BigDecimal`, no reflection and no `java.io`.

- An exact number — `decimal`, `decimal128`, or an `integer` or `number` whose type-table row keeps
  it exact (§2.6) — is the runtime's `Decimal`, a value class over the decimal text: every digit
  round-trips, it does no arithmetic, and a caller on the JVM converts it to `BigDecimal`.
- A `datetime_offset` is the runtime's `OffsetDateTime`, a `kotlinx-datetime` `LocalDateTime`
  beside the `UtcOffset` it was written with, which converts to an `Instant`. The offset survives,
  as it does in Go's `time.Time` and the JVM's `OffsetDateTime`, and re-encoding writes it back.
