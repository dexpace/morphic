# OpenAPI matrix witnesses

The rows of [`ir-spec-matrix.md`](../../../docs/ir-spec-matrix.md) that its `openapi` column marks
expressible, each with the specs in this directory that witness it, named by file name without the
extension, or the reason none does yet. A reason names what blocks a spec and what retires it. A
test holds the entries to the column and the names to the directory. Whether a spec exercises the
row it names is for a reviewer to judge: touching a construct on the way to another subject is not
witnessing it.

### `named-objects`

- Witnesses: `named-types`, `component-reuse`, `shared-response-across-status`, `ref-site-docs`

### `inline-anonymous`

- Witnesses: `inline-types`, `component-reuse`, `shared-response-across-status`,
  `inline-hoist-positions`, `inline-annotations`, `inline-residue`

### `inheritance`

- Witnesses: `allof-inheritance`, `discriminator-inheritance`, `discriminator-inline-subtype`,
  `discriminator-transitive`, `discriminated-union-beside-model`, `required-members`

### `tagged-unions`

- Witnesses: `oneof-discriminated`, `anyof-discriminated`, `discriminator-inheritance`,
  `discriminator-inline-subtype`, `discriminator-default-mapping`, `discriminator-transitive`,
  `discriminator-alias-mapping`, `discriminated-union-beside-model`, `nullable-union-discriminator`

### `untagged-unions`

- Witnesses: `allof-oneof-cooccurrence`, `allof-ref-branch-siblings`, `union-beside-ref`,
  `anyof-untagged`, `union-decorated-branch`

### `intersection`

- Witnesses: `allof-mixins`, `allof-inline-merge`, `allof-required-only`,
  `allof-oneof-cooccurrence`, `allof-inline-residue`, `allof-nested-inline`,
  `allof-ref-branch-siblings`, `allof-boolean-branch`, `allof-narrowing-redeclaration`,
  `allof-redeclared-models`, `allof-position-constraints`, `codeclared-keywords`

### `negation`

- Witnesses: `negation-not`

### `enums-string`

- Witnesses: `codeclared-keywords`, `enum-string`, `nullable-enum-31`, `enum-member-identity`

### `enums-numeric`

- Witnesses: `enum-numeric`, `enum-member-identity`

### `open-enums`

- Witnesses: `extension-promotion`

### `custom-scalars`

- Witnesses: `scalar-format`

### `encoding-hints`

- Witnesses: `encoding-byte`, `content-vocabulary`

### `wire-name-distinct`

- Witnesses: `neutral-naming`, `empty-names`

### `optionality-vs-nullability`

- Witnesses: `nullable-union-discriminator`, `nullability-four-states`, `nullable-30`,
  `nullable-31-ref`, `nullable-enum-31`, `nullability-conjunction`, `null-only-union`,
  `nullable-keyword-30`, `nullable-keyword-31`

### `defaults`

- Witnesses: `defaults`, `yaml-timestamp-scalars`, `numeric-precision`, `param-ref-inheritance`

### `constraints`

- Witnesses: `constraints`, `numeric-precision`

### `visibility`

- Witnesses: `readonly-writeonly`

### `redaction`

- Witnesses: `secret-format`

### `recursive-types`

- Witnesses: `recursive`

### `maps`

- Witnesses: `maps`, `required-members`

### `tuples`

- Witnesses: `tuples-prefixitems`

### `tuple-rest`

- Witnesses: `tuples-prefixitems`

### `literal-types`

- Witnesses: `codeclared-keywords`, `yaml-timestamp-scalars`, `numeric-precision`, `literal-const`

### `operation-grouping`

- Witnesses: `tags-grouping`, `group-identity`, `tags-grouping-32`

### `http-binding`

- Witnesses: `param-identity`, `http-binding`, `path-item-operations`

### `param-styles`

- Witnesses: `param-styles`, `param-style-matrix`

### `multi-content`

- Witnesses: `multi-content`, `param-content-fields`

### `multipart-encoding`

- Witnesses: `multipart-encoding`, `media-type-spelling`, `nested-encoding-32`

### `per-status-errors`

- Witnesses: `per-status-errors`, `default-response`

### `streaming-server`

- Witnesses: `sequential-media`, `streaming-media-30`, `streaming-media-31`

### `streaming-client`

- Witnesses: `streaming-media-31`

### `events-channels`

- Witnesses: `webhooks`

### `callbacks`

- Witnesses: `callbacks`

### `pagination`

- Uncovered: OpenAPI states it only through links and x-*, and this compiler keeps both verbatim
  rather than reading either into ir.Pagination — response-links pins that they survive. Invariant
  6 puts the inference in a pass rather than in the compiler, so this retires when such a pass lands
  and a spec can read ir.Pagination back

### `long-running-operations`

- Uncovered: OpenAPI states it only through vendor extensions, so a spec for this row would assert
  what extensions-x already asserts — that x-* survives. ir.LongRunning models the capability; the
  missing half is a pass reading those extensions into it, and this retires when one lands

### `idempotency`

- Uncovered: OpenAPI conveys it through HTTP verb semantics alone, and the method string
  http-binding pins is the whole of what a spec could read. ir.Idempotency models the capability,
  and a pass inferring safe/idempotent from that method is what would let a spec witness the row;
  this retires when one lands

### `auth-schemes`

- Witnesses: `security-schemes`, `oauth-flow-fields`

### `per-op-auth`

- Witnesses: `security-or-and`

### `servers`

- Witnesses: `servers-variables`

### `per-op-servers`

- Witnesses: `webhooks`, `callbacks`, `path-item-ref-siblings`, `yaml-merge-keys`

### `deprecation`

- Witnesses: `param-ref-inheritance`, `deprecation`, `extension-promotion`

### `stability`

- Uncovered: no spec witnesses it until the compiler reads a stability extension into `Stability`; a
  conformance case of that rule retires this.

### `examples`

- Witnesses: `examples`, `examples-32`

### `docs-summary-description`

- Witnesses: `param-ref-inheritance`, `path-item-docs`, `docs-summary-desc`, `request-body-docs`,
  `ref-site-docs`, `response-summary-32`

### `vendor-extensions`

- Witnesses: `extensions-x`, `inline-annotations`

### `positional-encoding`

- Witnesses: `tuples-prefixitems`

### `server-initiated-messages`

- Witnesses: `webhooks`
