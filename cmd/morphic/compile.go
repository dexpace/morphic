package main

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// newEngine constructs the pipeline engine. It is a package var so tests can
// inject a construction failure — the real engine.New registers exactly one
// compiler into a fresh registry, so neither of Register's failure modes (no
// formats reported, a format collision) can actually fire — and a stub engine
// that returns a nil Document.
var newEngine = engine.New

// outputFile is the temp file that -o writes through. Sync is part of the
// contract rather than an implementation detail: Close returns once the bytes
// reach the page cache, so without an explicit Sync the publishing rename can
// expose a file the filesystem has not taken yet.
type outputFile interface {
	io.WriteCloser
	Sync() error
}

// createOutput creates the temp file that -o writes through. It is a package var
// so tests can inject an outputFile whose Write, Sync or Close fails; a real
// *os.File's Close does not fail after a successful write on the platforms
// Morphic targets.
var createOutput = func(path string, perm os.FileMode) (outputFile, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

// chmodOutput restores a replaced destination's mode onto the temp file. It is a
// package var so tests can inject a chmod failure, which is otherwise reachable
// only by racing the filesystem.
var chmodOutput = os.Chmod

// renameOutput publishes a fully written temp file over the destination. It is a
// package var so tests can inject a rename failure, which is otherwise reachable
// only by racing the filesystem.
var renameOutput = os.Rename

// maxTempAttempts bounds the search for an unused temp-file name. Each attempt
// draws a fresh random suffix, so exhausting it means the destination directory
// is unusable rather than that the names happened to collide.
const maxTempAttempts = 10

// newFilePerm is the mode a temp file is created with: the same 0666 os.Create
// requests, so the umask narrows it identically. A destination that already
// exists overrides this via chmodOutput — see destMode.
const newFilePerm os.FileMode = 0o666

// newCompileCommand builds compile's command-table entry. It is a function, not
// a package-level var — see commands.
func newCompileCommand() command {
	return command{
		name:    "compile",
		summary: "lower an API spec (OpenAPI 3.x) into Morphic IR JSON",
		usage:   "morphic compile <spec-file> [flags]",
		description: "Lower an API spec (OpenAPI 3.x) into Morphic IR JSON on stdout, and write\n" +
			"diagnostics to stderr. Output to stdout is indented for reading; a file\n" +
			"written with -o is compact unless --pretty asks for the indented form.\n\n" +
			"--explain reports what compiling produced at one source coordinate — the\n" +
			"type node interned there, the coordinates interned beneath it, and the\n" +
			"diagnostics stamped at it — instead of writing the document. The coordinate\n" +
			"is a JSON Pointer, so '' is the whole document.\n\n" +
			"--disable-pass turns off the named IR pass, and may be repeated; a name no\n" +
			"pass carries is refused (exit 2). --skip-validate is its alias for validate.\n\n" +
			"--opt passes a setting to the compiler the spec selects, which names and\n" +
			"validates its own options; morphic itself knows none of them. The OpenAPI\n" +
			"compiler's are listed in the README.\n\n" +
			"A -- argument ends flag parsing: every argument after it is an operand,\n" +
			"even one that begins with a dash, which is how a spec file named like a\n" +
			"flag is passed.",
		printFlags: func(w io.Writer) {
			fs, _ := newCompileFlags()
			fs.SetOutput(w)
			fs.PrintDefaults()
		},
		bind: bindCompile,
	}
}

// specOptions holds what the shared pipeline runner reads, parsed by the flags
// every spec-taking command defines. validate is compile's pipeline with the
// document dropped, so the two configure that pipeline the same way: a spec that
// needs an option to compile needs it to be validated as it will be compiled.
type specOptions struct {
	failOn       string
	skipValidate bool
	disable      passFlag
	settings     settingFlag
}

// disabledPasses is the pass names the flags turn off: --disable-pass's, plus
// validate when --skip-validate, its alias, was given. A name given both ways
// is listed once.
func (o specOptions) disabledPasses() []string {
	names := slices.Clone(o.disable)
	if o.skipValidate && !slices.Contains(names, engine.ValidatePass) {
		names = append(names, engine.ValidatePass)
	}
	return names
}

// compileOptions holds the values compile's flags parse into.
type compileOptions struct {
	specOptions
	outPath string
	explain pointerFlag
	pretty  bool
}

// bindSpecFlags registers the flags every spec-taking command shares onto fs.
// Sharing the registration is what keeps their spellings, defaults and help
// text identical across commands rather than identical-looking.
func bindSpecFlags(fs *flag.FlagSet, opts *specOptions) {
	opts.settings = settingFlag{}

	fs.StringVar(&opts.failOn, "fail-on", "error",
		"fail (exit 1) on diagnostics at or above this severity: error|warning")
	fs.BoolVar(&opts.skipValidate, "skip-validate", false,
		"alias for --disable-pass validate")
	fs.Var(&opts.disable, "disable-pass",
		"disable the named IR pass (repeatable)")
	fs.Var(opts.settings, "opt",
		"set one `key=value` option on the compiler the spec selects (repeatable)")
}

// newCompileFlags returns compile's FlagSet and the options its flags write
// into. Directing Output to io.Discard is the whole of the silencing — the flag
// package writes both its error line and its usage dump there — so parse
// failures and help requests reach the CLI only as errors from Parse, and the
// CLI renders exactly one text for them.
func newCompileFlags() (*flag.FlagSet, *compileOptions) {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts compileOptions
	bindSpecFlags(fs, &opts.specOptions)
	fs.StringVar(&opts.outPath, "o", "", "write IR JSON to this file instead of stdout")
	fs.Var(&opts.explain, "explain",
		"report what compiling produced at this source `pointer` instead of writing IR JSON; '' is the whole document")
	fs.BoolVar(&opts.pretty, "pretty", false,
		"indent the IR JSON -o writes; stdout is indented either way")

	return fs, &opts
}

// bindCompile parses compile's arguments and returns the compile they ask for.
func bindCompile(args []string) (work, error) {
	fs, opts := newCompileFlags()

	specPath, err := bindSpec(fs, "compile", args, &opts.specOptions)
	if err != nil {
		return nil, err
	}

	return func(stdout, stderr io.Writer) int {
		return compileSpec(specPath, *opts, stdout, stderr)
	}, nil
}

// bindSpec parses what every spec-taking command takes — its flags in any
// position, then the one spec file it works on — and returns that file's path.
// name is the command's own, so a misuse names the command that was typed.
//
// Every error it returns is rendered by the caller's dispatch, flag.ErrHelp
// included: the flag package reports a help request as an error from Parse, and
// passing it up unwrapped is what lets one place answer -h for every command.
func bindSpec(fs *flag.FlagSet, name string, args []string, opts *specOptions) (string, error) {
	positional, err := parseArgs(fs, args)
	if err != nil {
		return "", err
	}
	if opts.failOn != string(ir.SeverityError) && opts.failOn != string(ir.SeverityWarning) {
		return "", fmt.Errorf("invalid --fail-on %q (want %s or %s)",
			opts.failOn, ir.SeverityError, ir.SeverityWarning)
	}
	if len(positional) != 1 {
		return "", fmt.Errorf("%s requires exactly one spec file", name)
	}
	return positional[0], nil
}

// runPipeline runs the engine over specPath and renders every diagnostic the
// run produced to stderr.
//
// ok is false when there is no document to work with — the engine could not be
// built, the run failed, or it lowered nothing — and code is then the exit code
// to return. Otherwise res holds the document and code is the exit code the
// diagnostics call for, which the caller returns once it has emitted whatever
// it emits.
func runPipeline(specPath string, opts specOptions, stderr io.Writer) (*engine.Result, int, bool) {
	eng, err := newEngine()
	if err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return nil, 2, false
	}

	res, err := eng.Run(context.Background(), specPath, engine.RunOptions{
		CompilerOptions: opts.settings,
		DisablePasses:   opts.disabledPasses(),
	})
	if err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return nil, 2, false
	}

	renderDiagnostics(stderr, res)
	if res.Document == nil {
		return nil, 1, false
	}
	return res, exitCodeFor(res.Diagnostics, opts.failOn), true
}

