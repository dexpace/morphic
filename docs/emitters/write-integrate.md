# Write / integrate

Regenerating into a directory its owner also writes in has two halves, and they land apart.

The **file-level** half lands with the first write, since a second generation into a directory is
only correct if the files the first wrote and this one does not are removed, and removing a file
needs the list of what was written: [one output root per target](#one-output-root-per-target), a
manifest of the files a generation wrote, [a file judged by its
digest](#a-file-is-judged-by-its-digest), and [every write planned before the run's
verdict](#every-write-is-planned-before-the-verdict).

The **entity-level** half is post-milestone (architecture.md §2.3; M4 of the [milestone
plan](milestones.md)): the entity → symbol map, the names a generation shipped, regions, merging,
blocks and staleness. The contract is designed so it bolts on with **no IR change** (INV9). The
`Artifact` fields returned from `Generate` and the manifest's entities are the seams; the
`Origin`-tagged AST makes the entities a byproduct of the emit walk.

- **Generation manifest keyed by IR stable IDs.** Records what the generation was built from,
  a **sorted** file list (deterministic diffs), and an **entity → generated-symbol map keyed by
  `TypeID`/`OperationID`/`PropertyID`** — assembled from the `Origin` on every emitted node. This is
  the biggest win of stable-ID keying: a rename of anything an ID is not built from is "same ID, new
  symbol" — a lookup, not oagen's structural rename-inference machinery — and one of what it is
  built from is carried by the correlation key each entry records where the key's own source holds,
  as an operation's route does when a path variable is renamed. A path or a component renamed
  changes its key too ([correlation keys](../ir/derived-facts.md#correlation-keys)), and nothing
  derived carries it ([open question 5](../ir/open-questions.md)): what was keyed by its old ID
  matches nothing, which the generation reports ([a key that matches
  nothing](#a-key-that-matches-nothing)). A symbol derived from an entity keys under it, as
  `User.ID#create` does ([`User`](targets/go/models.md#user-in-go)), a shape's declaration under its
  lifecycles and its patch ([TypePlan](plan/types.md#the-declaration-a-shape-takes)), a union's
  wrappers, fallback and inclusive fields among them. An ID keeps its source's text, a `#` included,
  so every key writes its entity's ID with `%` and `#` percent-encoded and a derivation follows the
  one `#` left: the `create` shape of a property `x`'s type keys apart from the type of a property
  `x#create` beside it, where the two joined as written were one key. So no two symbols share a key
  (T-60). Write/integrate and surface verification (architecture.md §2.3) both rest on it.
- **A digest gates pruning.** The writer deletes a file only where the manifest, or a pending one an
  interrupted run left, lists its path and its bytes are ones a generation wrote
  ([below](#a-file-is-judged-by-its-digest)), so it **never deletes a file its owner wrote or
  edited**. First adoption (no manifest, and no pending one) prunes nothing, writes a baseline,
  emits a notice. The generated-by line a target writes ([the
  contract](contract/emitter.md#the-generated-by-line)) tells a reader the file is generated, and
  gates one thing: overwriting a file no manifest lists.
- **Ignore regions & additive-only merge.** Two provenance markers — whole-file
  (`@morphic:ignore-file`) and region (`@morphic:keep <name>`/`:end`,
  [below](#regions-follow-their-entity)) — with append-only merge. The structural merge is
  **language-parameterised** by an adapter the target supplies
  ([capabilities](contract/registry.md#a-capability-an-interface-shows);
  [Go](targets/go/overview.md#merging-and-surface-extraction),
  [Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)). No adapter ⇒ skip, never
  clobber. A file renamed in letter case alone needs no temporary name: its old spelling is pruned
  before its new one is written ([below](#every-write-is-planned-before-the-verdict)).
- **Staleness check.** `(prior-manifest entities − current entities) ∩ files-on-disk` surfaces
  dead code additive writers cannot prune — cheap because both manifests are ID-keyed and the prior
  IR/plan snapshot is serializable (INV7). A prior entity counts as current where its entry is a
  current entity's prior entry ([the names a generation
  shipped](refine/naming.md#collisions-and-the-names-a-generation-shipped)), so an operation whose
  path variable was renamed is not listed as dead though its old ID is gone.

## One output root per target

Each target a project configures has an output root of its own ([the
project](contract/project.md)), and every path of its generation is under it. A run is refused,
before any target runs, where two targets' roots are one directory or one holds the other, compared
segment by segment under the path fold ([emit rules](emit/rules.md)), so `out` beside `Out` and
`out` beside `out/go` are each refused. A root shared by two targets would make each one's files
unlisted files in the other's way, and each one's manifest the other's.

## What the manifest records

A manifest holds the choices a later generation must honor and cannot recompute from the spec: the
names it shipped, the behavior it pinned, the files a user now owns. So it records more than hashes.
The engine composes it, since it alone knows what was written, and keeps it at the target's output
root as `morphic.manifest.json`:

```go
package manifest

type Manifest struct {
    Schema   int                 // this format's version; an older one is migrated forward
    Target   string              // the emitter's TargetKey
    Options  int                 // Configuration.Version, the behavior pinned for this SDK
                                 // (contract/options.md)
    Inputs   Inputs              // what the generation was built from
    Files    []File              // every file of the SDK, sorted by path, written by this run
                                 // or kept from the prior manifest (below)
    Entities map[string]Entry    // every entity the SDK declares and each symbol derived from
                                 // one, keyed by EntityID (contract/emitter.md)
    Keys     int                 // irfacts.CorrelationVersion, the derivation of every Key here
                                 // (ir/derived-facts.md#correlation-keys)
    Regions  []Region            // the hand-written regions found, sorted by entity, then name
}

// Generations is a cross-version target's manifest, in the place any target's lies: each run's
// Manifest by the label of its snapshot, "" for an unsliced document (contract/emitter.md).
type Generations struct {
    Schema    int                 // as Manifest.Schema, this format's version
    Snapshots map[string]Manifest // the last label's holds the files spanning versions
}

type Inputs struct {
    Doc     string               // the document's digest, which Plan.Version takes
                                 // (plan/overview.md)
    Plan    string               // Plan.Version
    Emitter string               // the Morphic version and the Go release that built it, from
                                 // its build information
    Config  string               // SHA-256 of the decoded options (Configuration.Canonical),
                                 // resolved policy and resolved hints, each as options.Canonical
                                 // writes it (contract/options.md)
    Handed  string               // SHA-256 of the snapshot label, samples and routes the engine
                                 // handed the run beside its document (contract/emitter.md), as
                                 // options.Canonical writes them; "" = none
}

type File struct {
    Path    string
    Digest  string               // SHA-256 over the bytes written, normalized (below), as
                                 // "sha256:" and its lowercase hex
    Origins []string             // entity IDs, as Artifact.Origins lists them
    Once    bool                 // its Artifact's SkipIfExists: written by the first generation
                                 // that finds it absent, the user's thereafter
}

type Entry struct {
    Key    string                // the entity's correlation key, which ir/irfacts derives
    Scope  string                // the scope its name was resolved in, as its target names the
                                 // scopes it resolves (refine/reserved-names.md)
    Base   string                // its rendered name before a collision was resolved
    Symbol string                // the name the generation gave it (refine/naming.md)
    Kind   string                // the target-AST node kind it was declared as
    File   string                // the path it was written to
}
```

- **What the first write records.** `Schema`, `Target`, `Options`, `Inputs` and `Files`, a file's
  `Path` and `Digest`, are written from the first generation on. `Entities`, `Keys`, `Regions`, a
  file's `Origins` and `Once`, `Inputs.Handed` and `Generations` each join with the code that fills
  and reads it: the entities and regions with M4, `Once` with the first scaffolding file, and
  `Handed` and `Generations` with the docs target. `Inputs.Config` digests the decoded options
  alone until policy and hints are resolved. A manifest is decoded strictly, so each member that
  joins is a new `Schema`, whose migration from the one before adds nothing: an older Morphic
  meeting it refuses the manifest as newer, where it would otherwise refuse a member it does not
  know and say nothing of why.
- **The document, not the spec text.** `Inputs.Doc` is the IR's digest, canonical JSON with
  `sources` and `diagnostics` left out. Reformatting a spec, or moving it to another path, changes
  the spec's bytes and where its reports point, and leaves the rest of the IR whole, so it is no new
  generation.
- **A file as written.** `Digest` covers the bytes after formatting, each keep region's text left
  out and its markers kept, with a byte-order mark stripped, line endings made `\n` and trailing
  newlines dropped, so an edit inside a region, a run of the formatter the generator ran and a
  checkout that converts line endings are not mistaken for an edit. A file the manifest lists whose
  bytes on disk, normalized alike, match neither a digest recorded for it nor what this run
  generates was edited outside its regions. The writer leaves it as it is, neither overwriting nor
  pruning it, reports it as a warning `engine/file-edited`, since nothing is lost, and keeps an
  entry for it in the new manifest ([below](#a-file-is-judged-by-its-digest)), so the next
  generation reports it again. An entity a rename moves out of it is written in its new place as any
  other, since its callers name it there, and its regions stay in the kept file, which still holds
  them, rather than being written twice. A file marked `@morphic:ignore-file` is its user's whole,
  as a file written once is, so it is never written, pruned or checked for an edit, and a file
  integrated as blocks is checked by its blocks alone ([below](#a-document-the-user-writes-around)).
- **Nothing to do.** A file whose bytes on disk are what this run generated is not written, and
  where no file is written or pruned, no pending manifest stands and the manifest the run would
  write is the one the root holds, its `Options` and every `Inputs` field included, nothing is
  written, so a regeneration that changes nothing touches nothing and leaves every modification
  time. A file written once or marked `@morphic:ignore-file` is set aside. `Emitter` is a version,
  not the hash of the executable the plan and IR caches key on, since a team's machines share the
  manifest and must agree on what it holds, and it holds the Go release beside Morphic's, since its
  `go/format` shapes Go's output ([emit rules](emit/rules.md)). The run generates whatever the
  manifest says: what is compared is what it generated, so a change being developed, whose version
  names no one tree, is never skipped.
- **A scoped run.** A run under a `Scope` writes what the scope selects, removes and prunes nothing
  outside it, a README block among them, and keeps the prior manifest's entry of every entity, file
  and region it leaves out, so its manifest is the whole SDK's: the next run's names, staleness
  check and no-op test read what was shipped, not what one run wrote.
- **One generation across snapshots.** A cross-version target is run on every version snapshot as
  one generation, each run under its label's directory ([the
  contract](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)). Its manifest is
  a `Generations`, which holds each snapshot run's `Manifest` under that snapshot's label; each
  run's `Prior` is its own label's, and the writer reads a path across all of them, so a file no run
  returns any longer, a dropped label's directory among them, is pruned as any other, and one that
  moves from one label's run to another's, as a file spanning versions does when another label
  becomes last, is neither pruned nor taken for a user's edit. Its manifest is a `Generations` even
  for an unsliced document, whose one label is `""`, and where a prior holds no entry for the last
  label, as when a document gains, loses or renames versions, that run takes the prior's entry
  holding the files spanning versions, the one entry listing a path outside its own label's
  directory, else its `""` one, so the names the site last shipped carry over. A prior is read as a
  `Generations` where it holds `Snapshots`, a field no `Manifest` has, else as a `Manifest`, which a
  target that becomes cross-version reads as its `""` entry and one that stops being so reads as the
  entry holding the spanning files.
- **Behavior.** `Options` is the version whose defaults this SDK keeps until its owner moves it
  ([target options](contract/options.md)). No refine step's version is recorded: what a
  regeneration changed is read off the digests, the surface and `Inputs`
  ([verification](verification.md#what-a-regeneration-changed)).
- **Checked.** A manifest is held to the output it describes, its coverage exactly and its symbols
  against the built tree ([verification](verification.md#the-manifest-says-what-the-output-holds)).
- **Written once.** A file a target generates for the user to own, such as a test to fill in, is an
  artifact with `SkipIfExists` ([the contract](contract/emitter.md)), recorded as `Once`: written by
  the first generation that finds it absent and never again, so one its user deleted stays deleted,
  never pruned, and never checked for an edit, since editing it is what it is for. It is matched to
  a later generation through its origins, each followed to the entity whose prior entry
  ([collisions](refine/naming.md#collisions-and-the-names-a-generation-shipped)) it is, so an entity
  renamed while its key holds, as a `Rename` hint renames one, is not written a second time under
  its new name.
- **Its own version.** Unlike the IR, a manifest holds choices that cannot be recomputed, so an
  older `Schema` is never refused: each version step has a migration, applied in order, and a
  manifest from a newer Morphic is refused as an error `engine/manifest-newer` and nothing is
  written, since its fields cannot be read and a generation run without them would rename what was
  shipped. A manifest whose `Target` is another target's is an error `engine/manifest-foreign`,
  and nothing is written: the root is another generation's. A manifest whose bytes are no manifest
  of any `Schema`, or a directory or a link at its path, is an error `engine/manifest-unreadable`
  naming the path, and nothing is written, since what it held cannot be read and a generation run
  without it would treat the root as a first one; a read the disk fails is a Go error, as any I/O
  is. A pending manifest states its `Schema` and `Target` as the manifest does
  ([below](#every-write-is-planned-before-the-verdict)) and is refused by each rule the same way,
  so a root where another target's first generation was cut short is refused, and none of that
  target's files is pruned.

## A file is judged by its digest

Before a run writes, the engine reads the output root and judges every path the generation holds
and every path the manifest lists. A path is *listed* where the manifest, or a pending manifest an
interrupted run left ([below](#every-write-is-planned-before-the-verdict)), records it. *Known* is
every digest either records for the path, and *fresh* the digest of this generation's artifact
there, where it has one.

| On disk at the path | The generation holds the path | It does not |
|---|---|---|
| nothing | write | drop the entry |
| a file whose digest is fresh | nothing to do | — |
| a file whose digest is known | write | prune |
| a file of any other digest, the path listed | keep, `engine/file-edited` | keep, `engine/file-edited` |
| a file of any other digest, the path unlisted, the marker present | write | — |
| a file of any other digest, the path unlisted, no marker | `engine/artifact-would-overwrite` | — |
| a directory | write where the run's prunes and directory removals leave it empty or gone, else `engine/artifact-would-overwrite` | drop the entry |
| a file where the path needs a directory | write where that file is pruned, else `engine/artifact-would-overwrite` | drop the entry |
| a symbolic link, at the path or at a directory above it | `engine/artifact-would-overwrite` | keep, `engine/file-edited` |

A path the generation does not hold and nothing lists is never read: it is its owner's. Planning
reads a path segment by segment without following a link (`os.Root.Lstat`), so a link is met as
itself, and it is its owner's, as no generation writes one: nothing is written or pruned through
it. `os.Root` refuses a link that leaves the root ([emit rules](emit/rules.md)) and follows one that
stays inside it, so a write through `models`, a link to `util`, would land in `util/` while the
manifest listed `models/`, and a prune there would remove a file the link's target holds. A kept
file's entry keeps the digest the manifest recorded for it, or, where only a pending manifest
lists the path, the least of the digests that one records, so the path stays listed and the next
generation judges it the same way.

- **The digest decides, with or without a marker.** A file is the generation's own where its bytes
  are ones a generation wrote there, which the manifest records, and its owner's where they are
  not. So a file that can hold no comment, a JSON file whose `Header` is `none`, is rewritten and
  pruned as any other, and a file carrying the generated-by line that its owner then edited is
  kept. Gating a prune on the line alone, as the generators this design was drawn from do
  ([reference learnings](../reference-learnings.md)), fails both ways: it can never prune a file
  with no comment syntax, and it prunes a marked file whatever its owner did to it.
- **The marker gates one thing: a file no manifest lists.** A first generation into a tree an
  older generation wrote, or one whose manifest was deleted, meets files it has no digest for. One
  carrying the marker where its artifact's `Header` places it ([the
  contract](contract/emitter.md#the-generated-by-line)) is rewritten; any other is an error
  `engine/artifact-would-overwrite`, and nothing is written. With the manifest gone, a file an
  earlier generation wrote and this one drops stays, since nothing lists it.
- **A first generation says so.** A root holding no manifest is a first generation, and the run
  reports an info `engine/first-generation`. Nothing is pruned there but what a pending manifest
  lists, as a first generation cut short leaves one.
- **Spellings a disk may not tell apart are judged together.** Where a listed path the generation
  drops folds as a path it holds, `Setup.kt` and then `SetUp.kt`, a volume that ignores case
  answers the new spelling with the old file, and a run cut short there leaves both spellings
  listed, whatever the next generation holds. So what is known of one listed spelling is known of
  every path that folds as it, whether the generation holds one of them or none, and a file that
  answers several of them is pruned once. Where the generation holds one and any of them holds a
  file of any other digest, or a directory the run's prunes and directory removals leave standing,
  what stands there is its owner's and the write is `engine/artifact-would-overwrite` on every
  volume: on one that ignores case the write would replace the file or meet the directory, and on
  one that does not it would replace the file at the new spelling, or leave beside an edited old
  one, or beside the directory, a second name no other volume can hold. Otherwise, where an old
  spelling holds a file, it is pruned and the new one written, whatever the new spelling answers,
  since the old file may be what answers; where none does, the new spelling is judged by the
  table. Where the generation holds none of them, each is judged by the table, so a file that is
  kept is kept under each spelling it answers: on a volume that ignores case a saved file two
  listed spellings name is reported `engine/file-edited` at each and keeps both entries, where a
  volume that tells case apart keeps one entry and drops the other.
- **A file or directory in a write's way is refused before the verdict.** A file that stays, its
  owner's or an edited one, or an unlisted subdirectory, even empty, under a directory a write needs
  as a file, or a file that stays where a write needs a directory, is
  `engine/artifact-would-overwrite`. A directory left empty at a write's path, already empty on disk
  or emptied by the run's prunes and dropped-path directory removals, is removed. The same holds
  across letter case for listed paths on every volume: where a kept edited listed path's folded path
  is a directory prefix of a held path (`Models` beside `models/user.go`), or a held path's folded
  path is a directory prefix of a kept edited listed path (`models` beside `Models/user.go`), that
  held path draws `engine/artifact-would-overwrite` on a volume that tells case apart as on one
  that does not, so no run writes a tree or manifest a volume that ignores case cannot hold.
- **A directory a write passes through keeps the spelling the generation gives it.** A directory or
  a file standing under another spelling of a segment a write passes through, `Models` for `models`,
  is `engine/artifact-would-overwrite` unless the run's prunes and dropped-path directory removals
  remove it before the write: a file the run prunes, or a directory holding only files pruned in
  this run or only directories emptied above listed paths the generation drops, at any depth. Under
  a directory left standing, a write on a volume that ignores case would land under the old
  spelling, and the manifest would list a path another volume lacks; beside a file, a volume that
  ignores case meets it where the directory must go, and one that does not holds two names no volume
  that ignores case can. Planning lists each directory a write passes through to find another
  spelling, so a file there costs no further read. A name a volume lists in another normalization, a
  composed `é` decomposed, is the same name where the volume answers the write's spelling with it
  (`os.SameFile` holds of the two), as APFS does; on a volume that keeps the two apart, as ext4
  stores a name's bytes, it is another spelling, refused as one, since the write would leave beside
  it a second directory no volume that joins them can hold. The rule is met at a write alone: on a
  volume that ignores case, a directory under which every held file is fresh is refused only once a
  write passes through it, and until then the manifest lists a path under a spelling the disk does
  not hold.
- **A file's own name is judged by what the volume answers.** No rule looks for a file or a
  directory standing under another spelling of a held path's own name where nothing lists that
  spelling: `Setup.kt` for `SetUp.kt` in a root whose manifest was deleted, or its owner's
  `readme.md` where a generation adds `README.md`. A volume that ignores case answers the held path
  with that file, and the table judges it as the file there. Where the held path is unlisted too,
  one of the fresh digest is left under the spelling it has and a marked one is replaced, the
  manifest listing the held spelling either way, whatever name the volume keeps, and any other is
  `engine/artifact-would-overwrite`. A volume that tells case apart never reads that file: the held
  path is judged alone, and once it is written the tree holds two names no volume that ignores case
  can. So the verdict and the tree differ by volume there, and a root the first kind left listing a
  spelling its disk lacks gains, on the second, a file under that spelling beside the old one. On
  the second a later generation holding the old spelling again is `engine/artifact-would-overwrite`
  over the file left under it, marked or not, where its digest is neither fresh nor known, the two
  spellings being judged together. And on either kind a run cut short over such a root can end in
  another tree than an uninterrupted one's, or refused where that one is not: no modelled sequence
  starts from such a root ([below](#every-write-is-planned-before-the-verdict)).
- **An emptied directory does not outlive its files.** A prune removes each directory above the
  file that it leaves empty, and a listed path the generation drops and the disk no longer holds
  has its empty directories removed too. Either removal goes up from the path, passes over a
  directory already gone and ends at the first that is no empty directory, one holding something or
  a file standing in a directory's place, which it leaves, so a run cut between a prune and the
  removal of its directory, or between the removals of two directories above one file, is finished
  by the next. `os.Root` removes a file and an empty directory by one call, `Remove`, so each is
  read without following a link (`os.Root.Lstat`) before it is removed: a run cut once it had
  replaced a directory `a/b` with a file `a/b` would else lose that file to the next run's removals
  above the dropped `a/b/x.go`.

`engine/file-edited` is a warning, since nothing is lost, and `engine/artifact-would-overwrite` an
error. Each is a finding about the output root, which the engine reports and a threshold or a triage
rule reads as any other. Whatever either makes of an error, the root it was found in is not written:
its plan holds nothing ([below](#every-write-is-planned-before-the-verdict)).

## Every write is planned before the verdict

The engine splits writing in two. *Planning* reads a target's output root and decides every write
and every prune of its generation, by the rules above, and writes nothing. *Applying* carries a plan
out and decides nothing. Every target's plan is made before the run's one verdict (architecture.md
§4), and its findings join the run's, so everything that can fail a run is found before anything is
written, and a failed run writes nothing: of two targets one of which would overwrite a file,
neither is written, whichever order they are named in. That order decides nothing else either: a
run's targets have their findings reported, and their writes planned and applied, in the byte order
of their keys.

A plan that draws an error holds no write and no prune, so applying it changes nothing, whatever the
verdict. Where the `none` threshold or a triage rule lets such a run stand, the other targets' roots
are written and that one is left as it was: no rating writes over a file its owner wrote, or into a
root holding another target's manifest or a newer Morphic's.

Applying a plan, in order:

1. **The root is read again.** The plan records what it read at every path a decision turned on,
   the manifest's among them, a digest or an absence. Where one no longer holds, the root moved:
   another run wrote there, or its owner saved a file. Nothing is written, and the run fails with
   an error naming the root.
2. **A pending manifest is written**, `morphic.manifest.pending.json`: the `Schema` and `Target` the
   manifest will state, and for each path, every digest known to be the generation's: those the
   manifest and any earlier pending manifest record, the one read at a marked file no manifest lists
   that the plan overwrites, and the new one where the plan writes the path or finds it fresh, never
   at a path kept as its owner's, whose bytes the run leaves as they are. The marked file's is
   recorded because this record makes its path a listed one: left out, a run cut before that file's
   write would leave it at a listed path under a digest nothing knows, which reads as its owner's
   edit.
3. **Prunes come before writes**, and with them the removal of each directory left empty, those
   above a listed path the generation drops and the disk no longer holds among them. A path a
   write needs may be held by a file a prune removes: the other spelling of a file renamed in
   letter case alone, or a file where a directory must go. Written first on a volume that ignores
   case, the new file is the one the prune of the old then removes, or it lands under the old
   spelling of a directory an interrupted run emptied and left standing.
4. **Each file is replaced atomically**, through a temporary file beside it, under a root opened at
   the file's own directory (`os.Root.OpenRoot`) ([emit rules](emit/rules.md)), after any empty
   directory standing at its path, or at a listed path folding as it, is removed. The atomic replace
   handed the output root alone makes its temporary file at the root's top (`renameio.WithRoot`,
   v2.0.2), away from the directory what a killed write leaves (below) stands in. A directory no
   longer empty there, a file or subdirectory added under it since the plan, is the root moved as
   well, met after the prunes and the writes before it: the run stops as an interrupted one does,
   and the next plan finds it in the write's way.
5. **The manifest is written last**, and then the pending manifest is removed.

- **An interrupted write heals.** A run killed once it has begun to write leaves the pending
  manifest, which lists every digest the generation may have written and that of each marked file it
  set out to replace, and planning reads it as it reads the manifest. So the next run recognizes a
  half-written tree as the generation's own, finishes it, and reports no file as its owner's edit,
  however many runs were cut short: a pending manifest only grows until a manifest lands.
- **The gaps that stay.** Reading the root again narrows the time between a plan and its writes and
  does not close it: a file saved between that check and its own write is replaced, so two runs into
  one root at once are not supported. And that a failed run writes nothing holds for what planning
  can find. A root that moved, or a disk that fails, is met only while applying, after an earlier
  target's plan was carried out; the root it stops in heals on the next run that writes, as any
  interrupted one does. Nor is the verdict the last thing that can fail a run that asks for the
  build check, which runs on the tree as written: a check that fails the run leaves the files in
  place ([verification](verification.md#the-output-builds)). And a rename in letter case cut
  short does not heal over its owner's save. While two spellings of a path are listed, as the
  pending manifest of such a run lists them, a file saved under either is
  `engine/artifact-would-overwrite` to a generation holding either
  ([above](#a-file-is-judged-by-its-digest)). That holds for the generation whose spelling the
  file stands under too, where an uninterrupted run keeps the file and reports
  `engine/file-edited`. The refused plan holds nothing, so what the cut run left, its pending
  manifest and its half-written tree, stays until the save is undone or the file moved away.
- **What a killed write leaves.** A temporary file, named for its destination with a dot before
  and digits after, may be left beside it. No later run reads or removes it, and a file's name is
  held short enough that it fits ([emit rules](emit/rules.md)). It is an unlisted file like any
  other, so the directory holding it is never emptied: it stands when the generation drops every
  file in it, and a later generation that puts a file in that directory's place, or spells it
  another way, is `engine/artifact-would-overwrite` until the leftover is deleted.

The rules were modelled before they were written: planning and applying, over a volume that joins
names that fold alike, a composed and a decomposed letter among them, and one that joins none, each
run cut after every single operation on the disk, then rerun with the same generation and with a
later one, cut again, and finished. Every sequence ended in the tree an uninterrupted run leaves,
with no pending manifest, no empty directory the root did not hold, no file reported as edited and
no two names that fold alike, but over a root a limit above leaves unread, and in one way more: an
empty directory where a generation writes a file, which an uninterrupted run removes, stands after a
run cut before that write when the generation that finishes no longer writes there. Each rule taken
out failed sequences of its own: prunes before writes, a pending manifest that keeps every earlier
digest, planning that reads the pending manifest, spellings judged together, the removal of
directories a cut left empty, passing over a directory already gone, the digest read at a marked
file, a directory at a dropped spelling read, a file under another spelling of a directory a write
passes through, another normalization the same name only where the volume answers with it, and a
removal going up that stops at a file. Recording no new digest at a path kept as its owner's needs
an owner's save the sequences do not make, which a trace beside the model shows: with that digest
recorded, a run cut once its pending manifest is written, and its owner then saving exactly those
bytes, the next generation replaces a save an uninterrupted run keeps. Links, an unreadable manifest
and the temporary file's place are not modelled. T-40 holds the rules, the interrupted runs among
them. The model is on #2247, in the comment that brings the one its body holds to these rules.

## Where generation runs

Morphic is not built for Windows, generation included. The atomic replace every file it writes
goes through, an IR document's (`morphic compile -o`) as a generation's, does not build there
(`GOOS=windows go build ./cmd/morphic` fails in `engine/internal/atomicfile`, morphic
@cfb366317), and the rules above were measured on APFS and modelled over a volume that ignores
case and one that does not, never on NTFS. What a run writes is meant to be portable all the same:
every segment of a path it writes is one Windows takes, and no two paths fold alike ([emit
rules](emit/rules.md)), though that the fold joins every pair NTFS does is assumed there and not
measured, and nothing bounds a path's whole length.

## Regions follow their entity

A `@morphic:keep <name>` region belongs to an entity: the one whose declaration encloses it, or, at
a file's top level, the one whose declaration it follows, else the file's first origin, whose
declaration it precedes. The manifest records each as a `Region`, keyed by that entity's ID, its
correlation key and the region's name, with the region's text and where it stood against the
declaration. A second region of one name in one entity is a warning, and the two stay where they
are, their file kept, since the key cannot tell which goes where. So a region follows its entity, to
the one whose prior entry is the region's ([the names a generation
shipped](refine/naming.md#collisions-and-the-names-a-generation-shipped)): when a rename moves the
entity to another file, the next generation writes the region where it stood against the entity's
declaration there, at the end of the declaration's body when the declaration enclosed it, so a
member a user added stays a member, and before or after the declaration otherwise. The old file,
emptied of generated symbols, is pruned as any other, unless its user edited it outside its regions:
then it is kept, and the regions stay in it ([above](#what-the-manifest-records)).

```go
type Region struct {
    Entity string                // the entity ID it belongs to
    Key    string                // that entity's correlation key
    Name   string                // the name its marker gives it, unique within the entity
    Place  string                // where it stood against the entity's declaration: "inside" its
                                 // body, or "before" or "after" it at the file's top level
    File   string                // where it was found
    Text   string                // what it held, markers excluded
}
```

A region whose entity no longer exists is a warning, and kept: its file is not pruned while it holds
one, and the staleness check lists it. Merging stays additive. A mode that rewrote generated code a
user edited would take ordinary three-way merge semantics, the prior generation as the base and
conflicts written with the markers merge tools read, never a merge of its own invention.

## A key that matches nothing

Every ID an input names can outlive its entity: a hint's key in any layer, the group its `Remount`
or a `Mount` names, and a per-target naming or compat overlay's key ([shaping
hints](contract/shaping-hints.md)); a runtime-policy layer's operation key
([layers](contract/runtime-policy.md#layers-and-operations)); and an envelope's `Except` and `Bases`
entries ([envelopes](plan/policy.md#envelopes)). A spec change that removes or renames the entity
leaves the ID matching nothing, and ignoring it would drop the user's customization in silence.

So the engine, which alone holds every input and the prior manifest, matches each such ID against
the document before any stage reads it, those of the inputs every emitter shares once for the run
and an emitter's own once for that emitter. Each that matches nothing is a warning
`emitters/unmatched-key`, not an error, since one configuration may serve several documents: it
fails a run only at a threshold the caller sets to count warnings (architecture.md §4). It names the
input, the ID and, where the prior manifest records a key for that ID and exactly one entity has it,
that entity, as the likely new home. A stage reading such an ID passes over it, since it configures
nothing, so each is reported once, by the one reader that can name where it went. A hint's
`ParamKey`, which names a parameter by its wire identity rather than an ID, is reported so too, with
no new home to name ([client settings](operations/client.md#client-settings)). A prior manifest's
entry for a removed entity is not such an ID: the staleness check reports it.

## A document the user writes around

A README is mostly hand-written, with generated sections: installation, authentication, an
example per feature, a table of contents. A keep region protects hand-written islands in generated
code; a README is the reverse, so it is integrated as blocks. The docs emitter
([docs and mocks](docs-and-mock.md)) returns each generated section as a block with a stable ID,
between `<!-- morphic:block <id> -->` and `<!-- morphic:block:end -->`, in an artifact whose
`Integrate.Blocks` is set. The ID is written percent-encoded as a URI path segment is (RFC 3986
§3.3), since one built from an IR ID can hold a space or `-->`, either of which would break the
marker. The engine's integrate step then rewrites only those blocks in the file on disk:

- a block found there is replaced by the new one of its ID, and everything outside the markers is
  kept byte for byte;
- a block marked `<!-- morphic:block <id> frozen -->` is the user's, and kept as it is;
- a block built from an entity's ID follows the entity as [a region
  does](#regions-follow-their-entity): after a rename the entity's new block takes its old one's
  place, and one frozen before it stands for the entity, no second block added beside it;
- a block new to this generation goes after the nearest block before it in the generated order
  that the file holds, else before the nearest one after it, else at the end, so a section first in
  the generated order is not written last;
- a block the generation no longer has is removed, unless frozen, and reported as an info;
- the table of contents is a block of its own, rebuilt last from the headings of the merged file,
  so it lists the user's sections too.

A file integrated as blocks is the user's around them, so it carries no generated header and is
never pruned, and its `Digest` covers its unfrozen blocks alone, an edit elsewhere being no edit. An
edit inside an unfrozen block is replaced as the block is, since freezing is how a user keeps one,
and where those blocks no longer match the digest the file is reported as a warning, so the edit is
not lost unseen.

A file that exists with no markers yet, such as a README written before the SDK was generated,
takes the generated blocks after its own text, and where no file exists the whole artifact is
written.
