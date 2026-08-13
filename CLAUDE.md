# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

Morphic is in **early development**, past the design stage. `README.md` carries the milestone table
and what is implemented — read it there. Restating it here would only give it a second place to rot,
which is the same rule as "derive counts; never maintain them" below.

Standard Go tooling works today: `go build ./...`, `go test ./...`,
`go test ./ir -run TestName` for a single test, `go vet ./...`.

## What Morphic is

A spec-to-SDK compiler: any API spec (OpenAPI, Swagger 2.0, TypeSpec, Smithy, GraphQL, AsyncAPI,
Protobuf, Erlang/OTP module specs) → **one spec-agnostic intermediate representation (IR)** →
idiomatic SDKs and docs.
Pipeline: **compilers** (spec → IR) → **IR passes** (IR → IR) → **emitters** (IR → artifacts).

## The documents are the spec — read them first

`ls docs/*.md` is the set; what follows is why each one matters, not how many there are.

- **`docs/ir-design.md` is normative.** Field names and struct shapes in it are the contract;
  receiver methods and helpers are not. When implementing the IR, match its shapes exactly.
- **`docs/emitter-design.md` is normative for the emitter half**, the way `ir-design.md` is for the
  IR. Nothing under `emitters/` exists yet, so read it before writing the first one.
- `docs/architecture.md` — pipeline stages, package layout, layering rules, milestones.
- `docs/ir-spec-matrix.md` — the union of source-format capabilities the IR is designed against.
- `docs/prior-art.md` — the evidence base (oagen, Kiota, TypeSpec/TCGC) and the specific mistakes
  each Morphic decision is designed to avoid. Read this before proposing IR changes; most
  "simplifications" that come to mind are failure modes already rejected here.
- `docs/reference-learnings.md` — the same evidence base widened to every shipped generator it
  surveys (its header names them), each finding carrying a verdict on a Morphic decision and
  citing the repo it came from.
- `docs/micro-compiler-design.md` and `docs/micro-compiler-plan.md` — the restructuring that
  produced today's `compilers/openapi`. Both record work that has landed; read them for why the
  package boundaries fall where they do, not as a proposal or a backlog.

## Invariants that must not be violated

These are load-bearing design decisions, not preferences. Breaking one defeats the project's core
claim (lossless, spec-agnostic, many-target). Before changing any of them, re-read the rationale
in the docs.

1. **The IR is the ABI.** Compilers and emitters never see each other. A compiler's only output is
   an IR document + diagnostics; an emitter's only input is an IR document + its own options.
2. **Lossless by default, lowered late.** Compilers never flatten (no `allOf` merging, no
   union-to-optional-fields collapse, no primary-response selection). Composition, unions,
   visibility, discriminators, encodings, streaming stay in source-semantic form. Lowering to what
   a target language can express happens only in emitter refiners. The one documented carve-out is
   validation-only JSON Schema (`not`/`if-then-else`/`dependentSchemas`), kept verbatim in
   `Unmodeled` — see `ir-design.md §4.7`. Every `Unmodeled` entry records *why* it was kept
   (`UnmodeledReason`) and where it came from, so a consumer can take only the subset it wants.
3. **Stable IDs; names are presentation.** Every named entity has a synthetic ID derived from its
   source pointer (never from a display name, never rewritten by renames). Entities live in flat,
   ID-keyed registries and reference each other by ID; no node embeds another named node.
   Corollary — **a minted node needs a namespace of its own.** Any lowering that synthesizes a node
   at a pointer the source can *also* name must derive its ID in a separate namespace, never equal
   to the pointer's own ID. Two types on one pointer makes the IR depend on which declaration
   lowered first, and it does so silently: no diagnostic in either order, `pass/validate` clean on
   both. `ir-design.md §4.3` records this for distributed unions; the rule is the mechanism, not
   that one site.
4. **Names are neutral, never cased.** `Naming` stores source name + neutral canonical word
   sequence + wire name (+ numeric wire ID where applicable). The IR never stores camelCase /
   PascalCase — emitters own all identifier rendering, casing, and reserved-word escaping.
5. **Pure, reentrant stages.** Every stage is `f(input, options) → (output, diagnostics)` with no
   package-level mutable state. No stage writes to stderr; each emits typed `Diagnostic` values
   (severity, stable string code, message, provenance). The engine/CLI decides what is fatal.
6. **Heuristics are policy, not semantics.** Anything inferred rather than declared (pagination
   from param names, envelope detection, acronym casing) lives in injectable per-compiler/emitter
   policy, is marked `Inferred` in provenance, and can be disabled.
