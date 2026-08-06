package annotation

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// schemaFromYAMLUnvalidated parses body like schemaFromYAML but keeps the
// library's own validation findings instead of requiring none.
//
// A literal the library cannot read as a float64 is the case it exists for —
// a magnitude beyond that range, or a spelling that is no number to it at all.
// The library types these keywords as float64 and reports the literal as a
// string it could not convert, while the compiler's loader suppresses exactly
// that finding because such a bound may still be valid and must survive.
// Requiring a clean parse here would put every one of them out of reach.
func schemaFromYAMLUnvalidated(t *testing.T, body string) *oas3.Schema {
	t.Helper()
	var js oas3.JSONSchema[oas3.Referenceable]
	_, err := marshaller.Unmarshal(t.Context(), strings.NewReader(body), &js)
	require.NoError(t, err)
	s := js.GetSchema()
	require.NotNil(t, s, "the fixture is a schema, not a bare boolean")
	return s
}

// bigOf returns a pointer to v as a BigVal, which every numeric bound is.
func bigOf(v string) *ir.BigVal {
	b := ir.BigVal(v)
	return &b
}

// i64 returns a pointer to v, which the length and count bounds take.
func i64(v int64) *int64 { return &v }

// TestConstraints_ReadsEveryScalarKeyword pins the whole scalar set in one
// place. A keyword read into the wrong field, or not read at all, is a
// constraint the source declared and the IR does not carry — and the emptiness
// check below can only be right if this list and that one agree.
func TestConstraints_ReadsEveryScalarKeyword(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nminimum: 1\nmaximum: 9\nmultipleOf: 3\n"+
		"minLength: 2\nmaxLength: 8\npattern: '^a'\nminProperties: 1\nmaxProperties: 4\n")

	got, diags := Constraints(s, false)

	require.Empty(t, diags)
	require.NotNil(t, got)
	assert.Equal(t, bigOf("1"), got.Min)
	assert.Equal(t, bigOf("9"), got.Max)
	assert.Equal(t, bigOf("3"), got.MultipleOf)
	assert.Equal(t, i64(2), got.MinLength)
	assert.Equal(t, i64(8), got.MaxLength)
	assert.Equal(t, "^a", got.Pattern)
	assert.Equal(t, i64(1), got.MinProps)
	assert.Equal(t, i64(4), got.MaxProps)
}

// TestConstraints_NothingDeclaredIsNilNotEmpty pins the emptiness check. An
// empty *Constraints on every schema would put a constraint node on nodes that
// declare none, which is a difference the IR would carry and the source never
// wrote.
func TestConstraints_NothingDeclaredIsNilNotEmpty(t *testing.T) {
	t.Parallel()
	got, diags := Constraints(schemaFromYAML(t, "type: string\n"), false)
	assert.Nil(t, got)
	assert.Empty(t, diags)

	got, diags = Constraints(nil, false)
	assert.Nil(t, got)
	assert.Nil(t, diags)
}

// TestConstraints_KeepsTheExactLiteral pins the no-float64 invariant at the one
// place it is read: the bound comes off the raw node, so a magnitude beyond
// float64 and a decimal float64 cannot represent both survive as written.
func TestConstraints_KeepsTheExactLiteral(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: number\nminimum: 9007199254740993\nmaximum: 0.30000000000000004\n")

	got, diags := Constraints(s, false)

	require.Empty(t, diags)
	require.NotNil(t, got)
	assert.Equal(t, bigOf("9007199254740993"), got.Min, "an integer past float64's exact range")
	assert.Equal(t, bigOf("0.30000000000000004"), got.Max, "and a decimal it cannot represent")
}

// TestNumericBounds_AMalformedLiteralIsReportedNotDropped pins the error route.
// The loader suppresses the library's own float64 type-mismatch on these
// keywords, so this is the only diagnostic a bad bound gets: staying silent
// would drop it with nothing said.
func TestNumericBounds_AMalformedLiteralIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	for _, keyword := range []string{"minimum", "maximum", "multipleOf"} {
		t.Run(keyword, func(t *testing.T) {
			t.Parallel()
			got, diags := Constraints(schemaFromYAML(t, "type: number\n"+keyword+": .inf\n"), false)

			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, diag.NumericPrecision, diags[0].Code)
			assert.Contains(t, diags[0].Message, keyword)
			assert.Nil(t, got, "the bound is dropped rather than half-read")
		})
	}
}

