package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

const budgetedSpec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"

func withBytes(limit int) compilers.Options {
	return compilers.Options{FormatOptions: Options{Limits: Limits{MaxSourceBytes: limit}}}
}

// TestDetect_DeclinesASourcePastTheCallersByteBudget pins that recognition is
// held to the budget the compile is. A source past it is declined unread and
// the decline says why, in the words Compile uses for the same source, since
// "nothing recognized it" would send the caller to the document rather than to
// the budget they set.
func TestDetect_DeclinesASourcePastTheCallersByteBudget(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "api.yaml", Data: []byte(budgetedSpec)}
	limit := len(budgetedSpec) - 1

	rec, diags, ok := New().Detect(src, withBytes(limit))

	assert.False(t, ok)
	assert.Equal(t, compilers.Recognition{}, rec)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, 0, diags[0].Provenance.Source, "detection names the one source it was handed")

	_, compiled, err := New().Compile(t.Context(), []compilers.Source{src}, withBytes(limit))
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	assert.Equal(t, compiled[0].Message, diags[0].Message, "detection and the compile refuse the source alike")
}

// TestDetect_ReadsASourceWithinTheCallersByteBudget pins the other side,
// including a caller who turned the budget off. That case is asserted on a
// small source: the byte budget is the only bound on what detection reads, so
// off means unbounded, but a source past the 64 MiB default costs 15 s and
// 1.3 GB under -race, which is what the gate's only test step runs with. It
// was measured by hand instead when the byte scan was deleted (GitHub #486):
// with the budget off, a source past 64 MiB is recognized.
func TestDetect_ReadsASourceWithinTheCallersByteBudget(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "api.yaml", Data: []byte(budgetedSpec)}
	want := compilers.SourceFormat{Name: "openapi", Version: "3.1"}

	for _, opts := range []compilers.Options{
		withBytes(len(budgetedSpec)),
		withBytes(-1),
		{},
		{FormatOptions: "another compiler's options"},
	} {
		rec, diags, ok := New().Detect(src, opts)
		require.True(t, ok, "%+v: %+v", opts, diags)
		assert.Equal(t, want, rec.Format, "%+v", opts)
	}
}
