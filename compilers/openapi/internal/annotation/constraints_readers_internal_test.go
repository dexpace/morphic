package annotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

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