7. **Serializable & deterministic.** The whole `Document` round-trips through JSON (maps emitted in
   sorted-key order, slices in source order). This underpins golden snapshots, IR diffing, caching.
8. **Optionality ≠ nullability.** `Property.Required` (wire presence) is orthogonal to
   `TypeRef.Nullable` (this usage admits null). Both are needed for the four distinct states.
9. **The IR capability surface is complete from day one.** Only compilers are staged over time;
   shipping OpenAPI first must never force an IR schema change when later compilers land.
10. **SDK runtime policy is not IR.** Retry/timeout/telemetry/error-class taxonomy is a separate
    emitter input. The IR describes the API; policy describes the SDK.

## Go representation conventions the design mandates

- **Closed sums = sealed interfaces**: unexported marker method (`typeDef()`), one concrete struct
  per kind, a `Kind()` accessor for switch-dispatch, and a switch-completeness test over the kind
  enum (the `assertNever` lesson). None of it is code-generated — the module has no `go:generate`
  at all (`grep -rn go:generate --include='*.go' .` is empty). `TestTypeDef_KindDispatchIsComplete`
  iterates a hand-written `allKinds`, and `TestTypeDef_HandWrittenKindListsAreComplete` holds that
  list and every other hand-written kind list to the kinds the `ir` sources declare, so adding a
  kind without updating a list reddens there instead of quietly narrowing what a test covers. JSON
  encodes sums with an adjacent `kind` tag.
- **No `float64` anywhere in the IR.** Numeric values, defaults, and constraints use arbitrary-
  precision decimal strings (`BigVal`). This is a hard rule (the TypeSpec `Numeric` lesson).
- **Values are a separate channel from types** (`Value`/`ValueKind`), per the TypeSpec Type-vs-Value
  split. Defaults, consts, literals, enum values, examples are typed data, not type nodes.
- **Containers (list/map/tuple) are real type nodes with IDs**, hoisted like any anonymous type —
  never flags on a reference.

## Package layout & layering (enforced by an architecture test)

```
ir/          Layer 0 — IR nodes, IDs, traversal, JSON round-trip. Imports ONLY the stdlib.
  irverify/  Layer 0 — structural-invariant checks over a compiled Document. Imports ir only.
  irtest/    Layer 0 — golden-file helper (`-update` rewrites). Imports ir + go-cmp.
compilers/   Layer 1 — the Compiler contract and the format-keyed registry. Imports ir only.
  compile/   Layer 1 — what every compiler shares: the type registry with its coordinate map,
             diagnostics, and the naming and identifier grammars. Imports ir only.
  <format>/  Layer 1 — one compiler per format: its public face and nothing else — the
             Compiler, its Options, and the run that calls its lowerings in order. Imports ir,
             compilers, compilers/compile, its own internal/* (+ own format libs); never each
             other, never emitters/engine.
    internal/  Layer 1 — that compiler's own packages, each with its own allowlist rather than
             the compiler's. The ordering among them is real and enforced, from diag (reaches
             only ir) up to operation. None may reach the compiler above it.
pass/        Layer 1 — IR → IR passes. Imports ir only.
emitters/*   Layer 2 — imports ir + emitter contract; never compiler. (Not built yet.)
engine/      Layer 3 — orchestration; imports everything below.
cmd/morphic/ Layer 4 — CLI; imports ir + engine.
cmd/morphic-harness/
             Layer 4 — sweeps a spec or directory through the oracles; imports internal/harness.
internal/    Test/tooling infrastructure, outside the pipeline (harness, archtest, testspec).
```

The layering is enforced by `internal/archtest`, and **its `rules` map is the source of truth** —
this diagram is prose, and deliberately neither names nor counts a compiler's internal packages.
Read them off the tree:

```bash
git ls-files '*/*.go' | xargs -n1 dirname | sort -u
```

Three things follow when you add a package: put it in `rules` (or in `exempt` with the reason it
is out), because `TestImportGraph_EveryPackageIsRuledOrExempt` fails otherwise; remember an
unkeyed subdirectory is audited under its nearest keyed ancestor's allowlist, so nesting is not a
way to widen one; and give it its own entry rather than letting it inherit a wider one, since an
inherited allowlist says nothing about where the package sits.

Two caps guard shape rather than the graph. No type under `compilers/` may carry more than 20
methods (`internal/archtest`), and no function may exceed 70 lines or a cognitive complexity of 20
(`.golangci.yml`, tests excluded). The method cap is the one that matters: the god object this
structure replaced reached 159 methods without ever writing a long function, so every rule then in
force stayed green while it grew. A failure is a prompt to ask what the type has started doing.

