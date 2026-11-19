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
	Text   string
	Number ir.BigVal
	Bytes  []byte
	Kind   ScalarKind
	Bool   bool
}

// Classify reads the scalar node node by its resolved tag, never by the schema
// around it: the one classification a value lowered into the IR and a payload
// kept verbatim both render, so the two never read one scalar as two kinds
// (GitHub #783, #784). The tag is the parser's
// (docs/parser-library.md#scalar-resolution), and ShortTag resolves a node
// built by hand as a parse does.
//
// A scalar whose text its tag does not admit, as `!!bool maybe` or `.inf`, is
// an error, as yaml.v3's decoding refuses it, never text of a kind the source
// did not write (docs/compilers/openapi.md#values).
func Classify(node *yaml.Node) (Scalar, error) {
	if node == nil || node.Kind != yaml.ScalarNode {
		return Scalar{}, errors.New("openapi: not a scalar")
	}
	scalar := Scalar{Text: node.Value}
	switch tag := node.ShortTag(); tag {
	case "!!null":
		return scalar, decodes(node, new(any), "null")
	case "!!bool":
		scalar.Kind = ScalarBool
		return scalar, decodes(node, &scalar.Bool, "bool")
	case "!!int", "!!float":
		number, err := NumericLiteral(node)
		if err != nil {
			return Scalar{}, fmt.Errorf("openapi: numeric literal %q: %w", node.Value, err)
		}
		scalar.Kind, scalar.Number = ScalarNumber, number
		return scalar, nil
	case "!!str", "!!merge":
		return text(node, scalar)
	case "!!timestamp":
		scalar.Kind = ScalarTimestamp
		return scalar, decodes(node, new(time.Time), "timestamp")
	case "!!binary":
		return binary(node, scalar)
	default:
		scalar.Kind = ScalarTagged
		return scalar, nil
	}
}

// JSONKind returns the kind of JSON value s reads as wherever only JSON's
// kinds are read, as an extension's payload is kept and a selector compares
// one: null, a boolean and a number as they are, and every other kind a string
// of the text the source wrote, a timestamp, a !!binary and a scalar under a
// document's own tag among them, since the spelling is not derivable from
// what they resolve to (GitHub #242).
func (s Scalar) JSONKind() ScalarKind {
	switch s.Kind {
	case ScalarNull, ScalarBool, ScalarNumber:
		return s.Kind
	default:
		return ScalarString
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
func text(node *yaml.Node, scalar Scalar) (Scalar, error) {
	scalar.Kind = ScalarString
	if node.Style != 0 || node.ShortTag() != "!!str" {
		return scalar, nil
	}
	number, err := ir.NewBigVal(node.Value)
	switch {
	case err == nil:
		scalar.Kind, scalar.Number = ScalarNumber, number
		return scalar, nil
	case errors.Is(err, ir.ErrExponentRange):
		return Scalar{}, fmt.Errorf("openapi: numeric literal %q: %w", node.Value, err)
	default:
		return scalar, nil
	}
}

// binary classifies a !!binary scalar, decoding its base64 to the bytes it
// holds. yaml.v3 base64-decodes a !!binary node into a string (it rejects a
// []byte target), so the decode goes through one. The error names no text,
// which may be any length of bytes.
func binary(node *yaml.Node, scalar Scalar) (Scalar, error) {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return Scalar{}, fmt.Errorf("openapi: binary literal: %w", err)
	}
	scalar.Kind, scalar.Bytes = ScalarBinary, []byte(raw)
	return scalar, nil
}

// decodes reports whether yaml.v3 decodes node into target, which is the check
// that node's text is one its tag admits.
func decodes(node *yaml.Node, target any, what string) error {
	if err := node.Decode(target); err != nil {
		return fmt.Errorf("openapi: %s literal %q: %w", what, node.Value, err)
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
		if coreInteger, ok := nodeview.ParseCoreInteger(node.Value); ok {
			return coreLiteral(coreInteger)
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

// coreLiteral renders coreInteger, an integer the core schema spells, as its
// decimal literal: a base-ten one as written, its leading zeros dropped, and an
// 0o or 0x one converted, up to maxBasedDigits digits.
func coreLiteral(coreInteger nodeview.CoreInteger) (ir.BigVal, error) {
	if coreInteger.Base == 10 {
		digits := strings.TrimLeft(coreInteger.Digits, "0")
		if digits != "" && coreInteger.Negative {
			digits = "-" + digits
		}
		return ir.NewBigVal(cmp.Or(digits, "0"))
	}
	if len(coreInteger.Digits) > maxBasedDigits {
		return "", fmt.Errorf("openapi: a base-%d integer of %d digits is past the %d digits read",
			coreInteger.Base, len(coreInteger.Digits), maxBasedDigits)
	}
	v, _ := new(big.Int).SetString(coreInteger.Digits, coreInteger.Base) // the grammar admits digits of the base alone
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
