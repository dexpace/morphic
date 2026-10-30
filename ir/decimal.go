package ir

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxDecimalExp bounds the power of ten a [Decimal] holds: the leading
// significant digit of a nonzero value carries one within ±MaxDecimalExp. The
// bound is on the value, not its spelling, so two spellings of one value are
// accepted or refused together. At 10¹⁸ − 1 every quantity derived from two
// exponents and a digit count, such as the gap between two values' lowest
// digits, stays inside int64.
const MaxDecimalExp = 999_999_999_999_999_999

// expSaturation caps what exponentValue reads. It is far enough past
// [MaxDecimalExp] that no digit count brings a saturated exponent back in range.
const expSaturation = 4_000_000_000_000_000_000

var (
	// ErrNotDecimal is wrapped by the refusal of text outside the decimal
	// numeric grammar: "1p4", "0x10", "NaN" and the empty string among it.
	ErrNotDecimal = errors.New("not a decimal numeric literal")
	// ErrExponentRange is what an [ExponentRangeError] wraps: the refusal of a
	// decimal whose leading digit carries a power of ten past [MaxDecimalExp].
	ErrExponentRange = errors.New("exponent out of range")
)

// ExponentRangeError is the refusal of a nonzero decimal whose leading digit
// carries a power of ten past ±[MaxDecimalExp]. It matches [ErrExponentRange]
// under errors.Is. No [Decimal] holds the value, but its sign and integrality
// still read, so a caller that asks only those of a literal is not bound by the
// range.
type ExponentRangeError struct {
	neg   bool
	small bool // the power of ten is below -MaxDecimalExp rather than above +MaxDecimalExp
}

// Error returns the text of [ErrExponentRange].
func (e *ExponentRangeError) Error() string { return ErrExponentRange.Error() }

// Unwrap returns [ErrExponentRange].
func (e *ExponentRangeError) Unwrap() error { return ErrExponentRange }

// Sign returns -1 or +1 as the refused value is negative or positive. It is
// never 0, since zero is held at any exponent.
func (e *ExponentRangeError) Sign() int {
	if e.neg {
		return -1
	}
	return 1
}

// IsInt reports whether the refused value is an integer. Past the top of the
// range it is, since only a digit count no string reaches would bring its last
// digit below the units place. Past the bottom it is a fraction.
func (e *ExponentRangeError) IsInt() bool { return !e.small }

// Decimal is the value a decimal numeric literal denotes, read without
// materializing it: a sign, the significant digits and the power of ten the
// leading digit carries, so 1e1000001 reads in constant space where math/big
// will not build it.
//
// Digits hold no leading or trailing zero and are empty exactly for zero, which
// is never negative. So == is value equality and a Decimal can key a map. The
// zero value is the number zero. [ParseDecimal] builds one.
type Decimal struct {
	neg    bool
	digits string
	exp    int64
}

// decimalLiteral is a numeric literal split as written: its sign, the digit
// runs either side of the point, whether a point was written at all, which a
// point with no digits after it leaves no other trace of, and the exponent with
// its e or E and sign, or "" when it has none. It keeps what [BigVal] spells and
// [Decimal] normalizes.
type decimalLiteral struct {
	neg           bool
	intPart, frac string
	point         bool
	exponent      string
}

// ParseDecimal reads s as a [Decimal]. It refuses text outside the decimal
// numeric grammar with [ErrNotDecimal], and a nonzero value whose leading digit
// carries a power of ten outside ±[MaxDecimalExp] with an [ExponentRangeError].
// Zero is accepted at any written exponent. The result is the zero Decimal on
// error, which reads as the number zero, so a caller must check the error.
func ParseDecimal(s string) (Decimal, error) {
	l, exp, err := readDecimal(s)
	if err != nil {
		return Decimal{}, fmt.Errorf("decimal: %q: %w", s, err)
	}
	return l.decimal(exp), nil
}

// readDecimal splits s and reads the power of ten its leading digit carries, so
// [NewBigVal] spells the literal from the split that [ParseDecimal] reads the
// value from. The split is substrings of s, so reading it allocates nothing.
// Its error is bare, for each of them to name itself in.
func readDecimal(s string) (decimalLiteral, int64, error) {
	l, ok := scanDecimal(s)
	if !ok {
		return decimalLiteral{}, 0, ErrNotDecimal
	}
	exp, err := l.leadExponent()
	if err != nil {
		return decimalLiteral{}, 0, err
	}
	return l, exp, nil
}

// scanDecimal splits s by the grammar sign? (digits ("." digits*)? | "." digits)
// ((e|E) sign? digits)? over ASCII and nothing else, and reports false for any
// other text. math/big's base-10 parser reads a wider grammar, a p/P binary
// exponent among it ("1p4" is 16), so none of it decides membership.
func scanDecimal(s string) (decimalLiteral, bool) {
	var l decimalLiteral
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		l.neg = s[i] == '-'
		i++
	}
	end := scanDigits(s, i)
	l.intPart, i = s[i:end], end
	if i < len(s) && s[i] == '.' {
		end = scanDigits(s, i+1)
		l.frac, l.point, i = s[i+1:end], true, end
	}
	if l.intPart == "" && l.frac == "" {
		return decimalLiteral{}, false // no digit anywhere: "", "+", ".", "Inf"
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		start := i
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		end = scanDigits(s, i)
		if end == i {
			return decimalLiteral{}, false // an e with no exponent digits
		}
		l.exponent, i = s[start:end], end
	}
	if i != len(s) {
		return decimalLiteral{}, false // unconsumed text: "0x10", "1p4", "5 "
	}
	return l, true
}

