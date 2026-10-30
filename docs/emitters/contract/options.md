### 2.5 Target options

A target's options are a typed Go struct of its own, read by its `DecodeOptions` before `Generate`
runs, so a mistake in a configuration is reported before anything is generated rather than
discovered in the output. `raw` is the target's configuration as one JSON object; the engine reads
it from wherever the user wrote it. `TargetOptions` is the interface every target's options type
meets. The engine hands `Generate` only the value the same target's `DecodeOptions` returned, and
`Generate` asserts it back to its own type; a value of another type is a programmer error, which
`Generate` reports as an error diagnostic rather than a panic.

```go
type TargetOptions interface {
    OptionsVersion() int // the version the configuration stated; the value is in the current shape
}
```

- **Strict.** An unknown key, a missing required field and an invalid value are an error, as a
  compiler's unknown setting is (`compilers.Compiler.DecodeOptions`): a setting silently ignored
  leaves its author believing they configured something.
- **Versioned.** A configuration states the options `version` it was written against. One that
  states none is refused, and so is an absent configuration, because the version decides which
  defaults apply and that must not be guessed. A version newer than the target knows is refused: it
  was written for a later Morphic.
- **Defaults belong to a version.** A target declares each optional setting's default per version,
  and fills what a configuration leaves unset with the defaults of the version it states. A change
  that would alter what existing configurations generate is a new version whose defaults differ, so
  an existing configuration keeps generating what it did until its owner moves it. New
  configurations start at the current version. A golden test per version decodes a configuration
  that states only that version and pins every default it fills, so a default changed in place
  fails a test rather than reaching existing SDKs.
- **Migrations change the shape, never a default.** A setting renamed, retyped or removed between
  versions has one pure migration per version step, applied in order to the raw configuration, so
  `Generate` sees only the current shape. A migration writes no default: those come from the stated
  version, so no migration can carry the wrong ones forward.
- **The upgrade report.** Decoding a configuration behind the current version reports, as info
  diagnostics coded `emitters/<target>/options-behind`, each setting it leaves unset whose default
  differs between its version and the current one, so an owner sees exactly what moving changes.
  The manifest (§11) records the stated version, `OptionsVersion()`.

Per-target heuristics live here too, beside the target's other choices, rather than in the plan: the
parameter-passing shape (§4.13), the type-table rows that give an unbounded number a width (§2.6),
and the naming tables (§4.12) — the acronyms, the character names that give words to a name with
none, and the leading words an identifier that would begin with a digit takes. The target-AST nodes
a default shapes carry `Inferred` provenance (§4.3), as INV6 requires. How a failure reaches its
caller is a setting here as well (§8.5).
