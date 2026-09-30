# Plan — #641: warn when a Paths Object key is not a valid path

Seed: `plan-641/001-prompt.md`. Verified against the throwaway tree at `07576ab` (`plan-641-001`). Push branch: `fix/malformed-path-key-warning`. PR title: `fix(compilers/openapi): warn on a malformed path key` — 59 chars including the ` (#641)` GitHub appends (cap 72); body Summary / Test plan; `gh pr create --assignee fuad-daoud --reviewer OmarAlJarrah`.

## Behaviour

**Today** (reproduced: `go run ./cmd/morphic compile` on the issue's spec): `"a/b c?x"` lowers to `uriTemplate: "a/b c?x"` (`compilers/openapi/internal/operation/operations.go:220`), stderr empty, exit 0. Same for `widgets` (no slash), `/a b`, `/a?x=1`, `/a/%zz`.

**After**: every lowering is unchanged — the key still reaches the IR verbatim, field for field (`uriTemplate` from `operations.go:220`; the no-operationId name hint `METHOD <key>` from `operationName`, `operations.go:480`; path-prefix grouping from `firstPathSegment`, `operations.go:1283`; `OpID`, pointers, `SharedRoute`, groups untouched). In addition the compiler emits **one warning per malformed Paths Object key**:

- code `openapi/invalid-path-key` (new `diag.InvalidPathKey`, in the `InvalidStatusKey` / `InvalidMethodKey` / `InvalidLocationKeyword` family);
- severity `warning` — the key lowers, so an error would be a refusal this compiler does not perform, and `harness.Check` stops at the first error diagnostic, hiding later findings (the `invalidStatusKeyDiag` rationale at `operations.go:1230-1241`);
- pointer `/paths/<key>` via `ids.Ptr("paths", key)` (RFC 6901-escaped, so `a/b c?x` → `/paths/a~1b c?x`), through `c.DiagAt` so provenance stays single-sourced (`lowering.go:312`);
- message names the key with `%q` and each defect found, and closes by stating the key is not rewritten. One diagnostic per key, not per defect.

### What is malformed

1. **Missing leading `/`** (empty key included). Paths Object Patterned Fields: "The field name MUST begin with a forward slash (`/`)" — 3.0.3 §4.7.8.1, 3.1.0 §4.8.8.1, 3.2.0 §4.8.1. The same sentence in all three: **not version-gated**; do not read `c.Source.Format`.
2. **Contains `?`** — it carries a query string: RFC 3986 §3.4 keeps the query out of the path, and OpenAPI declares query data as Parameter Objects with `in: query` ("Parameters that are appended to the URL", 3.0.3 §4.7.12.1; 3.1.0 Parameter Object; 3.2.0 §4.12).
3. **Contains a character that is not valid in a URI path** — RFC 3986 §3.3 (`pchar` = unreserved / pct-encoded / sub-delims / `:` / `@`) plus `/`, plus `{}` for OpenAPI path templating (3.x "Path Templating"). Allowed: `A-Z a-z 0-9 - . _ ~`, `! $ & ' ( ) * + , ; =`, `: @ / { }`, and `%` only as the start of a pct-encoded triplet (`%` + two hex digits). Any non-ASCII rune is allowed: the IR field is an RFC 6570 template (`ir/bindings.go:52`) whose §2.1 literals admit `ucschar`/`iprivate` and pct-encode them on expansion. Reported: space, tab/control/DEL, `"`, `<`, `>`, `\`, `^`, `` ` ``, `|`, `[`, `]`, and a `%` that begins no triplet.
4. **`#` is deliberately deferred to #602**, which will strip the fragment, set `SharedRoute`, regroup and report it. A warning from this code saying the key is lowered as written would be false the moment #602 lands; reporting the same key twice at one pointer serves nobody. The silence is pinned by a test citing #602.

### Exempt, and left alone

- **Webhook keys** (`lowerWebhooks`, `operations.go:244-289`): a webhook key is a name, not a path (`testdata/conformance/openapi/webhooks.yaml` lowers `newPet` to `uriTemplate: "newPet"`).
- **Callback keys**: runtime expressions (`{$request.body#/url}`), not paths.
- **Paths Object `x-*` extensions**: not path items; carried by `schema.ExtensionsIn` at `operations.go:143` and not yielded by `paths.All()` (verified on `testdata/conformance/openapi/extensions-x.yaml`).

### Cases pinned by tests

| Key | Outcome |
| --- | --- |
| `a/b c?x` | warn: missing `/`, `' '`, query; `uriTemplate` kept verbatim |
| `widgets`, `""` | warn: missing `/` |
| `/a b`, `/a` + control, `/a[b` | warn: invalid character |
| `/a?x=1` | warn: query |
| `/a/%zz`, `/a/%`, `/a/%2` | warn: `%` begins no triplet |
| `/a/b`, `/`, `/a/{id}`, `/a/%20`, `/a/~b`, `/a/é` | silent |
| `/a#frag` | silent here (#602) |
| webhook `newPet` | silent |

Version coverage: the malformed key warns under 3.0.3, 3.1.0 and 3.2.0 (`openapitest.PathsSpecVer`, `spec.go:39`).

## Seams

- `compilers/openapi/internal/diag/diag.go` — new exported const `InvalidPathKey = "openapi/invalid-path-key"` after `InvalidLocationKeyword` (line 377), doc comment in house style: rule + citations, why warning, why the key still lowers, why no fragment here (#602).
- `compilers/openapi/internal/diag/diag_test.go` — add `diag.InvalidPathKey` to `codes()` (lines 131-152); `TestCodes_MatchTheDeclaredSet` reddens otherwise.
- `compilers/openapi/internal/operation/pathkeys.go` (new, package `operation`) — `pathKeyDiags(c lowering.Ctx, key string) []ir.Diagnostic` (0 or 1) plus its unexported classifier.
- `compilers/openapi/internal/operation/operations.go` — in `lowerPaths` (186-203), inside the loop **after** the `pi == nil` guard (197-199) and **before** the `lowerPathItem` call (200): append `pathKeyDiags(c, path)...`. Every key that resolves to a path item is checked once, in source order; a key whose item does not resolve is already an `UnresolvedRef` error and claims nothing.
- `compilers/openapi/internal/operation/pathkeys_internal_test.go` (new, package `operation`) — classifier table.
- `compilers/openapi/internal/operation/operations_test.go` (package `operation_test`) — lowering-level tables, next to the status-key pair (lines 140-201); use `openapitest.DiagMessageAt` (single match at the pointer), `HasDiag`, and `op.Bindings.HTTP[0].URITemplate`.
- Not touched: `internal/operation/params.go` (read only, for the #408 style), `internal/auth/auth.go`, `meta.go`, `internal/schema/accumulate.go`, `lowerWebhooks`, and every golden.

## Steps

1. **Add the code to the vocabulary** — const in `diag.go` (line 377) + entry in `codes()`; worked when `go test ./compilers/openapi/internal/diag -count=1` passes both `TestCodes_*`.
2. **Add the classifier and wire it** — `pathkeys.go` with `pathKeyDiags`; call in `lowerPaths` after the guard; worked when `go build ./...` and `go vet ./compilers/openapi/internal/operation` are clean.
3. **Pin the classifier** — table in `pathkeys_internal_test.go` covering every allowed and rejected class, `%20`/`%2`/`%zz`/trailing `%`, non-ASCII, `?`, `#`, empty key (coverage is an exact-100% gate, so every branch needs a row); worked when `go test ./compilers/openapi/internal/operation -run TestPathKey -count=1` is green.
4. **Pin the lowering** — malformed table (one warning at `/paths/<escaped>`, message names the key and defect, `URITemplate` verbatim), overreach table, three-version table, fragment-silence pin, webhook-silence pin; worked when `go test ./compilers/openapi/... -count=1` is green with no golden diff.
5. **Full gate** — `make gate`; worked when it exits 0 and `git status --porcelain -- '*.golden.json'` is empty. If a golden moves the rule overreached (webhook/extension keys, a valid path class): fix the rule, never `-update`.
6. **Open the PR** — `git branch -m fix/malformed-path-key-warning`, conventional commit, `gh pr create --assignee fuad-daoud --reviewer OmarAlJarrah`; worked when `gh pr view` shows the measured title and both flags.

Focused iteration command: `go test ./compilers/openapi/internal/operation -run TestPathKey -count=1` (then `go test ./compilers/openapi/internal/diag -count=1`); full check: `make gate`.

## Deleted behaviour

1. Nothing is deleted. This adds one diagnostic: no lowering, helper, fixture, golden or test is removed, and no existing diagnostic changes code, severity, message or pointer.

## The report must include

- The diagnostic verbatim (code, severity, pointer, message) and the emitted artifact beside it (`uriTemplate` unchanged), backed by the targeted test output.
- Test names and what each pins (classifier table, malformed table, overreach table, version table, fragment silence, webhook silence).
- The explicit decisions: `#` deferred to #602 and why; webhook/callback keys exempt; not version-gated, with the three spec citations.
- No golden regenerated — no `-update` run; evidence: the corpus scan (no committed `paths` key is malformed) and empty `git status` for `*.golden.json`.
- The focused command output and the `make gate` result.
- The PR: branch, measured title, body, assignee/reviewer; confirmation that the four in-flight files were not touched.