// compileSpec runs the pipeline over specPath, writes the IR document and its
// diagnostics, and returns the process exit code.
func compileSpec(specPath string, opts compileOptions, stdout, stderr io.Writer) int {
	res, code, ok := runPipeline(specPath, opts.specOptions, stderr)
	if !ok {
		return code
	}

	if opts.explain.set {
		explainDocument(stdout, res.Document, res.Diagnostics, opts.explain.pointer)
		return code
	}
	if err := writeCompiled(opts, stdout, res.Document); err != nil {
		emitf(stderr, "morphic: %v\n", err)
		return writeFailureExit(code)
	}
	return code
}

// writeFailureExit returns the exit code for a run whose diagnostics earned
// code and whose output then could not be written.
//
// A failed write does not overwrite a non-zero code. Whether a destination can
// be written is a property of the destination (/dev/null and a read-only
// directory both refuse replaceFile's temp file), not of the spec, so the
// verdict on the spec keeps the exit code and 2 is left for runs that failed
// for a reason outside the spec.
func writeFailureExit(code int) int {
	if code != 0 {
		return code
	}
	return 2
}

// parseArgs binds fs and collects positional arguments, tolerating flags that
// appear either before or after the spec path (stdlib flag stops at the first
// non-flag argument, so it is invoked once per positional).
//
// A "--" ends flag parsing for the whole invocation, so it is split off before
// the loop. Parse consumes the marker without reporting it, so a later round
// would re-enable flag parsing for operands the user had marked as such.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	before, operands := splitAtTerminator(fs, args)

	var positional []string
	rest := before
	for {
		if err := fs.Parse(rest); err != nil {
			// Returned verbatim, not wrapped: this error is rendered straight to
			// the user, and the flag package's messages already name the flag.
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return append(positional, operands...), nil
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
}

// renderDiagnostics writes each diagnostic to w, one per line, as
// "<severity> <code> <location>: <message>". This is the sole place in the
// pipeline where diagnostics are rendered for a human.
func renderDiagnostics(w io.Writer, res *engine.Result) {
	for _, d := range res.Diagnostics {
		emitf(w, "%s %s%s: %s\n",
			d.Severity, d.Code, location(res.Sources, d.Provenance), d.Message)
	}
}

// location renders the "where" of a diagnostic line, leading space included:
// " <path>#<pointer>" for a pointer, " <path>:<line>:<column>" for a position
// (the form editors and terminals turn into a link), " <path>" for the source
// as a whole, and the bare node for an IR-space node. A finding with several
// locators is spelled by the first of them in that order.
//
// sources is the run's table, which a refused compile has too. A locator whose
// index names no entry in it is spelled without a path, and a finding naming
// neither a source nor a node prints nothing rather than a guess.
func location(sources []ir.SourceInfo, prov ir.Provenance) string {
	path := sourcePath(sources, prov.Source)
	switch {
	case prov.Pointer != "":
		return " " + onPath(path, "#", string(prov.Pointer))
	case prov.Position.Line > 0:
		return " " + onPath(path, ":", positionText(prov.Position))
	case path != "":
		return " " + path
	case prov.Node != "":
		return " " + prov.Node
	default:
		return ""
	}
}

// onPath spells locator against path, joined by sep, or alone when no path
// resolved.
func onPath(path, sep, locator string) string {
	if path == "" {
		return locator
	}
	return path + sep + locator
}

// positionText spells a position as <line>:<column>, or <line> alone when the
// producer knows no column.
func positionText(p ir.Position) string {
	if p.Column > 0 {
		return strconv.Itoa(p.Line) + ":" + strconv.Itoa(p.Column)
	}
	return strconv.Itoa(p.Line)
}

// sourcePath resolves a diagnostic's source index to its file path, returning
// "" when the index addresses no entry of sources.
func sourcePath(sources []ir.SourceInfo, source int) string {
	if source < 0 || source >= len(sources) {
		return ""
	}
	return sources[source].Path
}

// exitCodeFor returns 1 when any diagnostic is at or above the failOn severity,
// otherwise 0. failOn is one of "error" or "warning".
func exitCodeFor(diags []ir.Diagnostic, failOn string) int {
	threshold := severityRank(ir.Severity(failOn))
	for _, d := range diags {
		if severityRank(d.Severity) >= threshold {
			return 1
		}
	}
	return 0
}

// severityRank orders severities so a threshold comparison is a plain integer
// compare: error > warning > info > unknown.
func severityRank(s ir.Severity) int {
	switch s {
	case ir.SeverityError:
		return 3
	case ir.SeverityWarning:
		return 2
	case ir.SeverityInfo:
		return 1
	default:
		return 0
	}
}

// writeCompiled emits doc's IR JSON to opts.outPath, or to stdout when it is
// empty. Stdout is indented because a person is reading it; a file is compact
// unless --pretty asks for the indented form.
//
// A file is encoded straight into replaceFile's temp file, which saves a copy
// of the output; a document that will not marshal still leaves outPath
// untouched, since only the rename reaches it. Stdout has no rename to
// withhold, so it is encoded whole into a buffer and receives the document
// entirely or not at all.
func writeCompiled(opts compileOptions, stdout io.Writer, doc *ir.Document) error {
	if opts.outPath == "" {
		var buf bytes.Buffer
		if err := encodeDocument(&buf, doc, true); err != nil {
			return err
		}
		return writeRaw(stdout, buf.Bytes())
	}
	return replaceFile(opts.outPath, func(w io.Writer) error {
		return encodeDocument(w, doc, opts.pretty)
	})
}

// replaceFile atomically replaces outPath with what fill produces: it writes a
// temp file in outPath's directory, which keeps the rename on one filesystem,
// then renames it over outPath. A failure leaves the previous content.
//
// Costs of never writing in place: the directory must accept a new entry
// (-o /dev/null fails; see writeFailureExit); a symlink or FIFO at outPath is
// replaced, not written through; other hard links keep the old content. The
// temp name is 21 characters longer, so a near-limit basename can fail to
// create. The directory is not synced: a crash can leave the previous content,
// never a partial one.
func replaceFile(outPath string, fill func(io.Writer) error) error {
	perm, replacing, err := destMode(outPath)
	if err != nil {
		return err
	}

	tmp, err := writeTemp(outPath, fill)
	if err != nil {
		return err
	}

	// A brand-new file keeps the umask-narrowed mode it was created with; one
	// that replaces an existing destination inherits that destination's mode,
	// which is what truncating it in place would have preserved.
	if replacing {
		if err := chmodOutput(tmp, perm); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("chmod output %q: %w", outPath, err)
		}
	}

	if err := renameOutput(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace output %q: %w", outPath, err)
	}
	return nil
}

