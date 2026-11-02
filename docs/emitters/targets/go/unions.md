# Unions in Go

Go has no sum type, so the Go refiner pairs a struct with a sealed interface, the closed-sum idiom
the IR itself mandates, with a generated switch-completeness test over the variant set:

```go
type Payment struct{ Value PaymentVariant }           // what a reference names; declares the codec

type PaymentVariant interface{ isPaymentVariant() }   // the closed set
type PaymentCard struct{ Value Card }                 // one wrapper per variant
type PaymentBankTransfer struct{ Value BankTransfer }
```

- **The struct declares the codec.** Go refuses a method whose receiver is an interface type, and
  json/v2 refuses to decode into a sealed one:
  `cannot derive concrete type for nil interface with finite type set`. On the struct, the pair
  travels with the type, so a caller's own `json.Unmarshal` decodes a `Payment` and any type of the
  caller's that holds one, as [the type table](types.md#what-a-mapped-type-must-do) asks of every Go
  type an SDK names. The struct's `Value` is the price at each use. Decoding in each holder's codec
  and the client would leave a caller's own call failing, and a registered `json.WithUnmarshalers`
  would reach only values decoded under the SDK's options, which is why a Go row of the type table
  has no codec of its own ([a mapped type](types.md#what-a-mapped-type-must-do)). The struct encodes
  through its variant, and one whose `Value` is nil refuses to encode, never writing `null`, and
  fails `Validate`.
- **A wrapper for every variant.** The marker is an unexported method, which only a type declared in
  the interface's package can implement. So a variant in another output unit ([output
  units](../../refine/output-units.md)) cannot, nor can one a target spells as `string`, `[]T` or a
  map, which declares no method, and a type many unions name would carry a marker for each. A
  wrapper in the union's unit takes any variant, and keeps apart two variants that lower to one Go
  type. A wrapper declares the codec pair too, delegating to its `Value`, so it codes as its variant
  does when a caller's own call meets it alone.
- **A polymorphic base lowers the same way** ([the shared
  rule](../../refine/unions.md#a-polymorphic-base)). `Pet` is a struct holding `PetVariant`, with a
  wrapper per subtype.

Whatever the [strategy that recognizes a
variant](../../refine/unions.md#variant-identity-and-recognition), the codec is a
`MarshalJSONTo`/`UnmarshalJSONFrom` pair, for the reason [Discriminators](#discriminators) gives.

## The fallback

The fallback ([the shared rule](../../refine/unions.md#a-value-no-variant-recognizes)) is one more
variant, `PaymentUnknown`, holding the value as `jsontext.Value`. It re-encodes as the same JSON
value, though json/v2 normalizes whitespace and string escapes as it writes.

## An inclusive union

An inclusive union ([the shared rule](../../refine/unions.md#a-value-several-variants-admit)) holds
every variant a value admits, so it is a struct with a pointer per variant and no interface:

```go
type Target struct {        // anyOf: [Zone, Account], which one value can fit
    Zone    *Zone           // set when Zone admits the value
    Account *Account        // set when Account admits it, beside Zone or alone
    Unknown jsontext.Value  // an open union's fallback, never set beside a variant
}
```

- **A pointer per variant**, whatever the variant's Go type, so nil means "does not admit it" for a
  slice or a string too. Each field is named as the variant's wrapper in a closed sum is, so two
  variants that lower to one Go type stay apart.
- **The struct declares the codec pair**, as a closed sum's struct does. `UnmarshalJSONFrom` buffers
  the value (`dec.ReadValue()`), decodes it into each variant under `dec.Options()`, and keeps each
  that admits it: the decode succeeds, and the unexported presence check generated beside the
  variant's `Validate`, which reads no `Constraints`, finds every required member. `MarshalJSONTo`
  writes the merge.
- **`Validate` reports a struct with no field set**, and validates each field that is. With no sum
  to close, it has no marker, no wrapper and no switch-completeness test.

## Discriminators

`LowerDiscriminator` emits a tag-dispatched `UnmarshalJSONFrom` that peeks the discriminator
**wire** key (never the Go field name), typed factory functions, and a `MarshalJSONTo` that writes
the tag ([the shared rule](../../refine/unions.md#discriminators)). Go declares the pair on the
union's struct.

The pair is `encoding/json/v2`'s `MarshalerTo`/`UnmarshalerFrom`, not v1's `MarshalJSON`/
`UnmarshalJSON`, because only the v2 methods see the caller's encoder and decoder. `MarshalJSONTo`
writes the variant straight to the caller's `jsontext.Encoder`. A tag may sit anywhere in its
object, so `UnmarshalJSONFrom` buffers the one value it dispatches (`dec.ReadValue()`), scans it for
the tag without decoding the rest, and decodes the variant from that buffer under `dec.Options()`:
the options the caller decoded under reach the variant's body. A v1 `UnmarshalJSON` receives only
bytes, so it decodes the variant under options of its own choosing, never the caller's, and a
type-specific decoder the caller registered with `json.WithUnmarshalers` stops applying at the first
union. The one option a model sets over its caller's is `RejectUnknownMembers`, which each model
pins for itself ([`User`](models.md#user-in-go)). `ir/json.go` is the worked example:
`(*TypeRegistry).UnmarshalJSONFrom` buffers each entry, `stringMember` finds the `kind` tag by token
scan, and `decodeTypeDef` decodes the concrete kind under the decoder's options.

## `Payment` in Go

The IR and the plan are in [unions](../../refine/unions.md#a-discriminated-union).

- **refine.** Go has no sum type → `LowerUnions` declares the struct, its sealed interface and a
  wrapper per variant: `type Payment struct{ Value PaymentVariant }`, with `PaymentCard` and
  `PaymentBankTransfer` implementing an unexported marker. `LowerDiscriminator` reads `Mapping`
  (keyed by **TypeID**) and generates, on the struct, an `UnmarshalJSONFrom` that peeks the `"type"`
  wire key and dispatches; a tag that selects no variant, with no `Default`, lands in
  `PaymentUnknown` on an open union and is a coded decode error on a closed one, never a silent nil.
  Variant identity survives → `NewPaymentCard(...)`.
- **emit.** Printer emits the struct, the interface, the wrappers and markers, and the
  `UnmarshalJSONFrom`/`MarshalJSONTo` pair keyed on the **wire** name `"type"`. The
  switch-completeness test guarantees every variant is handled.
