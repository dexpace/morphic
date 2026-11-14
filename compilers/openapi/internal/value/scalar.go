// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package value

import (
	"cmp"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// ScalarKind is the kind of value a YAML scalar holds, read by its resolved
// tag.
type ScalarKind uint8

const (
	// ScalarNull is a null.
	ScalarNull ScalarKind = iota
	// ScalarBool is true or false.
	ScalarBool
	// ScalarNumber is a number, its exact decimal in Scalar.Num.
	ScalarNumber
	// ScalarString is a string, its text in Scalar.Text.
	ScalarString
	// ScalarTimestamp is a YAML timestamp. JSON has no such type, and the
	// resolved time is derivable from the text while the text is not from the
	// time, so the text is what a renderer keeps (GitHub #242).
	ScalarTimestamp
	// ScalarBinary is base64 text tagged !!binary, its bytes in Scalar.Bytes.
	ScalarBinary
	// ScalarTagged is a scalar under a tag YAML gives no type, such as a
	// document's own `!acme`: text a renderer either keeps or refuses.
	ScalarTagged
)

// Scalar is one YAML scalar classified: its kind, its text as written, and
// the value its kind reads that text as.
type Scalar struct {
	Text  string
	Num   ir.BigVal
	Bytes []byte
	Kind  ScalarKind
	Bool  bool
}

// Classify reads the scalar node n by its resolved tag, never by the schema
// around it: the one classification a value lowered into the IR and a payload
// kept verbatim both render, so the two never read one scalar as two kinds
// (GitHub #783, #784). The tag is the parser's
// (docs/parser-library.md#scalar-resolution), and ShortTag resolves a node
// built by hand as a parse does.
//
// A scalar whose text its tag does not admit, as `!!bool maybe` or `.inf`, is
// an error, as yaml.v3's decoding refuses it, never text of a kind the source
// did not write (docs/compilers/openapi.md#values).
func Classify(n *yaml.Node) (Scalar, error) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return Scalar{}, errors.New("openapi: not a scalar")
	}
	s := Scalar{Text: n.Value}
	switch tag := n.ShortTag(); tag {
	case "!!null":
		return s, decodes(n, new(any), "null")
	case "!!bool":
		s.Kind = ScalarBool
		return s, decodes(n, &s.Bool, "bool")
	case "!!int", "!!float":
		num, err := NumericLiteral(n)
		if err != nil {
			return Scalar{}, fmt.Errorf("openapi: numeric literal %q: %w", n.Value, err)
		}
		s.Kind, s.Num = ScalarNumber, num
		return s, nil
	case "!!str", "!!merge":
		return text(n, s)
	case "!!timestamp":
		s.Kind = ScalarTimestamp
		return s, decodes(n, new(time.Time), "timestamp")
	case "!!binary":
		return binary(n, s)
	default:
		s.Kind = ScalarTagged
		return s, nil
	}
}

// text classifies a scalar YAML resolved to a string. A plain one that spells
// a decimal is a number all the same: yaml.v3 resolves a number past float64's
// range to a string, and the source wrote a number (GitHub #16). One whose
// power of ten no BigVal holds (ir.MaxDecimalExp) is refused, as a !!float
// past it is, rather than read as the text it is not. A quoted one, or one
// tagged !!str outright, is the string it says.
//
// `<<` written as a value, which yaml.v3 tags !!merge wherever it stands, is
// the text it spells: only a key merges.
func text(n *yaml.Node, s Scalar) (Scalar, error) {
	s.Kind = ScalarString
	if n.Style != 0 || n.ShortTag() != "!!str" {
		return s, nil
	}
	num, err := ir.NewBigVal(n.Value)
	switch {
	case err == nil:
		s.Kind, s.Num = ScalarNumber, num
		return s, nil
	case errors.Is(err, ir.ErrExponentRange):
		return Scalar{}, fmt.Errorf("openapi: numeric literal %q: %w", n.Value, err)
	default:
		return s, nil
	}
}

// binary classifies a !!binary scalar, decoding its base64 to the bytes it
// holds. yaml.v3 base64-decodes a !!binary node into a string (it rejects a
// []byte target), so the decode goes through one. The error names no text,
// which may be any length of bytes.
func binary(n *yaml.Node, s Scalar) (Scalar, error) {
	var raw string
	if err := n.Decode(&raw); err != nil {
		return Scalar{}, fmt.Errorf("openapi: binary literal: %w", err)
	}
	s.Kind, s.Bytes = ScalarBinary, []byte(raw)
	return s, nil
}