## Testing strategy

These all exist already — extend them rather than building a parallel mechanism:

- **Golden IR snapshots**: `spec → IR → JSON`, compared via `ir/irtest` (`-update` rewrites).
  Corpus under `testdata/golden/`.
- **Capability conformance corpus** (`testdata/conformance/`): one minimal spec per
  `ir-spec-matrix.md` row per format that can express it, asserting lossless capture. This is what
  keeps "lossless by default" honest. The row↔spec mapping is machine-read, not prose: matrix rows
  carry stable keys, each case names the keys it witnesses, and
  `compilers/openapi/conformance_matrix_test.go` requires every expressible row to be witnessed or
  listed with a reason. What it cannot check is whether a spec that *names* a row exercises that
  capability — that claim is read by a reviewer, so weigh it like any other.
- **Oracles**: `internal/harness` drives a spec through no-panic → no error diagnostic →
  `irverify` invariants → JSON round-trip → determinism → order-invariance, stopping at the first
  one that fires. `harness.Check` is the list — read it there rather than trusting this sentence;
  `go run ./cmd/morphic-harness <file|dir>` runs them over one spec or a whole tree. `irverify` is
  the structural-invariant checker (stable IDs, no dangling refs, neutral naming, routable
  `Unmodeled`, in-range provenance); its findings are `Violation` values — *our* bugs —
  deliberately a channel separate from `ir.Diagnostic`, which reports problems in the source spec.
- **Architecture test**: `internal/archtest`, per the layering section above.

Beyond those, "verify by executing" below has consequences specific enough to write down as
assertion shapes:

- **Order-dependence needs a two-order diff.** Compile the same source twice with the declaration
  order swapped and `cmp.Diff` the two documents. A single-order test passes on *both* orders of a
  colliding lowering, which is why the pointer collisions survived the suite. The order-invariance
  oracle above is the general form of this and already runs it across the corpus under `testdata/`,
  so the usual way to cover a new construct is to add a spec the sweep reaches, not to hand-roll
  the diff. A targeted case still earns its place when it pins one lowering, because the oracle
  proves order-independence only for the constructs its inputs happen to contain.
  - A fixture's own declaration order is part of the test. A golden that happens to declare things
    in the order the *correct* lowering already produced cannot see the fix at all: reverting it
    leaves the golden green, and only the two-order oracle reddens. If a single-order case is meant
    to guard an order-dependent fix, declare them in the order that was wrong, and confirm the
    revert reddens it rather than assuming the case covers what its name says.
- **A negative fixture must be reachable.** `harness.Check` returns at the *first* error diagnostic,
  before `irverify` ever runs, so a fixture that trips one never reaches the invariant checks it was
  written to exercise. Verify the fixture lands where you think it does, not merely that the check
  exists.

## Go code style — the dexpace styleguide is binding

