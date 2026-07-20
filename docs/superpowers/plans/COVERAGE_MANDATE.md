# Coverage mandate — 100%, all edge cases, no exceptions

User directive (2026-07-20): **100% test coverage across the whole module**, covering all edge
cases with no exceptions. OpenAPI specs may be sourced from the web or invented as test
material. This is a hard acceptance criterion for the PR, enforced by a CI gate.

## Definition of done

- `go test ./... -covermode=atomic -coverprofile=coverage.out` then
  `go tool cover -func=coverage.out` reports **100.0%** total AND 100.0% for every package.
- No `_ = x` coverage cheats, no `//go:nocover`-style exclusions, no untested files excluded
  from the profile. Every package that ships code ships tests that exercise every line and
  every branch.
- Genuinely unreachable defensive code is NOT left uncovered: either make it reachable with a
  crafted input / injected failure (preferred — malformed YAML, failing io.Writer, a
  fault-injecting interface), or remove the dead branch. Dead code is a defect, not an
  exception. The one acceptable structural technique is dependency injection of a failing
  collaborator (e.g. an io.Writer that returns an error) to hit I/O error branches.

## Approach (dedicated coverage phase, after all implementation lands)

Run per package, in dependency order (ir → frontend → pass → engine → cmd):

1. Measure: `go test ./<pkg>/... -coverprofile=cover.out` then
   `go tool cover -func=cover.out` and `go tool cover -html` inspection to list every
   uncovered line/branch by file:line.
2. For each uncovered site, add a targeted test that drives exactly that path — a table row,
   a malformed-input case, an injected failure, a diagnostic-emitting spec. Prefer extending
   existing `*_test.go`; add `*_edgecases_test.go` where a file grows unwieldy.
3. Re-measure; iterate until the package is 100.0%.

## Test material — OpenAPI corpus

Mix of invented focused specs (precise coverage control — each targets one code path or
diagnostic) and a few small well-known real specs for realism. Store under
`testdata/openapi/`. Edge cases that MUST be covered (non-exhaustive — the coverage tool is
the source of truth for completeness):

- Every diagnostic code the frontend can emit (unsupported-version, unresolved-ref,
  validation error passthrough, false-schema, validation-only-keyword for not/if-then-else/
  dependentSchemas/contains/unevaluated*, numeric-precision on malformed literals,
  degraded-construct for heterogeneous enums, non-required-body, itemSchema-not-exposed, etc.).
- Every schema shape: all primitive type+format pairs (incl. unknown formats → Scalar),
  nullable in both 3.0 and 3.1 spellings, all four required×nullable states, const, every
  enum kind (string/numeric/heterogeneous), allOf (sole-ref base / multi-ref mixins /
  inline-merge / discriminator hierarchy), oneOf/anyOf (untagged / discriminated / null-variant
  collapse / all-string-const), tuples/prefixItems, list with constraints, map variants
  (additionalProperties false/schema, patternProperties multi, unevaluatedProperties false),
  recursive + mutually-recursive + diamond schemas, deeply nested (near the depth cap and a
  spec that EXCEEDS maxSchemaDepth to hit the cap diagnostic), boolean schemas true and false.
- Every value kind through valueFromNode: null, bool, string, int, float, big-int precision,
  big-decimal precision, binary, sequence, mapping (order preserved), alias nodes, and a
  nesting that exceeds maxValueDepth.
- Operations: tag grouping and path-prefix grouping, untagged default group, no-operationId
  hint, every HTTP method, shared path-item params with override, all response status forms
  (2xx, 4XX/5XX ranges, explicit codes, default catch-all), error fault classification,
  security:[] vs absent, webhooks, callbacks, links, multiple bindings.
- Parameters: path/query/header/cookie, style/explode defaults materialized for each location,
  deepObject, content-style params, constraints on params.
- Content: single and multiple media types, multipart with per-part encoding (contentType,
  headers, style, array→Multi, binary→Filename), octet-stream→FileInfo, non-required body.
- Auth: apiKey (header/query/cookie), http basic/bearer/other-scheme, oauth2 all four flows,
  openIdConnect, mutualTLS; OR-of-ANDs with empty option; per-op override.
- Servers with variables (incl. enum), info contact/license/termsOfService, document-level
  x-* extensions.
- Malformed / error inputs: unparseable YAML, wrong FormatOptions type, zero and multiple
  sources to Parse, missing file (engine), swagger 2.0 (engine sniff error), unrecognized
  format, external $ref with DisableExternalRefs.
- CLI: every subcommand and flag, every exit code (0/1/2), -o file vs stdout, --fail-on
  error/warning/invalid, --skip-validate, unknown command, no args.

## CI gate

Add `scripts/check-coverage.sh` (runs the profile, greps `total:` and every per-package line,
fails if any < 100.0%) and wire it into `.github/workflows/gate.yml` as a required step.
