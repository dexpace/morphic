package openapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// amplificationBombFixture is a document whose YAML aliases expand it to many
// times its own size, which no alias budget admits.
const amplificationBombFixture = "../../testdata/openapi/amplification_alias_bomb.yaml"

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
				openapitest.RequireNoErrorDiags(t, diags)
				enum, ok := node.(*ir.Enum)
				require.True(t, ok, "an enum inside the budget lowers as an Enum")
				assert.Len(t, enum.Members, tc.members)
				return
			}
			openapitest.AssertHasCode(t, diags, diag.BudgetExceeded, ir.SeverityError)
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
				openapitest.RequireNoErrorDiags(t, diags)
				return
			}
			assert.Nil(t, doc, "an over-budget source is refused before it is parsed")
			openapitest.AssertHasCode(t, diags, diag.BudgetExceeded, ir.SeverityError)
			assert.Equal(t,
				fmt.Sprintf("source document is past the %d-byte budget", tc.limit),
				messageOf(t, diags, diag.BudgetExceeded))
		})
	}
}

func TestCompile_SourceNodeBudgetRefusesADocumentPastIt(t *testing.T) {
	t.Parallel()
	doc, diags := compileBounded(t, enumSpec(3), Limits{MaxSourceNodes: 1})

	assert.Nil(t, doc, "an over-budget node count is refused before the model is built")
	openapitest.AssertHasCode(t, diags, diag.BudgetExceeded, ir.SeverityError)
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
	openapitest.RequireNoErrorDiags(t, diags)
	assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded), "a large but legal document is not refused for its size")
	enum, ok := typeByName(doc, "E").(*ir.Enum)
	require.True(t, ok)
	assert.Len(t, enum.Members, members, "every declared member reaches the IR")
}

func TestCompile_NegativeBudgetIsUnbounded(t *testing.T) {
	t.Parallel()
	const members = 5
	doc, diags := compileBounded(t, enumSpec(members),
		Limits{MaxSourceBytes: -1, MaxSourceNodes: -1, MaxEnumMembers: -1, MaxAliasSurplus: -1, MaxExternalDocuments: -1})

	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	enum, ok := typeByName(doc, "E").(*ir.Enum)
	require.True(t, ok, "a caller who turned the budgets off gets the pre-budget lowering")
	assert.Len(t, enum.Members, members)
}

// aliasFanOutSpec is a minimal document whose one component lists n aliases
// to a four-property schema: a document that is large once expanded without
// being many times its own size, which is the shape the alias budget bounds
// and the ratio rule beside it does not.
func aliasFanOutSpec(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n  schemas:\n" +
		"    Leaf: &leaf {type: object, properties: {a: {type: string}, b: {type: string}, c: {type: string}, d: {type: string}}}\n" +
		"    Fan: {allOf: [")
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("*leaf")
	}
	b.WriteString("]}\n")
	return b.String()
}

// TestCompile_AliasBudgetBindsAtItsDefault pins the default reaching the scan:
// a document whose aliases add more than DefaultMaxAliasSurplus nodes is
// refused under the zero Limits, and as a budget rather than as a bomb.
func TestCompile_AliasBudgetBindsAtItsDefault(t *testing.T) {
	t.Parallel()
	doc, diags := compileBounded(t, aliasFanOutSpec(20_000), Limits{})

	assert.Nil(t, doc, "an over-budget expansion is refused before the model is built")
	openapitest.AssertHasCode(t, diags, diag.BudgetExceeded, ir.SeverityError)
	assert.Contains(t, messageOf(t, diags, diag.BudgetExceeded),
		fmt.Sprintf("past the %d-node alias budget", DefaultMaxAliasSurplus))
}

func TestCompile_AliasBudgetIsTheCallersToSet(t *testing.T) {
	t.Parallel()
	src := aliasFanOutSpec(3)

	doc, diags := compileBounded(t, src, Limits{MaxAliasSurplus: 1})
	assert.Nil(t, doc)
	openapitest.AssertHasCode(t, diags, diag.BudgetExceeded, ir.SeverityError)
	assert.Contains(t, messageOf(t, diags, diag.BudgetExceeded), "past the 1-node alias budget")

	doc, diags = compileBounded(t, src, Limits{MaxAliasSurplus: -1})
	require.NotNil(t, doc, "a caller who turned the budget off has the document lowered")
	openapitest.RequireNoErrorDiags(t, diags)
}

