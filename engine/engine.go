package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
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
	// Verify holds the document the compiler returns, and each pass's, to the
	// invariants ir/irverify states, and each pass to leaving the document it was
	// handed as it found it. The first stage to break one fails the run with a
	// *VerifyError, or an error wrapping pass.ErrContract. It costs more than the
	// compile itself, and ctx does not interrupt it, so it is for developing a
	// stage.
	Verify bool `json:"verify,omitzero"`
}

// errOptionChannels reports both option channels set at once. Which one wins
// would be a precedence rule no caller can see the effect of, and a run
// configured two ways is a mistake in the caller rather than a case to resolve.
var errOptionChannels = errors.New(
	"engine: set FormatOptions or CompilerOptions, not both")

// errCompilerOptionsUnused reports compiler options given to a run no compiler
// takes part in. Ignoring them would leave the caller believing they shaped the
// result.
var errCompilerOptionsUnused = errors.New(
	"engine: RunDocument compiles nothing, so it takes no FormatOptions or CompilerOptions")

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
// identical documents, not merely uncorrupted ones. Each run builds a fresh
// compilers.Source, so the state a compiler leaves on one (Source.Parsed) is
// never shared between runs. New finishes the registry and the pass list
// before the Engine exists, and a run only reads them. The rest is the purity
// of compilers and passes: one holding mutable state breaks both.
//
// TestEngine_ConcurrentRunSharesOneEngine pins both properties: the second on
// every run, the first only under -race, which the coverage gate uses.
type Engine struct {
	registry *compilers.Registry
	passes   []namedPass
	// verifier is what a verified run holds each document to, fixed when the
	// engine is built.
	verifier verifier
}

// Compilers returns the built-in compilers, fresh values in registration order:
// the set New registers, for a caller that drives compilers without an Engine.
// Future compilers are added here and only here.
func Compilers() []compilers.Compiler {
	return []compilers.Compiler{openapi.New()}
}

// Option configures the Engine New builds. Options apply in order, so a later
// one setting what an earlier one set wins.
type Option func(*config)

// config is what New builds an Engine from: the built-in compilers and the
// default passes, as the options leave them.
type config struct {
	compilers []compilers.Compiler
	passes    []pass.Pass
}

// WithCompilers registers cs, in order, in place of the built-in compilers, for
// a test or an embedder composing a set of its own. The list is copied.
func WithCompilers(cs ...compilers.Compiler) Option {
	cs = slices.Clone(cs)
	return func(c *config) { c.compilers = cs }
}

// WithPasses runs ps, in order, in place of the default passes. An embedder
// adding a pass of its own composes the list from Passes, putting a transform
// before validate so validate judges what the transform returns. The list is
// copied, and an empty one runs no pass.
func WithPasses(ps ...pass.Pass) Option {
	ps = slices.Clone(ps)
	return func(c *config) { c.passes = ps }
}

// New builds an Engine registering every built-in compiler (Compilers) and
// running the default passes (Passes), unless opts say otherwise. A nil option,
// a compiler the registry refuses, and a pass list a run could not address by
// name (a nil pass, an unnamed one, one whose Name panics, two sharing a name)
// are each a Go error naming the position, rather than a panic.
//
// An empty compiler set is refused too. A built engine cannot gain a compiler,
// so one with none could compile nothing and would report every source as
// unrecognized, blaming the document for the caller's misconfiguration.
func New(opts ...Option) (*Engine, error) {
	cfg := config{compilers: Compilers(), passes: Passes()}
	for i, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("engine: option %d is nil", i)
		}
		opt(&cfg)
	}
	if len(cfg.compilers) == 0 {
		return nil, errors.New("engine: no compilers; an engine with none can compile nothing")
	}

	reg := compilers.NewRegistry()
	for i, c := range cfg.compilers {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("engine: register compiler %d: %w", i, err)
		}
	}
	passes, err := bindPasses(cfg.passes)
	if err != nil {
		return nil, err
	}
	return &Engine{registry: reg, passes: passes, verifier: irverify.Verify}, nil
}

