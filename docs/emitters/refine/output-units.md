# Output units

Every declaration a target emits lands in one output unit, and a reference across units is an import
`ComputeImports` adds. What a unit is in a target's build is the target's
([Go](../targets/go/overview.md#output-units),
[Kotlin](../targets/kotlin/overview.md#output-units)). `PlaceDeclarations` decides each
declaration's unit as a function of the document and the target's options alone, never of how many
declarations there are: moving a declaration to another unit renames it for every caller, so a
document that grows must move none. Placement reads the whole document, so a scoped run places each
declaration as a full run does ([contract
rules](../contract/emitter.md#contract-rules-the-type-system-and-tests-enforce)).

Two rules move a declaration out of the root unit. Both are off by default, and the first that
applies decides.

1. **Partition by source document.** `Partition` maps a `SourceInfo.Path` to a unit, for an API
   split across files that wants each file's types as a package of its own, a cross-file `$ref`
   becoming a cross-package reference. A type declaration goes to the unit its own
   `Provenance.Source` maps to, and one an overlay introduced or rewrote belongs to the document the
   overlay patched, the entry document ([`overlays`](../../compilers/openapi.md#overlays)).
   Operations, sub-clients, the client and request types stay where rule 2 or the root puts them,
   whichever file declared them: an operation is a method of its group's client type, and Go
   declares a method in its receiver's package. What the document cannot honour is reported:
   - a key with a `#fragment` is an options error, since a partition is a whole document;
   - a key naming the entry document, `Sources[0]`, or an overlay is an error diagnostic: the root
     unit is the options' to name already, and an overlay's declarations are its document's;
   - a key naming no source of the document is a warning, since it partitions nothing;
   - an import cycle between units is an error, since neither a Go package nor a Gradle module
     admits one, naming the units and one reference that closes the cycle.

   Each document a spec's references read is a `Sources` entry after the entry document and its
   overlay, and a type it declares, inline in a parameter or response it holds included, names that
   entry (ir-design §3.1), so a key names a file the references read.
2. **Split by group.** With `SplitByGroup`, each top-level operation group is a unit, holding the
   group's operations, its sub-client and every type only its operations reach, as `TypePlan.Reach`
   says; `Reach` spreads from each operation's roots along the type graph as usage does (ir-design
   §4.9), a service's `CommonErrors` counting as every one of its groups' roots, since each of its
   operations can return them. A type several groups reach goes to a shared unit, and a type none
   reaches stays in the root with the client. No cycle can arise: whatever a reached type holds is
   reached by every group that reaches it, so a group unit imports at most the shared unit, the
   shared unit imports none, and only the root, which nothing imports, holds a type that may reach
   into a group. A type moves between a group's unit and the shared unit only when the set of groups
   reaching it changes, which takes a change to the operations that reach it.

The root unit's name is the options'. A group's unit is the group's name rendered for a package
under the root, the shared unit is `shared` under the root, and a partition's unit is the name its
key maps to. The units' names are a scope of their own ([reserved names](reserved-names.md)): a
group named `shared`, or `internal` in Go, meets a reserved name, and two groups whose names render
alike — tags `Webhooks` and `webhooks`, both the Go package `webhooks` — collide, and are told apart
by group ID as any collision is. A name the options give a unit never yields. A declaration a target
derives from another lands in that one's unit — a visibility shape
([visibility](models.md#visibility-projections--n-wire-shapes)) in its type's, a union's interface,
wrappers and fallback in the union's, an operation's request and headers types in the operation's —
and a unit is a namespace of its own, so two declarations collide only within one ([reserved
names](reserved-names.md)). A wrapper imports a variant another unit holds, which is how a union
reaches into the shared unit. A service-wide error's headers type lands where a type all its groups
reach does. T-18 holds placement to these rules.

## The size check

Each target states a limit on the declarations one unit holds, a default of its options version
([target options](../contract/options.md)), and reports each unit over it once, as a warning
`emitters/<target>/unit-over-limit` naming the unit, its count and the limit, and `SplitByGroup`
while it is off. The limit decides nothing: a count that moved declarations when it crossed a
threshold would move them as a document grew, the instability placement exists to prevent. Each
target's limit is in its document ([Go](../targets/go/overview.md#output-units),
[Kotlin](../targets/kotlin/overview.md#output-units)).