// TestCompile_AnUnboundedAliasBudgetStillRefusesABomb pins what turning the
// budget off leaves standing: a document expanding to many times its own size
// is a bomb, which no budget admits.
func TestCompile_AnUnboundedAliasBudgetStillRefusesABomb(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(amplificationBombFixture)
	require.NoError(t, err)

	doc, diags := compileBounded(t, string(data), Limits{MaxAliasSurplus: -1})
	assert.Nil(t, doc)
	openapitest.AssertHasCode(t, diags, diag.AliasAmplification, ir.SeverityError)
	assert.NotContains(t, messageOf(t, diags, diag.AliasAmplification), "budget",
		"the refusal must not name a budget, since raising one would not admit the document")
}

// TestLoadOptions_ReadsEachBudgetAsLimitsSpellsIt pins the one reading of the
// public budgets: each field left zero takes its own default, a set one is
// kept, and a negative one, whatever its value, reaches its phase unbounded.
func TestLoadOptions_ReadsEachBudgetAsLimitsSpellsIt(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "spec.yaml"}
	resolved := func(l Limits) []budget.Limit {
		o := Options{Limits: l}.withDefaults()
		phase := loadOptions(o, nil, src)
		enum := loweringCtx(&load.Document{}, o).Limits.MaxEnumMembers
		return []budget.Limit{phase.MaxSourceBytes, phase.MaxSourceNodes, enum, phase.MaxAliasSurplus,
			phase.MaxExternalDocuments}
	}
	tests := []struct {
		name string
		in   Limits
		want []budget.Limit
	}{
		{"the zero value takes every default", Limits{}, []budget.Limit{
			budget.AtMost(DefaultMaxSourceBytes), budget.AtMost(DefaultMaxSourceNodes),
			budget.AtMost(DefaultMaxEnumMembers), budget.AtMost(DefaultMaxAliasSurplus),
			budget.AtMost(DefaultMaxExternalDocuments)}},
		{"a set budget is kept", Limits{11, 12, 13, 14, 15}, []budget.Limit{
			budget.AtMost(11), budget.AtMost(12), budget.AtMost(13), budget.AtMost(14), budget.AtMost(15)}},
		{"a negative budget is the caller asking for none", Limits{-1, -2, -3, -4, -5}, []budget.Limit{
			budget.Unbounded(), budget.Unbounded(), budget.Unbounded(), budget.Unbounded(), budget.Unbounded()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, resolved(tc.in))
		})
	}
}

// TestOptionsWithDefaults_KeepsTheCallersBudgets pins that the budgets are not
// resolved twice: withDefaults leaves them as the caller spelt them, for the
// projection onto each phase to read.
func TestOptionsWithDefaults_KeepsTheCallersBudgets(t *testing.T) {
	t.Parallel()
	got := Options{Limits: Limits{MaxEnumMembers: -1}}.withDefaults()

	assert.Equal(t, GroupByTags, got.Grouping)
	assert.Equal(t, Limits{MaxEnumMembers: -1}, got.Limits)
}

// TestSourceLimit_IsTheLoadersByteBudget pins SourceLimit to the byte budget
// the load phase is handed, spelled as the compilers contract spells one: zero
// for none.
func TestSourceLimit_IsTheLoadersByteBudget(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		in   Limits
		want int
	}{
		"the default":      {Limits{}, DefaultMaxSourceBytes},
		"a budget it sets": {Limits{MaxSourceBytes: 9}, 9},
		"no budget":        {Limits{MaxSourceBytes: -1}, 0},
	} {
		got := (&Compiler{}).SourceLimit(compilers.Options{FormatOptions: Options{Limits: tc.in}})
		assert.Equal(t, tc.want, got, name)
	}
}

// TestCompile_TheDocumentBudgetIsTheCallersToSet pins the document budget
// through the public options: a chain of external documents that never ends
// is read to the budget the caller set, and refused past it as
// openapi/budget-exceeded, a diagnostic and not a Go error.
func TestCompile_TheDocumentBudgetIsTheCallersToSet(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		_, err := fmt.Fprintf(w, "x: {$ref: './%d.yaml#/x'}\n", n)
		assert.NoError(t, err, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	src := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
		"components:\n  schemas:\n    A: {$ref: '" + srv.URL + "/0.yaml#/x'}\n"

	_, diags, err := New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(src)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: Options{AllowExternalRefs: true, Limits: Limits{MaxExternalDocuments: 3}}})

	require.NoError(t, err)
	assert.Equal(t, int32(3), requests.Load())
	assert.Contains(t, messageOf(t, diags, diag.BudgetExceeded), "more documents than the 3-document budget")
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

