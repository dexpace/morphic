package compilers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/ir"
)

// ErrContract reports a compiler that broke its contract: a duty Compiler's doc
// comments state, which the registry or a Detection checked and found unmet. It
// names a bug in the compiler, never in the source, so it is a Go error rather
// than a diagnostic, and it is raised before the outcome reaches anyone who
// would act on it.
var ErrContract = errors.New("compiler broke its contract")

// Compiled is what compiling one detected source produced, held to the
// compiler's contract.
type Compiled struct {
	// Document is the lowered document, or nil when the compiler refused the
	// source.
	Document *ir.Document
	// Diagnostics is every finding the compile reported: the Document's own,
	// then each one Compile returned that the Document does not hold. A
	// compiler may fill either channel (see Compiler), so reading one alone
	// would lose what only the other holds.
	Diagnostics []ir.Diagnostic
	// Sources is the table every Provenance.Source in both lists indexes: the
	// Document's, or the compiler's SourceTable for a refusal.
	Sources []ir.SourceInfo
}

// Compile compiles d.Source with the compiler d names, under d's options,
// handing it what recognizing the source parsed, and holds the outcome to the
// duties Compiler's Compile and SourceTable state that the inputs let a caller
// check. A breach, a panic in either method among them, is an error wrapping
// ErrContract; an error the compiler returned is passed on as it is, so a
// cancellation still reads as one.
//
// A Detection compiles once: the parse it hands over is live, and a compile
// may write through it (Source.Parsed).
func (d Detection) Compile(ctx context.Context) (Compiled, error) {
	if isNilCompiler(d.Compiler) {
		return Compiled{}, errors.New("compilers: compile: the detection names no compiler")
	}
	src := d.Source
	src.Parsed = d.Recognition.Parsed
	sources := []Source{src}

	var (
		doc   *ir.Document
		diags []ir.Diagnostic
		err   error
	)
	if berr := d.call("Compile", func() { doc, diags, err = d.Compiler.Compile(ctx, sources, d.Options) }); berr != nil {
		return Compiled{}, berr
	}
	if err != nil {
		return Compiled{}, err
	}

	var table []ir.SourceInfo
	if berr := d.call("SourceTable", func() { table = d.Compiler.SourceTable(sources, d.Options) }); berr != nil {
		return Compiled{}, berr
	}

	// The compiler may keep the slices it answered with, so the caller is handed
	// copies; the Document is the one thing it gives up (Compiler.Compile).
	table, diags = slices.Clone(table), slices.Clone(diags)
	out := Compiled{Document: doc, Diagnostics: diags, Sources: table}
	if doc != nil {
		out.Sources = doc.Sources
	}
	if err := d.check(out, table); err != nil {
		return Compiled{}, fmt.Errorf("compilers: compile as %s: %w", d.Recognition.Format, err)
	}
	if doc != nil {
		out.Diagnostics = ir.MergeDiagnostics(doc.Diagnostics, diags)
	}
	return out, nil
}

// call runs f, a call of d's compiler's method, behind a barrier, and reports a
// panic in it as that method's breach.
func (d Detection) call(method string, f func()) error {
	if perr := barrier.Catch(f); perr != nil {
		return fmt.Errorf("compilers: compile as %s: %w", d.Recognition.Format, breach(method, perr))
	}
	return nil
}

// check holds out, compiled from d.Source, to the contract. table is what the
// compiler's SourceTable named for the compile.
func (d Detection) check(out Compiled, table []ir.SourceInfo) error {
	if len(table) == 0 || table[0].Path != d.Source.Path {
		return contractf("SourceTable does not name the source %q first", d.Source.Path)
	}
	if out.Document == nil {
		// A refusal leaves the caller its diagnostics alone, so one with no error
		// among them fails the run with no reason given.
		if _, stated := ir.FirstError(out.Diagnostics); !stated {
			return contractf("refused the source with no error diagnostic saying why")
		}
		return checkIndexes(out.Diagnostics, len(out.Sources))
	}
	if v := out.Document.IRVersion; !ir.CompatibleVersion(v) {
		return contractf("document is stamped irVersion %q, where this build writes %q", v, ir.IRVersion)
	}
	if err := checkSources(out.Sources, table, d.Source, d.Recognition.Format); err != nil {
		return err
	}
	if err := checkIndexes(out.Document.Diagnostics, len(out.Sources)); err != nil {
		return fmt.Errorf("on the document: %w", err)
	}
	return checkIndexes(out.Diagnostics, len(out.Sources))
}

// checkSources reports a document whose source table does not open with the
// one the compile's inputs give: SourceTable's entries in its order, src's
// entry first, naming the format src was recognized as and the digest of its
// bytes. An entry past them names another document the compile read, which
// SourceTable, reading no input, cannot know of.
func checkSources(got, table []ir.SourceInfo, src Source, format SourceFormat) error {
	if len(got) < len(table) {
		return contractf("document lists %d sources where SourceTable names %d", len(got), len(table))
	}
	for i := range table {
		if got[i].Path != table[i].Path {
			return contractf("document source %d is %q where SourceTable names %q", i, got[i].Path, table[i].Path)
		}
	}
	if got[0].Format != format.String() {
		return contractf("document reads source 0 as %q, recognized as %q", got[0].Format, format)
	}
	if want := ir.SourceHash(src.Data); got[0].Hash != want {
		return contractf("document hashes source 0 as %q, its bytes as %q", got[0].Hash, want)
	}
	return nil
}

// checkDecline holds what a compiler said while declining a source to
// Compiler.Detect. Silence is how it declines bytes that are not its own, so
// anything it says is about its own source: it includes an error saying why the
// source cannot be read, and indexes the one source detection was handed, or
// none. Speaking with no error leaves the caller no document and no reason.
func checkDecline(diags []ir.Diagnostic) error {
	if _, stated := ir.FirstError(diags); len(diags) > 0 && !stated {
		return contractf("declined its own source with no error diagnostic saying why")
	}
	return checkIndexes(diags, 1)
}

// checkIndexes reports the first diagnostic of diags that is not in a table of n
// sources (ir.FirstOutsideTable).
func checkIndexes(diags []ir.Diagnostic, n int) error {
	if d, outside := ir.FirstOutsideTable(diags, n); outside {
		return contractf("diagnostic %s indexes source %d of %d", d.Code, d.Provenance.Source, n)
	}
	return nil
}

// contractf builds a breach: its detail, then ErrContract.
func contractf(format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, ErrContract)...)
}

// valid reports whether f names a dialect: a name and a version, the name free
// of the '@' String puts between them, so the rendered form reads back as one
// pair.
func (f SourceFormat) valid() bool {
	return f.Name != "" && f.Version != "" && !strings.Contains(f.Name, "@")
}

// breach reports a panic in one of a compiler's methods: the method, what the
// barrier recovered, then ErrContract, so a caller can match either.
func breach(method string, perr error) error {
	return fmt.Errorf("%s: %w: %w", method, perr, ErrContract)
}