// Run executes the pipeline for the spec at specPath: read it, compile it with
// the compiler that recognizes it, run the passes opts.DisablePasses does not
// name, and judge the Result at opts.FailOn. References are read through
// reader.Disk, as far as the compiler's options allow.
//
// A Go error is reserved for I/O, cancellation and programmer errors: an
// unreadable file, a cancelled ctx, a refused option (ErrUnknownPass,
// ErrUnknownFailOn), or a compiler or pass breaking its contract, a panic among
// the ways (compilers.ErrContract, pass.ErrContract). Everything wrong with the
// spec is a diagnostic. Calls may overlap; each owns the document it returns.
func (e *Engine) Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error) {
	p, err := e.prepare(opts)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("engine: read spec %q: %w", specPath, err)
	}
	res, err := e.compile(ctx, compilers.Source{Path: specPath, Data: data}, reader.Disk{}, os.ReadFile, opts, p)
	if err != nil {
		return nil, err
	}
	return p.failOn.judge(res)
}

// RunSources executes Run's pipeline over sources held in memory, and reads
// nothing else. The first source is the spec; every source is a document the
// spec's references, or a file a setting such as an overlay names, may name by
// its Path (reader.Memory), so a spec split across files compiles with no file
// written. Source.Parsed is not read, and the bytes must not change while the
// run reads them.
//
// An empty list and two sources naming one document are Go errors: the run is
// misconfigured. It shares Run's other contracts.
func (e *Engine) RunSources(ctx context.Context, sources []compilers.Source, opts RunOptions) (*Result, error) {
	p, err := e.prepare(opts)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, errors.New("engine: no sources; the first is the spec to compile")
	}
	docs, err := reader.NewMemory(sources)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	readFile := func(name string) ([]byte, error) {
		return docs.ReadDocument(ctx, compilers.DocumentRequest{Location: name})
	}
	spec := compilers.Source{Path: sources[0].Path, Data: sources[0].Data}
	res, err := e.compile(ctx, spec, docs, readFile, opts, p)
	if err != nil {
		return nil, err
	}
	return p.failOn.judge(res)
}

// RunDocument runs the passes not named in opts.DisablePasses over doc, a
// document already compiled, such as a decoded file, and judges the Result at
// opts.FailOn. No compiler runs, so opts may name no compiler option and
// Result.Format is zero. Result.Diagnostics starts from doc's own findings.
//
// doc is not modified. The Result's document may share any field of doc but
// Diagnostics, so neither may change while the other is read. Go errors: a nil
// doc, one whose IRVersion this build cannot read, one holding a finding
// indexing none of its Sources, one breaking the IR's invariants under
// opts.Verify, and compiler options.
func (e *Engine) RunDocument(ctx context.Context, doc *ir.Document, opts RunOptions) (*Result, error) {
	p, err := e.prepare(opts)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errors.New("engine: no document to run the passes over")
	}
	if opts.FormatOptions != nil || len(opts.CompilerOptions) > 0 {
		return nil, errCompilerOptionsUnused
	}
	// An unstamped document was never stamped by its producer, where a
	// mismatched one was paired with the wrong build (ir.CompatibleVersion).
	if doc.IRVersion == "" {
		return nil, fmt.Errorf("engine: document carries no IR version; this build reads %q", ir.IRVersion)
	}
	if !ir.CompatibleVersion(doc.IRVersion) {
		return nil, fmt.Errorf("engine: document IR version %q is not %q, the one this build reads",
			doc.IRVersion, ir.IRVersion)
	}
	// Its findings join the Result as they are, and no compiler made them, so
	// they are held to the table here, as a compile's and a pass's are.
	if d, outside := ir.FirstOutsideTable(doc.Diagnostics, len(doc.Sources)); outside {
		return nil, fmt.Errorf("engine: document finding %s indexes source %d of %d",
			d.Code, d.Provenance.Source, len(doc.Sources))
	}
	// Run refuses a cancelled ctx whatever passes are on, and so does this,
	// though with every pass off no pass would see it.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("engine: stop before the passes: %w", err)
	}
	sum, err := p.hold("input document", nil, doc)
	if err != nil {
		return nil, err
	}
	out, found, err := p.runPasses(ctx, doc, sum)
	if err != nil {
		return nil, err
	}
	// The engine's own write, Diagnostics, goes into a copy, as no pass mutates
	// the document it is handed; a copy of the list keeps the Result from sharing
	// the caller's slice.
	out = withFindings(out, ir.MergeDiagnostics(slices.Clone(doc.Diagnostics), found))
	return p.failOn.judge(&Result{Document: out, Diagnostics: out.Diagnostics, Sources: out.Sources})
}

