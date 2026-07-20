# Implementation decisions — resolved facts (read before any task)

This file resolves every *verify at impl time* / *decision procedure* marker in
`2026-07-20-ir-and-openapi-frontend.md`. These were verified against the actually-vendored
dependencies on this machine. Where the plan says "verify" or shows a placeholder, use the
value here — it is not a guess.

## Environment (confirmed present)

- Go: `go1.26.4` installed; `go.mod` declares `go 1.26.3` (floor). Both fine.
- `golangci-lint` 2.12.2 (v2 config format — the plan's `.golangci.yml` is correct).
- `github.com/speakeasy-api/openapi` **v1.24.0** (pinned in go.mod).
- `github.com/stretchr/testify` and `github.com/google/go-cmp` fetched.

## The YAML module — `gopkg.in/yaml.v3`

Confirmed: `github.com/speakeasy-api/openapi/extensions` and `.../jsonschema/oas3` both import
`gopkg.in/yaml.v3`. `values.Value = *yaml.Node` and `extensions.Extension = *yaml.Node`, both
from `gopkg.in/yaml.v3`. There is **no** speakeasy yaml fork in the graph.

Therefore, everywhere the plan writes `<yaml module>` or "the module from the decision
procedure", use:

```go
import yaml "gopkg.in/yaml.v3"
```

Add it to the Task 18 architecture-test allowlist for `frontend/openapi` and `engine`:
`"gopkg.in/yaml.v3"`. It is already an indirect dependency; after the first frontend code
imports it, run `go mod tidy` so it becomes a direct require.

`*yaml.Node` fields you will use in `valueFromNode`: `.Kind` (`yaml.ScalarNode`,
`yaml.SequenceNode`, `yaml.MappingNode`, `yaml.AliasNode`, `yaml.DocumentNode`), `.Tag`
(`!!null`, `!!bool`, `!!str`, `!!int`, `!!float`, `!!binary`), `.Value` (the raw literal
string — full precision, this is the no-float64 escape), `.Content` (children; mapping nodes
are flat `[k0,v0,k1,v1,...]`), `.Alias` (target of an alias node).

## The numeric-precision trap — CONFIRMED REAL

`oas3.Schema.Minimum`, `.Maximum`, `.MultipleOf` are `*float64`. NEVER lower a numeric
constraint or default from these fields. Read the raw node instead:
`schema.GetPropertyNode("minimum")` returns the `*yaml.Node`; `node.Value` is the exact
decimal string; feed it to `ir.NewBigVal(node.Value)`. Same for `default`, `enum`, `const`
(the latter three are already `values.Value = *yaml.Node`, so read `.Value` directly).
`MaxLength/MinLength/MaxItems/MinItems/MaxProperties/MinProperties` are `*int64` — safe to use
directly. `UniqueItems *bool` — safe.

## Confirmed speakeasy signatures (v1.24.0)

- `openapi.Unmarshal(ctx context.Context, doc io.Reader, opts ...Option[UnmarshalOptions]) (*OpenAPI, []error, error)`.
  Middle return = validation errors (already run unless `WithSkipValidation()`).
- `(*OpenAPI).ResolveAllReferences(ctx, ResolveAllOptions) ([]error, error)`;
  `ResolveAllOptions{OpenAPILocation string; DisableExternalRefs bool; VirtualFS
  system.VirtualFS; HTTPClient system.Client}`. Pass `OpenAPILocation: src.Path`; set
  `DisableExternalRefs: opts.DisableExternalRefs`.
- `validation.Error{UnderlyingError error; Node *yaml.Node; Severity Severity; Rule string;
  Fix Fix; DocumentLocation string}` with methods `Error()`, `GetLineNumber() int`,
  `GetColumnNumber() int`, `GetDocumentLocation() string`. Extract with
  `errors.As(err, &verr)` where `var verr validation.Error` (value type, NOT pointer — its
  methods are on the value receiver; try `*validation.Error` first, fall back to value if
  `errors.As` fails — confirm with a quick test).
- `validation.Severity` string values: `"error"`, `"warning"`, `"hint"` → map to
  `ir.SeverityError`/`SeverityWarning`/`SeverityInfo`.
- `marshaller.Model[T]` (embedded in every high-level type): `GetCore() *T`,
  `GetRootNode() *yaml.Node`, `GetPropertyNode(prop string) *yaml.Node`,
  `GetRootNodeLine() int`, `GetRootNodeColumn() int`, `GetPropertyLine(prop string) int`.
- `marshaller.CoreModel.GetJSONPointer(topLevelRootNode *yaml.Node) string` — available if you
  want a real JSON pointer for provenance; the plan's `line:col` form via
  `GetRootNodeLine/Column` is simpler and sufficient for milestone 1. Use `line:col`.

## Schema model access (confirmed)

- `(*Schema).GetType() []SchemaType` normalizes single/array `type` to a slice — use it for
  nullability detection (contains `"null"`) and type dispatch. `SchemaType` is a string
  (`"object"`,`"array"`,`"string"`,`"number"`,`"integer"`,`"boolean"`,`"null"`).
- `Nullable *bool` (OAS 3.0) preserved as-is — check both it and the 3.1 type-array `"null"`.
- `Properties`, `PatternProperties`, `DependentSchemas`, `Defs` are
  `*sequencedmap.Map[string, *JSONSchema[Referenceable]]` — iterate `.All()` for source order.
- `AllOf/OneOf/AnyOf []*JSONSchema[Referenceable]`; `Items`, `Not`, `If/Then/Else`, `Contains`,
  `AdditionalProperties`, `UnevaluatedProperties` are `*JSONSchema[Referenceable]`.
- `ExclusiveMinimum`/`ExclusiveMaximum` are either-value types (bool arm for 3.0, number arm
  for 2020-12). Read via their `Get*` accessors; when the number arm is set, read the raw node
  for the bound value (float64 trap applies) — or simpler: when exclusive is a bool `true`,
  set the exclusive flag on the corresponding Min/Max already read from `minimum`/`maximum`;
  when exclusive carries a number, that number IS the bound — read it from the raw
  `exclusiveMinimum`/`exclusiveMaximum` node and set the flag. `go doc
  github.com/speakeasy-api/openapi/jsonschema/oas3.ExclusiveMinimum` for the exact accessors.

## `JSONSchema[Referenceable]` ref handling (confirmed shape)

`type JSONSchema[T] struct { values.EitherValue[Schema, core.Schema, bool, bool] }`.
Accessors: `IsSchema()/GetSchema() *Schema` (left), `IsBool()/GetBool() *bool` (right =
boolean schema). Reference detection on the schema: `(*Schema).Ref *references.Reference`
(non-nil ⇒ `$ref`); the schema also exposes `GetRef()`. For resolution use the
`ResolveAllReferences` whole-doc call in `load` (Task 9), then read resolved targets via the
already-populated model. Do NOT hand-resolve per node in milestone 1 — whole-doc resolve is
simpler and the library caches it.

## 3.2-only surface — decision outcomes

The library targets 3.2.0, so probe each with `go doc` before lowering; when the field exists,
lower per the ir-design §14 row; when it does not, preserve the raw node under the namespaced
`Extensions` key the plan names + an `info` diagnostic. Confirmed present enough to rely on:
`additionalOperations`, `Webhooks`, `Server.name`. Probe at point of use: `itemSchema`/
`itemEncoding` (Task 14), `defaultMapping` on Discriminator (Task 11), `oauth2MetadataUrl` and
device flow (Task 15). The fallback (raw + diagnostic) is always correct, so a missing field
never blocks.

## Test helper note

`*yaml.Node` from `yaml.Unmarshal([]byte, &node)` yields a `DocumentNode` whose single
`.Content[0]` is the root value node — that is what schema fields and `valueFromNode` expect.
The plan's `yamlNode` helper (Task 10) already encodes this.
