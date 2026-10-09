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
  <a href="go.mod"><img alt="Go" src="https://img.shields.io/github/go-mod/go-version/dexpace/morphic?logo=go&logoColor=white"></a>
</p>

Morphic is a spec-to-SDK compiler. It reads an API specification, lowers it into **one
spec-agnostic intermediate representation (IR)**, and generates SDKs and documentation from that
IR. Compilers (spec → IR) and emitters (IR → artifacts) never see each other; the IR is the only
thing that passes between them, so supporting a new source format and supporting a new target
language are independent pieces of work.

> **Status: early development.** The IR and the OpenAPI 3.0, 3.1 and 3.2 compiler work end to end
> through the `morphic` CLI. No emitter exists yet, so today Morphic produces IR, not SDKs. There
> is no released version, and the IR schema and the CLI may change between commits.

## Contents

[Design principles](#design-principles) ·
[Pipeline](#pipeline) ·
[Status](#status) ·
[Install](#install) ·
[Usage](#usage) ·
[Package layout](#package-layout) ·
[Testing](#testing) ·
[Design docs](#design-docs) ·
[License](#license)

## Design principles

Each of these is a load-bearing decision, not a preference. [Architecture](docs/architecture.md)
and [IR design](docs/ir-design.md) give the reasoning.

- **The IR is the contract.** A compiler's only output is an IR document plus diagnostics, and an
  emitter's only input is an IR document plus its own options.
- **Lossless by default, lowered late.** Compilers keep source semantics (composition, unions,
  discriminators, visibility, encodings, streaming) instead of flattening them. Lowering to what a
  target language can express happens in the emitter, so no target's limits leak back into the
  shared IR. What the IR does not model structurally is kept verbatim, along with why it was kept.
- **Stable IDs, neutral names.** Every named entity gets an ID derived from its location in the
  source, never from its display name, and no rename rewrites it. Names are stored as written, as
  sent on the wire, and as a neutral word sequence; casing and reserved-word escaping are left to
  each emitter.
- **Deterministic output.** A document round-trips through JSON. Compiling the same spec twice
  gives the same bytes, and reordering a spec's declarations changes nothing beyond source order.
- **Diagnostics, not logs.** Every stage is a pure function `f(input, options) → (output,
  diagnostics)` with no package-level state. Problems in a spec come back as typed `ir.Diagnostic`
  values, and the caller decides which are fatal.
- **Heuristics are policy.** Anything inferred rather than declared, such as which media types
  imply a stream or which vendor extensions fill typed fields, is an injectable policy the caller
  can change or switch off, and what it produces is marked as inferred in the IR's provenance.
- **Untrusted input is bounded.** External `$ref`s are off unless enabled, and budgets on source
  size, parsed nodes, enum members and YAML alias expansion turn a pathological document into a
  diagnostic instead of an exhausted host.

## Pipeline

```
spec ──▶ compiler ──▶ IR ──▶ passes ──▶ IR ──▶ emitter ──▶ SDK / docs
        (spec → IR)         (IR → IR)          (IR → artifacts)
```

- **Compilers** (`compilers/*`) turn one source format into an IR document plus diagnostics. Each
  recognizes its own format from the bytes and owns its options. OpenAPI 3.x is the first; the
  formats planned after it are in [Status](#status).
- **Passes** (`pass/`) are small, order-explicit IR → IR transforms. `validate` checks referential
  integrity and runs by default. Deduplication, filtering, version slicing and IR overlays are
  designed in [Architecture](docs/architecture.md) but not built.
- **Emitters** (`emitters/*`, not built yet) turn an IR document into the artifacts for one
  target. SDK runtime policy (retries, timeouts, telemetry, the error taxonomy) is a separate
  emitter input, not part of the IR.

## Status

| Milestone | Scope | State |
|---|---|---|
| 1 | IR package + OpenAPI 3.x compiler, `validate` pass, golden corpus, JSON round-trip | **Implemented** |
| 2 | Swagger 2.0 lift into the OpenAPI compiler (format-version-normalization seam) | Planned |
| 3 | First emitter: one language end to end (plan / refine / emit boundary) | Planned |
| 4 | Second family compiler (TypeSpec or Smithy), proving the spec-agnostic claim | Planned |
| 5 | Event-shaped compiler (AsyncAPI), then GraphQL, Protobuf, Erlang/OTP | Planned |

The IR's capability surface covers every format above from the start, so the later compilers land
without reshaping it. Until milestone 2, a Swagger 2.0 document is recognized and refused with
`engine/no-compiler-for-format`.

## Install

Requires the Go release named by the `go` directive in [`go.mod`](go.mod), or newer. Under the
default `GOTOOLCHAIN=auto`, an older `go` command from Go 1.21 on downloads that release itself.

```bash
go install github.com/dexpace/morphic/cmd/morphic@latest
```

Or build the CLI from a checkout:

```bash
go build -o morphic ./cmd/morphic
```

## Usage

### CLI

```bash
morphic compile openapi.yaml                    # IR JSON to stdout
morphic compile openapi.yaml -o api.ir.json     # ...or to a file
morphic validate openapi.yaml                   # diagnostics and exit code only
morphic compile openapi.yaml --explain /components/schemas/Pet
```

`compile` lowers one spec into IR JSON and writes diagnostics to stderr. Stdout is indented for
reading; a file written with `-o` is compact, about half the bytes, unless `--pretty` asks for the
indented form. `validate` runs the same pipeline and drops the document, for a check that wants
the diagnostics and the exit code and nothing else.

`morphic help` lists the commands, and `morphic help <command>` or `morphic <command> --help`
prints a command's flags. Help always goes to stdout and exits `0`.

| Flag | Commands | Meaning |
|---|---|---|
| `--fail-on error\|warning` | both | Exit non-zero when a diagnostic at or above this severity is emitted (default `error`). |
| `--skip-validate` | both | Skip the referential-integrity `validate` pass. |
| `--opt <key>=<value>` | both | Set one option on the compiler the spec selects. Repeatable; a repeated key is refused. |
| `-o <file>` | `compile` | Write IR JSON to `<file>` instead of stdout, compact rather than indented. |
| `--pretty` | `compile` | Indent the JSON `-o` writes; stdout is indented either way. |
| `--explain <json-pointer>` | `compile` | Instead of writing the document, report what compiling produced at this source coordinate: the type node there, the coordinates beneath it, and the diagnostics stamped at it. `''` is the whole document. |

#### Diagnostics

Diagnostics print one per line as `<severity> <code> <location>: <message>`:

```
error openapi/unresolved-ref openapi.yaml#/components/schemas/Pet/discriminator/mapping/dog: discriminator mapping "dog" references unresolved schema "#/components/schemas/Dog"
```

`<location>` is the most precise form available:

- `<path>#<pointer>` for a finding at a JSON Pointer in a spec file;
- `<path>:<line>:<column>` for one found before the spec had pointers, or `<path>:<line>` when the
  column is unknown;
- `<path>` alone for one about the file as a whole, including a spec that was refused or that no
  compiler recognized;
- the bare IR location for one an IR pass made about the document.

#### Exit codes

Both commands share them.

| Code | Meaning |
|---|---|
| `0` | Clean, or a help request. |
| `1` | The spec has problems: a diagnostic reached the `--fail-on` threshold, or the spec could not be lowered at all (an undecodable file, an unrecognized or unsupported format, a version no compiler claims). |
| `2` | The invocation or the filesystem was wrong: a bad flag or argument, a spec that could not be read, an output that could not be written. Nothing in the spec's own contents leads to `2`. |

One run can earn both `1` and `2`: a spec that reached the threshold, whose `-o` destination then
refused the write. The verdict on the spec wins, so `1` always means the spec has problems, and the
write error is printed on stderr either way. `-o` publishes by rename, so a destination whose
directory will not take a temp file, such as `/dev/null` or a read-only directory, cannot be
written to at all.

#### Compiler options

`--opt` names an option in the vocabulary of whichever compiler recognizes the spec. Morphic itself
knows none of them, and the compiler refuses a name it does not know rather than ignoring it. The
OpenAPI compiler accepts:

| Option | Values | Meaning |
|---|---|---|
| `grouping` | `tags` (default), `path-prefix` | How operations are grouped into operation groups. |
| `allow-external-refs` | `true`, `false` (default) | Let `$ref` resolution leave the source document, reading files and fetching URLs. |
| `overlay` | a file path | Apply an [OpenAPI Overlay](https://spec.openapis.org/overlay/latest.html) document to the source before lowering. |
| `overlay-lax` | `true`, `false` (default) | Do not refuse when an overlay action's selector matches nothing. |

```bash
morphic compile openapi.yaml --opt grouping=path-prefix --opt overlay=patch.yaml
```

### Library

The same pipeline is available as a Go package. `engine.New` builds the default engine: every
built-in compiler plus the `validate` pass. `Run` reads the spec, asks the registered compilers
which one recognizes it, compiles, and runs the passes.

```go
eng, err := engine.New()
if err != nil {
    return err
}

res, err := eng.Run(ctx, "openapi.yaml", engine.RunOptions{
    CompilerOptions: map[string]string{"grouping": "path-prefix"},
})
if err != nil {
    return err // misuse or I/O; problems in the spec arrive as diagnostics
}

for _, d := range res.Diagnostics {
    fmt.Println(d.Severity, d.Code, d.Message)
}
if res.Document == nil {
    return errors.New("the spec could not be lowered")
}

data, err := json.Marshal(res.Document) // encoding/json/v2; canonical, deterministic bytes
```

A Go error from `Run` means misuse or I/O. Everything wrong with the spec itself, including a spec
no compiler can lower, is a diagnostic in the result, with a nil `Document` when nothing could be
lowered. Options go in either as text through `RunOptions.CompilerOptions`, the CLI's `--opt`
vocabulary, or as a typed value such as an `openapi.Options` through `RunOptions.FormatOptions`;
set one or the other, not both.

## Package layout

The import graph is layered, and `internal/archtest` enforces it: a package imports only what its
entry in that test allows, and nothing from a layer above its own.

| Package | Layer | Imports |
|---|---|---|
| `ir/` | 0: IR nodes, IDs, traversal, JSON round-trip | stdlib only |
| `ir/irverify/`, `ir/irtest/` | 0: structural-invariant checker; golden-snapshot and fixture helpers | `ir` (`irtest` also `go-cmp`) |
| `compilers/` | 1: the `Compiler` contract and the format-keyed registry | `ir` only |
| `compilers/compile/` | 1: what every compiler shares (type registry, diagnostics, naming and identifier grammars) | `ir` only |
| `compilers/*` | 1: one compiler per format; a public face over its own `internal/` packages | `ir` + `compilers` + `compilers/compile` + own `internal/*` + format libs |
| `pass/` | 1: IR → IR passes | `ir` only |
| `emitters/*` | 2: IR → artifacts (not built yet) | `ir` + emitter contract |
| `engine/` | 3: orchestration | everything below |
| `cmd/morphic/` | 4: CLI | `engine` + `ir` |
| `cmd/morphic-harness/`, `internal/*` | tooling outside the pipeline: the oracle sweep, the architecture rules, shared fixtures | as their entries allow; `internal/harness` is exempt |

Compilers and emitters never import each other. A compiler's own `internal/` packages each carry
their own entry rather than inheriting the compiler's, so the order among them is enforced too,
and none of them may reach the compiler above it.

This table is a summary; the `rules` map in `internal/archtest` is the source of truth. To read
the layout off the tree, run `git ls-files '*/*.go' | xargs -n1 dirname | sort -u`.

## Testing

One command, and it must pass before a change lands:

```bash
make gate
```

That is not a summary of CI; it is what CI runs. Every check in `.github/workflows/gate.yml` runs
a `Makefile` target bar `lint`, which the golangci-lint action runs at the version the `Makefile`
pins, so the local command and the job run the same steps in the same order. The `Makefile` holds
the step list, and each target (`make coverage`, `make fuzz`, `make bench` and the rest) runs on its
own while iterating. Coverage is gated at exactly 100% of statements, counted from the profile
rather than from `go test`'s rounded percentage, so one uncovered statement fails the build.

Run a single test with `go test ./ir -run TestName`. Beyond that:

- **Golden snapshots.** `testdata/golden/` and `testdata/conformance/` pair each spec with the IR
  it must compile to, byte for byte. After an intentional change, rewrite them and review the diff:

  ```bash
  go test ./compilers/openapi -run 'TestGolden|TestConformance' -update
  ```

- **Conformance corpus.** Each case in `testdata/conformance/openapi/` is a minimal spec that
  witnesses one or more rows of the [spec capability matrix](docs/ir-spec-matrix.md). To add one,
  write `<name>.yaml`, add an entry to `conformanceCases()` in
  `compilers/openapi/conformance_test.go` naming its assertion and the matrix row keys it
  witnesses, then run the command above to write `<name>.golden.json`. The suite fails on a spec
  with no entry, and on a matrix row OpenAPI can express that no case witnesses, unless the row
  is listed as uncovered with a reason.
- **Oracle harness.** `go run ./cmd/morphic-harness <file|dir>` sweeps specs through the
  bug-catching oracles, stopping at the first that fires for each: no panic, no error diagnostic,
  the `ir/irverify` structural invariants, JSON round-trip, determinism, and order-invariance. It
  exits `1` if any spec fails. `testdata/` holds negative fixtures that fail on purpose, so sweep
  it with `go test ./internal/harness`, which knows which ones are expected to.
- **Fuzzing.** `make fuzz` runs every fuzz target for 10s each (`make fuzz FUZZTIME=5m` for a
  longer search). One target runs on its own with, for example,
  `go test ./compilers/openapi -run '^$' -fuzz '^FuzzCompile$' -fuzztime 1m`.

## Design docs

The design documents are normative. Read them before proposing changes to the IR or the pipeline.

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Pipeline stages, package layout, layering rules, milestones. |
| [IR design](docs/ir-design.md) | The intermediate representation: node catalog, semantics, per-format lowering. Its field shapes are the contract. |
| [Spec capability matrix](docs/ir-spec-matrix.md) | What each source format can express: the union the IR is designed against. |
| [Emitter design](docs/emitter-design.md) | The emitter contract and the plan / refine / emit boundary. |

## License

Licensed under the [MIT License](LICENSE). Copyright © 2026 dexpace.
