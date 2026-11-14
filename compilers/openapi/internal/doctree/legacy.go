// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// legacy appends to found what YAML 1.1 reads otherwise in n, a node of a
// document declaring YAML 1.1, before its text is restored: a plain value
// scalar YAML 1.1's types read as another JSON value than the core schema does,
// and a line-break character written raw in its text or in a comment attached
// to it, which YAML 1.1 ends a line at. A key is text, so its reading is none.
func (s *settler) legacy(found []Legacy, n *yaml.Node) []Legacy {
	if n.Kind == yaml.ScalarNode && n.Style == 0 && !s.keys[n] {
		was, is := yaml11Reading(n.Value), coreReading(n)
		long := len(n.Value) > maxReadText
		if long {
			was, is = was.kindOnly(), is.kindOnly()
		}
		if !was.same(is) {
			found = append(found, Legacy{Node: n, Was: was.String(), Is: is.String(), Kinds: long})
		}
	}
	if c := s.respelling.breakIn(n.Value); c != 0 {
		found = append(found, Legacy{Node: n, Break: c})
	}
	if c := s.respelling.breakIn(n.HeadComment + n.LineComment + n.FootComment); c != 0 {
		found = append(found, Legacy{Node: n, Break: c, InComment: true})
	}
	return found
}

// maxReadText bounds the text of a plain scalar whose two readings are
// computed and compared as values. A longer one is read and compared by kind
// alone, in time linear in its text: rendering a based or base-60 number in
// base ten costs time quadratic in its digits, and its wording would quote it.
const maxReadText = 128

// reading is the JSON value a plain scalar is read as, comparable: a string, a
// null, a boolean or a number, the number as a decimal (decimalOf).
type reading struct {
	text    string // the boolean, or the number's digits, or what reads as no finite number
	exp     int    // the number is its digits times ten to exp
	kind    readingKind
	neg     bool
	integer bool // whether the number was read as an integer, for its wording
}

// readingKind is the kind of JSON value a reading is.
type readingKind uint8

const (
	asString readingKind = iota
	asNull
	asBool
	asNumber
	asOther // an infinity, NaN, or a number past what the reading spells
)

// kindOnly is r with its value dropped, as a scalar past maxReadText compares.
func (r reading) kindOnly() reading {
	if r.kind == asString {
		return r
	}
	return reading{kind: asNumber, integer: r.integer && r.kind == asNumber, exp: valueless}
}

// same reports whether r and o are one JSON value, however each was read.
func (r reading) same(o reading) bool {
	r.integer, o.integer = false, false
	return r == o
}

// String words r as a finding names a reading: "a string", "the integer 420".
func (r reading) String() string {
	switch r.kind {
	case asNull:
		return "null"
	case asBool:
		return "the boolean " + r.text
	case asNumber:
		return r.numberString()
	case asOther:
		return r.text
	default:
		return "a string"
	}
}

// numberString words the number r is, or its kind alone for a reading of no
// value (kindOnly).
func (r reading) numberString() string {
	switch {
	case r.valueless():
		return map[bool]string{true: "an integer", false: "a number"}[r.integer]
	case r.integer:
		return "the integer " + r.decimal()
	default:
		return "the number " + r.decimal()
	}
}

// valueless reports whether r is a number read by kind alone: zero is spelled
// "0" with no digits, so a valueless reading is one marked so.
func (r reading) valueless() bool { return r.exp == valueless }

// valueless marks a number's exponent in a reading of its kind alone.
const valueless = math.MinInt

// decimal spells the number r is in plain decimal, or with an exponent when
// that would be long.
func (r reading) decimal() string {
	sign := ""
	if r.neg {
		sign = "-"
	}
	switch {
	case r.text == "":
		return "0"
	case r.exp > maxSpelledExp || r.exp < -maxSpelledExp:
		return sign + r.text + "e" + strconv.Itoa(r.exp)
	case r.exp >= 0:
		return sign + r.text + strings.Repeat("0", r.exp)
	case -r.exp < len(r.text):
		point := len(r.text) + r.exp
		return sign + r.text[:point] + "." + r.text[point:]
	default:
		return sign + "0." + strings.Repeat("0", -r.exp-len(r.text)) + r.text
	}
}