// destMode reports the mode an existing destination carries, and whether there
// is one at all. A missing destination is the ordinary case, not an error.
func destMode(outPath string) (os.FileMode, bool, error) {
	info, err := os.Stat(outPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("stat output %q: %w", outPath, err)
	}
	return info.Mode().Perm(), true, nil
}

// writeTemp runs fill into a newly created file beside outPath and returns that
// file's path. Creation is O_EXCL under a random name, so it neither clobbers an
// existing file nor follows a symlink planted at the name it drew; that makes the
// unpredictability of the suffix a convenience, not a security boundary.
func writeTemp(outPath string, fill func(io.Writer) error) (string, error) {
	dir := filepath.Dir(outPath)
	base := filepath.Base(outPath)

	for range maxTempAttempts {
		tmp := filepath.Join(dir, fmt.Sprintf(".%s.tmp%016x", base, rand.Uint64()))
		f, err := createOutput(tmp, newFilePerm)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create output %q: %w", outPath, err)
		}
		if err := fillTemp(f, tmp, fill); err != nil {
			return "", err
		}
		return tmp, nil
	}
	return "", fmt.Errorf("create output %q: no unused temp name in %d attempts",
		outPath, maxTempAttempts)
}

// fillTemp runs fill into f, flushes it to the filesystem, and closes it,
// removing tmp if any step fails so a failed run leaves no debris beside the
// destination — including a fill that fails after writing part of its output.
//
// The Sync is what lets replaceFile's caller believe the rename publishes
// durable bytes: without it the rename can expose a file whose contents are
// still only in the page cache, and a crash before writeback leaves the
// destination short or empty — the same loss writing through a temp file exists
// to prevent.
func fillTemp(f outputFile, tmp string, fill func(io.Writer) error) error {
	if err := fill(f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync output %q: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close output %q: %w", tmp, err)
	}
	return nil
}

