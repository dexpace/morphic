# Conformance Grid and Resolver Extraction — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the (format × aspect × site-kind) conformance grid that makes an unhandled annotation
position a visible empty cell, then extract the OpenAPI compiler's eleven reference-resolution
functions into a named resolver component that produces sites.

**Architecture:** The grid is test-only vocabulary (`Aspect`, `SiteKind`, `Cell`) in
`internal/harness`, consumed by a table-driven per-format grid test in `compilers/openapi`. Nothing
in the production write path references it and `ir.Diagnostic` gains no field. The resolver
extraction is behaviour-neutral: a `site` value type, a pure file move, then rewiring so the resolver
produces sites instead of each call site deriving its own.

**Tech Stack:** Go 1.26, `testify` (`require` for preconditions, `assert` for values),
`go-cmp` for diffs, `speakeasy-api/openapi` for OpenAPI parsing, `gopkg.in/yaml.v3`.

## Global Constraints

- Every task ends green on all four gates: `gofmt -l .` prints nothing, `golangci-lint run` passes,
  `go vet ./...` passes, `go test ./...` passes.
- Coverage stays at 100%.
- Functions: 70-line hard cap, aim 20–40.
- GoDoc on every exported symbol, starting with the symbol's name, complete sentences.
- Comments explain *why*, not *what*. Keep them short.
- Tests: table-driven and flat, `TestFunc_Scenario` names, `t.Parallel()`, `t.Helper()` in helpers.
- External test packages (`package foo_test`) preferred.
- Imports in three `gci` groups: stdlib, external, local.
- Conventional Commits: `type(scope): subject`, imperative, ≤72 chars, no trailing period.
- **Never** add a `Co-Authored-By` trailer or mention any AI assistant in commits or PR text.
- No IR schema change. No `docs/ir-design.md` change.
- Golden corpus output stays byte-identical for every task in this plan. Any task that changes
  golden output has a bug — stop and investigate rather than regenerating goldens.

## Existing context the implementer needs

- `compilers/openapi/conformance_test.go` is `package openapi_test` and already defines helpers your
  new test file can use directly: `namedID(name string) ir.TypeID`,
  `propByWire(m *ir.Model, wire string) (ir.Property, bool)`,
  `propsByWire(props []ir.Property) map[string]ir.Property`, `parseCorpus`, `assertNoErrorDiags`.
  Do not redefine them.
- `compilers/openapi/helpers_test.go` is `package openapi` (internal). It has its own `propsByWire`.
  The two do not collide because the packages differ. Your new files go in `openapi_test`.
- `internal/archtest/arch_test.go` skips `_test.go` files entirely, so a test file in
  `compilers/openapi` may import `internal/harness` without an allowlist change.
- `internal/harness` is intentionally absent from the archtest `rules` map and is never walked.
- OpenAPI 3.0 ignores keywords written beside a `$ref`; 3.1 allows them. Every reference-site cell in
  this plan uses `openapi: 3.1.0` for that reason.

## File Structure

**Create:**
- `internal/harness/grid.go` — `Aspect`, `SiteKind`, `Cell`, `Aspects`, `SiteKinds`, `Cells`,
  `MissingCells`. Test-only vocabulary; no compiler dependency.
- `internal/harness/grid_test.go` — unit tests for the vocabulary and `MissingCells`.
- `compilers/openapi/grid_test.go` — the OpenAPI grid: one case per cell, plus the
  every-cell-covered-or-excused assertion.
- `compilers/openapi/resolve.go` — the resolver: the eleven reference-resolution functions plus the
  `site` type and its constructor.

**Modify:**
- `compilers/openapi/schema.go` — remove the seven resolver functions moved to `resolve.go`.
- `compilers/openapi/ids.go` — remove the four resolver functions moved to `resolve.go`.

---

### Task 1: Grid vocabulary in `internal/harness`

**Files:**
- Create: `internal/harness/grid.go`
- Test: `internal/harness/grid_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `harness.Aspect` (string), `harness.SiteKind` (string), `harness.Cell` struct with
  fields `Aspect Aspect` and `SiteKind SiteKind`, `harness.Aspects() []Aspect`,
  `harness.SiteKinds() []SiteKind`, `harness.Cells() []Cell`,
  `harness.MissingCells(covered []Cell) []Cell`.

- [ ] **Step 1: Write the failing test**

Create `internal/harness/grid_test.go`:

```go
package harness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
)

func TestCells_IsFullCrossProduct(t *testing.T) {
	t.Parallel()
	cells := harness.Cells()
	assert.Len(t, cells, len(harness.Aspects())*len(harness.SiteKinds()))

	seen := make(map[harness.Cell]bool, len(cells))
	for _, c := range cells {
		require.False(t, seen[c], "duplicate cell %+v", c)
		seen[c] = true
	}
}

