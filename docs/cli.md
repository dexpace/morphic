# The `morphic` command

The reference for the CLI: its commands and flags, how it reports what it finds in a spec, what
its exit status means, and the options the OpenAPI compiler takes. The [README](../README.md) has
the short version, and [Morphic as a Go library](library.md) the same pipeline called from Go.

## Commands

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

## Flags

| Flag | Commands | Meaning |
|---|---|---|
| `--fail-on error\|warning\|info\|none` | both | Exit non-zero when a finding is rated at or above this severity and no `--triage` rule accepted it (default `error`). `none` exits non-zero only when the spec lowered to no document. |
| `--disable-pass <name>` | both | Turn off the IR pass with this name, such as `validate`. Repeatable; a name no pass carries is refused with exit `2` and a message naming the passes there are. |
| `--skip-validate` | both | Alias for `--disable-pass validate`. |
| `--verify` | both | Check the IR the compiler and each pass return, and the findings each reports, against the IR's structural invariants, and that no pass changes the document it was handed. The first stage to break one ends the run with exit `2`, listing what it broke. For developing a compiler or a pass: it slows the run. |
| `--compiler-option <key>=<value>` | both | Set one option on the compiler the spec selects. Repeatable; a repeated key is refused. |
| `--cache-dir <dir>` | both | Keep compiled documents in `<dir>`, and answer a later run from there while nothing a document depends on has changed: this build, the spec, where it lies and every document it reads, the settings, and the passes. Output and exit status are the same either way. A cache that cannot be used costs the time and a warning on stderr; a `--verify` run does not use it. The directory holds its entries to `cache.MaxBytes`, 1 GiB, those unused longest going first, though they can pass it by an eighth between prunes. It is safe to share between processes, but not with anyone you would not let replace the binary: an entry is trusted as it is. |
| `--triage <file>` | both | Rate or accept findings by the rules this JSON file lists ([Triage](#triage)). A file that cannot be read, or holds a rule no run can apply, is refused with exit `2` before anything compiles. |
| `--diagnostics-format text\|json` | both | Write the findings to stderr as text (the default), or as one [JSON report](#json-report). |
| `--min-severity error\|warning\|info` | both | In text, write each finding rated at or above this severity, and count the rest (default `info`). Refused beside `--diagnostics-format json`. |
| `--diagnostics-per-code-limit <n>` | both | In text, write the first `<n>` findings of each code and severity, and count the rest (default `3`); `0` writes every one. Refused beside `--diagnostics-format json`. |
| `-o`, `--output <file>` | `compile` | Write IR JSON to `<file>` instead of stdout, compact rather than indented. |
| `--pretty` | `compile` | Indent the JSON `--output` writes; stdout is indented either way. |
| `--explain <json-pointer>` | `compile` | Instead of writing the document, report what compiling produced at this source coordinate: the type node the document holds there, the coordinates beneath it, each construct kept verbatim under `Unmodeled` at or beneath it with the node keeping it and its reason, and the diagnostics stamped at it, as their stages reported them. A pass may replace or remove a node the compiler made, which `--disable-pass` shows. `''` is the whole document. Refused together with `--output`, which it would otherwise ignore. |

## Diagnostics

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

A finding that concerns more than its own place, as a conflict between two declarations does, has
one line all the same, at the place it sits: its message names the declaration it kept, and the
list of what else it concerns, each with its role, is in the [JSON report](#json-report) and the
IR document ([provenance](ir/provenance.md#related-places)).

Each finding is written at the severity it is rated at, which is the one its stage reported unless a
[triage](#triage) rule rates it otherwise. A spec can draw thousands of findings, so the text format
writes the first three of each code and severity, in the order they were reported, and counts the
rest; `--diagnostics-per-code-limit 0` writes every one, and `--min-severity warning` or
`--min-severity error` counts the findings below that severity instead of writing them. After the
findings, each on a line indented so no reader of `<severity> <code>` lines takes it for a finding,
come a line for each code and severity some findings of which were not written, a line for each
triage rule that matched no finding, and, once anything was left unwritten, a tally:

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

## Triage

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
  and `--min-severity` then read. A finding a stage reported at a severity the IR does not declare
  still ends the run with exit `2`, as a stage breaking its contract, whatever a rule says of it.
- `accept: true` accepts the findings, which then fail the run at no `--fail-on`, `info` included,
  and are counted rather than written. A rule accepting findings names their place, so it accepts
  none its code reports elsewhere. It does accept every finding of its code at that place, one
  reported later included, and a place can hold a whole scope: a pass reports a collision between
  two of the document's types at `document`, so a rule accepting one there accepts the next
  (#1825).
- `pointer` places the rule at the findings this JSON Pointer locates in the spec, the file
  compiled, an overlay's writing into it included; `node` places it at the findings an IR pass
  located at that IR node, such as `service/openapi/0`. A rule names at most one place, and one
  naming a place outranks one naming only the code. A finding in a document a `$ref` reads (#1804),
  or located by a line alone, can be ruled by its code only.

A rule names no pattern: a `*` in a code is refused, as is a rule both rating and accepting, an
acceptance naming no place, two rules ruling the same findings, a pointer that is not RFC 6901, a
member the file does not define and a missing reason. A rule no finding matched is named after the
findings, as many of each code as of a code's findings, and listed as `unusedRules` in the JSON
report: unless a pass it was written for is off, the finding it names is gone, and the rule can go
too. A run that lowered no document stopped short of most findings, so it names no rule unused. A
file is read as a spec is, no further than 16 MiB.

A file accepting each finding a run now draws at a place can start from the run's own report:

```bash
morphic validate openapi.yaml --diagnostics-format json 2>&1 >/dev/null |
  jq '{rules: [.findings[].diagnostic
    | if .provenance.source == 0 and .provenance.pointer then {code, pointer: .provenance.pointer}
      elif .provenance.node then {code, node: .provenance.node} else empty end
    | . + {accept: true, reason: "accepted as found"}] | unique}' > triage.json
```

Write each `reason` yourself before relying on it: it is what the next reader has to go on. A rule
the recipe places at a scope, such as `document`, accepts every later finding of its code about that
scope too.

## JSON report

`--diagnostics-format json` writes one JSON object to stderr, then a newline, for a program to read;
the run's exit status and stdout are what text gives. It is written for every run that reaches its
verdict, a spec that lowered to no document included, and nothing else is written to stderr then,
not even a warning about the cache. A run that exits `2` before its verdict writes text instead, its
warnings and then `morphic: ...`. One stopped after its report, or whose document is then refused,
adds that line after the report, the report staying the first line, whatever status it exits with: a
refused document leaves a run its findings failed at `1`, as [Exit codes](#exit-codes) says.

```json
{"version": 2, "failed": false,
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
| `version` | the report's schema version, `2` |
| `failed` | the verdict, the one the exit status reads |
| `sources` | the files each `provenance.source` indexes, as an IR document's `sources` |
| `findings` | every finding, in the order reported: `diagnostic` as its stage reported it, an IR diagnostic, its `related` list naming what else the finding concerns ([provenance](ir/provenance.md#related-places)), `severity` as it is rated, `accepted` when a rule accepted it, and `rule`, the rule it met |
| `unusedRules` | the triage rules no finding matched |
| `warnings` | what kept the run from working as asked without failing it, such as a cache it could not use, one line each |

Each list is present, empty when there is nothing in it. A change to any member's shape, a
diagnostic's included, moves `version`, which `TestSchemaVersion_PinsTheEncodedShape` in
`engine/report` holds to the encoded shape, so a consumer that checks it knows every member it
will meet. A string is escaped as JSON escapes it, unlike the text format: a C1 control or a
bidirectional override a spec writes stays raw, as it does in the IR document (#1826).

## Exit codes

Both commands share them.

| Code | Meaning |
|---|---|
| `0` | Clean, or a help request. |
| `1` | The spec has problems: a finding no triage rule accepted reached the `--fail-on` threshold, or the spec could not be lowered at all (an undecodable file, an unrecognized or unsupported format, a version no compiler claims). |
| `2` | The invocation or the filesystem was wrong, the run was stopped, or something inside morphic failed: a bad flag or argument, a spec or triage file that could not be read, an output that could not be written, a SIGINT, SIGTERM or SIGHUP that stopped the run, or a compiler or pass that broke its contract, which `--verify` also holds to the IR's invariants. Nothing in the spec's own contents leads to `2`. |

One run can earn both `1` and `2`: a spec that reached the threshold, whose `-o` destination then
refused the write. The verdict on the spec wins, so `1` always means the spec has problems, and the
write error is printed on stderr either way.

## Writing a file

`-o` replaces a file by renaming a temporary file over it, so a reader of the file sees the whole
previous document or the whole new one, never part of either. The file's directory must therefore
take a new entry: a writable file in a read-only directory is refused, as writing it in place would
give up that guarantee. Other hard links keep the old content. A FIFO or a character device, such
as `/dev/null` or the pipe a shell's `>(…)` names, has no content to replace and is written as
stdout is; a FIFO no process reads is refused rather than waited for. A directory, a socket or a
block device is refused. A symlink is judged by what it names, and one naming a file, or nothing, is
replaced, not followed. So `-o /dev/stdout`, with stdout redirected to a file, needs `/dev` to take
a temporary file: it is refused, or, run as root, replaces the link (#1864).

## Signals

SIGINT, SIGTERM or SIGHUP stops a run, which exits `2` whatever it had found, naming the signal on
stderr; a shell script interrupted with Ctrl-C goes on to its next command unless it checks that
status, as `set -e` does. A stopped run writes no document: a file `-o` names keeps its previous
content, its temporary file removed, and stdout gets none of it. Stopped during the compile, it
writes nothing at all; stopped during `compile`'s diagnostics, it finishes them, and no document or
`--explain` report follows. A signal that comes once the run's last output has begun is too late to
stop it: once the document is being written to stdout or to the FIFO or device `-o` names, or moved
into place over `-o`'s file, once an `--explain` report has begun, or once the compile is over for a
run that writes nothing after its diagnostics, `validate` or `compile` of a spec that lowered to no
document. The run then finishes and exits as though the signal had not been sent, so it never
reports a stop for a document it wrote. Another signal while the run is stopping changes nothing, so
a copy that a shell or a wrapper delivers along with the first cannot cut the cleanup short, and a
signal that comes as morphic exits does not end it by that signal either. Stopping can take a few
seconds on a large spec; SIGQUIT (`Ctrl-\`), which prints every goroutine's stack, or SIGKILL ends
morphic sooner, and either can leave the temporary file behind: a dot, the destination's name, then
digits. A SIGINT or SIGHUP morphic was started ignoring stays ignored, as a script's shell starts a
background job ignoring SIGINT and `nohup` a command ignoring SIGHUP.

## Compiler options

`--compiler-option` names an option in the vocabulary of whichever compiler recognizes the spec.
Morphic itself knows none of them, and the compiler refuses a name it does not know rather than
ignoring it.

```bash
morphic compile openapi.yaml --compiler-option grouping=path-prefix --compiler-option overlay=patch.yaml
```

The OpenAPI compiler accepts the options below. Each option that is `true` by default switches a
policy, and [`policies`](compilers/openapi.md#policies) states how every policy is switched, what it
marks, what it yields to, and what it reports.

### Grouping

| Option | Values | Meaning |
|---|---|---|
| `grouping` | `tags` (default), `path-prefix`, `extension` | How operations are grouped into operation groups: by tag, by their path's first segment, or by the group path a vendor extension states, a group nested for each segment. |
| `grouping-key` | an `x-` extension name | With `grouping=extension`, the extension stating an operation's group path, named together with `grouping-lists`. Naming neither reads `x-fern-sdk-group-name` with `segments`, and its aliases in `x-forge-aliases`. |
| `grouping-lists` | `segments`, `groups` | What a list under `grouping-key` means: the segments of one path, or several paths, the operation filed under the first and a group made for each other. A string is one path, its segments separated by dots. |
| `grouping-aliases` | an `x-` extension name | With `grouping=extension`, the extension listing an operation's aliases, each an object stating a further group path under `grouping-key`, for which a group is made. Naming neither `grouping-key` nor `grouping-lists` reads `x-forge-aliases`, and naming them reads none unless this names one. An embedded-policy mapping mounting operations in those groups must read the same extension ([`extension-groups`](compilers/openapi.md#extension-groups)). |
| `grouping-fallback` | `tags` (default), `path-prefix` | With `grouping=extension`, how an operation whose extension states no path is grouped. |

### References and overlays

| Option | Values | Meaning |
|---|---|---|
| `allow-external-refs` | `true`, `false` (default) | Let `$ref` resolution read documents outside the spec's directory: files anywhere (by path or local `file:` URI) and http(s) URLs. Off, only a relative `$ref` into another document is read, and only a regular file inside the spec's directory tree, symbolic links included; any other, an absolute path or `file:` URI among them, is reported unresolved, whatever another `$ref` names the same file by. Either way, a reference in a document read over http(s) reads only http(s) URLs. |
| `locate` | `<uri-prefix>=<location>`, pairs separated by commas | Read the documents a URI prefix names from a location: a directory, relative to the spec's, or an http(s) URL ending in `/`. The prefix is an absolute URI ending in `/`, with no query or fragment, each written once. A `$ref` to `<uri-prefix>rest` reads `<location>rest`, segment by segment, never above the location, and with `allow-external-refs` off only inside the spec's directory; a segment decoding to a separator is refused. Nothing is mapped unless written here. |
| `overlay` | a file path | Apply an [OpenAPI Overlay](https://spec.openapis.org/overlay/latest.html) 1.0 or 1.1 document to the source before lowering. It is read as the spec is: a regular file, within `max-source-bytes`. |
| `overlay-lax` | `true`, `false` (default) | Report an overlay action whose selector matches nothing as a warning, and apply the rest, rather than refuse the compile. |

### Inference and naming

| Option | Values | Meaning |
|---|---|---|
| `nullable-inference` | `true` (default), `false` | Read `nullable: true` as admitting null wherever a schema writes it, and keywords beside a `$ref` that admit null as a nullable reference, marking each reference only that reading makes nullable as inferred. `false` reads them only as the document's OpenAPI version does. |
| `discriminator-inference` | `true` (default), `false` | Read a union's tags off the strings each variant pins its tag property to: as the mapping of a `discriminator` declaring none, where OpenAPI's implicit mapping would route a variant by a component name it rejects, or not by every string it admits, and as a discriminator for a closed `oneOf` or `anyOf` declaring none, where every variant requires one property and pins it to strings no other variant admits; and read a subtype's tag off the strings it admits, where they rule out the component name its discriminator routes it by. What it reads is marked as inferred. `false` keeps only what the document declares. |
| `kind-inference` | `true` (default), `false` | Lower a schema that names no kind as one writing the `type` its keywords giving a member, an element or a value a type speak of: an object for a `properties` or `patternProperties` naming a member or an `additionalProperties` holding a schema other than `true`, `{}` and `false`, an array for an `items` holding such a schema or a `prefixItems` naming a position, and a union of both where it writes keywords of each, marking each node as inferred. A `required`, a `discriminator`, a closure and a catch-all of `true` or `{}` infer nothing. `false` lowers such a schema to the top type, its shape keywords kept verbatim. |
| `pagination-inference` | `true` (default), `false` | Read a `GET`'s or a `QUERY`'s pagination off the names it uses: a `page`, `page_number`, `offset`, `cursor`, `page_token`, `next_token` or `continuation_token` query parameter for where a walk continues and a `per_page`, `page_size` or `limit` one for the page size, and in each success response's body the list of items and, for a cursor or a token, the member handing back the next one. What it reads is marked as inferred, and an operation naming a walk it cannot settle is reported and left with none. An operation whose extensions, read under `extension-promotion`, speak of a pagination, filling one, failing to or stating it has none, is left to them. `false` keeps only what the document declares. |
| `idempotency-inference` | `true` (default), `false` | Read an operation's header parameter `Idempotency-Key`, in any letter case, as its idempotency token, where it states no idempotency and is sent by `POST`, `PUT`, `PATCH` or `DELETE`. What it reads is marked as inferred, and an operation it cannot settle, sent by a method only `additionalOperations` declares or carrying the header twice, is reported and left with none. `false` keeps only what the document declares. |
| `secret-inference` | `true` (default), `false` | Redact a parameter, header or property whose name ends in a credential's, such as `password`, `client_secret`, `api_key` or `access_token`, and whose value is text or bytes, where the document asks nothing about its redaction. Each use it redacts is marked as inferred. `false` redacts only what the document asks for. |
| `http-date-inference` | `true` (default), `false` | Write and read a `date-time` response header whose field an RFC defines as an HTTP-date, such as `Last-Modified` or `Expires`, as an HTTP-date, marking it as inferred and reporting it. `false` keeps the text its format names. |
| `replacement-inference` | `true` (default), `false` | Read a deprecation's replacement out of its message: the method and path of an operation the document declares, for an operation, and a sibling named in a code span, for a parameter or a property. What it reads is marked as inferred, and a message naming one beside a name that names nothing, or several, is reported and fills none. `false` leaves the replacement in the message alone. |
| `title-names` | `true` (default), `false` | Name an anonymous schema that writes a `title`, a union branch included, by the title's words, in place of the name its position composes (`variant_0`, its property's key, its owner's). `false` names every such schema by its position. The title is the schema's summary either way. |

### Streams and files

| Option | Values | Meaning |
|---|---|---|
| `streaming-media` | media types and `+suffix`es, separated by commas | The media types read as a stream of frames where a document declares no `itemSchema`, in place of the default list: `application/json-seq`, `application/jsonl`, `application/x-ndjson`, `text/event-stream` and `+json-seq`. An entry starting with `+` matches every media type with that structured suffix. |
| `streaming-inference` | `true` (default), `false` | Read a media type as a stream of frames, as `streaming-media` lists. `false` turns that reading off, whatever the list holds, leaving only what an `itemSchema` declares. |
| `binary-media` | media types, `type/*` wildcards and `+suffix`es, separated by commas | The media types under which a body or multipart part whose schema says no more than that it is a string is a file, in place of the default list: `application/gzip`, `application/pdf`, `application/vnd.tcpdump.pcap`, `application/wasm`, `application/zip`, `audio/*`, `font/*`, `image/*` and `video/*`. A `type/*` entry matches no media type with a structured suffix, so `image/*` leaves out `image/svg+xml`. Each file it reads is marked as inferred. `application/octet-stream` needs no entry: a body or part it carries is a file whatever the list holds. |
| `binary-inference` | `true` (default), `false` | Read a body or part as a file by its media type, as `binary-media` lists. `false` turns that reading off, leaving only the files a schema or `application/octet-stream` declares. |

### Extension promotion

| Option | Values | Meaning |
|---|---|---|
| `extension-promotion` | `true` (default), `false` | Read the vendor extensions the promotion mapping names into the typed fields they fill, marking each node read as inferred. `false` keeps every extension verbatim and reads none. |
| `promotion-targets` | a JSON object | The promotion mapping, in place of the default: each target to its sources in order of precedence, each source a `key`, the `member` of its object value to read, and the `values` that state the target's fact, as `openapi.ExtensionPromotions.Targets` encodes as, such as `{"deprecation.message": [{"key": "x-why"}]}`. A target the compiler does not read is refused. |
| `promotion-prefixes` | extension name prefixes, separated by commas | The prefixes the promotion policy owns: an extension under one that nothing reads, by the promotion mapping or by grouping, its aliases key included, is reported, naming the key read nearest it. |

### Budgets

| Option | Values | Meaning |
|---|---|---|
| `max-source-bytes` | an integer | The byte budget for one document: the spec, the overlay, and each document a `$ref` reads. None is read more than a byte past it. |
| `max-source-nodes` | an integer | The budget on the YAML nodes one document parses to. |
| `max-enum-members` | an integer | The budget on the members of one enum. |
| `max-alias-surplus` | an integer | The budget on the nodes YAML aliases may add to a document. |
| `max-external-documents` | an integer | The budget on how many documents external references read in one compile. |
| `max-reference-expansion` | an integer | The budget on what `$ref`s copy: each `$ref` to a declaration other than a schema lowers its own copy of it, and of each component alias on the way, weighed as a node for each YAML node, a YAML alias as a copy of what it names, a byte for each byte of scalar text, and a node for a schema it holds, which lowers once; and on what each operation a path item's `additionalOperations` declares copies of what the item declares. |

Each `max-*` option sets the `openapi.Limits` budget it names, whose default is documented there:
`0` takes the default, and a negative value turns the budget off.