// TestCompile_ACancelInTheLoweringStopsTheCompile pins a cancel that lands after
// the load has finished: the compile's last check, in the lowering, ends it
// with the context's error and no document, and that check is the last one
// the compile makes, so nothing after it runs on.
func TestCompile_ACancelInTheLoweringStopsTheCompile(t *testing.T) {
	t.Parallel()
	sources := []compilers.Source{{Path: "spec.yaml", Data: []byte(enumSpec(2))}}
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	doc, _, err := New().Compile(whole, sources, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)

	ctx := &ctxtest.Countdown{Context: t.Context(), Left: whole.Calls - 1}
	doc, _, err = New().Compile(ctx, sources, compilers.Options{})

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, doc, "a compile cut short assembles no document")
	assert.Equal(t, whole.Calls, ctx.Calls, "the check that refused is the compile's last")
}

// TestCompile_ACancelAtAnyCheckIsTheContexts cancels a compile at every check
// of its context a whole compile makes, and requires each to end with the
// context's error and no document: never a refusal, which a parse its context
// stopped would read as were it taken for a source that will not parse.
func TestCompile_ACancelAtAnyCheckIsTheContexts(t *testing.T) {
	t.Parallel()
	sources := []compilers.Source{{Path: "spec.yaml", Data: []byte(enumSpec(3))}}
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	_, _, err := New().Compile(whole, sources, compilers.Options{})
	require.NoError(t, err)

	for at := range whole.Calls {
		doc, _, err := New().Compile(&ctxtest.Countdown{Context: t.Context(), Left: at}, sources,
			compilers.Options{})

		require.ErrorIs(t, err, context.Canceled, "cancelled at check %d of %d", at+1, whole.Calls)
		require.Nil(t, doc, "check %d", at+1)
	}
}

// TestRun_RefusesAtEveryPhaseBoundaryOnCancellation pins each boundary run
// checks. The document declares nothing, so the walks never consult ctx.Err()
// and the boundaries are its only callers.
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
			ctx := &ctxtest.Countdown{Context: t.Context(), Left: tc.live}

			doc, diags, err := run(ctx, lowering.Ctx{Doc: &soa.OpenAPI{}}, compile.NewTypes())

			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, doc, "a partial registry is never assembled into a Document")
			assert.Empty(t, diags)
			assert.Zero(t, ctx.Left, "the boundary under test is the one that saw the cancellation")
			assert.Equal(t, tc.live+1, ctx.Calls,
				"and it stopped there: a boundary reached after it would have asked again")
		})
	}
}

// TestRun_CancellationReachesTheSettlements pins that the settling honours ctx
// as the walks do. Cancelled at its own check it takes no settlement, and the
// boundary after it refuses the document. One call later the settlement runs
// and only the boundary refuses, so the check that stopped it was the
// settling's own.
func TestRun_CancellationReachesTheSettlements(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
      oneOf: [{$ref: '#/components/schemas/Cat'}]
    Cat: {type: object}
`
	runUntil := func(live int) (int, []ir.Diagnostic, error) {
		source := compilers.Source{Path: "spec.yaml", Data: []byte(src)}
		loaded, _, err := load.Load(t.Context(), 0, source, loadOptions(Options{}.withDefaults(), nil, source))
		require.NoError(t, err)
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: live}
		_, diags, err := run(ctx, loweringCtx(loaded, Options{}.withDefaults()), compile.NewTypes())
		return ctx.Calls, diags, err
	}
	moved := func(diags []ir.Diagnostic) bool {
		return openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Pet/discriminator")
	}
	// Pet's is the one settlement, so the settling asks once, second to last,
	// and the boundary after it asks last.
	calls, diags, err := runUntil(math.MaxInt)
	require.NoError(t, err)
	require.True(t, moved(diags), "uncancelled, the settlement moves the discriminator")

	seen, diags, err := runUntil(calls - 2)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, moved(diags), "a cancelled settling takes no settlement")
	assert.Equal(t, calls, seen, "and the boundary after it still asks")

	_, diags, err = runUntil(calls - 1)
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, moved(diags), "one call later the settlement ran and only the boundary refused")
}