func TestMissingCells_ReportsUncoveredInStableOrder(t *testing.T) {
	t.Parallel()
	all := harness.Cells()
	require.Greater(t, len(all), 2)

	covered := all[2:]
	missing := harness.MissingCells(covered)
	assert.Equal(t, all[:2], missing)
}

func TestMissingCells_EmptyWhenFullyCovered(t *testing.T) {
	t.Parallel()
	assert.Empty(t, harness.MissingCells(harness.Cells()))
}

func TestMissingCells_IgnoresUnknownCells(t *testing.T) {
	t.Parallel()
	covered := append(harness.Cells(), harness.Cell{Aspect: "invented", SiteKind: "nowhere"})
	assert.Empty(t, harness.MissingCells(covered))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/harness/ -run 'TestCells|TestMissingCells' -v`
Expected: FAIL — build error, `undefined: harness.Cells`.

- [ ] **Step 3: Write the implementation**

Create `internal/harness/grid.go`:

```go
package harness

// Aspect names one annotation slot the IR offers at a site. It is test
// vocabulary: nothing in the compiler's write path refers to these values, and
// ir.Diagnostic carries no aspect field. Aspects form one axis of the
// conformance grid.
type Aspect string

// The annotation slots the IR offers. Adding a slot here widens the grid, which
// is the intended way to discover that a format handles it nowhere.
const (
	AspectDocs           Aspect = "docs"
	AspectExamples       Aspect = "examples"
	AspectConstraints    Aspect = "constraints"
	AspectDefault        Aspect = "default"
	AspectDeprecated     Aspect = "deprecated"
	AspectVisibility     Aspect = "visibility"
	AspectExtensions     Aspect = "extensions"
	AspectXMLHints       Aspect = "xmlHints"
	AspectValidationOnly Aspect = "validationOnly"
)

// SiteKind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own. The distinction
// is the grid's second axis because it is where annotations are most often
// dropped: an annotation written beside a reference belongs to the position it
// is written at, not to the referent.
type SiteKind string

// The kinds of position an annotation can be written at.
const (
	SiteDeclaration SiteKind = "declaration"
	SiteReference   SiteKind = "reference"
)

// Cell identifies one square of the conformance grid: one annotation slot at
// one kind of position.
type Cell struct {
	Aspect   Aspect   `json:"aspect"`
	SiteKind SiteKind `json:"siteKind"`
}

// Aspects returns every aspect in a stable order.
func Aspects() []Aspect {
	return []Aspect{
		AspectDocs,
		AspectExamples,
		AspectConstraints,
		AspectDefault,
		AspectDeprecated,
		AspectVisibility,
		AspectExtensions,
		AspectXMLHints,
		AspectValidationOnly,
	}
}

// SiteKinds returns every site kind in a stable order.
func SiteKinds() []SiteKind {
	return []SiteKind{SiteDeclaration, SiteReference}
}

// Cells returns the full aspect-by-site-kind cross product in a stable order,
// aspect-major. This is the grid a format must either cover or explicitly
// excuse.
func Cells() []Cell {
	aspects, kinds := Aspects(), SiteKinds()
	out := make([]Cell, 0, len(aspects)*len(kinds))
	for _, a := range aspects {
		for _, k := range kinds {
			out = append(out, Cell{Aspect: a, SiteKind: k})
		}
	}
	return out
}

// MissingCells returns the grid cells that covered does not include, in Cells
// order. Entries in covered that are not grid cells are ignored, so a caller
// cannot accidentally satisfy the grid with a typo.
func MissingCells(covered []Cell) []Cell {
	have := make(map[Cell]bool, len(covered))
	for _, c := range covered {
		have[c] = true
	}
	var out []Cell
	for _, c := range Cells() {
		if !have[c] {
			out = append(out, c)
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/harness/ -run 'TestCells|TestMissingCells' -v`
Expected: PASS, four tests.

- [ ] **Step 5: Run the full gates**

```bash
gofmt -l .
golangci-lint run
go vet ./...
go test ./...
```
Expected: `gofmt` prints nothing; the rest pass.

- [ ] **Step 6: Commit**

```bash
git add internal/harness/grid.go internal/harness/grid_test.go
git commit -m "test(harness): add aspect and site-kind conformance grid vocabulary"
```

---

### Task 2: OpenAPI grid scaffold with the two reference-site example cells

These two cells are where the sub-schema `$ref`-sibling defect lived. They should pass on current
`main`. If either fails, stop — that is a regression, not a grid finding.

**Files:**
- Create: `compilers/openapi/grid_test.go`

**Interfaces:**
- Consumes: `harness.Aspect`, `harness.SiteKind`, `harness.Cell` from Task 1; `namedID` and
  `propByWire` from `conformance_test.go`.
- Produces: `gridCase` struct and the `gridCases()` table, extended by Task 3.

- [ ] **Step 1: Write the failing test**

Create `compilers/openapi/grid_test.go`:

```go
// This file is the OpenAPI half of the (format × aspect × site-kind)
// conformance grid: one case per annotation slot per kind of position, plus an
// assertion that no cell is silently uncovered. It complements
// conformance_test.go, which covers capability rows rather than annotation
// positions.
package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// gridCase is one cell of the conformance grid: a minimal spec exercising one
// annotation slot at one kind of position, and the assertion that the
// annotation survived to the right place in the IR.
type gridCase struct {
	cell   harness.Cell
	spec   string
	assert func(*testing.T, *ir.Document)
}

// compileGrid compiles one in-memory grid spec and fails on any error
// diagnostic, since a grid spec is always well-formed by construction.
func compileGrid(t *testing.T, name, spec string) *ir.Document {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	return doc
}

func TestGrid(t *testing.T) {
	t.Parallel()
	for _, tc := range gridCases() {
		name := string(tc.cell.Aspect) + "/" + string(tc.cell.SiteKind)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, compileGrid(t, name, tc.spec))
		})
	}
}

func gridCases() []gridCase {
	return []gridCase{
		{
			cell: harness.Cell{Aspect: harness.AspectExamples, SiteKind: harness.SiteDeclaration},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      example: {f: at-declaration}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok, "component S must own a node")
				require.Len(t, td.Common().Examples, 1)
			},
		},
		{
			cell: harness.Cell{Aspect: harness.AspectExamples, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          example: at-reference
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)

				// The example belongs to the position it is written at.
				require.Len(t, p.Examples, 1, "example beside a $ref belongs to the reference site")
				require.NotNil(t, p.Examples[0].Value)
				assert.Equal(t, "at-reference", p.Examples[0].Value.Str)

				// ...and must not have leaked onto the referent.
				target, ok := doc.Types[namedID("Target")]
				require.True(t, ok)
				assert.Empty(t, target.Common().Examples,
					"a reference-site example must not attach to the referent")
			},
		},
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./compilers/openapi/ -run TestGrid -v`
Expected: both subtests PASS. If `examples/reference` fails, stop and investigate — that cell was
fixed and must not have regressed.

- [ ] **Step 3: Run the full gates**

```bash
gofmt -l .
golangci-lint run
go vet ./...
go test ./...
```
Expected: all pass, golden corpus unchanged.

- [ ] **Step 4: Commit**

```bash
git add compilers/openapi/grid_test.go
git commit -m "test(compilers/openapi): add conformance grid with example-annotation cells"
```

---

### Task 3: Fill the remaining grid cells

**This task measures which annotation slots survive at which kind of position. It does not change
compiler behaviour.**

Every cell must end green, per the Global Constraints. A cell has exactly two shapes:

- The annotation survives → assert its presence.
- The annotation is dropped → mark the case `knownGap` (Step 3) and assert its **absence**, so the
  cell passes today and turns red the moment someone closes the gap.

A cell you cannot make pass either way is a finding to escalate, not a red test to leave behind. The
deliverable is the `knownGap` list, not a failing suite. Do not fix compiler behaviour here, and do
not add IR fields.

**Files:**
- Modify: `compilers/openapi/grid_test.go` (extend `gridCases()`)

**Interfaces:**
- Consumes: `gridCase`, `compileGrid`, `gridCases` from Task 2.
- Produces: a complete `gridCases()` table and the `excusedCells()` list.

- [ ] **Step 1: Add the constraints cells**

Append to the `gridCases()` slice, before the closing `}`:

```go
		{
			cell: harness.Cell{Aspect: harness.AspectConstraints, SiteKind: harness.SiteDeclaration},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: integer, minimum: 5}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a constrained scalar component owns a Scalar node")
				require.NotNil(t, sc.Constraints)
				require.NotNil(t, sc.Constraints.Min)
				assert.Equal(t, "5", sc.Constraints.Min.String())
			},
		},
		{
			cell: harness.Cell{Aspect: harness.AspectConstraints, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: integer}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          minimum: 5
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				require.NotNil(t, p.Constraints, "a bound beside a $ref binds the reference site")
				require.NotNil(t, p.Constraints.Min)
				assert.Equal(t, "5", p.Constraints.Min.String())
			},
		},
