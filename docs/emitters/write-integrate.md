# Write / integrate

Post-milestone (architecture.md §2.3), but the contract is designed so it bolts on with **no IR
change** (INV9). The `manifest.Manifest` and `Artifact` fields returned from `Generate` are the
seams; the `Origin`-tagged AST makes the manifest a byproduct of the emit walk.

- **Generation manifest keyed by IR stable IDs.** Records what the generation was built from, a
  **sorted** file list (deterministic diffs), and an **entity → generated-symbol map keyed by
  `TypeID`/`OpID`/`PropID`** — assembled from the `Origin` on every emitted node. This is the
  biggest win of stable-ID keying: a rename of anything an ID is not built from is "same ID, new
  symbol" — a lookup, not oagen's structural rename-inference machinery — and one of what it is
  built from is carried by the correlation key each entry records where the key's own source holds,
  as an operation's route does when a path variable is renamed. A path or a component renamed
  changes its key too ([correlation keys](../ir/derived-facts.md#correlation-keys)), so only the
  structural tier of [open question 5](../ir/open-questions.md) could carry it. A
  symbol derived from an entity keys under it, as `User.ID#create` does
  ([`User`](targets/go/models.md#user-in-go)), a union's wrappers, fallback and inclusive fields
  among them, so no two symbols share a key. Write/integrate and surface verification
  (architecture.md §2.3) both rest on it.
- **File-header provenance gates pruning.** `Artifact.Header` writes a generated-by header; the
  writer **never deletes a file lacking it** (hand-edited). First adoption (no prior manifest) skips
  pruning, writes a baseline, emits a notice.
- **Ignore regions & additive-only merge.** Two provenance markers — whole-file
  (`@morphic:ignore-file`) and region (`@morphic:keep <name>`/`:end`,
  [below](#regions-follow-their-entity)) — with append-only merge. The structural merge is
  **language-parameterised** by an adapter registered into the manifest driver
  ([Go](targets/go/overview.md#merging-and-surface-extraction),
  [Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)). No adapter ⇒ skip, never
  clobber. Case-only renames go via a temp name (APFS/NTFS hazard).
- **Staleness check.** `(prior-manifest entities − current entities) ∩ files-on-disk` surfaces
  dead code additive writers cannot prune — cheap because both manifests are ID-keyed and the prior
  IR/plan snapshot is serializable (INV7). A prior entity counts as current where its entry is a
  current entity's prior entry ([the names a generation
  shipped](refine/naming.md#collisions-and-the-names-a-generation-shipped)), so an operation whose
  path variable was renamed is not listed as dead though its old ID is gone.

## What the manifest records

A manifest holds the choices a later generation must honor and cannot recompute from the spec: the
names it shipped, the behavior it pinned, the files a user now owns. So it records more than hashes:

```go
package manifest

type Manifest struct {
    Schema   int                 // this format's version; an older one is migrated forward
    Target   string              // the emitter's TargetKey
    Options  int                 // OptionsVersion(), the behavior pinned for this SDK
                                 // (contract/options.md)
    Inputs   Inputs              // what the generation was built from
    Files    []File              // every file of the SDK, sorted by path, written by this run
                                 // or kept from the prior manifest (below)
    Entities map[EntityID]Entry  // every entity the SDK declares and each symbol derived from one
    Keys     int                 // irfacts.CorrelationVersion, the derivation of every Key here
                                 // (ir/derived-facts.md#correlation-keys)
    Steps    map[string]int      // each refine step and template that ran → its version (#702)
    Regions  []Region            // the hand-written regions found, sorted by entity, then name
}

// Generations is a cross-version target's manifest, in the place any target's lies: each run's
// Manifest by the label of its snapshot, "" for an unsliced document (contract/emitter.md).
type Generations struct {
    Schema    int                 // as Manifest.Schema, the version of this format
    Snapshots map[string]Manifest // the last label's holds the files spanning versions
}

type Inputs struct {
    Doc     string               // the document's digest, which Plan.Version takes
                                 // (plan/overview.md)
    Plan    string               // Plan.Version
    Emitter string               // the Morphic version and the Go release that built it, from
                                 // its build information
    Config  string               // SHA-256 of the decoded options, resolved policy and resolved
                                 // hints, each as its canonical JSON
    Handed  string               // SHA-256 of the snapshot label, samples and routes the engine
                                 // handed the run beside its document (contract/emitter.md), as
                                 // canonical JSON; "" = none
}

type File struct {
    Path    string
    Digest  string               // SHA-256 over the bytes written, normalized (below)
    Origins []EntityID           // as the Artifact lists them
    Once    bool                 // its Artifact's SkipIfExists: written by the first generation
                                 // that finds it absent, the user's thereafter
}

type Entry struct {
    Key    string                // the entity's correlation key, which ir/irfacts derives
    Scope  string                // the scope its name was resolved in, as Ctx.Syms keys it
                                 // (refine/reserved-names.md)
    Base   string                // its rendered name before a collision was resolved
    Symbol string                // the name the generation gave it (refine/naming.md)
    Kind   string                // the target-AST node kind it was declared as
    File   string                // the path it was written to
}
```

- **The document, not the spec text.** `Inputs.Doc` is the IR's digest, canonical JSON with
  `sources` and `diagnostics` left out. Reformatting a spec, or moving it to another path, changes
  the spec's bytes and where its reports point, and leaves the rest of the IR whole, so it is no new
  generation.
- **A file as written.** `Digest` covers the bytes after formatting, each keep region's text left
  out and its markers kept, with a byte-order mark stripped, line endings made `\n` and trailing
  newlines dropped, so an edit inside a region, a run of the formatter the generator ran and a
  checkout that converts line endings are not mistaken for an edit. A file carrying the generated
  header whose bytes on disk, normalized alike, no longer match its digest was edited outside its
  regions. The writer leaves it as it is, neither overwriting nor pruning it, reports it as a
  warning, since nothing is lost, and keeps its prior entry in the new manifest, so the next
  generation reports it again. An entity a rename moves out of it is written in its new place as any
  other, since its callers name it there, and its regions stay in the kept file, which still holds
  them, rather than being written twice. A file marked `@morphic:ignore-file` is its user's whole,
  as a file written once is, so it is never written, pruned or checked for an edit, and a file
  integrated as blocks is checked by its blocks alone ([below](#a-document-the-user-writes-around)).
- **Nothing to do.** When every `Inputs` field and every file's digest match the prior manifest, a
  file written once or marked `@morphic:ignore-file` aside, the engine writes nothing. `Emitter` is
  a version, not the hash of the executable the plan and IR caches key on, since a team's machines
  share the manifest and must agree on what it holds, and it holds the Go release beside Morphic's,
  since its `go/format` shapes Go's output ([emit rules](emit/rules.md)). A version naming no one
  tree, `(devel)` or one marked `+dirty`, never matches, so a change being developed is never
  skipped.
- **A scoped run.** A run under a `Scope` writes what the scope selects, removes and prunes nothing
  outside it, a README block among them, and keeps the prior manifest's entry of every entity, file
  and region it leaves out, so its manifest is the whole SDK's: the next run's names, staleness
  check and no-op test read what was shipped, not what one run wrote.
- **One generation across snapshots.** A target whose `Capabilities.CrossVersion` is set is run on
  every version snapshot as one generation, each run under its label's directory ([the
  contract](contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)). Its manifest is
  a `Generations`, which holds each snapshot run's `Manifest` under that snapshot's label; each
  run's `Prior` is its own label's, and the writer reads a path across all of them, so a file no run
  returns any longer, a dropped label's directory among them, is pruned as any other, and one that
  moves from one label's run to another's, as a file spanning versions does when another label
  becomes last, is neither pruned nor taken for a user's edit. Its manifest is a `Generations` even
  for an unsliced document, whose one label is `""`, and where a prior holds no entry for the last
  label, as when a document gains versions, that run takes the prior's `""` entry, so the names it
  shipped carry over.
- **Behavior and steps.** `Options` is the version whose defaults this SDK keeps until its owner
  moves it ([target options](contract/options.md)), and `Steps` names the version of each step
  that shaped it, which #702 plans so that a regeneration can say which steps moved.
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
  manifest from a newer Morphic is refused as an error and nothing is written, since its fields
  cannot be read and a generation run without them would rename what was shipped.

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
    Entity EntityID              // the entity it belongs to
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
