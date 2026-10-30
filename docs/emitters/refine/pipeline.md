# The refine layer

`emitters/<target>/refine`. This is where the "lossless, lowered late" bet is cashed (INV2) and
the **only** place names get cased (INV4). It reads the immutable IR and the shared Plan and
**constructs typed nodes of a per-language target AST**. It never mutates the IR and never mutates a
shared cross-language tree.

## The refiner contract — an ordered pipeline of pure lowerings

Following Kiota's concrete contract — an ordered list of small, named, idempotent steps, each valid
only because the un-lowered fact it needs is still present (prior-art.md §2) — but each step
is a **pure function `IR+Plan → nodes`**, not an in-place edit:

```go
package refine

type Refiner interface {
    Target() emitter.TargetKey
    Steps()  []Step             // ORDERED — composition order is part of the contract
}

type Step interface {
    Name() string               // stable, for diagnostics + golden step-trace tests
    Apply(*Ctx) []ir.Diagnostic // appends typed nodes to the target AST under construction; idempotent
}

type Ctx struct {
    Doc     *ir.Document        // read-only source of un-lowered facts
    Facts   *irfacts.Index      // read-only derived facts, the engine's one index (§2.2)
    Plan    *plan.Plan          // read-only shared decisions
    Types   typemap.Table       // the merged type table (§2.6), built from Options
    Policy  policy.Policy        // read-only SDK runtime policy (§6)
    Naming  naming.Policy        // the target's casing engine over its options' naming tables
                                 // (INV4; §4.12)
    Hints   map[emitter.EntityID]emitter.ShapeHint
    Options emitter.TargetOptions // decoded options (§2.5); a step asserts its own target's type
    Out     TargetAST            // the per-language AST being built (e.g. *goast.Module)
    Syms    *SymbolTable         // ID-keyed; collision resolution never touches IR identity
}

func Run(r Refiner, c *Ctx) []ir.Diagnostic  // runs Steps in order

// TargetAST is the per-language AST root marker; each target supplies a concrete type
// (e.g. *goast.Module). It is an interface here only so the shared refiner contract is language-
// neutral; every real step type-asserts to its own AST and appends typed nodes.
type TargetAST interface{ targetAST() }
```

**This ordered, named step list IS the IR acceptance test.** Each step of [the list](steps.md) is only implementable
because a specific un-lowered IR fact reached it; a missing IR capability shows up as an
unimplementable step, not a silent degrade. Ordering is asserted by a golden *step-trace* test:
collapsing aliases runs first, since every other step lowers references through it; placement
follows every structural lowering, and import computation follows placement, since a file imports
what its declarations reference from wherever those landed; the naming steps run last — casing,
then reserved-word escaping, whose scopes reserve what a file imports (§4.12), then collision
resolution — so every name, a unit's and an import's among them, is settled once, after everything
that names it exists. Because each step is idempotent and reads only its `Ctx`, a step-trace
snapshot pins the whole lowering deterministically.
