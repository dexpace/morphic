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

`ls docs/*.md docs/compilers/*.md` is the set; what follows is why each one matters, not how many
there are. A first change starts from [`CONTRIBUTING.md`](CONTRIBUTING.md), whose recipes for a
lowering rule, a pass and a compiler say which check each step answers to, and which steps no check
covers.

- **`docs/ir-design.md` is normative.** Field names and struct shapes in it are the contract;
  receiver methods and helpers are not. When implementing the IR, match its shapes exactly.
- **`docs/compilers/openapi.md` is normative for the OpenAPI compiler**: its lowering rules, one
  keyed entry each, with the cases, codes and tests that pin it. Read it for work on that compiler
  only; every other layer needs ir-design and not this.
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
- `docs/parser-library.md` — what the OpenAPI compiler depends on its parser library doing, defects
  included: one keyed entry per behaviour, naming the code that exists because of it and the canary
  in `compilers/openapi/internal/canary` that goes red when a library change alters it. Read it
  before working in the load phase or bumping the library, and cite an entry rather than restating
  a behaviour or naming a library version, which a test refuses.
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
   (severity, stable string code, message, provenance). The engine decides what is fatal.
6. **Heuristics are policy, not semantics.** Anything inferred rather than declared (pagination
   from param names, envelope detection, acronym casing) lives in injectable per-compiler/emitter
   policy, is marked `Inferred` in provenance, and can be disabled.
7. **Serializable & deterministic.** The whole `Document` round-trips through JSON (maps emitted in
   sorted-key order, slices in source order). This underpins golden snapshots, IR diffing, caching.
8. **Optionality ≠ nullability.** `Property.Required` (wire presence) is orthogonal to
   `TypeRef.Nullable` (this usage admits null). Both are needed for the four distinct states.
9. **The IR's capability surface is designed against every format; a gap closes as a versioned
   change.** Every construct a supported format can express is meant to have a planned home in the
   IR, or a reasoned `Unmodeled` entry where the IR declines to model it, so a later compiler fills
   fields rather than inventing them. That is a goal, not a finished state: a compiler that meets a
   construct with no home keeps it verbatim under `no_ir_home`, and the field that homes it lands as
   a change of the IR's shape, which bumps `IRVersion` (ir-design §2.1). Once an emitter reads a
   field, a change to it lands together with every emitter update it forces (architecture.md §2.5).
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
  irtest/    Layer 0 — golden-file helper (`-update` rewrites) and fixture helpers. Imports ir + go-cmp.
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
cmd/morphic/ Layer 4 — CLI; imports ir + engine + its command framework (cobra, pflag).
cmd/morphic-harness/
             Layer 4 — sweeps a spec or directory through the oracles; imports internal/harness.
cmd/morphic-census/
             Layer 4 — counts which IR facts the documents compiled from specs set; imports
             engine + internal/census + internal/harness.
cmd/morphic-covergate/
             Layer 4 — judges a coverage profile for `make coverage`; imports x/tools/cover.
internal/    Test/tooling infrastructure, outside the pipeline (harness, archtest, census, decls,
             jsonshape, keydoc, leakcheck, testspec).
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

Caps also guard shape rather than the graph. No type under `compilers/` may carry more than 20
methods (`internal/archtest`), and no function may exceed 70 lines or a cognitive complexity of 20
(`.golangci.yml`, tests excluded). The method cap is the one that matters: the god object this
structure replaced reached 159 methods without ever writing a long function, so every rule then in
force stayed green while it grew. A failure is a prompt to ask what the type has started doing.

No lint suppression stands in the source: `internal/archtest` refuses a `//nolint`, `revive:` or
`lint:ignore` comment. A suppression is a rule under `exclusions` in `.golangci.yml`, kept as narrow
as the finding it excuses, with its reason beside it.

## Testing strategy

These all exist already — extend them rather than building a parallel mechanism:

- **Golden IR snapshots**: `spec → IR → JSON`, compared via `ir/irtest` (`-update` rewrites).
  Corpus under `testdata/golden/`. A golden stores no `irVersion`: the helper asserts the stamp and
  leaves it out, so a version bump rewrites no golden.
- **IR shape pin**: `TestIRVersion_PinsTheEncodedShape` describes every type `ir.Document` reaches,
  through `internal/jsonshape`, and holds `ir.IRVersion` to the description's hash in the
  `generations` table beside it. A shape change without a bump fails, and so does a bump without
  a shape change unless its row says what meaning changed. A bump moves the constant, appends a
  row and adds an ir-design §2.1 history entry; `ir/testdata/shape.golden.txt` diffs the shape.
