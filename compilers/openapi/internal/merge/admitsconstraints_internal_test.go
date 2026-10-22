package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestAdmitsConstraints_JudgesEachKeyword pins each constraint keyword at its
// boundary, the last value in and the first out, a keyword that does not apply
// to the value's kind, and the keywords with no judge, which refuse what they
// apply to.
func TestAdmitsConstraints_JudgesEachKeyword(t *testing.T) {
	t.Parallel()
	n := func(i int64) *int64 { return &i }
	b := func(s string) *ir.BigVal { v := ir.BigVal(s); return &v }
	list := func(vs ...ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: vs} }
	obj := func(fs ...ir.Field) ir.Value { return ir.Value{Kind: ir.ValueObject, Object: fs} }
	field := func(name string, v ir.Value) ir.Field { return ir.Field{Name: name, Value: v} }
	nested := list(list(list(list(list(list(list(list(list(list())))))))))
	for _, tc := range []struct {
		name string
		c    *ir.Constraints
		v    ir.Value
		want bool
	}{
		{"no constraints", nil, str("anything"), true},
		{"minimum, at it", &ir.Constraints{Min: b("5")}, num("5.0"), true},
		{"minimum, below it", &ir.Constraints{Min: b("5")}, num("4.9"), false},
		{"exclusiveMinimum, at it", &ir.Constraints{ExclusiveMin: b("5")}, num("5"), false},
		{"exclusiveMinimum, above it", &ir.Constraints{ExclusiveMin: b("5")}, num("5.01"), true},
		{"maximum, at it", &ir.Constraints{Max: b("1e2")}, num("100"), true},
		{"maximum, above it", &ir.Constraints{Max: b("1e2")}, num("100.5"), false},
		{"exclusiveMaximum, at it", &ir.Constraints{ExclusiveMax: b("10")}, num("10"), false},
		{"exclusiveMaximum, below it", &ir.Constraints{ExclusiveMax: b("10")}, num("9"), true},
		{"a bound outside the grammar", &ir.Constraints{Max: b("1p4")}, num("1"), false},
		{"multipleOf, a multiple", &ir.Constraints{MultipleOf: b("0.1")}, num("0.3"), true},
		{"multipleOf, off the step", &ir.Constraints{MultipleOf: b("0.1")}, num("0.35"), false},
		{"multipleOf, past what can be judged", &ir.Constraints{MultipleOf: b("2")}, num("1e5000"), false},
		{"precision, which has no judge", &ir.Constraints{Precision: n(5)}, num("1"), false},
		{"scale, which has no judge", &ir.Constraints{Scale: n(2)}, num("1"), false},
		{"a numeric bound on a string", &ir.Constraints{Max: b("1")}, str("hello"), true},
		{"maxLength in code points, at it", &ir.Constraints{MaxLength: n(3)}, str("héé"), true},
		{"maxLength, past it", &ir.Constraints{MaxLength: n(3)}, str("hello"), false},
		{"minLength, short of it", &ir.Constraints{MinLength: n(2)}, str("a"), false},
		{"a length on a number", &ir.Constraints{MaxLength: n(1)}, num("12345"), true},
		{"a pattern, which has no judge", &ir.Constraints{Pattern: "^a"}, str("a"), false},
		{"a pattern on a number", &ir.Constraints{Pattern: "^a"}, num("1"), true},
		{"maxLength on bytes", &ir.Constraints{MaxLength: n(2)}, ir.Value{Kind: ir.ValueBytes, Bytes: []byte("abc")}, false},
		{"maxItems, at it", &ir.Constraints{MaxItems: n(2)}, list(num("1"), num("2")), true},
		{"maxItems, past it", &ir.Constraints{MaxItems: n(2)}, list(num("1"), num("2"), num("3")), false},
		{"minItems, short of it", &ir.Constraints{MinItems: n(1)}, list(), false},
		{"uniqueItems, distinct", &ir.Constraints{UniqueItems: true}, list(num("1"), str("1"), ir.Value{Kind: ir.ValueNull}), true},
		{"uniqueItems, one number spelled twice", &ir.Constraints{UniqueItems: true}, list(num("1"), num("1.0")), false},
		{"uniqueItems, one object in two member orders", &ir.Constraints{UniqueItems: true},
			list(obj(field("a", num("1")), field("b", ir.Value{Kind: ir.ValueBool, Bool: true})),
				obj(field("b", ir.Value{Kind: ir.ValueBool, Bool: true}), field("a", num("1")))), false},
		{"uniqueItems, two lists", &ir.Constraints{UniqueItems: true}, list(list(num("1")), list(num("2"))), true},
		{"uniqueItems, a member no instance holds", &ir.Constraints{UniqueItems: true}, list(ir.Value{Kind: ir.ValueSymbol}), false},
		{"uniqueItems, nesting past the cap", &ir.Constraints{UniqueItems: true}, list(nested), false},
		{"maxProps, past it", &ir.Constraints{MaxProps: n(1)}, obj(field("a", num("1")), field("b", num("2"))), false},
		{"minProps, at it", &ir.Constraints{MinProps: n(1)}, obj(field("a", num("1"))), true},
		{"a count on a bool", &ir.Constraints{MaxItems: n(0)}, ir.Value{Kind: ir.ValueBool}, true},
		{"a kind the IR does not declare", &ir.Constraints{}, ir.Value{Kind: "unknown"}, false},
		{"uniqueItems, a member of a kind the IR does not declare", &ir.Constraints{UniqueItems: true},
			list(ir.Value{Kind: "unknown"}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, admitsConstraints(tc.c, tc.v, 0))
		})
	}
}
