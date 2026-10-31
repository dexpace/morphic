# SDK-shaping hints — a third, ID-keyed input

oagen's `OperationHint` / rename / remount / split-union-body / url-builder machinery is *SDK
ergonomics, not API semantics* — the same category as runtime policy. It lives in
`EmitterInput.Hints`, keyed by **IR stable ID** (never `"METHOD /path"`), so a path rename never
drops a hint (INV3):

```go
type ShapeHint struct {
    Rename         string      // presentation rename for the entity's symbol; "" = none
    Remount        *ir.GroupID // move an operation under a different sub-client; nil = keep
    SplitUnionBody bool        // flatten a request-body union into typed wrapper methods — OPT-IN,
                               // the one sanctioned union→arguments collapse (../refine/unions.md); default off
    URLBuilder     bool        // expose a URL-builder variant of the operation
    Extra          map[string]ir.RawValue // free-form, forward-compatible per-target ergonomics
}
```

`Extra` is a bare raw-JSON map rather than `ir.RawConfig`: `RawConfig` names *declared protocol
configuration inside the IR* (`ir-design.md §12`), and a hint is neither — it is emitter input
authored alongside the SDK, and the IR never sees it.

Method-name *derivation* is itself an injectable policy marked `Inferred`, disable-able and
overridable. Per-target naming/compat overlays are the same shape: an emitter input keyed by IR ID,
so one IR document drives different compat baselines per language.
