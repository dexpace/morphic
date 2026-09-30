# Task: fix irverify naming rules and message quoting — #398, #399, #400 message half

Repo `dexpace/morphic`, base `main` @ `07576ab`. Your worktree branch is `relevo/irverify-naming`; rename it to `fix/irverify-naming` before pushing. This is PR A of a two-PR batch; PR B owns `ir/irverify/values.go` and its tests, which are OFF LIMITS here. Read `CLAUDE.md` first — its invariants, the 100%-coverage gate and the "verify by executing" rules bind this work. The plan commits as `docs/plans/398-irverify-naming.md`.

## MasterMind decisions (binding — these override the planning note)

1. **#398 claim scope is type-registry Namings only** (`TypeCommon.Name` of each `doc.Types` entry). An Avro *field* alias is scoped to its own record, so a document-wide compare would false-positive; the reproducer is type-registry. Record the scope in the doc comment and the PR.
2. **#399 implements BOTH the blank and the repeat rule for `Namespace`** — the issue says "blank and repeat are both defects, same as an alias", and the owner confirmed it. This overrides the planning note that would have left the repeat rule out. `FieldPath` is still a path and keeps no repeat rule.
3. **#400 ships the message half only.** Do NOT change `Violation.Path` and do NOT claim to close #400 — a maintainer comment on #400 asks for the Path half too, and that needs `ir.WalkValues` plus the hand-built path sites changed. The PR body ends `Closes #398, #399` and `Refs #400`, and you open the Path follow-up issue (below) first and link it.

## Behaviour

### #398 — two type-registry Namings claim one alias

**Today.** `appendAliasViolations` (`ir/irverify/naming.go:149-178`) keeps `seen` per Naming; two models claiming one alias verify clean, and `TestVerify_AliasSharedByTwoNamings` (`naming_alias_test.go:260-281`) pins that.

**After.** A new walk check `checkAliasClaims(doc, declarations) ([]Violation, bool)` in `naming.go`, registered in `walkChecks()` (`irverify.go:82-97`).
- **Selection:** the visitor matches struct type `ir.TypeCommon`; the walk visits the embedded value at its owner's path (`doc.Types[t/x/A]`), and the check reads `.Name` there. It does not prune (Namings inside must still reach `checkNaming`).
- **Claims:** a non-empty `Source` at `<P>.Name.Source`, and each `Aliases[i]` at `<P>.Name.Aliases[i]` (built with `aliasPath`). A blank or non-UTF-8 entry is not a claim — it is already reported. `Canonical`/`Hint` never participate.
- **Matching:** exact string equality; the first claimant in walk order (sorted registry keys) stands.
- **Code `ir/naming-alias-shared`:** a later alias claimed by a different Naming is reported at that alias, naming the first claimant; a later Source matching an earlier alias of a different Naming is reported at the *alias*, naming the Source, once per alias entry; Source against Source is never reported.
- Same-Naming repeats stay with `-duplicate` and `-redundant` only.
- **Message:** `alias "X" is also claimed by <quoted other path>`.
- **Limit (doc comment + PR):** an alias equal to another type's namespace-qualified name (Namespace plus Source) is not caught unless Source holds the full name, because matching is exact equality.

### #399 — the string lists

**Today.** Only `Naming.Aliases` is held, by the `switch` at `naming.go:151-176`.

