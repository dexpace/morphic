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
  values, and the engine decides whether they fail the run, at a severity threshold the caller
  sets.
- **Heuristics are policy.** Anything inferred rather than declared, such as which media types
  imply a stream or which vendor extensions fill typed fields, is an injectable policy the caller
  can change or switch off, and what it produces is marked as inferred in the IR's provenance.
- **Untrusted input is bounded.** Unless `allow-external-refs` is set, a `$ref` reads only files
  inside the spec's directory, and only through relative references. Budgets on source size, parsed
  nodes, enum members and YAML alias expansion turn a pathological document into a diagnostic
  instead of an exhausted host.

## Pipeline

```
spec ──▶ compiler ──▶ IR ──▶ passes ──▶ IR ──▶ emitter ──▶ SDK / docs
        (spec → IR)         (IR → IR)          (IR → artifacts)
```

- **Compilers** (`compilers/*`) turn one source format into an IR document plus diagnostics. Each
  recognizes its own format from the bytes and owns its options. OpenAPI 3.x is the first; the
  formats planned after it are in [Status](#status).
- **Passes** (`pass/`) are small, order-explicit IR → IR transforms. `validate` checks referential
  integrity and the well-formedness rules the IR states, and runs by default. Deduplication,
  filtering, version slicing and IR overlays are designed in [Architecture](docs/architecture.md)
  but not built.
- **Emitters** (`emitters/*`, not built yet) turn an IR document into the artifacts for one
  target. SDK runtime policy (retries, timeouts, telemetry, the error taxonomy) is a separate
  emitter input, not part of the IR.

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

`compile` lowers one spec into IR JSON and writes diagnostics to stderr. The spec is a regular file,
read no further than the compiler's byte budget (`max-source-bytes`) allows: a pipe, a FIFO such as
`<(…)` and a device are refused with exit `2` before they are opened, since a read of one can wait
for a writer or stream without end. Stdout is indented for reading; a file written with
`-o`/`--output` is compact, about half the bytes, unless `--pretty` asks for the indented form.
`validate` runs the same pipeline and drops the document, for a check that wants the diagnostics and
the exit code and nothing else.

`morphic help` lists the commands, and `morphic help <command>` or `morphic <command> --help`
prints a command's description, examples and flags. Help always goes to stdout and exits `0`.
Long flags take two dashes (`--fail-on`); `-o`, `-h` and `-v` are the only short ones. A long
flag typed with one dash is refused, with the two-dash spelling to use.

`morphic --version` prints the build and the version of the IR schema its output follows.
`morphic completion bash|zsh|fish|powershell` prints a completion script for that shell, which
completes commands, flags, and spec files by extension.

| Flag | Commands | Meaning |
|---|---|---|
| `--fail-on error\|warning\|info\|none` | both | Exit non-zero when a diagnostic at or above this severity is emitted (default `error`). `none` exits non-zero only when the spec lowered to no document. |
| `--disable-pass <name>` | both | Turn off the IR pass with this name, such as `validate`. Repeatable; a name no pass carries is refused with exit `2` and a message naming the passes there are. |
| `--skip-validate` | both | Alias for `--disable-pass validate`. |
| `--verify` | both | Check the IR the compiler and each pass return against the IR's structural invariants, and that no pass changes the document it was handed. The first stage to break one ends the run with exit `2`, listing what it broke. For developing a compiler or a pass: it slows the run. |
| `--opt <key>=<value>` | both | Set one option on the compiler the spec selects. Repeatable; a repeated key is refused. |
| `-o`, `--output <file>` | `compile` | Write IR JSON to `<file>` instead of stdout, compact rather than indented. |
| `--pretty` | `compile` | Indent the JSON `--output` writes; stdout is indented either way. |
| `--explain <json-pointer>` | `compile` | Instead of writing the document, report what compiling produced at this source coordinate: the type node there, the coordinates beneath it, and the diagnostics stamped at it. `''` is the whole document. Refused together with `--output`, which it would otherwise ignore. |

#### Diagnostics

Diagnostics print one per line as `<severity> <code> <location>: <message>`:

```
error openapi/unresolved-ref openapi.yaml#/components/schemas/Pet/discriminator/mapping/dog: discriminator mapping "dog" references unresolved schema "#/components/schemas/Dog": it names nothing in the document it points into
```

`<location>` is the most precise form available:

- `<path>#<pointer>` for a finding at a JSON Pointer in a spec file;
- `<path>:<line>:<column> (#<pointer>)` for one the loader made on a node of the parsed tree, which
  has both: the position first, so a terminal or editor links it, and the pointer after;
- `<path>:<line>:<column>` for one on the file's top-level object, or in a part of the file no
  pointer reaches, such as a later document of a YAML stream, or `<path>:<line>` when the column is
  unknown;
- `<path>` alone for one about the file as a whole, including a spec that was refused or that no
  compiler recognized;
- the bare IR location for one an IR pass made about the document.

#### Exit codes

Both commands share them.

| Code | Meaning |
|---|---|
| `0` | Clean, or a help request. |
| `1` | The spec has problems: a diagnostic reached the `--fail-on` threshold, or the spec could not be lowered at all (an undecodable file, an unrecognized or unsupported format, a version no compiler claims). |
| `2` | The invocation or the filesystem was wrong, the run was stopped, or something inside morphic failed: a bad flag or argument, a spec that could not be read, an output that could not be written, a SIGINT, SIGTERM or SIGHUP, or a compiler or pass that broke its contract, which `--verify` also holds to the IR's invariants. Nothing in the spec's own contents leads to `2`. |

One run can earn both `1` and `2`: a spec that reached the threshold, whose `-o` destination then
refused the write. The verdict on the spec wins, so `1` always means the spec has problems, and the
write error is printed on stderr either way. `-o` publishes by rename, so a destination whose
directory will not take a temp file, such as `/dev/null` or a read-only directory, cannot be
written to at all.

SIGINT, SIGTERM or SIGHUP stops a run, which exits `2` whatever it had found, naming the signal on
stderr; a shell script interrupted with Ctrl-C goes on to its next command unless it checks that
status, as `set -e` does. A stopped run writes no document: a file `-o` names keeps its previous
content, its temporary file removed, and stdout gets none of it. Stopped during the compile, it
writes nothing at all; stopped during `compile`'s diagnostics, it finishes them, and no document or
`--explain` report follows. A signal that comes once the run's last output has begun is too late to
stop it: once the document is being written to stdout or moved into place over `-o`'s file, once an
`--explain` report has begun, or, for `validate`, whose diagnostics are all it writes, once the
compile is over. The run then finishes and exits as though the signal had not been sent, so it
never reports a stop for a document it wrote. Another signal while the run is stopping changes
nothing, so a copy that a shell or a wrapper delivers along with the first cannot cut the cleanup
short, and a signal that comes as morphic exits does not end it by that signal either. Stopping can
take a few seconds on a large spec; SIGQUIT (`Ctrl-\`), which prints every goroutine's stack, or
SIGKILL ends morphic sooner, and either can leave the temporary file behind: a dot, the
destination's name, then digits. A SIGINT or SIGHUP morphic was started ignoring stays ignored, as a
script's shell starts a background job ignoring SIGINT and `nohup` a command ignoring SIGHUP.

#### Compiler options

`--opt` names an option in the vocabulary of whichever compiler recognizes the spec. Morphic itself
knows none of them, and the compiler refuses a name it does not know rather than ignoring it. The
OpenAPI compiler accepts:

| Option | Values | Meaning |
|---|---|---|
| `grouping` | `tags` (default), `path-prefix` | How operations are grouped into operation groups. |
| `allow-external-refs` | `true`, `false` (default) | Let `$ref` resolution read documents outside the spec's directory: files anywhere (by path or local `file:` URI) and http(s) URLs. Off, only a relative `$ref` into another document is read, and only a regular file inside the spec's directory tree, symbolic links included; any other, an absolute path or `file:` URI among them, is reported unresolved, whatever another `$ref` names the same file by. |
| `overlay` | a file path | Apply an [OpenAPI Overlay](https://spec.openapis.org/overlay/latest.html) document to the source before lowering. It is read as the spec is: a regular file, within `max-source-bytes`. |
| `overlay-lax` | `true`, `false` (default) | Do not refuse when an overlay action's selector matches nothing. |
| `nullable-widening` | `true` (default), `false` | Read `nullable: true` as admitting null wherever a schema writes it, and keywords beside a `$ref` that admit null as a nullable reference, marking each reference only that reading makes nullable as inferred. `false` reads them only as the document's OpenAPI version does. |
| `max-source-bytes` | an integer | The byte budget for one document: the spec, the overlay, and each document a `$ref` reads. None is read more than a byte past it. |
| `max-source-nodes` | an integer | The budget on the YAML nodes one document parses to. |
| `max-enum-members` | an integer | The budget on the members of one enum. |
| `max-alias-surplus` | an integer | The budget on the nodes YAML aliases may add to a document. |
| `max-external-documents` | an integer | The budget on how many documents external references read in one compile. |

Each `max-*` option sets the `openapi.Limits` budget it names, whose default is documented there:
`0` takes the default, and a negative value turns the budget off.

```bash
morphic compile openapi.yaml --opt grouping=path-prefix --opt overlay=patch.yaml
```

### Library

The same pipeline is available as a Go package. `engine.New` builds the default engine: every
built-in compiler and the default IR passes. `engine.WithCompilers` and `engine.WithPasses` replace
either set, so an embedder can run a pass of its own, composed with `engine.Passes`, the default
list in order; a pass changes types through an `ir.Editor`. `Run` reads the spec, no further than a
compiler would read it, asks the registered compilers which one recognizes it, compiles, and runs
the passes in order, each over the document the one before it returned. `RunOptions.DisablePasses`
turns passes off by name (`engine.ValidatePass` names the `validate` pass), and a name no pass
carries fails the run with `engine.ErrUnknownPass`. A compiler breaking its contract, say by
returning a document whose source table does not describe the spec, or by panicking, fails the run
with an error wrapping `compilers.ErrContract`; a pass doing so fails it with one wrapping
`pass.ErrContract`. A panic arrives as a `*barrier.PanicError` in the chain, which keeps the value
and the stack beside the one-line message. A stage that ignores `ctx` holds `Run` until it returns,
and the run then fails with the context's error, whatever the stage returned. The structural duties
only `irverify` checks are left to the harness, unless `RunOptions.Verify` is set: it holds each
stage's document to them, and each pass to leaving its input alone, and fails the run with an
`*engine.VerifyError` listing what broke.

```go
eng, err := engine.New()
if err != nil {
    return err
}

res, err := eng.Run(ctx, "openapi.yaml", engine.RunOptions{
    CompilerOptions: map[string]string{"grouping": "path-prefix"},
})
if err != nil {
    return err // misuse, I/O or a broken contract; problems in the spec arrive as diagnostics
}

// The CLI's own rendering, one diagnostic per line.
if err := report.WriteDiagnostics(os.Stderr, res.Diagnostics, res.Sources); err != nil {
    return err
}
if res.Failed {
    return errors.New("the spec has problems") // no document, or a diagnostic at FailOn
}

// Canonical, deterministic bytes, replacing the file atomically as `-o` does.
return output.ReplaceDocument(ctx, "api.ir.json", res.Document, output.Compact)
```

`RunSpec` compiles a spec held in memory, such as an edited buffer, as the file at its path, and
reads every other document from disk as `Run` does. `RunSources` compiles sources already in
memory: the first is the spec, every source is a document its references or its settings may name
by its `Path`, and nothing else is read, so a spec split across files compiles without a file
written. `RunDocument` runs the passes over a document already compiled, such as one decoded from
a file. `engine/report` renders diagnostics and `--explain` (`report.Explain`) exactly as the CLI
prints them, and `engine/output` writes a document as the CLI does: to a stream whole or not at
all, or to a file replaced atomically. Both take the run's context, and a write it cancels leaves
no partial document behind.

A Go error from `Run` means misuse, I/O or a stage breaking its contract. Everything wrong with the spec itself, including a spec
no compiler can lower, is a diagnostic in the result, with a nil `Document` when nothing could be
lowered. Options go in either as text through `RunOptions.CompilerOptions`, the CLI's `--opt`
vocabulary, or as a typed value such as an `openapi.Options` through `RunOptions.FormatOptions`;
set one or the other, not both.

`Result.Failed` is the engine's verdict, the one the CLI's exit status reads: a run fails when it
lowered no document, or when a diagnostic reaches `RunOptions.FailOn`, which is `error` unless set
to `warning`, `info` or `none`. A failed run still carries its document and every diagnostic.

## Package layout

The import graph is layered, and `internal/archtest` enforces it: a package imports only what its
entry in that test allows, and nothing from a layer above its own.

| Package | Layer | Imports |
|---|---|---|
| `barrier/` | 0: turns a panic in code the caller does not own into an error, and quotes what that code says on one bounded line | stdlib only |
| `ir/` | 0: IR nodes, IDs, traversal, editing, JSON round-trip | stdlib only |
| `ir/irfacts/` | 0: what a document implies and does not state (subtypes, exposed properties, usage, recursion), never stored | `ir` only |
| `ir/irverify/`, `ir/irtest/` | 0: structural-invariant checker; golden-snapshot and fixture helpers | `ir` (`irtest` also `go-cmp`) |
| `compilers/` | 1: the `Compiler` contract and the format-keyed registry | `ir` + `barrier` |
| `compilers/compile/` | 1: what every compiler shares (type registry, diagnostics, naming and identifier grammars) | `ir` only |
| `compilers/*` | 1: one compiler per format; a public face over its own `internal/` packages | `ir` + `compilers` + `compilers/compile` + own `internal/*` + format libs |
| `pass/` | 1: IR → IR passes | `ir` + `ir/irfacts` |
| `emitters/*` | 2: IR → artifacts (not built yet) | `ir` + emitter contract |
| `engine/` | 3: orchestration | everything below |
| `engine/reader/`, `engine/report/`, `engine/output/` | 3: what the engine reads, renders and writes | `compilers`; `ir`; `ir` + `engine/internal/atomicfile` (over `renameio`) |
| `cmd/morphic/` | 4: CLI | `engine` (and its `report` and `output`) + `ir` + its command framework (`cobra`, `pflag`) |
| `cmd/morphic-harness/`, `cmd/morphic-census/`, `cmd/morphic-covergate/`, `cmd/morphic-vet/`, `internal/*` | tooling outside the pipeline: the oracle sweep, the census of IR facts, the coverage gate's judge, the go vet tool and the lint rules it runs, the architecture rules, the goroutine leak check, type-checked reads of declarations for tests, the IR's JSON shape that the IRVersion pin and the census read, the reader of the keyed documents, shared fixtures | as their entries allow; `internal/harness` is exempt |

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
pins, so the local command and CI run the same steps; CI splits them across two jobs that run side
by side. The `Makefile` holds the step list, and each target (`make coverage`, `make fuzz`,
`make bench` and the rest) runs on its own while iterating. Coverage is gated at exactly 100% of
statements, counted from the profile rather than from `go test`'s rounded percentage, so one
uncovered statement fails the build.

Run a single test with `go test ./ir -run TestName`. Beyond that:

- **Golden snapshots.** `testdata/golden/` and `testdata/conformance/` pair each spec with the IR
  it must compile to, byte for byte, except the `irVersion` stamp: the helper asserts it rather than
  storing it, so moving the version rewrites no golden. After an intentional change, rewrite them
  and review the diff:

  ```bash
  go test ./compilers/openapi/e2e -run 'TestGolden|TestConformance' -update
  ```

- **Conformance corpus.** Each case in `testdata/conformance/openapi/` is a minimal spec that
  witnesses one or more rows of the [spec capability matrix](docs/ir-spec-matrix.md). To add one,
  write `<name>.yaml`, add an entry to `conformanceCases()` in
  `compilers/openapi/e2e/conformance_test.go` naming its assertion, list it under the matrix rows it
  witnesses in `testdata/conformance/openapi/matrix.md`, then run the command above to write
  `<name>.golden.json`, and name the case in the rule of the
  [OpenAPI lowering specification](docs/compilers/openapi.md) it pins. The suite fails on a spec
  with no entry, on a case no rule names, and on a matrix row OpenAPI can express that no case
  witnesses, unless `matrix.md` lists the row as uncovered with a reason.
- **Oracle harness.** `go run ./cmd/morphic-harness <file|dir>` runs specs as
  `morphic compile --verify` does, passes included, through the bug-catching oracles, stopping at
  the first that fires for each: no panic, the `ir/irverify` structural invariants, no error
  diagnostic, JSON round-trip, determinism, pass idempotence, and order-invariance. It exits `1` if
  any spec fails. `testdata/` holds negative fixtures that fail on purpose, so sweep it with
  `go test ./internal/harness`, which knows which ones are expected to.
- **IR census.** `go run ./cmd/morphic-census <file|dir>...` compiles the specs and lists every
  fact of the IR with the number of documents that set it and the number of instances, zeros
  included: each member of its structs, the emptiness of a member whose empty value differs from
  its absence, and each value of its enumerations. The facts no committed spec sets are held to
  `testdata/unwitnessed.golden.txt`, which `go test ./cmd/morphic-census -update` rewrites and
  which scopes the first emitters (`docs/emitter-design.md` §14.1). Run it from inside the module:
  it reads the IR's declarations through the go command.
- **Fuzzing.** `make fuzz` runs every fuzz target for 10s each (`make fuzz FUZZTIME=5m` for a
  longer search). One target runs on its own with, for example,
  `go test ./compilers/openapi/e2e -run '^$' -fuzz '^FuzzCompile$' -fuzztime 1m`.

## Design docs

The design documents are normative. Read them before proposing changes to the IR or the pipeline,
and start a first contribution from [CONTRIBUTING.md](CONTRIBUTING.md), which says which of them a
change needs.

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Pipeline stages, package layout, layering rules, milestones. |
| [IR design](docs/ir-design.md) | The intermediate representation: node catalog and semantics, and how each format without a lowering specification of its own is planned to lower. Its field shapes are the contract. |
| [OpenAPI lowering](docs/compilers/openapi.md) | How the OpenAPI compiler lowers each construct, one keyed rule each, with the cases, codes and tests that pin it. |
| [Spec capability matrix](docs/ir-spec-matrix.md) | What each source format can express: the union the IR is designed against. |
| [Emitter design](docs/emitter-design.md) | The emitter contract and the plan / refine / emit boundary. |

## License

Licensed under the [MIT License](LICENSE). Copyright © 2026 dexpace.
