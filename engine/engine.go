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
	SkipValidate    bool              `json:"skipValidate,omitzero"`
}

// errOptionChannels reports both option channels set at once. Which one wins
// would be a precedence rule no caller can see the effect of, and a run
// configured two ways is a mistake in the caller rather than a case to resolve.
var errOptionChannels = errors.New(
	"engine: set FormatOptions or CompilerOptions, not both")

// Result is the outcome of a pipeline run. A nil Document alongside diagnostics
// is a legal outcome and the shape every refusal takes — a source no compiler
// claims, or one a compiler declined to lower; the caller decides what is fatal.
//
// Diagnostics is the whole list for the run. When Document is non-nil it holds
// the same values, so a caller reading either channel sees every finding.
type Result struct {
	Document    *ir.Document           `json:"document,omitzero"`
	Diagnostics []ir.Diagnostic        `json:"diagnostics,omitempty"`
	Format      compilers.SourceFormat `json:"format"`
}

// Engine orchestrates the detect → compiler → passes pipeline over a registry
// of compilers.
//
// An Engine is safe for concurrent use, and concurrent runs over one spec yield
// identical documents, not merely uncorrupted ones. Run builds a fresh
// compilers.Source per call, so the state a compiler leaves on one
// (Source.Parsed) is never shared between runs. NewWith finishes the registry
// before the Engine exists, and Run only reads it. The rest is
// compilers.Compiler's purity: a compiler holding package-level mutable state
// would break both.
//
// TestEngine_ConcurrentRunSharesOneEngine pins both properties: the second on
// every run, the first only under -race, which the coverage gate uses.
type Engine struct {
	registry *compilers.Registry
}

// New composes the default engine: a registry with every built-in compiler
// registered. Future compilers are added here and only here.
func New() (*Engine, error) {
	return NewWith(openapi.New())
}

// NewWith registers the given compilers into a fresh registry and wraps it in
// an Engine, for tests and embedders that need a custom compiler set. A nil
// compiler and a register failure (a compiler reporting no formats, or two
// claiming the same format) surface as a Go error rather than a panic, naming
// the argument position.
//
// An empty set is refused. A built engine cannot gain a compiler, so one with
// none could compile nothing and would report every source as unrecognized,
// blaming the document for the caller's misconfiguration.
func NewWith(fronts ...compilers.Compiler) (*Engine, error) {
	if len(fronts) == 0 {
		return nil, errors.New("engine: no compilers; an engine with none can compile nothing")
	}

	reg := compilers.NewRegistry()
	for i, front := range fronts {
		if err := reg.Register(front); err != nil {
			return nil, fmt.Errorf("engine: register compiler %d: %w", i, err)
		}
	}
	return &Engine{registry: reg}, nil
}

// Run executes the pipeline for the spec at specPath: read the file, ask the
// registered compilers which recognizes it, dispatch to that one, and unless
// disabled append the validate pass's diagnostics.
//
// A Go error is reserved for I/O and programmer errors: an unreadable file, or
// a compiler failing in a way its contract calls an error. Everything wrong
// with the spec itself, a source no compiler can lower included, is a
// diagnostic in the Result, so a caller can treat a Go error as misuse, as the
// CLI does. Calls may overlap; each owns the document it returns.
func (e *Engine) Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error) {
	// Ahead of the read, because an engine that never went through a constructor
	// is the caller's mistake whatever the path turns out to say, and reporting
	// the path first would send them to look at the file. Without this the nil
	// registry is dereferenced during detection and the package panics, which is
	// no way to report a misuse of it.
	if e == nil || e.registry == nil {
		return nil, errors.New("engine: uninitialized; build one with New or NewWith")
	}

	// Also ahead of the read: a run configured two ways is wrong whatever the
	// source turns out to be, and every compiler would otherwise be asked to
	// resolve options that no compiler could.
	if opts.FormatOptions != nil && len(opts.CompilerOptions) > 0 {
		return nil, errOptionChannels
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("engine: read spec %q: %w", specPath, err)
	}
	source := compilers.Source{Path: specPath, Data: data}

	// Each compiler is asked under the options it would compile with, because
	// recognizing a source is a read of it that the caller's budgets bound as
	// much as the compile's reads.
	det, ok, err := e.registry.Detect(ctx, source, func(c compilers.Compiler) (compilers.Options, error) {
		formatOpts, err := formatOptions(c, opts)
		return compilers.Options{FormatOptions: formatOpts}, err
	})
	if err != nil {
		return nil, fmt.Errorf("engine: detect %q: %w", specPath, err)
	}
	format := det.Recognition.Format
	if !ok {
		return &Result{Format: format, Diagnostics: e.undetected(format, det.Declined)}, nil
	}
	// What detection parsed to recognize the source is what the compile lowers,
	// so the source carries it forward rather than being read twice. The
	// registry has already dropped it unless the compiler about to be asked is
	// the one that made it.
	source.Parsed = det.Recognition.Parsed
	doc, diags, err := det.Compiler.Compile(ctx, []compilers.Source{source}, det.Options)
	if err != nil {
		return nil, fmt.Errorf("engine: parse %q: %w", specPath, err)
	}
	if doc == nil {
		return &Result{Diagnostics: diags, Format: format}, nil
	}
	if !opts.SkipValidate {
		diags = append(diags, pass.Validate(doc)...)
	}
	// Both channels end up carrying the whole list: the Result is what a caller
	// gates on, and the document is what gets persisted (golden snapshots, IR
	// diff, caches, emitters). Merging rather than picking one is what keeps a
	// finding its compiler put on only one of them — see mergeDiagnostics.
	doc.Diagnostics = mergeDiagnostics(doc.Diagnostics, diags)
	return &Result{Document: doc, Diagnostics: doc.Diagnostics, Format: format}, nil
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

// served names the formats this build compiles, for a reader who has just been
// told theirs is not one of them. Naming what failed without naming what would
// have worked leaves the next step to guesswork, and the engine can answer it
// from its own registry without knowing what any of the names mean.
//
// NewWith builds every Engine and refuses an empty compiler set, so there is
// always at least one format to name and the sentence never trails off.
func (e *Engine) served() string {
	formats := e.registry.Formats()
	names := make([]string, 0, len(formats))
	for _, format := range formats {
		names = append(names, format.String())
	}
	return "this build compiles " + strings.Join(names, ", ")
}

// formatOptions resolves the options front would compile with, decoding the
// textual channel through front itself. os.ReadFile is what makes a
// path-valued setting work: the engine already reads the spec, so loading a file
// a setting names keeps the I/O on this side of the contract and the compiler
// pure.
func formatOptions(front compilers.Compiler, opts RunOptions) (any, error) {
	if len(opts.CompilerOptions) == 0 {
		return opts.FormatOptions, nil
	}
	decoded, err := front.DecodeOptions(compilers.OptionSet{
		Settings: opts.CompilerOptions,
		ReadFile: os.ReadFile,
	})
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return decoded, nil
}
