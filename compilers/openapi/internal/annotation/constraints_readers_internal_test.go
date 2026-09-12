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
// A bound whose magnitude is beyond float64's range is the case it exists for.
// The library types these keywords as float64 and reports such a literal as a
// string it could not convert, while the compiler's loader suppresses exactly
// that finding because the bound is still valid and must survive. Requiring a
// clean parse here would put every one of them out of reach.
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

	got, _, diags := Constraints(s, false, "/p", 0)

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
	got, _, diags := Constraints(schemaFromYAML(t, "type: string\n"), false, "/p", 0)
	assert.Nil(t, got)
	assert.Empty(t, diags)

	got, _, diags = Constraints(nil, false, "/p", 0)
	assert.Nil(t, got)
	assert.Nil(t, diags)
}

// TestConstraints_KeepsTheExactLiteral pins the no-float64 invariant at the one
// place it is read: the bound comes off the raw node, so a magnitude beyond
// float64 and a decimal float64 cannot represent both survive as written.
func TestConstraints_KeepsTheExactLiteral(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: number\nminimum: 9007199254740993\nmaximum: 0.30000000000000004\n")

	got, _, diags := Constraints(s, false, "/p", 0)

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
			got, _, diags := Constraints(schemaFromYAML(t, "type: number\n"+keyword+": .inf\n"), false, "/p", 0)

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
		wantExclMin      *ir.BigVal
		wantExclMax      *ir.BigVal
	}{
		{
			name: "3.0 boolean turns the bound beside it exclusive", exclusiveBoolean: true,
			body:        "minimum: 1\nexclusiveMinimum: true\nmaximum: 9\nexclusiveMaximum: true\n",
			wantExclMin: bigOf("1"), wantExclMax: bigOf("9"),
		},
		{
			name: "3.0 false leaves the bound inclusive", exclusiveBoolean: true,
			body:    "minimum: 1\nexclusiveMinimum: false\n",
			wantMin: bigOf("1"),
		},
		{
			name: "2020-12 numeric carries the bound itself", exclusiveBoolean: false,
			body:        "exclusiveMinimum: 1\nexclusiveMaximum: 9\n",
			wantExclMin: bigOf("1"), wantExclMax: bigOf("9"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, diags := Constraints(schemaFromYAML(t, "type: number\n"+tc.body), tc.exclusiveBoolean, "/p", 0)

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
			got, _, diags := Constraints(schemaFromYAML(t, "type: number\n"+tc.body), tc.exclusiveBoolean, "/p", 0)

			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, diag.ExclusiveBoundForm, diags[0].Code)
			assert.Contains(t, diags[0].Message, tc.wantSays)
			assert.Contains(t, diags[0].Message, "exclusiveMinimum")
			require.NotNil(t, got, "the sibling minimum is still read")
			assert.Nil(t, got.ExclusiveMin, "the mismatched value sets no bound")
			assert.Equal(t, bigOf("1"), got.Min, "and it stays inclusive, unmoved")
		})
	}
}

// TestApplyExclusive_AMalformedNumericBoundIsReported pins the error route
// through the 2020-12 arm, which reads its own bound off the raw node and so
// has the same way to fail as minimum and maximum do.
func TestApplyExclusive_AMalformedNumericBoundIsReported(t *testing.T) {
	t.Parallel()
	got, _, diags := Constraints(schemaFromYAML(t, "type: number\nexclusiveMaximum: .inf\n"), false, "/p", 0)

	require.Len(t, diags, 1)
	assert.Equal(t, diag.NumericPrecision, diags[0].Code)
	assert.Contains(t, diags[0].Message, "exclusiveMaximum")
	assert.Nil(t, got)
}

