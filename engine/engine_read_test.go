package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// limitedCompiler reads a source no further than limit, declining one past it
// with a reason as the contract asks, and records how many bytes of the source
// each Detect was handed.
type limitedCompiler struct {
	stubFront
	format compilers.SourceFormat
	limit  int
	handed *[]int
}

func (c limitedCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{c.format}
}

func (c limitedCompiler) SourceLimit(compilers.Options) int { return c.limit }

func (c limitedCompiler) Detect(src compilers.Source, _ compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	*c.handed = append(*c.handed, len(src.Data))
	if c.limit > 0 && len(src.Data) > c.limit {
		return compilers.Recognition{}, []ir.Diagnostic{{Severity: ir.SeverityError, Code: "x/too-large",
			Message: c.format.Name + " reads no more"}}, false
	}
	return compilers.Recognition{Format: c.format}, nil, true
}

func (c limitedCompiler) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: ir.IRVersion, Sources: sourcesOf(c.format.String(), sources)}, nil, nil
}

// TestEngine_RunReadsTheSpecNoFurtherThanACompilerWould pins how much of a spec
// Run reads: all of it while a compiler would read it all, and past the largest
// limit any compiler sets, that limit and one byte more, which is as far as
// detection needs to read to decline it. Every compiler is then handed the same
// bytes, and the run reports each one's reason for declining.
func TestEngine_RunReadsTheSpecNoFurtherThanACompilerWould(t *testing.T) {
	t.Parallel()
	const size = 1000
	tests := []struct {
		name     string
		limits   []int
		handed   int
		declined bool
	}{
		{name: "a limit past the spec", limits: []int{2000}, handed: size},
		{name: "a limit at the spec", limits: []int{size}, handed: size},
		{name: "no limit", limits: []int{0}, handed: size},
		{name: "a limit under the spec", limits: []int{10}, handed: 11, declined: true},
		{name: "two limits under the spec", limits: []int{10, 20}, handed: 21, declined: true},
		{name: "one compiler with no limit", limits: []int{10, 0}, handed: size},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var handed []int
			var cs []compilers.Compiler
			for i, limit := range tc.limits {
				format := compilers.SourceFormat{Name: string(rune('a' + i)), Version: "1"}
				cs = append(cs, limitedCompiler{format: format, limit: limit, handed: &handed})
			}
			eng, err := engine.New(engine.WithCompilers(cs...), engine.WithPasses())
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), writeSpec(t, strings.Repeat("x", size)), engine.RunOptions{})

			require.NoError(t, err)
			require.NotEmpty(t, handed)
			for _, n := range handed {
				assert.Equal(t, tc.handed, n, "every compiler asked was handed the bytes read")
			}
			if tc.declined {
				assert.Nil(t, res.Document)
				assert.Len(t, res.Diagnostics, len(tc.limits), "each compiler's own reason")
			}
		})
	}
}

// TestEngine_RunReadsASpecPastTheBudgetAsTheCompilerRefusesIt runs the bound
// with the OpenAPI compiler and its textual budget: a spec past
// max-source-bytes is declined with the compiler's own refusal, naming the
// budget the caller set, a diagnostic and not a Go error.
func TestEngine_RunReadsASpecPastTheBudgetAsTheCompilerRefusesIt(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	spec := writeSpec(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\nx-pad: "+
		strings.Repeat("p", 4096)+"\n")

	res, err := eng.Run(t.Context(), spec, engine.RunOptions{
		CompilerOptions: map[string]string{"max-source-bytes": "1000"},
	})

	require.NoError(t, err)
	assert.Nil(t, res.Document)
	assert.Equal(t, compilers.SourceFormat{}, res.Format, "detection read nothing past the budget, so named no format")
	require.Len(t, res.Diagnostics, 1, "%+v", res.Diagnostics)
	assert.Equal(t, "openapi/budget-exceeded", res.Diagnostics[0].Code)
	assert.Equal(t, "source document is past the 1000-byte budget", res.Diagnostics[0].Message)
	assert.True(t, res.Failed)
}

// TestEngine_RunRefusesASpecThatIsNoRegularFile pins the refusal of a spec
// path naming something other than a regular file, a directory here, which
// every platform has: a Go error before anything is read, as for a FIFO, whose
// open would wait for a writer, or a device, which can stream without end.
func TestEngine_RunRefusesASpecThatIsNoRegularFile(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	dir := t.TempDir()

	res, err := eng.Run(t.Context(), dir, engine.RunOptions{})

	require.ErrorIs(t, err, reader.ErrNotRegular)
	assert.Contains(t, err.Error(), "engine: read spec")
	assert.Nil(t, res)
}

// TestEngine_RunAsksTheLimitBeforeReading pins the order: how far to read is
// the compilers' to say, so a compiler breaking its contract there fails the
// run before the spec is read, here one that does not exist.
func TestEngine_RunAsksTheLimitBeforeReading(t *testing.T) {
	t.Parallel()
	eng := engineOf(t, faulty{limit: func() int { panic("boom") }})

	res, err := eng.Run(t.Context(), "no/such/spec.yaml", engine.RunOptions{})

	requireBreach(t, err, compilers.ErrContract, "boom")
	assert.Contains(t, err.Error(), "SourceLimit")
	assert.Nil(t, res)
}

// TestEngine_RunReadsANamedOverlayInTheCompile pins the overlay's read through
// the engine: named by a setting, read by the compile through reader.Disk,
// held to the budget a source is, and refused past it with a diagnostic
// naming the overlay, while one that is no regular file is a Go error, as an
// unreadable spec is.
func TestEngine_RunReadsANamedOverlayInTheCompile(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	spec := writeSpec(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")
	overlay := writeNamed(t, "overlay.yaml", "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n"+
		"actions:\n  - target: $.info\n    update: {description: patched}\n")

	t.Run("applied", func(t *testing.T) {
		t.Parallel()
		res, err := eng.Run(t.Context(), spec, engine.RunOptions{CompilerOptions: map[string]string{"overlay": overlay}})

		require.NoError(t, err)
		require.NotNil(t, res.Document, "%+v", res.Diagnostics)
		assert.Equal(t, "patched", res.Document.Docs.Description)
		require.Len(t, res.Document.Sources, 2)
		assert.Equal(t, overlay, res.Document.Sources[1].Path)
	})
	t.Run("past the budget", func(t *testing.T) {
		t.Parallel()
		res, err := eng.Run(t.Context(), spec, engine.RunOptions{CompilerOptions: map[string]string{
			"overlay": overlay, "max-source-bytes": "80",
		}})

		require.NoError(t, err)
		assert.Nil(t, res.Document)
		require.Len(t, res.Diagnostics, 1, "%+v", res.Diagnostics)
		assert.Equal(t, "overlay document is past the 80-byte budget", res.Diagnostics[0].Message)
		assert.Equal(t, 1, res.Diagnostics[0].Provenance.Source, "the overlay is what crossed it")
		assert.Equal(t, overlay, res.Sources[1].Path)
	})
	t.Run("no regular file", func(t *testing.T) {
		t.Parallel()
		res, err := eng.Run(t.Context(), spec, engine.RunOptions{CompilerOptions: map[string]string{
			"overlay": t.TempDir(),
		}})

		require.ErrorIs(t, err, reader.ErrNotRegular)
		assert.Contains(t, err.Error(), "openapi: read overlay")
		assert.Nil(t, res)
	})
}
