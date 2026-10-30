# Rules the emit layer holds to

The [template guards](templates.md#two-guards-on-the-boilerplate-templates) say what a template may
contain. These rules hold for the emit layer as a whole, the printer and the engine's write
included, because each is easy to lose once code exists and none shows in a single run's output.
T-25 gives each a test.

- **No mutable model.** A template receives a value built for it: scalars, and lists and structs of
  scalars, with no pointer, map, function or method anywhere in it. `text/template` calls a method
  of its data, and one with a pointer receiver writes through it, so read-only has to be the value's
  shape rather than a convention. A render can therefore change neither the IR, the plan, the target
  AST nor another artifact.
- **No re-rendering.** Rendered output is never parsed as a template again, so a value from the
  document that looks like template syntax comes out verbatim. The printer escapes a value for what
  it writes into, since a target's own syntax re-reads text as a template does: a Kotlin `$` in a
  string would interpolate, and a `*/` in a description would end a KDoc comment.
- **Hermetic functions.** A template function reads its arguments and nothing else: no environment,
  clock, network or filesystem. The function map is a literal list, and the lint refuses `call`,
  which would run any function a value held.
- **Deterministic scheduling.** Emission may run in parallel, with bounded workers, over a queue
  built in the plan's key order, each result stored at its place in that queue, so the order work
  finishes in changes nothing. A target returns its artifacts sorted by `Path`.
- **Locale-independent ordering.** Every sort compares bytes, which for UTF-8 is code-point order,
  as Go's `slices.Sort` on strings does. Nothing imports a collation, and `LC_ALL` changes no
  output.
- **One artifact per path.** The engine refuses a run in which two artifacts of a target share a
  path, as an error `engine/duplicate-artifact-path` naming both `Origins`, before it writes any.
  Paths are compared case-folded, since a file system that ignores case keeps only one of `Foo.go`
  and `foo.go`.
- **Formatter failures.** A formatter that fails is the located, hard diagnostic of the [format
  pass](templates.md#two-guards-on-the-boilerplate-templates), and its artifact is not written:
  `Format` has no path that returns its input. No artifact reads the build that made it — a
  timestamp, a VCS stamp, an absolute path — so a build's flags change no byte. What a build can
  change is `go/format`, which is compiled into Morphic and which a Go release may revise, so moving
  the toolchain a release is built with is a change to the output, which T-6's goldens show.
  Kotlin's formatter runs outside `Generate`, and its printer lays out what it checks ([Kotlin
  formatting](../targets/kotlin/overview.md#formatting)).
- **Nothing written, something said.** A target that returns no artifact reports why, as a warning
  `emitters/<target>/no-artifacts` naming the cause, such as a `Scope` that matches nothing or a
  document with no type or operation, so an empty directory never reads as success.
- **Contained writes.** The engine checks every `Path` before the first write: forward-slashed,
  `filepath.IsLocal` once converted, and naming a file, or the run stops with an error
  `engine/artifact-outside-root` and writes nothing. It then writes through an `os.Root` opened at
  the output root, which refuses what only the disk shows: a symlink in the tree that leaves it. A
  string prefix is no check, as `out-evil` starts with `out`, and a lexical check alone passes the
  symlink.
