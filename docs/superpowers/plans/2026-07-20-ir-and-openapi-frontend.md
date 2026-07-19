# IR Package + OpenAPI 3.x Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Morphic end-to-end for its first format: the `ir` package (Layer 0), the
multi-format frontend contract + registry (Layer 1), the OpenAPI 3.x frontend, the
`pass/validate` pass, the `engine` orchestration layer (Layer 3), the `cmd/morphic` CLI
(Layer 4) as the user-facing entry point (`morphic parse spec.yaml -o ir.json`), and the test
infrastructure (golden snapshots, round-trip property, capability-conformance corpus,
architecture test).

**Architecture:** Pipeline is frontends (spec → IR) → passes (IR → IR) → backends (IR →
artifacts); this plan builds everything up to and including the CLI, with IR-JSON emission
standing in for backends until Milestone 3. The IR is the ABI: `ir` imports only the stdlib;
`frontend/openapi` imports `ir` + the speakeasy OpenAPI parser; `pass` imports `ir` only;
`engine` composes the registry, sniffs formats, and runs frontend + passes; `cmd/morphic`
renders diagnostics and owns exit codes (no pipeline stage writes to stderr). Every stage is
pure: `f(input, options) → (output, diagnostics)`. The frontend contract and registry are
format-neutral from day one so the seven future frontends (Swagger 2.0, TypeSpec, Smithy,
GraphQL, AsyncAPI, Protobuf, Erlang/OTP) plug in without touching the contract.

**Executor notes (read first):**
- Work strictly in task order; each task ends with the full gate + one commit. Never start a
  task with a dirty working tree.
- Before each task, read the ir-design.md / architecture.md sections its **Consumes** line
  cites — they are in-repo and normative; the plan intentionally does not duplicate them.
- Where a step says "transcribe from ir-design §N", copy the struct shapes exactly (field
  names, types, pointer-ness, field order) and add GoDoc + JSON tags per the Task 1 tag
  convention. Receiver methods are yours to write; shapes are not.
- Items marked *decision procedure* or *verify at impl time* are conditionals on the pinned
  library version: check the named symbol in the vendored source (`go doc <pkg>.<Symbol>` or
  the module cache); take branch A when it exists, branch B (always "preserve raw +
  diagnostic") when it does not. Never guess a third option, and never drop data.
- If a speakeasy signature drifts from the Library facts block, adapt the call site, keep
  this plan's own function signatures unchanged, and note the drift in the commit body.

**Tech Stack:** Go 1.26.3 · `github.com/speakeasy-api/openapi` (OpenAPI parsing) ·
`github.com/stretchr/testify` (require/assert) · `github.com/google/go-cmp` (diffs) ·
`golangci-lint` + `gofmt` + `go vet` (gate).

## Global Constraints

Copied from the normative docs (`docs/ir-design.md`, `docs/architecture.md`, `CLAUDE.md`) —
every task's requirements implicitly include this section.

- **Go version:** `go 1.26.3` in `go.mod`. Module path: `github.com/dexpace/morphic`.
- **`docs/ir-design.md` is normative.** Field names and struct shapes there are the contract.
  Transcribe them **exactly** — do not rename, reorder semantically, "simplify", or merge
  fields. Receiver methods/helpers are yours to design; shapes are not.
- **Layering (enforced by an architecture test, Task 18):** `ir` imports ONLY the stdlib.
  `frontend/*` imports `ir` + its own format libraries, never other frontends, never
  `backend`/`engine`. `pass` imports `ir` only. `engine` imports everything below it;
  `cmd/morphic` imports `engine` (and `ir` for diagnostic rendering) — nothing else from the
  module.
- **No `float64` anywhere in the IR.** All numeric values/constraints/defaults are `BigVal`
  (arbitrary-precision decimal strings).
- **Closed sums = sealed interfaces**: unexported marker method, one concrete struct per kind,
  `Kind()` accessor, JSON as adjacent `kind` tag (`{"kind": "model", ...}`), and a
  completeness test over the kind enum.
- **Deterministic JSON round-trip**: maps in sorted-key order (stdlib `encoding/json` already
  sorts string-kind map keys), slices in source order; `parse → serialize → deserialize →
  deep-equal` must hold for every corpus document.
- **Lossless by default**: no `allOf` flattening, no union-to-optional-fields collapse, no
  primary-response selection. Validation-only JSON Schema keywords (`not`, `if`/`then`/`else`,
  `dependentSchemas`, `contains`/`minContains`/`maxContains`, `unevaluatedItems`) are preserved
  verbatim in `Extensions` with an `info` diagnostic (ir-design §4.7).
- **Stable IDs**: derived from source JSON pointers, never from display names. One hoisting
  pass; no other code derives inline names/IDs.
- **Pure stages**: no package-level mutable state, no stdout/stderr writes; spec problems are
  `ir.Diagnostic` values (severity, stable code, message, provenance), Go `error` returns are
  for I/O and programmer errors only.
- **Heuristics are policy**: anything inferred (operation grouping strategy, pagination
  detection) lives in an injectable policy struct, marks output `Inferred` in provenance, and
  can be disabled. Milestone 1 ships the seam + tag-based grouping only.
- **dexpace Go styleguide is binding** (see `CLAUDE.md` §"Go code style"): 70-line function
  cap; ≥2 assertions per function on average; **recursion only with an explicit depth counter
  checked against a named cap** (schema lowering is recursive — this is load-bearing);
  `%w`-wrapped errors, lowercase unpunctuated; table-driven tests named `TestFunc_Scenario`;
  `testify/require` for preconditions, `assert` for values; `cmp.Diff` never
  `reflect.DeepEqual`; external test packages (`package foo_test`); explicit JSON tags on every
  field; imports in three groups (stdlib, external, local); no `utils`/`helpers`/`common`
  packages; GoDoc on every exported symbol; `doc.go` per package.
- **Verification gate before every commit:** `gofmt -l .` (prints nothing) ·
  `golangci-lint run` (clean) · `go vet ./...` · `go test ./...`.
- **Commits:** Conventional Commits, subject ≤72 chars, imperative, scope = touched package
  (`ir`, `frontend/openapi`, `pass`), e.g. `feat(ir): add type graph nodes`. Branch:
  `feat/ir-openapi-frontend` from `main`.

## File Structure

```
go.mod, go.sum                     # Task 0
.golangci.yml                      # Task 0
ir/
  doc.go                           # package comment (Task 1)
  ids.go                           # TypeID, OpID, ServiceID, ChannelID, MessageID, AuthID, PropID
  bigval.go                        # BigVal decimal-string wrapper
  naming.go                        # Naming
  provenance.go                    # Provenance, Diagnostic, Severity, SourceInfo
  docs.go                          # Docs, Link, Deprecation, Example, ErrorExample
  extensions.go                    # Extensions, RawValue
  value.go                         # Value, ValueKind, Field, ValueRef, CtorValue
  typeref.go                       # TypeRef
  typedef.go                       # TypeDef interface, TypeKind, TypeCommon, kind registry
  types.go                         # Primitive, Scalar, Model, Union, Enum, List, MapT, Tuple,
                                   #   Literal, External, Any + supporting structs
  property.go                      # Property, PresenceKind, Visibility, Lifecycle
  constraints.go                   # Constraints, Encoding, XMLHints
  availability.go                  # Availability, VersionedName, VersionedType, VersionedBool
  service.go                       # Service, OperationGroup, ResourceInfo, ProtocolDecl
  operation.go                     # Operation, Parameter, Payload, Content, PartEncoding,
                                   #   FileInfo, Response, ResponseConditions, StatusRange,
                                   #   ErrorCase, StreamingMode, StreamDetail, Idempotency,
                                   #   Pagination, PropPath, ParamPath, LongRunning, UsageFlags
  bindings.go                      # OpBindings, HTTPBinding, HTTPParamBinding, HTTPLocation,
                                   #   RequestCompression, Callback, RPCBinding, MessageBinding,
                                   #   Reply, GraphQLBinding, OTPBinding
  channel.go                       # Channel, Message
  auth.go                          # AuthScheme, AuthKind, OAuthFlow, AuthRequirement, SchemeUse
  server.go                        # Server, ServerVariable
  document.go                      # Document, Contact, License, TagDef
  json.go                          # sum-type (un)marshaling, TypeRegistry
  irtest/
    golden.go                      # golden-snapshot helpers
frontend/
  doc.go
  frontend.go                      # SourceFormat, Source, Options, Frontend, Registry
frontend/openapi/
  doc.go
  openapi.go                       # Frontend impl: Formats(), Parse() — the 4-phase pipeline
  options.go                       # Options, GroupingStrategy (policy seam)
  load.go                          # speakeasy load/validate → core doc + Diagnostics
  ids.go                           # JSON-pointer → TypeID/OpID/PropID construction
  schema.go                        # schema → TypeDef lowering (recursive, depth-capped)
  compose.go                       # allOf classification, oneOf/anyOf → Union, discriminator
  hoist.go                         # inline-schema hoisting bookkeeping + naming hints
  operations.go                    # paths/operations → Service/Groups/Operations + HTTPBinding
  params.go                        # parameters → Parameter + HTTPParamBinding (style/explode)
  content.go                       # requestBody/responses content → Payload/Content/encoding
  auth.go                          # securitySchemes/security → AuthScheme/AuthRequirement
  meta.go                          # info/servers/tags/webhooks → Document fields
  diag.go                          # diagnostic codes + speakeasy validation-error mapping
pass/
  doc.go
  validate.go                      # referential-integrity pass
engine/
  doc.go
  engine.go                        # Engine: registry composition, Run pipeline (Task 19)
  sniff.go                         # format detection from source bytes
cmd/morphic/
  main.go                          # CLI entry point (Task 20)
  parse.go                         # `morphic parse` subcommand
internal/archtest/
  arch_test.go                     # import-graph layering test (Task 18, extended Task 20)
testdata/                          # corpus (Task 17)
  conformance/openapi/...          # one minimal spec per capability row
  golden/openapi/...               # full-document golden snapshots
```

Interface stability note: tests live next to their package as `package <name>_test` files;
corpus files live under the repo-root `testdata/` tree, addressed with relative paths from the
test files that use them.

---

### Task 0: Module bootstrap

**Files:**
- Create: `go.mod`, `.golangci.yml`
- Branch: `feat/ir-openapi-frontend`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: the module `github.com/dexpace/morphic` at Go 1.26.3; later tasks `go get` their
  dependencies as they first need them (Task 1: testify+cmp; Task 9: speakeasy).

- [ ] **Step 1: Create the branch**

```bash
git checkout main && git pull && git checkout -b feat/ir-openapi-frontend
```

- [ ] **Step 2: Initialize the module**

```bash
go mod init github.com/dexpace/morphic
```

Then edit `go.mod` so the Go directive is exactly:

```
go 1.26.3
```

- [ ] **Step 3: Add the linter config**

Create `.golangci.yml`:

```yaml
version: "2"

linters:
  default: standard   # errcheck, govet, ineffassign, staticcheck, unused
  enable:
    - copyloopvar
    - errorlint      # %w discipline: errors.Is/As over == and type assertions
    - gocritic
    - misspell
    - nilerr
    - prealloc
    - revive
    - unconvert
    - unparam
  settings:
    revive:
      rules:
        - name: exported          # GoDoc on every exported symbol
        - name: package-comments

formatters:
  enable:
    - gofmt
    - gci
  settings:
    gci:
      sections:
        - standard
        - default
        - prefix(github.com/dexpace/morphic)
```

- [ ] **Step 4: Verify the toolchain**

Run: `go version && go vet ./... && golangci-lint run`
Expected: Go 1.26.3 reported; vet/lint succeed trivially (no packages yet). If `golangci-lint`
is not installed: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`.

- [ ] **Step 5: Commit**

```bash
git add go.mod .golangci.yml
git commit -m "build: initialize Go module and lint configuration"
```

---

### Task 1: ir foundation — IDs, BigVal, Extensions, Provenance, Naming, Docs

**Files:**
- Create: `ir/doc.go`, `ir/ids.go`, `ir/bigval.go`, `ir/extensions.go`, `ir/provenance.go`,
  `ir/naming.go`, `ir/docs.go`
- Test: `ir/bigval_test.go`, `ir/extensions_test.go`

**Interfaces:**
- Consumes: `docs/ir-design.md` §3.1 (IDs), §3.2 (Naming), §6 (BigVal), §12 (Docs,
  Deprecation, Example, Extensions), §13 (Provenance, Diagnostic), §2 (SourceInfo fields).
- Produces: `type TypeID string` (+ `OpID`, `ServiceID`, `ChannelID`, `MessageID`, `AuthID`,
  `PropID`); `type BigVal string` with `func NewBigVal(s string) (BigVal, error)`;
  `type Extensions map[string]RawValue` with `type RawValue = json.RawMessage`;
  `type Provenance struct{Source int; Pointer string; Inferred string}`;
  `type Severity string` (`SeverityError/SeverityWarning/SeverityInfo` = "error"/"warning"/"info");
  `type Diagnostic struct{Severity Severity; Code string; Message string; Provenance Provenance}`;
  `type SourceInfo struct{Format string; Path string; Hash string}`;
  `type Naming struct{Source, Canonical, Hint string; Aliases []string}`;
  `type Docs struct{Summary, Description string; ExternalDocs []Link}`;
  `type Link struct{URL, Description string}`;
  `type Deprecation struct{Message, Since, RemovalVersion string}`;
  `type Example struct{Name, Summary, Description string; Value, Headers, Input, Output *Value; Error *ErrorExample; ExternalURL string; Extensions Extensions}`;
  `type ErrorExample struct{Type TypeRef; Content Value}`.
  (`Value` and `TypeRef` are declared in Tasks 2–3; within one package, tests only compile once
  Tasks 1–3 land — write this task's tests against BigVal and Extensions only, which have no
  forward references.)

**JSON tag convention (applies to every ir struct in every task):** every exported field gets
an explicit tag; names are the Go field name in lowerCamel (`IRVersion` → `irVersion`,
`WireNameByFormat` → `wireNameByFormat`); `omitempty` on strings, slices, maps, and pointers;
no `omitempty` on booleans, embedded structs, or required scalars (`Severity`, `Code`).

- [ ] **Step 1: Write the failing BigVal test**

`ir/bigval_test.go`:

```go
package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestNewBigVal_AcceptsDecimalForms(t *testing.T) {
	t.Parallel()
	cases := []string{"0", "-1", "42", "3.14", "-0.5", "1e10", "2.5E-3", "9007199254740993"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			v, err := ir.NewBigVal(s)
			require.NoError(t, err)
			assert.Equal(t, s, v.String())
		})
	}
}

func TestNewBigVal_RejectsNonNumeric(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "abc", "1.2.3", "0x10", "NaN", "Infinity", "1,5"} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			_, err := ir.NewBigVal(s)
			require.Error(t, err)
		})
	}
}

func TestBigVal_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	v, err := ir.NewBigVal("123456789012345678901234567890.5")
	require.NoError(t, err)

	raw, err := json.Marshal(v)
	require.NoError(t, err)
	assert.Equal(t, `"123456789012345678901234567890.5"`, string(raw))

	var back ir.BigVal
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, v, back)
}
```

- [ ] **Step 2: Fetch test deps, run test to verify it fails**

```bash
go get github.com/stretchr/testify@latest github.com/google/go-cmp@latest
go test ./ir -run TestNewBigVal -v
```

Expected: FAIL (package ir does not exist yet).

- [ ] **Step 3: Implement the foundation files**

`ir/doc.go`:

```go
// Package ir defines Morphic's spec-agnostic intermediate representation: the
// single contract between spec frontends and generator backends.
//
// The shapes in this package are normatively specified in docs/ir-design.md;
// field names and struct layouts must match that document exactly. All named
// entities live in flat, ID-keyed registries on [Document] and reference each
// other by ID. The whole Document round-trips through JSON deterministically.
//
// This package imports only the standard library. It contains no parsing, no
// generation, and no I/O. All types are plain data and safe for concurrent
// reads; nothing in this package mutates package-level state.
package ir
```

`ir/ids.go`:

```go
package ir

// IDs are opaque to consumers but constructed deterministically by frontends
// from the source pointer of the defining occurrence (ir-design §3.1). They are
// never derived from display names and never rewritten by renames.

// TypeID identifies a TypeDef in Document.Types,
// e.g. "t/openapi/components/schemas/User".
type TypeID string

// OpID identifies an Operation, same construction as TypeID.
type OpID string

// ServiceID identifies a Service.
type ServiceID string

// ChannelID identifies a Channel in Document.Channels.
type ChannelID string

// MessageID identifies a Message in Document.Messages.
type MessageID string

// AuthID identifies an AuthScheme in Document.Auth.
type AuthID string

// PropID identifies a Property within the document.
type PropID string
```

`ir/bigval.go`:

```go
package ir

import (
	"fmt"
	"math/big"
)

// BigVal is an arbitrary-precision numeric value carried as its decimal string
// form. The IR never stores float64 (the TypeSpec Numeric lesson); helpers may
// convert through math/big at the boundary.
type BigVal string

// NewBigVal validates s as a decimal or scientific-notation numeric literal and
// returns it as a BigVal. It rejects the empty string, hex, NaN, and infinities.
func NewBigVal(s string) (BigVal, error) {
	if s == "" {
		return "", fmt.Errorf("bigval: empty numeric literal")
	}
	// big.Float's Parse with base 10 accepts decimal and e/E exponent forms
	// only; it rejects hex (base 10 is forced), NaN, Infinity, and separators.
	if _, _, err := big.ParseFloat(s, 10, 0, big.ToNearestEven); err != nil {
		return "", fmt.Errorf("bigval: parse %q: %w", s, err)
	}
	return BigVal(s), nil
}

// String returns the literal decimal form.
func (v BigVal) String() string { return string(v) }
```

(BigVal is a string kind, so `encoding/json` marshals it as a JSON string with no custom
marshaler. `big.ParseFloat` with `prec=0` only syntax-checks — no precision is lost because the
stored value is the original string.)

`ir/extensions.go`:

```go
package ir

