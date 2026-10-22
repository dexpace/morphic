package merge

import (
	"net/url"
	"strings"
	"time"
)

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

// uuidLength is the length of the 8-4-4-4-12 form isUUID reads: 32
// hexadecimal digits and four hyphens.
const uuidLength = 36

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
