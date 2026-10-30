### 8.2 A discriminated union (`Payment = Card | BankTransfer`, tag `type`)

IR: `Union{ Variants:[Card, BankTransfer], Exclusive:true, WireTagged:true,
Discriminator{PropertyName:"type", Mapping:{"card":Card.ID, "bank":BankTransfer.ID}} }`.

- **plan.** `TypePlan.Union` = `discriminated`, `Discrim` maps `card` and `bank` to the two
  variants' IDs, and `Open` says whether a value no variant recognizes is kept (§3.5). No
  primary-variant collapse — the union stays whole (INV2).
- **refine.** Go has no sum type → `LowerUnions` declares the struct, its sealed interface and a
  wrapper per variant (§4.4): `type Payment struct{ Value PaymentVariant }`, with `PaymentCard` and
  `PaymentBankTransfer` implementing an unexported marker. `LowerDiscriminator` reads `Mapping`
  (keyed by **TypeID**) and generates, on the struct, an `UnmarshalJSONFrom` that peeks the `"type"`
  wire key and dispatches; a tag that selects no variant, with no `Default`, lands in
  `PaymentUnknown` on an open union and is a coded decode error on a closed one, never a silent nil.
  Variant identity survives → `NewPaymentCard(...)`.
- **emit.** Printer emits the struct, the interface, the wrappers and markers, and the
  `UnmarshalJSONFrom`/`MarshalJSONTo` pair keyed on the **wire** name `"type"` (§4.8). The
  switch-completeness test guarantees every variant is handled.
