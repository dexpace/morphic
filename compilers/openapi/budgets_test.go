package openapi

import (
	"context"
	"fmt"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// enumSpec is a minimal document whose one component declares a string enum of
// n members. The specs these tests need differ only in how large they are, and
// building them here is what keeps a multi-megabyte fixture out of the corpus.
func enumSpec(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n  schemas:\n    E:\n      type: string\n      enum:\n")
	for i := range n {
		fmt.Fprintf(&b, "        - m%d\n", i)
	}
	return b.String()
}

// compileBounded compiles src under limits, requiring the compile itself to
// succeed: every budget refusal is a diagnostic, never a Go error.
func compileBounded(t *testing.T, src string, limits Limits) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{FormatOptions: Options{Limits: limits}})
	require.NoError(t, err, "an over-budget input is a spec problem, not a Go error")
	return doc, diags
}

// messageOf returns the message of the first diagnostic carrying code.
func messageOf(t *testing.T, diags []ir.Diagnostic, code string) string {
	t.Helper()
	for _, d := range diags {
		if d.Code == code {
			return d.Message
		}
	}
	t.Fatalf("no diagnostic with code %q in %+v", code, diags)
	return ""
}

func TestCompile_EnumBudgetBindsOnlyPastTheLimit(t *testing.T) {
	t.Parallel()
	const limit = 4
	tests := []struct {
		name    string
		members int
		refused bool
	}{
		{"at the budget", limit, false},
		{"one past the budget", limit + 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileBounded(t, enumSpec(tc.members), Limits{MaxEnumMembers: limit})
			require.NotNil(t, doc, "an over-budget enum degrades one node; the document still lowers")
			node := typeByName(doc, "E")
			require.NotNil(t, node)

			if !tc.refused {
				requireNoErrorDiags(t, diags)
				enum, ok := node.(*ir.Enum)
				require.True(t, ok, "an enum inside the budget lowers as an Enum")
				assert.Len(t, enum.Members, tc.members)
				return
			}
			assertHasErrorCode(t, diags, diag.BudgetExceeded)
			assert.Equal(t, "enum declares 5 members, past the 4-member budget; lowered as any",
				messageOf(t, diags, diag.BudgetExceeded),
				"the refusal names what was observed and what the budget was")
			_, ok := node.(*ir.Any)
			assert.True(t, ok, "an enum past the budget lowers as the top type, not a truncated Enum")
		})
	}
}

func TestCompile_SourceByteBudgetBindsOnlyPastTheLimit(t *testing.T) {
	t.Parallel()
	src := enumSpec(3)
	tests := []struct {
		name    string
		limit   int
		refused bool
	}{
		{"at the budget", len(src), false},
		{"one byte past the budget", len(src) - 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileBounded(t, src, Limits{MaxSourceBytes: tc.limit})
			if !tc.refused {
				require.NotNil(t, doc)
				requireNoErrorDiags(t, diags)
				return
			}
			assert.Nil(t, doc, "an over-budget source is refused before it is parsed")
			assertHasErrorCode(t, diags, diag.BudgetExceeded)
			assert.Equal(t,
				fmt.Sprintf("source document is %d bytes, past the %d-byte budget", len(src), tc.limit),
				messageOf(t, diags, diag.BudgetExceeded))
		})
	}
}

func TestCompile_SourceNodeBudgetRefusesADocumentPastIt(t *testing.T) {
	t.Parallel()
	doc, diags := compileBounded(t, enumSpec(3), Limits{MaxSourceNodes: 1})

	assert.Nil(t, doc, "an over-budget node count is refused before the model is built")
	assertHasErrorCode(t, diags, diag.BudgetExceeded)
	assert.Contains(t, messageOf(t, diags, diag.BudgetExceeded), "past the 1-node budget")
}

// TestCompile_LargeDocumentInsideTheDefaultBudgetsIsLoweredWhole is the other
// half of every test above: the budgets must refuse what is pathological
// without refusing what is merely big. Three thousand members is far past any
// enum in a published description and still well inside every default.
func TestCompile_LargeDocumentInsideTheDefaultBudgetsIsLoweredWhole(t *testing.T) {
	t.Parallel()
	const members = 3000
	src := enumSpec(members)
	require.Less(t, len(src), DefaultMaxSourceBytes, "the fixture is inside the byte budget")

	doc, diags := compileBounded(t, src, Limits{})

	require.NotNil(t, doc)
	requireNoErrorDiags(t, diags)
	assert.False(t, hasDiag(diags, diag.BudgetExceeded), "a large but legal document is not refused for its size")
	enum, ok := typeByName(doc, "E").(*ir.Enum)
	require.True(t, ok)
	assert.Len(t, enum.Members, members, "every declared member reaches the IR")
}

