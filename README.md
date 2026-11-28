<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/dexpace-wordmark-dark.svg">
    <img alt="dexpace" src="docs/assets/dexpace-wordmark-light.svg" width="280">
  </picture>
</p>

<h1 align="center">morphic</h1>

<p align="center">Idiomatic SDKs and docs from any API spec. One spec-agnostic IR, many targets.</p>

<p align="center">
  <a href="https://github.com/dexpace/morphic/actions/workflows/gate.yml"><img alt="gate" src="https://github.com/dexpace/morphic/actions/workflows/gate.yml/badge.svg?event=push"></a>
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
> is no released version, and the IR schema and the CLI may change between commits ([what a
> consumer storing IR pins](docs/ir/consumers.md#what-to-pin)).

## Quick start

Install the CLI. It requires the Go release named by the `go` directive in [`go.mod`](go.mod), or
newer; under the default `GOTOOLCHAIN=auto`, an older `go` command from Go 1.21 on downloads that
release itself.

```bash
go install github.com/dexpace/morphic/cmd/morphic@latest
```

Or build it from a checkout with `go build -o morphic ./cmd/morphic`. Then compile a spec:

```bash
morphic compile openapi.yaml                    # IR JSON to stdout
morphic compile openapi.yaml -o api.ir.json     # ...or to a file
morphic validate openapi.yaml                   # diagnostics and exit code only
morphic compile openapi.yaml --explain /components/schemas/Pet
```

`compile` lowers one spec into IR JSON, and `validate` runs the same pipeline and drops the
document. Both write what they find in the spec to stderr, one finding per line, as
`<severity> <code> <location>: <message>`:

```
error openapi/unresolved-ref openapi.yaml#/components/schemas/Pet/discriminator/mapping/dog: discriminator mapping "dog" references unresolved schema "#/components/schemas/Dog": it names nothing in the document it points into
```

The exit status is `0` for a clean run, `1` when the spec has problems, and `2` when the
invocation, the filesystem or Morphic itself failed, or the run was stopped. `--explain` reports
what compiling produced at one place in the spec instead of writing the document.

The [CLI reference](docs/cli.md) covers every flag, the triage file that rates or accepts findings,
the JSON report, and the options the OpenAPI compiler takes.

## Library

The same pipeline is available as a Go package:

```go
eng, err := engine.New()
if err != nil {
    return err
}

res, err := eng.Run(ctx, "openapi.yaml", engine.RunOptions{})
if err != nil {
    return err // misuse, I/O or a broken contract; problems in the spec arrive as diagnostics
}
if res.Failed {
    return errors.New("the spec has problems") // res.Diagnostics says which
}

// Canonical, deterministic bytes, replacing the file atomically as `-o` does.
return output.ReplaceDocument(ctx, "api.ir.json", res.Document, output.Compact)
```

[Morphic as a Go library](docs/library.md) covers compiling a spec held in memory, typed options,
passes of your own, triage, and rendering findings exactly as the CLI prints them.

## Pipeline

```
spec ──▶ compiler ──▶ IR ──▶ passes ──▶ IR ──▶ emitter ──▶ SDK / docs
        (spec → IR)         (IR → IR)          (IR → artifacts)
```

- **Compilers** (`compilers/*`) turn one source format into an IR document plus diagnostics. Each
  recognizes its own format from the bytes and owns its options. OpenAPI 3.x is the first; the
  formats planned after it are in [Status](#status).
- **Passes** (`pass/`) are small, order-explicit IR → IR transforms. `const-unions` rewrites a
  closed union of strings as the enum of them, `validate` checks referential integrity and the
  well-formedness rules the IR states, and `names` reports names a target may render alike; all
  three run by default. Deduplication, filtering, version slicing and IR overlays are
  designed in [Architecture](docs/architecture.md) but not built.
- **Emitters** (`emitters/*`, most of it not built yet) turn an IR document into the artifacts for
  one target. SDK runtime policy (retries, timeouts, telemetry, the error taxonomy) is a separate
  emitter input, not part of the IR.

The packages are layered along the pipeline, and a test enforces the import graph: compilers and
emitters never import each other ([Architecture](docs/architecture.md) §3).

## Design principles

Each of these is a load-bearing decision, not a preference. [Architecture](docs/architecture.md)
and [IR design](docs/ir/README.md) give the reasoning.

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
  values, and the engine decides whether they fail the run, at a severity threshold the caller
  sets.
- **Heuristics are policy.** Anything inferred rather than declared, such as which media types
  imply a stream or which vendor extensions fill typed fields, is an injectable policy the caller
  can change or switch off, and what it produces is marked as inferred in the IR's provenance.
- **Untrusted input is bounded.** Unless `allow-external-refs` is set, a `$ref` reads only files
  inside the spec's directory, and only through relative references. Budgets on source size, parsed
  nodes, enum members and YAML alias expansion turn a pathological document into a diagnostic
  instead of an exhausted host.

## Status

| Milestone | Scope | State |
|---|---|---|
| 1 | IR package + OpenAPI 3.x compiler, `validate` pass, golden corpus, JSON round-trip | **Implemented** |
| 2 | Swagger 2.0 lift into the OpenAPI compiler (format-version-normalization seam) | Planned |
| 3 | First emitters: Go, Kotlin/JVM and Kotlin Multiplatform end to end from one plan (plan / refine / emit boundary) | Planned |
| 4 | Second family compiler (TypeSpec or Smithy), proving the spec-agnostic claim | Planned |
| 5 | Event-shaped compiler (AsyncAPI), then GraphQL, Protobuf, Erlang/OTP | Planned |

The IR is designed against every format above, so a later compiler fills fields rather than adding
them. Where one meets a construct the IR has no place for yet, it keeps the construct verbatim, and
the IR grows by a versioned change ([Architecture](docs/architecture.md) §2.5). Until milestone 2, a
Swagger 2.0 document is recognized and refused with `engine/no-compiler-for-format`.

## Documentation

Using Morphic:

| Document | Description |
|---|---|
| [CLI reference](docs/cli.md) | Commands and flags, diagnostics and triage, the JSON report, exit codes, and the OpenAPI compiler's options. |
| [Go library](docs/library.md) | The engine from Go: its inputs and options, its errors and verdict, and rendering and writing as the CLI does. |

The design documents are normative. Read them before proposing changes to the IR or the pipeline:

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Pipeline stages, package layout, layering rules, milestones. |
| [IR design](docs/ir/README.md) | The intermediate representation, one document per domain: node catalog and semantics. Its field shapes are the contract. |
| [OpenAPI lowering](docs/compilers/openapi.md) | How the OpenAPI compiler lowers each construct, one keyed rule each, with the cases, codes and tests that pin it. |
| [Planned lowerings](docs/compilers/planned-lowerings.md) | How each format without a lowering specification of its own is planned to lower. |
| [Spec capability matrix](docs/ir-spec-matrix.md) | What each source format can express: the union the IR is designed against. |
| [Emitter design](docs/emitters/README.md) | The emitter contract, the plan / refine / emit boundary, and what each target decides. |

## Contributing

Start from [CONTRIBUTING.md](CONTRIBUTING.md), which says which documents a change needs, walks the
common changes step by step, and lists the checks. One command runs everything CI runs, and it must
pass before a change lands:

```bash
make gate
```

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

Licensed under the [MIT License](LICENSE). Copyright © 2026 dexpace.