// scanDigits returns the index past the run of ASCII digits that starts at
// s[i].
func scanDigits(s string, i int) int {
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	return i
}

// isASCIIDigit reports whether b is a decimal digit. The grammar is ASCII only,
// so a byte scan is exact here and needs no rune decoding.
func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

// exponentValue reads the exponent text scanDecimal vouched for: empty, or an
// e or E, an optional sign and at least one digit. It saturates at
// expSaturation before multiplying, so a digit run of any length reads in one
// pass and its result never exceeds that cap.
func exponentValue(exponent string) int64 {
	if exponent == "" {
		return 0
	}
	body := exponent[1:] // past the e or E
	neg := body[0] == '-'
	if neg || body[0] == '+' {
		body = body[1:]
	}
	var e int64
	for i := range len(body) {
		if e >= expSaturation/10 {
			e = expSaturation
			break
		}
		e = e*10 + int64(body[i]-'0')
	}
	if neg {
		return -e
	}
	return e
}

// leadExponent returns the power of ten l's leading significant digit carries,
// or 0 for zero, which is accepted at any written exponent; any other value
// must carry it within ±[MaxDecimalExp]. It counts the digits where they lie
// rather than joining the two runs, so it allocates nothing.
//
// The sum below stays inside int64: exponentValue is at most 4·10¹⁸ in
// magnitude and the digit term at most len(s), which is below Go's largest
// allocation. Saturating an exponent never changes a verdict, since a saturated
// exponent plus any string's digit count is still past the bound.
func (l decimalLiteral) leadExponent() (int64, error) {
	zeros := l.leadingZeros()
	if zeros == len(l.intPart)+len(l.frac) {
		return 0, nil // zero, at any written exponent
	}
	exp := exponentValue(l.exponent) + int64(len(l.intPart)-zeros-1)
	if exp > MaxDecimalExp || exp < -MaxDecimalExp {
		return 0, &ExponentRangeError{neg: l.neg, small: exp < 0}
	}
	return exp, nil
}

// leadingZeros returns how many zeros open l's digits: the integer part's, and
// when those are all zeros, the fraction's after them.
func (l decimalLiteral) leadingZeros() int {
	zeros := len(l.intPart) - len(strings.TrimLeft(l.intPart, "0"))
	if zeros == len(l.intPart) {
		zeros += len(l.frac) - len(strings.TrimLeft(l.frac, "0"))
	}
	return zeros
}

// decimal reads l, whose leading digit carries the power of ten exp, as a
// [Decimal]. Its digits are the two runs joined, which costs an allocation when
// both hold one, so only [ParseDecimal] asks for them.
func (l decimalLiteral) decimal(exp int64) Decimal {
	lead := strings.TrimLeft(l.intPart+l.frac, "0")
	if lead == "" {
		return Decimal{} // zero, at any written exponent
	}
	return Decimal{neg: l.neg, digits: strings.TrimRight(lead, "0"), exp: exp}
}

// Sign returns -1, 0 or +1 as d is negative, zero or positive. Having no digits
// is checked first, so a literal that carries a minus and is zero is not read
// as negative.
func (d Decimal) Sign() int {
	switch {
	case d.digits == "":
		return 0
	case d.neg:
		return -1
	default:
		return 1
	}
}

// Cmp returns -1, 0 or +1 as d is less than, equal to or greater than e. It is
// exact however far apart the magnitudes are: sign first, then the power of ten
// the leading digit carries, then the digits as strings. That last step is exact
// because trailing zeros are stripped, so at one exponent a run that is a strict
// prefix of another is the smaller value.
func (d Decimal) Cmp(e Decimal) int {
	if order := cmp.Compare(d.Sign(), e.Sign()); order != 0 {
		return order
	}
	if d.digits == "" {
		return 0
	}
	order := cmp.Compare(d.exp, e.exp)
	if order == 0 {
		order = strings.Compare(d.digits, e.digits)
	}
	if d.neg {
		return -order
	}
	return order
}

// IsInt reports whether d has no fractional part. Zero is integral.
func (d Decimal) IsInt() bool {
	return d.digits == "" || d.exp >= int64(len(d.digits))-1
}

// Exponent returns the power of ten the leading significant digit carries, so
// 1500 is 3 and 0.05 is -2. It is 0 for zero.
func (d Decimal) Exponent() int64 { return d.exp }

// Digits returns the significant digits: no leading or trailing zero, and ""
// for zero.
func (d Decimal) Digits() string { return d.digits }

// String returns the canonical decimal of d: the leading digit, the others
// after a point, and the power of ten, so 1500, 15e2 and 1.5e3 are all "1.5e3".
// Every zero is "0". Two literals share a spelling exactly when they are one
// value.
func (d Decimal) String() string {
	if d.digits == "" {
		return "0"
	}
	var b strings.Builder
	if d.neg {
		b.WriteByte('-')
	}
	b.WriteByte(d.digits[0])
	if len(d.digits) > 1 {
		b.WriteByte('.')
		b.WriteString(d.digits[1:])
	}
	b.WriteByte('e')
	b.WriteString(strconv.FormatInt(d.exp, 10))
	return b.String()
}
