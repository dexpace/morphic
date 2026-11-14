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
> is no released version, and the IR schema and the CLI may change between commits ([what a
> consumer storing IR pins](docs/ir/consumers.md#what-to-pin)).

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
flag typed with one dash is refused, with the two-dash spelling to use. A command's flags may come
before its name, as in `morphic --verify compile openapi.yaml`, and are read as the command reads
them.

`morphic --version` prints the build and the version of the IR schema its output follows.
`morphic completion bash|zsh|fish|powershell` prints a completion script for that shell, which
completes commands, flags, and spec files by extension.

| Flag | Commands | Meaning |
|---|---|---|
| `--fail-on error\|warning\|info\|none` | both | Exit non-zero when a finding is rated at or above this severity and no `--triage` rule accepted it (default `error`). `none` exits non-zero only when the spec lowered to no document. |
| `--disable-pass <name>` | both | Turn off the IR pass with this name, such as `validate`. Repeatable; a name no pass carries is refused with exit `2` and a message naming the passes there are. |
| `--skip-validate` | both | Alias for `--disable-pass validate`. |
| `--verify` | both | Check the IR the compiler and each pass return, and the findings each reports, against the IR's structural invariants, and that no pass changes the document it was handed. The first stage to break one ends the run with exit `2`, listing what it broke. For developing a compiler or a pass: it slows the run. |
| `--opt <key>=<value>` | both | Set one option on the compiler the spec selects. Repeatable; a repeated key is refused. |
| `--cache-dir <dir>` | both | Keep compiled documents in `<dir>`, and answer a later run from there while nothing a document depends on has changed: this build, the spec, where it lies and every document it reads, the settings, and the passes. Output and exit status are the same either way. A cache that cannot be used costs the time and a warning on stderr; a `--verify` run does not use it. The directory holds its entries to `cache.MaxBytes`, 1 GiB, those unused longest going first, though they can pass it by an eighth between prunes. It is safe to share between processes, but not with anyone you would not let replace the binary: an entry is trusted as it is. |
| `--triage <file>` | both | Rate or accept findings by the rules this JSON file lists ([Triage](#triage)). A file that cannot be read, or holds a rule no run can apply, is refused with exit `2` before anything compiles. |
| `--diag-format text\|json` | both | Write the findings to stderr as text (the default), or as one [JSON report](#json-report). |
| `--show error\|warning\|info` | both | In text, write each finding rated at or above this severity, and count the rest (default `info`). Refused beside `--diag-format json`. |
| `--diag-limit <n>` | both | In text, write the first `<n>` findings of each code and severity, and count the rest (default `3`); `0` writes every one. Refused beside `--diag-format json`. |
| `-o`, `--output <file>` | `compile` | Write IR JSON to `<file>` instead of stdout, compact rather than indented. |
| `--pretty` | `compile` | Indent the JSON `--output` writes; stdout is indented either way. |
| `--explain <json-pointer>` | `compile` | Instead of writing the document, report what compiling produced at this source coordinate: the type node the document holds there, the coordinates beneath it, each construct kept verbatim under `Unmodeled` at or beneath it with the node keeping it and its reason, and the diagnostics stamped at it, as their stages reported them. A pass may replace or remove a node the compiler made, which `--disable-pass` shows. `''` is the whole document. Refused together with `--output`, which it would otherwise ignore. |

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

Each finding is written at the severity it is rated at, which is the one its stage reported unless
a [triage](#triage) rule rates it otherwise. A spec can draw thousands of findings, so the text
format writes the first three of each code and severity, in the order they were reported, and
counts the rest; `--diag-limit 0` writes every one, and `--show warning` or `--show error` counts
the findings below that severity instead of writing them. After the findings, each on a line
indented so no reader of `<severity> <code>` lines takes it for a finding, come a line for each code
and severity some findings of which were not written, a line for each triage rule that matched no
finding, and, once anything was left unwritten, a tally:

```
  1303 more info openapi/degraded-construct not shown
  triage rule for openapi/unresolved-ref at /components/schemas/Pet matched no finding
  32 of 1890 findings shown: 4 error, 222 warning, 1664 info, 0 accepted
```

Every finding stays in the [JSON report](#json-report), which writes them all. A spec writes the
keys, references and messages a line quotes, so a control character, a format character such as a
bidirectional override, or a byte that is not UTF-8 is written escaped as Go's `%q` spells it
(`\x1b`, `\n`, `\u202e`, `\xff`), in the findings and in `--explain`'s report alike: a spec cannot
start a line of its own or send a terminal a control sequence.

#### Triage

A triage file lists rules about the findings of a run, each naming one diagnostic code, matched
exactly, and a reason:

```json
{"rules": [
  {"code": "openapi/unresolved-security-scheme", "severity": "warning",
   "reason": "the gateway names these schemes, and the SDK sends none of them"},
  {"code": "openapi/degraded-construct", "pointer": "/components/schemas/Pet/readOnly", "accept": true,
   "reason": "Pet is only ever read"}
]}
```

- `severity` rates the code's findings at `error`, `warning` or `info`, which is what `--fail-on`
  and `--show` then read. A finding a stage reported at a severity the IR does not declare still
  ends the run with exit `2`, as a stage breaking its contract, whatever a rule says of it.
- `accept: true` accepts the findings, which then fail the run at no `--fail-on`, `info` included,
  and are counted rather than written. A rule accepting findings names their place, so it accepts
  none its code reports elsewhere. It does accept every finding of its code at that place, one
  reported later included, and a place can hold a whole scope: a pass reports a collision between
  two of the document's types at `doc`, so a rule accepting one there accepts the next (#1825).
- `pointer` places the rule at the findings this JSON Pointer locates in the spec, the file
  compiled, an overlay's writing into it included; `node` places it at the findings an IR pass
  located at that IR node, such as `s/openapi/0`. A rule names at most one place, and one naming a
  place outranks one naming only the code. A finding in a document a `$ref` reads (#1804), or
  located by a line alone, can be ruled by its code only.

A rule names no pattern: a `*` in a code is refused, as is a rule both rating and accepting, an
acceptance naming no place, two rules ruling the same findings, a pointer that is not RFC 6901, a
member the file does not define and a missing reason. A rule no finding matched is named after the
findings, as many of each code as of a code's findings, and listed as `unusedRules` in the JSON
report: unless a pass it was written for is off, the finding it names is gone, and the rule can go
too. A run that lowered no document stopped short of most findings, so it names no rule unused. A
file is read as a spec is, no further than 16 MiB.

A file accepting each finding a run now draws at a place can start from the run's own report:

```bash
morphic validate openapi.yaml --diag-format json 2>&1 >/dev/null |
  jq '{rules: [.findings[].diagnostic
    | if .provenance.source == 0 and .provenance.pointer then {code, pointer: .provenance.pointer}
      elif .provenance.node then {code, node: .provenance.node} else empty end
    | . + {accept: true, reason: "accepted as found"}] | unique}' > triage.json
```

Write each `reason` yourself before relying on it: it is what the next reader has to go on. A rule
the recipe places at a scope, such as `doc`, accepts every later finding of its code about that
scope too.

#### JSON report

`--diag-format json` writes one JSON object to stderr, then a newline, for a program to read; the
run's exit status and stdout are what text gives. It is written for every run that reaches its
verdict, a spec that lowered to no document included, and nothing else is written to stderr then,
not even a warning about the cache. A run that exits `2` before its verdict writes text instead, its
warnings and then `morphic: ...`. One stopped after its report, or whose document is then refused,
adds that line after the report, the report staying the first line, whatever status it exits with:
a refused document leaves a run its findings failed at `1`, as [Exit codes](#exit-codes) says.

```json
{"version": 1, "failed": false,
 "sources": [{"format": "openapi@3.1", "path": "openapi.yaml", "hash": "…"}],
 "findings": [{"diagnostic": {"severity": "error", "code": "openapi/unresolved-security-scheme",
                              "message": "…", "provenance": {"source": 0, "pointer": "/security/0/key"}},
               "severity": "warning",
               "rule": {"code": "openapi/unresolved-security-scheme", "severity": "warning", "reason": "…"}}],
 "unusedRules": [],
 "warnings": []}
```

| Member | Holds |
|---|---|
| `version` | the report's schema version, `1` |
| `failed` | the verdict, the one the exit status reads |
| `sources` | the files each `provenance.source` indexes, as an IR document's `sources` |
| `findings` | every finding, in the order reported: `diagnostic` as its stage reported it, an IR diagnostic, `severity` as it is rated, `accepted` when a rule accepted it, and `rule`, the rule it met |
| `unusedRules` | the triage rules no finding matched |
| `warnings` | what kept the run from working as asked without failing it, such as a cache it could not use, one line each |

Each list is present, empty when there is nothing in it. A change to any member's shape, a
diagnostic's included, moves `version`, which `TestSchemaVersion_PinsTheEncodedShape` in
`engine/report` holds to the encoded shape, so a consumer that checks it knows every member it
will meet. A string is escaped as JSON escapes it, unlike the text format: a C1 control or a
bidirectional override a spec writes stays raw, as it does in the IR document (#1826).

#### Exit codes

Both commands share them.

| Code | Meaning |
|---|---|
| `0` | Clean, or a help request. |
| `1` | The spec has problems: a finding no triage rule accepted reached the `--fail-on` threshold, or the spec could not be lowered at all (an undecodable file, an unrecognized or unsupported format, a version no compiler claims). |
| `2` | The invocation or the filesystem was wrong, the run was stopped, or something inside morphic failed: a bad flag or argument, a spec or triage file that could not be read, an output that could not be written, a SIGINT, SIGTERM or SIGHUP that stopped the run, or a compiler or pass that broke its contract, which `--verify` also holds to the IR's invariants. Nothing in the spec's own contents leads to `2`. |

One run can earn both `1` and `2`: a spec that reached the threshold, whose `-o` destination then
refused the write. The verdict on the spec wins, so `1` always means the spec has problems, and the
write error is printed on stderr either way.

`-o` replaces a file by renaming a temporary file over it, so a reader of the file sees the whole
previous document or the whole new one, never part of either. The file's directory must therefore
take a new entry: a writable file in a read-only directory is refused, as writing it in place would
give up that guarantee. Other hard links keep the old content. A FIFO or a character device, such
as `/dev/null` or the pipe a shell's `>(…)` names, has no content to replace and is written as
stdout is; a FIFO no process reads is refused rather than waited for. A directory, a socket or a
block device is refused. A symlink is judged by what it names, and one naming a file, or nothing, is
replaced, not followed. So `-o /dev/stdout`, with stdout redirected to a file, needs `/dev` to take
a temporary file: it is refused, or, run as root, replaces the link (#1864).

SIGINT, SIGTERM or SIGHUP stops a run, which exits `2` whatever it had found, naming the signal on
stderr; a shell script interrupted with Ctrl-C goes on to its next command unless it checks that
status, as `set -e` does. A stopped run writes no document: a file `-o` names keeps its previous
content, its temporary file removed, and stdout gets none of it. Stopped during the compile, it
writes nothing at all; stopped during `compile`'s diagnostics, it finishes them, and no document or
`--explain` report follows. A signal that comes once the run's last output has begun is too late to
stop it: once the document is being written to stdout or to the FIFO or device `-o` names, or
moved into place over `-o`'s file, once an `--explain` report has begun, or once the compile is over
for a run that writes nothing after its diagnostics, `validate` or `compile` of a spec that lowered
to no document. The run then finishes
and exits as though the signal had not been sent, so it never reports a stop for a document it
wrote. Another signal while the run is stopping changes nothing, so a copy that a shell or a wrapper
delivers along with the first cannot cut the cleanup short, and a signal that comes as morphic
exits does not end it by that signal either. Stopping can take a few seconds on a large spec;
SIGQUIT (`Ctrl-\`), which prints every goroutine's stack, or SIGKILL ends morphic sooner, and
either can leave the temporary file behind: a dot, the destination's name, then digits. A SIGINT or
SIGHUP morphic was started ignoring stays ignored, as a script's shell starts a background job
ignoring SIGINT and `nohup` a command ignoring SIGHUP.

#### Compiler options

`--opt` names an option in the vocabulary of whichever compiler recognizes the spec. Morphic itself
knows none of them, and the compiler refuses a name it does not know rather than ignoring it. The
OpenAPI compiler accepts:

| Option | Values | Meaning |
|---|---|---|
| `grouping` | `tags` (default), `path-prefix`, `extension` | How operations are grouped into operation groups: by tag, by their path's first segment, or by the group path a vendor extension states, a group nested for each segment. |
| `grouping-key` | an `x-` extension name | With `grouping=extension`, the extension stating an operation's group path, named together with `grouping-lists`. Naming neither reads `x-fern-sdk-group-name` with `segments`, and its aliases in `x-forge-aliases`. |
| `grouping-lists` | `segments`, `groups` | What a list under `grouping-key` means: the segments of one path, or several paths, the operation filed under the first and a group made for each other. A string is one path, its segments separated by dots. |
| `grouping-aliases` | an `x-` extension name | With `grouping=extension`, the extension listing an operation's aliases, each an object stating a further group path under `grouping-key`, for which a group is made. Naming neither `grouping-key` nor `grouping-lists` reads `x-forge-aliases`, and naming them reads none unless this names one. An embedded-policy mapping mounting operations in those groups must read the same extension ([`extension-groups`](docs/compilers/openapi.md#extension-groups)). |
| `grouping-fallback` | `tags` (default), `path-prefix` | With `grouping=extension`, how an operation whose extension states no path is grouped. |
| `allow-external-refs` | `true`, `false` (default) | Let `$ref` resolution read documents outside the spec's directory: files anywhere (by path or local `file:` URI) and http(s) URLs. Off, only a relative `$ref` into another document is read, and only a regular file inside the spec's directory tree, symbolic links included; any other, an absolute path or `file:` URI among them, is reported unresolved, whatever another `$ref` names the same file by. Either way, a reference in a document read over http(s) reads only http(s) URLs. |
| `overlay` | a file path | Apply an [OpenAPI Overlay](https://spec.openapis.org/overlay/latest.html) 1.0 or 1.1 document to the source before lowering. It is read as the spec is: a regular file, within `max-source-bytes`. |
| `overlay-lax` | `true`, `false` (default) | Report an overlay action whose selector matches nothing as a warning, and apply the rest, rather than refuse the compile. |
| `nullable-widening` | `true` (default), `false` | Read `nullable: true` as admitting null wherever a schema writes it, and keywords beside a `$ref` that admit null as a nullable reference, marking each reference only that reading makes nullable as inferred. `false` reads them only as the document's OpenAPI version does. |
| `discriminator-inference` | `true` (default), `false` | Read a union's tags off the strings each variant pins its tag property to: as the mapping of a `discriminator` declaring none, where OpenAPI's implicit mapping would route a variant by a component name it rejects, or not by every string it admits, and as a discriminator for a closed `oneOf` or `anyOf` declaring none, where every variant requires one property and pins it to strings no other variant admits; and read a subtype's tag off the strings it admits, where they rule out the component name its discriminator routes it by. What it reads is marked as inferred. `false` keeps only what the document declares. |
| `pagination-inference` | `true` (default), `false` | Read a `GET`'s or a `QUERY`'s pagination off the names it uses: a `page`, `page_number`, `offset`, `cursor`, `page_token`, `next_token` or `continuation_token` query parameter for where a walk continues and a `per_page`, `page_size` or `limit` one for the page size, and in each success response's body the list of items and, for a cursor or a token, the member handing back the next one. What it reads is marked as inferred, and an operation naming a walk it cannot settle is reported and left with none. An operation whose extensions, read under `extension-promotion`, speak of a pagination, filling one, failing to or stating it has none, is left to them. `false` keeps only what the document declares. |
| `idempotency-inference` | `true` (default), `false` | Read an operation's header parameter `Idempotency-Key`, in any letter case, as its idempotency token, where it states no idempotency and is sent by `POST`, `PUT`, `PATCH` or `DELETE`. What it reads is marked as inferred, and an operation it cannot settle, sent by a method only `additionalOperations` declares or carrying the header twice, is reported and left with none. `false` keeps only what the document declares. |
| `secret-inference` | `true` (default), `false` | Redact a parameter, header or property whose name ends in a credential's, such as `password`, `client_secret`, `api_key` or `access_token`, and whose value is text or bytes, where the document asks nothing about its redaction. Each use it redacts is marked as inferred. `false` redacts only what the document asks for. |
| `http-date-inference` | `true` (default), `false` | Write and read a `date-time` response header whose field an RFC defines as an HTTP-date, such as `Last-Modified` or `Expires`, as an HTTP-date, marking it as inferred and reporting it. `false` keeps the text its format names. |
| `replacement-inference` | `true` (default), `false` | Read a deprecation's replacement out of its message: the method and path of an operation the document declares, for an operation, and a sibling named in a code span, for a parameter or a property. What it reads is marked as inferred, and a message naming one beside a name that names nothing, or several, is reported and fills none. `false` leaves the replacement in the message alone. |
| `title-names` | `true` (default), `false` | Name an anonymous schema that writes a `title`, a union branch included, by the title's words, in place of the name its position composes (`variant_0`, its property's key, its owner's). `false` names every such schema by its position. The title is the schema's summary either way. |
| `streaming-media` | media types and `+suffix`es, separated by commas | The media types read as a stream of frames where a document declares no `itemSchema`, in place of the default list: `application/json-seq`, `application/jsonl`, `application/x-ndjson`, `text/event-stream` and `+json-seq`. An entry starting with `+` matches every media type with that structured suffix. |
| `streaming-inference` | `true` (default), `false` | Read a media type as a stream of frames, as `streaming-media` lists. `false` turns that reading off, whatever the list holds, leaving only what an `itemSchema` declares. |
| `binary-media` | media types, `type/*` wildcards and `+suffix`es, separated by commas | The media types under which a body or multipart part whose schema says no more than that it is a string is a file, in place of the default list: `application/gzip`, `application/pdf`, `application/vnd.tcpdump.pcap`, `application/wasm`, `application/zip`, `audio/*`, `font/*`, `image/*` and `video/*`. A `type/*` entry matches no media type with a structured suffix, so `image/*` leaves out `image/svg+xml`. Each file it reads is marked as inferred. `application/octet-stream` needs no entry: a body or part it carries is a file whatever the list holds. |
| `binary-inference` | `true` (default), `false` | Read a body or part as a file by its media type, as `binary-media` lists. `false` turns that reading off, leaving only the files a schema or `application/octet-stream` declares. |
| `extension-promotion` | `true` (default), `false` | Read the vendor extensions the promotion mapping names into the typed fields they fill, marking each node read as inferred. `false` keeps every extension verbatim and reads none. |
| `promotion-targets` | a JSON object | The promotion mapping, in place of the default: each target to its sources in order of precedence, each source a `key`, the `member` of its object value to read, and the `values` that state the target's fact, as `openapi.ExtensionPromotions.Targets` encodes as, such as `{"deprecation.message": [{"key": "x-why"}]}`. A target the compiler does not read is refused. |
| `promotion-prefixes` | extension name prefixes, separated by commas | The prefixes the promotion policy owns: an extension under one that nothing reads, by the promotion mapping or by grouping, its aliases key included, is reported, naming the key read nearest it. |
| `max-source-bytes` | an integer | The byte budget for one document: the spec, the overlay, and each document a `$ref` reads. None is read more than a byte past it. |
| `max-source-nodes` | an integer | The budget on the YAML nodes one document parses to. |
| `max-enum-members` | an integer | The budget on the members of one enum. |
| `max-alias-surplus` | an integer | The budget on the nodes YAML aliases may add to a document. |
| `max-external-documents` | an integer | The budget on how many documents external references read in one compile. |
| `max-reference-expansion` | an integer | The budget on what `$ref`s copy: each `$ref` to a declaration other than a schema lowers its own copy of it, and of each component alias on the way, weighed as a node for each YAML node, a YAML alias as a copy of what it names, a byte for each byte of scalar text, and a node for a schema it holds, which lowers once; and on what each operation a path item's `additionalOperations` declares copies of what the item declares. |

Each `max-*` option sets the `openapi.Limits` budget it names, whose default is documented there:
`0` takes the default, and a negative value turns the budget off. Each option that is `true` by
default switches a policy, and [`policies`](docs/compilers/openapi.md#policies) states how every
policy is switched, what it marks, what it yields to, and what it reports.

```bash
morphic compile openapi.yaml --opt grouping=path-prefix --opt overlay=patch.yaml
```

### Library

The same pipeline is available as a Go package. `engine.New` builds the default engine: every
built-in compiler and the default IR passes. `engine.WithCompilers` and `engine.WithPasses` replace
either set, so an embedder can run a pass of its own, composed with `engine.Passes`, the default
list in order; a pass changes a document through an `ir.Editor`. `Run` reads the spec, no further
than a compiler would read it, asks the registered compilers which one recognizes it, compiles, and
runs the passes in order, each over the document the one before it returned.
`RunOptions.DisablePasses` turns passes off by name (`engine.ValidatePass` names the `validate`
pass), and a name no pass carries fails the run with `engine.ErrUnknownPass`. A compiler breaking
its contract, say by returning a document whose source table does not describe the spec, or by
panicking, fails the run with an error wrapping `compilers.ErrContract`; a pass doing so fails it
with one wrapping `pass.ErrContract`. A panic arrives as a `*barrier.PanicError` in the chain, which
keeps the value and the stack beside the one-line message. A stage that ignores `ctx` holds `Run`
until it returns, and the run then fails with the context's error, whatever the stage returned. The
structural duties only `irverify` checks are left to the harness, unless `RunOptions.Verify` is set:
it holds each stage's document to them, and the findings each stage reports, and each pass to
leaving its input alone, and fails the run with an `*engine.VerifyError` listing what broke.
`engine.WithCache` answers a run from an earlier one with the same inputs, skipping detection, the
compile and the passes; `cache.NewDir` is a store on disk and `cache.BuildID` names the running
build. A cache that fails reports on `Result.CacheErr` and never fails the run
([Architecture](docs/architecture.md) §2.6).

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

// The CLI's own rendering, one finding per line, each one no rule accepted.
r := report.Report{Sources: res.Sources, Findings: res.Findings, UnusedRules: res.UnusedRules}
if err := report.WriteText(os.Stderr, r, report.TextOptions{}); err != nil {
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
a file. `engine/report` renders the findings and `--explain` (`report.Explain`) exactly as the CLI
prints them, and `engine/output` writes a document as the CLI does: to a stream whole or not at
all, or to a file replaced atomically. Both take the run's context, and a write it cancels leaves
no partial document behind.

A Go error from `Run` means misuse, I/O or a stage breaking its contract. Everything wrong with the spec itself, including a spec
no compiler can lower, is a diagnostic in the result, with a nil `Document` when nothing could be
lowered. Options go in either as text through `RunOptions.CompilerOptions`, the CLI's `--opt`
vocabulary, or as a typed value such as an `openapi.Options` through `RunOptions.FormatOptions`;
set one or the other, not both.

`Result.Failed` is the engine's verdict, the one the CLI's exit status reads: a run fails when it
lowered no document, or when a finding no rule accepted is rated at `RunOptions.FailOn` or above,
which is `error` unless set to `warning`, `info` or `none`. A failed run still carries its document
and every diagnostic.

`RunOptions.Triage` takes the [triage](#triage) rules, as `triage.Rule` values or read from a file
by `engine.ReadTriage`; a rule `triage.NewRuleset` refuses fails the run with an error wrapping
`triage.ErrInvalidRule` before the spec is read. `Result.Diagnostics` holds the findings as their
stages reported them, as the document does, and `Result.Findings` the same findings as rated, index
for index, each with the rule it met; `Result.UnusedRules` lists the rules that matched none. The
rules are applied once the run is in hand, so a cached run is rated by the rules of the run it
answers. `report.WriteText` renders the findings as the CLI's text does, and `report.WriteJSON` as
its JSON report.

## Package layout

The import graph is layered, and `internal/archtest` enforces it: a package imports only what its
entry in that test allows, and nothing from a layer above its own.

| Package | Layer | Imports |
|---|---|---|
| `barrier/` | 0: turns a panic in code the caller does not own into an error, and quotes what that code says on one bounded line | stdlib only |
| `ir/` | 0: IR nodes, IDs, traversal, editing, JSON round-trip | stdlib only |
| `ir/irfacts/` | 0: what a document implies and does not state (subtypes, exposed properties, usage, recursion, correlation keys), never stored | `ir` only |
| `ir/irverify/`, `ir/irtest/` | 0: structural-invariant checker; golden-snapshot and fixture helpers | `ir` (`irtest` also `go-cmp`) |
| `compilers/` | 1: the `Compiler` contract and the format-keyed registry | `ir` + `barrier` |
| `compilers/compile/` | 1: what every compiler shares (type registry, diagnostics, naming and identifier grammars) | `ir` only |
| `compilers/*` | 1: one compiler per format; a public face over its own `internal/` packages | `ir` + `compilers` + `compilers/compile` + own `internal/*` + format libs |
| `pass/` | 1: IR → IR passes | `ir` + `ir/irfacts` |
| `emitters/*` | 2: IR → artifacts (not built yet) | `ir` + emitter contract |
| `engine/` | 3: orchestration | everything below |
| `engine/triage/` | 3: the rules a caller writes about a run's findings, and the findings as rated | `ir` only |
| `engine/reader/`, `engine/report/`, `engine/output/` | 3: what the engine reads, renders and writes | `compilers`; `ir` + `engine/triage`; `ir` + `engine/internal/atomicfile` (over `renameio`) |
| `cmd/morphic/` | 4: CLI | `engine` (and its `report`, `output` and `triage`) + `ir` + its command framework (`cobra`, `pflag`) |
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
  which scopes the first emitters (`docs/emitters/milestones.md`). Run it from inside the module:
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
| [IR design](docs/ir/README.md) | The intermediate representation, one document per domain: node catalog and semantics. Its field shapes are the contract. |
| [OpenAPI lowering](docs/compilers/openapi.md) | How the OpenAPI compiler lowers each construct, one keyed rule each, with the cases, codes and tests that pin it. |
| [Planned lowerings](docs/compilers/planned-lowerings.md) | How each format without a lowering specification of its own is planned to lower. |
| [Spec capability matrix](docs/ir-spec-matrix.md) | What each source format can express: the union the IR is designed against. |
| [Emitter design](docs/emitters/README.md) | The emitter contract, the plan / refine / emit boundary, and what each target decides. |

## License

Licensed under the [MIT License](LICENSE). Copyright © 2026 dexpace.