import "encoding/json"

// RawValue is source JSON preserved verbatim.
type RawValue = json.RawMessage

// Extensions is the lossless escape hatch: source metadata without a
// first-class IR node survives here, keys namespaced by origin so two formats'
// extensions never collide: "openapi:x-rate-limit", "smithy:aws.api#arn",
// "graphql:@key", "erlang:opaque" (ir-design §12).
type Extensions map[string]RawValue
```

`ir/provenance.go`:

```go
package ir

// Provenance records where a node came from and whether it was declared or
// inferred (ir-design §13). Everything heuristic is auditable; everything
// broken is reportable with an exact source location.
type Provenance struct {
	// Source indexes into Document.Sources.
	Source int `json:"source"`
	// Pointer is a JSON pointer or line:col into that source.
	Pointer string `json:"pointer,omitempty"`
	// Inferred is "" for declared facts; otherwise it names the heuristic that
	// produced this node (e.g. "pagination-name-match").
	Inferred string `json:"inferred,omitempty"`
}

// Severity classifies a Diagnostic. The engine decides what is fatal.
type Severity string

// Diagnostic severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Diagnostic is a typed report from a frontend or pass. Codes are stable
// strings ("openapi/unresolved-ref", "ir/dangling-type-ref") so CI can
// allowlist them.
type Diagnostic struct {
	Severity   Severity   `json:"severity"`
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	Provenance Provenance `json:"provenance"`
}

// SourceInfo describes one input file of a Document.
type SourceInfo struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Hash   string `json:"hash"`
}
```

`ir/naming.go` — transcribe `Naming` from ir-design §3.2 verbatim with the tag convention
(fields: `Source`, `Canonical`, `Hint`, `Aliases []string`; document each with the comments
from the spec).

`ir/docs.go` — transcribe from ir-design §12: `Docs{Summary, Description string; ExternalDocs
[]Link}`, `Link{URL, Description string}`, `Deprecation{Message, Since, RemovalVersion
string}`, `Example` and `ErrorExample` exactly as listed in **Produces** above.

- [ ] **Step 4: Run the tests**

Run: `go test ./ir -v`
Expected: BigVal + Extensions tests PASS. (If `docs.go` fails to compile because `Value`/
`TypeRef` don't exist yet, keep the `Example`/`ErrorExample` structs in a temporary
`//go:build ignore` file and move them into `docs.go` in Task 3 — do NOT weaken their shapes.
Preferred: implement Tasks 1–3 on one branch sequentially and let the package compile at
Task 3; the per-task tests still gate each commit.)

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add ir go.mod go.sum
git commit -m "feat(ir): add IDs, BigVal, extensions, provenance, naming, docs"
```

---

### Task 2: ir Values channel

**Files:**
- Create: `ir/value.go`
- Test: `ir/value_test.go`

**Interfaces:**
- Consumes: ir-design §6 verbatim; `BigVal`, `TypeID` from Task 1.
- Produces: `type ValueKind string` with constants `ValueNull/ValueBool/ValueString/
  ValueNumber/ValueBytes/ValueSymbol/ValueList/ValueObject/ValueRefKind/ValueCtor` ("null",
  "bool", "string", "number", "bytes", "symbol", "list", "object", "ref", "ctor" — the
  `"ref"` constant is named `ValueRefKind` because `ValueRef` is the struct);
  `type Value struct{Kind ValueKind; Bool bool; Str string; Num BigVal; Bytes []byte;
  List []Value; Object []Field; Ref *ValueRef; Ctor *CtorValue}`;
  `type Field struct{Name string; Value Value}`;
  `type ValueRef struct{Type TypeID; Member string}`;
  `type CtorValue struct{Scalar TypeID; Name string; Args []Value}`.

- [ ] **Step 1: Write the failing round-trip test**

`ir/value_test.go`:

```go
package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestValue_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	cases := map[string]ir.Value{
		"null":   {Kind: ir.ValueNull},
		"bool":   {Kind: ir.ValueBool, Bool: true},
		"string": {Kind: ir.ValueString, Str: "hello"},
		"symbol": {Kind: ir.ValueSymbol, Str: "ok"},
		"number": {Kind: ir.ValueNumber, Num: ir.BigVal("3.14")},
		"bytes":  {Kind: ir.ValueBytes, Bytes: []byte{0x01, 0x02}},
		"list": {Kind: ir.ValueList, List: []ir.Value{
			{Kind: ir.ValueNumber, Num: ir.BigVal("1")},
			{Kind: ir.ValueNumber, Num: ir.BigVal("2")},
		}},
		"object": {Kind: ir.ValueObject, Object: []ir.Field{
			{Name: "b", Value: ir.Value{Kind: ir.ValueBool, Bool: false}},
			{Name: "a", Value: ir.Value{Kind: ir.ValueString, Str: "x"}},
		}},
		"ref": {Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: ir.TypeID("t/x"), Member: "M"}},
		"ctor": {Kind: ir.ValueCtor, Ctor: &ir.CtorValue{
			Scalar: ir.TypeID("t/s"),
			Name:   "fromISO",
			Args:   []ir.Value{{Kind: ir.ValueString, Str: "2024-05-06"}},
		}},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(v)
			require.NoError(t, err)
			var back ir.Value
			require.NoError(t, json.Unmarshal(raw, &back))
			if diff := cmp.Diff(v, back); diff != "" {
				t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
```

Naming collision note: the spec names both the value-kind constant (`"ref"`) and the struct
`ValueRef`. Keep the struct name `ValueRef` (normative) and, for the `"ref"` kind only, name
the constant `ValueRefKind` (constant names are not normative shapes; its string value is
still `"ref"`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ir -run TestValue_JSONRoundTrip -v`
Expected: FAIL (types undefined).

- [ ] **Step 3: Implement `ir/value.go`**

Transcribe ir-design §6 with the tag convention. `Object` is an **ordered** `[]Field` slice —
never a map (JSON object member order carries meaning for values). Payload fields all get
`omitempty`; the empty `Value{Kind: ValueNull}` must marshal to `{"kind":"null"}`. `Bytes`
marshals via stdlib base64. Plain struct tags suffice — no custom marshaler: unset payload
fields round-trip to their zero values, which is semantically identical because `Kind` gates
which payload field is meaningful.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ir -v`
Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add ir
git commit -m "feat(ir): add typed values channel"
```

---

### Task 3: ir type graph — sealed TypeDef sum + all eleven kinds

**Files:**
- Create: `ir/typeref.go`, `ir/typedef.go`, `ir/types.go`, `ir/property.go`,
  `ir/constraints.go`, `ir/availability.go`
- Test: `ir/typedef_test.go`

**Interfaces:**
- Consumes: ir-design §3.3 (TypeRef), §4 (TypeKind, TypeCommon, TemplateInstantiation,
  TemplateArg), §4.1–4.6 (all kinds + supporting structs), §5.1–5.4 (Property, PresenceKind,
  Visibility, Lifecycle, Constraints, Encoding, XMLHints), §11 (Availability).
- Produces (backbone for every later task):
  - `type TypeRef struct{Target TypeID; Nullable bool}`
  - `type TypeKind string` + constants `KindPrimitive/KindScalar/KindModel/KindUnion/KindEnum/
    KindList/KindMap/KindTuple/KindLiteral/KindExternal/KindAny` = "primitive", "scalar",
    "model", "union", "enum", "list", "map", "tuple", "literal", "external", "any".
  - `type TypeDef interface{ typeDef(); Kind() TypeKind; Common() *TypeCommon }`
  - Concrete kinds: `Primitive{TypeCommon; Prim PrimKind}`, `Scalar{TypeCommon; Base *TypeRef;
    Constraints *Constraints; Encoding *Encoding}`, `Model{...}` (all §4.3 fields incl.
    `Properties []Property`, `Base/Implements/Mixins`, `AdditionalProps *AdditionalProps`,
    `Additional AdditionalMode`, `Abstract/Positional bool`, `ExtensionRanges []WireIDRange`,
    `Discriminator *Discriminator`, `DiscriminatorValue string`, `InputOnly bool`),
    `Union{TypeCommon; Variants []Variant; Exclusive, WireTagged bool; Discriminator
    *Discriminator}`, `Enum{TypeCommon; ValueType PrimKind; Members []EnumMember; Closed,
    Flags bool; FallbackMember string}`, `List{TypeCommon; Elem TypeRef; Constraints
    *Constraints; Encoding *Encoding}`, `MapT{TypeCommon; Key TypeRef; Value TypeRef}`,
    `Tuple{TypeCommon; Elems []TypeRef}`, `Literal{TypeCommon; Value Value}`,
    `External{TypeCommon; Identity string; Package string; MinVersion string}`,
    `Any{TypeCommon}`.
  - Supporting: `PrimKind` (all 25 values from §4.1), `AdditionalMode` ("", "closed",
    "closed_after_composition"), `WireIDRange{From, To int}`, `AdditionalProps{Value TypeRef;
    Key *TypeRef; Patterns []PatternProps}`, `PatternProps{Pattern string; Value TypeRef}`,
    `Discriminator` (§4.3 all 8 fields), `Variant` (§4.4 all 11 fields), `EventInfo`,
    `EnumMember` (§4.5), `TemplateInstantiation`, `TemplateArg`.
  - `Property` (§5.1 — all 27 fields), `PresenceKind`, `Lifecycle = string`,
    `Visibility{Only []Lifecycle; None bool}`, `Constraints` (§5.3), `Encoding{Name string;
    WireType *TypeRef; MediaType string}`, `XMLHints` (§5.4),
    `Availability` + `VersionedName{Version, Name string}` + `VersionedType{Version string;
    Type TypeRef}` + `VersionedBool{Version string; WasRequired bool}` (§11),
    `UsageFlags` — define as `type UsageFlags uint32` with bits `UsageInput/UsageOutput/
    UsageError/UsageMultipart` (1, 2, 4, 8), JSON-encoded as a number.
  - Kind registry: `func NewTypeDef(k TypeKind) (TypeDef, bool)` returning a new zero value
    of the concrete struct for each kind — single source of truth consumed by JSON decoding
    (Task 5) and the completeness test.

- [ ] **Step 1: Write the failing sum-contract test**

`ir/typedef_test.go`:

```go
package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// allKinds is the closed kind set from ir-design §4. Adding a TypeKind without
// updating every consumer must break this file (the assertNever lesson).
var allKinds = []ir.TypeKind{
	ir.KindPrimitive, ir.KindScalar, ir.KindModel, ir.KindUnion, ir.KindEnum,
	ir.KindList, ir.KindMap, ir.KindTuple, ir.KindLiteral, ir.KindExternal, ir.KindAny,
}

func TestTypeDef_KindDispatchIsComplete(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td, ok := ir.NewTypeDef(k)
			require.True(t, ok, "no concrete type registered for kind %q", k)
			assert.Equal(t, k, td.Kind())
			require.NotNil(t, td.Common())
		})
	}
}

func TestNewTypeDef_UnknownKind(t *testing.T) {
	t.Parallel()
	_, ok := ir.NewTypeDef(ir.TypeKind("bogus"))
	assert.False(t, ok)
}

func TestTypeDef_ConcreteTypesImplementInterface(t *testing.T) {
	t.Parallel()
	// Compile-time completeness: one entry per kind.
	for _, td := range []ir.TypeDef{
		&ir.Primitive{}, &ir.Scalar{}, &ir.Model{}, &ir.Union{}, &ir.Enum{},
		&ir.List{}, &ir.MapT{}, &ir.Tuple{}, &ir.Literal{}, &ir.External{}, &ir.Any{},
	} {
		assert.Contains(t, allKinds, td.Kind())
	}
}
```

(`NewTypeDef(k TypeKind) (TypeDef, bool)` is the exported face of the kind registry — it
returns a new zero-valued concrete instance for `k`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ir -run TestTypeDef -v` — Expected: FAIL.

- [ ] **Step 3: Implement the type graph**

`ir/typeref.go`, `ir/typedef.go`, `ir/types.go`, `ir/property.go`, `ir/constraints.go`,
`ir/availability.go`: transcribe every struct listed in **Produces** from its ir-design
section, verbatim field names, spec comments condensed to GoDoc, JSON tags per the Task 1
convention. The sum machinery in `ir/typedef.go`:

```go
package ir

// TypeDef is the sealed sum of all type-graph nodes (ir-design §4). Concrete
// kinds: Primitive, Scalar, Model, Union, Enum, List, MapT, Tuple, Literal,
// External, Any. JSON encodes the sum with an adjacent "kind" tag.
type TypeDef interface {
	typeDef() // sealed: only this package's types implement TypeDef
	Kind() TypeKind
	Common() *TypeCommon
}

// newTypeDefByKind maps every TypeKind to a constructor of its concrete type.
// It is the single source of truth for kind dispatch: JSON decoding and the
// switch-completeness test both consume it.
var newTypeDefByKind = map[TypeKind]func() TypeDef{
	KindPrimitive: func() TypeDef { return &Primitive{} },
	KindScalar:    func() TypeDef { return &Scalar{} },
	KindModel:     func() TypeDef { return &Model{} },
	KindUnion:     func() TypeDef { return &Union{} },
	KindEnum:      func() TypeDef { return &Enum{} },
	KindList:      func() TypeDef { return &List{} },
	KindMap:       func() TypeDef { return &MapT{} },
	KindTuple:     func() TypeDef { return &Tuple{} },
	KindLiteral:   func() TypeDef { return &Literal{} },
	KindExternal:  func() TypeDef { return &External{} },
	KindAny:       func() TypeDef { return &Any{} },
}

// NewTypeDef returns a new zero-valued concrete TypeDef for kind k, or false
// when k is not a registered kind.
func NewTypeDef(k TypeKind) (TypeDef, bool) {
	ctor, ok := newTypeDefByKind[k]
	if !ok {
		return nil, false
	}
	return ctor(), true
}
```

Each concrete type gets the three methods, e.g. for `Model`:

```go
func (*Model) typeDef()             {}
func (*Model) Kind() TypeKind       { return KindModel }
func (m *Model) Common() *TypeCommon { return &m.TypeCommon }
```

`TypeCommon` is **embedded** (not a named field) in every concrete kind and carries the JSON
tag `json:",inline"`-equivalent behavior by embedding (stdlib inlines embedded structs with no
tag). Move `Example`/`ErrorExample` into `ir/docs.go` now if Task 1 parked them.

- [ ] **Step 4: Run tests**

Run: `go test ./ir -v` — Expected: PASS (all files from Tasks 1–3 now compile together).

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add ir
git commit -m "feat(ir): add sealed type graph with all eleven kinds"
```

---

### Task 4: ir service layer, bindings, auth, channels, servers, document root

**Files:**
- Create: `ir/service.go`, `ir/operation.go`, `ir/bindings.go`, `ir/channel.go`, `ir/auth.go`,
  `ir/server.go`, `ir/document.go`
- Test: `ir/document_test.go`

**Interfaces:**
- Consumes: ir-design §2 (Document, Contact, License, TagDef), §7.1–7.3 (service layer), §8
  (bindings), §8.3 (Channel, Message), §9 (auth), §10 (Server, ServerVariable); all Task 1–3
  types.
- Produces: every struct named in those sections, verbatim. Key signatures later tasks use:
  - `const IRVersion = "0.1.0"` — the IR schema's own semver, declared in `document.go`;
    frontends stamp it into `Document.IRVersion`.
  - `Document{IRVersion, Name, Version string; Docs Docs; Contact *Contact; License *License;
    TermsOfService string; Services []Service; Types TypeRegistry; Channels
    map[ChannelID]Channel; Messages map[MessageID]Message; Auth map[AuthID]AuthScheme;
    Servers []Server; TagDefs []TagDef; Versions []string; Extensions Extensions;
    Diagnostics []Diagnostic; Sources []SourceInfo}` — `TypeRegistry` is
    `type TypeRegistry map[TypeID]TypeDef` (declared here, JSON in Task 5).
  - `Service`, `OperationGroup`, `ResourceInfo`, `ProtocolDecl` (§7.1); `Operation`,
    `Parameter`, `Payload`, `Content`, `PartEncoding`, `FileInfo`, `Response`,
    `ResponseConditions`, `StatusRange{From, To int}`, `ErrorCase` (§7.2); `Pagination`,
    `PageStrategy`, `PropPath`, `ParamPath`, `LongRunning`, `StreamingMode`, `StreamDetail`
    (§7.3); `Idempotency` — the spec sketches this as one field whose states are
    `unknown | safe | idempotent | idempotency_token(param)`; the last is parameterized, so a
    bare string cannot carry it. Represent it as `type Idempotency struct { Kind
    IdempotencyKind; TokenParam string }` with `type IdempotencyKind string` constants for
    `""`/`"safe"`/`"idempotent"`/`"idempotency_token"` (`TokenParam` set only for the token
    kind). This is a resolved representation of the spec's shorthand, not a shape change —
    note it in the PR description so ir-design.md can be tightened to match. All other
    shapes: verbatim.
  - `OpBindings`, `HTTPBinding`, `HTTPParamBinding`, `HTTPLocation` with constants
    `HTTPLocationPath/Query/Querystring/Header/Cookie/Body/BodyProperty/Host` (string values
    "path", "query", "querystring", "header", "cookie", "body", "body_property", "host"),
    `RequestCompression`, `Callback`,
    `RPCBinding`, `MessageBinding`, `Reply`, `GraphQLBinding`, `OTPBinding`, `MsgDirection`
    ("send"/"receive") (§8); `Channel`, `Message` (§8.3); `AuthScheme`, `AuthKind` constants
    (all 14 from §9), `OAuthFlow{Kind, AuthorizationURL, TokenURL, RefreshURL string; Scopes
    map[string]string; Extensions Extensions}`, `AuthRequirement{Schemes []SchemeUse}`,
    `SchemeUse{Scheme AuthID; Scopes []string}` (§9); `Server`, `ServerVariable{Name string;
    Default string; Enum []string; Docs Docs; Extensions Extensions}` (§10).

- [ ] **Step 1: Write the failing document-construction test**

`ir/document_test.go` — build a small but representative in-memory document and assert
structural invariants (this is a construction smoke test; JSON comes in Task 5):

```go
package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestDocument_ConstructRepresentative(t *testing.T) {
	t.Parallel()
	userID := ir.TypeID("t/openapi/components/schemas/User")
	doc := ir.Document{
		IRVersion: "0.1.0",
		Name:      "Petstore",
		Version:   "1.0.0",
		Types: ir.TypeRegistry{
			userID: &ir.Model{
				TypeCommon: ir.TypeCommon{
					ID:   userID,
					Name: ir.Naming{Source: "User", Canonical: "user"},
				},
				Properties: []ir.Property{{
					ID:       ir.PropID("p/openapi/components/schemas/User/properties/id"),
					Name:     ir.Naming{Source: "id", Canonical: "id"},
					WireName: "id",
					Type:     ir.TypeRef{Target: ir.TypeID("t/openapi/prim/string")},
					Required: true,
				}},
			},
		},
		Services: []ir.Service{{
			ID:   ir.ServiceID("s/openapi/petstore"),
			Name: ir.Naming{Source: "Petstore", Canonical: "petstore"},
			Groups: []ir.OperationGroup{{
				Name: ir.Naming{Source: "users", Canonical: "users"},
				Operations: []ir.Operation{{
					ID:   ir.OpID("op/openapi/paths/~1users/get"),
					Name: ir.Naming{Source: "listUsers", Canonical: "list_users"},
					Responses: []ir.Response{{
						Conditions: ir.ResponseConditions{
							StatusCodes: []ir.StatusRange{{From: 200, To: 200}},
						},
						Payload: &ir.Payload{Contents: []ir.Content{{
							MediaType: "application/json",
							Type:      ir.TypeRef{Target: userID},
						}}},
					}},
					Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
						Method:      "GET",
						URITemplate: "/users",
					}}},
				}},
			}},
		}},
	}

	require.Len(t, doc.Services, 1)
	got, ok := doc.Types[userID]
	require.True(t, ok)
	model, ok := got.(*ir.Model)
	require.True(t, ok, "expected *ir.Model, got %T", got)
	assert.Equal(t, ir.KindModel, model.Kind())
	assert.True(t, model.Properties[0].Required)
	assert.False(t, model.Properties[0].Type.Nullable)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ir -run TestDocument_ConstructRepresentative -v` — Expected: FAIL.

- [ ] **Step 3: Implement the seven files**

Transcribe every struct listed in **Produces** from the cited ir-design sections. Rules:
- Field names, types, and pointer-ness exactly as the spec sketches them.
- Every struct that the spec gives an `Extensions` field keeps it (§12's rule: every node that
  can carry source metadata has one — that includes `Response`, `ErrorCase`, `Payload`,
  `Content`, `Example`, `ServerVariable`, `OAuthFlow`, and every binding struct).
- GoDoc every exported symbol; carry the spec's semantic comments over (e.g. `OneWay`'s
  "distinct from a response with no body").
- `Operation.Streaming StreamingMode`, `RequestStream/ResponseStream *StreamDetail`,
  `Pagination *Pagination`, `LongRunning *LongRunning` — all per §7.2.
- `Callback struct{Expression string; Operations []OpID}` per §8.1.

- [ ] **Step 4: Run tests**

Run: `go test ./ir -v` — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add ir
git commit -m "feat(ir): add service layer, protocol bindings, auth, channels, document root"
```

---

### Task 5: ir JSON round-trip for the TypeDef sum + determinism

**Files:**
- Create: `ir/json.go`
- Test: `ir/json_test.go`

**Interfaces:**
- Consumes: `TypeDef`, `NewTypeDef`, `newTypeDefByKind`, `TypeRegistry`, `Document` (Tasks
  3–4).
- Produces: `MarshalJSON() ([]byte, error)` on every concrete TypeDef kind (adjacent-tag
  encoding `{"kind":"model",...}`); `func (r TypeRegistry) UnmarshalJSON([]byte) error`
  (kind-dispatched decoding); `Document` round-trips with plain
  `json.Marshal`/`json.Unmarshal`.

- [ ] **Step 1: Write the failing round-trip + determinism test**

`ir/json_test.go`:

```go
package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// sampleDocument builds one document that touches every TypeDef kind.
func sampleDocument(t *testing.T) ir.Document {
	t.Helper()
	mk := func(id string, td ir.TypeDef) (ir.TypeID, ir.TypeDef) {
		typeID := ir.TypeID(id)
		td.Common().ID = typeID
		return typeID, td
	}
	types := ir.TypeRegistry{}
	for _, entry := range []ir.TypeDef{
		&ir.Primitive{Prim: "string"},
		&ir.Scalar{Base: &ir.TypeRef{Target: "t/p/string"}},
		&ir.Model{Additional: "closed"},
		&ir.Union{Exclusive: true},
		&ir.Enum{ValueType: "string", Closed: true},
		&ir.List{Elem: ir.TypeRef{Target: "t/p/string"}},
		&ir.MapT{Key: ir.TypeRef{Target: "t/p/string"}, Value: ir.TypeRef{Target: "t/p/string"}},
		&ir.Tuple{Elems: []ir.TypeRef{{Target: "t/p/string"}}},
		&ir.Literal{Value: ir.Value{Kind: ir.ValueString, Str: "fixed"}},
		&ir.External{Identity: "erlang:pid"},
		&ir.Any{},
	} {
		id, td := mk("t/k/"+string(entry.Kind()), entry)
		types[id] = td
	}
	return ir.Document{IRVersion: "0.1.0", Name: "kinds", Version: "1", Types: types}
}

func TestDocument_JSONRoundTripAllKinds(t *testing.T) {
	t.Parallel()
	doc := sampleDocument(t)

	raw, err := json.Marshal(doc)
	require.NoError(t, err)

	var back ir.Document
	require.NoError(t, json.Unmarshal(raw, &back))
	if diff := cmp.Diff(doc, back); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestDocument_MarshalIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := sampleDocument(t)
	a, err := json.Marshal(doc)
	require.NoError(t, err)
	b, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))
}

