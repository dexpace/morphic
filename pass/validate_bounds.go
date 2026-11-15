// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// shape is the kind of value a position's type holds, as far as the checks of a
// value need to tell: what a bound applies to and what a value must be.
type shape uint8

const (
	// shapeUnjudged is a type the checks read no further: an enum, a literal, a
	// union, an external type, a scalar with no base, and one nothing declares.
	shapeUnjudged shape = iota
	shapeNumber
	shapeInteger
	shapeText
	shapeBytes
	shapeBoolean
	shapeList
	shapeObject
	shapeAnything
)

// maxAliasHops bounds how many alias scalars settle follows, so a cycle of
// aliases ends.
const maxAliasHops = 64

// primShape is the shape of a primitive of kind k. Every kind has one:
// TestPrimShapes_EveryPrimKindHasARow holds the cases to the kinds ir declares.
func primShape(k ir.PrimKind) shape {
	switch k {
	case ir.PrimFloat32, ir.PrimFloat64, ir.PrimFloat, ir.PrimNumber, ir.PrimDecimal, ir.PrimDecimal128:
		return shapeNumber
	case ir.PrimInt8, ir.PrimInt16, ir.PrimInt32, ir.PrimInt64,
		ir.PrimUint8, ir.PrimUint16, ir.PrimUint32, ir.PrimUint64, ir.PrimInteger:
		return shapeInteger
	case ir.PrimString, ir.PrimDate, ir.PrimTime, ir.PrimDatetime, ir.PrimDatetimeOffset,
		ir.PrimDuration, ir.PrimURL, ir.PrimUUID:
		return shapeText
	case ir.PrimBytes:
		return shapeBytes
	case ir.PrimBool:
		return shapeBoolean
	case ir.PrimAny:
		return shapeAnything
	default:
		return shapeUnjudged
	}
}

// bounds holds the tightest of each bound the positions of a chain of types
// declare together, so a value must meet every one. The numeric ends are held
// as numbers and as written, for a message to quote; the counts point at the
// document's own fields, so holding them allocates nothing.
type bounds struct {
	lo, hi             ir.Decimal
	loText, hiText     ir.BigVal
	loSet, hiSet       bool
	loOpen, hiOpen     bool
	minLen, maxLen     *int64
	minItems, maxItems *int64
	minProps, maxProps *int64
}

// and conjoins the bounds c declares into b. A numeric bound that does not
// parse is passed over: irverify reports it.
func (b *bounds) and(c *ir.Constraints) {
	if c == nil {
		return
	}
	b.raise(c.Min, false)
	b.raise(c.ExclusiveMin, true)
	b.lower(c.Max, false)
	b.lower(c.ExclusiveMax, true)
	b.minLen, b.maxLen = greater(b.minLen, c.MinLength), lesser(b.maxLen, c.MaxLength)
	b.minItems, b.maxItems = greater(b.minItems, c.MinItems), lesser(b.maxItems, c.MaxItems)
	b.minProps, b.maxProps = greater(b.minProps, c.MinProps), lesser(b.maxProps, c.MaxProps)
}

// raise takes v as the lower end where it is greater than the one held, or
// equal to it and exclusive.
func (b *bounds) raise(v *ir.BigVal, open bool) {
	if v == nil {
		return
	}
	d, err := v.Decimal()
	if err != nil {
		return
	}
	if c := d.Cmp(b.lo); !b.loSet || c > 0 || (c == 0 && open) {
		b.lo, b.loText, b.loSet, b.loOpen = d, *v, true, open
	}
}

// lower takes v as the upper end where it is less than the one held, or equal
// to it and exclusive.
func (b *bounds) lower(v *ir.BigVal, open bool) {
	if v == nil {
		return
	}
	d, err := v.Decimal()
	if err != nil {
		return
	}
	if c := d.Cmp(b.hi); !b.hiSet || c < 0 || (c == 0 && open) {
		b.hi, b.hiText, b.hiSet, b.hiOpen = d, *v, true, open
	}
}

