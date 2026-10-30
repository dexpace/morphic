# The registry and capabilities

## The registry

Keyed by `TargetKey`, and composed by the engine exactly as the compiler registry is
(`compilers.Registry`, architecture.md §2.1): a plain value, with no package-level default and no
`init()`-time self-registration, either of which would be the mutable global INV5 rules out. The
engine's default constructor registers every built-in target in one place, beside the built-in
compilers, and a test or an embedder composes a set of its own, as `engine.WithCompilers` does for
compilers. Registration finishes before the engine exists, so concurrent runs only read it.

```go
type Registry struct{ /* by target, and registration order */ }

func NewRegistry() *Registry
func (r *Registry) Register(e Emitter) error           // refuses nil, a repeated key, no Lifecycle
func (r *Registry) Lookup(t TargetKey) (Emitter, bool)
func (r *Registry) Targets() []TargetKey               // sorted — determinism (INV7)
```

The engine is the only caller of `Lookup`, the only thing that runs `Generate`, and the only thing
that touches the filesystem, prunes, and renders diagnostics. Emitters are I/O-free. Reading,
rendering and writing the IR document have homes already: `engine/reader` reads what a run's
references name, `engine/report` renders, and `engine/output` writes, replacing a file through the
atomic replace in `engine/internal/atomicfile`. Writing artifacts and pruning land with §11, under
§5.6's rules. The engine runs `Generate` only for a run that has not failed (`engine.Result.Failed`,
architecture.md §4): at the default threshold a document carrying an error diagnostic reaches no
emitter, and a caller that wants one built from it anyway says so with the `none` threshold.

## Capabilities

`Capabilities()` lets the engine reason about an emitter without running it — which IR `TypeKind`s
it lowers, whether it supports write/integrate, whether it produces a surface for compat. It is
advisory metadata, not a second decision channel; it never changes what `Generate` emits.

```go
type Capabilities struct {
    LowersKinds     []ir.TypeKind // the kinds this emitter has an emit path for
    WriteIntegrate  bool          // supports additive merge into a live repo (§11)
    SurfaceVerify   bool          // produces a verify.Surface (§12)
    Protocols       []string      // "http", "grpc", "message", "graphql", "otp"
    Lifecycle       Lifecycle     // "hidden" | "experimental" | "stable" | "sunset"; required
    Successor       TargetKey     // the target replacing a sunset one; "" when none does
}
```

`Lifecycle` is the one field the engine acts on, and only to tell the user, never to change an
artifact. The states are a target's life in order. A `hidden` target is under construction: it is
left out of every listing — help, completion, the targets a misspelt name is answered with — and
runs only when named, with the `experimental` warning. An `experimental` target runs with a warning
diagnostic saying its output may change in any release. A `sunset` target runs with a warning that
names its `Successor` when it has one. The registry refuses a target that states no lifecycle, so
none is stable by omission.