func TestCompile_NegativeBudgetIsUnbounded(t *testing.T) {
	t.Parallel()
	const members = 5
	doc, diags := compileBounded(t, enumSpec(members),
		Limits{MaxSourceBytes: -1, MaxSourceNodes: -1, MaxEnumMembers: -1})

	require.NotNil(t, doc)
	requireNoErrorDiags(t, diags)
	enum, ok := typeByName(doc, "E").(*ir.Enum)
	require.True(t, ok, "a caller who turned the budgets off gets the pre-budget lowering")
	assert.Len(t, enum.Members, members)
}

func TestLimitsWithDefaults_FillsEveryUnsetBudgetAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Limits
		want Limits
	}{
		{
			"the zero value takes every default",
			Limits{},
			Limits{DefaultMaxSourceBytes, DefaultMaxSourceNodes, DefaultMaxEnumMembers},
		},
		{
			"a set budget is left alone and the others still default",
			Limits{MaxEnumMembers: 9},
			Limits{DefaultMaxSourceBytes, DefaultMaxSourceNodes, 9},
		},
		{
			"a negative budget is the caller asking for none",
			Limits{MaxSourceBytes: -1, MaxSourceNodes: -2, MaxEnumMembers: -3},
			Limits{-1, -2, -3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.in.withDefaults())
		})
	}
}

func TestOptionsWithDefaults_ResolvesTheLimits(t *testing.T) {
	t.Parallel()
	got := Options{}.withDefaults()

	assert.Equal(t, GroupByTags, got.Grouping)
	assert.Equal(t, Limits{}.withDefaults(), got.Limits, "the compiler resolves budgets once, at entry")
}

func TestBounded_TranslatesTheCallersSpellingOfUnbounded(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, bounded(-1), "negative is the public spelling of unbounded; zero is the internal one")
	assert.Equal(t, 0, bounded(0))
	assert.Equal(t, 7, bounded(7))
}

func TestLoadOptions_CarriesTheResolvedSizeBudgets(t *testing.T) {
	t.Parallel()
	got := loadOptions(Options{Limits: Limits{MaxSourceBytes: 11, MaxSourceNodes: -1}}.withDefaults())

	assert.Equal(t, 11, got.MaxSourceBytes)
	assert.Equal(t, 0, got.MaxSourceNodes, "an unbounded budget reaches the loader as no budget")
}

func TestCompile_CanceledContextStopsTheCompile(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	doc, _, err := New().Compile(ctx,
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(enumSpec(2))}}, compilers.Options{})

	require.ErrorIs(t, err, context.Canceled, "cancellation reaches the caller as a Go error, not a diagnostic")
	assert.Nil(t, doc, "a compile cut short assembles no document")
}

// liveForCalls answers Err with nil for its first left calls and
// context.Canceled from then on.
//
// run consults ctx.Err() once per phase boundary and nothing else does, so a
// context cancelled outright always lands on the first boundary; the later ones
// could otherwise be reached only by racing the walk against a timer. Counting
// the calls puts the cancellation at a chosen boundary, which is what makes this
// deterministic rather than usually right.
type liveForCalls struct {
	context.Context
	left *int
}

func (c liveForCalls) Err() error {
	if *c.left > 0 {
		*c.left--
		return nil
	}
	return context.Canceled
}

func TestRun_RefusesAtEveryPhaseBoundaryOnCancellation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		live int
	}{
		{"before the component schemas are lowered", 0},
		{"after the service walk", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			left := tc.live

			doc, diags, err := run(liveForCalls{Context: t.Context(), left: &left},
				lowering.Ctx{Doc: &soa.OpenAPI{}}, compile.NewTypes(0))

			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, doc, "a partial registry is never assembled into a Document")
			assert.Empty(t, diags)
			assert.Zero(t, left, "the boundary under test is the one that saw the cancellation")
		})
	}
}
