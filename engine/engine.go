package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// RunOptions configures a single pipeline run.
//
// Compiler options arrive by one of two channels, never both. FormatOptions is
// a value of the compiler's own options type, forwarded as
// compilers.Options.FormatOptions by a caller that imports it. CompilerOptions
// is textual, for a caller (the CLI) that does not: each compiler decodes it
// into its own type. Every compiler asked to recognize the spec decodes it, as
// the settings bound that read too, but only the one that takes the spec is
// held to the result: its failure is an error, another's falls back to that
// compiler's defaults. See compilers.Registry.Detect.
type RunOptions struct {
	FormatOptions   any               `json:"formatOptions,omitzero"`
	CompilerOptions map[string]string `json:"compilerOptions,omitempty"`
	// DisablePasses names passes the run skips. A name none of the engine's
	// passes carries fails the run with ErrUnknownPass before the spec is read,
	// since ignoring a misspelt one would leave its pass running unseen.
	DisablePasses []string `json:"disablePasses,omitempty"`
	// FailOn is the threshold the run is judged at, FailOnError when empty,
	// and Result.Failed is the verdict. A value naming no threshold fails the
	// run with ErrUnknownFailOn before the spec is read.
	FailOn FailOn `json:"failOn,omitempty"`
}

// errOptionChannels reports both option channels set at once. Which one wins
// would be a precedence rule no caller can see the effect of, and a run
// configured two ways is a mistake in the caller rather than a case to resolve.
var errOptionChannels = errors.New(
	"engine: set FormatOptions or CompilerOptions, not both")

// Result is the outcome of a pipeline run. A nil Document alongside diagnostics
// is a legal outcome and the shape every refusal takes — a source no compiler
// claims, or one a compiler declined to lower.
//
// Diagnostics is the whole list for the run. When Document is non-nil it holds
// the same values, so a caller reading either channel sees every finding.
//
// Sources is what each Provenance.Source in Diagnostics indexes, NoSource
// aside, on every path: the Document's table, the refusing compiler's, or the
// spec alone when no compiler claimed it.
type Result struct {
	Document    *ir.Document           `json:"document,omitzero"`
	Diagnostics []ir.Diagnostic        `json:"diagnostics,omitempty"`
	Sources     []ir.SourceInfo        `json:"sources,omitempty"`
	Format      compilers.SourceFormat `json:"format"`
	// Failed is the engine's verdict: the run produced no document, or one
	// with a diagnostic at or above RunOptions.FailOn. A failed run still
	// returns what it produced, to inspect or persist. What failing stops is
	// building on it: the engine starts no emitter on a failed run.
	Failed bool `json:"failed,omitzero"`
}

// Engine orchestrates the detect → compiler → passes pipeline.
//
// An Engine is safe for concurrent use, and concurrent runs over one spec yield
// identical documents, not merely uncorrupted ones. Run builds a fresh
// compilers.Source per call, so the state a compiler leaves on one
// (Source.Parsed) is never shared between runs. NewWith finishes the registry
// and the pass list before the Engine exists, and Run only reads them. The rest
// is the purity of compilers and passes: one holding mutable state breaks both.
//
// TestEngine_ConcurrentRunSharesOneEngine pins both properties: the second on
// every run, the first only under -race, which the coverage gate uses.
type Engine struct {
	registry *compilers.Registry
	passes   []pass.Pass
}

// Compilers returns the built-in compilers, fresh values in registration order:
// the set New registers, for a caller that drives compilers without an Engine.
// Future compilers are added here and only here.
func Compilers() []compilers.Compiler {
	return []compilers.Compiler{openapi.New()}
}

// New composes the default engine: a registry with every built-in compiler
// registered (Compilers), and the default passes.
func New() (*Engine, error) {
	return NewWith(Compilers()...)
}

// NewWith registers the given compilers into a fresh registry and wraps it in
// an Engine running the default passes, for tests and embedders that need a
// custom compiler set. A nil compiler and a register failure (a compiler
// reporting no formats, or two claiming the same format) surface as a Go error
// rather than a panic, naming the argument position.
//
// An empty set is refused. A built engine cannot gain a compiler, so one with
// none could compile nothing and would report every source as unrecognized,
// blaming the document for the caller's misconfiguration.
func NewWith(cs ...compilers.Compiler) (*Engine, error) {
	if len(cs) == 0 {
		return nil, errors.New("engine: no compilers; an engine with none can compile nothing")
	}

	reg := compilers.NewRegistry()
	for i, c := range cs {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("engine: register compiler %d: %w", i, err)
		}
	}
	return &Engine{registry: reg, passes: defaultPasses()}, nil
}

// Run executes the pipeline for the spec at specPath: read it, compile it with
// the compiler that recognizes it, run the passes opts.DisablePasses does not
// name, in order, and judge the Result at opts.FailOn.
//
// A Go error is reserved for I/O, cancellation and programmer errors: an
// unreadable file, a cancelled ctx, a compiler or pass breaking its contract,
// or an option ErrUnknownPass or ErrUnknownFailOn refuses. Everything wrong
// with the spec, a source no compiler can lower included, is a diagnostic in
// the Result, so a Go error never blames the spec. Calls may overlap; each
// owns the document it returns.
func (e *Engine) Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error) {
	passes, failOn, err := e.prepare(opts)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("engine: read spec %q: %w", specPath, err)
	}
	res, err := e.compile(ctx, compilers.Source{Path: specPath, Data: data}, opts, passes)
	if err != nil {
		return nil, err
	}

	failed, err := failOn.verdict(res)
	if err != nil {
		return nil, err
	}
	res.Failed = failed
	return res, nil
}

