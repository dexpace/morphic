# Contract

The angle that reads the change from outside, as each consumer of what Morphic emits meets it: the
IR document's shape and meaning, `IRVersion`, the names emitters render, diagnostic codes and
severities, the CLI's flags, output and exit codes, and the exported Go API. Each changes on purpose
or not at all. It loads `morphic-changing-the-ir` and `morphic-shipping-prs`.

1. A change to the IR's shape or meaning bumps `IRVersion` by `docs/ir/ir-version.md`, with its
   entry in `docs/ir/version-history.md`, and claims a version no open PR holds
   (`.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#version-collisions`). So
   does a change to a name an emitter renders, which `TestIRVersion_PinsTheNames` holds.
2. A field holding a new value, or a new field, is read by each of its consumers
   (`.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#widened-field-values`,
   `.claude/skills/morphic-changing-the-ir/references/ir-lessons.md#widened-field-consumers`).
3. Each diagnostic code the change adds, removes or re-ranks is listed under its rule in
   `docs/compilers/openapi.md`, each new site of an existing code is in the code's doc
   (`.claude/skills/morphic-openapi-compiler/references/openapi-lessons.md#diag-code-sites`), and
   each newly refused spelling runs on `main` (`references/contract.md#refusal-regression`).
4. Each exit code, flag and output stream the change alters is diffed on both builds
   (`.claude/skills/morphic-verifying-changes/references/differentials.md#argv-differential`).
5. A removed or changed exported identifier makes the change breaking
   (`.claude/skills/morphic-shipping-prs/references/commits-and-prs.md#exported-api-change-needs-bang`),
   and every break is stated in the PR's Breaking section
   (`references/correctness.md#better-than-main`).
6. A golden or census line the change rewrites, `testdata/unwitnessed.golden.txt` included, is a
   change of output to explain, not only to regenerate.

The matrix is each surface, with its diff on both builds and where the PR states it.

## A refusal test can enshrine a regression
`refusal-regression`

- **Rule.** Run every spelling a change newly refuses through a `main` build. Exit 0 becoming exit 1
  is a regression unless the PR says it is deliberate.
- **Why.** An over-broad refusal was pinned by the PR's own unit test and passed several rounds
  (#763).
