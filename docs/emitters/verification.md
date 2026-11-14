# Verifying a generation

The mandatory tests pin bytes and behavior ([mandatory tests](testing/mandatory-tests.md)), and a
golden that does not compile is a golden nonetheless. This document says how a generation itself is
checked: what its target could not lower, what it changed since the last one, whether its output
builds, and whether its manifest says what the output holds. Each finding is a coded diagnostic,
and the engine decides which of them fail the run (INV5, architecture.md §4).

## What a target cannot lower

A construct a target cannot lower draws a diagnostic of a code of its own from `Generate`, never a
silent fallback (T-7, [what M3 lowers](milestones.md#what-m3-lowers)): a binding
(`emitters/<target>/unsupported-binding`), a constraint its runtime cannot check
(`unchecked-constraint`), an enum member its constants cannot hold (`unrepresentable-enum-member`),
an encoding its type table has no row for (`unmapped-encoding`). `Generate` writes nothing, so the
engine holds every such finding before a file exists. It rates a target's findings as it rates any
stage's and writes that target's artifacts only where the run has not failed once the target has
reported, so a caller who wants no SDK that degrades a construct fails the run on that construct's
code, and one who accepts it rates the code down, everywhere or at one place (architecture.md §4).

A target declaring up front the rows of the [capability matrix](../ir-spec-matrix.md) it honors,
for the engine to compare with the rows a document exercises before `Generate` runs, was weighed
and not taken:

- **A row is not what a target declines.** A row is a capability of a source format, and a target
  declines one construct: one pattern its regular expressions cannot check, one member past its
  row's range, one binding's system. Two constructs a target may well decline, a cookie parameter
  and a list of files in a multipart body, have no row, and one IR fact can sit under two: an
  OpenAPI `allOf` beside properties, which witnesses the `intersection` row, lowers to
  `Model.Mixins`, the fact the `mixins` row names.
- **What a target honors is not constant.** An enum member a row cannot hold is lowered once a
  configuration maps its kind ([a member on the wire](refine/enums.md#a-member-on-the-wire)), and
  an encoding once the type table gains its row, while `Capabilities` is constant for the target
  ([the registry](contract/registry.md)). A list of rows would be a second statement of what the
  codes above already say at the construct, and the two could disagree.
- **One code cannot be rated per row.** A triage rule rates a code, so a single code for every
  unhonored row would leave a caller unable to accept one row and refuse another.
- **Nothing is gained before a write.** `Generate` is pure, so "before generation" and "before a
  file is written" differ only by the time `Generate` takes.

**A target's support table is derived, not declared.** Each format's conformance corpus names the
specs that witness every matrix row its format can express
(`testdata/conformance/<format>/matrix.md`), so running a target over a row's witnesses says what
it does with that row: the codes it draws there, or none. A test generates that table for each
target and holds a committed copy to it, as the census holds `testdata/unwitnessed.golden.txt`, so
what a target is documented to support is what it does (T-72). A row no committed spec witnesses
has no entry, which is the scope the first emitters lower ([what M3
lowers](milestones.md#what-m3-lowers)).

## What a regeneration changed

A generator changes between releases, and a user regenerating needs to know whether their SDK
changed, what the change does to its callers, and why it happened. The engine already holds the
answer to each:

- **Whether** is the manifest's file digests ([what the manifest
  records](write-integrate.md#what-the-manifest-records)). A release of Morphic that changes nothing
  an SDK uses changes none of its files, so that SDK has not changed, whatever else moved.
- **What it does to callers** is the surface's change records and the level they give
  ([surface verification](surface-verification.md#the-level-a-regeneration-reports)).
- **Why** is which of the manifest's `Inputs` moved: `Doc`, the API itself; `Config`, the owner's
  configuration; `Emitter`, Morphic, whose release notes cover the two versions the manifests name;
  and beside them `Options`, the defaults the owner moved to ([target
  options](contract/options.md)).

A version on each refine step and boilerplate template, recorded by the manifest for the steps a
generation used, so that release notes could name the steps that moved, was weighed and not taken:

- **Nothing would hold it.** A version bumped by hand names no change its author forgot to bump for,
  which is why the plan carries a digest and no revision ([the plan's
  version](plan/overview.md#the-plans-version)). T-10's trace lists step names, so a step whose
  output changes at an unmoved version fails no test that names the version.
- **It would not narrow.** `Run` runs every step of a target's list on every document ([the
  refiner contract](refine/pipeline.md)), and templates render into every SDK, so the steps an SDK
  used are its target's whole list. The file digests already narrow a change to the SDKs it
  reaches.
- **Steps are not all that shapes a file.** The plan, the derived facts, the printer and the
  formatter each change bytes and are no step's, so the steps that moved would not account for what
  changed.

## The output builds

`Generate` performs no I/O, so building what it returned is an engine stage of its own, the build
check. It runs after the writer, in the output root as the write left it, and only for a run that
asks for it (`RunOptions.Build`). Each target declares how its output is checked, in
`Capabilities.Build`:

```go
type Build struct {
    Toolchains []Toolchain   // what the checks need installed
    Checks     []Check       // run in order, in the output root
}

type Toolchain struct {
    Name    string           // "go", "java", "gradle"
    Probe   []string         // the version probe, such as ["go", "version"]
    Version string           // a pattern capturing the version from the probe's output
    Minimum string           // the oldest version the generated code builds with
    Maximum string           // the newest, "" where none is known
    Install string           // guidance shown when it is missing or out of range
}

type Check struct {
    Name    string           // "resolve", "build", "lint", "test"
    Command []string         // such as ["go", "vet", "./..."]
    Needs   []string         // the toolchains it runs on
    Setup   bool             // fetches what the others need, as resolving dependencies does
    Timeout time.Duration    // required; the engine ends the check at it
}
```

- **The tree as it landed.** The check builds what the user builds: the generation merged with
  their regions, their own files and the files written once ([write /
  integrate](write-integrate.md)), where it lies, since an SDK written into a live repository can
  be a package of a module, or a project of a Gradle build, that only resolves there. Building the
  bare generation in a directory of its own would pass an SDK a kept region breaks and fail one
  whose build is its parent's. So a failing check does not undo the write: it fails the run, and the
  files stay for their owner to read.
- **Asked for, never the default.** The check starts the target's toolchain on code generated from
  the spec, a build script and tests among it, with the caller's privileges, environment and
  network, which resolving dependencies needs. A spec is untrusted input
  ([security policy](../../SECURITY.md)), so it runs where a caller would run the SDK's own build
  anyway, and nowhere a caller did not ask.
- **Bounded.** Each check ends at its `Timeout`, and the engine keeps no more than a fixed size of
  what a check prints, saying when it cut it. A check ended at its timeout has failed.
- **A check that could not run is not one that passed.** A toolchain missing or outside its range
  is reported with its install guidance as a warning `emitters/<target>/check-skipped`, and every
  check needing it is skipped and named there. A `Setup` check that fails, as resolving
  dependencies does with no network, is reported under the same code, and the checks after it are
  skipped: what it could not fetch says nothing of the output.
- **A failure maps to its entity.** A target declaring checks implements `Checker` ([the
  contract](contract/emitter.md)), a pure function from what a check printed to its messages, each
  with the file and line its tool gave, since one build prints its compiler's lines and its build
  tool's and only the target knows either form. The engine maps a message's line through
  `Artifact.Spans` to the innermost declaration holding it, and reports an error
  `emitters/<target>/check-failed` carrying that entity's provenance. A file's `Origins` would not
  do: they list every entity of the file, which for one of Go's units is every model in it. A
  message on a line no span holds, a package clause or an import, or in a file with none, as the
  runtime's are, is reported at the file, and one with no position at the target, naming the check.
  None is dropped.
- **Samples are built by the same checks.** A target that returns samples
  ([docs and mocks](docs-and-mock.md#samples-come-from-the-sdk-emitters)) returns, among its test
  artifacts, a file holding each sample of the snapshot it writes, its `Code` as returned inside
  what the language needs around it to compile, so a sample the docs show is one that builds
  against the SDK it documents. Two kinds are not built: a sample in a language the host's
  toolchains do not build, as a Multiplatform target's Swift is off macOS, which is skipped and said
  to be, and a sample of another snapshot, whose calls the written SDK may not hold.
- **CI runs the check over every golden output** on the gate's host, so a golden that does not
  build fails the change that produced it (T-73). The gate's runners hold no Apple toolchain, so a
  Multiplatform golden's framework and Swift samples are built on macOS whenever a golden of that
  target is rewritten, as T-52's sweeps are run.

The check lands with the first targets (M3a), since from then a golden either builds or fails the
gate ([milestones](milestones.md)).

## The manifest says what the output holds

A manifest that is incomplete, or that names a symbol the built output lacks, is worse than none:
write/integrate, surface verification and every consumer mapping operations to SDK calls trust it.
It is built from `Origin` rather than inferred from source ([write /
integrate](write-integrate.md)), so it is checked exactly, with no tolerance, each miss an error
diagnostic of the target's, since the manifest is the target's to build:

- **Coverage** (`emitters/<target>/manifest-mismatch`). Every operation the run's scope selects has
  an entry, or a diagnostic of the target's at that operation saying why it has none. Every entry
  names an entity of the document, by the ID before its key's derivation. Every declaration the
  printer wrote under an `Origin`, which `Artifact.Spans` lists, has an entry naming its file, and
  every entry the run wrote names such a declaration. No two entries of one `Scope` share a
  `Symbol`. Which types take a declaration is each target's lowering, an alias collapsed, a
  container lowered in place, a model declared once per shape, so no count of the document's types
  is expected: a type something references and nothing declares fails the build check above.
- **Resolution** (`emitters/<target>/symbol-unresolved`). The target's extractor reads the tree the
  writer left ([surface verification](surface-verification.md)), and every record of the
  generation's own `Surface` must be among what it reads, under the symbol the manifest gives its
  entity. It is that machinery's smallest use, and finds what coverage cannot: a collision rename
  that never reached the manifest, a kept region shadowing a generated symbol. A symbol the surface
  leaves out, one generated code alone calls, is held by the build instead, and a target with no
  extractor (`Capabilities.SurfaceVerify` unset) has the check skipped and said to be, as a missing
  toolchain's are.

Both land with the manifest (M4), and T-74 holds them.
