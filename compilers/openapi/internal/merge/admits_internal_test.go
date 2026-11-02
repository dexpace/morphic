// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
		{"decimal128 34 digits ending at the lowest exponent", ir.PrimDecimal128,
			num("1.234567890123456789012345678901234e-6143"), true},
		{"decimal128 34 digits ending one below it", ir.PrimDecimal128,
			num("1.234567890123456789012345678901234e-6144"), false},
		{"float32 last in", ir.PrimFloat32, num("340282356779733661637539395458142568447"), true},
		{"float32 first out", ir.PrimFloat32, num("340282356779733661637539395458142568448"), false},
		{"float32 negative last in", ir.PrimFloat32, num("-340282356779733661637539395458142568447"), true},
		{"float32 negative first out", ir.PrimFloat32, num("-340282356779733661637539395458142568448"), false},
		{"float64 last in", ir.PrimFloat64, num("179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497791"), true},
		{"float64 first out", ir.PrimFloat64, num("179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497792"), false},
		{"float64 negative last in", ir.PrimFloat64, num("-179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497791"), true},
		{"float64 negative first out", ir.PrimFloat64, num("-179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497792"), false},
		{"float64 zero", ir.PrimFloat64, num("0"), true},
		{"url space", ir.PrimURL, str("mailto:a b"), false},
		{"url space in path", ir.PrimURL, str("http://x/a b"), false},
		{"url braces", ir.PrimURL, str("x:{}"), false},
		{"url non ascii", ir.PrimURL, str("http://x/é"), false},
		{"url control byte", ir.PrimURL, str("http://x/a\x01"), false},
		{"url backslash", ir.PrimURL, str("http://x/a\\b"), false},
		{"url percent octet", ir.PrimURL, str("http://x/a%20b"), true},
		{"url bad percent octet", ir.PrimURL, str("http://x/a%2"), false},
		{"url bad percent digit", ir.PrimURL, str("http://x/a%2g"), false},
		{"url delimiters", ir.PrimURL, str("a+b-c.d://u@[::1]:1/p?q=1&r=!$'()*,;~_#f"), true},
		{"url second fragment delimiter", ir.PrimURL, str("http://x/#a#b"), false},
		{"url bracket in path", ir.PrimURL, str("http://x/a[b"), false},
		{"url bracket without authority", ir.PrimURL, str("x:a]"), false},
		{"url ip literal host", ir.PrimURL, str("http://[::1]/"), true},
		{"url bracket ends a reg-name host", ir.PrimURL, str("http://x]/"), false},
		{"url bracket ends a host before the port", ir.PrimURL, str("http://x]:80/"), false},
		{"url bracket ends a userinfo host", ir.PrimURL, str("http://u@x]/"), false},
		{"url bracket inside a reg-name host", ir.PrimURL, str("http://a]b/"), false},
		{"url second @ in the authority", ir.PrimURL, str("http://a@b@x/"), false},
		{"url second @ before an IP-literal", ir.PrimURL, str("http://a@b@[::1]/"), false},
		{"url @ after the authority is a path or query byte", ir.PrimURL, str("http://u@x/a@b?c@d"), true},
		{"url @ in a path with no authority", ir.PrimURL, str("mailto:a@b@c"), true},
		{"url @ in a query right after the host", ir.PrimURL, str("http://u@x?a@b"), true},
		{"url @ in a fragment right after the host", ir.PrimURL, str("http://u@x#a@b"), true},
		{"url two closing brackets", ir.PrimURL, str("http://[::1]]/"), false},
		{"url text after the literal", ir.PrimURL, str("http://[::1]a/"), false},
		{"url opening bracket ends a host", ir.PrimURL, str("http://x[/"), false},
		{"url opening bracket in the userinfo", ir.PrimURL, str("http://u[@x/"), false},
		{"url ipvfuture host is refused as url.Parse refuses it", ir.PrimURL, str("http://[v1.x]/"), false},
		{"url ip literal with a port, path, query and fragment", ir.PrimURL, str("http://[::1]:80/a?b#c"), true},
		{"url ip literal with userinfo", ir.PrimURL, str("http://u@[::1]/"), true},
		{"url ip literal without a path", ir.PrimURL, str("x://[::1]"), true},
		{"url ip literal with a zone", ir.PrimURL, str("http://[fe80::1%25en0]/"), true},
		{"url query then fragment", ir.PrimURL, str("http://x?a#b"), true},
		{"url empty", ir.PrimURL, str(""), false},
		{"url no colon", ir.PrimURL, str("abc"), false},
		{"url scheme only", ir.PrimURL, str("a:"), true},
		{"url bad scheme byte", ir.PrimURL, str("a_b:c"), false},
		{"time comma fraction", ir.PrimTime, str("23:59:59,5Z"), false},
		{"time dot fraction", ir.PrimTime, str("23:59:59.5Z"), true},
		{"time empty fraction", ir.PrimTime, str("23:59:59.Z"), false},
		{"time offset hour 24", ir.PrimTime, str("23:59:59+24:00"), false},
		{"time offset minute 60", ir.PrimTime, str("23:59:59+05:60"), false},
		{"time offset max", ir.PrimTime, str("23:59:59-23:59"), true},
		{"time offset bad digit", ir.PrimTime, str("23:59:59+0a:00"), false},
		{"time offset bad sign", ir.PrimTime, str("23:59:59*05:00"), false},
		{"time offset no colon", ir.PrimTime, str("23:59:59+0500"), false},
		{"time lower case z", ir.PrimTime, str("23:59:59z"), true},
		{"time short", ir.PrimTime, str("23:59"), false},
		{"time bad separator", ir.PrimTime, str("23-59:59Z"), false},
		{"time bad digit", ir.PrimTime, str("2a:59:59Z"), false},
		{"time leap second", ir.PrimTime, str("23:59:60Z"), true},
		{"time leap second behind utc", ir.PrimTime, str("15:59:60-08:00"), true},
		{"time leap second wrong minute", ir.PrimTime, str("23:58:60Z"), false},
		{"time second 61", ir.PrimTime, str("23:59:61Z"), false},
		{"datetime offset leap second", ir.PrimDatetimeOffset, str("2016-12-31T23:59:60Z"), true},
		{"datetime offset leap second wrong hour", ir.PrimDatetimeOffset, str("2016-12-31T22:59:60Z"), false},
		{"datetime offset comma fraction", ir.PrimDatetimeOffset, str("2024-01-01T00:00:00,5Z"), false},
		{"datetime offset dot fraction", ir.PrimDatetimeOffset, str("2024-01-01T00:00:00.5Z"), true},
		{"datetime offset hour 24 offset", ir.PrimDatetimeOffset, str("2024-01-01T00:00:00+24:00"), false},
		{"datetime offset minute 60 offset", ir.PrimDatetimeOffset, str("2024-01-01T00:00:00+05:60"), false},
		{"datetime offset lower case", ir.PrimDatetimeOffset, str("2024-01-01t00:00:00z"), true},
		{"datetime offset lower case t", ir.PrimDatetimeOffset, str("2024-01-01t00:00:00Z"), true},
		{"datetime offset space separator", ir.PrimDatetimeOffset, str("2024-01-01 00:00:00Z"), false},
		{"datetime offset short", ir.PrimDatetimeOffset, str("2024-01-01"), false},
		{"datetime offset bad calendar", ir.PrimDatetimeOffset, str("2024-02-30T00:00:00Z"), false},
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
		assert.Equal(t, tc.want, Admits(tc.kind, tc.v), "%s: %s of %v", tc.name, tc.kind, tc.v)
	}
}