// destWriter passes writes through to w and records the error w returned. It is
// what keeps a destination that refused the bytes distinguishable from a
// document that would not marshal: json.MarshalWrite reports both as one error,
// and they are different things to tell a user about.
type destWriter struct {
	w   io.Writer
	err error
}

func (d *destWriter) Write(p []byte) (int, error) {
	n, err := d.w.Write(p)
	if err != nil {
		d.err = err
	}
	return n, err
}

// encodeDocument writes doc's IR JSON to w, ending in a newline either way.
//
// Both forms go through json.MarshalWrite, which streams the encoded bytes
// straight to w rather than building them in a slice first; indenting is just
// an encode option on that same call, not a separate route. MarshalWrite ends
// no value with a newline, so this writes one, which makes the indented form
// byte-for-byte what irtest.WriteGolden writes.
func encodeDocument(w io.Writer, doc *ir.Document, indent bool) error {
	dest := &destWriter{w: w}

	var err error
	if indent {
		err = json.MarshalWrite(dest, doc, jsontext.WithIndent("  "))
	} else {
		err = json.MarshalWrite(dest, doc)
	}
	if err != nil {
		if dest.err != nil {
			return fmt.Errorf("write ir document: %w", dest.err)
		}
		return fmt.Errorf("marshal ir document: %w", err)
	}
	return writeRaw(w, []byte("\n"))
}

// writeRaw writes raw to w, wrapping any error with context.
func writeRaw(w io.Writer, raw []byte) error {
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("write ir document: %w", err)
	}
	return nil
}