// prepare checks what Run checks before it reads anything, and returns the
// passes opts leaves on and the threshold the run is judged at.
//
// An engine no constructor built is the caller's mistake whatever the path
// says, and reporting the path first would send them to look at the file;
// unchecked, the nil registry panics during detection. A run configured two
// ways is wrong whatever the source, and every compiler would otherwise be
// asked to resolve options no compiler could.
func (e *Engine) prepare(opts RunOptions) ([]pass.Pass, threshold, error) {
	if e == nil || e.registry == nil {
		return nil, 0, errors.New("engine: uninitialized; build one with New or NewWith")
	}
	if opts.FormatOptions != nil && len(opts.CompilerOptions) > 0 {
		return nil, 0, errOptionChannels
	}

	passes, err := e.enabledPasses(opts.DisablePasses)
	if err != nil {
		return nil, 0, err
	}
	failOn, err := opts.FailOn.resolve()
	if err != nil {
		return nil, 0, err
	}
	return passes, failOn, nil
}

// compile runs the pipeline over source: detect, compile, then passes. The
// Result it returns is not yet judged.
func (e *Engine) compile(
	ctx context.Context, source compilers.Source, opts RunOptions, passes []pass.Pass,
) (*Result, error) {
	// Each compiler is asked under the options it would compile with, because
	// recognizing a source is a read of it that the caller's budgets bound as
	// much as the compile's reads.
	det, ok, err := e.registry.Detect(ctx, source, func(c compilers.Compiler) (compilers.Options, error) {
		formatOpts, err := formatOptions(c, opts)
		return compilers.Options{FormatOptions: formatOpts}, err
	})
	if err != nil {
		return nil, fmt.Errorf("engine: detect %q: %w", source.Path, err)
	}
	format := det.Recognition.Format
	if !ok {
		// Detection is handed the one source, which is the whole table its
		// findings and the engine's own can index.
		return &Result{Format: format, Diagnostics: e.undetected(format, det.Declined),
			Sources: []ir.SourceInfo{{Path: source.Path}}}, nil
	}
	// The detection hands the compile what recognizing the source parsed, and
	// holds what comes back to the compiler's contract, so a Result never carries
	// a finding indexing no source or a document misnaming its input.
	out, err := det.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("engine: compile %q: %w", source.Path, err)
	}
	doc, compiled := out.Document, out.Diagnostics
	if doc == nil {
		return &Result{Diagnostics: compiled, Format: format, Sources: out.Sources}, nil
	}
	// Both channels end up carrying the whole list: the Result is what a caller
	// gates on, and the document is what gets persisted (golden snapshots, IR
	// diff, caches, emitters). The compiler's findings, both of its channels
	// already merged by the detection, are taken before any pass runs, since the
	// passes' contract leaves Document.Diagnostics to the engine: a transform
	// that builds a fresh document cannot lose what the compiler stored on the
	// old one.
	doc, found, err := runPasses(ctx, doc, passes)
	if err != nil {
		return nil, err
	}
	doc.Diagnostics = ir.MergeDiagnostics(compiled, found)
	return &Result{Document: doc, Diagnostics: doc.Diagnostics, Sources: doc.Sources, Format: format}, nil
}

// undetected reports a source no registered compiler will take. None of the
// three cases is an I/O failure or a programmer error, so none may leave Run as
// a Go error, which the CLI would report as misuse of itself.
//
// A named format means a compiler recognized the source but this build does not
// carry the format, as with Swagger 2.0. Otherwise nothing recognized the
// bytes, and the declining compilers' own account of why is preferred, since
// the engine parses nothing and can say only that nobody claimed it.
func (e *Engine) undetected(format compilers.SourceFormat, declined []ir.Diagnostic) []ir.Diagnostic {
	if format.Name != "" {
		return []ir.Diagnostic{specProblem(codeNoCompilerForFormat,
			"no compiler registered for format %s; %s", format, e.served())}
	}
	if len(declined) > 0 {
		return declined
	}
	return []ir.Diagnostic{specProblem(codeUnrecognizedFormat,
		"unrecognized spec format; %s", e.served())}
}

// Formats returns every format e compiles, in compilers.Registry.Formats's
// order: what a caller names to say which specs this build reads.
func (e *Engine) Formats() []compilers.SourceFormat {
	return e.registry.Formats()
}

// served names the formats this build compiles, for a reader who has just been
// told theirs is not one of them. Naming what failed without naming what would
// have worked leaves the next step to guesswork, and the engine can answer it
// from its own registry without knowing what any of the names mean.
//
// NewWith builds every Engine and refuses an empty compiler set, so there is
// always at least one format to name and the sentence never trails off.
func (e *Engine) served() string {
	formats := e.Formats()
	names := make([]string, 0, len(formats))
	for _, format := range formats {
		names = append(names, format.String())
	}
	return "this build compiles " + strings.Join(names, ", ")
}

// formatOptions resolves the options c would compile with, decoding the
// textual channel through c itself. os.ReadFile is what makes a
// path-valued setting work: the engine already reads the spec, so loading a file
// a setting names keeps the I/O on this side of the contract and the compiler
// pure.
func formatOptions(c compilers.Compiler, opts RunOptions) (any, error) {
	if len(opts.CompilerOptions) == 0 {
		return opts.FormatOptions, nil
	}
	decoded, err := c.DecodeOptions(compilers.OptionSet{
		Settings: opts.CompilerOptions,
		ReadFile: os.ReadFile,
	})
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return decoded, nil
}
