# Reuse and maintainability

The angle that asks whether the change writes code someone else could maintain, as every line of
our own is a line to keep: anything the tree, the standard library or a maintained library already
holds, anything written twice, and whether what needs no morphic concept stays apart from what
does. It loads `morphic-designing-code`, whose rules decide when sites share one implementation and
where code with no domain lives.

1. Search for what each addition already has (`references/reuse.md#mechanism-exists`), and for a
   library that could own each piece that is not Morphic's concern
   (`references/reuse.md#maintained-library-first`).
2. For each behaviour two sites share, weigh one implementation
   (`.claude/skills/morphic-designing-code/references/design-rules.md#share-only-what-means-the-same`).
3. Separate each domain-free piece from the domain code around it
   (`.claude/skills/morphic-designing-code/references/design-rules.md#domain-free-code-lives-apart`).

A finding names each copy or library, the one kept and where the shared form lives. The matrix is
each addition with what each search returned.

## A new helper, check, walk or golden may already exist
`mechanism-exists`

- **Rule.** For each helper, check, walk, algorithm or golden the change adds, search for one that
  already answers it: in the standard library and the module's dependencies, then in the tree, by
  the concept's vocabulary across code, `*_test.go` and `testdata/`, by bodies alike once renamed
  (a `go/ast` scan, and `dupl` under the golangci-lint the `Makefile` pins), and by comments saying
  "mirrors" or "as X does". Search the change's own hunks against one another the same way. Each
  copy found is a finding: call the one, or say why the two stay apart.
- **Why.** A new census duplicated an unwitnessed-facts golden CI already held (#1005). Three scalar
  hoisters differed in three inputs, so #147's missing constraints had to be fixed in each (#834).
- **Probe.** The report names each search and its hits.

## Let a maintained library own what is not Morphic's to maintain
`maintained-library-first`

- **Rule.** For each piece the change writes that is not Morphic's concern, such as a grammar, a
  codec or a file write, look for a maintained library owning it: the standard library,
  `golang.org/x`, `go.mod`'s modules, then a new one. Adopt one a probe shows doing the job on
  every input the piece meets, whose license and upkeep fit, and which the layering admits (`ir/`
  imports the standard library alone). Pin each behaviour relied on with a test, as
  `docs/parser-library.md` pins the parser library.
- **Why.** Hand-written pointer decoding gave way to `jsontext.Pointer` (#834) and file replacement
  to `renameio` (#1009), while the parser library's unbounded `$ref` recursion came back into the
  compiler (#558).
- **Probe.** The report names each library weighed, the hits of
  `gh issue list --state all --limit 1000 --search "<library>"`, and its probe.