**After.**
- The per-list switch becomes one helper `appendListViolations(vs, list, path, codePrefix, rules)`; rule flags are *blank* and *repeat*, plus a `source` string for the redundant check. Aliases call it with prefix `ir/naming-alias` and all three rules; alias codes, messages (apart from the #400 quoting) and paths stay byte-identical. The helper's doc comment records one verdict per field.
- **Namespace:** `checkNaming`'s visitor gains owner selection through `namespaceOwners = {ir.TypeCommon, ir.Service}`, reads `FieldByName("Namespace")` and passes `path+".Namespace"`. Segment path `<owner>.Namespace[i]`, spelled like `aliasPath`, with a `namespaceField` const following the `nameField` precedent (`naming.go:24`). Call the helper with the blank AND repeat rules and prefix **`ir/namespace`**:
  - **`ir/namespace-blank`** — message `namespace segment is blank, so it names no package or module` (nothing quoted).
  - **`ir/namespace-duplicate`** — a repeat is reported at the later entry, naming the earlier index, message quoting the segment: `namespace segment "X" is listed here and at index N`.
- **Verdicts recorded in the helper's doc comment, not implemented:**
  - **Tags** (`Operation`, `TypeCommon`, `Channel`, `Message`, `Server`) and **Scopes** (`SchemeUse`): a source may legally repeat them, so deduplicating and diagnosing belongs to the compiler. OpenAPI passes both through verbatim — `Operation.Tags: src.GetTags()` (`compilers/openapi/internal/operation/operations.go:361`) and `SchemeUse{Scopes: scopes}` (`compilers/openapi/internal/auth/auth.go:528`) — so draft the follow-up issue below.
  - **ContentTypes, RequestContentTypes, Encodings:** ordered by priority; a repeat is redundant, not ambiguous. Undecided; needs its own issue.
  - **Server `Enum`:** `""` is a legal value for a server variable, so no blank rule; a repeat is the compiler's.
  - **Versions, Added/Removed:** may legally repeat (re-add cycles, `availability.go:7-8`).
  - **FieldPath:** a path; a repeated segment is legitimate (`a.b.a`). No rule.
- **What closes #399:** the helper, `Namespace` blank AND repeat, the recorded verdicts, and the Tags/Scopes follow-up.
- **What does not:** repeat rules for Tags/Scopes (compiler-side, follow-up) and any rule for content types or encodings (undecided).

### #400 message half — quote document-derived text

Every document-derived fragment in a `Violation.Message` goes through `strconv.Quote`, following `bigval.go:76,86` and `kinds.go:39,72`. Paths interpolated into messages are included (they embed map keys verbatim). `Violation.Path` itself is OUT OF SCOPE (decision 3).

| Site | Before | After |
|---|---|---|
| `naming.go:164` duplicate | `"alias " + alias + " is listed…"` | quote `alias` |
| `naming.go:170` redundant | `"alias " + alias + " is the entity's…"` | quote `alias` |
| `naming.go:227` cased | `channel + " " + name + …` | quote `name` |
| `naming.go:234` not-words | same | quote `name` |
| `naming.go:241` unsegmented | same | quote `name` |
| `naming.go:278-279` not-derived | `canon`, `source`, `(want)` | quote all three; `(want)` becomes `(Quote(want))` |
| new `-shared` / `namespace-*` | — | alias/segment and path quoted; `namespace-blank` quotes nothing |
| `duplicates.go:89` | `d.ID`, `at.path` | quote both |
| `ids.go:84-85` prim-id | `kind`, `id`, `want` | quote all three |
| `ids.go:87` (kind empty) | `id` | quote |
| `ids.go:105-106` reserved space | `id` (`kind` is a Go constant) | quote `id` |
| `ids.go:116` malformed | `id` (`kind` is a constant) | quote `id` |
| `ids.go:130-131` provenance | `id`, `idPath`, `prov.Pointer` | quote all three |
| `irverify.go:182` | `key`, `nodeID` | quote both |
| `refs.go:93` dangling | `s.id` (`reg.Label` is a constant) | quote `s.id` |
| `rawpayloads.go:94` | `string(reason)` | quote |
| `version.go:43` | `doc.IRVersion` | quote |

These stay as-is, with the reason:
- `ir/invalid-utf8` (`utf8.go:34`), alias blank, the alias ill-formed arm (`naming.go:153-160`), and `namespace-blank`: they decline to quote because their complaint is not the spelling.
- `declared.go:60`, `emptyrefs.go:227-231`, `rawpayloads.go:77/87/125-128`, `typerefs.go:37`, `unions.go:46`, `version.go:36`, `irverify.go:26/130/149/175`, `indices.go`, `provenance.go:36/73/81`: only Go constants or integers.
- `values.go:78/93/99`: already quoted; PR B owns the file, do not touch it.

Tests whose message assertions change: `naming_alias_test.go:156` → `alias "dup" is listed here and at index 0`; `naming_alias_test.go:170-171` → `alias "User" is the entity's own source name, so it matches nothing more`; `refs_test.go:256-265` (helper `refIDInMessage` parses the quoted token with `strconv.QuotedPrefix`/`strconv.Unquote`; `:251` stays `t/x/ghost`); `naming_test.go:390-403` (rewrite the "#400 out of scope" comment) and `:405-442` (assert every message `utf8.ValidString` and carries the `\xe9` escape; `wantTotal` stays 3). These `Contains` assertions keep passing: `duplicates_test.go:36,71,136-137`, `naming_test.go:146`, `version_test.go:57`, `rawpayloads_test.go:67`, `bigval_test.go:84`, `kinds_test.go:46`.

## Draft follow-up issues (open BOTH before `gh pr create`, then link them in the PR body)

1. **Title:** `openapi: repeated tags and scopes pass into the IR undeduplicated`
   **Body:** `Operation.Tags` (`compilers/openapi/internal/operation/operations.go:361`) and `SchemeUse.Scopes` (`compilers/openapi/internal/auth/auth.go:528`) are copied verbatim, so `tags: [a, a]` or `scopes: [read, read]` reaches the IR repeated, and `tags: [""]` reaches it blank. A source may legally write either, so irverify cannot reject it (#399 records that verdict). The compiler should deduplicate, keeping the first occurrence, and emit a warning diagnostic at the repeated entry's pointer. Fixture: one operation with repeated and blank tags and one security requirement with repeated scopes; assert the deduplicated lists and the diagnostics.
2. **Title:** `ir: Violation.Path carries raw bytes for an ill-formed ID or map key`
   **Body:** #400 quoted every document-derived fragment in a `Violation.Message`, but `Violation.Path` still spells its subject the way `ir.WalkValues` renders it — a map key through `fmt.Sprintf("%v", k)` (`ir/walk.go`), and hand-built paths embedding an ID or key directly (`ir/irverify/ids.go`, `kinds.go`, `unions.go`, `rawpayloads.go`). When that string is ill-formed UTF-8 the path carries the raw bytes: #539's `ir/invalid-utf8` reports a type whose ID holds 0xE9 at `doc.Types[t/x/caf\xe9].key`. (Omar's comment on #400 asks for this half.) A path must stay byte-equal to the walk's rendering, so the fix is in `ir.WalkValues` and the hand-built sites: sanitize the offending segment (e.g. `strconv.Quote` for a string key that is not valid UTF-8) while leaving every well-formed path byte-identical. Test that a well-formed document's paths are unchanged and that an ill-formed key yields a path whose message is `utf8.ValidString`.

## Seams

- `ir/irverify/naming.go`: `checkNaming` (71-96) gains namespace owner selection; `appendAliasViolations` (149-178) becomes the helper plus a thin alias call; new `checkAliasClaims`; quoting at 164, 170, 227, 234, 241, 278-279.
- `ir/irverify/irverify.go`: register `checkAliasClaims` in `walkChecks()` (82-97); quote at 182.
- `ir/irverify/duplicates.go:89`; `ids.go:84-87,105-106,116,130-131`; `refs.go:93`; `rawpayloads.go:94`; `version.go:43`: quoting only.
- Tests: `naming_alias_test.go` (flip 247-281, add claim tests); `naming_test.go:390-442`; new `naming_namespace_test.go`; `refs_test.go:256-265`.
- Docs (else they become false): `ir/naming.go:38-49` (alias rule list) gains the shared rule; `docs/ir-design.md:263-285` gains an `ir/naming-alias-shared` bullet with the type-registry scope.
- **Off limits:** `ir/irverify/values.go` and its tests (PR B), `ir/walk.go`, every compiler.

## Ordered steps (each with its check)

1. **Helper refactor.** Turn the alias switch into `appendListViolations` with no behaviour change. Check: `go test ./ir/irverify -run 'Alias' -count=1` green, no assertion edits.
2. **#400 quoting.** Quote every site in the table and update the listed tests. Check: `go test ./ir/irverify -count=1` green; `grep -rn 'Message:' ir/irverify/*.go | grep -v _test` shows no raw document-derived concatenation.
3. **#399 Namespace.** Add owner selection, `ir/namespace-blank` and `ir/namespace-duplicate`, and the verdict doc comment. Tests:
   - `TestVerify_BlankNamespaceSegmentIsAViolation` — a `TypeCommon` row and a `Service` row, `""`, `" "`, `"\u3164"`, path `….Namespace[i]`.
   - `TestVerify_RepeatedNamespaceSegmentIsAViolation` — `["dup","dup"]` on both owners, reported at index 1.
   - `TestVerify_NamespacePathIsSpelledAsTheWalkWould` — the `naming_alias_test.go:289` pattern.
   - `TestNamespaceOwners_CoverEveryNamespaceField` — reflect over the types reachable from `*ir.Document`; every `[]string` field named `Namespace` must have an owner in the map.
   Check: `go test ./ir/irverify -run 'Namespace' -count=1`.
4. **#398 claims.** Add and register `checkAliasClaims`; flip `TestVerify_AliasSharedByTwoNamings` to one violation, code `ir/naming-alias-shared`, path `doc.Types[t/x/B].Name.Aliases[0]`, message naming the quoted `doc.Types[t/x/A].Name.Aliases[0]`, keeping its not-`-duplicate` guard. Add `…AliasEqualToAnotherSourceIsShared` (both registry orders, always reported at the alias), `…TwoSourcesAreNotShared`, `…SameNamingRepeatStaysDuplicate`, `…ThreeClaimantsReportTwo`, `…BlankAndIllFormedAreNoClaims`, `…PropertyAliasesAcrossModelsAreClean` (scope pin). Check: `go test ./ir/irverify -run 'Alias|WalkChecks' -count=1`.
5. **Docs.** Update `ir/naming.go:38-49` and `docs/ir-design.md:263-285`. Check: the prose names all four alias codes and the scope.
6. **Mutation probes (each: delete, watch red, restore, confirm green).**
   - Remove `checkAliasClaims` from `walkChecks()` → `TestVerify_AliasSharedByTwoNamings` reddens.
   - Drop `ir.Service` from `namespaceOwners` → the Service row of the blank test reddens.
   - Turn off the repeat rule in the namespace helper call → `RepeatedNamespaceSegmentIsAViolation` reddens.
   - Revert the quote in `appendContentViolations` → the `ValidString` assertion in `TestVerify_IllFormedNameIsAViolation` reddens.
7. **Corpus and conformance.** `go test ./ir/irverify -run TestVerify_Corpus -count=1`, `go test ./compilers/openapi -run TestConformance -count=1`, `go run ./cmd/morphic-harness testdata`. Expect no new violations, no golden diff (no compiler sets `Namespace` or `Aliases`; messages aren't in goldens).
8. **Gate.** `make gate` exits 0 at exactly 100% coverage. A pre-existing unrelated failure halts the round and is reported, not worked around.
9. **Ship.**
   - Rename the worktree branch to `fix/irverify-naming`; commit `docs/plans/398-irverify-naming.md` (this plan) first; then the change commits.
   - PR title: `fix(irverify): tighten naming rules and quote messages` — 61 chars; verify `printf '%s (#999)' "<title>" | wc -c` ≤ 72.
   - PR body: Summary / Test plan, ending `Closes #398, #399` and `Refs #400`. Name the out-of-scope items (the Path half of #400, the Tags/Scopes follow-up, the undecided content-type/encoding lists, the non-type Naming scope).
   - `gh pr create --assignee fuad-daoud --reviewer OmarAlJarrah`. Push the branch.

**Coverage for every new arm:**

| Flag / arm | Covering test |
|---|---|
| Helper blank / repeat / redundant (aliases) | existing alias tests |
| Helper blank+repeat on Namespace | the two namespace tests |
| `namespace-blank` on TypeCommon and Service | the blank test's two rows |
| `namespace-duplicate` on TypeCommon and Service | the repeat test's two rows |
| Claims: alias vs alias | `AliasSharedByTwoNamings` |
| Claims: Source later / Source first | both orders of `AliasEqualToAnotherSourceIsShared` |
| Claims: Source vs Source skip | `TwoSourcesAreNotShared` |
| Claims: same-Naming skip | `SameNamingRepeatStaysDuplicate` |
| Claims: blank / ill-formed skip | `BlankAndIllFormedAreNoClaims` |
| Claims: truncation | `TestWalkChecks_EachReportsTruncation` |

## Deleted behaviour (closed list)

1. Two type-registry Namings claiming one alias string (at least one claim an alias) no longer verify clean.
2. A blank `Namespace` segment on `TypeCommon` or `Service` no longer verifies clean.
3. A repeated `Namespace` segment on `TypeCommon` or `Service` no longer verifies clean.
4. Violation messages no longer carry raw document text at the 17 sites in the table.
5. `TestVerify_AliasSharedByTwoNamings → Empty` and the "#400 out of scope" sentence at `naming_test.go:395-398` are replaced.

Nothing else is removed: no code, golden or fixture.

## Report must include

- Changed paths with line ranges; the commands run and their outcomes (targeted tests, corpus, conformance, harness, `make gate`).
- The golden status.
- Each mutation probe: what was deleted, which test reddened, the green result after restoring.
- The settled code names.
- How decisions 1-3 were applied.
- Both follow-up issue URLs and the statement of what closes #399 and what does not.
- Branch, PR URL, measured title length.