func TestTypeRegistry_KindTagIsAdjacent(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(&ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x"}})
	require.NoError(t, err)
	var probe struct {
		Kind ir.TypeKind `json:"kind"`
		ID   ir.TypeID   `json:"id"`
	}
	require.NoError(t, json.Unmarshal(raw, &probe))
	assert.Equal(t, ir.KindModel, probe.Kind)
	assert.Equal(t, ir.TypeID("t/x"), probe.ID)
}

func TestTypeRegistry_UnmarshalRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	var reg ir.TypeRegistry
	err := json.Unmarshal([]byte(`{"t/x":{"kind":"bogus"}}`), &reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ir -run 'TestDocument_JSON|TestTypeRegistry' -v` — Expected: FAIL.

- [ ] **Step 3: Implement `ir/json.go`**

Pattern — per-kind `MarshalJSON` via an alias type (avoids recursion) plus a kind header:

```go
package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// kindHeader peeks the adjacent tag during decoding.
type kindHeader struct {
	Kind TypeKind `json:"kind"`
}

// marshalWithKind emits {"kind":"<k>", ...fields of v...}.
func marshalWithKind(k TypeKind, v any) ([]byte, error) {
	fields, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("ir: marshal %s: %w", k, err)
	}
	if len(fields) < 2 || fields[0] != '{' {
		return nil, fmt.Errorf("ir: marshal %s: concrete kind must encode as an object", k)
	}
	var buf bytes.Buffer
	buf.Grow(len(fields) + 16)
	fmt.Fprintf(&buf, `{"kind":%q`, string(k))
	if !bytes.Equal(fields, []byte("{}")) {
		buf.WriteByte(',')
		buf.Write(fields[1 : len(fields)-1])
		buf.WriteByte('}')
		return buf.Bytes(), nil
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// One MarshalJSON per concrete kind; the alias type strips the method set so
// json.Marshal doesn't recurse.
func (m *Model) MarshalJSON() ([]byte, error) {
	type alias Model
	return marshalWithKind(KindModel, (*alias)(m))
}
```

Write the same three-line `MarshalJSON` for all eleven kinds (Primitive, Scalar, Model, Union,
Enum, List, MapT, Tuple, Literal, External, Any). Then registry decoding:

```go
// UnmarshalJSON decodes a kind-tagged TypeDef per entry, dispatching through
// the same registry the completeness test walks.
func (r *TypeRegistry) UnmarshalJSON(data []byte) error {
	var rawByID map[TypeID]json.RawMessage
	if err := json.Unmarshal(data, &rawByID); err != nil {
		return fmt.Errorf("ir: type registry: %w", err)
	}
	out := make(TypeRegistry, len(rawByID))
	for id, raw := range rawByID {
		var head kindHeader
		if err := json.Unmarshal(raw, &head); err != nil {
			return fmt.Errorf("ir: type %s: reading kind tag: %w", id, err)
		}
		td, ok := NewTypeDef(head.Kind)
		if !ok {
			return fmt.Errorf("ir: type %s: unknown kind %q", id, head.Kind)
		}
		if err := json.Unmarshal(raw, td); err != nil {
			return fmt.Errorf("ir: type %s (%s): %w", id, head.Kind, err)
		}
		out[id] = td
	}
	*r = out
	return nil
}
```

Concrete kinds do NOT need `UnmarshalJSON` (plain struct decoding ignores the extra `kind`
member). Marshaling of `TypeRegistry` needs no custom code: `map[TypeID]TypeDef` sorts keys
(string kind) and calls each value's `MarshalJSON`.

- [ ] **Step 4: Run tests**

Run: `go test ./ir -v` — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add ir
git commit -m "feat(ir): add deterministic kind-tagged JSON round-trip"
```

---

### Task 6: ir/irtest golden-snapshot helpers

**Files:**
- Create: `ir/irtest/golden.go`
- Test: `ir/irtest/golden_test.go`

**Interfaces:**
- Consumes: `ir.Document` and its JSON round-trip (Task 5).
- Produces: `func CompareGolden(t *testing.T, goldenPath string, doc *ir.Document)` — marshals
  `doc` with `json.MarshalIndent(doc, "", "  ")` + trailing newline, compares byte-for-byte
  against the file, rewrites the file instead when `-update` is set;
  `func Update() bool` reporting the flag. Used by every frontend corpus test (Task 15).

- [ ] **Step 1: Write the failing test**

`ir/irtest/golden_test.go`:

```go
package irtest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestCompareGolden_WritesThenMatches(t *testing.T) {
	// Not parallel: exercises the -update path via WriteGolden.
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	doc := &ir.Document{IRVersion: "0.1.0", Name: "g", Version: "1"}

	// First write the golden explicitly, then compare against it.
	require.NoError(t, irtest.WriteGolden(path, doc))
	irtest.CompareGolden(t, path, doc)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, len(raw) > 0 && raw[len(raw)-1] == '\n', "golden must end in newline")
}
```

(`WriteGolden(path string, doc *ir.Document) error` is exported so tests and the `-update`
path share one serializer — no second place can drift.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ir/irtest -v` — Expected: FAIL.

- [ ] **Step 3: Implement `ir/irtest/golden.go`**

```go
// Package irtest provides golden-snapshot helpers for IR documents.
package irtest

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dexpace/morphic/ir"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing")

// Update reports whether -update was passed to go test.
func Update() bool { return *update }

// WriteGolden serializes doc deterministically and writes it to path, creating
// parent directories as needed.
func WriteGolden(path string, doc *ir.Document) error {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("irtest: marshal golden %s: %w", path, err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("irtest: mkdir for golden %s: %w", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("irtest: write golden %s: %w", path, err)
	}
	return nil
}

// CompareGolden compares doc against the golden file at goldenPath, or
// rewrites it when -update is set. Failures include a full diff.
func CompareGolden(t *testing.T, goldenPath string, doc *ir.Document) {
	t.Helper()
	if Update() {
		if err := WriteGolden(goldenPath, doc); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", goldenPath, err)
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	raw = append(raw, '\n')
	if diff := cmp.Diff(string(want), string(raw)); diff != "" {
		t.Errorf("golden mismatch for %s (-golden +got):\n%s", goldenPath, diff)
	}
}
```

- [ ] **Step 4: Run tests** — `go test ./ir/... -v` — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add ir/irtest
git commit -m "feat(ir): add golden-snapshot test helpers"
```

---

### Task 7: frontend contract + registry (the multi-format seam)

**Files:**
- Create: `frontend/doc.go`, `frontend/frontend.go`
- Test: `frontend/frontend_test.go`

**Interfaces:**
- Consumes: `ir.Document`, `ir.Diagnostic`.
- Produces (the contract every future frontend implements — Swagger, TypeSpec, Smithy,
  GraphQL, AsyncAPI, Protobuf, OTP):
  - `type SourceFormat struct{Name string; Version string}` (e.g. `{"openapi", "3.1"}`) with
    `func (f SourceFormat) String() string` → `"openapi@3.1"`.
  - `type Source struct{Path string; Data []byte}` — the *root* documents, pre-read by the
    caller (engine/tests). A frontend may still resolve external `$ref`s through its format
    library relative to `Path` (architecture §2.1 makes reference resolution the frontend's
    job); whether and how is frontend-specific and configurable via `FormatOptions`.
  - `type Options struct{FormatOptions any}` — `FormatOptions` carries the frontend-specific
    options value; each frontend documents its accepted concrete type and treats `nil` as
    defaults.
  - `type Frontend interface{ Formats() []SourceFormat; Parse(ctx context.Context, sources
    []Source, opts Options) (*ir.Document, []ir.Diagnostic, error) }`.
  - `type Registry struct{ /* unexported */ }`, `func NewRegistry() *Registry`,
    `func (r *Registry) Register(f Frontend) error` (error on duplicate format),
    `func (r *Registry) Lookup(format SourceFormat) (Frontend, bool)`,
    `func (r *Registry) Formats() []SourceFormat` (sorted, for CLI display).
    The registry is an instance — no package-level default registry, no `init()`
    self-registration (explicit over implicit; the engine composes its registry).

- [ ] **Step 1: Write the failing registry test**

`frontend/frontend_test.go`:

```go
package frontend_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/ir"
)

// stubFrontend registers under fixed formats and returns an empty document.
type stubFrontend struct{ formats []frontend.SourceFormat }

func (s *stubFrontend) Formats() []frontend.SourceFormat { return s.formats }

func (s *stubFrontend) Parse(_ context.Context, _ []frontend.Source, _ frontend.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: "0.1.0"}, nil, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	oa := &stubFrontend{formats: []frontend.SourceFormat{
		{Name: "openapi", Version: "3.0"},
		{Name: "openapi", Version: "3.1"},
	}}
	require.NoError(t, reg.Register(oa))

	got, ok := reg.Lookup(frontend.SourceFormat{Name: "openapi", Version: "3.1"})
	require.True(t, ok)
	assert.Same(t, frontend.Frontend(oa), got)

	_, ok = reg.Lookup(frontend.SourceFormat{Name: "smithy", Version: "2.0"})
	assert.False(t, ok)
}

func TestRegistry_RejectsDuplicateFormat(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	fmtA := &stubFrontend{formats: []frontend.SourceFormat{{Name: "openapi", Version: "3.1"}}}
	fmtB := &stubFrontend{formats: []frontend.SourceFormat{{Name: "openapi", Version: "3.1"}}}
	require.NoError(t, reg.Register(fmtA))
	err := reg.Register(fmtB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "openapi@3.1")
}

func TestSourceFormat_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "openapi@3.1", frontend.SourceFormat{Name: "openapi", Version: "3.1"}.String())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./frontend -v` — Expected: FAIL.

- [ ] **Step 3: Implement `frontend/frontend.go`** (with `doc.go` package comment: "Package
frontend defines the contract between spec frontends and the engine: a Frontend lowers source
documents of its formats into an ir.Document plus diagnostics, purely and reentrantly.")

```go
package frontend

import (
	"context"
	"fmt"
	"sort"

	"github.com/dexpace/morphic/ir"
)

// SourceFormat identifies one spec dialect a frontend accepts.
type SourceFormat struct {
	Name    string // "openapi", "swagger", "typespec", "smithy", ...
	Version string // "3.0", "3.1", "2.0", ...
}

// String renders the canonical "name@version" form used in diagnostics and
// registry errors.
func (f SourceFormat) String() string { return f.Name + "@" + f.Version }

// Source is one pre-read input document. Frontends perform no file I/O; the
// caller loads bytes so parsing stays pure and reentrant.
type Source struct {
	Path string
	Data []byte
}

// Options carries per-parse configuration. FormatOptions is the
// frontend-specific options value; each frontend documents the concrete type
// it accepts and treats nil as defaults.
type Options struct {
	FormatOptions any
}

// Frontend lowers source documents into the IR. Implementations must be pure:
// no package-level mutable state, no writes to stderr; spec problems are
// returned as ir.Diagnostic values and the error return is reserved for
// I/O-level and programmer errors.
type Frontend interface {
	Formats() []SourceFormat
	Parse(ctx context.Context, sources []Source, opts Options) (*ir.Document, []ir.Diagnostic, error)
}

// Registry maps source formats to frontends. It is a plain instance — there is
// no package-level default and no init()-time self-registration; the engine
// composes its registry explicitly.
type Registry struct {
	byFormat map[SourceFormat]Frontend
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byFormat: make(map[SourceFormat]Frontend)}
}

// Register adds f under every format it reports. It fails if any format is
// already claimed; on failure nothing is registered.
func (r *Registry) Register(f Frontend) error {
	formats := f.Formats()
	if len(formats) == 0 {
		return fmt.Errorf("frontend: register: frontend reports no formats")
	}
	for _, format := range formats {
		if _, taken := r.byFormat[format]; taken {
			return fmt.Errorf("frontend: register: format %s already registered", format)
		}
	}
	for _, format := range formats {
		r.byFormat[format] = f
	}
	return nil
}

// Lookup returns the frontend registered for format.
func (r *Registry) Lookup(format SourceFormat) (Frontend, bool) {
	f, ok := r.byFormat[format]
	return f, ok
}

// Formats lists every registered format, sorted for stable display.
func (r *Registry) Formats() []SourceFormat {
	out := make([]SourceFormat, 0, len(r.byFormat))
	for f := range r.byFormat {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}
```

- [ ] **Step 4: Run tests** — `go test ./frontend -v` — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend
git commit -m "feat(frontend): add format-neutral frontend contract and registry"
```

---

### Library facts (verified against speakeasy-api/openapi @ v1.24.0 — pin ≥ this)

Every frontend task below relies on these; they were verified against the library source and
pkg.go.dev. Anything marked *verify at impl time* must be checked against the vendored version
before use.

- `go get github.com/speakeasy-api/openapi@latest` (the module requires Go ≥1.24.3,
  compatible with our 1.26.3).
- Parse: `openapi.Unmarshal(ctx context.Context, doc io.Reader, opts ...Option[UnmarshalOptions]) (*OpenAPI, []error, error)`
  from `github.com/speakeasy-api/openapi/openapi`. Middle return = validation errors;
  extract structured info with `errors.As` into `*validation.Error`
  (`github.com/speakeasy-api/openapi/validation`): fields `Severity` ("error"/"warning"/
  "hint"), `Rule`, methods `GetLineNumber()/GetColumnNumber()`.
- One unified model for 3.0.x/3.1.x/3.2.0; raw version string in `doc.OpenAPI`.
  **No normalization on parse**: 3.0 `nullable` stays `Schema.Nullable *bool`; 3.1
  `type: [T, "null"]` stays in the `Type` either-value (`(*Schema).GetType() []SchemaType`
  normalizes to a slice). Our frontend owns normalization.
- References are **not** auto-resolved. Whole-document:
  `doc.ResolveAllReferences(ctx, openapi.ResolveAllOptions{OpenAPILocation: src.Path})
  ([]error, error)`. Ref-or-inline positions are `Referenced*` wrappers
  (`ReferencedParameter`, `ReferencedRequestBody`, `ReferencedResponse`, `ReferencedHeader`,
  `ReferencedSecurityScheme`, ...): `IsReference()`, `Resolve(...)`, `GetObject() *T`.
  Schemas: `*oas3.JSONSchema[oas3.Referenceable]` (`github.com/speakeasy-api/openapi/
  jsonschema/oas3`) with `IsReference()`, `GetSchema() *Schema`, `GetBool() *bool` (boolean
  schemas), `GetRef()/GetAbsRef()`, `GetResolvedSchema()`. *Verify at impl time* which extra
  fields `ResolveAllOptions` carries (`DisableExternalRefs`/`VirtualFS` exist on
  `references.ResolveOptions`; fall back to per-reference resolution if the All-variant lacks
  them).
- **Numeric fidelity trap:** `Schema.Minimum/Maximum/MultipleOf` are `*float64` in the
  high-level model. NEVER lower them from those fields — read the raw decimal string from the
  YAML node instead: every model embeds `marshaller.Model[core]` with `GetCore()`,
  `GetRootNode() *yaml.Node`, `GetPropertyNode(prop string) *yaml.Node`,
  `GetRootNodeLine()/Column()`. `Enum []values.Value`, `Const/Default/Example values.Value`
  where `values.Value = *yaml.Node` — the node's `.Value` is the untruncated literal string.
- All object maps (`Paths`, `Responses`, `Content`, `Properties`, `Components.*`,
  `Extensions`) are `*sequencedmap.Map[K,V]` preserving **source order**: iterate with
  `All() iter.Seq2[K,V]`; also `Get(k) (V, bool)`, `Len()`.
- Extensions: `Extensions *extensions.Extensions` on every model type — an ordered map of
  `x-name → *yaml.Node`.
- `ExclusiveMinimum/Maximum` are either-values: bool (3.0) or number (2020-12) — both
  preserved; handle both arms.
- 3.2 surface (e.g. `itemSchema`, `additionalOperations`, `in: querystring`): the library
  targets 3.2.0; *verify at impl time* which of these are exposed as model fields. Where a
  field exists, lower it per the ir-design §14 OpenAPI row; where it does not, the raw node is
  still reachable — preserve under `Extensions` with an `info` diagnostic rather than drop.

---

### Task 8: frontend/openapi — pointers, IDs, options, diagnostic codes

**Files:**
- Create: `frontend/openapi/doc.go`, `frontend/openapi/ids.go`, `frontend/openapi/options.go`,
  `frontend/openapi/diag.go`
- Test: `frontend/openapi/ids_test.go`

**Interfaces:**
- Consumes: `ir` ID types.
- Produces (every later frontend task keys identity through these — nothing else may derive
  IDs, per the one-hoisting-pass rule):
  - `func ptr(segments ...string) string` — joins segments into an RFC 6901 JSON pointer,
    escaping `~`→`~0` then `/`→`~1` per segment. `ptr("paths", "/users/{id}", "get")` →
    `"/paths/~1users~1{id}/get"`.
  - `func namedTypeID(pointer string) ir.TypeID` → `"t/openapi" + pointer` (components-named
    schemas); `func anonTypeID(pointer string) ir.TypeID` → `"t/anon" + pointer` (hoisted
    inline types); `func primTypeID(k ir.PrimKind) ir.TypeID` → `"t/prim/" + k` (interned
    primitives); `func opID(pointer string) ir.OpID` → `"op/openapi" + pointer`;
    `func propID(pointer string) ir.PropID` → `"p/openapi" + pointer`;
    `func authIDFor(name string) ir.AuthID` → `"auth/openapi" + ptr("components",
    "securitySchemes", name)`; `func serviceID(sourceIndex int) ir.ServiceID` →
    `"s/openapi/" + strconv.Itoa(sourceIndex)`.
  - `func refTypeID(ref string) (ir.TypeID, error)` — `"#/components/schemas/User"` →
    `namedTypeID("/components/schemas/User")`; any other local pointer target →
    `anonTypeID(<pointer>)`; external ref (`other.yaml#/...`) → `ir.TypeID("t/openapi/ext/" +
    ref)`; empty/malformed → error.
  - `type Options struct{Grouping GroupingStrategy; DisableExternalRefs bool}` with
    `type GroupingStrategy string`, constants `GroupByTags GroupingStrategy = "tags"`
    (default) and `GroupByPathPrefix = "path-prefix"` — the injectable-policy seam
    (architecture principle 6). `func (o Options) withDefaults() Options`.
  - `diag.go`: stable code constants — `codeValidation = "openapi/validation"` (suffixed with
    the library rule name), `codeUnsupportedVersion = "openapi/unsupported-version"`,
    `codeUnresolvedRef = "openapi/unresolved-ref"`, `codeValidationOnlyKeyword =
    "openapi/validation-only-keyword"`, `codeFalseSchema = "openapi/false-schema"`,
    `codeNumericPrecision = "openapi/invalid-numeric-literal"`, `codeDegradedConstruct =
    "openapi/degraded-construct"` — plus
    `func diagf(sev ir.Severity, code string, prov ir.Provenance, format string, args ...any) ir.Diagnostic`.

- [ ] **Step 1: Write the failing pointer/ID test**

`frontend/openapi/ids_test.go` (internal test package — these helpers are unexported):

```go
package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestPtr_EscapesPerRFC6901(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		segments []string
		want     string
	}{
		{"plain", []string{"components", "schemas", "User"}, "/components/schemas/User"},
		{"slash in segment", []string{"paths", "/users/{id}", "get"}, "/paths/~1users~1{id}/get"},
		{"tilde in segment", []string{"components", "schemas", "a~b"}, "/components/schemas/a~0b"},
		{"tilde-slash order", []string{"x", "~/"}, "/x/~0~1"},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ptr(tc.segments...))
		})
	}
}

func TestRefTypeID_LocalAndExternal(t *testing.T) {
	t.Parallel()
	id, err := refTypeID("#/components/schemas/User")
	require.NoError(t, err)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/User"), id)

	id, err = refTypeID("#/paths/~1users/get/responses/200")
	require.NoError(t, err)
	assert.Equal(t, ir.TypeID("t/anon/paths/~1users/get/responses/200"), id)

	id, err = refTypeID("common.yaml#/components/schemas/Err")
	require.NoError(t, err)
	assert.Equal(t, ir.TypeID("t/openapi/ext/common.yaml#/components/schemas/Err"), id)

	_, err = refTypeID("")
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./frontend/openapi -v` — Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Implement the four files**

`ids.go` core:

```go
package openapi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// ptr joins segments into an RFC 6901 JSON pointer. IDs are derived from these
// pointers (ir-design §3.1); no other code may construct IDs or pointers.
func ptr(segments ...string) string {
	if len(segments) == 0 {
		return ""
	}
	var b strings.Builder
	for _, seg := range segments {
		b.WriteByte('/')
		b.WriteString(escapeSegment(seg))
	}
	return b.String()
}

// escapeSegment applies RFC 6901 escaping: ~ first, then /.
func escapeSegment(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

func namedTypeID(pointer string) ir.TypeID { return ir.TypeID("t/openapi" + pointer) }
func anonTypeID(pointer string) ir.TypeID  { return ir.TypeID("t/anon" + pointer) }
func primTypeID(k ir.PrimKind) ir.TypeID   { return ir.TypeID("t/prim/" + string(k)) }
func opID(pointer string) ir.OpID          { return ir.OpID("op/openapi" + pointer) }
func propID(pointer string) ir.PropID      { return ir.PropID("p/openapi" + pointer) }

func authIDFor(name string) ir.AuthID {
	return ir.AuthID("auth/openapi" + ptr("components", "securitySchemes", name))
}

func serviceID(sourceIndex int) ir.ServiceID {
	return ir.ServiceID("s/openapi/" + strconv.Itoa(sourceIndex))
}

// refTypeID maps a $ref string to the stable ID of its target.
func refTypeID(ref string) (ir.TypeID, error) {
	doc, pointer, found := strings.Cut(ref, "#")
	if !found && doc == "" {
		return "", fmt.Errorf("openapi: empty $ref")
	}
	switch {
	case doc != "": // external document
		return ir.TypeID("t/openapi/ext/" + ref), nil
	case strings.HasPrefix(pointer, "/components/schemas/"):
		return namedTypeID(pointer), nil
	case pointer != "":
		return anonTypeID(pointer), nil
	default:
		return "", fmt.Errorf("openapi: unsupported $ref form %q", ref)
	}
}
```

`options.go` and `diag.go` per **Produces**; `doc.go` package comment: "Package openapi lowers
OpenAPI 3.0/3.1/3.2 documents into the Morphic IR. It implements frontend.Frontend. Parsing is
delegated to github.com/speakeasy-api/openapi; this package owns identity (pointer-derived
IDs), hoisting, normalization (nullable spellings, allOf classification), and lossless
preservation of constructs the IR does not model structurally."

- [ ] **Step 4: Run tests** — `go test ./frontend/openapi -v` — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi
git commit -m "feat(frontend/openapi): add pointer-derived IDs, options, diagnostic codes"
```

---

### Task 9: frontend/openapi — load & validate phase

**Files:**
- Create: `frontend/openapi/load.go`
- Test: `frontend/openapi/load_test.go`

**Interfaces:**
- Consumes: speakeasy `openapi.Unmarshal`, `validation.Error`, `ResolveAllReferences`;
  Task 8 options/diag.
- Produces: `type loaded struct{Doc *soa.OpenAPI; Format frontend.SourceFormat; Source
  ir.SourceInfo}` (alias the library import as `soa "github.com/speakeasy-api/openapi/openapi"`
  to avoid clashing with this package's own name) and
  `func load(ctx context.Context, srcIndex int, src frontend.Source, opts Options) (*loaded, []ir.Diagnostic, error)`:
  1. `soa.Unmarshal(ctx, bytes.NewReader(src.Data))`; hard error → wrapped Go error return.
  2. Each validation error → `ir.Diagnostic`: `errors.As` to `*validation.Error` gives
     severity mapping (`"error"`→`SeverityError`, `"warning"`→`SeverityWarning`, `"hint"`→
     `SeverityInfo`), code `codeValidation + "/" + e.Rule`, provenance
     `ir.Provenance{Source: srcIndex, Pointer: fmt.Sprintf("%d:%d", e.GetLineNumber(),
     e.GetColumnNumber())}`; non-matching errors → `SeverityError` with the bare message.
  3. Version gate: `doc.OpenAPI` must have major.minor prefix `3.0`/`3.1`/`3.2`; otherwise
     return one `codeUnsupportedVersion` error diagnostic and a nil document (not a Go error —
     it is a spec problem).
  4. Resolve: unless `opts.DisableExternalRefs` forces local-only, call
     `doc.ResolveAllReferences(ctx, soa.ResolveAllOptions{OpenAPILocation: src.Path})`;
     returned resolution errors → `codeUnresolvedRef` diagnostics (severity error), not Go
     errors — lowering continues and the validate pass reports dangling refs.
  5. `ir.SourceInfo{Format: "openapi@" + <major.minor>, Path: src.Path, Hash:
     sha256-hex(src.Data)}`.

- [ ] **Step 1: Write the failing test**

`frontend/openapi/load_test.go` (internal package; table-driven):

```go
package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/ir"
)

const minimal31 = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
`

func TestLoad_Minimal31(t *testing.T) {
	t.Parallel()
	got, diags, err := load(t.Context(), 0, frontend.Source{Path: "spec.yaml", Data: []byte(minimal31)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, frontend.SourceFormat{Name: "openapi", Version: "3.1"}, got.Format)
	assert.Equal(t, "spec.yaml", got.Source.Path)
	assert.Len(t, got.Source.Hash, 64)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

func TestLoad_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	src := frontend.Source{Path: "old.yaml", Data: []byte("swagger: \"2.0\"\ninfo: {title: T, version: \"1\"}\npaths: {}\n")}
	got, diags, err := load(t.Context(), 0, src, Options{}.withDefaults())
	require.NoError(t, err) // spec problems are diagnostics, not Go errors
	assert.Nil(t, got)
	require.NotEmpty(t, diags)
	assert.Equal(t, codeUnsupportedVersion, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func TestLoad_ValidationErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	// paths entry with a bogus structure triggers library validation errors.
	src := frontend.Source{Path: "bad.yaml", Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {/x: {get: {responses: \"nope\"}}}\n")}
	_, diags, err := load(t.Context(), 0, src, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotEmpty(t, diags)
	found := false
	for _, d := range diags {
		if d.Provenance.Pointer != "" {
			found = true
		}
	}
	assert.True(t, found, "diagnostics should carry line:col provenance")
}
```

- [ ] **Step 2: Fetch the dependency, run test to verify it fails**

```bash
go get github.com/speakeasy-api/openapi@latest
go test ./frontend/openapi -run TestLoad -v
```

Expected: FAIL (`load` undefined). If the exact `Unmarshal`/`ResolveAllOptions` signatures
drifted from the Library facts block, adapt to the vendored version and note it in the commit
body — the *shape* of `load` (its signature and diagnostic behavior) must not change.

- [ ] **Step 3: Implement `load.go`** per **Produces** (all five numbered behaviors).

- [ ] **Step 4: Run tests** — `go test ./frontend/openapi -v` — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi go.mod go.sum
git commit -m "feat(frontend/openapi): load, validate, and resolve source documents"
```

---

### Task 10: frontend/openapi — value conversion + schema lowering core

**Files:**
- Create: `frontend/openapi/value.go`, `frontend/openapi/schema.go`,
  `frontend/openapi/hoist.go`
- Test: `frontend/openapi/value_test.go`, `frontend/openapi/schema_test.go`

**Interfaces:**
- Consumes: Tasks 8–9; `oas3.Schema` fields and `GetCore()` node access per Library facts.
- Produces:
  - `func valueFromNode(node *yaml.Node) (ir.Value, error)` — YAML scalar tags `!!null` →
    `ValueNull`, `!!bool` → `ValueBool`, `!!str` → `ValueString`, `!!int`/`!!float` →
    `ValueNumber` with `ir.NewBigVal(node.Value)` (raw literal string — full precision),
    `!!binary` → `ValueBytes`; sequence → `ValueList`; mapping → `ValueObject` (ordered
    `[]ir.Field`); alias nodes are followed; anything else → error. Bound the recursion with
    an explicit depth counter against `const maxValueDepth = 128`.
  - `type lowerer struct` — the single mutable context of one Parse call (a local, not a
    global): `srcIndex int`, `doc *soa.OpenAPI`, `out *ir.Document`, `opts Options`,
    `diags []ir.Diagnostic`, `byPointer map[string]ir.TypeID` (the hoisting/interning table),
    `depth int`.
  - `const maxSchemaDepth = 256` — lowering recursion cap (styleguide bounded-recursion rule);
    exceeding it emits an error diagnostic and returns `t/prim/any`.
  - `func (l *lowerer) primRef(k ir.PrimKind) ir.TypeRef` — interns a `Primitive` under
    `primTypeID(k)` on first use.
  - `func (l *lowerer) schemaRef(js *oas3.JSONSchema[oas3.Referenceable], pointer string, hint string) ir.TypeRef`
    — THE schema entry point; every schema position (property, items, params, bodies) goes
    through it. Behavior:
    1. `nil` or boolean `true` schema → `primRef(any)`. Boolean `false` → hoisted empty
       `Model{Additional: "closed"}` + `codeFalseSchema` info diagnostic.
    2. `$ref` → `refTypeID(...)` (use `GetRef()`; `GetAbsRef()` for externals). Target
       lowering happens where the target is *defined* (components loop / its own inline
       position) — never lower a target from a ref site (single-hoisting-pass rule).
       If the target schema is resolvable and carries 3.0 `nullable: true`, OR the ref-site
       nullability in.
    3. Inline schema → `l.lower(schema, pointer, hint)` which **interns by pointer**:
       `byPointer[pointer]` hit → return existing ID (this is what makes recursion and
       diamond sharing terminate); miss → record the ID *before* descending, build the
       TypeDef, register in `out.Types`.
    4. Nullability normalization (both dialects → the one IR bit, ir-design §3.3):
       3.0 `Nullable *bool` true → `ref.Nullable = true`; 3.1 type array containing `"null"`
       → strip it, `ref.Nullable = true`; a oneOf/anyOf whose variant set is {null-schema, X}
       → lower X only, `ref.Nullable = true` (never a union node — rule from §3.3).
  - `func (l *lowerer) lower(s *oas3.Schema, pointer string, hint string) ir.TypeID` —
    dispatch on effective type(s):
    - multiple non-null types (`["string","integer"]`) → anonymous `Union` with one variant
      per type, `Exclusive: true, WireTagged: false`.
    - `"object"` (or presence of `properties`) → `Model` (Task 12 fills detail).
    - `"array"`: `PrefixItems` present → `Tuple{Elems: ...}` (+ `Items` residue →
      `Extensions["openapi:items-after-prefix"]` if both present); else `List{Elem:
      schemaRef(Items, pointer+"/items", hint+"_item")}`, constraints
      minItems/maxItems/uniqueItems onto `List.Constraints`.
    - `"string"|"number"|"integer"|"boolean"` → format mapping table below.
    - no type at all: `enum`/`const` present → Task 11; composition keywords → Task 11;
      otherwise → `any`.
    - `Anonymous: true` + `Naming{Hint: hint}` on every hoisted inline type; named components
      get `Naming{Source: <key>, Canonical: canonicalWords(<key>)}`.
  - `func canonicalWords(name string) string` — neutral `lower_snake` word sequence: split on
    `_`/`-`/spaces and lower↔upper camel boundaries (`userID` → `user_id`, `HTTPServer` →
    `http_server`), lowercase, join with `_`. No acronym opinions beyond boundary detection
    (casing policy is a backend concern).
  - **Format mapping table** (`type`+`format` → IR), implemented as one package-level
    `var formatTable map[string]...` consulted by `lower`:
    | source | IR |
    |---|---|
    | `string` (no format) | prim `string` |
    | `string`+`date` / `time` / `duration` / `uuid` / `uri`→`url` | prim `date`/`time`/`duration`/`uuid`/`url` |
    | `string`+`date-time` | prim `datetime_offset` (RFC 3339 with offset is its default encoding — no Encoding node needed) |
    | `string`+`byte` | hoisted `Scalar{Base: primRef(bytes), Encoding: &ir.Encoding{Name: "base64", WireType: &<string prim ref>}}` |
    | `string`+`binary` | prim `bytes` (raw octet body; `FileInfo` handling is content-level, Task 14) |
    | `string`+`password` | prim `string`; the *property* lowering sets `Secret: true` (§5.1) |
    | `string`+unknown format | hoisted `Scalar{Base: primRef(string), Encoding: &ir.Encoding{Name: <format>}}` |
    | `integer` (no format) | prim `integer` |
    | `integer`+`int32`/`int64` | prim `int32`/`int64` |
    | `number` (no format) | prim `number` |
    | `number`+`float`/`double` | prim `float32`/`float64` |
    | `number`+`decimal` | prim `decimal` |
    | `boolean` | prim `bool` |
    Unknown format on numeric/bool types → same hoisted-Scalar pattern as strings.
  - Provenance on every produced node: `ir.Provenance{Source: l.srcIndex, Pointer: pointer}`.

- [ ] **Step 1: Write the failing tests**

`frontend/openapi/value_test.go`:

*Decision procedure (yaml module):* `valueFromNode` takes the same `*yaml.Node` type the
speakeasy schemas expose. After `go get`, run `go doc github.com/speakeasy-api/openapi/
extensions.Extension` — its definition names the yaml module (either `gopkg.in/yaml.v3` or a
speakeasy fork). Import exactly that module everywhere this plan says `yaml`; add it to
`go.mod` explicitly (`go get <module>`), and to the Task 18 allowlist.

```go
package openapi

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// yamlNode parses a YAML snippet and returns its root value node (the
// document node's single content child), matching what schema fields expose.
func yamlNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	require.Len(t, doc.Content, 1, "expected a single document node")
	return doc.Content[0]
}

func TestValueFromNode_Scalars(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      ir.Value
	}{
		{"null", "null", ir.Value{Kind: ir.ValueNull}},
		{"bool", "true", ir.Value{Kind: ir.ValueBool, Bool: true}},
		{"string", `"hi"`, ir.Value{Kind: ir.ValueString, Str: "hi"}},
		{"int precision", "9007199254740993", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("9007199254740993")}},
		{"big decimal", "0.30000000000000004", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("0.30000000000000004")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := valueFromNode(yamlNode(t, tc.src))
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValueFromNode_ObjectPreservesOrder(t *testing.T) {
	t.Parallel()
	got, err := valueFromNode(yamlNode(t, "b: 1\na: 2\n"))
	require.NoError(t, err)
	require.Equal(t, ir.ValueObject, got.Kind)
	require.Len(t, got.Object, 2)
	require.Equal(t, "b", got.Object[0].Name)
	require.Equal(t, "a", got.Object[1].Name)
}
```

`frontend/openapi/schema_test.go` — table-driven over minimal spec strings. Define these two
helpers in this file now; every schema-level test in Tasks 10–14 uses them (Task 15's tests
switch to the public `Parse`):

```go
// lowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics. It drives the same lowerer Parse
// will use, without requiring the not-yet-written operation lowering.
func lowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load(t.Context(), 0, frontend.Source{Path: "spec.yaml", Data: []byte(src)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	l := newLowerer(0, loadedDoc, Options{}.withDefaults())
	l.lowerComponentSchemas() // named components; the entry Parse's run() calls first
	return l.out, append(diags, l.diags...)
}

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		require.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}
```

(`newLowerer(srcIndex int, doc *loaded, opts Options) *lowerer` and
`func (l *lowerer) lowerComponentSchemas()` are part of this task's implementation —
`newLowerer` allocates the maps and an empty `*ir.Document` with `Types: ir.TypeRegistry{}`;
`lowerComponentSchemas` iterates `Components.GetSchemas().All()` in source order and interns
each named schema at pointer `ptr("components", "schemas", name)`.)

```go
func TestSchemaRef_NullableNormalization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, schema string // YAML fragment under components/schemas/S/properties/p
		wantNullable bool
		wantTarget   ir.TypeID
	}{
		{"3.0 nullable", "{type: string, nullable: true}", true, "t/prim/string"},
		{"3.1 type array", `{type: [string, "null"]}`, true, "t/prim/string"},
		{"plain", "{type: string}", false, "t/prim/string"},
	}
	// build spec, lower, then assert on the property's TypeRef.
	...
}

func TestLower_RecursiveSchemaTerminates(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Node:
      type: object
      properties:
        next: {$ref: "#/components/schemas/Node"}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	node, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Node")].(*ir.Model)
	require.True(t, ok)
	require.Equal(t, ir.TypeRef{Target: "t/openapi/components/schemas/Node"}, node.Properties[0].Type)
}

func TestLower_InlineSchemaHoistedOnce(t *testing.T) { /* items object under a named schema
	appears in Types exactly once, ID "t/anon/components/schemas/S/properties/tags/items",
	Anonymous true, Hint "tags_item" */ }

func TestCanonicalWords(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"userID": "user_id", "HTTPServer": "http_server", "list-users": "list_users",
		"User": "user", "APIKey2": "api_key_2",
	} {
		assert.Equal(t, want, canonicalWords(in), "input %q", in)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail** — `go test ./frontend/openapi -v` — FAIL.

- [ ] **Step 3: Implement `value.go`, `schema.go`, `hoist.go`** per **Produces**. The
interning core in `hoist.go`:

```go
// intern returns the TypeID for pointer, lowering the schema on first visit.
// Registering the ID before descending is what terminates recursive schemas.
func (l *lowerer) intern(pointer string, id ir.TypeID, build func() ir.TypeDef) ir.TypeID {
	if existing, ok := l.byPointer[pointer]; ok {
		return existing
	}
	l.byPointer[pointer] = id
	l.out.Types[id] = build() // build may recurse; self-references hit byPointer
	return id
}
```

(Note: `build` must place the returned TypeDef's fields via its `Common()` before returning;
self-referential lookups only need the ID, never the finished node — assert this stays true.)

- [ ] **Step 4: Run tests** — `go test ./frontend/openapi -v` — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi
git commit -m "feat(frontend/openapi): lower schemas with pointer-interned hoisting"
```

---

### Task 11: frontend/openapi — composition: allOf, oneOf/anyOf, discriminators, enums, const

**Files:**
- Create: `frontend/openapi/compose.go`
- Modify: `frontend/openapi/schema.go` (dispatch to compose paths)
- Test: `frontend/openapi/compose_test.go`

**Interfaces:**
- Consumes: `lowerer`, `schemaRef`, `intern` (Task 10).
- Produces (all called from `lower`):
  - `func (l *lowerer) lowerAllOf(s *oas3.Schema, pointer, hint string) ir.TypeID` —
    ir-design §4.3 classification, exactly: `$ref` entry that participates in a discriminator
    hierarchy (the ref target carries `discriminator`, or maps to this schema from one), or
    the *sole* `$ref` entry → `Model.Base`; other `$ref` entries → `Model.Mixins` (source
    order); inline entries → their properties merged into `Model.Properties`, each property's
    `Provenance.Pointer` pointing into the allOf branch it came from. Never flatten refs.
  - `func (l *lowerer) lowerOneOfAnyOf(s *oas3.Schema, pointer, hint string) ir.TypeRef` —
    null-variant collapse first (§3.3, Task 10 rule); the rest → `Union{Exclusive: oneOf,
    WireTagged: false}`, one `Variant` per branch (`Name.Hint` from target name or index,
    `Type` via `schemaRef(branch, pointer+"/oneOf/<i>", ...)`). A oneOf whose variants are
    all string consts is **still a Union** here — the enum collapse is a `pass/`
    normalization, not frontend behavior (§14 row).
  - `func (l *lowerer) lowerDiscriminator(s *oas3.Schema, u *ir.Union) *ir.Discriminator` —
    `propertyName` → `Discriminator.PropertyName` (union form), `mapping` values are `$ref`
    strings or schema names → resolve each to a `TypeID` via `refTypeID` (bare names imply
    `#/components/schemas/<name>`); nil mapping stays nil (infer-by-name semantics). On a
    `Model` base (allOf hierarchies): `Discriminator.Property` (the tag `PropID`) +
    subtypes get `DiscriminatorValue` from the mapping (or their schema name); 3.2
    `defaultMapping` → `Discriminator.Default` *(verify at impl time whether the library
    exposes it; else preserve raw)*.
  - Enum/const lowering in `lower`: `Const` set → `Literal{Value: valueFromNode(...)}`;
    `Enum` set → `Enum{ValueType: <from type or inferred from members>, Closed: true,
    Members: one per value}` — member `Name.Source` = the literal's string form,
    `Value` via `valueFromNode`; heterogeneous or non-scalar enum members → fall back to
    `Union` of `Literal`s with a `codeDegradedConstruct` info diagnostic (nothing dropped).

- [ ] **Step 1: Write the failing tests** — `frontend/openapi/compose_test.go`, table-driven
minimal specs asserting exact registry shapes (same `lowerSpec` helper):

```go
func TestAllOf_SoleRefBecomesBase(t *testing.T) {
	spec := /* Dog: allOf: [$ref Animal, {type: object, properties: {bark: {type: string}}}] */
	// assert: Dog is *ir.Model, Base.Target == t/openapi/components/schemas/Animal,
	// Mixins empty, own Properties == [bark], bark's Provenance.Pointer contains "/allOf/1".
}

func TestAllOf_ExtraRefsBecomeMixins(t *testing.T) { /* refs A, B + inline: Base=A? NO —
	two $refs, neither in a discriminator hierarchy and neither sole → NO Base, both Mixins.
	Assert Base == nil, Mixins == [A, B] in source order. */ }

func TestOneOf_WithDiscriminator(t *testing.T) { /* Pet: oneOf[Cat, Dog] + discriminator
	propertyName=petType, mapping{cat: '#/components/schemas/Cat'} — assert Union exclusive,
	WireTagged false, Discriminator.PropertyName == "petType",
	Mapping == {"cat": "t/openapi/components/schemas/Cat"}, and both Variants present. */ }

func TestAnyOf_IsNonExclusiveUnion(t *testing.T)   { /* Exclusive == false */ }
func TestOneOf_NullVariantCollapses(t *testing.T)  { /* oneOf[{type: string},{type: "null"}]
	→ property TypeRef{Target: t/prim/string, Nullable: true}; NO union in Types. */ }
func TestEnum_StringClosed(t *testing.T)           { /* enum [a,b] → Enum closed, 2 members */ }
func TestConst_BecomesLiteral(t *testing.T)        { /* const: "fixed" → Literal */ }
```

Write each spec fragment out fully in the test file (5–10 lines of YAML each — no
abbreviations in the actual code; the comments above are the assertions to implement).

- [ ] **Step 2: Run tests to verify they fail** — `go test ./frontend/openapi -run 'TestAllOf|TestOneOf|TestAnyOf|TestEnum|TestConst' -v` — FAIL.

- [ ] **Step 3: Implement `compose.go`** per **Produces**.

- [ ] **Step 4: Run tests** — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi
git commit -m "feat(frontend/openapi): lower composition, unions, discriminators, enums"
```

---

### Task 12: frontend/openapi — model detail: properties, constraints, visibility, XML, extensions, validation-only keywords

**Files:**
- Modify: `frontend/openapi/schema.go` (object lowering)
- Create: `frontend/openapi/constraints.go`
- Test: `frontend/openapi/model_test.go`

**Interfaces:**
- Consumes: Tasks 10–11.
- Produces (completing `Model` lowering):
  - Properties: source order from `Properties.All()`; `ID: propID(pointer+"/properties/<n>")`,
    `Name{Source: <key>, Canonical: canonicalWords(<key>)}`, `WireName: <key>`,
    `Required: slices.Contains(s.Required, key)`, `Type: schemaRef(...)`. Use-site
    precedence for `$ref` siblings (3.1) and annotations: `description`, `deprecated`,
    `default`, `readOnly/writeOnly` present at the referencing schema override the target's
    (§14 row: applied uniformly here, nowhere else).
  - `Default` via `valueFromNode(s.GetCore().GetPropertyNode("default"))`-equivalent (raw
    node, then `valueFromNode`); property-schema `format: password` → `Secret: true`;
    `readOnly` → `Visibility{Only: ["read"]}` (plus the §5.2 note), `writeOnly` →
    `Visibility{Only: ["create", "update"]}`; `deprecated: true` → `Deprecation{}`;
    `example`/`examples` → `Examples` (values through `valueFromNode`); property-level
    `xml` → `Property.XML`.
  - `constraints.go`: `func constraintsFromSchema(s *oas3.Schema) (*ir.Constraints, []ir.Diagnostic)`
    — **numerics from raw nodes only**: `minimum`/`maximum`/`multipleOf` read via
    `GetPropertyNode` and `ir.NewBigVal(node.Value)` (never the `*float64` fields — the
    no-float64 invariant would silently corrupt `0.1`-like bounds); malformed literal →
    `codeNumericPrecision` diagnostic. `exclusiveMinimum/Maximum`: bool arm → flags on
    Min/Max; numeric arm → `Min/Max` + flag true. `minLength/maxLength/pattern/minItems/
    maxItems/uniqueItems/minProperties/maxProperties` from the typed fields (they are ints —
    safe). Attach at the natural site: string/number constraints on the *property/usage*
    (`Property.Constraints`), container ones on `List.Constraints` (already in Task 10).
  - `additionalProperties: false` → `Model.Additional = "closed"`; `additionalProperties:
    <schema>` → `Model.AdditionalProps = &ir.AdditionalProps{Value: schemaRef(...)}`;
    `patternProperties` → `AdditionalProps.Patterns` (source order);
    `unevaluatedProperties: false` → `Additional = "closed_after_composition"` (§4.7
    carve-back).
  - Validation-only keywords, preserved verbatim + one `codeValidationOnlyKeyword` info
    diagnostic each (§4.7): `not` → `Extensions["openapi:not"]`, `if`/`then`/`else` →
    `Extensions["openapi:if-then-else"]` (one raw object with the present arms),
    `dependentSchemas` → `Extensions["openapi:dependentSchemas"]`, `contains`/`minContains`/
    `maxContains` → `Extensions["openapi:contains"]`, non-false `unevaluatedProperties` and
    all `unevaluatedItems` → `Extensions["openapi:unevaluated"]`. Raw JSON: serialize the
    keyword's YAML node (`yaml.Node` → JSON bytes) into `ir.RawValue`.
  - `x-*` on every lowered object (schemas here; ops/params/responses in Tasks 13–14):
    iterate `Extensions.All()`, key as `"openapi:" + <x-name>`, value = node serialized to
    JSON `RawValue`. One shared helper: `func extensionsFrom(ext *extensions.Extensions) (ir.Extensions, []ir.Diagnostic)`.
  - Schema `title` → `Docs.Summary`, `description` → `Docs.Description`,
    `externalDocs` → `Docs.ExternalDocs`.

- [ ] **Step 1: Write the failing tests** — `model_test.go`. Anchor tests (write these
verbatim), all using the Task 10 helpers:

```go
package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestModel_FourOptionalityStates(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      required: [reqPlain, reqNull]
      properties:
        reqPlain: {type: string}
        reqNull: {type: [string, "null"]}
        optPlain: {type: string}
        optNull: {type: [string, "null"]}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 4)
	byName := map[string]ir.Property{}
	for _, p := range m.Properties {
		byName[p.WireName] = p
	}
	assert.True(t, byName["reqPlain"].Required)
	assert.False(t, byName["reqPlain"].Type.Nullable)
	assert.True(t, byName["reqNull"].Required)
	assert.True(t, byName["reqNull"].Type.Nullable)
	assert.False(t, byName["optPlain"].Required)
	assert.False(t, byName["optPlain"].Type.Nullable)
	assert.False(t, byName["optNull"].Required)
	assert.True(t, byName["optNull"].Type.Nullable)
}

func TestConstraints_NumericPrecisionSurvives(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        ratio:
          type: number
          minimum: 0.30000000000000004
          maximum: 9007199254740993
          multipleOf: 0.1
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	c := m.Properties[0].Constraints
	require.NotNil(t, c)
	// Exact decimal strings — a float64 path would corrupt all three.
	assert.Equal(t, ir.BigVal("0.30000000000000004"), *c.Min)
	assert.Equal(t, ir.BigVal("9007199254740993"), *c.Max)
	assert.Equal(t, ir.BigVal("0.1"), *c.MultipleOf)
}

func TestModel_ValidationOnlyKeywordPreserved(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties: {a: {type: string}}
      not: {required: [b]}
`
	doc, diags := lowerSpec(t, spec)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	raw, ok := m.Extensions["openapi:not"]
	require.True(t, ok, "not-keyword must be preserved verbatim")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw))
	found := false
	for _, d := range diags {
		if d.Code == codeValidationOnlyKeyword {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}
```

Then add one table-driven test per remaining behavior, same helper pattern, each with its
own full inline spec: default with big literal (`default: 9007199254740993` →
`Property.Default.Num` exact); `readOnly` → `Visibility{Only: ["read"]}` and `writeOnly` →
`Visibility{Only: ["create","update"]}`; `format: password` → `Secret: true`;
`additionalProperties: false` → `Additional == "closed"`; `additionalProperties: {type:
integer}` → `AdditionalProps.Value`; `patternProperties` (two patterns, order preserved);
`unevaluatedProperties: false` → `"closed_after_composition"`; `x-rate-limit: 100` on the
schema → `Extensions["openapi:x-rate-limit"]` == `100`; `title`/`description` →
`Docs.Summary`/`Docs.Description`; `deprecated: true` → non-nil `Deprecation`; property
`xml: {name: n, attribute: true}` → `Property.XML{Name: "n", NodeType: "attribute"}`;
3.1 `$ref` + sibling `description` → referencing property's `Docs.Description` wins.

- [ ] **Step 2: Run tests to verify they fail.**

- [ ] **Step 3: Implement** per **Produces**.

- [ ] **Step 4: Run tests** — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi
git commit -m "feat(frontend/openapi): lower model detail, constraints, and lossless extensions"
```

---

### Task 13: frontend/openapi — operations, grouping, responses, errors, webhooks, callbacks

**Files:**
- Create: `frontend/openapi/operations.go`
- Test: `frontend/openapi/operations_test.go`

**Interfaces:**
- Consumes: `lowerer`, `schemaRef` (Task 10), Options.Grouping (Task 8); Task 14 provides
  `lowerParameters` and `lowerContent` — to keep this task independently testable, implement
  minimal stubs here (`func (l *lowerer) lowerParameters(...) ([]ir.Parameter, []ir.HTTPParamBinding)`
  returning empty slices, `func (l *lowerer) lowerPayload(...) *ir.Payload` returning nil)
  and let Task 14 replace their bodies.
- Produces:
  - `func (l *lowerer) lowerService() ir.Service` — one Service per document:
    `ID: serviceID(l.srcIndex)`, `Name` from `doc.Info.Title`, `Docs` from info
    description, groups per grouping strategy.
  - Grouping (the policy seam): `GroupByTags` (default) — one `OperationGroup` per first tag
    of each operation, group `Name.Source` = tag name, group `Docs` from the matching
    `doc.Tags` entry; untagged operations land in a group with `Name{Source: "",
    Hint: "default"}`. Operations *not* grouped by declared tags (i.e. the strategy itself)
    are declared facts; `GroupByPathPrefix` — first path segment → group, and every
    operation lowered under it gets `Provenance.Inferred = "group-path-prefix"`.
    All tags (first and rest) also go to `Operation.Tags` verbatim; `doc.Tags` metadata →
    `Document.TagDefs`.
  - Operation lowering — for each `(path, item)` in `doc.Paths.All()` and each method
    operation on the `PathItem` (`Get()/Put()/Post()/Delete()/Options()/Head()/Patch()/
    Trace()`): pointer = `ptr("paths", path, method)`; `ID: opID(pointer)`;
    `Name{Source: operationId or "", Canonical: canonicalWords(operationId), Hint (when no
    operationId): canonicalWords(method + " " + path)}`; summary/description →
    `Docs`; `deprecated` → `Deprecation`; security override → `Auth` (Task 15's
    `lowerSecurityRequirements`, stub here) — **note §7.2: an operation with `security: []`
    gets `Auth = []ir.AuthRequirement{}` (empty non-nil = explicitly public); absent security
    → nil** (inherit service default);
    `Bindings.HTTP = []ir.HTTPBinding{{Method: strings.ToUpper(method), URITemplate: path}}`
    (OpenAPI paths are already RFC 6570-compatible level-1 templates).
  - Responses: each status entry (source order) with status < 400 → `ir.Response{Conditions:
    ResponseConditions{StatusCodes: [statusRange(code)]}, Payload: lowerPayload(content),
    Headers: lowerResponseHeaders(...)}`; `statusRange`: `"200"` → `{200,200}`, `"4XX"` →
    `{400,499}`, `"default"` → `{0,0}`. Entries ≥ 400 and `default` → `ir.ErrorCase`.
    ir-design §7.2 gives `ErrorCase.Type` a *single* error-model reference while OpenAPI
    error responses carry full content maps, so: lower **every** content entry's schema into
    the type registry (nothing dropped), set `ErrorCase.Type` from the first content entry,
    and when more than one media type exists preserve the raw content map under
    `ErrorCase.Extensions["openapi:content"]` with an info diagnostic (candidate ir-design
    clarification — surface in the PR description). `Fault`: 4XX → "client", 5XX → "server", default →
    `""` + `Conditions {0,0}` catch-all. Response `description` → `Docs`.
  - Response headers → `[]ir.Property`: each `ReferencedHeader` → Property with
    `WireName: <header name>`, `Type: schemaRef(header schema)`, `Required` from the header's
    required field, ID `propID(<response pointer>/headers/<name>)`.
  - Webhooks (`doc.Webhooks.All()`): lower each entry's operations exactly like path
    operations, pointer `ptr("webhooks", name, method)`, with `HTTPBinding.IsWebhook: true`,
    grouped into an `OperationGroup{Name: {Source: "webhooks"}}`.
  - Callbacks: each operation's callbacks → lower the callback expression's operations as
    Operations (pointer-derived IDs under the callback pointer, registered in the same group)
    and attach `HTTPBinding.Callbacks = []ir.Callback{{Expression: <expr>, Operations:
    [their OpIDs]}}`.
  - Links on responses → `Response.Extensions["openapi:links"]` raw (promotable later, §14).
  - `PathItem`-level `parameters` merge into each operation (use-site precedence: an
    operation parameter with same (name, in) overrides the path-item one). `PathItem`-level
    `servers` → `Extensions["openapi:servers"]` on each of its operations (Server scoping
    below document level is out of the §10 model) + info diagnostic.

- [ ] **Step 1: Write the failing tests** — `operations_test.go`. Add this helper beside
`lowerSpec` (it drives components + service lowering, which is all Parse adds later):

```go
// lowerServiceSpec lowers components and the service layer of src.
func lowerServiceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags := func() (*ir.Document, []ir.Diagnostic) {
		loadedDoc, loadDiags, err := load(t.Context(), 0, frontend.Source{Path: "spec.yaml", Data: []byte(src)}, Options{}.withDefaults())
		require.NoError(t, err)
		require.NotNil(t, loadedDoc)
		l := newLowerer(0, loadedDoc, Options{}.withDefaults())
		l.lowerComponentSchemas()
		l.out.Services = []ir.Service{l.lowerService()}
		return l.out, append(loadDiags, l.diags...)
	}()
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}
```

Anchor tests (verbatim), then the enumerated remainder:

```go
func TestGrouping_ByFirstTag(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
tags:
  - {name: users, description: User ops}
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users, admin]
      responses: {"200": {description: ok}}
  /misc:
    get:
      operationId: misc
      responses: {"200": {description: ok}}
`
	doc, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 2)
	byName := map[string]ir.OperationGroup{}
	for _, g := range svc.Groups {
		byName[g.Name.Source] = g
	}
	users, ok := byName["users"]
	require.True(t, ok)
	assert.Equal(t, "User ops", users.Docs.Description)
	require.Len(t, users.Operations, 1)
	op := users.Operations[0]
	assert.Equal(t, ir.OpID("op/openapi/paths/~1users/get"), op.ID)
	assert.Equal(t, []string{"users", "admin"}, op.Tags)
	def, ok := byName[""]
	require.True(t, ok, "untagged op lands in the default group")
	assert.Equal(t, "default", def.Name.Hint)
	require.Len(t, doc.TagDefs, 1) // declared tag metadata registered once
}

func TestResponses_ErrorSplitAndRanges(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /w:
    get:
      operationId: w
      responses:
        "200": {description: ok}
        "404":
          description: missing
          content: {application/json: {schema: {type: object, properties: {msg: {type: string}}}}}
        "5XX": {description: server oops}
        default: {description: anything else}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	require.Len(t, op.Responses, 1)
	assert.Equal(t, []ir.StatusRange{{From: 200, To: 200}}, op.Responses[0].Conditions.StatusCodes)
	require.Len(t, op.Errors, 3)
	assert.Equal(t, []ir.StatusRange{{From: 404, To: 404}}, op.Errors[0].Conditions.StatusCodes)
	assert.Equal(t, "client", op.Errors[0].Fault)
	assert.NotEmpty(t, op.Errors[0].Type.Target, "404 error model lowered and referenced")
	assert.Equal(t, []ir.StatusRange{{From: 500, To: 599}}, op.Errors[1].Conditions.StatusCodes)
	assert.Equal(t, "server", op.Errors[1].Fault)
	assert.Equal(t, []ir.StatusRange{{From: 0, To: 0}}, op.Errors[2].Conditions.StatusCodes)
	assert.Equal(t, "", op.Errors[2].Fault)
}

func TestOperation_ExplicitlyPublicSecurity(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /open:
    get:
      operationId: open
      security: []
      responses: {"200": {description: ok}}
  /inherits:
    get:
      operationId: inherits
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	ops := map[string]ir.Operation{}
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			ops[op.Name.Source] = op
		}
	}
	require.NotNil(t, ops["open"].Auth, "security: [] must be the empty non-nil slice")
	assert.Empty(t, ops["open"].Auth)
	assert.Nil(t, ops["inherits"].Auth, "absent security inherits the service default")
}
```

Remaining behaviors, one test each with a full inline spec: webhook entry → operation with
`IsWebhook: true` in the `webhooks` group and pointer-derived ID under `/webhooks/`;
response headers → `Response.Headers` Properties (name, required, typed); callbacks → ops
registered + `HTTPBinding.Callbacks` expression/OpIDs; path-item-level `parameters` merged
with operation-level override on same (name, in); links preserved under
`Response.Extensions["openapi:links"]`; `GroupByPathPrefix` option → groups by first segment
and every operation `Provenance.Inferred == "group-path-prefix"`; no-operationId op gets
empty `Name.Source` and method+path `Hint`.

- [ ] **Step 2: Run tests to verify they fail.**

- [ ] **Step 3: Implement `operations.go`** per **Produces** (with Task 14/15 stubs).

- [ ] **Step 4: Run tests** — Expected: PASS.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi
git commit -m "feat(frontend/openapi): lower operations, groups, responses, errors, webhooks"
```

---

### Task 14: frontend/openapi — parameters, bodies, content, multipart

**Files:**
- Create: `frontend/openapi/params.go`, `frontend/openapi/content.go`
- Modify: `frontend/openapi/operations.go` (replace stubs)
- Test: `frontend/openapi/params_test.go`, `frontend/openapi/content_test.go`

**Interfaces:**
- Consumes: Tasks 10–13.
- Produces:
  - `func (l *lowerer) lowerParameters(params []*soa.ReferencedParameter, opPointer string) ([]ir.Parameter, []ir.HTTPParamBinding, []ir.Diagnostic)`
    — for each parameter (resolved via `GetObject()`): logical side →
    `ir.Parameter{Name{Source: name, Canonical: canonicalWords(name)}, Type:
    schemaRef(schema, <param pointer>/schema, name), Required: required (path params always
    required), Default (from schema default; use-site precedence), Constraints (from schema —
    reuse `constraintsFromSchema`), Docs, Deprecation, Examples, Extensions}`. Protocol side →
    `ir.HTTPParamBinding{Param: name, Location: <in> (path|query|header|cookie — map to
    HTTPLocation constants; 3.2 querystring if exposed), WireName: name, Style: style,
    Explode: explode pointer, AllowReserved: allowReserved}`. Defaults per OpenAPI spec when
    style/explode absent: query→form/true, path→simple/false, header→simple/false,
    cookie→form/true — materialize the *resolved* values (declared facts, not policy).
    Content-style parameters (`content: {<mt>: schema}` instead of `schema`) →
    `HTTPParamBinding.ContentType = <mt>` + schema lowered from the media-type entry.
    Bindings attach to the operation's `HTTPBinding.ParamBindings`.
  - `func (l *lowerer) lowerPayload(content *sequencedmap.Map[string, *soa.MediaType], pointer string, hint string) (*ir.Payload, []ir.Diagnostic)`
    — one `ir.Content` per media type, **all kept**, source order: `MediaType: <key>`,
    `Type: schemaRef(mt schema, <pointer>/content/<mt>/schema, hint)`, `Examples` from
    `example`/`examples`, `Extensions`. Request bodies: `requestBody.required` → note —
    the IR expresses body optionality via `Operation.Request == nil` vs present; a
    non-required body stays present with `Payload.Extensions["openapi:required"] = false`
    raw + info diagnostic (revisit as a candidate ir-design clarification).
    `RequestContentTypes` on the HTTPBinding = the media-type keys, priority = source order.
  - Multipart/form encoding: for `multipart/*` and `application/x-www-form-urlencoded`
    content, the media type's `Encoding` map → `Content.Encoding
    map[ir.PropID]ir.PartEncoding` keyed by the body model's property IDs:
    `ContentTypes` (split the encoding `contentType` on ","), per-part `Headers` (lowered
    like response headers), `Style/Explode` from the encoding object, `Multi` true when the
    part's schema is an array (per-item parts semantics), `Filename` true when the part
    schema is `string`+`binary`/`byte` (file part).
  - Binary bodies: content whose schema is `string`+`binary` (or absent schema on
    `application/octet-stream`) → `Content.File = &ir.FileInfo{IsText: false, ContentTypes:
    [<the media type key>]}` and `Type` = prim `bytes` ref.
  - 3.2 `itemSchema`/`itemEncoding` (sequential media types): *verify at impl time*; if
    exposed → `Content.Item`/`Content.ItemEncoding`; if not → preserve raw under
    `Content.Extensions["openapi:itemSchema"]` + info diagnostic.

- [ ] **Step 1: Write the failing tests** — `params_test.go` + `content_test.go`, reusing
`lowerServiceSpec`. Anchor tests (verbatim):

```go
func TestParams_LocationsAndSerializationDefaults(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /items/{id}:
    get:
      operationId: getItem
      parameters:
        - {name: id, in: path, required: true, schema: {type: string, format: uuid}}
        - {name: limit, in: query, schema: {type: integer, format: int32, default: 20}}
        - {name: filter, in: query, style: deepObject, explode: true, schema: {type: object, properties: {kind: {type: string}}}}
        - {name: X-Trace, in: header, schema: {type: string}}
        - {name: session, in: cookie, schema: {type: string}}
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	require.Len(t, op.Params, 5)
	require.Len(t, op.Bindings.HTTP, 1)
	bindings := map[string]ir.HTTPParamBinding{}
	for _, b := range op.Bindings.HTTP[0].ParamBindings {
		bindings[b.Param] = b
	}
	require.Len(t, bindings, 5, "every logical param bound exactly once")

	id := bindings["id"]
	assert.Equal(t, ir.HTTPLocationPath, id.Location)
	assert.Equal(t, "simple", id.Style) // resolved OpenAPI default
	require.NotNil(t, id.Explode)
	assert.False(t, *id.Explode)

	limit := bindings["limit"]
	assert.Equal(t, ir.HTTPLocationQuery, limit.Location)
	assert.Equal(t, "form", limit.Style)
	require.NotNil(t, limit.Explode)
	assert.True(t, *limit.Explode)

	assert.Equal(t, "deepObject", bindings["filter"].Style)
	assert.Equal(t, ir.HTTPLocationHeader, bindings["X-Trace"].Location)
	assert.Equal(t, ir.HTTPLocationCookie, bindings["session"].Location)

	params := map[string]ir.Parameter{}
	for _, p := range op.Params {
		params[p.Name.Source] = p
	}
	assert.True(t, params["id"].Required, "path params are always required")
	require.NotNil(t, params["limit"].Default)
	assert.Equal(t, ir.BigVal("20"), params["limit"].Default.Num)
}

func TestContent_AllMediaTypesKeptInOrder(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /docs:
    post:
      operationId: createDoc
      requestBody:
        required: true
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
          application/xml: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"201": {description: created}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 2, "no primary-content selection in the IR")
	assert.Equal(t, "application/json", op.Request.Contents[0].MediaType)
	assert.Equal(t, "application/xml", op.Request.Contents[1].MediaType)
	assert.Equal(t, []string{"application/json", "application/xml"},
		op.Bindings.HTTP[0].RequestContentTypes)
}

func TestContent_MultipartPartEncoding(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                meta: {type: object, properties: {k: {type: string}}}
                file: {type: string, format: binary}
            encoding:
              meta:
                contentType: application/json
                headers:
                  X-Part: {schema: {type: string}}
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	content := svc.Groups[0].Operations[0].Request.Contents[0]
	metaProp := ir.PropID("p/openapi" + ptr("paths", "/upload", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "meta"))
	enc, ok := content.Encoding[metaProp]
	require.True(t, ok, "encoding keyed by the part property's PropID; got keys %v", content.Encoding)
	assert.Equal(t, []string{"application/json"}, enc.ContentTypes)
	require.Len(t, enc.Headers, 1)
	assert.Equal(t, "X-Part", enc.Headers[0].WireName)

	fileProp := ir.PropID("p/openapi" + ptr("paths", "/upload", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "file"))
	fileEnc, ok := content.Encoding[fileProp]
	require.True(t, ok, "binary part gets a synthesized file PartEncoding")
	assert.True(t, fileEnc.Filename)
}
```

Remaining behaviors, one test each with a full inline spec: content-style parameter
(`content: {application/json: {schema: ...}}` instead of `schema`) → binding
`ContentType == "application/json"`; `application/octet-stream` body with
`{type: string, format: binary}` → `Content.File` non-nil, `IsText` false, `Type` targets
`t/prim/bytes`; non-required requestBody → `Payload.Extensions["openapi:required"]` false +
info diagnostic; array-typed multipart part → `PartEncoding.Multi` true; param schema
constraints (e.g. `maximum`) land on `Parameter.Constraints` via `constraintsFromSchema`.

- [ ] **Step 2: Run tests to verify they fail.**

- [ ] **Step 3: Implement; replace Task 13 stubs.**

- [ ] **Step 4: Run tests** — Expected: PASS (including Task 13's suite, now with real
params/bodies).

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi
git commit -m "feat(frontend/openapi): lower parameters, bodies, and multipart encoding"
```

---

### Task 15: frontend/openapi — auth, servers, document assembly, Parse()

**Files:**
- Create: `frontend/openapi/auth.go`, `frontend/openapi/meta.go`, `frontend/openapi/openapi.go`
- Test: `frontend/openapi/auth_test.go`, `frontend/openapi/parse_test.go`

**Interfaces:**
- Consumes: everything above; `frontend.Frontend` contract (Task 7).
- Produces:
  - `auth.go`: `func (l *lowerer) lowerSecuritySchemes()` — each
    `components/securitySchemes/<name>` → `ir.AuthScheme` in `Document.Auth` keyed
    `authIDFor(name)`: `type: apiKey` → `Kind: apiKey` + `In`/`KeyName`; `type: http` →
    `http_basic`/`http_bearer` by scheme (other schemes → `Kind: custom` + `Scheme`),
    `BearerFormat`; `type: oauth2` → `Kind: oauth2`, each flow present →
    `ir.OAuthFlow{Kind: authorization_code|client_credentials|implicit|password,
    AuthorizationURL, TokenURL, RefreshURL, Scopes}`; `type: openIdConnect` →
    `openid_connect` + `OpenIDConnectURL`; `type: mutualTLS` → `mutual_tls`. 3.2
    `oauth2MetadataUrl`/device flow: *verify at impl time*, else raw Extensions.
    `func (l *lowerer) lowerSecurityRequirements(reqs []*soa.SecurityRequirement) []ir.AuthRequirement`
    — OR-of-ANDs (§9): each requirement object → one `AuthRequirement`; each `<schemeName>:
    [scopes]` member → `SchemeUse{Scheme: authIDFor(name), Scopes: scopes}`; an empty
    requirement object `{}` → `AuthRequirement{Schemes: []}` ("no auth is one acceptable
    choice"). Document-level security → `Service.Auth`; op-level per Task 13's rule.
  - `meta.go`: `doc.Info` → `Document.Name/Version/Docs/Contact/License/TermsOfService`;
    `doc.Servers` → `[]ir.Server{URLTemplate: url, Description → Docs, Variables:
    [{Name, Default, Enum, Docs}]}` (+ 3.2 server `name` if exposed); document extensions →
    `Document.Extensions` namespaced.
  - `openapi.go` — the `Frontend` implementation, tying the architecture §2.1 phases
    together:

    ```go
    // Frontend lowers OpenAPI 3.x documents into the IR.
    type Frontend struct{}

    // New returns the OpenAPI frontend.
    func New() *Frontend { return &Frontend{} }

    func (*Frontend) Formats() []frontend.SourceFormat {
        return []frontend.SourceFormat{
            {Name: "openapi", Version: "3.0"},
            {Name: "openapi", Version: "3.1"},
            {Name: "openapi", Version: "3.2"},
        }
    }

    // Parse implements frontend.Frontend. Milestone 1 accepts exactly one root
    // source; multi-document stitching belongs to the link pass.
    func (f *Frontend) Parse(ctx context.Context, sources []frontend.Source, opts frontend.Options) (*ir.Document, []ir.Diagnostic, error) {
        if len(sources) != 1 {
            return nil, nil, fmt.Errorf("openapi: expected exactly one source, got %d", len(sources))
        }
        formatOpts, err := optionsFrom(opts) // nil FormatOptions → defaults; wrong type → error
        if err != nil {
            return nil, nil, err
        }
        loadedDoc, diags, err := load(ctx, 0, sources[0], formatOpts)
        if err != nil || loadedDoc == nil {
            return nil, diags, err
        }
        l := newLowerer(0, loadedDoc, formatOpts)
        out := l.run() // components schemas → auth → service/operations → meta; assembles Document
        out.Diagnostics = append(diags, l.diags...)
        return out, out.Diagnostics, nil
    }
    ```

    `run()` order matters: named component schemas first (so refs from operations find
    interned IDs), then security schemes, then the service walk, then meta. Set
    `Document.IRVersion = ir.IRVersion`, `Sources = [loadedDoc.Source]`.
  - `parse_test.go`: end-to-end — a ~60-line petstore-flavored spec exercising models,
    an inline hoist, a union, params, a body, responses+error, security, servers; assert
    top-level counts and spot-check nodes; **plus** registration:
    `reg := frontend.NewRegistry(); require.NoError(t, reg.Register(openapi.New()))` and
    lookup by `{openapi, 3.1}`. **Plus the round-trip property**: `Parse` result →
    `json.Marshal` → `json.Unmarshal` → `cmp.Diff` empty.

- [ ] **Step 1: Write the failing tests.** `auth_test.go` — one table-driven test over the
five scheme kinds plus one requirements test (anchor, verbatim):

```go
func TestAuth_RequirementsOrOfAnds(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - {}
  - key: []
  - oauth: [read, write]
    key: []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
    oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://a.example/token
          scopes: {read: r, write: w}
`
	doc, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)

	keyID := authIDFor("key")
	scheme, ok := doc.Auth[keyID]
	require.True(t, ok)
	assert.Equal(t, ir.AuthKindAPIKey, scheme.Kind)
	assert.Equal(t, "header", scheme.In)
	assert.Equal(t, "X-Key", scheme.KeyName)

	oauth := doc.Auth[authIDFor("oauth")]
	require.Len(t, oauth.Flows, 1)
	assert.Equal(t, "client_credentials", oauth.Flows[0].Kind)
	assert.Equal(t, "https://a.example/token", oauth.Flows[0].TokenURL)

	require.Len(t, svc.Auth, 3) // OR across options, source order
	assert.Empty(t, svc.Auth[0].Schemes, "empty requirement = no-auth is one acceptable choice")
	require.Len(t, svc.Auth[1].Schemes, 1)
	assert.Equal(t, keyID, svc.Auth[1].Schemes[0].Scheme)
	require.Len(t, svc.Auth[2].Schemes, 2, "one option, two schemes ANDed")
	assert.Equal(t, []string{"read", "write"}, svc.Auth[2].Schemes[0].Scopes)
}
```

(`AuthKind` constants follow the §9 list with Go names `AuthKindAPIKey`, `AuthKindHTTPBasic`,
`AuthKindHTTPBearer`, `AuthKindOAuth2`, `AuthKindOpenIDConnect`, `AuthKindMutualTLS`, ...,
`AuthKindCustom` — string values exactly as §9 spells them.)

`parse_test.go` — end-to-end through the public contract (anchor, verbatim; the spec constant
is written out in full in the test file — it must exercise: two named schemas with a `$ref`
between them, one inline hoist, one oneOf union, path+query params, a JSON request body,
200 + 404 + default responses, document security + one public op, tags, and one server with
a variable):

```go
package openapi_test // external test package — exercises only the public API

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/frontend/openapi"
	"github.com/dexpace/morphic/ir"
)

const petstore = `<the full ~70-line spec described above>`

func parsePetstore(t *testing.T) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Parse(context.Background(),
		[]frontend.Source{{Path: "petstore.yaml", Data: []byte(petstore)}}, frontend.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

func TestParse_EndToEnd(t *testing.T) {
	t.Parallel()
	doc, diags := parsePetstore(t)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
	assert.Equal(t, ir.IRVersion, doc.IRVersion)
	assert.NotEmpty(t, doc.Name)
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Sources, 1)
	assert.Len(t, doc.Sources[0].Hash, 64)
	// Spot-checks: named schema present under its pointer ID; at least one
	// hoisted anonymous type; the union survived as a Union node.
	// (Write these against the concrete spec content.)
}

func TestParse_RegistersInRegistry(t *testing.T) {
	t.Parallel()
	reg := frontend.NewRegistry()
	require.NoError(t, reg.Register(openapi.New()))
	got, ok := reg.Lookup(frontend.SourceFormat{Name: "openapi", Version: "3.1"})
	require.True(t, ok)
	assert.NotNil(t, got)
}

func TestParse_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	doc, _ := parsePetstore(t)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(raw, &back))
	if diff := cmp.Diff(doc, &back); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
	again, err := json.Marshal(&back)
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(again), "marshal must be deterministic")
}
```

Also in this step: `optionsFrom(opts frontend.Options) (Options, error)` — nil
`FormatOptions` → `Options{}.withDefaults()`; a value of type `Options` → its
`withDefaults()`; any other type → error `"openapi: FormatOptions must be openapi.Options,
got %T"`.

- [ ] **Step 2: Run tests to verify they fail.**
- [ ] **Step 3: Implement `auth.go`, `meta.go`, `openapi.go`; remove the last Task 13 stubs.**
- [ ] **Step 4: Run tests** — full `go test ./...` — Expected: PASS.
- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add frontend/openapi
git commit -m "feat(frontend/openapi): assemble documents and implement the frontend contract"
```

---

### Task 16: pass/validate — referential integrity

**Files:**
- Create: `pass/doc.go`, `pass/validate.go`
- Test: `pass/validate_test.go`

**Interfaces:**
- Consumes: `ir` only (layering rule — no frontend imports).
- Produces: `func Validate(doc *ir.Document) []ir.Diagnostic` — pure, no mutation. Checks
  (each with a stable code, severity error unless noted):
  - `ir/dangling-type-ref` — every `TypeRef.Target` in the document (walk: model
    properties/Base/Implements/Mixins/AdditionalProps, union variants, scalar bases, list
    elem, map key/value, tuple elems, operation params/payload contents/responses/errors,
    channel messages via Payload, encoding WireType) resolves in `doc.Types`. Keep the
    TypeRef walker private to `pass` for now (it is the first consumer); promote it to `ir`
    only when a second pass needs it.
  - `pass/discriminator-missing-variant` — every `Discriminator.Mapping` TypeID exists AND
    is one of the union's variant targets (or a subtype when on a model base).
  - `pass/duplicate-wire-name` — within one model's own `Properties`, `WireName` values are
    unique.
  - `pass/param-binding-mismatch` — per `HTTPBinding`: every `ParamBindings.Param` names an
    `Operation.Params` entry; every logical param is bound at most once per non-host
    location (host is additive, §8.1); unbound params → warning (body-carried operations
    legitimately bind nothing).
  - `pass/oneway-with-responses` — `OneWay && len(Responses) > 0` → error (§7.2).
  - `pass/args-outside-graphql` — `Property.Args` non-empty on a model not reachable from a
    GraphQL binding → error (§5.1 scope rule; with no GraphQL frontend yet, any Args is a
    violation).
  - `pass/dangling-auth-ref` — `SchemeUse.Scheme` resolves in `doc.Auth`.
  - Explicitly legal (regression tests, not checks): duplicate enum member *values*
    (protobuf allow_alias, §4.5); multiple operations sharing method+path when
    `SharedRoute` (§8.1).

- [ ] **Step 1: Write the failing tests** — `pass/validate_test.go` (external package
`pass_test`), one in-memory document per check. The shared pattern and two anchors
(verbatim):

```go
package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// validDoc returns a minimal internally consistent document that every case
// mutates. Keep it tiny: one model referencing one primitive.
func validDoc() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion, Name: "t", Version: "1",
		Types: ir.TypeRegistry{
			"t/prim/string": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/prim/string"}, Prim: "string"},
			"t/m": &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/m"}, Properties: []ir.Property{
				{ID: "p/m/a", WireName: "a", Type: ir.TypeRef{Target: "t/prim/string"}},
			}},
		},
	}
}

func codes(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

func TestValidate_CleanDocumentHasNoDiagnostics(t *testing.T) {
	t.Parallel()
	assert.Empty(t, pass.Validate(validDoc()))
}

func TestValidate_DanglingTypeRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	m := doc.Types["t/m"].(*ir.Model)
	m.Properties[0].Type.Target = "t/nowhere"
	diags := pass.Validate(doc)
	require.NotEmpty(t, diags)
	assert.Contains(t, codes(diags), "ir/dangling-type-ref")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}
```

Then one mutation test per remaining code (`pass/discriminator-missing-variant`,
`pass/duplicate-wire-name`, `pass/param-binding-mismatch` — both an unknown param name and a
double-bound param —, `pass/oneway-with-responses`, `pass/args-outside-graphql`,
`pass/dangling-auth-ref`), and the two explicitly-legal regression tests: an `Enum` with two
members sharing a `Value` produces no diagnostics; two operations with identical
method+path where both bindings set `SharedRoute: true` produce no diagnostics.

- [ ] **Step 2: Run tests to verify they fail** — `go test ./pass -v` — FAIL.
- [ ] **Step 3: Implement `pass/validate.go`** (small per-check functions ≤70 lines each,
one shared TypeRef walker).
- [ ] **Step 4: Run tests** — Expected: PASS.
- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add pass
git commit -m "feat(pass): add referential-integrity validate pass"
```

---

### Task 17: corpora — capability conformance + golden snapshots

**Files:**
- Create: `testdata/conformance/openapi/*.yaml` (one per capability), `testdata/golden/openapi/petstore.yaml` + `.golden.json`, `frontend/openapi/conformance_test.go`, `frontend/openapi/golden_test.go`

**Interfaces:**
- Consumes: `openapi.New().Parse`, `irtest.CompareGolden`, `pass.Validate`. Layering note:
  production code in `frontend/openapi` must not import `pass`, so the corpus test in
  `frontend/openapi` asserts frontend output only, and a *second* thin test,
  `pass/validate_corpus_test.go`, walks the same `testdata/conformance/openapi` files through
  the frontend and asserts they validate clean. Test files may import across layers — the
  architecture test (Task 18) checks non-test files only.
- Produces: one minimal spec per OpenAPI-expressible row of `ir-spec-matrix.md`, each with a
  focused assertion + golden. The conformance set (file → asserted capability):
  `named-types`, `inline-types` (hoist once, hint), `allof-inheritance` (Base),
  `allof-mixins`, `allof-inline-merge`, `oneof-discriminated` (+mapping IDs),
  `anyof-untagged`, `negation-not` (verbatim extension + diagnostic), `enum-string`,
  `enum-numeric` (BigVal member values), `scalar-format` (unknown format → Scalar+Encoding),
  `encoding-byte` (base64 Scalar), `nullability-four-states` (3.1: required×nullable),
  `nullable-30` (3.0 spelling, same IR), `defaults` (big-literal precision), `constraints`
  (decimal-string bounds), `readonly-writeonly` (Visibility), `recursive` (self-ref
  terminates), `maps` (additionalProperties/patternProperties/closed/
  closed_after_composition), `tuples-prefixitems`, `literal-const`,
  `tags-grouping`, `http-binding` (methods, URITemplate), `param-styles` (form/explode
  defaults, deepObject, content-param), `multi-content` (two media types kept),
  `multipart-encoding` (PartEncoding), `per-status-errors` (ranges, fault, default
  catch-all), `webhooks`, `callbacks`, `deprecation`, `examples`, `docs-summary-desc`,
  `extensions-x` (namespacing), `servers-variables`, `security-schemes` (all five kinds),
  `security-or-and` (OR-of-ANDs + empty option + explicitly-public op).
  Each conformance test: parse → no error-severity diagnostics (except the files that
  *assert* a diagnostic) → capability-specific assertions → `irtest.CompareGolden` against
  `<file>.golden.json` → `pass.Validate` returns no errors (in the pass-side test).
  The golden test: full petstore-style doc → `CompareGolden`; regenerate via
  `go test ./frontend/openapi -run TestGolden -update`.

- [ ] **Step 1: Write `conformance_test.go` as a table over the corpus directory** (file
name → assertion func), plus 5–6 spec files and run — FAIL (missing files/assertions).
- [ ] **Step 2: Author every corpus file listed above** (each 10–40 lines of YAML, exactly
one capability each) and its assertion.
- [ ] **Step 3: Generate goldens**: `go test ./frontend/openapi -update ./...`; then re-run
without `-update` — PASS. **Read the generated goldens** — hand-verify at least:
IDs are pointer-derived, no float64-looking mangled numbers, unions not collapsed,
all content types present.
- [ ] **Step 4: Add `pass/validate_corpus_test.go`** (every corpus doc validates clean).
- [ ] **Step 5: Full run** — `go test ./...` — Expected: PASS.
- [ ] **Step 6: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add testdata frontend/openapi pass
git commit -m "test(frontend/openapi): add capability-conformance and golden corpora"
```

---

### Task 18: architecture test + CI gate

**Files:**
- Create: `arch_test.go` (repo root, `package morphic_test` — requires a root `doc.go` with
  `package morphic` or place the test under `internal/archtest/`; choose
  `internal/archtest/arch_test.go` to avoid a root package)
- Create: `.github/workflows/gate.yml`

**Interfaces:**
- Consumes: source tree layout; the layering rules (architecture §3).
- Produces: a stdlib-only import-graph assertion — for each rule, parse every non-test
  `.go` file with `go/parser` (`parser.ImportsOnly`) and assert allowed import prefixes:
  - `ir/` → stdlib only (no dot in the import path — stdlib heuristic — with an explicit
    allowlist check that fails on any `github.com/...`).
  - `frontend/` (contract) → stdlib + `github.com/dexpace/morphic/ir`.
  - `frontend/openapi/` → stdlib + `ir` + `frontend` + `github.com/speakeasy-api/openapi/...`
    (+ its yaml dependency).
  - `pass/` → stdlib + `ir`.
  - Nothing outside `frontend/openapi` imports `github.com/speakeasy-api/...`.

- [ ] **Step 1: Write the failing test** (`internal/archtest/arch_test.go`):

```go
package archtest_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const module = "github.com/dexpace/morphic"

// rules maps a directory (relative to repo root) to its allowed non-stdlib
// import prefixes. Test files are exempt; layering applies to production code.
var rules = map[string][]string{
	"ir":               {},
	"ir/irtest":        {module + "/ir", "github.com/google/go-cmp"},
	"frontend":         {module + "/ir"},
	"frontend/openapi": {module + "/ir", module + "/frontend", "github.com/speakeasy-api/openapi"},
	"pass":             {module + "/ir"},
}

func TestImportGraph_LayeringHolds(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for dir, allowed := range rules {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return err
				}
				f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				require.NoError(t, perr)
				for _, imp := range f.Imports {
					ip := strings.Trim(imp.Path.Value, `"`)
					if !strings.Contains(strings.SplitN(ip, "/", 2)[0], ".") {
						continue // stdlib: first path element has no dot
					}
					// An empty allowlist (the "ir" rule) forbids every
					// non-stdlib import.
					if !hasAllowedPrefix(ip, allowed) {
						t.Errorf("%s imports %q: not allowed for %s (allowed: %v)", path, ip, dir, allowed)
					}
				}
				return nil
			})
			require.NoError(t, err)
		})
	}
}
```

(Complete `repoRoot` — walk up from `runtime.Caller` to the `go.mod` dir — and
`hasAllowedPrefix`. Note `ir/irtest` is nested under `ir/`: exclude the `irtest` subtree when
walking the `ir` rule.)

Note the `frontend/openapi` allowlist intentionally omits the yaml module until the
implementation reveals which one the schemas expose (`gopkg.in/yaml.v3` or speakeasy's fork);
the first run tells you — add exactly that one.

- [ ] **Step 2: Run** — `go test ./internal/archtest -v` — Expected: PASS if layering held
(it should); deliberately add a forbidden import locally to see it fail, then revert.

- [ ] **Step 3: Add the CI gate** (repo rule: one bundled gate job):

`.github/workflows/gate.yml`:

```yaml
name: gate
on:
  push:
    branches: [main]
  pull_request:
jobs:
  gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26.3"
      - name: gofmt
        run: test -z "$(gofmt -l .)"
      - name: vet
        run: go vet ./...
      - name: lint
        uses: golangci/golangci-lint-action@v8
      - name: build
        run: go build ./...
      - name: test
        run: go test ./...
```

- [ ] **Step 4: Full gate locally** — all four commands clean.
- [ ] **Step 5: Commit**

```bash
git add internal/archtest .github
git commit -m "test: enforce import-graph layering and add CI gate"
```

---

### Task 19: engine — orchestration (sniff → frontend → passes)

**Files:**
- Create: `engine/doc.go`, `engine/sniff.go`, `engine/engine.go`
- Test: `engine/sniff_test.go`, `engine/engine_test.go`

**Interfaces:**
- Consumes: `frontend.Registry`/`Frontend`/`Source`/`Options` (Task 7), `openapi.New()`
  (Task 15), `pass.Validate` (Task 16). Layering: `engine` imports everything below it —
  this is the ONLY production package that may import both a frontend and `pass`.
- Produces:
  - `func Sniff(data []byte) (frontend.SourceFormat, error)` — probe-decode the bytes with
    the yaml module (YAML is a JSON superset, so one decode handles both) into
    `struct{ OpenAPI string "yaml:\"openapi\""; Swagger string "yaml:\"swagger\"" }`:
    `openapi: "3.X.Y"` → `{Name: "openapi", Version: "3.X"}` (major.minor prefix);
    `swagger: "2.0"` → error `"swagger 2.0 is not supported yet (planned: lift into the
    openapi frontend)"`; neither key → error `"unrecognized spec format"`. Undecodable bytes
    → wrapped decode error.
  - `type RunOptions struct{ FormatOptions any; SkipValidate bool }` — `FormatOptions`
    forwarded verbatim to the frontend.
  - `type Result struct{ Document *ir.Document; Diagnostics []ir.Diagnostic; Format
    frontend.SourceFormat }`.
  - `type Engine struct{ /* unexported registry */ }`;
    `func New() (*Engine, error)` — composes the default registry (registers `openapi.New()`;
    future frontends are added here and only here);
    `func NewWithRegistry(reg *frontend.Registry) *Engine` (for tests and embedders);
    `func (e *Engine) Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error)`:
    1. Read the file (`os.ReadFile` — the engine owns file I/O, frontends stay pure).
    2. `Sniff` → `Lookup` (unknown/unregistered format → wrapped error).
    3. `Parse(ctx, []frontend.Source{{Path: specPath, Data: data}},
       frontend.Options{FormatOptions: opts.FormatOptions})`.
    4. Unless `SkipValidate` or the document is nil: append `pass.Validate(doc)` to the
       diagnostics.
    5. Return `Result` (nil `Document` with diagnostics is a legal outcome — e.g.
       unsupported version; the caller decides fatality, architecture §4). Go `error` only
       for I/O and programmer errors.

- [ ] **Step 1: Write the failing tests**

`engine/sniff_test.go` (verbatim):

```go
package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/frontend"
)

func TestSniff_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      frontend.SourceFormat
		wantErr   string
	}{
		{"openapi 3.1 yaml", "openapi: 3.1.0\ninfo: {}\n", frontend.SourceFormat{Name: "openapi", Version: "3.1"}, ""},
		{"openapi 3.0 json", `{"openapi": "3.0.3"}`, frontend.SourceFormat{Name: "openapi", Version: "3.0"}, ""},
		{"swagger", "swagger: \"2.0\"\n", frontend.SourceFormat{}, "swagger 2.0 is not supported yet"},
		{"unknown", "hello: world\n", frontend.SourceFormat{}, "unrecognized spec format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := engine.Sniff([]byte(tc.src))
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
```

`engine/engine_test.go` (verbatim):

```go
package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

const tinySpec = `openapi: 3.1.0
info: {title: Tiny, version: "1"}
paths:
  /ping:
    get:
      operationId: ping
      responses: {"200": {description: ok}}
`

func writeSpec(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestEngine_RunEndToEnd(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, tinySpec), engine.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "Tiny", res.Document.Name)
	assert.Equal(t, "3.1", res.Format.Version)
	for _, d := range res.Diagnostics {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
}

func TestEngine_RunMissingFile(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), filepath.Join(t.TempDir(), "absent.yaml"), engine.RunOptions{})
	require.Error(t, err)
}

func TestEngine_ValidateRuns(t *testing.T) {
	t.Parallel()
	// SkipValidate=false is the default path; assert the validate pass ran by
	// checking Run on a valid doc appends nothing AND that SkipValidate=true
	// yields the same document (pass purity).
	eng, err := engine.New()
	require.NoError(t, err)
	path := writeSpec(t, tinySpec)
	withPass, err := eng.Run(t.Context(), path, engine.RunOptions{})
	require.NoError(t, err)
	withoutPass, err := eng.Run(t.Context(), path, engine.RunOptions{SkipValidate: true})
	require.NoError(t, err)
	assert.Equal(t, withoutPass.Document.Name, withPass.Document.Name)
}
```

- [ ] **Step 2: Run tests to verify they fail** — `go test ./engine -v` — FAIL.
- [ ] **Step 3: Implement `sniff.go` and `engine.go`** per **Produces** (`doc.go`: "Package
engine orchestrates the Morphic pipeline: it sniffs the source format, dispatches to the
registered frontend, and runs IR passes. It is the only package that composes frontends and
passes together.").
- [ ] **Step 4: Run tests** — `go test ./engine -v` — Expected: PASS.
- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add engine
git commit -m "feat(engine): orchestrate sniff, frontend dispatch, and validate pass"
```

---

### Task 20: cmd/morphic — the CLI entry point

**Files:**
- Create: `cmd/morphic/main.go`, `cmd/morphic/parse.go`
- Modify: `internal/archtest/arch_test.go` (add `engine` and `cmd/morphic` rules)
- Test: `cmd/morphic/parse_test.go`

**Interfaces:**
- Consumes: `engine` (Task 19), `ir.Diagnostic`/`Severity` for rendering.
- Produces the user-facing entry point:
  - Usage: `morphic parse <spec-file> [-o <out-file>] [--fail-on error|warning] [--skip-validate]`.
    `parse` lowers a spec to IR JSON: pretty document JSON (`json.MarshalIndent`, trailing
    newline — same bytes as `irtest.WriteGolden`) to `-o` or stdout; diagnostics rendered to
    stderr, one per line: `<severity> <code> <path>#<pointer>: <message>` (path from
    `Document.Sources[d.Provenance.Source].Path`; the CLI is the ONLY place diagnostics are
    rendered — architecture §4).
  - Exit codes: `0` success; `1` at least one diagnostic at or above `--fail-on` (default
    `error`); `2` usage, I/O, or engine errors. Nil `Result.Document` exits `1` after
    rendering diagnostics.
  - Structure (stdlib `flag` only — no CLI framework):

    ```go
    // main.go
    package main

    import (
    	"fmt"
    	"os"
    )

    func main() {
    	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
    }

    // run dispatches subcommands and returns the process exit code. It exists
    // so tests can drive the CLI without a subprocess; only main calls os.Exit.
    func run(args []string, stdout, stderr *os.File) int {
    	if len(args) == 0 {
    		fmt.Fprintln(stderr, usage)
    		return 2
    	}
    	switch args[0] {
    	case "parse":
    		return runParse(args[1:], stdout, stderr)
    	default:
    		fmt.Fprintf(stderr, "morphic: unknown command %q\n%s\n", args[0], usage)
    		return 2
    	}
    }

    const usage = `usage:
      morphic parse <spec-file> [-o out.json] [--fail-on error|warning] [--skip-validate]

    parse lowers an API spec (OpenAPI 3.x) into Morphic IR JSON.`
    ```

    `parse.go` implements `runParse(args []string, stdout, stderr *os.File) int` with a
    `flag.NewFlagSet("parse", flag.ContinueOnError)` (output redirected to stderr), calling
    `engine.New()` + `Run` with `context.Background()`, then `renderDiagnostics(stderr,
    res)` and `writeDocument(stdout|outFile, res.Document)`. `--fail-on` accepts only
    `error`/`warning` (anything else: usage error, exit 2). Keep every function ≤70 lines:
    split `runParse` / `renderDiagnostics` / `writeDocument` / `exitCodeFor(diags, failOn)`.
    (Signatures take `*os.File` because `run` hands real files; if you prefer `io.Writer`
    for tests, take `io.Writer` — tests below only need `bytes.Buffer` compatibility, so
    `io.Writer` is the better choice where possible: use `io.Writer` for both.)
  - Arch-test additions (Task 18's `rules` map):
    `"engine": {module + "/ir", module + "/frontend", module + "/frontend/openapi",
    module + "/pass", "gopkg.in/yaml.v3"}` and
    `"cmd/morphic": {module + "/ir", module + "/engine"}`.

- [ ] **Step 1: Write the failing tests** — `cmd/morphic/parse_test.go` (internal package
`main`, driving `run` directly; verbatim):

```go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

const tinySpec = `openapi: 3.1.0
info: {title: Tiny, version: "1"}
paths:
  /ping:
    get:
      operationId: ping
      responses: {"200": {description: ok}}
`

func writeFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestRun_ParseWritesIRToFile(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", tinySpec)
	out := filepath.Join(t.TempDir(), "ir.json")
	var stdout, stderr bytes.Buffer

	code := run([]string{"parse", spec, "-o", out}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	var doc ir.Document
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "Tiny", doc.Name)
	assert.True(t, bytes.HasSuffix(raw, []byte("\n")))
}

func TestRun_ParseUnknownSpecFails(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "junk.yaml", "hello: world\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"parse", spec}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "unrecognized spec format")
}

func TestRun_DiagnosticsGateExitCode(t *testing.T) {
	t.Parallel()
	// A parseable spec that produces at least one warning-or-info diagnostic
	// but no errors: validation-only keyword.
	spec := writeFile(t, "warn.yaml", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {type: object, not: {required: [x]}}
`)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"parse", spec}, &stdout, &stderr),
		"info diagnostics must not fail the default gate")
	assert.Contains(t, stderr.String(), "openapi/validation-only-keyword")

	stdout.Reset()
	stderr.Reset()
	// Not every diagnostic severity is easy to synthesize; the gate logic is
	// also unit-tested directly:
	assert.Equal(t, 1, exitCodeFor([]ir.Diagnostic{{Severity: ir.SeverityWarning}}, "warning"))
	assert.Equal(t, 0, exitCodeFor([]ir.Diagnostic{{Severity: ir.SeverityWarning}}, "error"))
	assert.Equal(t, 1, exitCodeFor([]ir.Diagnostic{{Severity: ir.SeverityError}}, "error"))
}