```

- [ ] **Step 2: Run and record**

Run: `go test ./compilers/openapi/ -run TestGrid -v`

Record each subtest as PASS or FAIL in a scratch note. Do not fix failures.

- [ ] **Step 3: Add the known-gap mechanism**

Two declaration-site cells have no IR field to land in, so they cannot be written as passing
assertions. They are compiler gaps, not format limitations, so per the spec they do not belong in
`excusedCells`. Mark them instead, following the `knownInvalid` pattern already used in
`internal/harness/corpus_test.go`.

Add a field to `gridCase` (additive — Task 2's cases keep compiling):

```go
	// knownGap, when non-empty, records that the compiler currently drops this
	// annotation at this kind of position and why there is nowhere for it to go.
	// The case then asserts the annotation is ABSENT, so closing the gap turns
	// the cell red and whoever closes it removes the marker.
	knownGap string
```

Leave `TestGrid` itself untouched — Step 7 rewrites its body, and duplicating gap logging in both
places would conflict. Gaps surface through their own listing test instead, so they stay visible
rather than buried in subtest output:

```go
func TestGrid_KnownGapsAreListed(t *testing.T) {
	t.Parallel()
	for _, tc := range gridCases() {
		if tc.knownGap != "" {
			t.Logf("GAP %s/%s: %s", tc.cell.Aspect, tc.cell.SiteKind, tc.knownGap)
		}
	}
}
```

- [ ] **Step 4: Add the docs and deprecated cells**

Both have a `TypeCommon` home and a `Property` home, so both cells assert presence. Same two-cell
shape as the constraints cases in Step 1: the declaration cell writes the keyword on component `S`;
the reference cell writes it beside a `$ref` on property `f` of `S` and asserts it landed on the
property, not on `Target`.

| aspect | keyword | declaration assertion | reference assertion |
|---|---|---|---|
| `AspectDocs` | `description: at-declaration` / `at-reference` | `doc.Types[namedID("S")].Common().Docs.Description == "at-declaration"` | `p.Docs.Description == "at-reference"` |
| `AspectDeprecated` | `deprecated: true` | `doc.Types[namedID("S")].Common().Deprecation` non-nil | `p.Deprecation` non-nil |

- [ ] **Step 5: Add the default and visibility cells**

These field names are verified against `ir/` — do not substitute guesses:

- `ir.Property.Default` is `*ir.Value`; `ir.Value.Num` is a `BigVal` (a string type with `String()`).
- `ir.Property.Visibility` is `ir.Visibility`, a **struct** `{Only []Lifecycle; None bool}` — not an
  enum. There is no `ir.VisibilityReadOnly`. `readOnly: true` maps to
  `ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}`
  (see `effectiveVisibility`, `schema.go:1330`).
- `ir.Scalar` has fields `Base`, `Constraints`, `Encoding` only — **no** `Default`.
- `ir.TypeCommon` has **no** `Visibility` field. It has `Access string` and `Usage UsageFlags`, and
  the OpenAPI compiler never assigns either.

Reference cells (assert presence):

```go
		{
			cell: harness.Cell{Aspect: harness.AspectDefault, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: integer}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          default: 7
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				require.NotNil(t, p.Default, "a default beside a $ref binds the reference site")
				assert.Equal(t, "7", p.Default.Num.String())
			},
		},
		{
			cell: harness.Cell{Aspect: harness.AspectVisibility, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          readOnly: true
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				assert.Equal(t,
					[]ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery},
					p.Visibility.Only)
			},
		},
