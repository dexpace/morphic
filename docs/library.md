# Morphic as a Go library

The pipeline the [`morphic` command](cli.md) runs is available as a Go package. `engine.New` builds
the default engine: every built-in compiler and the default IR passes. `engine.WithCompilers` and
`engine.WithPasses` replace either set, so an embedder can run a pass of its own, composed with
`engine.Passes`, the default list in order; a pass changes a document through an `ir.Editor`. `Run`
reads the spec, no further than a compiler would read it, asks the registered compilers which one
recognizes it, compiles, and runs the passes in order, each over the document the one before it
returned.

```go
eng, err := engine.New()
if err != nil {
    return err
}

res, err := eng.Run(ctx, "openapi.yaml", engine.RunOptions{
    CompilerOptions: map[string]string{"grouping": "path-prefix"},
})
if err != nil {
    return err // misuse, I/O or a broken contract; problems in the spec arrive as diagnostics
}

// The CLI's own rendering, one finding per line, each one no rule accepted.
r := report.Report{Sources: res.Sources, Findings: res.Findings, UnusedRules: res.UnusedRules}
if err := report.WriteText(os.Stderr, r, report.TextOptions{}); err != nil {
    return err
}
if res.Failed {
    return errors.New("the spec has problems") // no document, or a diagnostic at FailOn
}

// Canonical, deterministic bytes, replacing the file atomically as `-o` does.
return output.ReplaceDocument(ctx, "api.ir.json", res.Document, output.Compact)
```

## Inputs

`RunSpec` compiles a spec held in memory, such as an edited buffer, as the file at its path, and
reads every other document from disk as `Run` does. `RunSources` compiles sources already in memory:
the first is the spec, every source is a document its references or its settings may name by its
`Path`, and nothing else is read, so a spec split across files compiles without a file written.
`RunDocument` runs the passes over a document already compiled, such as one decoded from a file.

## Output

`engine/report` renders the findings and `--explain` (`report.Explain`) exactly as the CLI prints
them, and `engine/output` writes a document as the CLI does: to a stream whole or not at all, or to
a file replaced atomically. Both take the run's context, and a write it cancels leaves no partial
document behind.

## Options

Options go in either as text through `RunOptions.CompilerOptions`, the CLI's `--compiler-option` vocabulary, or
as a typed value such as an `openapi.Options` through `RunOptions.FormatOptions`; set one or the
other, not both.

`RunOptions.DisablePasses` turns passes off by name (`engine.ValidatePass` names the `validate`
pass), and a name no pass carries fails the run with `engine.ErrUnknownPass`.

The structural duties only `irverify` checks are left to the harness, unless `RunOptions.Verify` is
set: it holds each stage's document to them, and the findings each stage reports, and each pass to
leaving its input alone, and fails the run with an `*engine.VerifyError` listing what broke.

`engine.WithCache` answers a run from an earlier one with the same inputs, skipping detection, the
compile and the passes; `cache.NewDir` is a store on disk and `cache.BuildID` names the running
build. A cache that fails reports on `Result.CacheErr` and never fails the run
([Architecture](architecture.md) §2.6).

## Errors and the verdict

A Go error from `Run` means misuse, I/O or a stage breaking its contract. Everything wrong with the
spec itself, including a spec no compiler can lower, is a diagnostic in the result, with a nil
`Document` when nothing could be lowered.

A compiler breaking its contract, say by returning a document whose source table does not describe
the spec, or by panicking, fails the run with an error wrapping `compilers.ErrContract`; a pass
doing so fails it with one wrapping `pass.ErrContract`. A panic arrives as a `*barrier.PanicError`
in the chain, which keeps the value and the stack beside the one-line message. A stage that ignores
`ctx` holds `Run` until it returns, and the run then fails with the context's error, whatever the
stage returned.

`Result.Failed` is the engine's verdict, the one the CLI's exit status reads: a run fails when it
lowered no document, or when a finding no rule accepted is rated at `RunOptions.FailOn` or above,
which is `error` unless set to `warning`, `info` or `none`. A failed run still carries its document
and every diagnostic.

## Triage

`RunOptions.Triage` takes the [triage](cli.md#triage) rules, as `triage.Rule` values or read from a
file by `engine.ReadTriage`; a rule `triage.NewRuleset` refuses fails the run with an error wrapping
`triage.ErrInvalidRule` before the spec is read. `Result.Diagnostics` holds the findings as their
stages reported them, as the document does, and `Result.Findings` the same findings as rated, index
for index, each with the rule it met; `Result.UnusedRules` lists the rules that matched none. The
rules are applied once the run is in hand, so a cached run is rated by the rules of the run it
answers. `report.WriteText` renders the findings as the CLI's text does, and `report.WriteJSON` as
its JSON report.