// TestApplyExclusive_BothDialects pins the two spellings of an exclusive bound.
// 3.0 writes a boolean modifying minimum/maximum; the 2020-12 dialect writes the
// bound itself. Reading one as the other loses the bound or invents one.
func TestApplyExclusive_BothDialects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		body             string
		exclusiveBoolean bool
		wantMin, wantMax *ir.BigVal
		wantExclMin      bool
		wantExclMax      bool
	}{
		{
			name: "3.0 boolean flags the bound beside it", exclusiveBoolean: true,
			body:    "minimum: 1\nexclusiveMinimum: true\nmaximum: 9\nexclusiveMaximum: true\n",
			wantMin: bigOf("1"), wantMax: bigOf("9"), wantExclMin: true, wantExclMax: true,
		},
		{
			name: "3.0 false leaves the bound inclusive", exclusiveBoolean: true,
			body:    "minimum: 1\nexclusiveMinimum: false\n",
			wantMin: bigOf("1"),
		},
		{
			name: "2020-12 numeric carries the bound itself", exclusiveBoolean: false,
			body:    "exclusiveMinimum: 1\nexclusiveMaximum: 9\n",
			wantMin: bigOf("1"), wantMax: bigOf("9"), wantExclMin: true, wantExclMax: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags := Constraints(schemaFromYAML(t, "type: number\n"+tc.body), tc.exclusiveBoolean)

			require.Empty(t, diags)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantMin, got.Min)
			assert.Equal(t, tc.wantMax, got.Max)
			assert.Equal(t, tc.wantExclMin, got.ExclusiveMin)
			assert.Equal(t, tc.wantExclMax, got.ExclusiveMax)
		})
	}
}

// TestApplyExclusive_TheWrongFormForTheDialectIsReported pins the mismatch. A
// value in the other dialect's form carries no usable bound here, and the
// loader suppressed the library's check on these keywords, so accepting it
// silently would drop a constraint the source did write.
func TestApplyExclusive_TheWrongFormForTheDialectIsReported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		body             string
		exclusiveBoolean bool
		wantSays         string
	}{
		{
			name: "a number where 3.0 wants a boolean", exclusiveBoolean: true,
			body: "minimum: 1\nexclusiveMinimum: 5\n", wantSays: "a boolean",
		},
		{
			name: "a boolean where 2020-12 wants a number", exclusiveBoolean: false,
			body: "minimum: 1\nexclusiveMinimum: true\n", wantSays: "a number",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags := Constraints(schemaFromYAML(t, "type: number\n"+tc.body), tc.exclusiveBoolean)

			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, diag.ExclusiveBoundForm, diags[0].Code)
			assert.Contains(t, diags[0].Message, tc.wantSays)
			assert.Contains(t, diags[0].Message, "exclusiveMinimum")
			require.NotNil(t, got, "the sibling minimum is still read")
			assert.False(t, got.ExclusiveMin, "the mismatched value sets no flag")
		})
	}
}

// TestApplyExclusive_AMalformedNumericBoundIsReported pins the error route
// through the 2020-12 arm, which reads its own bound off the raw node and so
// has the same way to fail as minimum and maximum do.
func TestApplyExclusive_AMalformedNumericBoundIsReported(t *testing.T) {
	t.Parallel()
	got, diags := Constraints(schemaFromYAML(t, "type: number\nexclusiveMaximum: .inf\n"), false)

	require.Len(t, diags, 1)
	assert.Equal(t, diag.NumericPrecision, diags[0].Code)
	assert.Contains(t, diags[0].Message, "exclusiveMaximum")
	assert.Nil(t, got)
}