```

Declaration cells (assert the gap):

```go
		{
			cell:     harness.Cell{Aspect: harness.AspectDefault, SiteKind: harness.SiteDeclaration},
			knownGap: "ir.Scalar and ir.TypeCommon have no Default field, so a component-level default has nowhere to land",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: integer, default: 7}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok, "S still owns a node even though its default is dropped")
				_ = td
			},
		},
		{
			cell:     harness.Cell{Aspect: harness.AspectVisibility, SiteKind: harness.SiteDeclaration},
			knownGap: "ir.TypeCommon has no Visibility field; Access and Usage exist but the compiler never sets them",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, readOnly: true}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok)
				assert.Empty(t, td.Common().Access, "Access is never set today; closing this gap should turn this red")
			},
		},
```

Both gaps are findings for the PR description, not work for this plan. Do not add IR fields — that
would be an `ir-design.md` change, which the Global Constraints forbid.

- [ ] **Step 6: Run and record**

Run: `go test ./compilers/openapi/ -run TestGrid -v`
Record PASS/FAIL per subtest.

- [ ] **Step 7: Add the extensions, xmlHints, and validationOnly cells**

Same two-cell shape. Verified field homes: `ir.TypeCommon.Extensions` and `ir.Property.Extensions`
for extensions; `ir.TypeCommon.XML` and `ir.Property.XML`, both `*ir.XMLHints`, for XML hints.

Keywords: `x-vendor: at-declaration` / `at-reference` for extensions, `xml: {name: Renamed}` for XML
hints, `if: {type: string}` plus `then: {minLength: 1}` for validation-only.

Validation-only needs the diagnostics as well as the document, so extend `gridCase` with an optional
field rather than changing the existing `assert` signature — Task 2's cases must keep compiling:

```go
	// assertDiags, when set, additionally checks the compile's diagnostics.
	assertDiags func(*testing.T, []ir.Diagnostic)
