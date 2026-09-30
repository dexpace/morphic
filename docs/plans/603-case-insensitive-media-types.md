# Plan — #603: media types are classified case-sensitively

One logical change: route the compiler's two remaining raw media-type classifications through the one normalization it already has, so every classification is case-insensitive and parameter-insensitive while the IR keeps the declared spelling.

## Behaviour and cases

**Now**
- `Multipart/Form-Data` gets no `Encoding`: `lowerContent`'s switch (`content.go:75-86`) takes neither arm, because `isFormContent` compares `strings.HasPrefix(mt, "multipart/")` against the raw key.
- `Application/Octet-Stream: {}` (no schema) lowers to `t/prim/any` with no `FileInfo`, because `isBinaryBody` compares `mt == "application/octet-stream"` raw.

**After**
- Classification reads the normalized media type — lowercased, `;`-parameters dropped, surrounding space trimmed — exactly what `normalizeMediaType` (`lowering/streaming.go:75-80`) already does for the streaming policy. RFC 6838 §4.2: type and subtype are case-insensitive.
- `Multipart/Form-Data` (any case, any parameters) keeps its per-part `Encoding`, keyed by the part `PropID`s the same way the lowercase spelling does.
- `Application/Octet-Stream: {}` (any case, any parameters) is a file body: `File != nil`, `IsText false`, `Type.Target == t/prim/bytes`.
- **Declared spelling is never normalized into the IR.** `Content.MediaType` is assigned `mt` at `content.go:67`, before any classification; `FileInfo.ContentTypes` is `[]string{mt}` at `:77`; `Encoding.MediaType` (`:462`), `HTTPParamBinding.ContentType` (`params.go:134`) and `RequestContentTypes` (`:777-786`) are copies of declared keys; `mediaPtr` (`:64`) keeps the declared key, so no IR identity moves. Normalization is called *inside* the two predicates only — asserted by the tests, not by reading.
- Unchanged: lowercase spellings; the binary-arm-before-form-arm order; `multipart/*` beyond `form-data` still counting as form content; `operation/streaming.go:127` (the policy already normalizes; no edit).
- Edge: a media type with parameters (`Multipart/Form-Data; charset=utf-8`) classifies by the type alone — the reason a bare `strings.ToLower` is wrong and the shared helper (which strips `;…`) is right.

## Where the shared normalizer lives

`compilers/openapi/internal/lowering` — the package both walks already reach and neither may reach the other (its own package doc says so). `internal/operation` already imports it (`content.go:30`), and `lowering` may import neither `operation` nor `schema` (`internal/archtest/arch_test.go:144-149`), so the promoted helper adds no import edge and no `rules` entry. Mirroring a private copy in `operation` was rejected: two spellings of one rule is the drift this defect is made of.

Sweep (to be re-run by the builder, not trusted from reading): the only raw media-type comparisons in non-test Go are `content.go:815` and `:823` plus `lowering/streaming.go`; `content.go:428` and `params.go:113` are `EqualFold`s on HTTP *header names*, and `schemaIsFilePart`'s `format` compare (`content.go:835-841`) is a schema format, not a media type — all deliberately untouched.

## Seams

- `compilers/openapi/internal/lowering/streaming.go:73-80` — `normalizeMediaType` moves out, exported.
- `compilers/openapi/internal/lowering/mediatype.go` — **new**: `NormalizeMediaType`, GoDoc stating the rule and that it is never what the IR records.
- `compilers/openapi/internal/lowering/streaming.go:50` and `:66` — call sites renamed only.
- `compilers/openapi/internal/lowering/lowering.go:1-12` — package doc gains one sentence naming the shared normalization (the current doc claims only the substrate; leaving it would be a false statement).
- `compilers/openapi/internal/operation/content.go:812-816` (`isFormContent`), `:818-826` (`isBinaryBody`) — classify the normalized string; call sites `:76`, `:79` unchanged in shape.
- `compilers/openapi/internal/operation/content_test.go` — two new tests beside the lowercase controls at `:40` and `:76`, using `lowerServiceSpec` + `openapitest.FirstOp`.
- `compilers/openapi/internal/lowering/mediatype_test.go` — **new**: table test for the exported symbol's contract.
- Not touched: `internal/operation/params.go`, `meta.go`, `internal/schema/accumulate.go`, `internal/auth/auth.go` (in flight elsewhere); no IR type, no diagnostic, no fixture.

