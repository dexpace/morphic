# The refine layer

`emitters/<target>/refine`. This is where the "lossless, lowered late" bet is cashed (INV2) and
the **only** place names get cased (INV4). It reads the immutable IR and the shared Plan and
**constructs typed nodes of a per-language target AST**. It never mutates the IR and never mutates a
shared cross-language tree.

## The refiner contract — an ordered pipeline of pure lowerings

Following Kiota's concrete contract — an ordered list of small, named, idempotent steps, each valid
only because the un-lowered fact it needs is still present (prior-art.md §2) — but each step
is a **pure function `IR+Plan → nodes`**, not an in-place edit. What every target shares of it is
the runner, in `emitters/refine`, and nothing else:

```go
package refine

// Step is one named lowering over a target's own state.
type Step[State any] struct {
    Name  string // stable, for diagnostics and the golden step trace
    Apply func(ctx context.Context, state *State) []ir.Diagnostic // appends typed nodes to the
                                                                  // AST under construction;
                                                                  // idempotent
}

// RunSteps applies steps in order, asks the context between them, and returns the names of the
// steps it ran, which a golden trace pins.
func RunSteps[State any](ctx context.Context, state *State, steps []Step[State]) (trace []string, diagnostics []ir.Diagnostic, err error)
```

- **`State` is the target's own**, a struct it declares: what it reads, the fields of its
  `EmitterInput` ([the contract](../contract/emitter.md)) and its decoded options with the merged
  type table and the naming tables they give ([type table](../contract/type-table.md),
  [naming](naming.md)), and what it builds, its AST root, such as `*goast.Module`, and the symbols
  it resolves. Every member has the target's own type, so a step asserts nothing.
- **The step list is the target's**, a value its generator builds: the order is part of the
  target's contract, and T-10 pins it.
- **`RunSteps` refuses** a step with no name or no `Apply`, and two steps of one name, since the
  trace names them, and stops between two steps at a done context with the context's error.

A shared `Refiner` interface, a context struct `Ctx` every target's steps read, and a marker
interface `TargetAST` over the AST roots stood here before. A context struct every target shares
grows a member for each target's need, its options reached a step as a value of a shared interface
the step asserted back to its own type, and the marker typed nothing: every step asserted the root
to its own AST. A step over the target's own state needs neither assertion. The runner is generic,
so it lands with the first target's pipeline, its first caller that is not.

The helpers more than one target's steps call are functions of `emitters/refine` too, each of the
values it reads: the decorator fold ([decorators](decorators.md)), the cycle finder over
`Facts.Component` ([recursive references](models.md#recursive-references)), untagged-union
recognition ([unions](unions.md)), and the resolution of a reference to its type-table key through
the plan's scalar chain ([type table](../contract/type-table.md)). Each lands with its first
caller in a target.

**This ordered, named step list IS the IR acceptance test.** Each step of [the list](steps.md) is
only implementable because a specific un-lowered IR fact reached it; a missing IR capability shows
up as an unimplementable step, not a silent degrade. Ordering is asserted by a golden *step-trace*
test: collapsing aliases runs first, since every other step lowers references through it; redaction
and the stability and deprecation marks follow every step declaring a value's type or a method an
operation lowers to, since each adds to those; placement follows every structural lowering, and
import computation follows placement, since a file imports what its declarations reference from
wherever those landed; the naming steps run last — casing, then reserved-word escaping, whose scopes
reserve on Go what a file imports ([reserved names](reserved-names.md)), then collision resolution —
so every name, a unit's and an import's among them, is settled once, after everything that names it
exists. On Go a function's scope reserves the names its file's imports are given, so Go's list holds
the escaping and collision steps twice after casing, and its step trace shows both pairs: the first
settles the units' names and, after them, the name each file gives its imports, and the second every
other scope. One run of each could not, since the escaping of a function's scope reads a name the
collision step gave an import. Because each step is idempotent and reads only its state, a
step-trace snapshot pins the whole lowering deterministically.