```

`compileGrid` currently discards diagnostics. Change it to return them and update its two existing
call sites:

```go
func compileGrid(t *testing.T, name, spec string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}
```

Note this drops the `assertNoErrorDiags` call that was inside `compileGrid`. Move it into `TestGrid`
so every cell still rejects error diagnostics:

```go
			doc, diags := compileGrid(t, name, tc.spec)
			assertNoErrorDiags(t, diags)
			tc.assert(t, doc)
			if tc.assertDiags != nil {
				tc.assertDiags(t, diags)
			}
```

For the validation-only cells, assert the keyword was preserved verbatim in `Extensions` per
`ir-design.md` §4.7, and that a diagnostic with code `openapi/validation-only-keyword` was emitted.
The code constant is unexported (`codeValidationOnlyKeyword`, `diag.go:32`) and your test is in
`openapi_test`, so compare against the literal string.

- [ ] **Step 8: Add the coverage assertion and excuse list**

Append to `grid_test.go`:

```go
// excusedCells lists grid cells OpenAPI cannot express, with the reason. An
// excuse is a claim about the format, not about the compiler — a cell that the
// format *can* express but the compiler drops belongs in gridCases as a failing
// case, not here.
func excusedCells() []harness.Cell {
	return nil
}

func TestGrid_EveryCellCoveredOrExcused(t *testing.T) {
	t.Parallel()
	var covered []harness.Cell
	for _, tc := range gridCases() {
		covered = append(covered, tc.cell)
	}
	covered = append(covered, excusedCells()...)

	missing := harness.MissingCells(covered)
	assert.Empty(t, missing, "grid cells with neither a case nor an excuse: %+v", missing)
}
```

If a cell genuinely cannot be expressed in OpenAPI, add it to `excusedCells` with a comment giving
the reason. Otherwise write the case, even if it fails.

- [ ] **Step 9: Run the whole grid**

Run: `go test ./compilers/openapi/ -run TestGrid -v`
Expected: every subtest PASSES, including `TestGrid_EveryCellCoveredOrExcused`. A cell that asserts
presence and fails means the annotation is dropped — convert it to a `knownGap` case asserting
absence and record it. A cell you cannot make pass in either shape is an escalation.

- [ ] **Step 10: Run the full gates**

```bash
gofmt -l .
golangci-lint run
go vet ./...
go test ./...
git diff --stat HEAD -- testdata/
```

Expected: all green, `testdata/` diff empty. Per the Global Constraints there are no expected
failures — a dropped annotation is recorded as a `knownGap`, never left as a red test.

- [ ] **Step 11: Commit**

```bash
git add compilers/openapi/grid_test.go
git commit -m "test(compilers/openapi): complete the annotation conformance grid"
```

- [ ] **Step 12: Report the findings**

Write the PASS/FAIL table into the PR description. **This table is the decision input for whether the
facet extraction happens at all** — the spec records that a large number of failing reference-site
cells argues for it and a small number argues against. Do not skip this step.

---

### Task 4: The `site` type

**Files:**
- Create: `compilers/openapi/resolve.go`
- Test: `compilers/openapi/resolve_internal_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `site` struct with fields `Pointer string`, `Kind siteKind`, `Node *oas3.Schema`,
  `Referent *oas3.Schema`; constants `siteDeclaration`, `siteReference`; method
  `(l *lowerer) siteAt(js *oas3.JSONSchema[oas3.Referenceable], pointer string) site`.

- [ ] **Step 1: Write the failing test**

Create `compilers/openapi/resolve_internal_test.go`:

```go
package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSiteAt_DeclarationHasNoReferent(t *testing.T) {
	t.Parallel()
	l := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, description: d}
`)
	js := l.doc.Components.GetSchemas().GetOrZero("S")
	require.NotNil(t, js)

	st := l.siteAt(js, ptr("components", "schemas", "S"))
	assert.Equal(t, siteDeclaration, st.Kind)
	require.NotNil(t, st.Node)
	assert.Equal(t, "d", st.Node.GetDescription())
	assert.Nil(t, st.Referent, "a declaration site has no referent")
}

func TestSiteAt_ReferenceCarriesBothNodes(t *testing.T) {
	t.Parallel()
	l := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string, description: target-desc}
    S:
      $ref: '#/components/schemas/Target'
      description: site-desc