## Ordered steps

1. **Reproduce first.** Add the mixed-case pair — `TestContent_MixedCaseFormContentKeepsEncoding` (`Multipart/Form-Data` with `encoding`, asserting `Encoding` keyed by the part `PropID` and `Content.MediaType` equal to the declared spelling) and `TestContent_MixedCaseOctetStreamBodyIsFile` (`Application/Octet-Stream: {}`, asserting non-nil `File`, `ContentTypes == []string{"Application/Octet-Stream"}`, `t/prim/bytes`) — with the parameter spelling folded into their tables. Deliverable: `go test ./compilers/openapi/internal/operation/ -run TestContent_MixedCase` red for the documented reasons (no encoding key; no `File`), read in the failure text. If they pass, stop and re-check reach before proceeding.
2. **Promote the helper.** Move `streaming.go:73-80` to `mediatype.go`, export it as `NormalizeMediaType`, refresh the GoDoc (one rule for every media-type classification; callers keep the declared spelling), rename `streaming.go:50`/`:66`, extend the package doc. Deliverable: `go test ./compilers/openapi/internal/lowering/` green, including `streaming_test.go:34-36` (case, parameters, space).
3. **Pin the contract.** Add `mediatype_test.go`, flat and table-driven over the spellings the rule collapses. Deliverable: same package green, statements covered.
4. **Fix the two predicates** in `content.go:812-826`; leave `:67`, `:77`, `:64` alone. Deliverable: `go test ./compilers/openapi/internal/operation/ -run TestContent` green — the new pair *and* the lowercase controls (`TestContent_MultipartPartEncoding` `:40`, `TestContent_BinaryOctetStreamBody` `:76`, `TestContent_MultipartRefBodyKeepsEncoding` `:124`, `TestContent_OctetAndErrorMulti` `:485` — the real `{}`-control).
5. **Break what the tests catch.** Restore the raw comparisons and watch both new tests fail; then a `strings.ToLower`-only variant and watch the parameter spelling fail. Restore the fix. Deliverable: observed reds, nothing committed.
6. **Re-sweep the mechanism.** Run the grep named in "Where the shared normalizer lives" over non-test `compilers/`. Deliverable: exactly the two fixed sites. Anything else → halt and report rather than widen the PR.
7. **Fixture check.** `go test ./compilers/openapi/ -run 'TestConformance|TestGolden'` then `git status --short testdata/`. Deliverable: empty — no committed golden changes, because no fixture spells a media type in mixed case or with parameters. No new corpus case: it would drag a new `.golden.json` and matrix bookkeeping into a one-defect PR.
8. **Full gate.** `make gate` (fmt, vet, lint, nolint, build, coverage-count, coverage at exactly 100%, fuzz, bench-smoke). Deliverable: pass; fix every reported error before re-running.
9. **Ship.** Branch `fix/case-insensitive-media-types`; commit subject `fix(compilers/openapi): classify media types case-insensitively` (imperative, no period); measure `printf '%s (#603)' "<title>" | wc -c` ≤72; push; `gh pr create --assignee fuad-daoud --reviewer OmarAlJarrah` with Summary / Test plan. Deliverable: PR URL, title length printed.

## Deleted

1. `normalizeMediaType` (unexported, `streaming.go:73-80`) — replaced by the exported `NormalizeMediaType` in the same package, which keeps its exact behavior; its two streaming callers are renamed.
2. The raw form comparison `strings.HasPrefix(mt, "multipart/") || mt == "application/x-www-form-urlencoded"` (`content.go:815`) — replaced by the same test on the normalized spelling.
3. The raw binary comparison `mt == "application/octet-stream"` (`content.go:823`) — replaced by the same test on the normalized spelling.

Nothing else is deleted: no IR field, diagnostic, option, policy, test, fixture or capability goes away; the change only admits spellings RFC 6838 already treats as identical.

## The report must include

- The diff by file and line: helper home, the two predicates, the declaration sites left untouched.
- Red-before / green-after output for the mixed-case pair, plus the lowercase controls' names and their green run.
- The mutation evidence (raw comparison restored → both red; `ToLower`-only → parameter case red), stated per planted site.
- The sweep command and its output showing the two sites are the mechanism's whole extent.
- `git status --short testdata/` empty after the conformance/golden run, with the command.
- `make gate` verdict, and the PR URL with the measured title length.