// decodes reports whether yaml.v3 decodes n into target, which is the check
// that n's text is one its tag admits.
func decodes(n *yaml.Node, target any, what string) error {
	if err := n.Decode(target); err != nil {
		return fmt.Errorf("openapi: %s literal %q: %w", what, n.Value, err)
	}
	return nil
}

// NumericLiteral converts a numeric-tagged YAML scalar into the BigVal of the
// value YAML 1.2's core schema reads it as (docs/parser-library.md#scalar-resolution):
// an !!int spelling a core integer, [-+]?[0-9]+, 0o[0-7]+ or 0x[0-9a-fA-F]+, is
// read exactly in the base its prefix names (coreLiteral), so 0644 is 644 and
// 0x1f is 31, whatever tagged it. Any other !!int, which only a tag the document writes
// makes one, keeps the parser's decode.
//
// A !!float keeps its text, because decoding one would round it through
// float64 and lose the arbitrary precision BigVal exists to preserve; only its
// separators are dropped.
func NumericLiteral(node *yaml.Node) (ir.BigVal, error) {
	if node == nil {
		return "", errors.New("openapi: nil numeric node")
	}
	switch node.ShortTag() {
	case "!!int":
		if c, ok := nodeview.ParseCoreInteger(node.Value); ok {
			return coreLiteral(c)
		}
		return decodeIntLiteral(node)
	case "!!float":
		return ir.NewBigVal(strings.ReplaceAll(node.Value, "_", ""))
	default:
		// A non-numeric tag — a quoted bound, say — carries no YAML-assigned
		// base, so its text reads as the decimal literal it looks like.
		return ir.NewBigVal(node.Value)
	}
}

// maxBasedDigits bounds the digits of an 0o or 0x integer NumericLiteral
// reads: rendering one in base ten costs time quadratic in its digits. A
// base-ten integer is copied, at any length.
const maxBasedDigits = 256

// coreLiteral renders c, an integer the core schema spells, as its decimal
// literal: a base-ten one as written, its leading zeros dropped, and an 0o or
// 0x one converted, up to maxBasedDigits digits.
func coreLiteral(c nodeview.CoreInteger) (ir.BigVal, error) {
	if c.Base == 10 {
		digits := strings.TrimLeft(c.Digits, "0")
		if digits != "" && c.Negative {
			digits = "-" + digits
		}
		return ir.NewBigVal(cmp.Or(digits, "0"))
	}
	if len(c.Digits) > maxBasedDigits {
		return "", fmt.Errorf("openapi: a base-%d integer of %d digits is past the %d digits read",
			c.Base, len(c.Digits), maxBasedDigits)
	}
	v, _ := new(big.Int).SetString(c.Digits, c.Base) // the grammar admits digits of the base alone
	return ir.NewBigVal(v.String())
}

// decodeIntLiteral renders an !!int node as its exact decimal literal. The
// signed range is tried first so a negative value is never read back as a large
// positive one; the unsigned range covers the int64..uint64 band above it.
//
// A magnitude past uint64 reaches an !!int node only when the source tags one
// explicitly (yaml.v3 resolves an untagged one as !!float), so nothing can
// decode it and its text is taken at face value. That is safe only for a
// spelling readsInBaseTen accepts: one with no base prefix.
func decodeIntLiteral(node *yaml.Node) (ir.BigVal, error) {
	var signed int64
	if err := node.Decode(&signed); err == nil {
		return ir.NewBigVal(strconv.FormatInt(signed, 10))
	}
	var unsigned uint64
	if err := node.Decode(&unsigned); err == nil {
		return ir.NewBigVal(strconv.FormatUint(unsigned, 10))
	}
	plain := strings.ReplaceAll(node.Value, "_", "")
	if !readsInBaseTen(plain) {
		return "", fmt.Errorf("openapi: integer literal %q resolves to no base-10 value", node.Value)
	}
	return ir.NewBigVal(plain)
}

// readsInBaseTen reports whether YAML reads the !!int spelling s in base 10.
// Every YAML base prefix (0b, 0o, 0x, and YAML 1.1's bare 0) starts with a
// zero, so an optionally signed digit run with no leading zero is the base-10
// spelling. The decimal grammar cannot answer this: 0644 is a decimal literal,
// and YAML's 420.
func readsInBaseTen(s string) bool {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if s == "" || (s[0] == '0' && len(s) > 1) {
		return false
	}
	return strings.TrimLeft(s, "0123456789") == ""
}
