package merge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func num(s string) ir.Value { return ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal(s)} }
func str(s string) ir.Value { return ir.Value{Kind: ir.ValueString, Str: s} }

// TestAdmits_JudgesEachKindExactly pins the boundary of every judge: the last
// value in and the first value out, and the near misses of each grammar.
func TestAdmits_JudgesEachKindExactly(t *testing.T) {
	t.Parallel()
	digits35 := "12345678901234567890123456789012345"
	for _, tc := range []struct {
		name string
		kind ir.PrimKind
		v    ir.Value
		want bool
	}{
		{"int8 min", ir.PrimInt8, num("-128"), true},
		{"int8 max", ir.PrimInt8, num("127"), true},
		{"int8 above", ir.PrimInt8, num("128"), false},
		{"int8 below", ir.PrimInt8, num("-129"), false},
		{"int8 fraction", ir.PrimInt8, num("1.5"), false},
		{"int8 integral fraction", ir.PrimInt8, num("5.0"), true},
		{"int16 max", ir.PrimInt16, num("32767"), true},
		{"int16 above", ir.PrimInt16, num("32768"), false},
		{"int32 max", ir.PrimInt32, num("2147483647"), true},
		{"int32 above", ir.PrimInt32, num("2147483648"), false},
		{"int32 below", ir.PrimInt32, num("-2147483649"), false},
		{"int64 max", ir.PrimInt64, num("9223372036854775807"), true},
		{"int64 above", ir.PrimInt64, num("9223372036854775808"), false},
		{"int64 huge exponent", ir.PrimInt64, num("1e1000001"), false},
		{"int64 tiny exponent", ir.PrimInt64, num("1e-1000001"), false},
		{"uint8 max", ir.PrimUint8, num("255"), true},
		{"uint8 above", ir.PrimUint8, num("256"), false},
		{"uint8 negative", ir.PrimUint8, num("-1"), false},
		{"uint16 max", ir.PrimUint16, num("65535"), true},
		{"uint16 above", ir.PrimUint16, num("65536"), false},
		{"uint32 max", ir.PrimUint32, num("4294967295"), true},
		{"uint32 above", ir.PrimUint32, num("4294967296"), false},
		{"uint64 max", ir.PrimUint64, num("18446744073709551615"), true},
		{"uint64 above", ir.PrimUint64, num("18446744073709551616"), false},
		{"integer huge", ir.PrimInteger, num("1e1000001"), true},
		{"integer fraction", ir.PrimInteger, num("0.5"), false},
		{"integer zero", ir.PrimInteger, num("0.0"), true},
		{"integer string", ir.PrimInteger, str("1"), false},
		{"number fraction", ir.PrimNumber, num("1.5"), true},
		{"number bool", ir.PrimNumber, ir.Value{Kind: ir.ValueBool}, false},
		{"float any number", ir.PrimFloat, num("1e1000001"), true},
		{"decimal any number", ir.PrimDecimal, num("1e1000001"), true},
		{"float32 in range", ir.PrimFloat32, num("3.4e38"), true},
		{"float32 out of range", ir.PrimFloat32, num("3.5e38"), false},
		{"float32 negative out of range", ir.PrimFloat32, num("-3.5e38"), false},
		{"float32 rounds to max", ir.PrimFloat32, num("3.40282356e38"), true},
		{"float32 huge exponent", ir.PrimFloat32, num("1e1000001"), false},
		{"float32 string", ir.PrimFloat32, str("1"), false},
		{"float64 in range", ir.PrimFloat64, num("1.7e308"), true},
		{"float64 out of range", ir.PrimFloat64, num("1.8e308"), false},
		{"decimal128 34 digits", ir.PrimDecimal128, num(digits35[:34]), true},
		{"decimal128 35 digits", ir.PrimDecimal128, num(digits35), false},
		{"decimal128 zero", ir.PrimDecimal128, num("0"), true},
		{"decimal128 top exponent", ir.PrimDecimal128, num("1e6144"), true},
		{"decimal128 above exponent", ir.PrimDecimal128, num("1e6145"), false},
		{"decimal128 lowest exponent", ir.PrimDecimal128, num("1e-6176"), true},
		{"decimal128 below exponent", ir.PrimDecimal128, num("1e-6177"), false},
		{"decimal128 string", ir.PrimDecimal128, str("1"), false},
		{"bool", ir.PrimBool, ir.Value{Kind: ir.ValueBool, Bool: true}, true},
		{"bool number", ir.PrimBool, num("1"), false},
		{"string", ir.PrimString, str("x"), true},
		{"string symbol", ir.PrimString, ir.Value{Kind: ir.ValueSymbol, Str: "x"}, false},
		{"date", ir.PrimDate, str("2024-02-29"), true},
		{"date invalid calendar", ir.PrimDate, str("2023-02-29"), false},
		{"date short", ir.PrimDate, str("2024-2-9"), false},
		{"time", ir.PrimTime, str("23:59:59Z"), true},
		{"time fraction offset", ir.PrimTime, str("23:59:59.5+01:00"), true},
		{"time no offset", ir.PrimTime, str("23:59:59"), false},
		{"datetime offset", ir.PrimDatetimeOffset, str("2024-02-29T23:59:59Z"), true},
		{"datetime offset none", ir.PrimDatetimeOffset, str("2024-02-29T23:59:59"), false},
		{"datetime offset hour", ir.PrimDatetimeOffset, str("2024-02-29T24:00:00Z"), false},
		{"uuid", ir.PrimUUID, str("123e4567-e89b-12d3-a456-426614174000"), true},
		{"uuid upper", ir.PrimUUID, str("123E4567-E89B-12D3-A456-426614174000"), true},
		{"uuid short", ir.PrimUUID, str("123e4567-e89b-12d3-a456-42661417400"), false},
		{"uuid no dash", ir.PrimUUID, str("123e4567xe89b-12d3-a456-426614174000"), false},
		{"uuid non hex", ir.PrimUUID, str("123e4567-e89b-12d3-a456-42661417400g"), false},
		{"uuid word", ir.PrimUUID, str("hello"), false},
		{"url", ir.PrimURL, str("https://example.com/a?b=c"), true},
		{"url mailto", ir.PrimURL, str("mailto:a@example.com"), true},
		{"url relative", ir.PrimURL, str("/a/b"), false},
		{"url bad", ir.PrimURL, str("1http://x"), false},
		{"url number", ir.PrimURL, num("1"), false},
		{"duration", ir.PrimDuration, str("P1Y2M3DT4H5M6S"), true},
		{"duration weeks", ir.PrimDuration, str("P2W"), true},
		{"duration time only", ir.PrimDuration, str("PT5M"), true},
		{"duration skipped designator", ir.PrimDuration, str("P1Y3D"), false},
		{"duration date from months", ir.PrimDuration, str("P2M3D"), true},
		{"duration out of order", ir.PrimDuration, str("P3D2M"), false},
		{"duration empty", ir.PrimDuration, str("P"), false},
		{"duration empty time", ir.PrimDuration, str("P1DT"), false},
		{"duration bare T", ir.PrimDuration, str("PT"), false},
		{"duration no prefix", ir.PrimDuration, str("1D"), false},
		{"duration weeks mixed", ir.PrimDuration, str("P1W2D"), false},
		{"duration no digits", ir.PrimDuration, str("PD"), false},
		{"duration trailing digits", ir.PrimDuration, str("P1"), false},
		{"duration repeated", ir.PrimDuration, str("P1D1D"), false},
		{"duration unknown", ir.PrimDuration, str("P1X"), false},
		{"duration date after time", ir.PrimDuration, str("PT1H1D"), false},
		{"date number", ir.PrimDate, num("1"), false},
		{"bytes", ir.PrimBytes, ir.Value{Kind: ir.ValueBytes, Bytes: []byte("x")}, false},
		{"datetime", ir.PrimDatetime, str("2024-02-29T23:59:59Z"), false},
		{"any", ir.PrimAny, ir.Value{Kind: ir.ValueObject}, true},
		{"list against integer", ir.PrimInteger, ir.Value{Kind: ir.ValueList, List: []ir.Value{num("1")}}, false},
		{"unknown kind", ir.PrimKind("nope"), str("x"), false},
	} {
		assert.Equal(t, tc.want, admits(tc.kind, tc.v), "%s: %s of %v", tc.name, tc.kind, tc.v)
	}
}