`)
	js := l.doc.Components.GetSchemas().GetOrZero("S")
	require.NotNil(t, js)

	st := l.siteAt(js, ptr("components", "schemas", "S"))
	assert.Equal(t, siteReference, st.Kind)
	require.NotNil(t, st.Node)
	assert.Equal(t, "site-desc", st.Node.GetDescription(), "Node is the schema written here")
	require.NotNil(t, st.Referent)
	assert.Equal(t, "target-desc", st.Referent.GetDescription(), "Referent is one hop away")
}
```

`loweredFor` does not exist yet — add it to `compilers/openapi/helpers_test.go` (`package openapi`),
next to the existing `lowerSpec`, which it deliberately mirrors:

```go
// loweredFor loads src and returns the lowerer over it with nothing lowered
// yet, so a test can drive one entry point at a time. lowerSpec is the same
// load path but returns only the document it produced.
func loweredFor(t *testing.T, src string) *lowerer {
	t.Helper()
	loadedDoc, diags, err := load(t.Context(), 0,
		compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	return newLowerer(0, loadedDoc, Options{}.withDefaults())
}
```

Do **not** use `newRawLowerer` (`helpers_test.go:139`) — it takes a hand-constructed
`*soa.OpenAPI` and bypasses the parser deliberately, for exercising nil-entry guards. These tests
need a really-parsed document.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./compilers/openapi/ -run TestSiteAt -v`
Expected: FAIL — build error, `undefined: siteDeclaration`.

- [ ] **Step 3: Write the implementation**

Create `compilers/openapi/resolve.go`:

```go
package openapi

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
)

// siteKind distinguishes a position that declares a type from one that
// references another type and may carry annotations of its own.
type siteKind int

const (
	siteDeclaration siteKind = iota
	siteReference
)

// site is a position that owns an IR node. Node is the schema written at the
// position; Referent is the schema one hop away, set only for a reference site.
//
// The split is what keeps annotations attached where they were written: a
// site-only annotation (examples, constraints) reads Node alone, while a
// site-overrides-referent annotation (docs, deprecation, visibility, default)
// reads Node and falls back to Referent. Resolving this once here is what stops
// each attachment point from re-deciding it.
type site struct {
	Pointer  string
	Kind     siteKind
	Node     *oas3.Schema
	Referent *oas3.Schema
}