// TestReconcileBound_KeepsTheTighterOfTwoCoDeclaredBounds pins the 2020-12 rule
// that a side's two keywords are independent and conjunctive, so one bound slot
// must hold the tighter of them. Keeping the looser is a constraint weaker than
// the source wrote, which is a wrong answer rather than an incomplete one
// (GitHub #33) — {minimum: 10, exclusiveMinimum: 0} once compiled to "> 0".
//
// The tie rows are the reason each side is spelled out rather than derived from
// the other: "x >= 5 and x > 5" is "x > 5" and "x <= 5 and x < 5" is "x < 5", so
// the exclusive bound wins a tie on both sides even though "tighter" runs the
// opposite way on each.
func TestReconcileBound_KeepsTheTighterOfTwoCoDeclaredBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		want     ir.Constraints
		wantSays []string
	}{
		{
			name: "minimum is the tighter of the pair",
			body: "minimum: 10\nexclusiveMinimum: 0\n",
			want: ir.Constraints{Min: bigOf("10")},
			wantSays: []string{"minimum 10", "exclusiveMinimum 0",
				"kept minimum as the tighter", "dropped exclusiveMinimum"},
		},
		{
			name: "exclusiveMinimum is the tighter of the pair",
			body: "minimum: 0\nexclusiveMinimum: 10\n",
			want: ir.Constraints{Min: bigOf("10"), ExclusiveMin: true},
			wantSays: []string{"exclusiveMinimum 10", "minimum 0",
				"kept exclusiveMinimum as the tighter", "dropped minimum"},
		},
		{
			name: "equal minimums leave the exclusive one standing",
			body: "minimum: 5\nexclusiveMinimum: 5\n",
			want: ir.Constraints{Min: bigOf("5"), ExclusiveMin: true},
			wantSays: []string{"exclusiveMinimum 5", "minimum 5",
				"kept exclusiveMinimum as the tighter", "dropped minimum"},
		},
		{
			name: "maximum is the tighter of the pair",
			body: "maximum: 10\nexclusiveMaximum: 100\n",
			want: ir.Constraints{Max: bigOf("10")},
			wantSays: []string{"maximum 10", "exclusiveMaximum 100",
				"kept maximum as the tighter", "dropped exclusiveMaximum"},
		},
		{
			name: "exclusiveMaximum is the tighter of the pair",
			body: "maximum: 100\nexclusiveMaximum: 10\n",
			want: ir.Constraints{Max: bigOf("10"), ExclusiveMax: true},
			wantSays: []string{"exclusiveMaximum 10", "maximum 100",
				"kept exclusiveMaximum as the tighter", "dropped maximum"},
		},
		{
			name: "equal maximums leave the exclusive one standing",
			body: "maximum: 5\nexclusiveMaximum: 5\n",
			want: ir.Constraints{Max: bigOf("5"), ExclusiveMax: true},
			wantSays: []string{"exclusiveMaximum 5", "maximum 5",
				"kept exclusiveMaximum as the tighter", "dropped maximum"},
		},
		{
			name: "a bound decided by a digit float64 cannot hold",
			body: "minimum: 9007199254740993\nexclusiveMinimum: 9007199254740992\n",
			want: ir.Constraints{Min: bigOf("9007199254740993")},
			wantSays: []string{"minimum 9007199254740993", "exclusiveMinimum 9007199254740992",
				"kept minimum as the tighter", "dropped exclusiveMinimum"},
		},
		{
			name: "one value spelled two ways is still a tie",
			body: "minimum: 1e2\nexclusiveMinimum: 100\n",
			want: ir.Constraints{Min: bigOf("100"), ExclusiveMin: true},
			wantSays: []string{"exclusiveMinimum 100", "minimum 1e2",
				"kept exclusiveMinimum as the tighter", "dropped minimum"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags := Constraints(schemaFromYAML(t, "type: number\n"+tc.body), false)

			require.NotNil(t, got)
			if diff := cmp.Diff(tc.want, *got); diff != "" {
				t.Errorf("constraints (-want +got):\n%s", diff)
			}
			require.Len(t, diags, 1, "the keyword that did not reach the IR is reported")
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
			for _, says := range tc.wantSays {
				assert.Contains(t, diags[0].Message, says)
			}
		})
	}
}

// TestReconcileBound_OneKeywordPerSideIsNotReconciled pins the silent path. A
// side that writes one keyword has nothing to reconcile, so announcing a
// dropped bound there would report a loss that did not happen — and it is the
// common case, which a diagnostic on every numeric schema would drown.
func TestReconcileBound_OneKeywordPerSideIsNotReconciled(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"minimum: 1\nmaximum: 9\n",
		"exclusiveMinimum: 1\nexclusiveMaximum: 9\n",
		"minimum: 1\nexclusiveMaximum: 9\n",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			got, diags := Constraints(schemaFromYAML(t, "type: number\n"+body), false)

			require.NotNil(t, got)
			assert.Empty(t, diags)
		})
	}
}

