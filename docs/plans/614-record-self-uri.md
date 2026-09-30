# Plan — #614: an OpenAPI 3.2 `$self` URI is recorded on its source

## What the change does

Compiling a document that declares `$self` records the URI on that input file's `ir.SourceInfo`, so it appears in the document's `sources` entry beside `format`, `path` and `hash` and survives the JSON round-trip. Nothing else moves: no new diagnostic, no lowering change, no reference resolution.

Forward references below: D1–D6 are the recorded decisions, S1–S7 the seams, and steps 1–9 the ordered work.

## Cases

1. 3.2 with `$self` → `sources[0].selfURI` is the declared value; the document's JSON carries the key in that source entry.
2. 3.2 without `$self`, and 3.0 without → no key, empty field.
3. `$self: ""` → the same state as an unset `$self` (D3 pins it with its own row).
4. A 3.1 (or 3.0) document that writes `$self` → recorded as declared. The library parses the keyword at every 3.x minor and reports no finding for it (probe below), so a version gate would silently drop a declared value — the defect class this closes.
5. Malformed `$self` (`$self: "ht!tp://invalid-scheme"`) → unchanged today and after: `openapi/validation/validation-invalid-format` is already an error diagnostic at the value's position (`2:8` in the probe). No case is added for it.
6. Overlay sources unchanged: `overlay.sourceInfo` records an OpenAPI Overlay document, which declares no `$self`.
7. Unchanged and deliberately out of scope: resolving relative references through `$self` (#74 owns the wiring; the library already uses it as a base URI internally), and any diagnostic for a `$self` written by a dialect that does not define it — stated in the field's GoDoc and in the PR body, since the analogous location check for `allowReserved` is #628 and not this change.

## Decisions (recorded, with the reason)

- **D1 — Home: `ir.SourceInfo`, not `ir.Document`.** `$self` is a fact about one input file; #74 loads several, each may declare its own, and a document-level field could not say which file declared it. The issue points the same way.
- **D2 — Name: `SelfURI` / `json:"selfURI"`.** The IR names the concept, not the keyword (`Server.URLTemplate`, `HTTPBinding.URITemplate`, `AuthScheme.OAuth2MetadataURL`); the key must read on its own in JSON, where no GoDoc travels with it; and a neutral name leaves room for a later format's document self-URI without a schema change (invariant 9).
- **D3 — `omitempty` on a plain `string`: an empty `$self` and an unset one are one state.** Nothing downstream can act on the difference — an empty URI reference is not a base URI (RFC 3986 §5.1), it identifies nothing when documents are combined, and the library's own `GetDocumentBaseURI` returns `""` for both. CLAUDE.md's serialization rule reserves `omitempty` for strings whose empty and absent forms mean the same; this one qualifies. The library keeps them apart as `Self *string`; that is a fact about the parser, not a consumer-visible state.
- **D4 — No version gate.** The compiler records what the document declared, at any supported minor, for the reason in case 4. The field's GoDoc says so, and says reporting an out-of-dialect `$self` is a separate change.
- **D5 — `IRVersion` stays `0.6.0`: the change is additive only.** Full reasoning below.
- **D6 — Nothing else changes**: no `openapi.Options`, no `pass/`, `engine/`, CLI or `irverify` change; no diagnostic code is added.

### D5 in full — the check the seed asks for

CLAUDE.md is silent on IR changes, so the binding rules are `docs/ir-design.md` §2.1 (normative) and `ir/document.go`'s GoDoc log. §2.1 says: "Any change to the JSON shape of a `Document`: a key renamed or removed, an encoding changed, or the meaning of an existing key changed." A new optional member is none of those, and the history agrees: every bump the constant has taken was forced by a rename, a removal, an encoding change or a meaning change (0.2.0 rename, 0.3.0 rename, 0.4.0 `ErrorCase.Type` removed plus absence sentinels, 0.5.0 encodings, 0.6.0 the locator split), and the one pure field addition in the history — `Property.Constraints`, `ecd2e78` — landed without one. **Counter-reading, stated not hidden:** `ir/document.go` warns that "a shape change that reaches main without a bump leaves a consumer pinned to the old version accepting a document it cannot read", and a 0.6.0 build handed a document carrying `selfURI` does fail to decode it — loudly, on the unknown member, which is the failure mode §2.1's rationale calls the silent one it exists to prevent. This plan takes the §2.1 enumeration, which also keeps the seed's fixed title. If the batch owner rules the other way, the follow-up is mechanical and touches nothing else in this plan: bump the constant to `0.7.0` with a log paragraph, re-anchor `TestCompatibleVersion`'s three neighbour literals (`ir/document_test.go:89-91`), run the `-update` sweep (all 87 goldens rewrite their `irVersion` string), and title the PR `feat(ir)!: …` instead.

## Seams

- **S1** `ir/provenance.go:118-123` — `SourceInfo` gains `SelfURI string` with tag `json:"selfURI,omitempty"` after `Hash` (line 122), doc-commented per D2–D4.
- **S2** `compilers/openapi/internal/load/load.go:256-264` — the `ir.SourceInfo` literal inside `build` gains `SelfURI: doc.GetSelf()`. Library contract (go.mod:7, speakeasy-api/openapi v1.25.2): `OpenAPI.Self *string`, `GetSelf()` returning `""` for nil (`openapi/openapi.go:38-40, 88-93`).
- **S3** `ir/provenance_test.go` — new table, anchored on `TestProvenance_JSONEncodesEachLocatorUnderItsOwnKey` (line 83).
- **S4** `compilers/openapi/openapi_test.go` — new table, beside `TestParse_EndToEnd` (line 34, which already pins `doc.Sources[0].Hash` at line 49).
- **S5** `compilers/openapi/conformance_test.go` — a `conformanceCase` row in the document-metadata block (table ends line 246; document rows at 239-244) and one assert function beside `assertDocsSummaryDesc` (line 2995).
- **S6** `testdata/conformance/openapi/self-uri.yaml` + its `self-uri.golden.json`.
- **S7** `docs/ir-design.md` — §2 sketch (`SourceInfo` block after `type TagDef`, line 82) and §14's OpenAPI lowering row (`$self` → the source's `SelfURI`).