// TestConstraints_CoDeclaredBoundsBothReachAField pins the 2020-12 rule that a
// side's two keywords are independent and conjunctive: each is a restriction the
// source wrote, ir.Constraints has a field for each, and neither is chosen over
// the other.
//
// The rows come in pairs that swap which keyword is the tighter while leaving
// the same two magnitudes on the side. One slot per side answers both rows of a
// pair with the tighter bound alone, so a consumer diffing two revisions of a
// spec across such a swap saw a change of a different kind than the one that
// happened — and a revision that moved only the looser keyword read as no change
// at all (GitHub #425). Two fields answer them differently, which is what these
// pairs are here to hold.
//
// Nothing is kept verbatim and nothing is reported: with both keywords in the
// document there is no residue to keep and no degradation to announce.
func TestConstraints_CoDeclaredBoundsBothReachAField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want ir.Constraints
	}{
		{
			name: "minimum is the tighter of the pair",
			body: "minimum: 10\nexclusiveMinimum: 0\n",
			want: ir.Constraints{Min: bigOf("10"), ExclusiveMin: bigOf("0")},
		},
		{
			name: "exclusiveMinimum is the tighter of the pair",
			body: "minimum: 0\nexclusiveMinimum: 10\n",
			want: ir.Constraints{Min: bigOf("0"), ExclusiveMin: bigOf("10")},
		},
		{
			name: "equal minimums are two keywords, not one",
			body: "minimum: 5\nexclusiveMinimum: 5\n",
			want: ir.Constraints{Min: bigOf("5"), ExclusiveMin: bigOf("5")},
		},
		{
			name: "maximum is the tighter of the pair",
			body: "maximum: 10\nexclusiveMaximum: 100\n",
			want: ir.Constraints{Max: bigOf("10"), ExclusiveMax: bigOf("100")},
		},
		{
			name: "exclusiveMaximum is the tighter of the pair",
			body: "maximum: 100\nexclusiveMaximum: 10\n",
			want: ir.Constraints{Max: bigOf("100"), ExclusiveMax: bigOf("10")},
		},
		{
			name: "both sides co-declared keep all four keywords",
			body: "minimum: 10\nexclusiveMinimum: 20\nmaximum: 100\nexclusiveMaximum: 999\n",
			want: ir.Constraints{
				Min: bigOf("10"), ExclusiveMin: bigOf("20"),
				Max: bigOf("100"), ExclusiveMax: bigOf("999"),
			},
		},
		{
			name: "a pair no float64 tells apart keeps both literals",
			body: "minimum: 9007199254740993\nexclusiveMinimum: 9007199254740992\n",
			want: ir.Constraints{Min: bigOf("9007199254740993"), ExclusiveMin: bigOf("9007199254740992")},
		},
		{
			name: "one value spelled two ways stays two keywords",
			body: "minimum: 1e2\nexclusiveMinimum: 100\n",
			want: ir.Constraints{Min: bigOf("1e2"), ExclusiveMin: bigOf("100")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, kept, diags := Constraints(schemaFromYAML(t, "type: number\n"+tc.body), false, "/p", 3)

			require.NotNil(t, got)
			if diff := cmp.Diff(tc.want, *got); diff != "" {
				t.Errorf("constraints (-want +got):\n%s", diff)
			}
			assert.Empty(t, kept, "every keyword written reaches a field of its own")
			assert.Empty(t, diags, "so there is no degradation to report")
		})
	}
}

// TestConstraints_ABoundNoFloatHoldsIsCarriedVerbatim pins that the fields hold
// the literal the source wrote at magnitudes nothing else here could carry.
// math/big will not build 1e2000000 as a rational and float64 has no room for it
// at all, so a lowering that reduced either bound to a number would have to
// round or fail; ir.NewBigVal keeps the text, and both keywords keep their own.
func TestConstraints_ABoundNoFloatHoldsIsCarriedVerbatim(t *testing.T) {
	t.Parallel()
	got, kept, diags := Constraints(schemaFromYAMLUnvalidated(t,
		"type: number\nminimum: 1.0e2000000\nexclusiveMinimum: 5\nmaximum: 1e-1000001\nexclusiveMaximum: 5\n"),
		false, "/p", 0)

	require.NotNil(t, got)
	want := ir.Constraints{
		Min: bigOf("1.0e2000000"), ExclusiveMin: bigOf("5"),
		Max: bigOf("1e-1000001"), ExclusiveMax: bigOf("5"),
	}
	if diff := cmp.Diff(want, *got); diff != "" {
		t.Errorf("constraints (-want +got):\n%s", diff)
	}
	assert.Empty(t, kept)
	assert.Empty(t, diags)
}

// TestConstraints_OneKeywordPerSideKeepsNothing pins the ordinary case. Every
// keyword written reaches a field, so there is nothing to keep verbatim — an
// entry restating one would give a bound two homes — and nothing to report,
// which a diagnostic on every numeric schema would drown anyway.
func TestConstraints_OneKeywordPerSideKeepsNothing(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"minimum: 1\nmaximum: 9\n",
		"exclusiveMinimum: 1\nexclusiveMaximum: 9\n",
		"minimum: 1\nexclusiveMaximum: 9\n",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			got, kept, diags := Constraints(schemaFromYAML(t, "type: number\n"+body), false, "/p", 0)

			require.NotNil(t, got)
			assert.Empty(t, diags)
			assert.Empty(t, kept)
		})
	}
}