// TestReconcileBound_ThreeZeroDialectPairIsUntouched pins the 3.0 arm against
// the 2020-12 fix. There exclusiveMinimum is a boolean modifier of the minimum
// beside it, so the two cannot be rival bounds and there is nothing to drop:
// reconciling them would invent a diagnostic and could discard the bound the
// flag modifies.
func TestReconcileBound_ThreeZeroDialectPairIsUntouched(t *testing.T) {
	t.Parallel()
	got, diags := Constraints(schemaFromYAML(t,
		"type: number\nminimum: 10\nexclusiveMinimum: true\nmaximum: 20\nexclusiveMaximum: true\n"), true)

	require.NotNil(t, got)
	assert.Empty(t, diags)
	want := ir.Constraints{Min: bigOf("10"), Max: bigOf("20"), ExclusiveMin: true, ExclusiveMax: true}
	if diff := cmp.Diff(want, *got); diff != "" {
		t.Errorf("constraints (-want +got):\n%s", diff)
	}
}

// TestReconcileBound_AMagnitudeNoRationalHoldsStillCompares pins the exactness
// of the comparison at the size where the obvious way to make it gives out.
// math/big will not build 1e2000000 as a rational — the exponent is past its
// own limit for one — so reconciling through a rational had to fall back, and
// the fallback keeps the exclusive bound. Here that is the looser one: "> 5"
// where the source says ">= 1e2000000" is the wrong constraint GitHub #33 is
// about, in a rarer case and with a warning attached.
//
// These magnitudes are legal in a spec and ir.NewBigVal keeps them, so the
// comparison has to reach them; the exponent alone separates the two bounds,
// and nothing here needs the million digits it stands for.
func TestReconcileBound_AMagnitudeNoRationalHoldsStillCompares(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		want     ir.Constraints
		wantSays []string
	}{
		{
			name: "a minimum too large for a rational is still the tighter",
			body: "minimum: 1.0e2000000\nexclusiveMinimum: 5\n",
			want: ir.Constraints{Min: bigOf("1.0e2000000")},
			wantSays: []string{"minimum 1.0e2000000", "exclusiveMinimum 5",
				"kept minimum as the tighter", "dropped exclusiveMinimum"},
		},
		{
			name: "a maximum too small for one is the tighter on its side",
			body: "maximum: 1e-1000001\nexclusiveMaximum: 5\n",
			want: ir.Constraints{Max: bigOf("1e-1000001")},
			wantSays: []string{"maximum 1e-1000001", "exclusiveMaximum 5",
				"kept maximum as the tighter", "dropped exclusiveMaximum"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags := Constraints(schemaFromYAMLUnvalidated(t, "type: number\n"+tc.body), false)

			require.NotNil(t, got)
			if diff := cmp.Diff(tc.want, *got); diff != "" {
				t.Errorf("constraints (-want +got):\n%s", diff)
			}
			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity, "the pair did compare")
			for _, says := range tc.wantSays {
				assert.Contains(t, diags[0].Message, says)
			}
		})
	}
}

// TestReconcileBound_ABoundNoDecimalReadingOrdersKeepsTheExclusiveOne pins the
// guard standing at this reader's boundary with ir.NewBigVal.
//
// It is driven through reconcileBound rather than through a schema because no
// schema reaches it: every bound arrives via ir.NewBigVal, whose grammar
// TestBigValGrammarStaysWithinTheDecimalReading holds inside the one
// parseDecimalBound orders. The guard is what keeps a later widening of that
// grammar from widening a bound instead — a bound that cannot be ordered is one
// that could be silently replaced by the looser of the pair — so it keeps the
// exclusive bound and says the discarded one may have been the tighter, rather
// than claiming a comparison it never made.
func TestReconcileBound_ABoundNoDecimalReadingOrdersKeepsTheExclusiveOne(t *testing.T) {
	t.Parallel()
	c := &ir.Constraints{Min: bigOf("1p4")}

	diags := reconcileBound(c, true, ir.BigVal("5"))

	want := ir.Constraints{Min: bigOf("5"), ExclusiveMin: true}
	if diff := cmp.Diff(want, *c); diff != "" {
		t.Errorf("constraints (-want +got):\n%s", diff)
	}
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity, "the kept bound may be the looser one")
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
	assert.Contains(t, diags[0].Message, "could not be compared")
	assert.Contains(t, diags[0].Message, "minimum 1p4")
	assert.Contains(t, diags[0].Message, "exclusiveMinimum 5")
}
