// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/typeformat"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

func number(s string) ir.Value      { return ir.Value{Kind: ir.ValueNumber, Number: ir.BigVal(s)} }
func stringValue(s string) ir.Value { return ir.Value{Kind: ir.ValueString, String: s} }

// TestAdmits_JudgesEachKindExactly pins the boundary of every judge: the last
// value in and the first value out, and the near misses of each grammar.
func TestAdmits_JudgesEachKindExactly(t *testing.T) {
	t.Parallel()
	digits35 := "12345678901234567890123456789012345"
	for _, tc := range []struct {
		name string
		kind ir.PrimitiveKind
		v    ir.Value
		want bool
	}{
		{"int8 min", ir.PrimitiveInt8, number("-128"), true},
		{"int8 max", ir.PrimitiveInt8, number("127"), true},
		{"int8 above", ir.PrimitiveInt8, number("128"), false},
		{"int8 below", ir.PrimitiveInt8, number("-129"), false},
		{"int8 fraction", ir.PrimitiveInt8, number("1.5"), false},
		{"int8 integral fraction", ir.PrimitiveInt8, number("5.0"), true},
		{"int16 max", ir.PrimitiveInt16, number("32767"), true},
		{"int16 above", ir.PrimitiveInt16, number("32768"), false},
		{"int32 max", ir.PrimitiveInt32, number("2147483647"), true},
		{"int32 above", ir.PrimitiveInt32, number("2147483648"), false},
		{"int32 below", ir.PrimitiveInt32, number("-2147483649"), false},
		{"int64 max", ir.PrimitiveInt64, number("9223372036854775807"), true},
		{"int64 above", ir.PrimitiveInt64, number("9223372036854775808"), false},
		{"int64 huge exponent", ir.PrimitiveInt64, number("1e1000001"), false},
		{"int64 tiny exponent", ir.PrimitiveInt64, number("1e-1000001"), false},
		{"uint8 max", ir.PrimitiveUint8, number("255"), true},
		{"uint8 above", ir.PrimitiveUint8, number("256"), false},
		{"uint8 negative", ir.PrimitiveUint8, number("-1"), false},
		{"uint16 max", ir.PrimitiveUint16, number("65535"), true},
		{"uint16 above", ir.PrimitiveUint16, number("65536"), false},
		{"uint32 max", ir.PrimitiveUint32, number("4294967295"), true},
		{"uint32 above", ir.PrimitiveUint32, number("4294967296"), false},
		{"uint64 max", ir.PrimitiveUint64, number("18446744073709551615"), true},
		{"uint64 above", ir.PrimitiveUint64, number("18446744073709551616"), false},
		{"integer huge", ir.PrimitiveInteger, number("1e1000001"), true},
		{"integer fraction", ir.PrimitiveInteger, number("0.5"), false},
		{"integer zero", ir.PrimitiveInteger, number("0.0"), true},
		{"integer string", ir.PrimitiveInteger, stringValue("1"), false},
		{"number fraction", ir.PrimitiveNumber, number("1.5"), true},
		{"number bool", ir.PrimitiveNumber, ir.Value{Kind: ir.ValueBool}, false},
		{"float any number", ir.PrimitiveFloat, number("1e1000001"), true},
		{"decimal any number", ir.PrimitiveDecimal, number("1e1000001"), true},
		{"float32 in range", ir.PrimitiveFloat32, number("3.4e38"), true},
		{"float32 out of range", ir.PrimitiveFloat32, number("3.5e38"), false},
		{"float32 negative out of range", ir.PrimitiveFloat32, number("-3.5e38"), false},
		{"float32 rounds to max", ir.PrimitiveFloat32, number("3.40282356e38"), true},
		{"float32 huge exponent", ir.PrimitiveFloat32, number("1e1000001"), false},
		{"float32 string", ir.PrimitiveFloat32, stringValue("1"), false},
		{"float64 in range", ir.PrimitiveFloat64, number("1.7e308"), true},
		{"float64 out of range", ir.PrimitiveFloat64, number("1.8e308"), false},
		{"decimal128 34 digits", ir.PrimitiveDecimal128, number(digits35[:34]), true},
		{"decimal128 35 digits", ir.PrimitiveDecimal128, number(digits35), false},
		{"decimal128 zero", ir.PrimitiveDecimal128, number("0"), true},
		{"decimal128 top exponent", ir.PrimitiveDecimal128, number("1e6144"), true},
		{"decimal128 above exponent", ir.PrimitiveDecimal128, number("1e6145"), false},
		{"decimal128 lowest exponent", ir.PrimitiveDecimal128, number("1e-6176"), true},
		{"decimal128 below exponent", ir.PrimitiveDecimal128, number("1e-6177"), false},
		{"decimal128 string", ir.PrimitiveDecimal128, stringValue("1"), false},
		{"decimal128 34 digits ending at the lowest exponent", ir.PrimitiveDecimal128,
			number("1.234567890123456789012345678901234e-6143"), true},
		{"decimal128 34 digits ending one below it", ir.PrimitiveDecimal128,
			number("1.234567890123456789012345678901234e-6144"), false},
		{"float32 last in", ir.PrimitiveFloat32, number("340282356779733661637539395458142568447"), true},
		{"float32 first out", ir.PrimitiveFloat32, number("340282356779733661637539395458142568448"), false},
		{"float32 negative last in", ir.PrimitiveFloat32, number("-340282356779733661637539395458142568447"), true},
		{"float32 negative first out", ir.PrimitiveFloat32, number("-340282356779733661637539395458142568448"), false},
		{"float64 last in", ir.PrimitiveFloat64, number("179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497791"), true},
		{"float64 first out", ir.PrimitiveFloat64, number("179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497792"), false},
		{"float64 negative last in", ir.PrimitiveFloat64, number("-179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497791"), true},
		{"float64 negative first out", ir.PrimitiveFloat64, number("-179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497792"), false},
		{"float64 zero", ir.PrimitiveFloat64, number("0"), true},
		{"url space", ir.PrimitiveURL, stringValue("mailto:a b"), false},
		{"url space in path", ir.PrimitiveURL, stringValue("http://x/a b"), false},
		{"url braces", ir.PrimitiveURL, stringValue("bytes:{}"), false},
		{"url non ascii", ir.PrimitiveURL, stringValue("http://x/é"), false},
		{"url control byte", ir.PrimitiveURL, stringValue("http://x/a\x01"), false},
		{"url backslash", ir.PrimitiveURL, stringValue("http://x/a\\b"), false},
		{"url percent octet", ir.PrimitiveURL, stringValue("http://x/a%20b"), true},
		{"url bad percent octet", ir.PrimitiveURL, stringValue("http://x/a%2"), false},
		{"url bad percent digit", ir.PrimitiveURL, stringValue("http://x/a%2g"), false},
		{"url delimiters", ir.PrimitiveURL, stringValue("a+b-c.d://u@[::1]:1/p?q=1&r=!$'()*,;~_#f"), true},
		{"url second fragment delimiter", ir.PrimitiveURL, stringValue("http://x/#a#b"), false},
		{"url bracket in path", ir.PrimitiveURL, stringValue("http://x/a[b"), false},
		{"url bracket without authority", ir.PrimitiveURL, stringValue("bytes:a]"), false},
		{"url ip literal host", ir.PrimitiveURL, stringValue("http://[::1]/"), true},
		{"url bracket ends a reg-name host", ir.PrimitiveURL, stringValue("http://x]/"), false},
		{"url bracket ends a host before the port", ir.PrimitiveURL, stringValue("http://x]:80/"), false},
		{"url bracket ends a userinfo host", ir.PrimitiveURL, stringValue("http://u@x]/"), false},
		{"url bracket inside a reg-name host", ir.PrimitiveURL, stringValue("http://a]b/"), false},
		{"url second @ in the authority", ir.PrimitiveURL, stringValue("http://a@b@x/"), false},
		{"url second @ before an IP-literal", ir.PrimitiveURL, stringValue("http://a@b@[::1]/"), false},
		{"url @ after the authority is a path or query byte", ir.PrimitiveURL, stringValue("http://u@x/a@b?c@d"), true},
		{"url @ in a path with no authority", ir.PrimitiveURL, stringValue("mailto:a@b@c"), true},
		{"url @ in a query right after the host", ir.PrimitiveURL, stringValue("http://u@x?a@b"), true},
		{"url @ in a fragment right after the host", ir.PrimitiveURL, stringValue("http://u@x#a@b"), true},
		{"url two closing brackets", ir.PrimitiveURL, stringValue("http://[::1]]/"), false},
		{"url text after the literal", ir.PrimitiveURL, stringValue("http://[::1]a/"), false},
		{"url opening bracket ends a host", ir.PrimitiveURL, stringValue("http://x[/"), false},
		{"url opening bracket in the userinfo", ir.PrimitiveURL, stringValue("http://u[@x/"), false},
		{"url ipvfuture host is refused as url.Parse refuses it", ir.PrimitiveURL, stringValue("http://[v1.x]/"), false},
		{"url ip literal with a port, path, query and fragment", ir.PrimitiveURL, stringValue("http://[::1]:80/a?b#c"), true},
		{"url ip literal with userinfo", ir.PrimitiveURL, stringValue("http://u@[::1]/"), true},
		{"url ip literal without a path", ir.PrimitiveURL, stringValue("bytes://[::1]"), true},
		{"url ip literal with a zone", ir.PrimitiveURL, stringValue("http://[fe80::1%25en0]/"), true},
		{"url query then fragment", ir.PrimitiveURL, stringValue("http://x?a#b"), true},
		{"url empty", ir.PrimitiveURL, stringValue(""), false},
		{"url no colon", ir.PrimitiveURL, stringValue("abc"), false},
		{"url scheme only", ir.PrimitiveURL, stringValue("a:"), true},
		{"url bad scheme byte", ir.PrimitiveURL, stringValue("a_b:c"), false},
		{"time comma fraction", ir.PrimitiveTime, stringValue("23:59:59,5"), false},
		{"time dot fraction", ir.PrimitiveTime, stringValue("23:59:59.5"), true},
		{"time empty fraction", ir.PrimitiveTime, stringValue("23:59:59."), false},
		{"time with a Z offset", ir.PrimitiveTime, stringValue("23:59:59Z"), false},
		{"time with a numeric offset", ir.PrimitiveTime, stringValue("23:59:59.5+01:00"), false},
		{"time short", ir.PrimitiveTime, stringValue("23:59"), false},
		{"time bad separator", ir.PrimitiveTime, stringValue("23-59:59"), false},
		{"time bad digit", ir.PrimitiveTime, stringValue("2a:59:59"), false},
		{"time hour 24", ir.PrimitiveTime, stringValue("24:00:00"), false},
		{"time minute 60", ir.PrimitiveTime, stringValue("23:60:00"), false},
		{"time leap second at any minute", ir.PrimitiveTime, stringValue("12:34:60"), true},
		{"time second 61", ir.PrimitiveTime, stringValue("23:59:61"), false},
		{"time number", ir.PrimitiveTime, number("1"), false},
		{"datetime", ir.PrimitiveDatetime, stringValue("2024-02-29T23:59:59"), true},
		{"datetime dot fraction", ir.PrimitiveDatetime, stringValue("2024-02-29T23:59:59.5"), true},
		{"datetime lower case t", ir.PrimitiveDatetime, stringValue("2024-02-29t23:59:59"), true},
		{"datetime with a Z offset", ir.PrimitiveDatetime, stringValue("2024-02-29T23:59:59Z"), false},
		{"datetime with a numeric offset", ir.PrimitiveDatetime, stringValue("2024-02-29T23:59:59+01:00"), false},
		{"datetime space separator", ir.PrimitiveDatetime, stringValue("2024-01-01 00:00:00"), false},
		{"datetime short", ir.PrimitiveDatetime, stringValue("2024-01-01"), false},
		{"datetime bad calendar", ir.PrimitiveDatetime, stringValue("2023-02-29T00:00:00"), false},
		{"datetime hour 24", ir.PrimitiveDatetime, stringValue("2024-01-01T24:00:00"), false},
		{"datetime leap second at any minute", ir.PrimitiveDatetime, stringValue("2024-01-01T05:29:60"), true},
		{"datetime second 61", ir.PrimitiveDatetime, stringValue("2016-12-31T23:59:61"), false},
		{"datetime number", ir.PrimitiveDatetime, number("1"), false},
		{"datetime offset max", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T23:59:59-23:59"), true},
		{"datetime offset bad digit", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T23:59:59+0a:00"), false},
		{"datetime offset bad sign", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T23:59:59*05:00"), false},
		{"datetime offset no colon", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T23:59:59+0500"), false},
		{"datetime offset empty fraction", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T23:59:59.Z"), false},
		{"datetime offset leap second behind utc", ir.PrimitiveDatetimeOffset, stringValue("2016-12-31T15:59:60-08:00"), true},
		{"datetime offset leap second wrong minute", ir.PrimitiveDatetimeOffset, stringValue("2016-12-31T23:58:60Z"), false},
		{"datetime offset second 61", ir.PrimitiveDatetimeOffset, stringValue("2016-12-31T23:59:61Z"), false},
		{"datetime offset leap second", ir.PrimitiveDatetimeOffset, stringValue("2016-12-31T23:59:60Z"), true},
		{"datetime offset leap second wrong hour", ir.PrimitiveDatetimeOffset, stringValue("2016-12-31T22:59:60Z"), false},
		{"datetime offset comma fraction", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T00:00:00,5Z"), false},
		{"datetime offset dot fraction", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T00:00:00.5Z"), true},
		{"datetime offset hour 24 offset", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T00:00:00+24:00"), false},
		{"datetime offset minute 60 offset", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01T00:00:00+05:60"), false},
		{"datetime offset lower case", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01t00:00:00z"), true},
		{"datetime offset lower case t", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01t00:00:00Z"), true},
		{"datetime offset space separator", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01 00:00:00Z"), false},
		{"datetime offset short", ir.PrimitiveDatetimeOffset, stringValue("2024-01-01"), false},
		{"datetime offset bad calendar", ir.PrimitiveDatetimeOffset, stringValue("2024-02-30T00:00:00Z"), false},
		{"bool", ir.PrimitiveBool, ir.Value{Kind: ir.ValueBool, Bool: true}, true},
		{"bool number", ir.PrimitiveBool, number("1"), false},
		{"string", ir.PrimitiveString, stringValue("x"), true},
		{"string symbol", ir.PrimitiveString, ir.Value{Kind: ir.ValueSymbol, String: "x"}, false},
		{"date", ir.PrimitiveDate, stringValue("2024-02-29"), true},
		{"date invalid calendar", ir.PrimitiveDate, stringValue("2023-02-29"), false},
		{"date short", ir.PrimitiveDate, stringValue("2024-2-9"), false},
		{"time", ir.PrimitiveTime, stringValue("23:59:59"), true},
		{"datetime offset", ir.PrimitiveDatetimeOffset, stringValue("2024-02-29T23:59:59Z"), true},
		{"datetime offset none", ir.PrimitiveDatetimeOffset, stringValue("2024-02-29T23:59:59"), false},
		{"datetime offset hour", ir.PrimitiveDatetimeOffset, stringValue("2024-02-29T24:00:00Z"), false},
		{"uuid", ir.PrimitiveUUID, stringValue("123e4567-e89b-12d3-a456-426614174000"), true},
		{"uuid upper", ir.PrimitiveUUID, stringValue("123E4567-E89B-12D3-A456-426614174000"), true},
		{"uuid short", ir.PrimitiveUUID, stringValue("123e4567-e89b-12d3-a456-42661417400"), false},
		{"uuid no dash", ir.PrimitiveUUID, stringValue("123e4567xe89b-12d3-a456-426614174000"), false},
		{"uuid non hex", ir.PrimitiveUUID, stringValue("123e4567-e89b-12d3-a456-42661417400g"), false},
		{"uuid word", ir.PrimitiveUUID, stringValue("hello"), false},
		{"url", ir.PrimitiveURL, stringValue("https://example.com/a?b=c"), true},
		{"url mailto", ir.PrimitiveURL, stringValue("mailto:a@example.com"), true},
		{"url relative", ir.PrimitiveURL, stringValue("/a/b"), false},
		{"url bad", ir.PrimitiveURL, stringValue("1http://x"), false},
		{"url number", ir.PrimitiveURL, number("1"), false},
		{"duration", ir.PrimitiveDuration, stringValue("P1Y2M3DT4H5M6S"), true},
		{"duration weeks", ir.PrimitiveDuration, stringValue("P2W"), true},
		{"duration time only", ir.PrimitiveDuration, stringValue("PT5M"), true},
		{"duration skipped designator", ir.PrimitiveDuration, stringValue("P1Y3D"), false},
		{"duration date from months", ir.PrimitiveDuration, stringValue("P2M3D"), true},
		{"duration out of order", ir.PrimitiveDuration, stringValue("P3D2M"), false},
		{"duration empty", ir.PrimitiveDuration, stringValue("P"), false},
		{"duration empty time", ir.PrimitiveDuration, stringValue("P1DT"), false},
		{"duration bare T", ir.PrimitiveDuration, stringValue("PT"), false},
		{"duration no prefix", ir.PrimitiveDuration, stringValue("1D"), false},
		{"duration weeks mixed", ir.PrimitiveDuration, stringValue("P1W2D"), false},
		{"duration no digits", ir.PrimitiveDuration, stringValue("PD"), false},
		{"duration trailing digits", ir.PrimitiveDuration, stringValue("P1"), false},
		{"duration repeated", ir.PrimitiveDuration, stringValue("P1D1D"), false},
		{"duration unknown", ir.PrimitiveDuration, stringValue("P1X"), false},
		{"duration date after time", ir.PrimitiveDuration, stringValue("PT1H1D"), false},
		{"date number", ir.PrimitiveDate, number("1"), false},
		{"bytes", ir.PrimitiveBytes, ir.Value{Kind: ir.ValueBytes, Bytes: []byte("x")}, false},
		{"any", ir.PrimitiveAny, ir.Value{Kind: ir.ValueObject}, true},
		{"list against integer", ir.PrimitiveInteger, ir.Value{Kind: ir.ValueList, List: []ir.Value{number("1")}}, false},
		{"unknown kind", ir.PrimitiveKind("nope"), stringValue("x"), false},
	} {
		assert.Equal(t, tc.want, Admits(tc.kind, tc.v), "%s: %s of %v", tc.name, tc.kind, tc.v)
	}
}

// TestAdmitsFormat_JudgesTimeAsAFullTime pins the boundary of the one format
// judged by name: time, RFC 3339's full-time, which carries the offset the time
// kind refuses. Any other name, a table format's among them, is held by
// nothing, and time is held only while the table pairs it with no kind.
func TestAdmitsFormat_JudgesTimeAsAFullTime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		format       string
		v            ir.Value
		admits, held bool
	}{
		{"a Z offset", "time", stringValue("23:59:59Z"), true, true},
		{"a fraction and a numeric offset", "time", stringValue("23:59:59.5+01:00"), true, true},
		{"a lower case z", "time", stringValue("23:59:59z"), true, true},
		{"the widest offset", "time", stringValue("23:59:59-23:59"), true, true},
		{"no offset", "time", stringValue("23:59:59"), false, true},
		{"a date before it", "time", stringValue("2024-01-01T23:59:59Z"), false, true},
		{"a comma fraction", "time", stringValue("23:59:59,5Z"), false, true},
		{"an empty fraction", "time", stringValue("23:59:59.Z"), false, true},
		{"an offset hour of 24", "time", stringValue("23:59:59+24:00"), false, true},
		{"an offset minute of 60", "time", stringValue("23:59:59+05:60"), false, true},
		{"an offset with no colon", "time", stringValue("23:59:59+0500"), false, true},
		{"an hour of 24", "time", stringValue("24:00:00Z"), false, true},
		{"a leap second", "time", stringValue("23:59:60Z"), true, true},
		{"a leap second behind UTC", "time", stringValue("15:59:60-08:00"), true, true},
		{"a leap second at the wrong minute", "time", stringValue("23:58:60Z"), false, true},
		{"a second of 61", "time", stringValue("23:59:61Z"), false, true},
		{"a number", "time", number("1"), false, true},
		{"a symbol spelling one", "time", ir.Value{Kind: ir.ValueSymbol, String: "23:59:59Z"}, false, true},
		{"a table format", "date-time", stringValue("2024-01-01T23:59:59Z"), false, false},
		{"a format with no grammar here", "email", stringValue("a@b.c"), false, false},
		{"time in another case", "Time", stringValue("23:59:59Z"), false, false},
	} {
		admits, held := admitsFormat(tc.format, tc.v)
		assert.Equal(t, tc.admits, admits, "%s: %s of %v", tc.name, tc.format, tc.v)
		assert.Equal(t, tc.held, held, "%s: whether %s is held", tc.name, tc.format)
	}
	_, paired := typeformat.Lookup("string/time")
	assert.False(t, paired, "a held format the table pairs with a kind would have two judges")
}

// TestAdmitsFormat_SharesNoStringWithAKind pins what formatAgainst assumes:
// no value of a string-format kind is a time, and no time is a value of one.
// Each sample is a value of the kind or format it is listed under, so a kind
// that comes to admit a time, or a time that a kind admits, fails here, and so
// does a string-format kind ir declares with no sample listed.
func TestAdmitsFormat_SharesNoStringWithAKind(t *testing.T) {
	t.Parallel()
	times := []string{"23:59:59Z", "23:59:59.5+01:00", "15:59:60-08:00", "00:00:00z"}
	samples := map[ir.PrimitiveKind][]string{
		ir.PrimitiveDate:           {"2024-02-29"},
		ir.PrimitiveTime:           {"23:59:59", "23:59:60.5"},
		ir.PrimitiveDatetime:       {"2024-02-29T23:59:59", "2024-02-29t23:59:59.5"},
		ir.PrimitiveDatetimeOffset: {"2024-02-29T23:59:59Z", "2016-12-31T23:59:60Z"},
		ir.PrimitiveDuration:       {"P1D", "PT12H"},
		ir.PrimitiveUUID:           {"00000000-0000-0000-0000-000000000000"},
		ir.PrimitiveURL:            {"urn:t:23:59:59Z", "http://h/23:59:59Z"},
	}
	all := slices.Clone(times)
	for primitiveKind, ss := range samples {
		for _, s := range ss {
			require.True(t, Admits(primitiveKind, stringValue(s)), "%q is a value of %s", s, primitiveKind)
		}
		all = append(all, ss...)
	}
	for _, s := range times {
		admits, _ := admitsFormat("time", stringValue(s))
		require.True(t, admits, "%q is a time", s)
	}

	pkg, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	consts, err := pkg.StringConsts("PrimitiveKind")
	require.NoError(t, err)
	for _, declaredConst := range consts {
		primitiveKind := ir.PrimitiveKind(declaredConst.Value)
		if primitiveKind == ir.PrimitiveBytes || typeformat.Relate(primitiveKind, ir.PrimitiveString) != typeformat.ANarrower {
			continue
		}
		assert.NotEmpty(t, samples[primitiveKind], "%s refines string and has no sample", primitiveKind)
		for _, s := range all {
			inTime, _ := admitsFormat("time", stringValue(s))
			assert.False(t, inTime && Admits(primitiveKind, stringValue(s)), "%q is both a time and a value of %s", s, primitiveKind)
		}
	}
}

// TestAdmits_ReadsAHugeExponentWithoutMaterializingIt pins that a literal no
// machine type holds is refused promptly rather than built.
func TestAdmits_ReadsAHugeExponentWithoutMaterializingIt(t *testing.T) {
	t.Parallel()
	start := time.Now()
	for _, primitiveKind := range []ir.PrimitiveKind{ir.PrimitiveInt64, ir.PrimitiveUint64, ir.PrimitiveFloat32, ir.PrimitiveFloat64, ir.PrimitiveDecimal128} {
		assert.False(t, Admits(primitiveKind, number("1e1000001")), "%s", primitiveKind)
		assert.False(t, Admits(primitiveKind, number("1e99999999999999999999")), "%s", primitiveKind)
	}
	assert.Less(t, time.Since(start), 5*time.Second)
}

// TestDecimalOf_DeclinesWhatIsNoReadableNumber pins that a value with no number
// to read is declined rather than read as zero: a literal outside the grammar, one
// past the exponent range a BigVal holds, and a value that is no number at all.
func TestDecimalOf_DeclinesWhatIsNoReadableNumber(t *testing.T) {
	t.Parallel()
	for _, value := range []ir.Value{number("1p4"), number("1e1000000000000000000"), stringValue("1")} {
		_, ok := decimalOf(value)

		assert.False(t, ok, "%v", value)
	}

	decimal, ok := decimalOf(number("1e1000001"))
	assert.True(t, ok)
	assert.Equal(t, int64(1000001), decimal.Exponent())
}

// TestInRange_DeclinesABoundThatReadsAsNoNumber pins the second half of the
// judge: a range whose end is no number admits nothing, where reading it as zero
// would admit everything above it.
func TestInRange_DeclinesABoundThatReadsAsNoNumber(t *testing.T) {
	t.Parallel()
	assert.True(t, inRange(number("5"), "0", "10"))
	assert.False(t, inRange(number("5"), "1p4", "10"), "a low end outside the grammar")
	assert.False(t, inRange(number("5"), "0", "1p4"), "a high end outside the grammar")
	assert.False(t, inRange(number("-1"), "-5", "1p4"), "a high end that would read as zero, above this value")
}
