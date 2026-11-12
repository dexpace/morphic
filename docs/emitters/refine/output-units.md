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

The runtime has a unit of its own ([below](#the-runtimes-unit)). Two rules move any other
declaration out of the root unit. Both are off by default, and the first that applies decides.

1. **Partition by source document.** `Partition` maps a `SourceInfo.Path` to a unit, for an API
   split across files that wants each file's types as a package of its own, a cross-file `$ref`
   becoming a cross-package reference. A type declaration goes to the unit its own
   `Provenance.Source` maps to, which for one an overlay introduced or rewrote is the document the
   overlay patched, the entry document, the overlay being its `WrittenBy`
   ([provenance](../../ir/provenance.md), [`overlays`](../../compilers/openapi.md#overlays)).
   Operations, sub-clients, the client and request types stay where rule 2 or the root puts them,
   whichever file declared them: an operation is a method of its group's client type, and Go
   declares a method in its receiver's package. What the document cannot honour is reported:
   - a key with a `#fragment` is an options error, since a partition is a whole document;
   - a key mapping to a name the units' scope reserves, such as `shared`, is an options error, since
     a name the options give a unit never yields ([reserved names](reserved-names.md));
   - two keys mapping to names that differ in letter case alone, such as `Billing` and `billing`,
     are an options error, since neither yields and a file system that ignores case would make their
     units one directory;
   - a key naming the entry document, `Sources[0]`, or an overlay is an error diagnostic: the root
     unit is the options' to name already, and an overlay's declarations are its document's;
   - a key naming no source of the document is a warning, since it partitions nothing;
   - an import cycle between units is an error, since neither a Go package nor a Gradle module
     admits one, naming the units and one reference that closes the cycle.

   Each document a spec's references read is a `Sources` entry after the entry document and its
   overlay, and a type it declares, inline in a parameter or response it holds included, names that
   entry ([identity](../../ir/identity.md)), so a key names a file the references read.
2. **Split by group.** With `SplitByGroup`, each top-level operation group is a unit, holding the
   group's operations, its sub-client and every type only its operations reach, as `TypePlan.Reach`
   says; `Reach` spreads from each operation's roots along the type graph as usage does
   ([derived-facts](../../ir/derived-facts.md)), a service's `CommonErrors` counting as every one of
   its groups' roots, since each of its operations can return them. A type several groups reach goes
   to a shared unit, and a type none reaches stays in the root with the client. With no partition,
   no cycle can arise: whatever a reached type holds is reached by every group that reaches it, so a
   group unit imports at most the shared unit and the runtime's, the shared unit at most the
   runtime's, which imports none ([the runtime's unit](#the-runtimes-unit)), and only the root,
   which nothing imports, holds a type that may reach into a group. A sub-client holds what its
   operations read of the client, its transport, server, credentials and client settings, as values
   of the runtime's types or of types its operations reach, which the client hands it when built,
   since a declaration of the root's would import the root. A type moves between a group's unit and
   the shared unit only when the set of groups reaching it changes, which takes a change to the
   operations that reach it. An operation a hint places in another group, by `Remount` or
   `AlsoMount` ([shaping hints](../contract/shaping-hints.md)), counts as each group whose client
   gets a method for it, so what it reaches goes where a type those groups reach goes: two groups
   mounting each other's operations would otherwise import each other. Placement reads the hints for
   that alone, and changing one moves the types its operation reaches. Which unit holds what an
   operation derives once `AlsoMount` counts it as several groups, its request type and its row of
   the runtime's settings among them, is #1736's.

The root unit's name is the options'. A group's unit is the group's name rendered for a package
under the root, the shared unit is `shared` under the root, the runtime's is `runtime` under the
root, and a partition's unit is the name its key maps to. The units' names are a scope of their own
([reserved names](reserved-names.md)): a group named `shared` or `runtime`, or `internal` in Go,
meets a reserved name, and two groups whose names render
alike — tags `Webhooks` and `webhooks`, both the Go package `webhooks` — collide, and are told apart
by group ID as any collision is. A unit is a directory on Go and on Kotlin, so the scope compares
its names case-folded ([collisions](naming.md#collisions-and-the-names-a-generation-shipped)), and a
group whose name differs from a partition's in letter case alone yields to it. A name the options
give a unit never yields. A declaration a target derives from another lands in that one's unit — a
visibility shape ([visibility](models.md#visibility-projections--n-wire-shapes)) in its type's, a
union's interface, wrappers and fallback in the union's, an operation's request and headers types
and its row of the runtime's settings ([runtime policy](../contract/runtime-policy.md#delivery)) in
the operation's — and a unit is a namespace of its own, so two declarations collide only within one
([reserved names](reserved-names.md)), save in the Objective-C names a Kotlin Multiplatform
framework gives them, which every unit shares ([Swift](../targets/kotlin/swift.md)). A wrapper, or
an inclusive union's field, imports a variant another unit holds, which is how a union reaches into
the shared unit. A service-wide error's headers type lands where a type all its groups reach does.
T-18 holds placement to these rules.

## The runtime's unit

The runtime is what a target's templates declare beside the generated code ([structure and
boilerplate](../emit/templates.md)): the failure value and the exceptions under it, the response and
stream types, the transport, the types a model may hold, such as Kotlin's `Optional` and a decimal,
and the helpers the generated code calls. It lands in a unit of its own, `runtime` under the root,
whether or not a rule above moves anything, so turning one on moves none of it. It imports no other
unit, and every unit may import it, since an operation in any unit fails with its failure value and
sends through its transport, and a model in any unit may hold one of its types. It declares nothing
named for a spec entity: each operation's row of its settings, though a template renders it, lands
in the operation's unit (above), and its failure kinds are the configuration's
([failures](../operations/errors.md#the-kinds-are-the-taxonomys)). So a runtime name meets a spec
name only in a file importing both, where Go writes it qualified
([Go](../targets/go/naming.md#imports)), and Kotlin, which cannot always, reserves it in every unit
([Kotlin](../targets/kotlin/naming.md#reserved-names)). Each
target's document says what that unit is in its build ([Go](../targets/go/overview.md#output-units),
[Kotlin](../targets/kotlin/overview.md#output-units)).

Five other places were weighed, and each fails an input:

- **The root**, beside the client, closes a cycle once a rule above moves anything. The root's
  client imports every group's unit, and a group's operations fail with the runtime's failure value:
  with Go 1.27.3, a root holding `Client` and `APIError` beside a group returning `*sdk.APIError`
  failed to build (`import cycle not allowed`). A partition closes one through types alone, its
  model holding a decimal while the root's operations reach the model.
- **The root until a rule moves something**, and a unit of its own after, moves every runtime name
  a caller writes, the failure type it matches among them, when the SDK's owner turns a rule on.
- **A published library**, versioned apart from the generated code, cannot hold what the templates
  render from a project's configuration. A failure's kinds are `Policy.Errors`' rows, each a class
  in Kotlin ([failures](../targets/kotlin/errors.md)), so a configuration adding a row needs a class
  no published library has. A library beside a generated unit holding the kinds keeps the unit and
  adds a dependency to version with it.
- **The shared unit** exists only under `SplitByGroup`, and it holds spec types, whose names would
  then meet the runtime's. With `Partition` on as well, it can hold a type that holds a partition's,
  and so import a partition that imports it.
- **The root, with the client moved out**, still holds the types no operation reaches, and one that
  holds a type a group reaches imports that group's unit, which imports the root.

No group is named `runtime` on the golden spec or in the corpus (596 groups, and 1,529 across the
570 corpus documents declaring any; spec @bc6fad24, morphic @ea70eba7), while `core`, the other name
weighed, names the Kubernetes API's group, which would then take another. Go's standard library has
a `runtime` package too, so a caller's file using both gives one a name of its own
([Go](../targets/go/naming.md#imports)).

## The size check

Each target states a limit on the declarations one unit holds, a default of its options version
([target options](../contract/options.md)), and reports each unit over it once, as a warning
`emitters/<target>/unit-over-limit` naming the unit, its count and the limit, and `SplitByGroup`
while it is off. The limit decides nothing: a count that moved declarations when it crossed a
threshold would move them as a document grew, the instability placement exists to prevent. Each
target's limit is in its document ([Go](../targets/go/overview.md#output-units),
[Kotlin](../targets/kotlin/overview.md#output-units)).