- **Capability conformance corpus** (`testdata/conformance/`): one minimal spec per
  `ir-spec-matrix.md` row per format that can express it, asserting lossless capture. This is what
  keeps "lossless by default" honest. The row↔spec mapping is machine-read, not prose: matrix rows
  carry stable keys, each case names the keys it witnesses, and
  `compilers/openapi/conformance_matrix_test.go` requires every expressible row to be witnessed or
  listed with a reason. What it cannot check is whether a spec that *names* a row exercises that
  capability — that claim is read by a reviewer, so weigh it like any other.
- **Lowering specification**: `docs/compilers/openapi.md` is machine-read like the matrix. Each rule
  names its evidence, and the `TestLoweringSpec` tests in `compilers/openapi` hold the names to the
  code both ways, each listed code to the rule's text, and every `docs/compilers/openapi.md#<key>`
  citation to a rule. It and `docs/parser-library.md` are read through `internal/keydoc`, the one
  reader of the repository's keyed documents, and a test a Markdown document names must exist
  (`internal/archtest`).
- **Oracles**: `internal/harness` drives a spec through no-panic → no error diagnostic →
  `irverify` invariants → JSON round-trip → determinism → order-invariance, stopping at the first
  one that fires. `harness.Check` is the list — read it there rather than trusting this sentence;
  `go run ./cmd/morphic-harness <file|dir>` runs them over one spec or a whole tree. `irverify` is
  the structural-invariant checker (stable IDs, no dangling refs, neutral naming, routable
  `Unmodeled`, in-range provenance); its findings are `Violation` values — *our* bugs —
  deliberately a channel separate from `ir.Diagnostic`, which reports problems in the source spec.
