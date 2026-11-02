# Verifying a generation

The mandatory tests pin bytes and behavior ([mandatory tests](testing/mandatory-tests.md)), and a
golden that does not compile is a golden nonetheless. So the engine checks a generation at three
points: before it, that each target honors what the document uses; while it runs, which versioned
steps shaped it; and after it, that the output builds and the manifest says what the output holds.
Each finding is a coded diagnostic whose severity the engine decides (INV5), so a check never fails
a run the caller did not ask it to fail.

## Before: what a target honors

A target that does not implement pagination, long-running operations, open enums, event streams,
file arrays in multipart or cookie parameters finds out construct by construct inside `Generate`,
and the user only after generating. So the engine compares, before generation, what the document
uses with what the target declares:

- **What the document uses** is `irfacts.Rows`, derived on demand and never stored, as every
  `irfacts` fact is ([derived facts](../ir/derived-facts.md)): each row of the [capability
  matrix](../ir-spec-matrix.md) the document exercises, keyed by the row's stable key, with the
  locations that exercise it. A test holds the detectors to the matrix's keys, as the conformance
  corpus is held to them, so a row added to the matrix has a detector or a stated reason for none.
- **What the target honors** is `Capabilities.Rows` ([the registry](contract/registry.md)), the
  keys it lowers. A row it degrades with a code (T-7) is not honored.
- **The difference** is reported per target and per row, a warning
  `emitters/<target>/unhonored-row` naming the row and its first locations, before `Generate`
  runs. The same two sets give a target's support matrix for its docs, generated rather than kept.

## During: steps carry versions

A generator changes between releases, and a user regenerating needs to know whether their SDK
changed, why, and what that does to their version number. A digest of the emitter says that
something changed and moves every SDK, whether or not the change touched anything it uses. So each
refine step and each boilerplate template carries a version, an integer bumped whenever what it
produces changes for some input, held by the step-trace golden as `plan.Revision` is held by the
plan's ([the plan's version](plan/overview.md#the-plans-version), T-10):

- the step trace of a run says which steps fired, and the manifest records each one's version
  ([what the manifest records](write-integrate.md#what-the-manifest-records));
- a regeneration's release notes are the changelog entries of the steps it used whose versions
  moved, and a moved step that changed an artifact sets at least a patch bump
  ([surface verification](surface-verification.md#the-version-a-regeneration-takes));
- a construct no step lowers is a T-7 diagnostic, as it always was.

## After: the output builds

`Generate` performs no I/O, so building what it returned is an engine stage of its own, run over
the generated files in a staging directory before the writer moves them into place. A generation
that fails it therefore never lands in the user's tree unless the caller lets that failure through.
Each target declares how its output is checked, in `Capabilities.Verify`:

```go
type Verify struct {
    Toolchains []Toolchain   // what the checks need installed
    Checks     []Check       // run in order, in the staged output root
}

type Toolchain struct {
    Name    string           // "go", "gradle"
    Probe   []string         // the version probe, such as ["go", "version"]
    Version string           // a pattern capturing the version from the probe's output
    Minimum string           // the oldest version the generated code supports
    Install string           // guidance shown when it is missing or older
}

type Check struct {
    Name    string           // "build", "lint", "test"
    Command []string         // such as ["go", "vet", "./..."]
    Needs   []string         // the toolchains it runs on
    Reports string           // how its messages give a position: "go", "kotlinc", "gradle"
}
```

- **A toolchain missing or too old** is reported with its install guidance, and that target's checks
  are skipped and said to be: a check that could not run never reads as one that passed.
- **A failure maps to its entity.** The printer records which lines each top-level declaration
  spans, `Artifact.Spans`, so a compiler message's file and line resolve to the entity whose
  declaration holds it, and the diagnostic carries that entity's IR provenance, not just a path in
  the output.
- **Samples are built too.** The usage samples each SDK target returns
  ([docs and mocks](docs-and-mock.md#samples-come-from-the-sdk-emitters)) are compiled in a snippet
  project the stage writes beside the SDK and never ships, so a sample the docs show is one that
  builds against the SDK it documents.
- **CI runs the stage over every golden output**, so a golden that does not build fails the change
  that produced it.

## After: the manifest says what the output holds

A manifest that is incomplete, or that names a symbol the built output lacks, is worse than none:
write/integrate, surface verification and every consumer mapping operations to SDK calls trust it.
It is built from `Origin` rather than inferred from source, so it is checked exactly:

- **Coverage.** Every `OpID` and `TypeID` the plan says a target emits has exactly one manifest
  entry, and every entry names an entity of the document. A miss or a duplicate is an error
  diagnostic of the target's, since the manifest is the target's to build, with no tolerance.
- **Resolution.** After the build, every symbol the manifest names exists in the built output. The
  check is the per-target extractor that [surface verification](surface-verification.md) reads an
  SDK through, so it is that machinery's smallest use. A symbol missing, as a collision rename that
  never reached the manifest or a keep region shadowing a generated symbol leaves one, is an error
  diagnostic naming the entry.