// greater returns the greater of two minimums, either of which may be unset.
func greater(a, b *int64) *int64 {
	if a == nil || (b != nil && *b > *a) {
		return b
	}
	return a
}

// lesser returns the lesser of two maximums, either of which may be unset.
func lesser(a, b *int64) *int64 {
	if a == nil || (b != nil && *b < *a) {
		return b
	}
	return a
}

// settle follows alias scalars from target for at most maxAliasHops hops,
// conjoining what each declares into b, and returns the shape the value is, the
// type the chain ends at, and whether a scalar or list on the way holds an
// encoding. The type is nil where the chain meets nothing declared or runs past
// the hops, and the shape then is shapeUnjudged.
func settle(doc *ir.Document, target ir.TypeID, b *bounds) (shape, ir.TypeDef, bool) {
	encoded := false
	for range maxAliasHops {
		td := doc.Types[target]
		if ir.IsNilTypeDef(td) {
			return shapeUnjudged, nil, encoded
		}
		switch t := td.(type) {
		case *ir.Scalar:
			b.and(t.Constraints)
			encoded = encoded || t.Encoding != nil
			if t.Base == nil {
				return shapeUnjudged, td, encoded
			}
			target = t.Base.Target
		case *ir.Primitive:
			return primShape(t.Prim), td, encoded
		case *ir.List:
			b.and(t.Constraints)
			return shapeList, td, encoded || t.Encoding != nil
		case *ir.Tuple:
			b.and(t.Constraints)
			return shapeList, td, encoded
		case *ir.Model:
			b.and(t.Constraints)
			return shapeObject, td, encoded
		case *ir.MapT:
			b.and(t.Constraints)
			return shapeObject, td, encoded
		case *ir.Any:
			return shapeAnything, td, encoded
		default:
			return shapeUnjudged, td, encoded
		}
	}
	return shapeUnjudged, nil, encoded
}

// empty reports whether the bounds b holds admit no value of shape s. A bound
// that does not apply to s is not judged.
func (b *bounds) empty(s shape) bool {
	switch s {
	case shapeNumber, shapeInteger:
		if !b.loSet || !b.hiSet {
			return false
		}
		c := b.lo.Cmp(b.hi)
		return c > 0 || (c == 0 && (b.loOpen || b.hiOpen))
	case shapeText, shapeBytes:
		return crossed(b.minLen, b.maxLen)
	case shapeList:
		return crossed(b.minItems, b.maxItems)
	case shapeObject:
		return crossed(b.minProps, b.maxProps)
	default:
		return false
	}
}

// crossed reports whether the minimum and maximum are both set and no count
// lies between them.
func crossed(minimum, maximum *int64) bool {
	return minimum != nil && maximum != nil && *minimum > *maximum
}

// describe says which values b admits none of, for a shape s that b.empty
// holds.
func (b *bounds) describe(s shape) string {
	switch s {
	case shapeText:
		return fmt.Sprintf("bounds admit no string: none has at least %d characters and at most %d", *b.minLen, *b.maxLen)
	case shapeBytes:
		return fmt.Sprintf("bounds admit no bytes: none has at least %d bytes and at most %d", *b.minLen, *b.maxLen)
	case shapeList:
		return fmt.Sprintf("bounds admit no list: none has at least %d items and at most %d", *b.minItems, *b.maxItems)
	case shapeObject:
		return fmt.Sprintf("bounds admit no object: none has at least %d members and at most %d", *b.minProps, *b.maxProps)
	default:
		lo, hi := "at least", "at most"
		if b.loOpen {
			lo = "above"
		}
		if b.hiOpen {
			hi = "below"
		}
		return fmt.Sprintf("bounds admit no number: none is %s %s and %s %s", lo, b.loText, hi, b.hiText)
	}
}