- **Architecture test**: `internal/archtest`, per the layering section above. It also holds each
  production package's uses of the parser library's reference-resolution API, which it derives
  from the library's sources, to `resolutionBaseline`. The baseline only shrinks: removing a use
  means lowering it (#558).
- **Declaration pins**: a test that holds a hand-written list (an enum's members, a sealed sum's
  kinds, the fields of a shape) to what a package declares reads the package through
  `internal/decls`, which type-checks it with `go/packages`. It does not parse the sources: parsed
  readers each rebuilt const-group carry-forward, aliases and import names their own way and were
  blind to spellings the compiler accepts (#409). Each package is loaded on its own, because
  `go/types` can race when one load checks several at once (golang/go#81122), so a type another
  package declares is compared through `Package.Imported`, the copy the package itself sees.
- **Goroutine leak check**: `internal/leakcheck` wraps `TestMain` around Go 1.27's `goroutineleak`
  profile, and `internal/archtest` requires it wherever a `go` statement or a single-argument
  `.Go(...)` call (`sync.WaitGroup.Go`, `errgroup.Group.Go`) appears. It finds only a goroutine
  blocked on an unreachable primitive — not a running one, nor one a global still holds; whether
  `goleak` should cover those too is #568.

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
  `defer`; external test packages (`package foo_test`) preferred; where goroutines exist, the
  goroutine leak check under Testing strategy above (the styleguide names `goleak`; #568).
- **Packages:** one package per directory; `internal/` aggressively for implementation detail;
  no `utils`/`helpers`/`common`; `doc.go` for package docs; imports in three `gci` groups
  (stdlib, external, local); no dot imports.
- **Docs:** GoDoc on every exported symbol starting with its name, complete sentences; package
  comment on every package; comments explain *why*, not what.
- **Doc comments: 100 words at most.** The cap is strict and holds for each doc comment on its
  own — package, declaration, field or method, tests included. A word is a token with a letter
  in it: `//` markers, directives, numbers and punctuation are not counted.
  A comment may exceed it only when the excess is both *necessary* (a reader of that symbol cannot
  do without it, and it has no better home in `docs/`, an issue or a commit message) and
  *valuable* (it says what neither the code nor the names can). Narrative history, restated code
  and a second telling of the why never qualify, and a longer comment nearby is not precedent.
  `internal/archtest` enforces the cap, and `go test ./internal/archtest -run TestDocComments`
  lists every comment over it; an exception is a `longDocComments` entry there, with its reason.
- **Serialization:** JSON goes through `encoding/json/v2` and `encoding/json/jsontext` only;
  `encoding/json` (v1) is banned by `internal/archtest`, tests included. Explicit struct tags on
  every field: `omitempty` only on strings, slices and maps whose empty and absent forms mean the
  same; `omitzero` on every other optional field (pointers, bools, numbers), because v2's
  `omitempty` keeps a zero number or bool and drops a pointer to an empty struct, and on a
  collection where nil and empty differ (`Operation.Auth`: absent inherits, `[]` is explicitly
  public). A `Document` owns its bytes both ways: `MarshalJSONTo` pins the canonical options
  (sorted maps, RFC 8785 minimal escaping, no invalid UTF-8), and `UnmarshalJSONFrom` reads
  `irVersion` first, then refuses unknown members, duplicate names and invalid UTF-8 whatever the
  caller's options. A sub-structure encoded on its own takes the caller's options: pass
  `json.Deterministic(true)`. Each `TypeDef` kind codes itself through one generic kind-tagged
  wrapper, and `(*TypeRegistry).UnmarshalJSONFrom` dispatches on the tag; `BigVal` has no codec
  because it *is* a string type. `grep -rnE 'func .*(Unm|M)arshalJSON' ir/` is the current list;
  never `float64` for money — and in this repo, never in the IR at all, per the representation
  conventions above.
- **Logging:** `log/slog` only, injected — but note the stronger repo invariant: pipeline
  stages don't log at all; they return diagnostics.

Before claiming any Go work done, run and pass the gate:

```bash
make gate
```

That is not a summary of CI — it is what CI runs. Every check in `.github/workflows/gate.yml` runs a
`Makefile` target bar `lint`, which the golangci-lint action runs at the version the `Makefile`
pins, so the local command and the job are the same commands in the same order. Read the `Makefile`
for the step list rather than restating it here; `make coverage`, `make fuzz`, `make bench` and the
rest are individually runnable while iterating.

**Coverage is a gate at exactly 100%, not a target.** `cmd/morphic-covergate` counts
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
- **Never rewrite a pushed PR branch.** Every change to an open PR, whether a review round's fixes
  or `main` merged in, goes on as new commits pushed as a fast-forward. Never amend, squash, rebase
  or force-push it, not even to keep it at one commit. A force-push deletes the per-round diffs a
  reviewer reads and orphans their comments, and the squash merge keeps `main` clean whatever the
  branch holds. To pick up `main`, merge it in rather than rebasing. The squash commit's *body* is
  settled when merging, not by shaping the branch:
  `gh api repos/dexpace/morphic --jq .squash_merge_commit_message` names what a default squash
  pastes, and `COMMIT_MESSAGES` pastes every branch commit's body. So write commit messages that
  could stand on `main`, and set the squash body at merge time.
- PR description: Summary / Test plan (/ Breaking, when applicable). Keep PRs scoped to one
  logical change; split unrelated changes into separate PRs.
- Write self-contained, human-framed titles/descriptions. No LLM/session artifacts, no internal
  audit/finding IDs, no "remediation"/"audit sweep" framing. State problem, change, and rationale
  on their own terms.

## Filing issues

An issue is read later by someone who was not in this session. File it so the problem can be
reproduced, ranked and fixed from its text alone. Labels are set by hand (nothing syncs them), so
the filer sets them.

### Before filing

- **Search the tracker.** `gh issue list --state all --search "<symbol, file or construct>"`. A
  duplicate gets a comment on the existing issue. A recorded decision is followed, or the new issue
  says why it departs from it.
- **Probe, don't read.** Reproduce the problem before writing it up: compile the spec, then quote
  the emitted IR and the full diagnostic list. The reproduction is a minimal spec, not a description
  of one. A negative claim ("X is never checked") names the probe that shows it.
- **One mechanism per issue, every site of it in that issue.** Sweep the sites before filing and
  list them, so a fix scoped to where the bug was noticed shows as incomplete. Unrelated findings
  are separate issues, each ranked, scheduled and closed on its own. An issue that spans tiers is
  split so each part carries its own.
- **File an adjacent bug; don't fix it inline.** The PR stays one logical change.

### Title and body

The title is `area: statement`. `area` is the package or document (`openapi`, `ir`, `pass`, `cli`,
`engine`, `test`, `docs/emitter-design`); `gh issue list --state open --json title` shows the
prefixes in use, so reuse one. After the colon: lowercase, no trailing period. A defect states what
happens (`a nested inline allOf lowers to an empty model`), a proposal is an imperative, and an open
question starts `decide whether`.

```markdown
## Problem
<what happens, on its own terms: a minimal spec, the observed IR or diagnostic, the expected result>

## Cause
<symbol or file at the named revision; omit when unknown>

## Direction
<a proposal, labelled as one; the alternatives weighed when there is a real choice>

## Out of scope
<what this issue deliberately leaves, stated where the next reader will reach it>

Related: #N

Priority: P<n>, <rule>. <command> → <result> (spec @<sha>, morphic @<sha>).
```

- **Name the revision** each observation came from and the command behind every number. A count in
  prose is a claim about one revision.
- **Self-contained and human-framed**, as for a commit message or PR: no session artifacts (probe
  labels, "the brief"), no internal finding IDs, no "audit" or "remediation" framing, no assistant
  attribution.
- **No third-party tool, repo or vendor extension key as the reason or origin.** State the problem
  itself. Standards (RFCs, OpenAPI, JSON Schema, Overlay) may be cited. The one external artifact
  an issue may name is the golden spec below, as the thing measured and never as why a change is
  wanted. Grep the draft for what it must not say before filing.
- **Direction is a draft.** The implementer reasons about it and improves it before building it as
  written.

### Priority

Exactly one `priority:` label; the rules below define the tiers, and a label's one-line description
is shorthand. Specs are assumed valid, so impact is measured, not argued, against the **golden
spec**: the milestone's reference input, the Cloudflare API schema
(<https://github.com/cloudflare/api-schemas>, `openapi.json`), itself treated as valid. Read it in
place; it is large and not ours to commit.

```bash
D=$(mktemp -d)   # outside the repo
gh api -H "Accept: application/vnd.github.raw" \
  repos/cloudflare/api-schemas/contents/openapi.json > "$D/openapi.json"
gh api repos/cloudflare/api-schemas/commits/HEAD --jq .sha   # the spec revision measured
go run ./cmd/morphic validate "$D/openapi.json"              # what the compiler already reports
```

Count a construct with `jq` or a short script over the file and quote the command. The first rule
that applies sets the tier:

1. **Housekeeping**: a refactor, cleanup, test, CI or docs task that changes no lowering,
   validation, IR or emit-contract behaviour. **P4**, unless it states which higher-tier work it
   unblocks.
2. **Corrupt-spec guard**: guards, validates or warns against a corrupt spec, or hardens against
   hostile input (crashes, exhaustion or private-address requests on adversarial documents).
   **P4**. A valid construct wrongly refused or mis-lowered is a defect and continues below.
3. **Off the golden spec**: it does not touch it (a construct it never uses, another source
   format, a later OpenAPI version, webhooks, external references, overlays). **P3** if it is
   valid-spec functionality or design that a later emitter, format or version needs, **P4** if it is
   cosmetic, tooling or docs polish. Never higher, however architectural.
4. **Blocks the first emitter**: **P0**. Without it no usable emitter can be built or run on the
   golden spec: the compiler crashes on or refuses a construct the spec uses, the IR has no home
   for a fact a model, operation, request or response needs, or the emitter contract leaves
   undefined something every emitter implements on its first model or operation. The emitter need
   not be perfect, so a gap whose workaround costs only polish (naming, ergonomics, detail) is P1.
5. **Direct impact**: it changes what the golden spec compiles or emits. **P1**. Examples: wrong,
   lossy or missing IR for a valid construct it uses (a fact a consumer needs, kept verbatim in
   `Unmodeled` instead of typed); a valid construct it uses refused; an architecture or design
   decision its output depends on (IR shape, pipeline and package boundaries, the emitter-plan
   contract).
6. **Indirect impact**: it touches the golden spec only indirectly (diagnostic volume or wording on
   it, internal cost, an effect reached only through emitter policy). **P2**.

- **Name the rule and show the probe** in the Priority line. A tier above P4 with no command behind
  it is a guess.
- **Re-rank on new evidence.** When a fix part-addresses an issue, or a new golden-spec revision
  changes what a probe returns, re-run the probe and edit the label in place with a comment saying
  what changed.
- **A tracking issue** states the end goal and the ordered plan. Each step is its own issue with the
  blocked-by relations set on the issues, and the tracker takes the tier of its highest open step.

### Labels

One `type:` label (`gh label list` gives the vocabulary and what each means), plus `performance`,
`security` or `dependencies` when they apply. Read the body from a file so quoting cannot alter it:

```bash
gh issue create --title "<title>" --body-file draft.md --label type:bug --label priority:P1
```

To audit a tier, read each issue's own `labels`: filtering the REST issues endpoint by label
returns stale matches after a label is recreated.