// TestApplyExclusiveFlag_ThreeZeroModifierMovesTheBound pins the 3.0 arm. There
// exclusiveMinimum is not a bound but a boolean modifying the minimum beside it,
// so "minimum: 10, exclusiveMinimum: true" is "x > 10" — which ir.Constraints
// spells as ExclusiveMin, not as Min plus something. The literal therefore moves
// into the exclusive field and the inclusive one is left empty: the 2020-12
// spelling of the same restriction, so a 3.0 document and its 3.1 translation
// lower to the same constraints rather than to two documents that diff.
//
// The maximum stays inclusive in the second case for the reason the first case
// cannot cover: flagging the wrong side is symmetric when both sides declare the
// modifier, so only a schema exclusive on one side can tell a crossed-over read
// from a correct one.
func TestApplyExclusiveFlag_ThreeZeroModifierMovesTheBound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want ir.Constraints
	}{
		{
			name: "both sides modified",
			body: "minimum: 10\nexclusiveMinimum: true\nmaximum: 20\nexclusiveMaximum: true\n",
			want: ir.Constraints{ExclusiveMin: bigOf("10"), ExclusiveMax: bigOf("20")},
		},
		{
			name: "only the side that wrote the modifier moves",
			body: "minimum: 10\nexclusiveMinimum: true\nmaximum: 20\n",
			want: ir.Constraints{ExclusiveMin: bigOf("10"), Max: bigOf("20")},
		},
		{
			name: "a false modifier leaves the bound where it is",
			body: "minimum: 10\nexclusiveMinimum: false\nmaximum: 20\nexclusiveMaximum: false\n",
			want: ir.Constraints{Min: bigOf("10"), Max: bigOf("20")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, kept, diags := Constraints(schemaFromYAML(t, "type: number\n"+tc.body), true, "/p", 0)

			require.NotNil(t, got)
			if diff := cmp.Diff(tc.want, *got); diff != "" {
				t.Errorf("constraints (-want +got):\n%s", diff)
			}
			assert.Empty(t, kept)
			assert.Empty(t, diags)
		})
	}
}

// TestApplyExclusiveFlag_AModifierWithNoBoundIsKeptAndReported pins the 3.0
// modifier that modifies nothing. Draft-4 requires minimum wherever
// exclusiveMinimum appears, so the schema is invalid and there is no bound for
// the IR to make exclusive — but the loader hands these two keywords to Morphic
// unchecked, so dropping it here would lose a declared keyword with nothing
// said. It is kept verbatim at its own pointer and reported instead.
//
// Both sides are declared at once because one boundResidue serves both calls to
// applyExclusive: were it to write the map rather than add to it, the surviving
// entry would be whichever side ran second, silently, since a schema writing
// both modifiers is exactly as valid (which is to say not) as one writing either.
func TestApplyExclusiveFlag_AModifierWithNoBoundIsKeptAndReported(t *testing.T) {
	t.Parallel()
	got, kept, diags := Constraints(schemaFromYAML(t,
		"type: number\nexclusiveMinimum: true\nexclusiveMaximum: true\n"), true, "/p", 3)

	assert.Nil(t, got, "a modifier that bounds nothing leaves no constraint behind")
	require.Len(t, kept, 2, "each side keeps its own modifier; got %v", kept)
	for _, want := range []struct{ key, pointer string }{
		{"openapi:exclusiveMinimum", "/p/exclusiveMinimum"},
		{"openapi:exclusiveMaximum", "/p/exclusiveMaximum"},
	} {
		entry, ok := kept[want.key]
		require.True(t, ok, "%s survives the other side", want.key)
		assert.Equal(t, "true", string(entry.Value), "the boolean is the whole of what it said")
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
		assert.Equal(t, ir.Provenance{Source: 3, Pointer: want.pointer}, entry.Provenance)
	}

	require.Len(t, diags, 2, "and each side reports its own")
	for _, d := range diags {
		assert.Equal(t, ir.SeverityWarning, d.Severity)
		assert.Equal(t, diag.DegradedConstruct, d.Code)
		assert.Contains(t, d.Message, "bounds nothing")
	}
	assert.Contains(t, diags[0].Message, "exclusiveMinimum is true with no minimum beside it")
	assert.Contains(t, diags[1].Message, "exclusiveMaximum is true with no maximum beside it")
}

// TestApplyExclusiveFlag_AnUnreadableBoundIsNotAMissingOne pins the difference
// between a minimum nobody wrote and one that would not read. numericBounds
// leaves the parsed slot nil in both cases, so a modifier reading only the slot
// took the second for the first: a literal that already earned its own error
// then drew a second diagnostic asserting the keyword was absent, and parked
// the modifier under Unmodeled as an orphan — on a schema that wrote the bound
// on the line above.
func TestApplyExclusiveFlag_AnUnreadableBoundIsNotAMissingOne(t *testing.T) {
	t.Parallel()
	got, kept, diags := Constraints(schemaFromYAML(t,
		"type: number\nminimum: .inf\nexclusiveMinimum: true\n"), true, "/p", 3)

	assert.Nil(t, got, "an unreadable bound leaves no constraint behind")
	assert.Empty(t, kept, "the modifier modifies a bound that was written; it is no orphan")
	require.Len(t, diags, 1, "the bad literal is the whole complaint; got %v", diags)
	assert.Equal(t, diag.NumericPrecision, diags[0].Code)
}