// unsatisfiable returns what own, the bounds declared at a position or a type,
// leaves no value of, or "" when it leaves one. The position's chain is the
// alias scalars its type reaches, read from target, or the shape self where it
// has none of its own (a list, a tuple, a model, a map). It is told where the
// contradiction completes: the chain alone already admitting no value is the
// type that holds the first part's to tell, so it is left to it.
func unsatisfiable(doc *ir.Document, own *ir.Constraints, target ir.TypeID, self shape) string {
	if own == nil {
		return ""
	}
	var b bounds
	s := self
	if s == shapeUnjudged {
		s, _, _ = settle(doc, target, &b)
	}
	if b.empty(s) {
		return ""
	}
	b.and(own)
	if !b.empty(s) {
		return ""
	}
	return b.describe(s)
}

// checkBounds reports each position and type whose bounds, together with those
// of the alias scalars its reference reaches, admit no value of the kind they
// apply to (pass/bounds-admit-no-value, docs/ir/constraints.md). It does not
// judge an exclusive pair with no integer between, nor a multiple none of the
// range holds: ir.Decimal compares and does no arithmetic.
func checkBounds(doc *ir.Document, live []ir.TypeID) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range live {
		td := doc.Types[id]
		if why := typeUnsatisfiable(doc, td); why != "" {
			diags = append(diags, diagAt("pass/bounds-admit-no-value", why,
				string(id), td.Common().Provenance))
		}
	}
	var keys []ir.PropID
	eachPosition(doc, live, &keys, func(p *position) {
		if why := unsatisfiable(doc, p.own, p.typ.Target, shapeUnjudged); why != "" {
			diags = append(diags, diagAt("pass/bounds-admit-no-value", why, p.node, p.from))
		}
	})
	return diags
}

// typeUnsatisfiable is unsatisfiable for the bounds td declares itself: read
// with the chain typeBounds names, or, for a union, kind by kind
// (unionUnsatisfiable).
func typeUnsatisfiable(doc *ir.Document, td ir.TypeDef) string {
	if u, isUnion := td.(*ir.Union); isUnion {
		return unionUnsatisfiable(u.Constraints)
	}
	own, target, self := typeBounds(td)
	return unsatisfiable(doc, own, target, self)
}

// unionShapes are the shapes a union's own bounds are judged for, one per kind
// of value a bound applies to: each bounds the values of its kind whichever
// variant holds one (docs/ir/unions.md#bounds-beside-a-union).
var unionShapes = [...]shape{shapeNumber, shapeText, shapeList, shapeObject}

// unionUnsatisfiable returns what own, a union's bounds, which no alias chain
// joins, leave no value of, for the first kind in unionShapes they admit none
// of, or "" where they admit a value of each kind.
func unionUnsatisfiable(own *ir.Constraints) string {
	if own == nil {
		return ""
	}
	var b bounds
	b.and(own)
	for _, s := range unionShapes {
		if b.empty(s) {
			return b.describe(s)
		}
	}
	return ""
}

// typeBounds returns the bounds td declares, and the chain they are read with:
// the base of a scalar, or the shape of a list, tuple, model or map, whose
// bounds apply to it alone.
func typeBounds(td ir.TypeDef) (*ir.Constraints, ir.TypeID, shape) {
	switch t := td.(type) {
	case *ir.Scalar:
		if t.Base != nil {
			return t.Constraints, t.Base.Target, shapeUnjudged
		}
	case *ir.List:
		return t.Constraints, "", shapeList
	case *ir.Tuple:
		return t.Constraints, "", shapeList
	case *ir.Model:
		return t.Constraints, "", shapeObject
	case *ir.MapT:
		return t.Constraints, "", shapeObject
	default:
		// no other type declares bounds a value is read against with a chain;
		// a union's are judged kind by kind (typeUnsatisfiable)
	}
	return nil, "", shapeUnjudged
}