// maxSpelledExp bounds the zeros a number's wording spells out, and the
// exponent a reading reads: past it a number is worded with its exponent, and
// read as its text.
const maxSpelledExp = 1000

// coreReading reads n, a plain scalar the core schema has tagged (resolve), as
// YAML 1.2 does.
func coreReading(n *yaml.Node) reading {
	switch n.ShortTag() {
	case "!!null":
		return reading{kind: asNull}
	case "!!bool":
		return reading{kind: asBool, text: strings.ToLower(n.Value)}
	case "!!int":
		return coreInteger(n.Value)
	case "!!float":
		if coreInt(n.Value) {
			return coreInteger(n.Value)
		}
		return floatReading(n.Value)
	default:
		return reading{kind: asString}
	}
}

// coreInteger reads text, an integer of the core schema (coreInt).
func coreInteger(text string) reading {
	c, _ := nodeview.ParseCoreInteger(text)
	if c.Base == 10 {
		r, _ := decimalOf(c.Negative, c.Digits)
		r.integer = true
		return r
	}
	return integerOf(false, c.Digits, c.Base)
}

// yaml11Reading reads text, a plain scalar, by YAML 1.1's types: null, bool,
// int and float, in the published expression of each, and a string otherwise.
// A date is a timestamp there, which a string reads alike, so it is a string.
func yaml11Reading(text string) reading {
	switch text {
	case "", "~", "null", "Null", "NULL":
		return reading{kind: asNull}
	case "y", "Y", "yes", "Yes", "YES", "true", "True", "TRUE", "on", "On", "ON":
		return reading{kind: asBool, text: "true"}
	case "n", "N", "no", "No", "NO", "false", "False", "FALSE", "off", "Off", "OFF":
		return reading{kind: asBool, text: "false"}
	}
	if r, ok := yaml11Integer(text); ok {
		return r
	}
	if r, ok := yaml11Float(text); ok {
		return r
	}
	return reading{kind: asString}
}

// yaml11Integer reads text as YAML 1.1's int: binary, octal from a leading 0,
// decimal, hexadecimal and base 60, each digit run separated by `_` at will.
func yaml11Integer(text string) (reading, bool) {
	neg, s := strings.HasPrefix(text, "-"), unsigned(text)
	switch {
	case strings.HasPrefix(s, "0b"):
		return separated(neg, s[2:], 2)
	case strings.HasPrefix(s, "0x"):
		return separated(neg, s[2:], 16)
	case s == "0":
		return integerOf(neg, s, 10), true
	case strings.HasPrefix(s, "0"):
		return separated(neg, s[1:], 8)
	case s == "" || s[0] < '1' || s[0] > '9':
		return reading{}, false
	case strings.Contains(s, ":"):
		return sexagesimal(neg, s, "", false)
	default:
		return separated(neg, s, 10)
	}
}

// yaml11Float reads text as YAML 1.1's float: a decimal holding a point and a
// digit, its exponent signed, a base-60 one, an infinity or NaN.
func yaml11Float(text string) (reading, bool) {
	if r, ok := special(text); ok {
		return r, true
	}
	neg, s := strings.HasPrefix(text, "-"), unsigned(text)
	mantissa, exp, exponent := strings.Cut(strings.ReplaceAll(s, "e", "E"), "E")
	whole, fraction, dotted := strings.Cut(mantissa, ".")
	switch {
	case !dotted || !strings.ContainsAny(mantissa, "0123456789"):
		return reading{}, false
	case exponent && (exp == "" || (exp[0] != '+' && exp[0] != '-') || !digits(exp[1:], 10)):
		return reading{}, false
	case !separatedDigits(fraction, 10):
		return reading{}, false
	case strings.Contains(whole, ":"):
		if exponent || whole[0] < '0' || whole[0] > '9' {
			return reading{}, false
		}
		return sexagesimal(neg, whole, fraction, true)
	case whole != "" && (whole[0] < '0' || whole[0] > '9' || !separatedDigits(whole, 10)):
		return reading{}, false
	default:
		return decimalOf(neg, strings.ReplaceAll(whole+"."+fraction, "_", "")+exponentPart(exp, exponent))
	}
}

