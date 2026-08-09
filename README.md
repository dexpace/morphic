<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/dexpace-wordmark-dark.svg">
    <img alt="dexpace" src="docs/assets/dexpace-wordmark-light.svg" width="280">
  </picture>
</p>

<h1 align="center">morphic</h1>

<p align="center">Idiomatic SDKs and docs from any API spec. One spec-agnostic IR, many targets.</p>

<p align="center">
  <a href="https://github.com/dexpace/morphic/actions/workflows/gate.yml"><img alt="gate" src="https://github.com/dexpace/morphic/actions/workflows/gate.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
  <img alt="Go" src="https://img.shields.io/badge/go-1.26-00ADD8.svg?logo=go&logoColor=white">
  <img alt="Coverage" src="https://img.shields.io/badge/coverage-100%25-success.svg">
</p>

Morphic is a spec-to-SDK compiler. It reads an API specification in any supported source format,
lowers it into **one spec-agnostic intermediate representation (IR)**, and generates idiomatic
SDKs and documentation from that IR. The IR is the contract: a compiler's only output is an IR
document, and an emitter's only input is an IR document — the two never see each other, so a new
source format and a new target language are independent pieces of work.

The design goal is *lossless by default*. Compilers preserve source semantics — composition
(`allOf`/`oneOf`/`anyOf`), unions, discriminators, visibility, encodings, streaming — rather than
flattening them early. Lowering to what a target language can express happens late, in emitter
refiners, so no target's limitations leak backward into the shared representation.

> **Status: early development.** The `ir` package and the OpenAPI 3.x compiler (Milestone 1) are
> implemented and exercised end-to-end by the `morphic compile` CLI; emitters are not built yet.
> There is no released version — the IR schema and the CLI surface are unstable and may change
> between commits. The full IR capability surface is fixed from day one, so later compilers land
> without reshaping it.

## Contents

