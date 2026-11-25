# The registry and capabilities

## The registry

Keyed by `TargetKey`, and composed by the engine exactly as the compiler registry is
(`compilers.Registry`, architecture.md §2.1): a plain value, with no package-level default and no
`init()`-time self-registration, either of which would be the mutable global INV5 rules out. The
engine's default constructor registers every built-in target in one place, beside the built-in
compilers, and a test or an embedder composes a set of its own, as `engine.WithCompilers` does for
compilers. Registration finishes before the engine exists, so concurrent runs only read it.

```go
type Registry struct{ /* by target */ }

func NewRegistry() *Registry
func (r *Registry) Register(emitter Emitter) error       // what it refuses is below
func (r *Registry) Lookup(key TargetKey) (Emitter, bool) // a hidden target among them
func (r *Registry) Targets() []TargetKey                 // sorted — determinism (INV7); a hidden
                                                         // target left out
```

`Register` reads an emitter's key and capabilities once, behind `barrier.Catch`, as
`compilers.Registry` reads a compiler's formats, so a panic in either is a breach of the contract
(`emitters.ErrContract`, [the checked calls](emitter.md#the-checked-calls)) and no crash. It
refuses:

- a nil emitter, and a key already registered;
- a key that is not one or more segments joined by `/`, each of lowercase ASCII letters, digits and
  `-` and none empty, as `go`, `kotlin-jvm` and `mock/go` are. A key is written into a diagnostic
  code, a manifest and a command line, so it holds what each takes as it stands;
- a key whose first segment is one a shared package reports its codes under, `plan`, `naming`,
  `policy` or `embed`: a target so keyed would report under `emitters/plan/…`, and a reader of a
  code could not tell the plan's finding from the target's
  ([codes](emitter.md#the-codes-a-target-reports));
- capabilities that state no lifecycle, or one outside the four; a `Successor` on a target that is
  not `sunset`; `rpc` listed with no system, or a system with no `rpc`; a protocol outside the
  vocabulary; and a declared build check on an emitter that does not implement `Checker`.

The engine is the only caller of `Lookup`, and the only thing that touches the filesystem, prunes,
and renders diagnostics. It configures and generates through the two checked calls and no other
way ([the contract](emitter.md#the-checked-calls)). Emitters are I/O-free. Reading, rendering and
writing the IR document have homes already: `engine/reader` reads what a run's references name,
`engine/report` renders, and `engine/output` writes, replacing a file through the atomic replace in
`engine/internal/atomicfile`. Writing artifacts and pruning are [write /
integrate](../write-integrate.md)'s, under the [emit rules](../emit/rules.md). The engine
configures and generates only for a run that has not failed (`engine.Result.Failed`,
architecture.md §4): at the default threshold a document carrying an error diagnostic reaches no
emitter, and a caller that wants one built from it anyway says so with the `none` threshold. A run
naming a target the registry lacks, or one the project does not configure
([the project](project.md)), is refused before the spec is read.

## Capabilities

`Capabilities()` lets the engine reason about an emitter without running it — which bindings it
lowers, how its output is built, where it is in its life. It is a declaration of what the target
does, constant for the target, and no second input: `Generate` stays a function of what it is
handed. It holds only what nothing else shows: a capability an optional interface shows is read
off the interface ([below](#a-capability-an-interface-shows)). The engine reads it to decide what it
hands a target and when, and
the one rule that reads it inside a run, which binding a target lowers, reads the target's own
`Protocols` and `RPCSystems` ([bindings](../plan/bindings.md)), so what a target declares it covers
and what it lowers are one list.

```go
type Capabilities struct {
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

`WritesSamples` says that a target returns samples, so a docs target naming one that does not is
refused (`engine/sample-target-invalid`). It stays a field because nothing else shows it before a
run: samples are a member of what `Generate` returns, and the engine must refuse the docs target
before any target runs. `RPCSystems` is there because one RPC arm holds
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
`engine/target-experimental` saying its output may change in any release. A `sunset` target runs
with a warning `engine/target-sunset` that names its `Successor` when it has one. The engine
reports each once per run in which the target runs, on however many snapshots. The registry refuses
a target that states no lifecycle, so none is stable by omission.

## A capability an interface shows

Four things a target may do are no field of `Capabilities`, because an interface the target
implements already shows each, and a flag beside it could say otherwise:

- **Reading every snapshot's routes** is implementing `Router`, and **showing other targets'
  samples** is implementing `SampleReader`, each on the target's generator ([the
  contract](emitter.md#a-target-is-configured-once)). The engine reads each off a target once that
  target is configured, and orders the run once every target it needs is, before any generates.
- **Merging into a live tree** and **extracting a surface** are a merge adapter and an extractor
  ([write / integrate](../write-integrate.md), [surface
  verification](../surface-verification.md)), each an optional interface that lands with M4. A
  target with none merges nothing and has no surface to compare.

Flags `CrossVersion`, `ReadsSamples`, `WriteIntegrate` and `SurfaceVerify` stood here before. Each
restated what an interface shows, so the two could disagree. The registry refused `CrossVersion`
set without `Router`, and a run refused `ReadsSamples` on a target whose options were no
`SampleReader`, while a target that implemented either and left its flag unset ran as if it had
neither.