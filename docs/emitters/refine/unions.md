### 4.4 Unions (the hardest axis)

`LowerUnions` reads the plan's wire shape (`TypePlan.Union`) and the target's capability. The
tag-mode matrix (each a distinct serializer) is normative (`ir-design.md §4.4`):

- `WireTagged=false` → untagged; variant inferred by validation (JSON oneOf/anyOf).
  `Exclusive=true` = exactly-one; `false` = one-or-more (anyOf). An untagged union carries
  no `Discriminator`.
- `WireTagged=true` + `Discriminator` → the tag travels in the payload, where the discriminator
  locates it: an OpenAPI `discriminator`, GraphQL `__typename`, an Erlang tagged tuple. It sits
  inline among the variant's own members (the property `PropertyName` names, or the tuple element
  at `Index`), or, with `Discriminator.Envelope == "object"`, in a `{kind, value}` wrapper around
  them.
- `WireTagged=true` + nil `Discriminator` → key-tagged: a single-key object keyed by the variant's
  wire name (Smithy unions, proto3-JSON oneof, GraphQL `@oneOf`).

Go has no sum type, so the Go refiner pairs a struct with a sealed interface, the closed-sum idiom
the IR itself mandates, with a generated switch-completeness test over the variant set:

```go
type Payment struct{ Value PaymentVariant }           // what a reference names; declares the codec

type PaymentVariant interface{ isPaymentVariant() }   // the closed set
type PaymentCard struct{ Value Card }                 // one wrapper per variant
type PaymentBankTransfer struct{ Value BankTransfer }
```

- **The struct declares the codec.** Go refuses a method whose receiver is an interface type, and
  json/v2 refuses to decode into a sealed one: `cannot derive concrete type for nil interface with
  finite type set`. On the struct, the pair travels with the type, so a caller's own
  `json.Unmarshal` decodes a `Payment` and any type of the caller's that holds one, as §2.6 asks of
  every Go type an SDK names. The struct's `Value` is the price at each use. Decoding in each
  holder's codec and the client would leave a caller's own call failing, and a registered
  `json.WithUnmarshalers` would reach only values decoded under the SDK's options, which is why a Go
  row of the type table has no codec of its own (§2.6). The struct encodes through its variant, and
  one whose `Value` is nil refuses to encode, never writing `null`, and fails `Validate`.
- **A wrapper for every variant.** The marker is an unexported method, which only a type declared
  in the interface's package can implement. So a variant in another output unit (§4.17) cannot, nor
  can one a target spells as `string`, `[]T` or a map, which declares no method, and a type many
  unions name would carry a marker for each. A wrapper in the union's unit takes any variant, and
  keeps apart two variants that lower to one Go type. A wrapper declares the codec pair too,
  delegating to its `Value`, so it codes as its variant does when a caller's own call meets it
  alone. Kotlin's variant classes are the same wrapper, for the same reason (§9).
- **A polymorphic base lowers the same way.** A reference to a model with a `Discriminator`, or an
  `Abstract` one (ir-design §4.9), holds any of its subtypes, so `Pet` is a struct holding
  `PetVariant`, with a wrapper per subtype, and `Discrim` routes it as a union's. The base's own
  shape, unless it is `Abstract`, is a variant too, a struct of its flattened properties.

Variant identity (`Variant.Name`) survives for named accessors / constructors; `WireName`/`WireID`
survive for (de)serialization. **Unions never degrade to optional-field merges** — flattening a
*body* union into method arguments is an opt-in `ShapeHint` (§6.4), never a silent loss (the oagen
counterexample, prior-art §1). A future TS/Python target lowers the *same* facts to a native
`A | B | C`; the decision differs, the fact base is identical. Kotlin declares its `KSerializer` on
the sealed interface itself, which a Kotlin interface allows, so it needs no struct. What `Value`
holds when a value satisfies several variants of an `anyOf` is #959's.

Within the untagged case, choosing *how* a variant is recognized on decode is refine's job, and
ogen's `schema_gen_sum.go` is the battle-tested enumeration to lift: try, in order, explicit
discriminator → implicit discriminator (variant const/name) → JSON-type discrimination (variants
distinguishable by JSON kind) → unique-field discrimination → value-based. The decisive divergence:
ogen computes this *while building the Go model*, so a union it cannot discriminate aborts the whole
generation. Because Morphic keeps `Union` lossless in the IR and only an emitter picks a strategy, a
Go refiner that finds no strategy emits a coded diagnostic and decodes every value into the fallback
below, which keeps its bytes as they arrived — a degrade, never a whole-document failure (§13 test
T-7). That graceful-degradation margin is exactly what the ABI seam buys and the fused-pipeline
generator cannot have. Whatever the strategy, the codec is a `MarshalJSONTo`/`UnmarshalJSONFrom`
pair, for the reason §4.8 gives.

**A value no variant recognizes.** All four wire shapes decode in one order: the variant the tag or
the key names, or the one recognition finds; then `Discriminator.Default`, for a tag it does not
name or no tag at all; then, on an `Open` union (§3.5), the fallback; and otherwise a coded decode
error. A tag that names a variant whose body fails to decode is an error either way: the variant is
known, and the value does not match it. The fallback is one more variant, `PaymentUnknown` on Go,
holding the value as `jsontext.Value`, and `Payment.Unknown` on Kotlin, holding a `JsonElement`. It
re-encodes as the same JSON value, its members in order and each number's text intact, though
json/v2 normalizes whitespace and string escapes as it writes. It has no factory, since only a
response produces one, and one a caller sends back goes out as it came. A closed union declares
none.

### 4.8 Discriminators

`LowerDiscriminator` reads a `Discriminator` whose `Mapping` points at **TypeIDs, not names**, so a
presentation rename never breaks the wiring. It emits a tag-dispatched `UnmarshalJSONFrom` that
peeks the discriminator **wire** key (never the Go field name), typed factory functions, and a
`MarshalJSONTo` that writes the tag. `Property` vs `PropertyName` vs `Index` (positional),
`Envelope` vs inline, and the `Default` variant on an absent/unrecognized tag all survive
un-lowered; the choice of factory vs native vs wrapper is the refiner's. Go declares the pair on
the union's struct (§4.4), and a tag that selects no variant takes §4.4's order.

The pair is `encoding/json/v2`'s `MarshalerTo`/`UnmarshalerFrom`, not v1's `MarshalJSON`/
`UnmarshalJSON`, because only the v2 methods see the caller's encoder and decoder. `MarshalJSONTo`
writes the variant straight to the caller's `jsontext.Encoder`. A tag may sit anywhere in its
object, so `UnmarshalJSONFrom` buffers the one value it dispatches (`dec.ReadValue()`), scans it
for the tag without decoding the rest, and decodes the variant from that buffer under
`dec.Options()`: the options the caller decoded under reach the variant's body. A v1
`UnmarshalJSON` receives only bytes, so it decodes the variant under options of its own choosing,
never the caller's, and a type-specific decoder the caller registered with `json.WithUnmarshalers`
stops applying at the first union. The one option a model sets over its caller's is
`RejectUnknownMembers`, which each model pins for itself (§8.1). `ir/json.go` is the worked
example: `(*TypeRegistry).UnmarshalJSONFrom` buffers each entry, `stringMember` finds the `kind`
tag by token scan, and `decodeTypeDef` decodes the concrete kind under the decoder's options.
