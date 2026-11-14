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
atomic replace in `engine/internal/atomicfile`. Writing artifacts and pruning land with [write /
integrate](../write-integrate.md), under the [emit rules](../emit/rules.md). The engine runs
`Generate` only for a run that has not failed (`engine.Result.Failed`, architecture.md §4): at the
default threshold a document carrying an error diagnostic reaches no emitter, and a caller that
wants one built from it anyway says so with the `none` threshold.

## Capabilities

`Capabilities()` lets the engine reason about an emitter without running it — which bindings it
lowers, whether it supports write/integrate, whether it produces a surface for compat. It is a
declaration of what the target does, constant for the target, and no second input: `Generate` stays
a function of what it is handed. The engine reads it to decide what it hands a target and when, and
the one rule that reads it inside a run, which binding a target lowers, reads the target's own
`Protocols` and `RPCSystems` ([bindings](../plan/bindings.md)), so what a target declares it covers
and what it lowers are one list.

```go
type Capabilities struct {
    WriteIntegrate  bool            // supports additive merge into a live repo (../write-integrate.md)
    SurfaceVerify   bool            // produces a verify.Surface (../surface-verification.md)
    CrossVersion    bool            // implements Router and reads every version snapshot's routes
                                    // (../docs-and-mock.md#routes-and-links)
    ReadsSamples    bool            // reads the samples of the targets its options name, so runs
                                    // after them
                                    // (../docs-and-mock.md#samples-come-from-the-sdk-emitters)
    WritesSamples   bool            // returns EmitterOutput.Samples, so a docs target may name it
    Build           Build           // the toolchains and checks that build its output
                                    // (../verification.md#the-output-builds)
    Protocols       []plan.Protocol // the protocols whose bindings it lowers (../plan/bindings.md)
    RPCSystems      []string        // the RPCBinding.System values it lowers; set exactly when
                                    // Protocols lists "rpc"
    Lifecycle       Lifecycle       // "hidden" | "experimental" | "stable" | "sunset"; required
    Successor       TargetKey       // the target replacing a sunset one; "" when none does
}
```

`CrossVersion` and `ReadsSamples` say what a run hands the target, every snapshot's routes and the
samples of the targets its options name, and when the engine runs it, and `WritesSamples` that it
returns samples, so a docs target naming one that does not is refused
(`engine/sample-target-invalid`); the registry refuses a target that sets `CrossVersion` without
implementing `Router` ([the contract](emitter.md)). `RPCSystems` is there because one RPC arm holds
every RPC system and a target lowering gRPC need not lower JSON-RPC: a binding of a system the list
omits is one the target does not cover ([bindings](../plan/bindings.md)). The registry refuses a
target listing `rpc` with no system, which would cover no RPC binding while claiming the arm, one
naming a system without listing `rpc`, and a protocol outside the vocabulary, and T-68 holds what a
target covers. No field lists the `TypeKind`s a target lowers: every target dispatches over every
kind (T-1), a kind it cannot lower draws a code (T-7), and its support table is derived from what it
draws ([verification](../verification.md#what-a-target-cannot-lower)), so a list would be a second
statement of the dispatch that could disagree with it. `Build` names the checks the engine runs over
a target's written output when a run asks for them, and the registry refuses a target that declares
a check without implementing `Checker` ([verification](../verification.md#the-output-builds)). A
message binding's system is each of its channel's servers' `Protocol`, such as `kafka` or `mqtt`,
and capabilities name none until a messaging target needs to
([open question 4](../open-questions.md)), since no compiler lowers a channel yet. `Lifecycle` is
the one field the engine acts on in every run, and only to tell the user, never to change an
artifact. The states are a target's life in order. A `hidden` target is under construction: it is
left out of every listing — help, completion, the targets a misspelt name is answered with — and
runs only when named, with the `experimental` warning. An `experimental` target runs with a warning
diagnostic saying its output may change in any release. A `sunset` target runs with a warning that
names its `Successor` when it has one. The registry refuses a target that states no lifecycle, so
none is stable by omission.