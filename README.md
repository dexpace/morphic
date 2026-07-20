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
[License](#license)

## Pipeline

```
spec ──▶ compiler ──▶ IR ──▶ passes ──▶ IR ──▶ emitter ──▶ SDK / docs
        (spec → IR)         (IR → IR)          (IR → artifacts)
```

- **Compilers** (`compilers/*`) turn one source format into an IR document plus diagnostics.
  OpenAPI 3.x ships first; Swagger 2.0, TypeSpec, Smithy, GraphQL, AsyncAPI, Protobuf, and
  Erlang/OTP are planned against the same IR.
- **Passes** (`pass/`) are small, order-explicit IR → IR transforms (validate, dedup, filter,
  version-slice, overlay). `validate` — referential integrity — runs by default.
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
  morphic compile <spec-file> [-o out.json] [--fail-on error|warning] [--skip-validate]
```

| Flag | Meaning |
|---|---|
| `-o <file>` | Write IR JSON to `<file>` instead of stdout. |
| `--fail-on error\|warning` | Exit non-zero when a diagnostic at or above this severity is emitted (default `error`). |
| `--skip-validate` | Skip the referential-integrity `validate` pass. |

Diagnostics print one per line as `<severity> <code> <path>#<pointer>: <message>`. Exit codes:
`0` clean, `1` a diagnostic reached the `--fail-on` threshold (or the spec could not be lowered),
`2` a usage or I/O error.

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
| `compilers/*` | 1 — one compiler per format (`compilers/openapi`) | `ir` + own format libs |
| `pass/` | 1 — IR → IR passes | `ir` only |
| `emitters/*` | 2 — IR → artifacts (future) | `ir` + emitter contract |
| `engine/` | 3 — orchestration | everything below |
| `cmd/morphic/` | 4 — CLI | `engine` |

Compilers and emitters never import each other; the IR is the only thing that crosses between
them.

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

## Building

Standard Go tooling. These are the same checks the CI `gate` runs, and all must pass before a
change lands:

```bash
gofmt -l .          # must print nothing
go vet ./...
golangci-lint run
go build ./...
go test ./...
./scripts/check-coverage.sh   # enforces 100% statement coverage, overall and per package
```

Run a single test with `go test ./ir -run TestName`. Golden IR snapshots are regenerated with
the corpus test's `-update` flag after an intentional change.

## License

Licensed under the [MIT License](LICENSE). Copyright © 2026 dexpace.
