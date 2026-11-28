# Rules the emit layer holds to

The [template guards](templates.md#two-guards-on-the-boilerplate-templates) say what a template may
contain. These rules hold for the emit layer as a whole, the printer and the engine's write
included, because each is easy to lose once code exists and none shows in a single run's output.
T-25 gives each a test.

A rule over the paths a target returns is one only a bug in the target can break, so it is a duty
of the contract and no finding. The checked call refuses a generation that breaks one, with a Go
error wrapping `emitters.ErrContract` that names the target, the path and the rule ([the
contract](../contract/emitter.md#the-checked-calls)). It draws no diagnostic: a diagnostic is a
finding about the user's document, which a threshold or a triage rule may accept, and no document
causes two artifacts on one path. Codes `engine/duplicate-artifact-path`,
`engine/artifact-path-unportable` and `engine/artifact-outside-root` stood here before, and under
them a run at the `none` threshold, or one whose triage rule rated a code down, would have written
the tree the rule exists to prevent.

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
  finishes in changes nothing. A target returns its artifacts sorted by `Path`, which the checked
  call holds.
- **Locale-independent ordering.** Every sort compares bytes, which for UTF-8 is code-point order,
  as Go's `slices.Sort` on strings does. Nothing imports a collation, and `LC_ALL` changes no
  output.
- **One artifact per path.** Two artifacts of a target that share a path are a breach. Paths are
  compared by the path fold below, since a file system that ignores case keeps only one of `Foo.go`
  and `foo.go`. The refusal is a backstop: a target holds the names it writes as paths apart by the
  same fold, or writes two that fold alike to one path
  ([collisions](../refine/naming.md#collisions-and-the-names-a-generation-shipped)), so two spec
  names that fold alike never reach it.
- **No path is a directory of another.** A file cannot stand where a directory must, so an
  artifact `a` beside one at `a/b` is a breach, and by the fold so is `Models` beside
  `models/x.go`, which a file system that ignores case makes the same pair.
- **One spelling per directory.** Of the directories the paths pass through, any two that fold
  alike are spelled alike, so `Models/a.go` beside `models/b.go` is a breach, as it is beside
  `models/sub/b.go`. A file system that ignores case keeps both files under the spelling written
  first, where the manifest lists the other, and one that does not holds two directories no other
  volume can. A directory standing on disk under another spelling is the writer's to judge,
  where a held path passes through it
  ([write / integrate](../write-integrate.md#a-file-is-judged-by-its-digest)).
- **No path is the manifest's own.** The engine keeps the generation's manifest at the output root,
  as `morphic.manifest.json`, and while it writes, `morphic.manifest.pending.json` beside it, written
  through `.morphic.manifest.pending.json.tmp` ([write
  / integrate](../write-integrate.md#every-write-is-planned-before-the-verdict)). An artifact at any
  of the three paths, or under one as a directory, compared by the fold, is a breach: the manifest's
  own writes would meet it, after the run's verdict. A manifest or pending manifest listing such a
  path is refused as it is decoded, by the same comparison. A listed path is held to no other
  segment rule: a pending manifest lists what stood on disk under another spelling of a held path,
  which may be longer than a segment the engine writes, and a manifest a build with newer Unicode
  tables wrote may list a character this build's tables leave unassigned, which `Portable` would
  refuse. No valid document leads a target there: a snapshot label takes a directory other than any
  of the three names ([contract
  rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)), and the docs
  route plan refuses a page routed there as its own finding
  ([docs](../docs-and-mock.md#routes-and-links)).
- **The path fold.** Two paths are one where their folds are equal, and a string's fold
  (`pathfold.Fold`, the one implementation the engine, the contract and naming compare by) is its
  canonical decomposition (NFD) with each character upper-cased by its simple mapping, then folded
  by `golang.org/x/text/cases.Fold`, Unicode's full case folding. It is to join every pair a file
  system Morphic writes to makes one path, and each step is there for a measurement, on APFS (macOS
  26.5.2, Go 1.27.4, `golang.org/x/text` 0.44.0, Unicode 17.0.0), of every spelling of each assigned
  character but a private-use one (the character, its NFC, NFD, NFKC, NFKD, full upper, lower and
  title, and folded forms) behind an `a`, 173,237 names that APFS made 158,204 files, and of the
  spellings of 40,000 names of one to four characters a seeded generator draws from the 15,446 that
  case folding, composition or mark order acts on (each name, its NFC, NFD, full upper, lower and
  folded forms, its marks reversed and each character decomposed alone), 93,823 names, which it made
  38,588. The fold parted no two names APFS made one file in either, while joining one pair it kept
  apart, `aı` and `ai`, which costs a name a suffix where folding less would lose a file.
  Decomposing the whole name comes first because APFS joins a composed `é` with a decomposed one,
  and two marks in either order: decomposing each character alone, which reorders no marks, split
  the names of one file 1,293 times in the second, as it parts `q` with U+0323 and U+0307 from the
  two reversed. Upper-casing comes before folding because `cases.Fold` alone, Unicode's canonical
  caseless match over these tables, split them 86 times in the first and 1,252 in the second, each
  Cherokee letter against its other case among them, as that function maps each to the other; Go's
  own tables upper-case both alike first. A final NFD, as the canonical caseless match ends with,
  changed no answer in either, so it is not taken, and a fold of ASCII letters alone split them
  14,694 and 55,123 times. The decomposition is the whole one: `norm.NFD` writes Unicode's
  stream-safe form, ordering no mark across the U+034F it inserts after 30 characters in a row that
  it counts as non-starters, every combining mark among them, while APFS orders a run of 31, the
  longest it writes. So the fold decomposes a name `norm.NFD` cuts a character at a time and orders
  each run of marks by combining class; over a third sweep, 1,000 seeded draws of 28 to 31 marks in
  a row behind a starter, each beside itself with each two neighbouring marks swapped, 29,462 names,
  which APFS made 11,324 files, it parted none APFS made one file and joined none it kept apart,
  where `norm.NFD` alone parted 164, each at 31. `strings.EqualFold` parts `Straße` from `Strasse`
  and a composed `é` from a decomposed one, which APFS makes one path (#1729). That the fold joins
  every pair NTFS makes one path is an assumption, not a measurement: NTFS compares names through an
  upper-case table each volume stores, which no run here read, and the same sweeps on an NTFS volume
  would settle it. A file system that tells case apart joins fewer. Unicode's stability policies
  keep the case folding and decomposition of a string of assigned characters across its versions, so
  a toolchain moving Unicode refolds only a name holding a character the older one left unassigned.
  So the fold reads the tables Morphic is built with, Go's `unicode` and those `golang.org/x/text`
  picks for the Go release, where casing reads a named version's ([target
  options](../contract/options.md)): each of the 149,249 characters the tables of Go 1.26.3 hold,
  Unicode 15.0.0's, each assigned but private use, which folds to itself, the surrogates, which no
  UTF-8 name holds, and NUL and `/`, which no file name holds, folds alike built with it and with Go
  1.27.3, which holds Unicode 17.0.0, though `unicode.ToUpper` differs between them at 55 code
  points, and a character the older left unassigned was no letter to the word grammar either, so a
  name holding one changes its words with the release whatever the fold reads. T-52 holds it.
- **A segment every file system writes.** Each segment of an artifact's path, a snapshot label's
  directory among them, is one `pathfold.Portable` admits. It is not empty, is valid UTF-8, which
  APFS refuses a name without, and is at most 255 bytes, the least any file system takes: ext4
  documents 255 bytes, and APFS, as NTFS documents, 255 UTF-16 units, which on macOS 26.5.2 took
  255 `日`, 765 bytes, and refused 256 `a`. No segment is a Windows device name up to its first dot,
  trailing spaces dropped, in any letter case: `con`, `prn`, `aux`, `nul`, `conin$`, `conout$`, or
  `com` or `lpt` followed by a digit or by `¹`, `²` or `³`, which covers what Windows documents and
  what Go refuses, so `aux.v1.go` and `nul .txt` are each one. `go build` builds a module holding
  `aux.go`, but `golang.org/x/mod` 0.42.0 refused to zip it (`malformed file path "aux.go": "aux"
  disallowed as path element component on Windows`), and a module the proxy cannot zip is one no
  caller downloads. No segment holds a character Windows refuses in a name, `<`, `>`, `:`, `"`, `/`,
  `\`, `|`, `?`, `*` or one below U+0020, or ends in `.` or a space. No segment holds a code point
  the tables Morphic is built with leave unassigned (general category `Cn`, the noncharacters among
  them), or more than 30 combining marks in a row, a mark being a character of non-zero canonical
  combining class and the count taken in the canonical decomposition, so a letter composed with a
  mark adds one. APFS (macOS 26.5.2) refused each of the 814,730 unassigned code points behind an
  `a` and wrote every other from U+0080, and refused 32 marks in a row where it wrote 31, so the
  rule refuses one length APFS writes; a run of characters of class zero it wrote at every length a
  segment holds, 85 conjoining vowels (U+1161) among them, and the rule admits it. It reads the
  building release's tables, as the fold does: a character newer than the tables of the system
  written to is admitted and fails there at its write, and one newer than Morphic's is refused until
  a release holds it. A path breaking one is a breach. This is a backstop as well: each target
  renders the names it writes as paths so that no spec name reaches it, and a snapshot label, which
  the engine takes as the document writes it, is held to these rules before any run that writes it
  as a directory, where breaking one is a finding about the document,
  `engine/snapshot-label-invalid` ([contract
  rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce),
  [naming](../refine/naming.md#a-name-too-long-for-a-path-is-shortened),
  [Go](../targets/go/naming.md#file-names), [Kotlin](../targets/kotlin/naming.md#reserved-names),
  [docs](../docs-and-mock.md#routes-and-links)). T-25 holds it.
- **A file name the writer can write.** A file's own name, its path's last segment, is at most
  235 bytes. The writer replaces a file through a temporary one beside it, named for it with a
  dot before and up to 19 digits after ([write /
  integrate](../write-integrate.md#every-write-is-planned-before-the-verdict)), which is 20 bytes
  more and a segment like any other. A longer name would generate and then fail at its write,
  after the run's verdict, and on every rerun, so the contract refuses it as a breach.
- **Formatter failures.** A formatter that fails is the located, hard diagnostic of the [format
  pass](templates.md#two-guards-on-the-boilerplate-templates), and its artifact is not written:
  `Format` has no path that returns its input. No artifact reads the build that made it — a
  timestamp, a VCS stamp, an absolute path — so a build's flags change no byte. What a build can
  change is `go/format`, which is compiled into Morphic and which a Go release may revise, so moving
  the toolchain a release is built with is a change to the output, which T-6's goldens show.
  Kotlin's formatter runs outside `Generate`, and its printer lays out what it checks ([Kotlin
  formatting](../targets/kotlin/overview.md#formatting)).
- **Nothing written, something said.** A target that returns no artifact reports why, as an error
  diagnostic where an error prevented emission (such as a formatter failure above) or as a warning
  `emitters/<target>/no-artifacts` naming the cause, such as a `Scope` that matches nothing or a
  document with no type or operation, so an empty directory never reads as success. A generation
  holding no artifact, no error diagnostic and no `no-artifacts` warning is a breach.
- **Contained writes.** Every `Path` is forward-slashed, clean (`path.Clean` leaves it as it is),
  local (`filepath.IsLocal` once converted) and names a file, which together leave no segment empty,
  `.` or `..`, or the generation is a breach and nothing of the run is written. `filepath.IsLocal`
  alone admits `a//b` and `a/./b` (Go 1.27.3), each a second spelling of `a/b` that the fold, which
  compares strings, does not join with it. The engine then reads and writes through an `os.Root`
  opened at the target's output root, which refuses what only the disk shows: a symlink in the tree
  that leaves it. A string prefix is no check, as `out-evil` starts with `out`, and a lexical check
  alone passes the symlink.