[Pipeline](#pipeline) ·
[Status](#status) ·
[Install](#install) ·
[Usage](#usage) ·
[Package layout](#package-layout) ·
[Design docs](#design-docs) ·
[Building](#building) ·
[Testing and verification](#testing-and-verification) ·
[License](#license)

## Pipeline

```
spec ──▶ compiler ──▶ IR ──▶ passes ──▶ IR ──▶ emitter ──▶ SDK / docs
        (spec → IR)         (IR → IR)          (IR → artifacts)
```

- **Compilers** (`compilers/*`) turn one source format into an IR document plus diagnostics.
  OpenAPI 3.x ships first; Swagger 2.0, TypeSpec, Smithy, GraphQL, AsyncAPI, Protobuf, and
  Erlang/OTP are planned against the same IR.
- **Passes** (`pass/`) are small, order-explicit IR → IR transforms. `validate` — referential
  integrity — is the only one implemented, and runs by default; `link`, `dedup`, `filter`,
  `version-slice` and `overlay` are designed in [architecture.md](docs/architecture.md) §2.2 and
  not built yet.
- **Emitters** (`emitters/*`, future) turn an IR document into artifacts for one target. SDK
  runtime policy (retry, timeout, telemetry, error taxonomy) is a *separate* emitter input, not
  part of the IR.

Every stage is a pure function `f(input, options) → (output, diagnostics)` with no package-level
state. Stages never write to stderr or log; they return typed `ir.Diagnostic` values and the
engine (or CLI) decides what is fatal.

## Status

| Milestone | Scope | State |
|---|---|---|
| 1 | IR package + OpenAPI 3.x compiler, `validate` pass, golden corpus, JSON round-trip | **Implemented** |
| 2 | Swagger 2.0 lift into the OpenAPI compiler (format-version-normalization seam) | Planned |
| 3 | First emitter — one language end-to-end (plan / refine / emit boundary) | Planned |
| 4 | Second family compiler (TypeSpec or Smithy) — proves the spec-agnostic claim | Planned |
| 5 | Event-shaped compiler (AsyncAPI), then GraphQL, Protobuf, Erlang/OTP | Planned |

## Install

Requires Go 1.26 or newer.

```bash
go install github.com/dexpace/morphic/cmd/morphic@latest
```

Or build the CLI from a checkout:

```bash
go build -o morphic ./cmd/morphic
```

## Usage

### CLI

`morphic compile` lowers one OpenAPI 3.x spec into Morphic IR JSON on stdout, and writes
diagnostics to stderr.

```bash
morphic compile openapi.yaml                 # IR JSON to stdout
morphic compile openapi.yaml -o api.ir.json  # ...or to a file
```

```
usage:
  morphic <command> [flags]
  morphic compile <spec-file> [flags]
```

`morphic`, `morphic help`, and `morphic` with a help flag (`-h`, `--help` or `-help`) print the
command list. `morphic help compile` and `morphic compile --help` print a command's flags. Help
always prints to stdout and exits `0`.

The flags below are `compile`'s:

| Flag | Meaning |
|---|---|
| `-o <file>` | Write IR JSON to `<file>` instead of stdout. |
| `--fail-on error\|warning` | Exit non-zero when a diagnostic at or above this severity is emitted (default `error`). |
| `--skip-validate` | Skip the referential-integrity `validate` pass. |
| `--explain <json-pointer>` | Report what compiling produced at this source coordinate instead of writing the document. |

Diagnostics print one per line to stderr, in one of two shapes. A diagnostic that names a source
file renders as `<severity> <code> <path>#<pointer>: <message>`; one that does not — a `validate`
finding, whose pointer is an IR ID rather than a source coordinate — renders as
`<severity> <code> <pointer>: <message>`, rather than fabricating a location in the spec.

The `<pointer>` is whatever the stage that raised the diagnostic recorded, so it is not always a
JSON pointer. All three of these come out of a single compile:

```
error openapi/validation/validation-allowed-values api.yaml#14:19: [14:19] error ...
error openapi/unresolved-ref api.yaml#: not found -- key Missing not found ...
info openapi/validation-only-keyword api.yaml#/components/schemas/Odd: validation-only keyword ...
```

A JSON pointer is the usual case; a `<line>:<col>` appears when the fact came from the upstream
spec validator, which reports positions rather than pointers; an empty pointer appears when the
position is not recoverable. Match on the code, never on the pointer's shape.

Exit codes: `0` clean (and for any help request), `1` a diagnostic reached the `--fail-on`
threshold (or the spec could not be lowered), `2` a usage or I/O error.

### Library

The same pipeline is available as a package. `engine.New` builds the default registry (OpenAPI
compiler + `validate` pass); `Run` sniffs the format, compiles, and runs passes.

```go
eng, err := engine.New()
if err != nil {
    return err
}

res, err := eng.Run(context.Background(), "openapi.yaml", engine.RunOptions{})
if err != nil {
    return err
}

for _, d := range res.Diagnostics {
    // res.Diagnostics are typed ir.Diagnostic values, not log lines.
}
doc := res.Document // *ir.Document — round-trips through JSON deterministically
```

## Package layout

The import graph is layered and enforced by an architecture test (`internal/archtest`): each
package may import only the packages one layer below it.

| Package | Layer | Imports |
|---|---|---|
| `ir/` | 0 — IR nodes, IDs, traversal, JSON round-trip | stdlib only |
| `ir/irverify/`, `ir/irtest/` | 0 — structural-invariant oracle; golden-snapshot helper | `ir` |
| `compilers/compile/` | 1 — what every compiler shares: type registry, diagnostics, naming and identifier grammars | `ir` only |
| `compilers/*` | 1 — one compiler per format; a public face over its own `internal/` packages | `ir` + `compilers` + `compilers/compile` + own `internal/*` + format libs |
| `pass/` | 1 — IR → IR passes | `ir` only |
| `emitters/*` | 2 — IR → artifacts (future) | `ir` + emitter contract |
| `engine/` | 3 — orchestration | everything below |
| `cmd/morphic/` | 4 — CLI | `engine` + `ir` |
| `cmd/morphic-harness/`, `internal/*` | tooling, outside the pipeline: the oracle sweep, the architecture rules, shared fixtures | as their entries allow |

Compilers and emitters never import each other; the IR is the only thing that crosses between
them. A compiler's own `internal/` packages each carry their own entry rather than inheriting the
compiler's, so the ordering among them is enforced too — and none may reach the compiler above it.

This table is prose; `internal/archtest`'s `rules` map is the source of truth. To read the layout
off the tree: `git ls-files '*/*.go' | xargs -n1 dirname | sort -u`.

## Design docs

The design documents are normative — read them before proposing changes to the IR or pipeline.

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Pipeline stages, package layout, layering rules, milestones. |
| [IR design](docs/ir-design.md) | The intermediate representation: node catalog, semantics, per-format lowering. `ir-design.md` field shapes are the contract. |
| [Spec capability matrix](docs/ir-spec-matrix.md) | What each source format can express — the union the IR is designed against. |
| [Emitter design](docs/emitter-design.md) | The emitter contract and the plan / refine / emit boundary. |
| [Prior art](docs/prior-art.md) | Lessons taken from oagen, Kiota, and TypeSpec/TCGC, and the mistakes each Morphic decision avoids. |
| [Reference learnings](docs/reference-learnings.md) | Detailed notes from the reference codebases studied during design. |
| [Micro-compiler design](docs/micro-compiler-design.md) | The restructuring that replaced the OpenAPI compiler's god object with its internal packages, and the caps that stop it regrowing. |
| [Micro-compiler plan](docs/micro-compiler-plan.md) | The order that restructuring landed in, kept as a record of how the pieces blocked each other. |

## Building

Standard Go tooling. These are the same checks the CI `gate` job runs
([`.github/workflows/gate.yml`](.github/workflows/gate.yml)), in this order, and all must pass
before a change lands:

```bash
gofmt -l $(git ls-files '*.go')   # must print nothing
go vet ./...
golangci-lint run
go build ./...
./scripts/check-coverage.sh       # runs go test ./... and enforces 100% statement coverage
```

`gofmt` is scoped to tracked files on purpose: `gofmt -l .` also walks git-ignored directories, so
a checkout carrying any ignored Go tree gets hundreds of lines from a command that is supposed to
print nothing.

Coverage is a gate at exactly 100%, not a target. `check-coverage.sh` counts statements from the
profile rather than reading `go test`'s rounded percentage — a package at 99.96% prints
`100.0%` — so one uncovered statement fails the build, and `go test ./...` passing locally is not
evidence the gate passes.

## Testing and verification

`go test ./...` runs everything. The workflows below are the ones that need a different command,
and each answers a question the plain run does not.

### One test, one package

```bash
go test ./ir -run TestNewBigVal_AcceptsDecimalForms   # one test
go test ./compilers/openapi -count=1                  # one package, test cache bypassed
```

### The oracle sweep

`internal/harness` drives a spec through six oracles in order: no panic, no error diagnostic,
`irverify`'s structural invariants, JSON round-trip, determinism across two compiles, and
order-independence — a recompile with every mapping's entry order reversed, diffed against the
first. That last one is what catches two declarations minting one node at a single pointer, which
a single-order test cannot see. [architecture.md](docs/architecture.md) §5 explains why.

```bash
go run ./cmd/morphic-harness testdata/conformance/openapi   # a directory, walked recursively
go run ./cmd/morphic-harness path/to/spec.yaml              # or one spec
```

Exit status is `0` when every spec passes, `1` when any fails an oracle, and `2` on a usage or
filesystem error, so it works as a script gate. Run it against whatever spec provoked a compiler
change before submitting it. The same sweep over every committed spec runs as a test:

```bash
go test ./internal/harness
```

### Golden IR snapshots

`spec → IR → JSON` is snapshot-compared byte for byte. Regenerate after an intentional change:

```bash
go test ./compilers/openapi -run TestConformance -update
```

`-update` is registered by `ir/irtest` rather than by the test framework, so only packages whose
tests import it accept the flag — `go test ./... -update` fails with
`flag provided but not defined`. To see which packages take it:

```bash
go list -f '{{.ImportPath}} {{.TestImports}} {{.XTestImports}}' ./... | grep ir/irtest | cut -d' ' -f1
```

A golden diff is something to read, not to regenerate reflexively: it is the only place an
unintended IR change surfaces. `TestConformance` will not rewrite a golden whose focused
capability assertion is failing, so `-update` cannot paper over a broken lowering.

### The conformance corpus

`testdata/conformance/openapi/` holds one minimal spec per row of
[`docs/ir-spec-matrix.md`](docs/ir-spec-matrix.md), each asserting the IR captures that capability
losslessly. This is what keeps *lossless by default* honest. To add a case:

1. Write `testdata/conformance/openapi/<name>.yaml`. It must be `.yaml` — nothing else pairs with
   a table row.
2. Add `{"<name>", assert<Name>}` to the table in `conformanceCases()`
   (`compilers/openapi/conformance_test.go`), with a focused assertion for the capability.
3. Run the `-update` command above to mint `<name>.golden.json`. The **first** run reports a
   failure, because the corpus/table check reads the directory before the golden lands; re-run to
   confirm green.

`TestConformance_TableNamesEveryCorpusSpec` fails if the table and the directory disagree in
either direction, so a spec cannot land un-asserted and a row cannot outlive its spec.

### Deliberately broken fixtures

`testdata/dangling/openapi/` holds reproducers for references the compiler once mishandled.
`compilers/openapi/danglingcheck_test.go` asserts the one property they share: the IR that comes
out is referentially closed. Each reproducer reaches that either by interning the reference
correctly or by dropping it with an error-severity diagnostic, and the table in that test records
which of the two each one is owed.

The ones that are refused outright are also listed as `knownInvalid` in
`internal/harness/corpus_test.go`, because an error diagnostic is the correct outcome for them and
the sweep would otherwise report it as a finding. That listing has a consequence worth knowing
before you add a fixture: the oracles stop at the first failure, so a spec that trips the
error-diagnostic oracle never reaches `irverify`, round-trip, determinism or order-independence.

### Fuzzing

```bash
go test ./compilers/openapi -run '^$' -fuzz FuzzCompile -fuzztime 30s
```

`-fuzz` takes exactly one target, and `-run '^$'` stops the package's ordinary tests from running
first. To list the targets rather than trust a count:

```bash
grep -rn '^func Fuzz' --include='*_test.go' .
```

### Atomic output

```bash
./scripts/verify-atomic-output.sh
```

Drives the built binary against a real filesystem to check that `morphic compile -o` publishes its
output atomically, and pins the four limitations publishing-by-rename brings. It is not part of the
CI gate; run it when touching output writing.

## License

Licensed under the [MIT License](LICENSE). Copyright © 2026 dexpace.
