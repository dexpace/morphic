package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
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
	assert.Equal(t, ir.NoSource, diags[0].Provenance.Source, "detection has no source table to index")

	_, compiled, err := New().Compile(t.Context(), []compilers.Source{src}, withBytes(limit))
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	assert.Equal(t, compiled[0].Message, diags[0].Message, "detection and the compile refuse the source alike")
}

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
