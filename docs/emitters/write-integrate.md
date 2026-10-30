# Write / integrate

Post-milestone (architecture.md §2.3), but the contract is designed so it bolts on with **no IR
change** (INV9). The `manifest.Manifest` and `Artifact` fields returned from `Generate` are the
seams; the `Origin`-tagged AST makes the manifest a byproduct of the emit walk.

- **Generation manifest keyed by IR stable IDs.** Records spec/plan/emitter/config **hashes**, a
  **sorted** file list (deterministic diffs), and an **entity → generated-symbol map keyed by
  `TypeID`/`OpID`/`PropID`** — assembled from the `Origin` on every emitted node. This is the
  biggest win of stable-ID keying: a spec rename is "same ID, new symbol" — a lookup, not oagen's
  structural rename-inference machinery. A declaration derived from an entity keys under it, as
  `User.ID#create` does (§8.1), a union's wrappers and fallback among them, so no two symbols share
  a key. Write/integrate and surface verification (architecture.md §2.3) both rest on it.
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