// TestAdmits_ReadsAHugeExponentWithoutMaterializingIt pins that a literal no
// machine type holds is refused promptly rather than built.
func TestAdmits_ReadsAHugeExponentWithoutMaterializingIt(t *testing.T) {
	t.Parallel()
	start := time.Now()
	for _, k := range []ir.PrimKind{ir.PrimInt64, ir.PrimUint64, ir.PrimFloat32, ir.PrimFloat64, ir.PrimDecimal128} {
		assert.False(t, admits(k, num("1e1000001")), "%s", k)
		assert.False(t, admits(k, num("1e99999999999999999999")), "%s", k)
	}
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestAdmitted_NullIsJudgedByNullability(t *testing.T) {
	t.Parallel()
	null := ir.Value{Kind: ir.ValueNull}

	assert.True(t, admitted(ir.PrimInteger, true, null))
	assert.False(t, admitted(ir.PrimInteger, false, null))
	assert.False(t, admitted(ir.PrimInteger, true, str("x")))
	assert.True(t, admitted(ir.PrimInteger, false, num("1")))
}

func TestExamplesAdmitted_AllOrNothing(t *testing.T) {
	t.Parallel()
	one, half := num("1"), num("1.5")
	require.True(t, examplesAdmitted(ir.PrimInteger, false, nil), "no examples say nothing")
	assert.True(t, examplesAdmitted(ir.PrimInteger, false, []ir.Example{{Value: &one}, {Summary: "no value"}}))
	assert.False(t, examplesAdmitted(ir.PrimInteger, false, []ir.Example{{Value: &one}, {Value: &half}}),
		"one refused example refuses the keyword")
}