All Go in this repo follows the org styleguide at
[dexpace/styleguide](https://github.com/dexpace/styleguide) (`go/` chapters 01–13). It extends
the [Google Go Style Guide](https://google.github.io/styleguide/go/); where they conflict,
Google wins except for the recorded deviations (function-size cap, assertion density, bounded
recursion). Priority order: **correctness > performance > developer experience**. The rules
below are the ones most likely to bite in this codebase — the full guide governs everything else.

- **Functions: 70-line hard cap, aim 20–40.** One thing each; blank lines between logical
  sections; guard clauses and early returns over nesting; no `else` after a returning `if`.
- **Assert aggressively** (TigerBeetle discipline): validate at every public boundary; aim ≥2
  assertions (precondition/postcondition/invariant checks returning errors) per function; split
  compound checks. Never accept garbage silently.
- **Bounded everything.** Every loop, queue, retry, buffer, and timeout has an explicit limit.
  Recursion is permitted **only** with a provably bounded depth: an explicit counter checked
  against a named hard cap, recovered at the public boundary. This applies directly to schema
  lowering and IR traversal — deep/cyclic specs are normal inputs, not edge cases.
- **Errors are values.** Handle every error (`_ = err` is banned); wrap with `%w` + context,
  one level, `%w` at the end; branch with `errors.Is`/`errors.As`, never string matching;
  sentinel errors `Err…`, error types `…Error` implementing `Unwrap`; error strings lowercase,
  unpunctuated; no in-band errors (no `-1`/`""` for "not found"); no panics escaping a package
  (`Must*` only in init/tests). Note: pipeline stages additionally report *spec problems* as
  `ir.Diagnostic` values, not Go errors — Go errors are for I/O and programmer errors.
- **API design:** accept interfaces, return concrete structs; small consumer-defined
  interfaces; `any` not `interface{}`; comma-ok on every type assertion; type switches carry a
  `default`; zero values useful or obviously invalid; copy slices/maps at API boundaries; no
  mutable globals (this repo's "pure, reentrant stages" invariant is the same rule).
- **Concurrency:** prefer synchronous APIs — the caller adds concurrency; `ctx context.Context`
  first parameter, never stored in a struct; every goroutine has a documented lifetime and stop
  mechanism; `errgroup` for groups; bounded fan-out.
- **Naming:** MixedCaps; scope-proportional length; no `Get` prefix on getters; initialisms in
  one consistent case; package names short lowercase nouns; never shadow builtins or imports.
- **Testing:** table-driven and flat; `TestFunc_Scenario` names; `testify/require` for
  preconditions, `assert` for values; compare with `cmp.Diff`, never `reflect.DeepEqual`;
  golden files for complex expected output; `t.Helper()` in helpers; `t.Cleanup()` over
  `defer`; external test packages (`package foo_test`) preferred; `goleak` where goroutines
  exist.
- **Packages:** one package per directory; `internal/` aggressively for implementation detail;
  no `utils`/`helpers`/`common`; `doc.go` for package docs; imports in three `gci` groups
  (stdlib, external, local); no dot imports.
- **Docs:** GoDoc on every exported symbol starting with its name, complete sentences; package
  comment on every package; comments explain *why*, not what.
- **Serialization:** explicit JSON struct tags on every field; `omitempty` only on optional
  fields; custom codecs for special forms, but not symmetrically — each member of the IR's
  `TypeDef` sum has a `MarshalJSON` that writes its adjacent `kind` tag, and *decoding* is
  centralized in `(*TypeRegistry).UnmarshalJSON`, which reads that tag; no member unmarshals
  itself, and `BigVal` has no codec at all because it *is* a string type and already marshals as
  a JSON string. `grep -rnE 'func .*(Unm|M)arshalJSON' ir/` is the current list; never `float64`
  for money — and in this repo, never in the IR at all, per the representation conventions above.
- **Logging:** `log/slog` only, injected — but note the stronger repo invariant: pipeline
  stages don't log at all; they return diagnostics.

Before claiming any Go work done, run and pass the same checks CI's `gate` job runs
(`.github/workflows/gate.yml`), in that order:

```bash
gofmt -l $(git ls-files '*.go')     # must print nothing
go vet ./...
golangci-lint run                   # must pass clean
go build ./...
./scripts/verify-coverage-count.sh  # how the gate below counts a profile
./scripts/check-coverage.sh         # go test ./... + exactly 100% statement coverage
```

**Coverage is a gate at exactly 100%, not a target.** `scripts/check-coverage.sh` counts
statements from the profile rather than reading `go test`'s rounded percentage, so one uncovered
statement fails the build — `go test ./...` passing locally is not evidence the gate passes.

## Repository rules

These match the conventions already in force across the other dexpace SDK repos
(`dexpace/java-sdk`, `dexpace/dexpace-react`) — kept consistent so contributors don't have to
context-switch between repos.

- Branch from `main`: `type/short-desc` (e.g. `feat/openapi-compiler-skeleton`,
  `fix/ir-nullable-defaulting`, `docs/architecture-milestone-2`).
- No CODEOWNERS, PR/issue templates, or CODE_OF_CONDUCT.md exist in this repo — don't assume them
  or invent content for files that aren't there.
- CI is one bundled `gate` job (`.github/workflows/gate.yml`) on PR and on push to `main`. Keep it
  that way — add steps to it rather than new workflows; both reference repos converge on one
  bundled gate over granular per-check pipelines.

## Commits & pull requests

- **Zero nits, zero flaws — correctness before shipping.** A PR is not ready because it works; it is
  ready when there is nothing left to find. Every known defect is fixed or explicitly justified in
  writing before the PR opens — never deferred silently, never left for the reviewer to catch. This
  applies to the small things too: a doc comment that is imprecise, a commit subject over the
  72-char cap, a test that passes without asserting what its name claims. "Minor" is a severity, not
  a permission to ship it.
  - Review the **final** state, not an intermediate one. If commits land after a review, that review
    is stale — re-review the branch as it will be merged.
  - Prefer proving a claim over asserting it — see "verify by executing" below.
  - A limitation that is deliberately out of scope must be stated in the code and in the PR body, in
    a place the next reader will actually reach — not only in a commit message or a scratch note.
  - **A line you were forced onto may already have a decision recorded.** When a change makes an
    unrelated line fail to compile or fail a new check, search the tracker for it before choosing —
    `gh issue list --search "<file or symbol>"`. A guard tightened in #185 made one `ir.Naming`
    literal a violation; the fix picked one of two spellings without knowing #184 had already framed
    that exact choice, said which way it should go, and said it wanted its own change. Correcting it
    took a second PR. Follow the recorded reasoning or say why you are departing from it, but do not
    settle a recorded decision as a side effect of something else.
- **Verify by executing.** Every rule below was learned by shipping its opposite here, and they are
  all one mistake: *asking a question whose answer the thing under test already controls.* Reading
  the code, reading the test, regenerating the golden, compiling one declaration order — none of
  these can disagree with you. Disagreement has to enter from outside: a planted mutation, a probe,
  the opposite order.
  - **Probe, don't read.** Reviews that read the code pronounced it clean while it was silently
    dropping data. Compile the input; inspect the emitted IR *and* the full diagnostic list.
  - **Break what a test claims to catch.** Assertions that look correct on the page find nothing.
    Tests here have been caught asserting nothing at all, and one "safety net" permitted the exact
    addition it advertised catching. Plant the defect and watch it go red.
  - **A regeneration that changes nothing looks exactly like a broken `-update`.** A corpus shares
    the blind spots of the code it covers, so confirm a deliberate deletion from a source spec
    reddens the suite.
  - **A check that runs is not a check that reaches.** A verifier can be complete, well-tested and
    wired into CI while never meeting the output it exists to check — an early return upstream, or
    an input class no committed fixture produces, is enough. Establish where it reaches before
    claiming what it covers; the testing section names the live instance.
  - **Scope a result narrowly, a defect widely.** These pull opposite ways on purpose. A mutation
    proves something about the site it was planted at, not every site of its kind — so name the site
    in the sentence. But a defect is a property of its *mechanism*: the pointer collision was fixed
    in the union lowering and left standing in the inline-position hoist on the same branch, because
    the fix was scoped to where it was noticed. Sweep every site of the mechanism before closing it.
  - **Derive counts; never maintain them.** Counts and universals in prose are where this repo's
    errors concentrate, and no test can catch them. Prefer the command that derives a number, or
    omit it. If a claim about code must be written down, name the revision it was true at.
- **Conventional Commits**: `type(scope): subject`, imperative mood, subject line only (no period,
  ≤72 chars). Common types: `feat`, `fix`, `refactor`, `docs`, `test`, `build`, `chore`, `ci`,
  `perf`. Scope is the touched package (`ir`, `compilers/openapi`, `pass`, `emitters/go`, `engine`)
  when it narrows things down — omit it when the change is repo-wide.
- **Breaking changes** mark the type with `!` (`feat!:`, `refactor!:`) and explain the break in the
  commit body — don't bury it in the subject line alone.
- **The PR title is the commit subject.** PRs are squash-merged, so the *title* — not the branch's
  commit subjects — is what lands on `main`. It takes the same Conventional Commits form and the
  same rules: `type(scope): subject`, imperative, no trailing period. A well-formed commit under a
  prose PR title still merges as a non-conforming commit, and since the squashed subject *is* the
  title, every non-conforming subject in this history arrived exactly that way. To see the current
  state rather than trust this sentence:

  ```bash
  git log --format='%s' origin/main | grep -E '\(#[0-9]+\)$' |
    grep -vE '^(feat|fix|refactor|docs|test|build|chore|ci|perf)(\(.+\))?!?: '
  ```

- **Budget the appended number inside the 72-char cap.** GitHub appends ` (#NNN)` to the squashed
  subject automatically — never type it yourself, but do count it: a three-digit PR leaves the title
  65 characters, not 72. Measure before opening, don't eyeball —
  `printf '%s (#999)' "<title>" | wc -c`. Most over-length subjects in this history were *within*
  the cap until the number was appended, which is the whole failure mode; to see that split rather
  than trust it, pipe the log above through `awk 'length($0) > 72'` and then through
  `sed -E 's/ \(#[0-9]+\)$//'` before the same `awk`.
- PR description: Summary / Test plan (/ Breaking, when applicable). Keep PRs scoped to one
  logical change; split unrelated changes into separate PRs.
- Write self-contained, human-framed titles/descriptions. No LLM/session artifacts, no internal
  audit/finding IDs, no "remediation"/"audit sweep" framing. State problem, change, and rationale
  on their own terms.
