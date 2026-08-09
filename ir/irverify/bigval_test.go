package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// bigVal returns a pointer to the literal as written, which is how the three
// bound fields carry one and the only way to write one past ir.NewBigVal.
func bigVal(literal string) *ir.BigVal {
	v := ir.BigVal(literal)
	return &v
}

// numericValue is a number-kinded ir.Value carrying the literal as written.
func numericValue(literal string) ir.Value {
	return ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal(literal)}
}

// bigValCarriers builds one document per field in the IR that carries a
// ir.BigVal, each holding literal at that field and nothing else out of place.
// The issue's own warning is that a check can be written and still not reach a
// carrier, so each is planted and asserted separately rather than all at once.
func bigValCarriers(literal string) map[string]*ir.Document {
	prov := ir.Provenance{Source: ir.NoSource}
	scalar := func(c *ir.Constraints) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{"t/x/S": &ir.Scalar{
			TypeCommon:  ir.TypeCommon{ID: "t/x/S", Name: named("s"), Provenance: prov},
			Constraints: c,
		}}}
	}
	valued := func(v ir.Value) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{"t/x/L": &ir.Literal{
			TypeCommon: ir.TypeCommon{ID: "t/x/L", Name: named("l"), Provenance: prov},
			Value:      v,
		}}}
	}
	property := func(p ir.Property) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{"t/x/M": &ir.Model{
			TypeCommon: ir.TypeCommon{ID: "t/x/M", Name: named("m"), Provenance: prov},
			Properties: []ir.Property{p},
		}}}
	}
	prop := ir.Property{ID: "p/x/M/f", Name: named("f"), Provenance: prov}
	prop.Type = ir.TypeRef{Target: "t/x/M"}

	withDefault, withBound := prop, prop
	def := numericValue(literal)
	withDefault.Default = &def
	withBound.Constraints = &ir.Constraints{Max: bigVal(literal)}

	return map[string]*ir.Document{
		"doc.Types[t/x/S].Constraints.Min":               scalar(&ir.Constraints{Min: bigVal(literal)}),
		"doc.Types[t/x/S].Constraints.Max":               scalar(&ir.Constraints{Max: bigVal(literal)}),
		"doc.Types[t/x/S].Constraints.MultipleOf":        scalar(&ir.Constraints{MultipleOf: bigVal(literal)}),
		"doc.Types[t/x/L].Value.Num":                     valued(numericValue(literal)),
		"doc.Types[t/x/M].Properties[0].Default.Num":     property(withDefault),
		"doc.Types[t/x/M].Properties[0].Constraints.Max": property(withBound),
	}
}

// assertBigValCode plants literal at every ir.BigVal carrier in turn and asserts
// each document yields exactly the one violation, at that carrier's path.
func assertBigValCode(t *testing.T, literal, code string) {
	t.Helper()
	for path, doc := range bigValCarriers(literal) {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(doc)
			require.Len(t, got, 1, "the document is sound apart from the literal")
			assert.Equal(t, code, got[0].Code)
			assert.Equal(t, path, got[0].Path)
			assert.Contains(t, got[0].Message, literal)
		})
	}
}

// TestVerify_NonNumericBigValIsAViolation drives the half ir.NewBigVal rejects
// outright. ir.BigVal is a defined string type with no UnmarshalJSON, so every
// one of these reaches a document either by conversion or by a JSON decode that
// meets no constructor.
func TestVerify_NonNumericBigValIsAViolation(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{"abc", "1.2.3", "0x1f", "NaN", "1e", "1 ; DROP TABLE"} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			assertBigValCode(t, literal, "ir/bigval-not-numeric")
		})
	}
}

// TestVerify_NonCanonicalBigValIsAViolation drives the half ir.NewBigVal
// accepts and rewrites. Each of these is a number, and none of them is one JSON
// admits, so a consumer splicing the text into JSON or into generated source
// emits something that will not parse.
func TestVerify_NonCanonicalBigValIsAViolation(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{"+5", "007", ".5", "5.", "00.5"} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			assertBigValCode(t, literal, "ir/bigval-not-canonical")
		})
	}
}

// TestVerify_CanonicalBigValIsClean is the silent half: a check that cannot stay
// quiet reports every document that carries a number. The values span what
// BigVal exists to carry — a magnitude and a precision no float64 holds, an
// exponent, and a signed zero — so the rule cannot be passing them by narrowing
// what a number is.
func TestVerify_CanonicalBigValIsClean(t *testing.T) {
	t.Parallel()
	for _, literal := range []string{
		"1", "-0", "0.5", "1e10", "1E-30", "9007199254740993",
		"123456789012345678901234567890.123456789",
	} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			for path, doc := range bigValCarriers(literal) {
				assert.Empty(t, irverify.Verify(doc), "%s", path)
			}
		})
	}
}

// TestVerify_UnusedNumIsNotABound pins the one distinction the walk has to draw.
// Value.Num is not a pointer and is the zero string on every Value that is not a
// number — most of the values a document holds — so an empty one there says the
// field is unused. A bound is carried by pointer and is present because
// something set it, so an empty one there is a bound that constrains nothing.
func TestVerify_UnusedNumIsNotABound(t *testing.T) {
	t.Parallel()
	prov := ir.Provenance{Source: ir.NoSource}
	unused := &ir.Document{Types: ir.TypeRegistry{"t/x/L": &ir.Literal{
		TypeCommon: ir.TypeCommon{ID: "t/x/L", Name: named("l"), Provenance: prov},
		Value:      ir.Value{Kind: ir.ValueString, Str: "not a number"},
	}}}
	assert.Empty(t, irverify.Verify(unused), "a non-numeric Value carries no literal to check")

	empty := &ir.Document{Types: ir.TypeRegistry{"t/x/S": &ir.Scalar{
		TypeCommon:  ir.TypeCommon{ID: "t/x/S", Name: named("s"), Provenance: prov},
		Constraints: &ir.Constraints{Min: bigVal("")},
	}}}
	got := irverify.Verify(empty)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/bigval-not-numeric", got[0].Code)
	assert.Equal(t, "doc.Types[t/x/S].Constraints.Min", got[0].Path)
}

// TestVerify_AbsentBoundIsClean holds the other side of that distinction: a nil
// bound is the ordinary case — most constraints set none — and reading one as a
// literal would report every document in the corpus.
func TestVerify_AbsentBoundIsClean(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/S": &ir.Scalar{
		TypeCommon:  ir.TypeCommon{ID: "t/x/S", Name: named("s"), Provenance: ir.Provenance{Source: ir.NoSource}},
		Constraints: &ir.Constraints{Pattern: "^[a-z]+$"},
	}}}
	assert.Empty(t, irverify.Verify(doc))
}