Evidence already gathered (re-runnable): `go run ./cmd/morphic compile <spec>` on a 3.2 document writing `$self` shows the URI nowhere in the IR and no diagnostic; the library probe shows `Self` nil for an absent key, `""` for `$self: ""`, the value for a set key, and the same value (no finding) for a 3.1 or 3.0 document that writes it; a malformed value already errors as `openapi/validation/validation-invalid-format`.

## Steps

1. **S1** — add the field and its GoDoc (D1–D4, including that an out-of-dialect `$self` gets no diagnostic here). *Check:* `go build ./ir`.
2. **S3** — `TestSourceInfo_JSONEncodesSelfURI`: table rows declared (byte-exact `{"format":…,"selfURI":"https://example.com/api/openapi.yaml"}`) and empty (key absent from the bytes), marshalled with `json.Deterministic(true)` per `ir/json.go:30-32`, each row also asserting a decode round-trip. *Check:* `go test ./ir -run TestSourceInfo`.
3. **S2** — add `SelfURI: doc.GetSelf()` to the literal. *Check:* `go build ./...`.
4. **S4** — `TestParse_RecordsSelfURI`: the five rows of cases 1–4 (3.2 with, 3.2 without, 3.2 with `$self: ""`, 3.1 with, 3.0 without), each compiled through `openapi.New().Compile`, asserting no error diagnostic, one source, and `Sources[0].SelfURI`. *Check:* `go test ./compilers/openapi -run TestParse_RecordsSelfURI`.
5. **S5, S6** — the corpus spec (3.2.0, `$self: https://example.com/api/openapi.yaml`, `info`, `paths: {}`), the row `{"self-uri", assertSelfURI, nil}`, and `assertSelfURI` asserting the source's format and `SelfURI`; its doc comment records why the golden is the byte-exact proof and why `rows` is empty (the matrix holds API capabilities; a document's self-assigned URI is source identity, so no row is invented). *Check:* `go test ./compilers/openapi -run TestConformance_TableNamesEveryCorpusSpec` fails until step 6.
6. **S6** — `go test ./compilers/openapi -run TestConformance -update`. *Check:* the diff adds `self-uri.golden.json` only; `unwitnessed.golden.txt` is unchanged (this case witnesses the new field) and no other golden moves; then `go test ./compilers/openapi`.
7. **S7** — add the `SourceInfo` block to §2 and the `$self` clause to §14's OpenAPI row. *Check:* read the §2 diff against the `Document` sketch.
8. **Planted mutations**, per the repo's "break what a test claims to catch": (a) delete the `SelfURI:` line from step 3 → steps 4 and 6 must redden; (b) delete `$self:` from the fixture, re-run step 6 with `-update`, and require the golden to lose the key — proof the fixture reaches the field rather than a broken `-update`; restore both, re-run step 6, and confirm the golden is byte-identical to the committed one. *Check:* the named tests, then a clean `git diff`.
9. **Gate** — `make gate`. *Check:* exit 0 and `scripts/check-coverage.sh` at exactly 100%; the recording line sits on every compile path and the field adds no statement.

## Deleted

Closed list — **nothing is deleted.** No key, field, rule, diagnostic or fixture is removed or renamed; the change adds one optional member on `ir.SourceInfo` and the line that fills it. Any diff hunk that removes a line is a mistake.

## Report must include

- changed paths, and every command run with its result;
- golden evidence: `git diff --stat` showing `self-uri.golden.json` added and no other golden touched;
- both planted-mutation results, naming the tests that reddened and confirming the restore;
- the measured title — `printf '%s (#614)' "fix(compilers/openapi): record \$self on the document's source" | wc -c` (68) — and the final title if it differs;
- the D5 statement carried in the PR Summary, so the reviewer reads the additive-only reading and can rule on it;
- confirmation the PR was opened with `gh pr create --assignee fuad-daoud --reviewer OmarAlJarrah`, body Summary / Test plan (no Breaking section, per D5), and that cases 4 and 7 are stated where the next reader reaches them.
