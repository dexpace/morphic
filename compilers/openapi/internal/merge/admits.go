package merge

import (
	"math/big"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

// Bounds of the fixed-width integers, as the decimal literals inRange compares.
const (
	int8Min   ir.BigVal = "-128"
	int8Max   ir.BigVal = "127"
	int16Min  ir.BigVal = "-32768"
	int16Max  ir.BigVal = "32767"
	int32Min  ir.BigVal = "-2147483648"
	int32Max  ir.BigVal = "2147483647"
	int64Min  ir.BigVal = "-9223372036854775808"
	int64Max  ir.BigVal = "9223372036854775807"
	uint8Max  ir.BigVal = "255"
	uint16Max ir.BigVal = "65535"
	uint32Max ir.BigVal = "4294967295"
	uint64Max ir.BigVal = "18446744073709551615"
)

// The IEEE 754 formats admits judges against: decimal128's digit budget and
// exponent range, and the mantissa width and top exponent of each binary float.
const (
	decimal128Digits    = 34
	decimal128MaxMsdExp = 6144
	decimal128MinLowExp = -6176
	float32Mantissa     = 24
	float32MaxExp       = 128
	float64Mantissa     = 53
	float64MaxExp       = 1024
	uuidLength          = 36
)

// admits reports whether v is a value of the primitive kind k, judged exactly:
// a bounded read of a number rather than a conversion, so an exponent no
// machine type holds is refused, not overflowed. A kind with no judge admits
// nothing, which keeps a value off a field it cannot be shown to fit.
//
// A null is not judged here: whether null is a value depends on the merged
// field's nullability, not on a kind.
func admits(k ir.PrimKind, v ir.Value) bool {
	switch k {
	case ir.PrimAny:
		return true
	case ir.PrimBool:
		return v.Kind == ir.ValueBool
	case ir.PrimString:
		return v.Kind == ir.ValueString
	case ir.PrimInt8:
		return inRange(v, int8Min, int8Max)
	case ir.PrimInt16:
		return inRange(v, int16Min, int16Max)
	case ir.PrimInt32:
		return inRange(v, int32Min, int32Max)
	case ir.PrimInt64:
		return inRange(v, int64Min, int64Max)
	case ir.PrimUint8:
		return inRange(v, "0", uint8Max)
	case ir.PrimUint16:
		return inRange(v, "0", uint16Max)
	case ir.PrimUint32:
		return inRange(v, "0", uint32Max)
	case ir.PrimUint64:
		return inRange(v, "0", uint64Max)
	case ir.PrimInteger:
		r, ok := reading(v)
		return ok && r.Integral
	case ir.PrimNumber, ir.PrimFloat, ir.PrimDecimal:
		_, ok := reading(v)
		return ok
	case ir.PrimDecimal128:
		return admitsDecimal128(v)
	case ir.PrimFloat32:
		return admitsFloat(v, float32Mantissa, float32MaxExp)
	case ir.PrimFloat64:
		return admitsFloat(v, float64Mantissa, float64MaxExp)
	case ir.PrimDate:
		return v.Kind == ir.ValueString && parses("2006-01-02", v.Str)
	case ir.PrimTime:
		return v.Kind == ir.ValueString && parsesRFC3339(v.Str, false)
	case ir.PrimDatetimeOffset:
		return v.Kind == ir.ValueString && parsesRFC3339(v.Str, true)
	case ir.PrimUUID:
		return v.Kind == ir.ValueString && isUUID(v.Str)
	case ir.PrimURL:
		return v.Kind == ir.ValueString && isAbsoluteURI(v.Str)
	case ir.PrimDuration:
		return v.Kind == ir.ValueString && isDuration(v.Str)
	case ir.PrimBytes, ir.PrimDatetime:
		return false
	default:
		return false
	}
}

// reading reads v as a number, reporting false for any other kind of value.
func reading(v ir.Value) (annotation.DecimalReading, bool) {
	if v.Kind != ir.ValueNumber {
		return annotation.DecimalReading{}, false
	}
	return annotation.ReadDecimal(v.Num)
}

// inRange reports whether v is an integral number from lo to hi inclusive.
func inRange(v ir.Value, lo, hi ir.BigVal) bool {
	r, ok := reading(v)
	if !ok || !r.Integral {
		return false
	}
	below, lowOK := annotation.CompareBigVal(v.Num, lo)
	above, highOK := annotation.CompareBigVal(v.Num, hi)
	return lowOK && highOK && below >= 0 && above <= 0
}

// admitsDecimal128 reports whether v fits IEEE 754 decimal128: at most 34
// significant digits, the leading one no higher than 10^6144 and the last no
// lower than 10^-6176.
func admitsDecimal128(v ir.Value) bool {
	r, ok := reading(v)
	if !ok {
		return false
	}
	if r.Zero {
		return true
	}
	lowest := new(big.Int).Sub(r.MsdExp, big.NewInt(int64(r.Digits-1)))
	return r.Digits <= decimal128Digits &&
		r.MsdExp.Cmp(big.NewInt(decimal128MaxMsdExp)) <= 0 &&
		lowest.Cmp(big.NewInt(decimal128MinLowExp)) >= 0
}

// admitsFloat reports whether v rounds to a finite value of a binary float
// with the given mantissa bits and top exponent: its magnitude is below
// 2^maxExp - 2^(maxExp-mantissa-1), the point at which round-to-nearest
// overflows. Rounding is allowed, as OpenAPI floats round.
func admitsFloat(v ir.Value, mantissa, maxExp uint) bool {
	if _, ok := reading(v); !ok {
		return false
	}
	limit := new(big.Int).Lsh(big.NewInt(1), maxExp)
	limit.Sub(limit, new(big.Int).Lsh(big.NewInt(1), maxExp-mantissa-1))
	text := limit.String()
	below, upOK := annotation.CompareBigVal(v.Num, ir.BigVal(text))
	above, downOK := annotation.CompareBigVal(v.Num, ir.BigVal("-"+text))
	return upOK && downOK && below < 0 && above > 0
}

// isAbsoluteURI reports whether s is an RFC 3986 URI with a scheme. The bytes
// are scanned first, since url.Parse accepts what the grammar does not (a
// space, a brace, a non-ASCII byte). The scan and ipLiteralPlaced own the
// characters and where brackets sit; url.Parse owns the structure and the
// IP-literal's content. An IPvFuture host ([v1.x]) is refused, since url.Parse
// refuses it; a refused value is kept verbatim, never lost.
func isAbsoluteURI(s string) bool {
	if !uriBytesValid(s) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme != ""
}

// uriBytesValid reports whether s is a scheme, a colon and then a tail
// uriTailValid admits.
func uriBytesValid(s string) bool {
	if s == "" || !isLetter(s[0]) {
		return false
	}
	i := 1
	for i < len(s) && (isLetter(s[i]) || isDigit(s[i]) || strings.IndexByte("+-.", s[i]) >= 0) {
		i++
	}
	if i == len(s) || s[i] != ':' {
		return false
	}
	return uriTailValid(s[i+1:])
}

// uriTailValid reports whether rest, what follows a URI's scheme colon, holds
// only bytes RFC 3986 allows: unreserved, a gen-delim, a sub-delim or a
// percent-encoded octet. A bracket is allowed only in the authority (an
// IP-literal host), an "@" at most once there, and a fragment delimiter only
// once.
func uriTailValid(rest string) bool {
	authorityEnd := 0
	if strings.HasPrefix(rest, "//") {
		authorityEnd = 2 + strings.IndexAny(rest[2:]+"/", "/?#")
	}
	if !ipLiteralPlaced(rest[:authorityEnd]) || strings.Count(rest[:authorityEnd], "@") > 1 {
		return false
	}
	fragments := 0
	for i := 0; i < len(rest); i++ {
		switch c := rest[i]; {
		case c == '%':
			if i+2 >= len(rest) || !isHex(rest[i+1]) || !isHex(rest[i+2]) {
				return false
			}
			i += 2
		case c == '[' || c == ']':
			if i >= authorityEnd {
				return false
			}
		case c == '#':
			if fragments++; fragments > 1 {
				return false
			}
		case !isLetter(c) && !isDigit(c) && strings.IndexByte("-._~:/?@!$&'()*+,;=", c) < 0:
			return false
		}
	}
	return true
}

// ipLiteralPlaced reports whether authority's brackets, if it has any, enclose
// its whole host, which RFC 3986 allows only for an IP-literal: one "[" right
// after "//" or the userinfo's "@", and one "]" ending the authority or
// followed by the port's ":".
func ipLiteralPlaced(authority string) bool {
	if !strings.ContainsAny(authority, "[]") {
		return true
	}
	host := authority[2:]
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	end := strings.IndexByte(host, ']')
	return strings.HasPrefix(host, "[") && end > 0 &&
		strings.Count(authority, "[") == 1 && strings.Count(authority, "]") == 1 &&
		(end == len(host)-1 || host[end+1] == ':')
}

// isLetter reports whether c is an ASCII letter.
func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// isDigit reports whether c is an ASCII decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// parses reports whether s is a time in layout. Calendar fields are checked, so
// 2024-02-30 is refused.
func parses(layout, s string) bool {
	_, err := time.Parse(layout, s)
	return err == nil
}

// parsesRFC3339 reports whether s is an RFC 3339 §5.6 full-time, or with date
// a date-time: the grammar's shape first, since time.Parse accepts a comma
// fraction and an offset hour above 23, then time.Parse for the calendar and
// range. The T separator and the Z offset may be lower case, which time.Parse
// refuses, so both are upper-cased once the shape is known. A second of 60 is
// a leap second, admitted where the time in UTC is 23:59 (§5.7); time.Parse
// refuses every one, so it is parsed as 59 and its hour and minute checked.
func parsesRFC3339(s string, date bool) bool {
	layout, clock := "15:04:05Z07:00", s
	if date {
		if len(s) < len("2006-01-02T") || s[10] != 'T' && s[10] != 't' {
			return false
		}
		layout, clock = time.RFC3339, s[11:]
		s = s[:10] + "T" + clock
	}
	if !fullTimeShape(clock) {
		return false
	}
	if last := len(s) - 1; s[last] == 'z' {
		s = s[:last] + "Z"
	}
	sec := len(s) - len(clock) + len("15:04:")
	leap := s[sec:sec+2] == "60"
	if leap {
		s = s[:sec] + "59" + s[sec+2:]
	}
	t, err := time.Parse(layout, s)
	return err == nil && (!leap || t.UTC().Hour() == 23 && t.UTC().Minute() == 59)
}

// fullTimeShape reports whether s is hh:mm:ss, an optional fraction of a dot
// and at least one digit, and an offset.
func fullTimeShape(s string) bool {
	const clockLength = len("15:04:05")
	if len(s) < clockLength || s[2] != ':' || s[5] != ':' {
		return false
	}
	for _, i := range []int{0, 1, 3, 4, 6, 7} {
		if !isDigit(s[i]) {
			return false
		}
	}
	rest := s[clockLength:]
	if strings.HasPrefix(rest, ".") {
		end := 1
		for end < len(rest) && isDigit(rest[end]) {
			end++
		}
		if end == 1 {
			return false
		}
		rest = rest[end:]
	}
	return isOffset(rest)
}

// isOffset reports whether s is Z, z or ±hh:mm with hh 00-23 and mm 00-59.
func isOffset(s string) bool {
	if s == "Z" || s == "z" {
		return true
	}
	if len(s) != len("+05:30") || s[0] != '+' && s[0] != '-' || s[3] != ':' {
		return false
	}
	hour, minute := twoDigits(s[1:3]), twoDigits(s[4:6])
	return hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

// twoDigits reads s, two bytes, as a number, or -1 when either is not a digit.
func twoDigits(s string) int {
	if !isDigit(s[0]) || !isDigit(s[1]) {
		return -1
	}
	return int(s[0]-'0')*10 + int(s[1]-'0')
}

// isUUID reports whether s is the 8-4-4-4-12 hexadecimal form.
func isUUID(s string) bool {
	if len(s) != uuidLength {
		return false
	}
	for i := range len(s) {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHex(s[i]) {
				return false
			}
		}
	}
	return true
}

// isHex reports whether c is a hexadecimal digit.
func isHex(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// isDuration reports whether s is an ISO 8601 duration as RFC 3339 Appendix A
// writes it: P, then a week count alone, or a date part and a time part, each
// a run of consecutive designators from years to days and from hours to
// seconds, at least one present.
func isDuration(s string) bool {
	rest, ok := strings.CutPrefix(s, "P")
	if !ok || rest == "" {
		return false
	}
	if weeks, found := strings.CutSuffix(rest, "W"); found {
		return isDigitRun(weeks)
	}
	datePart, timePart, hasTime := strings.Cut(rest, "T")
	if hasTime && timePart == "" || !hasTime && datePart == "" {
		return false
	}
	return consecutiveDesignators(datePart, "YMD") && consecutiveDesignators(timePart, "HMS")
}

// consecutiveDesignators reports whether s is empty or a run of digit-led
// designators each following the previous in order, starting anywhere.
func consecutiveDesignators(s, order string) bool {
	next := 0
	for s != "" {
		end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
		if end <= 0 || end == len(s) {
			return false
		}
		at := strings.IndexByte(order[next:], s[end])
		if at < 0 || (next > 0 && at != 0) {
			return false
		}
		next += at + 1
		s = s[end+1:]
	}
	return true
}

// isDigitRun reports whether s is a non-empty run of decimal digits.
func isDigitRun(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// admitsConstraints reports whether v meets every keyword of c that applies to
// a value of its kind, judged exactly. A keyword with no judge, a pattern or a
// decimal precision or scale, refuses every value it applies to, so a
// constrained node admits nothing it cannot be shown to fit. A keyword for
// another kind of value, maxLength on a number, constrains nothing.
func admitsConstraints(c *ir.Constraints, v ir.Value, depth int) bool {
	if c == nil {
		return true
	}
	switch v.Kind {
	case ir.ValueNumber:
		return admitsNumber(c, v.Num)
	case ir.ValueString:
		return c.Pattern == "" && within(int64(utf8.RuneCountInString(v.Str)), c.MinLength, c.MaxLength)
	case ir.ValueBytes:
		return within(int64(len(v.Bytes)), c.MinLength, c.MaxLength)
	case ir.ValueList:
		return within(int64(len(v.List)), c.MinItems, c.MaxItems) && (!c.UniqueItems || distinct(v.List, depth))
	case ir.ValueObject:
		return within(int64(len(v.Object)), c.MinProps, c.MaxProps)
	case ir.ValueNull, ir.ValueBool, ir.ValueSymbol, ir.ValueRefKind, ir.ValueCtor:
		// No keyword constrains a null or a bool, and the kind judge refuses the
		// rest before any keyword is read.
		return true
	default:
		return false
	}
}

// admitsNumber reports whether n meets c's numeric keywords: each bound, and
// multipleOf where annotation.IsMultipleOf can say. Precision and scale have no
// judge here.
func admitsNumber(c *ir.Constraints, n ir.BigVal) bool {
	if c.Precision != nil || c.Scale != nil {
		return false
	}
	bounds := []struct {
		bound *ir.BigVal
		holds func(order int) bool
	}{
		{c.Min, func(order int) bool { return order >= 0 }},
		{c.ExclusiveMin, func(order int) bool { return order > 0 }},
		{c.Max, func(order int) bool { return order <= 0 }},
		{c.ExclusiveMax, func(order int) bool { return order < 0 }},
	}
	for _, b := range bounds {
		if b.bound == nil {
			continue
		}
		if order, ok := annotation.CompareBigVal(n, *b.bound); !ok || !b.holds(order) {
			return false
		}
	}
	if c.MultipleOf == nil {
		return true
	}
	multiple, ok := annotation.IsMultipleOf(n, *c.MultipleOf)
	return ok && multiple
}

// within reports whether n lies between the inclusive counts lo and hi, either
// of which may be absent.
func within(n int64, lo, hi *int64) bool {
	return (lo == nil || n >= *lo) && (hi == nil || n <= *hi)
}

// distinct reports whether no two of vs are one JSON value (canonicalValue). A
// value it cannot spell canonically is judged not distinct, since its
// uniqueness cannot be shown.
func distinct(vs []ir.Value, depth int) bool {
	seen := make(map[string]bool, len(vs))
	for _, v := range vs {
		key, ok := canonicalValue(v, depth+1)
		if !ok || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// canonicalValue spells v so that two JSON-equal values spell alike: a number
// by its value (annotation.CanonicalDecimal), an object by its members in name
// order. ok is false for a kind JSON has no instance of, a number outside the
// decimal grammar, or nesting past maxJudgeDepth.
func canonicalValue(v ir.Value, depth int) (string, bool) {
	if depth > maxJudgeDepth {
		return "", false
	}
	switch v.Kind {
	case ir.ValueNull:
		return "null", true
	case ir.ValueBool:
		return strconv.FormatBool(v.Bool), true
	case ir.ValueString:
		return strconv.Quote(v.Str), true
	case ir.ValueNumber:
		return annotation.CanonicalDecimal(v.Num)
	case ir.ValueList:
		return canonicalMembers("[", "]", len(v.List), func(i int) (string, bool) {
			return canonicalValue(v.List[i], depth+1)
		})
	case ir.ValueObject:
		fields := slices.SortedFunc(slices.Values(v.Object), func(a, b ir.Field) int { return strings.Compare(a.Name, b.Name) })
		return canonicalMembers("{", "}", len(fields), func(i int) (string, bool) {
			member, ok := canonicalValue(fields[i].Value, depth+1)
			return strconv.Quote(fields[i].Name) + ":" + member, ok
		})
	case ir.ValueBytes, ir.ValueSymbol, ir.ValueRefKind, ir.ValueCtor:
		return "", false
	default:
		return "", false
	}
}

// canonicalMembers joins the n spellings member gives between open and close,
// or fails with the first member that cannot be spelled.
func canonicalMembers(open, closing string, n int, member func(i int) (string, bool)) (string, bool) {
	parts := make([]string, 0, n)
	for i := range n {
		part, ok := member(i)
		if !ok {
			return "", false
		}
		parts = append(parts, part)
	}
	return open + strings.Join(parts, ",") + closing, true
}
