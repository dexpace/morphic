# SDK-shaping hints — a third, ID-keyed input

oagen's `OperationHint` / rename / remount / split-union-body / url-builder machinery is *SDK
ergonomics, not API semantics* — the same category as runtime policy. It lives in
`EmitterInput.Hints`, keyed by **IR stable ID** (never `"METHOD /path"`), so a path rename never
drops a hint (INV3):

```go
type ShapeHint struct {
    Rename         string      // presentation rename for the entity's symbol; "" = none
    Remount        *ir.GroupID // move an operation under a different sub-client; nil = keep
    AlsoMount      []Mount     // expose an operation under more sub-clients too (below)
    SplitUnionBody bool        // flatten a request-body union into typed wrapper methods — OPT-IN,
                               // the one sanctioned union→arguments collapse (../refine/unions.md); default off
    URLBuilder     bool        // expose a URL-builder variant of the operation
    ClientParams   []ParamKey  // on a service: the parameters its client holds as settings
                               // (../operations/client.md#client-settings)
    Extra          map[string]ir.RawValue // free-form, forward-compatible per-target ergonomics
}

type Mount struct {
    Group  ir.GroupID          // the sub-client
    Rename string              // the method's name there; "" = the name its own group gives it
}

type ParamKey struct {         // a parameter as every operation names it, never by ID alone
    In   ir.HTTPLocation       // path, query, header, cookie or host; any other is reported
    Name string                // its wire name, a header's compared without case
}
```

A hint's JSON form names each field in lower camel case, as runtime policy's does
([delivery](runtime-policy.md#delivery)): `rename`, `alsoMount`, `clientParams`.

`Extra` is a bare raw-JSON map rather than `ir.RawConfig`: `RawConfig` names *declared protocol
configuration inside the IR* ([unmodeled](../../ir/unmodeled.md#rawconfig)), and a hint is neither —
it is emitter input authored alongside the SDK, and the IR never sees it.

Method-name *derivation* is itself an injectable policy marked `Inferred`, disable-able and
overridable. Per-target naming/compat overlays are the same shape: an emitter input keyed by IR ID,
so one IR document drives different compat baselines per language.

Hints come in layers, as runtime policy does ([layers](runtime-policy.md#layers-and-operations)):
the hints a document embeds ([embedded policy](embedded-policy.md)), then the project's, then the
ones the project sets for one emitter alone, each field taking the highest layer's value. An ID no
entity of the document has is reported, never ignored.

`AlsoMount` answers an operation a document exposes under two names, which one `Remount` cannot.
Each `Mount` gives the sub-client it names a method calling the operation, named by its `Rename`
where it has one, since an alias commonly carries a name of its own. The method is keyed in the
manifest under the operation's ID with the group beside it, as a declaration derived from an entity
is ([write / integrate](../write-integrate.md)), so the surface differ sees one operation reached
two ways. A `Mount` naming the group the operation is placed in, after any `Remount`, or a group
another `Mount` of it names, is reported and read once. `Remount` and each `Mount` name a group of
the operation's own service, whose client holds the servers, auth and settings its calls need; a
group of another service is reported and configures nothing. Under a split by group, an operation
mounted in another group counts as that group's for placement ([output
units](../refine/output-units.md)).
