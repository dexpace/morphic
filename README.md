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
diagnostics to stderr. Stdout is indented for reading; a file written with `-o` is compact, which
is about half the bytes, unless `--pretty` asks for the indented form. `morphic validate` runs the
same pipeline over the same spec for the diagnostics and the exit code alone, writing no IR
anywhere.

```bash
morphic compile openapi.yaml                 # IR JSON to stdout
morphic compile openapi.yaml -o api.ir.json  # ...or to a file
morphic validate openapi.yaml                # diagnostics and exit code only
```

```
usage:
  morphic <command> [flags]
  morphic compile <spec-file> [flags]
  morphic validate <spec-file> [flags]
```

`morphic`, `morphic help`, and `morphic` with a help flag (`-h`, `--help` or `-help`) print the
command list. `morphic help <command>` and `morphic <command> --help` print a command's flags.
Help always prints to stdout and exits `0`.

| Flag | Commands | Meaning |
|---|---|---|
| `--fail-on error\|warning` | both | Exit non-zero when a diagnostic at or above this severity is emitted (default `error`). |
| `--skip-validate` | both | Skip the referential-integrity `validate` pass. |
| `-o <file>` | `compile` | Write IR JSON to `<file>` instead of stdout, compact rather than indented. |
| `--pretty` | `compile` | Indent the JSON `-o` writes; stdout is indented either way. |
| `--explain <json-pointer>` | `compile` | Report what compiling produced at this source coordinate instead of writing the document. |
| `--opt <key>=<value>` | both | Set one option on the compiler the spec selects. Repeatable; a repeated key is refused. |

Diagnostics print one per line as `<severity> <code> <location>: <message>`, where `<location>` is
`<path>#<pointer>` for a finding in a spec file, a bare pointer for one an IR pass made about the
document, and absent for one raised before any document existed.

Both commands use the same exit codes: `0` clean (and for any help request); `1` the spec has
problems — a diagnostic reached the `--fail-on` threshold, or it could not be lowered at all, which
covers an undecodable file, an unrecognized or unsupported format, and a version no compiler claims;
`2` the invocation or the filesystem was wrong — a bad flag or argument, a spec that could not be
read, an output that could not be written. Nothing about the spec's own contents reaches `2`.

#### Compiler options

`--opt` names an option in the vocabulary of whichever compiler recognizes the spec — morphic
itself knows none of them, and an unknown name is refused by the compiler rather than ignored.
The OpenAPI compiler accepts:

| Option | Values | Meaning |
|---|---|---|
| `grouping` | `tags` (default), `path-prefix` | How operations are grouped into operation groups. |
| `allow-external-refs` | `true`, `false` (default) | Let `$ref` resolution leave the source document, reading files and fetching URLs. |
| `overlay` | a file path | Apply an [OpenAPI Overlay](https://spec.openapis.org/overlay/latest.html) document to the source before lowering. |
| `overlay-lax` | `true`, `false` (default) | Do not refuse when an overlay action's selector matches nothing. |

```bash
morphic compile openapi.yaml --opt grouping=path-prefix --opt overlay=patch.yaml
```

`1` and `2` can both be earned by one run — a spec that reached the threshold whose `-o`
destination then refused the write. The verdict on the spec wins, so `1` means what it says
whatever `-o` pointed at, and `2` means the run failed for a reason outside the spec. The write
error is printed on stderr either way. Note that `-o` publishes by rename, so a destination whose
directory will not take a temp file — `/dev/null`, a read-only directory — cannot be written to at
all.

### Library

The same pipeline is available as a package. `engine.New` builds the default registry (OpenAPI
compiler + `validate` pass); `Run` asks the registered compilers which of them recognizes the
source, compiles, and runs passes. A Go caller can set compiler options as a typed value through
`RunOptions.FormatOptions` instead of as text through `RunOptions.CompilerOptions`.

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

## Building

One command, and it must pass before a change lands:

```bash
make gate
```

That is not a summary of CI — it is what CI runs. Every check in `.github/workflows/gate.yml` runs
a `Makefile` target bar `lint`, which the golangci-lint action runs at the version the `Makefile`
pins, so the local command and the job are the same commands in the same order. Read the `Makefile`
for the step list rather than a copy here; `make coverage`, `make fuzz`, `make bench` and the rest
are individually runnable while iterating.

Run a single test with `go test ./ir -run TestName`. Golden IR snapshots are regenerated with
the corpus test's `-update` flag after an intentional change.

## License

Licensed under the [MIT License](LICENSE). Copyright © 2026 dexpace.