// plan is what a run needs once its options are checked: the passes it runs,
// in order, the threshold it is judged at, and the verifier it holds each stage
// to, if any.
type plan struct {
	passes []namedPass
	failOn threshold
	// verify is the verifier a verified run holds each document to, nil for a
	// run that does not verify.
	verify verifier
}

// prepare checks what every entry point checks before it reads anything, and
// returns the plan opts leave: the passes they leave on and the threshold the
// run is judged at.
//
// An engine no constructor built is the caller's mistake whatever the input
// says, and reporting the input first would send them to look at it;
// unchecked, the nil registry panics during detection. A run configured two
// ways is wrong whatever the source, and every compiler would otherwise be
// asked to resolve options no compiler could.
func (e *Engine) prepare(opts RunOptions) (plan, error) {
	if e == nil || e.registry == nil {
		return plan{}, errors.New("engine: uninitialized; build one with New")
	}
	if opts.FormatOptions != nil && len(opts.CompilerOptions) > 0 {
		return plan{}, errOptionChannels
	}

	passes, err := e.enabledPasses(opts.DisablePasses)
	if err != nil {
		return plan{}, err
	}
	failOn, err := opts.FailOn.resolve()
	if err != nil {
		return plan{}, err
	}
	p := plan{passes: passes, failOn: failOn}
	if opts.Verify {
		p.verify = e.verifier
	}
	return p, nil
}

// compile runs the pipeline over source: detect, compile, then passes. docs
// reads what source's references name, and readFile what its settings name.
// The Result it returns is not yet judged.
func (e *Engine) compile(ctx context.Context, source compilers.Source, docs compilers.DocumentReader,
	readFile func(string) ([]byte, error), opts RunOptions, p plan,
) (*Result, error) {
	// Each compiler is asked under the options it would compile with, because
	// recognizing a source is a read of it that the caller's budgets bound as
	// much as the compile's reads.
	det, ok, err := e.registry.Detect(ctx, source, func(c compilers.Compiler) (compilers.Options, error) {
		formatOpts, err := formatOptions(c, opts, readFile)
		return compilers.Options{FormatOptions: formatOpts, Reader: docs}, err
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
	// A verified run holds the document to the invariants before any pass reads it.
	sum, err := p.hold(fmt.Sprintf("compile %q", source.Path), compilers.ErrContract, doc)
	if err != nil {
		return nil, err
	}
	// Both channels end up carrying the whole list: the Result is what a caller
	// gates on, and the document is what gets persisted (golden snapshots, IR
	// diff, caches, emitters). The compiler's findings, both of its channels
	// already merged by the detection, are taken before any pass runs, since the
	// passes' contract leaves Document.Diagnostics to the engine: a transform
	// that builds a fresh document cannot lose what the compiler stored on the
	// old one.
	doc, found, err := p.runPasses(ctx, doc, sum)
	if err != nil {
		return nil, err
	}
	// The write goes into a copy: the compiler may keep the document it returned,
	// and runs overlap.
	doc = withFindings(doc, ir.MergeDiagnostics(compiled, found))
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
// New builds every Engine and refuses an empty compiler set, so there is
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
// textual channel through c itself. readFile is what makes a path-valued
// setting work: the engine reads the spec, so loading a file a setting names
// keeps the I/O on this side of the contract and the compiler pure.
func formatOptions(c compilers.Compiler, opts RunOptions, readFile func(string) ([]byte, error)) (any, error) {
	if len(opts.CompilerOptions) == 0 {
		return opts.FormatOptions, nil
	}
	decoded, err := c.DecodeOptions(compilers.OptionSet{
		Settings: opts.CompilerOptions,
		ReadFile: readFile,
	})
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return decoded, nil
}