// TestAdmits_ReadsAHugeExponentWithoutMaterializingIt pins that a literal no
// machine type holds is refused promptly rather than built.
func TestAdmits_ReadsAHugeExponentWithoutMaterializingIt(t *testing.T) {
	t.Parallel()
	start := time.Now()
	for _, k := range []ir.PrimKind{ir.PrimInt64, ir.PrimUint64, ir.PrimFloat32, ir.PrimFloat64, ir.PrimDecimal128} {
		assert.False(t, Admits(k, num("1e1000001")), "%s", k)
		assert.False(t, Admits(k, num("1e99999999999999999999")), "%s", k)
	}
	assert.Less(t, time.Since(start), 5*time.Second)
}

// TestDecimalOf_DeclinesWhatIsNoReadableNumber pins that a value with no number
// to read is declined rather than read as zero: a literal outside the grammar, one
// past the exponent range a BigVal holds, and a value that is no number at all.
func TestDecimalOf_DeclinesWhatIsNoReadableNumber(t *testing.T) {
	t.Parallel()
	for _, v := range []ir.Value{num("1p4"), num("1e1000000000000000000"), str("1")} {
		_, ok := decimalOf(v)

		assert.False(t, ok, "%v", v)
	}

	d, ok := decimalOf(num("1e1000001"))
	assert.True(t, ok)
	assert.Equal(t, int64(1000001), d.Exponent())
}

// TestInRange_DeclinesABoundThatReadsAsNoNumber pins the second half of the
// judge: a range whose end is no number admits nothing, where reading it as zero
// would admit everything above it.
func TestInRange_DeclinesABoundThatReadsAsNoNumber(t *testing.T) {
	t.Parallel()
	assert.True(t, inRange(num("5"), "0", "10"))
	assert.False(t, inRange(num("5"), "1p4", "10"), "a low end outside the grammar")
	assert.False(t, inRange(num("5"), "0", "1p4"), "a high end outside the grammar")
	assert.False(t, inRange(num("-1"), "-5", "1p4"), "a high end that would read as zero, above this value")
}