// siteAt builds the site for the position at pointer. A position carrying a
// $ref is a reference site and resolves its referent exactly one hop, never to
// the end of the chain: a sub-schema spelled {$ref: Other, minimum: 7} must read
// as this position, not as Other, or the bound written beside the $ref is lost
// before anything can record it.
func (l *lowerer) siteAt(js *oas3.JSONSchema[oas3.Referenceable], pointer string) site {
	st := site{Pointer: pointer, Kind: siteDeclaration, Node: siteSchema(js)}
	if js == nil || js.IsBool() {
		return st
	}
	if !js.IsReference() && (st.Node == nil || st.Node.Ref == nil) {
		return st
	}
	st.Kind = siteReference
	if decl := declaredSchema(js); decl != nil {
		st.Referent = siteSchema(decl)
	}
	return st
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./compilers/openapi/ -run TestSiteAt -v`
Expected: PASS, two tests.

If `TestSiteAt_ReferenceCarriesBothNodes` shows `Referent` as nil, `declaredSchema` returned the
declaration rather than the target for this shape. Read `declaredSchema` (`schema.go:288`) and
`GetReferenceResolutionInfo` before adjusting — do not change `declaredSchema`'s one-hop behaviour,
which is load-bearing for the `$ref`-sibling fix.

- [ ] **Step 5: Run the full gates**

```bash
gofmt -l .
golangci-lint run
go vet ./...
go test ./...
```
Expected: all pass. `site` is not yet used by production code, so golden output cannot have changed.

- [ ] **Step 6: Commit**

```bash
git add compilers/openapi/resolve.go compilers/openapi/resolve_internal_test.go
git commit -m "refactor(compilers/openapi): add site type for reference-position annotations"
```

---

### Task 5: Move the resolver functions into `resolve.go`

Pure relocation. No signature changes, no body changes, no behaviour change.

**Files:**
- Modify: `compilers/openapi/schema.go` (remove seven functions)
- Modify: `compilers/openapi/ids.go` (remove four functions)
- Modify: `compilers/openapi/resolve.go` (receive all eleven)

**Interfaces:**
- Consumes: `site` from Task 4.
- Produces: no new symbols. Every moved function keeps its exact name and signature.

- [ ] **Step 1: Record the baseline**

```bash
go test ./... 2>&1 | tail -5
git rev-parse HEAD
```
Note the commit — you will diff golden output against it in Step 4.

- [ ] **Step 2: Move the seven functions from `schema.go`**

Cut these from `schema.go` and paste them into `resolve.go`, keeping their doc comments verbatim:

- `schemaRef` (line ~125)
- `refTypeRef` (line ~247)
- `resolveSchemaRef` (line ~264)
- `declaredSchema` (line ~288)
- `hoistSubSchema` (line ~309)
- `refNullable` (line ~330)
- `refTargetSchema` (line ~843)

Add any imports `resolve.go` now needs (`ir`, and whatever the moved bodies reference). Remove
imports from `schema.go` that nothing there uses any more — `golangci-lint` will flag leftovers.

- [ ] **Step 3: Move the four functions from `ids.go`**

Cut these from `ids.go` and paste them into `resolve.go`, doc comments verbatim:

- `internalPointer` (line ~107)
- `sameFile` (line ~125)
- `internedID` (line ~139)
- `resolveComponentRef` (line ~160)

- [ ] **Step 4: Verify nothing changed**

```bash
gofmt -l .
golangci-lint run
go vet ./...
go test ./...
git diff --stat HEAD -- testdata/
```

Expected: all gates pass, and `git diff` over `testdata/` is **empty**. A pure move that changes a
golden file means something was altered in transit — revert and redo the move.

- [ ] **Step 5: Commit**

```bash
git add compilers/openapi/resolve.go compilers/openapi/schema.go compilers/openapi/ids.go
git commit -m "refactor(compilers/openapi): move reference resolution into resolve.go"
```

---

### Task 6: Resolver produces sites at the component-schema entry point

Rewire the narrowest call site first — `lowerComponentSchema` — so `site` carries real traffic before
the wider rewiring.

**Files:**
- Modify: `compilers/openapi/schema.go:50` (`lowerComponentSchema`)
- Test: `compilers/openapi/resolve_internal_test.go`

**Interfaces:**
- Consumes: `site`, `siteAt` from Task 4.
- Produces: `lowerComponentSchema` unchanged in signature; its body now derives annotations from a
  `site` rather than from `js` directly.

- [ ] **Step 1: Write the failing test**

Append to `resolve_internal_test.go`:

```go
func TestLowerComponentSchema_RefSiblingConstraintBindsTheSite(t *testing.T) {
	t.Parallel()
	l := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: integer}
    S:
      $ref: '#/components/schemas/Target'
      minimum: 5
`)
	l.lowerComponentSchemas()

	sc, ok := typeByName(l.out, "S").(*ir.Scalar)
	require.True(t, ok, "S aliases Target and must own a Scalar node")
	require.NotNil(t, sc.Constraints, "a bound beside a $ref binds S, not Target")
	require.NotNil(t, sc.Constraints.Min)
	assert.Equal(t, "5", sc.Constraints.Min.String())

	target := typeByName(l.out, "Target")
	require.NotNil(t, target)
	if tsc, ok := target.(*ir.Scalar); ok {
		assert.Nil(t, tsc.Constraints, "the referent must not acquire the site's bound")
	}
}
```

`typeByName(doc *ir.Document, name string) ir.TypeDef` already exists in `helpers_test.go:121`. Add
`"github.com/dexpace/morphic/ir"` to the file's imports.

- [ ] **Step 2: Run the test**

Run: `go test ./compilers/openapi/ -run TestLowerComponentSchema_RefSibling -v`
Expected: PASS on current behaviour — this is a characterization test locking in the `$ref`-sibling
fix before the rewiring. If it FAILS, stop: the fix regressed and that must be resolved first.

- [ ] **Step 3: Rewire `lowerComponentSchema` through `site`**

Replace the body of `lowerComponentSchema` (`schema.go:50`) with:

```go
func (l *lowerer) lowerComponentSchema(js *oas3.JSONSchema[oas3.Referenceable], pointer, name string) {
	st := l.siteAt(js, pointer)
	ref := l.schemaRef(js, pointer, name)
	if _, owned := l.byPointer[pointer]; owned {
		return // schemaRef interned the component's own node at its component ID
	}
	l.internAlias(pointer, name, ref, l.schemaConstraints(st.Node, pointer))
	// This alias is the first node the pointer owns, so the examples schemaBody
	// had nowhere to put now have a home.
	if st.Node != nil {
		l.attachSchemaExamples(st.Node, pointer)
	}
}
```

This replaces two independent `siteSchema(js)` derivations — one inside `componentConstraints`, one
inline — with a single `site`. `componentConstraints` now has no caller; delete it and its doc
comment.

- [ ] **Step 4: Verify behaviour is unchanged**

```bash
go test ./compilers/openapi/ -v
git diff --stat HEAD -- testdata/
```
Expected: all tests pass, `testdata/` diff empty.

- [ ] **Step 5: Run the full gates**

```bash
gofmt -l .
golangci-lint run
go vet ./...
go test ./...
```

`golangci-lint` will flag `componentConstraints` as unused if it was not deleted in Step 3.

- [ ] **Step 6: Commit**

```bash
git add compilers/openapi/schema.go compilers/openapi/resolve.go compilers/openapi/resolve_internal_test.go
git commit -m "refactor(compilers/openapi): derive component annotations from a site"
```

---

### Task 7: Route sub-schema hoisting through `site`

`hoistSubSchema` is the second position that independently derives a site. Route it through `siteAt`
so both entry points share one derivation.

**Files:**
- Modify: `compilers/openapi/resolve.go` (`hoistSubSchema`)

**Interfaces:**
- Consumes: `site`, `siteAt`.
- Produces: `hoistSubSchema` signature unchanged.

- [ ] **Step 1: Write the failing test**

Append to `resolve_internal_test.go`:

```go
func TestHoistSubSchema_RefSiblingExampleBindsTheSite(t *testing.T) {
	t.Parallel()
	l := loweredFor(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    Holder:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          example: at-reference
`)
	l.lowerComponentSchemas()

	target := typeByName(l.out, "Target")
	require.NotNil(t, target)
	assert.Empty(t, target.Common().Examples,
		"an example beside a $ref must not attach to the referent")
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./compilers/openapi/ -run TestHoistSubSchema_RefSibling -v`
Expected: PASS on current behaviour — another characterization test.

- [ ] **Step 3: Rewire `hoistSubSchema`**

In `resolve.go`, replace the two independent `siteSchema(decl)` derivations with one `site`:

```go
func (l *lowerer) hoistSubSchema(decl *oas3.JSONSchema[oas3.Referenceable], pointer string) (ir.TypeID, bool) {
	st := l.siteAt(decl, pointer)
	if st.Node == nil {
		return "", false
	}
	hint := refLastSegment(pointer)
	ref := l.schemaRef(decl, pointer, hint)
	if owned, ok := l.byPointer[pointer]; ok {
		return owned, true
	}
	id := l.internAlias(pointer, hint, ref, l.schemaConstraints(st.Node, pointer))
	// As in lowerComponentSchema: this alias is the first node the pointer owns,
	// so the annotations schemaRef had nowhere to put now have a home.
	l.attachSchemaExamples(st.Node, pointer)
	return id, true
}
```

- [ ] **Step 4: Verify behaviour is unchanged**

```bash
go test ./compilers/openapi/ -v
git diff --stat HEAD -- testdata/
```
Expected: all pass, `testdata/` diff empty.

- [ ] **Step 5: Run the full gates**

```bash
gofmt -l .
golangci-lint run
go vet ./...
go test ./...
```

- [ ] **Step 6: Verify coverage is still 100%**

```bash
go test ./... -coverprofile=cover.out
go tool cover -func=cover.out | tail -1
```
Expected: 100%. If a moved function lost coverage, the test that covered it referenced it by a path
that changed — find and restore it rather than adding a new test that merely reaches the line.

- [ ] **Step 7: Commit**

```bash
git add compilers/openapi/resolve.go compilers/openapi/resolve_internal_test.go
git commit -m "refactor(compilers/openapi): route sub-schema hoisting through a site"
```

---

## What this phase deliberately does not do

- **No facet extraction.** That decision waits on Task 3's findings, per the spec's risk entry.
- **No predicate reclassification.** The spec's third category (about twenty pure free functions
  like `schemaAdmitsNull` and `hasUnionSiblings`) needs no work of its own — it is a rule that they
  *stay* free functions as things move around them. It becomes relevant during facet extraction, not
  here. Do not convert any of them to `*lowerer` methods in Tasks 5–7.
- **No `compile.Types` / `compile.Diags`.** The framework lands after the resolver, so it is designed
  against extracted components.
- **No merge-engine extraction.** Explicitly the most deferrable item in the spec.
- **No behaviour changes.** Every task in this plan holds golden output byte-identical. The grid
  *measures*; it does not fix.

## Definition of done

- `internal/harness/grid.go` defines the vocabulary and is unit-tested.
- `compilers/openapi/grid_test.go` covers or explicitly excuses every cell, and
  `TestGrid_EveryCellCoveredOrExcused` passes.
- The PASS/FAIL findings table is in the PR description.
- All eleven resolver functions live in `resolve.go`.
- Both positions that derive a site do so through `siteAt`.
- Four gates green, coverage 100%, `testdata/` unchanged across the whole branch.
