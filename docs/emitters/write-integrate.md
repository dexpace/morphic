# Write / integrate

Post-milestone (architecture.md §2.3), but the contract is designed so it bolts on with **no IR
change** (INV9). The `manifest.Manifest` and `Artifact` fields returned from `Generate` are the
seams; the `Origin`-tagged AST makes the manifest a byproduct of the emit walk.

- **Generation manifest keyed by IR stable IDs.** Records what the generation was built from, a
  **sorted** file list (deterministic diffs), and an **entity → generated-symbol map keyed by
  `TypeID`/`OpID`/`PropID`** — assembled from the `Origin` on every emitted node. This is the
  biggest win of stable-ID keying: a rename of anything an ID is not built from is "same ID, new
  symbol" — a lookup, not oagen's structural rename-inference machinery — and a rename of what it is
  built from, a path or a component name, is carried by the correlation key each entry records. A
  declaration derived from an entity keys under it, as `User.ID#create` does
  ([`User`](targets/go/models.md#user-in-go)), a union's wrappers and fallback among them, so no two
  symbols share a key. Write/integrate and surface verification (architecture.md §2.3) both rest on
  it.
- **File-header provenance gates pruning.** `Artifact.Header` writes a generated-by header; the
  writer **never deletes a file lacking it** (hand-edited). First adoption (no prior manifest) skips
  pruning, writes a baseline, emits a notice.
- **Ignore regions & additive-only merge.** Two provenance markers — whole-file
  (`@morphic:ignore-file`) and region (`@morphic:keep`/`:end`) — with append-only merge. The
  structural merge is **language-parameterised** by an adapter registered into the manifest driver
  ([Go](targets/go/overview.md#merging-and-surface-extraction),
  [Kotlin](targets/kotlin/overview.md#merging-and-surface-extraction)). No adapter ⇒ skip, never
  clobber. Case-only renames go via a temp name (APFS/NTFS hazard).
- **Staleness check.** `(prior-manifest entities − current entities) ∩ files-on-disk` surfaces
  dead code additive writers cannot prune — cheap because both manifests are ID-keyed and the prior
  IR/plan snapshot is serializable (INV7).

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
    Files    []File              // every file written, sorted by path
    Entities map[EntityID]Entry  // every emitted entity and each declaration derived from one
    Steps    map[string]int      // each refine step and template that ran → its version
    Regions  []Region            // the hand-written regions found, each by its entity (below)
}

type Inputs struct {
    Doc     string               // the document's digest, which Plan.Version takes
                                 // (plan/overview.md)
    Plan    string               // Plan.Version
    Emitter string               // the emitter's own version
    Config  string               // SHA-256 of the decoded options, resolved policy and resolved
                                 // hints, each as its canonical JSON
}

type File struct {
    Path    string
    Digest  string               // SHA-256 over the bytes written, normalized (below)
    Origins []EntityID           // as the Artifact lists them
    Once    bool                 // written when absent, the user's thereafter
}

type Entry struct {
    Key    string                // the entity's correlation key, from the pass #679 plans
    Unit   string                // the unit the symbol was placed in
    Base   string                // its rendered name before a collision was resolved
    Symbol string                // the name the generation gave it (refine/naming.md)
    Kind   string                // the target-AST node kind it was declared as
    File   string                // the path it was written to
}
```

- **The document, not the spec text.** `Inputs.Doc` is the IR's digest, canonical JSON with
  `sources` left out. Reformatting a spec, or moving it to another path, changes the spec's bytes
  and leaves the IR whole, so it is no new generation.
- **A file as written.** `Digest` covers the bytes after formatting, with a byte-order mark
  stripped, line endings made `\n` and trailing newlines dropped, so a formatter run or a checkout
  that converts line endings is not mistaken for an edit. A file carrying the generated header
  whose bytes on disk no longer match was edited outside its regions. The writer leaves it as it is
  and reports it as a coded diagnostic, whose severity the engine decides, rather than overwrite
  the edit.
- **Nothing to do.** When every `Inputs` digest and every file's digest match the prior manifest,
  the engine writes nothing.
- **Behavior and steps.** `Options` is the version whose defaults this SDK keeps until its owner
  moves it ([target options](contract/options.md)), and `Steps` names the version of each step
  that shaped it, which [surface verification](surface-verification.md) reads to say what a
  regeneration changed and why.
- **Written once.** A file a target generates for the user to own, such as a test to fill in, is
  `Once`: written while absent, never rewritten or pruned. It is matched to a later generation by
  its origins' correlation keys, so a renamed entity's file is not written a second time under the
  new name.
- **Its own version.** Unlike the IR, a manifest holds choices that cannot be recomputed, so an
  older `Schema` is never refused: each version step has a migration, applied in order, and a
  manifest from a newer Morphic is refused, since its fields cannot be read.

## Regions follow their entity

A `@morphic:keep <name>` region belongs to an entity: the one whose declaration encloses it, or, at
a file's top level, the one whose declaration it follows, else the file's first origin. The
manifest records each as a `Region`, keyed by that entity's ID, its correlation key and the region's
name, with the region's text. So a region follows its entity: when a rename moves the entity to
another file, the next generation writes the region after the entity's declaration there, and the
old file, emptied of generated symbols, is pruned as any other.

```go
type Region struct {
    Entity EntityID              // the entity it belongs to
    Key    string                // that entity's correlation key
    Name   string                // the name its marker gives it, unique within the entity
    File   string                // where it was found
    Text   string                // what it held, markers excluded
}
```

A region whose entity no longer exists is reported and kept: its file is not pruned while it holds
one, and the staleness check lists it. Merging stays additive. A mode that rewrote generated code a
user edited would take ordinary three-way merge semantics, the prior generation as the base and
conflicts written with the markers merge tools read, never a merge of its own invention.

## A key that matches nothing

Every input keyed by an IR ID can outlive its entity: a hint ([shaping
hints](contract/shaping-hints.md)), a runtime-policy override
([layers](contract/runtime-policy.md#layers-and-operations)), a per-target naming overlay, an
overlay, the prior manifest. A spec change that removes or renames the entity leaves the key
matching nothing, and ignoring it would drop the user's customization in silence. So every such key
is reported, as a warning `emitters/unmatched-key` whose severity the caller controls and which
never aborts the run. It names the key and, where one exists, the entity whose correlation key
equals the one recorded for it, as the likely new home. A prior manifest's entry for a removed
entity is not such a key: the staleness check reports it.

## A document the user writes around

A README is mostly hand-written, with generated sections: installation, authentication, an
example per feature, a table of contents. A keep region protects hand-written islands in generated
code; a README is the reverse, so it is integrated as blocks. The docs emitter
([docs and mocks](docs-and-mock.md)) returns each generated section as a block with a stable ID,
between `<!-- morphic:block <id> -->` and `<!-- morphic:block:end -->`, in an artifact whose
`Integrate.Blocks` is set. The engine's integrate step then rewrites only those blocks in the file
on disk:

- a block found there is replaced by the new one of its ID, and everything outside the markers is
  kept byte for byte;
- a block marked `<!-- morphic:block <id> frozen -->` is the user's, and kept as it is;
- a block new to this generation goes after the last block it follows in the generated order, or
  at the end;
- a block the generation no longer has is removed, unless frozen, and reported;
- the table of contents is a block of its own, rebuilt last from the headings of the merged file,
  so it lists the user's sections too.

A file with no markers yet takes the generated blocks after its own text, once, and a first
generation writes the whole artifact.