func TestRun_UsageErrors(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 2, run(nil, &stdout, &stderr))
	assert.Equal(t, 2, run([]string{"bogus"}, &stdout, &stderr))
	assert.Equal(t, 2, run([]string{"parse", "x.yaml", "--fail-on", "hint"}, &stdout, &stderr))
	assert.True(t, strings.Contains(stderr.String(), "usage"))
}
```

(This fixes `run`'s signature to `run(args []string, stdout, stderr io.Writer) int`.)

- [ ] **Step 2: Run tests to verify they fail** — `go test ./cmd/morphic -v` — FAIL.
- [ ] **Step 3: Implement `main.go` + `parse.go`; extend the arch-test rules map.**
- [ ] **Step 4: Run tests + manual smoke** —

```bash
go test ./cmd/morphic ./internal/archtest -v
go run ./cmd/morphic parse testdata/golden/openapi/petstore.yaml | head -20
```

Expected: tests PASS; the smoke run prints pretty IR JSON starting with `{"irVersion": "0.1.0"`.

- [ ] **Step 5: Gate and commit**

```bash
gofmt -l . && golangci-lint run && go vet ./... && go test ./...
git add cmd internal/archtest
git commit -m "feat(cmd/morphic): add parse CLI entry point"
```

---

## Self-Review

Checked against `docs/ir-design.md`, `docs/architecture.md`, and the milestone-1 definition:

1. **Spec coverage.** End-to-end: Tasks 1–6 (ir), 7 (frontend contract), 8–15 (OpenAPI
   frontend), 16 (validate pass), 17 (corpora + round-trip), 18 (architecture test + CI),
   19 (engine orchestration: sniff → frontend → passes), 20 (CLI entry point `morphic
   parse`). Every §14 OpenAPI-row lowering has a home: schemas/hoisting (10),
   composition/discriminators/enums (11), model detail/validation-only/x-* (12),
   operations/responses/webhooks/callbacks/links (13), params/content/multipart (14),
   auth/servers/tags/info (15). Deliberately deferred, per docs: pagination inference
   (policy seam exists; no heuristic shipped — architecture principle 6 allows absence),
   `pass/` union→enum collapse (documented as a pass, not frontend; not needed until a
   backend consumes it), UsageFlags computation (OQ4, backend-driven), backends (milestone
   3 — the CLI emits IR JSON until then), Swagger 2.0 (milestone 2 — the engine's sniffer
   already names it in its error, and the library's `swagger` package is noted).
2. **Placeholder scan.** Two intentional indirections remain, both justified: (a) ir struct
   transcription steps reference exact ir-design sections instead of duplicating ~900 lines
   of normative Go — the doc is in-repo, normative, and duplication would invite drift; every
   such step names each struct and file it lands in. (b) Items marked *verify at impl time*
   are library-surface facts pinned to a moving dependency, each with a defined fallback
   (preserve raw + diagnostic). No TBDs, no "handle errors appropriately".
3. **Type consistency.** `schemaRef`/`lower`/`intern`/`lowerPayload`/`lowerParameters`/
   `lowerSecurityRequirements` signatures match across Tasks 10–15; `frontend.Source`/
   `Options`/`Registry` usage in Tasks 9, 15 matches Task 7; `irtest.CompareGolden`
   (Task 6) matches Task 17's calls; `Idempotency` resolution (Task 4) is not referenced by
   the OpenAPI tasks (OpenAPI declares no idempotency — correct).
4. **Known deviations to surface in the PR:** `Idempotency` struct form (Task 4);
   `ErrorCase` multi-content preservation and non-required request bodies via raw
   extension (Tasks 13–14) — both candidates for small ir-design clarifications, to be
   raised as doc follow-ups, not silently.

## Execution

Execute in task order (0 → 20). Hard dependencies: Tasks 1–5 before everything after them;
8–15 are sequential (each builds on the lowerer); 16 needs 4; 17 needs 15 (+16 for its
pass-side test); 18 needs 15; 19 needs 15+16; 20 needs 19 and extends 18's rules. The
`go run ./cmd/morphic parse …` smoke in Task 20 needs Task 17's corpus file to exist.
PR: one branch `feat/ir-openapi-frontend`, squash-merged, description per repo rules
(Summary / Test plan; note the deviations from Self-Review §4).