// exponentPart is the exponent a float writes, as it is spelled after its E.
func exponentPart(exp string, written bool) string {
	if !written {
		return ""
	}
	return "e" + exp
}

// special reads text as an infinity or NaN, which both versions spell alike.
func special(text string) (reading, bool) {
	switch text {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		return reading{kind: asOther, text: "positive infinity"}, true
	case "-.inf", "-.Inf", "-.INF":
		return reading{kind: asOther, text: "negative infinity"}, true
	case ".nan", ".NaN", ".NAN":
		return reading{kind: asOther, text: "NaN"}, true
	default:
		return reading{}, false
	}
}

// floatReading reads text, a float of the core schema (coreFloat).
func floatReading(text string) reading {
	if r, ok := special(text); ok {
		return r
	}
	r, _ := decimalOf(strings.HasPrefix(text, "-"), unsigned(text))
	return r
}

// sexagesimal reads whole, colon-separated base-60 digits, and, for a float,
// fraction, the decimal digits after its point, as YAML 1.1 reads a base-60
// number: each part after the first one or two digits below 60.
func sexagesimal(neg bool, whole, fraction string, float bool) (reading, bool) {
	parts := strings.Split(whole, ":")
	if !separatedDigits(parts[0], 10) {
		return reading{}, false
	}
	for _, p := range parts[1:] {
		if p == "" || len(p) > 2 || !digits(p, 10) || (len(p) == 2 && p[0] > '5') {
			return reading{}, false
		}
	}
	if len(whole)+len(fraction) > maxReadText {
		return reading{kind: asNumber, integer: !float, exp: valueless}, true
	}
	v, _ := new(big.Int).SetString(strings.ReplaceAll(parts[0], "_", ""), 10)
	for _, p := range parts[1:] {
		n, _ := strconv.Atoi(p)
		v.Mul(v, big.NewInt(60)).Add(v, big.NewInt(int64(n)))
	}
	if !float {
		return integerOf(neg, v.String(), 10), true
	}
	return decimalOf(neg, v.String()+"."+strings.ReplaceAll(fraction, "_", ""))
}

// separated reads digits of base, separated by `_` at will, as an integer.
func separated(neg bool, text string, base int) (reading, bool) {
	if text == "" || !separatedDigits(text, base) {
		return reading{}, false
	}
	return integerOf(neg, strings.ReplaceAll(text, "_", ""), base), true
}

// separatedDigits reports whether text is digits of base and `_` alone, one or
// more, or is empty.
func separatedDigits(text string, base int) bool {
	plain := strings.ReplaceAll(text, "_", "")
	return plain == "" || digits(plain, base)
}

// integerOf reads text, digits of base or none, as an integer, by kind alone
// past maxReadText.
func integerOf(neg bool, text string, base int) reading {
	if len(text) > maxReadText {
		return reading{kind: asNumber, integer: true, exp: valueless}
	}
	v, ok := new(big.Int).SetString(text, base)
	if !ok {
		v = new(big.Int)
	}
	r, _ := decimalOf(neg, v.String())
	r.integer = true
	return r
}

// decimalOf reads text, digits holding at most one point and then an exponent,
// as a number: its significant digits and the power of ten they are scaled by.
// An exponent past maxSpelledExp reads the number as its text.
func decimalOf(neg bool, text string) (reading, bool) {
	mantissa, exp, _ := strings.Cut(strings.ToLower(text), "e")
	e, err := strconv.Atoi(exp)
	if exp == "" {
		e, err = 0, nil
	}
	if err != nil || e > maxSpelledExp || e < -maxSpelledExp {
		return reading{kind: asOther, text: "the number " + text}, true
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	e -= len(fraction)
	trimmed := strings.TrimRight(digits, "0")
	e += len(digits) - len(trimmed)
	if trimmed == "" {
		return reading{kind: asNumber}, true
	}
	return reading{kind: asNumber, neg: neg, text: trimmed, exp: e}, true
}
