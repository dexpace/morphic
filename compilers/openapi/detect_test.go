// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

const budgetedSpec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"

func withBytes(limit int) compilers.Options {
	return compilers.Options{FormatOptions: openapi.Options{Limits: openapi.Limits{MaxSourceBytes: limit}}}
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

	rec, diags, ok := openapi.New().Detect(t.Context(), src, withBytes(limit))

	assert.False(t, ok)
	assert.Equal(t, compilers.Recognition{}, rec)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, 0, diags[0].Provenance.Source, "detection names the one source it was handed")

	_, compiled, err := openapi.New().Compile(t.Context(), []compilers.Source{src}, withBytes(limit))
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	assert.Equal(t, compiled[0].Message, diags[0].Message, "detection and the compile refuse the source alike")
}

// TestDetect_ReadsASourceWithinTheCallersByteBudget pins the other side,
// including a caller who turned the budget off. That case is asserted on a
// small source: the byte budget is the only bound on what detection reads, so
// off means unbounded, but a source past the 64 MiB default costs seconds and
// up to gigabytes, by how many nodes it holds. It was measured by hand instead
// when the byte scan was deleted (GitHub #486): with the budget off, a source
// past 64 MiB is recognized.
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
		rec, diags, ok := openapi.New().Detect(t.Context(), src, opts)
		require.True(t, ok, "%+v: %+v", opts, diags)
		assert.Equal(t, want, rec.Format, "%+v", opts)
	}
}

// TestDetect_ReadsTheVersionTheCompileReads holds detection to the compile on
// the roots where a reading of its own disagreed with the parser's: each is
// routed as the version its compile lowers, the agreement
// compilers.Detection.Compile checks. A key repeated over an unreadable value,
// an alias in either place, a key tagged `!!merge` that is not `<<`, an alias
// key outranking a merge, and a chain deeper than the old walk followed.
func TestDetect_ReadsTheVersionTheCompileReads(t *testing.T) {
	t.Parallel()
	const tail = "info: {title: t, version: '1'}\npaths: {}\n"
	cases := map[string]string{
		"a version over an unreadable value": "openapi: [x]\nopenapi: 3.1.0\n",
		"an alias as the key":                "k: &k openapi\n*k : 3.1.0\n",
		"an alias as the value":              "v: &v 3.1.0\nopenapi: *v\n",
		"a key tagged !!merge that is no <<": "!!merge openapi: 3.1.0\n",
		"an alias key outranking a merge":    "b: &b {openapi: 3.0.3}\nk: &k openapi\n<<: *b\n*k : 3.1.0\n",
		"a merge chain ten mappings deep":    string(openapitest.RootMergeChain(10, "openapi: 3.1.0", "")),
		"a merge written before its own key": "b: &b {openapi: 3.0.3}\n<<: *b\nopenapi: 3.1.0\n",
		"the first of two merges that agree": "a: &a {openapi: 3.1.0}\nb: &b {openapi: 3.0.3}\n<<: [*a, *b]\n",
	}
	for name, root := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := compilers.Source{Path: "api.yaml", Data: []byte(root + tail)}
			rec, diags, ok := openapi.New().Detect(t.Context(), src, compilers.Options{})
			require.True(t, ok, "the compile lowers it, so detection claims it: %v", diags)

			doc, compiled, err := openapi.New().Compile(t.Context(), []compilers.Source{src}, compilers.Options{})
			require.NoError(t, err)
			require.NotNil(t, doc, "%v", compiled)
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.1"}, rec.Format)
			assert.Equal(t, rec.Format.String(), doc.Sources[0].Format, "detection and the compile read one version")
		})
	}
}

// TestCompiler_SourceLimitReadsTheCallersOptions pins the limit detection, and
// a caller reading a source for it, takes from each kind of options, another
// compiler's included, which mean this one's default as
// compilers.Compiler.Detect requires rather than no limit. Detect's own tests
// exceed only a budget the caller sets: a source past the default costs seconds
// and up to gigabytes (TestDetect_ReadsASourceWithinTheCallersByteBudget), so
// another compiler's options read as no limit would pass them all.
func TestCompiler_SourceLimitReadsTheCallersOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts compilers.Options
		want int
	}{
		{"no options", compilers.Options{}, openapi.DefaultMaxSourceBytes},
		{"another compiler's options", compilers.Options{FormatOptions: "another compiler's options"}, openapi.DefaultMaxSourceBytes},
		{"a budget of its own", compilers.Options{FormatOptions: openapi.Options{Limits: openapi.Limits{MaxSourceBytes: 7}}}, 7},
		{"the budget turned off", compilers.Options{FormatOptions: openapi.Options{Limits: openapi.Limits{MaxSourceBytes: -1}}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, openapi.New().SourceLimit(tc.opts))
		})
	}
}
