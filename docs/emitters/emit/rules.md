# Rules the emit layer holds to

The [template guards](templates.md#two-guards-on-the-boilerplate-templates) say what a template may
contain. These rules hold for the emit layer as a whole, the printer and the engine's write
included, because each is easy to lose once code exists and none shows in a single run's output.
T-25 gives each a test.

- **No mutable model.** A template receives a value built for it: scalars, and lists and structs of
  scalars, with no pointer, map, function or method anywhere in it. `text/template` calls a method
  of its data, and one with a pointer receiver writes through it, so read-only has to be the value's
  shape rather than a convention. A render can therefore change neither the IR, the plan, the target
  AST nor another artifact.
- **No re-rendering.** Rendered output is never parsed as a template again, so a value from the
  document that looks like template syntax comes out verbatim. The printer escapes a value for what
  it writes into, since a target's own syntax re-reads text as a template does: a Kotlin `$` in a
  string would interpolate, and a `*/` in a description would end a KDoc comment.
- **Hermetic functions.** A template function reads its arguments and nothing else: no environment,
  clock, network or filesystem. The function map is a literal list, and the lint refuses `call`,
  which would run any function a value held.
- **Deterministic scheduling.** Emission may run in parallel, with bounded workers, over a queue
  built in the plan's key order, each result stored at its place in that queue, so the order work
  finishes in changes nothing. A target returns its artifacts sorted by `Path`.
- **Locale-independent ordering.** Every sort compares bytes, which for UTF-8 is code-point order,
  as Go's `slices.Sort` on strings does. Nothing imports a collation, and `LC_ALL` changes no
  output.
- **One artifact per path.** The engine refuses a run in which two artifacts of a target share a
  path, as an error `engine/duplicate-artifact-path` naming both `Origins`, before it writes any.
  Paths are compared by the path fold below, since a file system that ignores case keeps only one of
  `Foo.go` and `foo.go`. The refusal is a backstop: a target holds the names it writes as paths
  apart by the same fold, or writes two that fold alike to one path
  ([collisions](../refine/naming.md#collisions-and-the-names-a-generation-shipped)), so two spec
  names that fold alike never reach it.
- **The path fold.** Two paths are one where their folds are equal, and a string's fold is its
  canonical decomposition (NFD) with each character upper-cased by its simple mapping, then folded
  by `golang.org/x/text/cases.Fold`, Unicode's full case folding. It is to join every pair a file
  system Morphic writes to makes one path, and each step is there for a measurement, on APFS (macOS
  26.5.2, Go 1.27.3, `golang.org/x/text` 0.43.0, Unicode 17.0.0), of every spelling of each assigned
  character (the character, its NFC, NFD, NFKC, NFKD, upper, lower, title and folded forms) behind
  an `a`, 173,237 names that APFS made 158,204 files, and of 209,214 names of one to four characters
  drawn from those that case folding, composition or mark order acts on, which it made 183,414. The
  fold parted no two names APFS made one file in either, while joining some it kept apart, such as
  `aı` and `ai`, which costs a name a suffix where folding less would lose a file. Decomposing the
  whole name comes first because APFS joins a composed `é` with a decomposed one, and two marks in
  either order: decomposing each character alone, which reorders no marks, split the names of one
  file 832 times, `q` with U+0323 and U+0307 against them reversed among them. Upper-casing comes
  before folding because `cases.Fold` alone, Unicode's canonical caseless match over these tables,
  split them 86 times in the first and 687 in the second, each Cherokee letter against its other
  case among them, as that function maps each to the other, and a letter newer than its tables; Go's
  own tables upper-case both alike first. A final NFD, as the canonical caseless match ends with,
  changed no answer in either, so it is not taken, and a fold of ASCII letters alone split them
  14,692 and 23,554 times. `strings.EqualFold` parts `Straße` from `Strasse` and a composed `é` from
  a decomposed one, which APFS makes one path (#1729). That the fold joins every pair NTFS makes one
  path is an assumption, not a measurement: NTFS compares names through an upper-case table each
  volume stores, which no run here read, and the same two sweeps on an NTFS volume would settle it.
  A file system that tells case apart joins fewer. Unicode's stability policies keep the case
  folding and decomposition of a string of assigned characters across its versions, so a toolchain
  moving Unicode refolds only a name holding a character the older one left unassigned. So the fold
  reads the tables Morphic is built with, Go's `unicode` and those `golang.org/x/text` picks for the
  Go release, where casing reads a named version's ([target options](../contract/options.md)): each
  of the 149,249 characters the tables of Go 1.26.3 hold, Unicode 15.0.0's, folds alike built with
  it and with Go 1.27.3, which holds Unicode 17.0.0, though `unicode.ToUpper` differs between them
  at 55 code points, and a character the older left unassigned was no letter to the word grammar
  either, so a name holding one changes its words with the release whatever the fold reads. T-52
  holds it.
- **A segment every file system writes.** Each segment of an artifact's path, a snapshot label's
  directory among them, is at most 255 bytes of UTF-8, the least any file system takes: ext4
  documents 255 bytes, and APFS, as NTFS documents, 255 UTF-16 units, which on macOS 26.5.2 took 255
  `日`, 765 bytes, and refused 256 `a`. No segment is a Windows device name up to its first dot, in
  any letter case: `con`, `prn`, `aux`, `nul`, `conin$`, `conout$`, or `com` or `lpt` followed by a
  digit or by `¹`, `²` or `³`, which covers what Windows documents and what Go refuses. `go build`
  builds a module holding `aux.go`, but `golang.org/x/mod` 0.42.0 refused to zip it (`malformed file
  path "aux.go": "aux" disallowed as path element component on Windows`), and a module the proxy
  cannot zip is one no caller downloads. No segment holds a character Windows refuses in a name,
  `<`, `>`, `:`, `"`, `\`, `|`, `?`, `*` or one below U+0020, or ends in `.` or a space. A path
  breaking one is an error `engine/artifact-path-unportable` naming the artifact's `Origins` and the
  segment, before any write. This is a backstop as well: each target renders the names it writes as
  paths so that no spec name reaches it, and a snapshot label, which the engine takes as the
  document writes it, is held to these rules before any run that writes it as a directory ([contract
  rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce),
  [naming](../refine/naming.md#a-name-too-long-for-a-path-is-shortened),
  [Go](../targets/go/naming.md#file-names), [Kotlin](../targets/kotlin/naming.md#reserved-names),
  [docs](../docs-and-mock.md#routes-and-links)). T-25 holds it.
- **Formatter failures.** A formatter that fails is the located, hard diagnostic of the [format
  pass](templates.md#two-guards-on-the-boilerplate-templates), and its artifact is not written:
  `Format` has no path that returns its input. No artifact reads the build that made it — a
  timestamp, a VCS stamp, an absolute path — so a build's flags change no byte. What a build can
  change is `go/format`, which is compiled into Morphic and which a Go release may revise, so moving
  the toolchain a release is built with is a change to the output, which T-6's goldens show.
  Kotlin's formatter runs outside `Generate`, and its printer lays out what it checks ([Kotlin
  formatting](../targets/kotlin/overview.md#formatting)).
- **Nothing written, something said.** A target that returns no artifact reports why, as a warning
  `emitters/<target>/no-artifacts` naming the cause, such as a `Scope` that matches nothing or a
  document with no type or operation, so an empty directory never reads as success.
- **Contained writes.** The engine checks every `Path` before the first write: forward-slashed,
  `filepath.IsLocal` once converted, and naming a file, or the run stops with an error
  `engine/artifact-outside-root` and writes nothing. It then writes through an `os.Root` opened at
  the output root, which refuses what only the disk shows: a symlink in the tree that leaves it. A
  string prefix is no check, as `out-evil` starts with `out`, and a lexical check alone passes the
  symlink.
