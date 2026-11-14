// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"cmp"
	"context"
	"errors"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// scalarKind is the kind of JSON value a scalar is, or opaque for a scalar
// the compiler gives no JSON reading.
type scalarKind uint8

const (
	kindNull scalarKind = iota
	kindBool
	kindNumber
	kindString
	// kindOpaque is a scalar value.Classify refuses or reads as no JSON value,
	// a !!binary or a document's own tag among them: equal to itself alone.
	kindOpaque
)

// scalar is one primitive value: a literal of the query, a number a function
// computed, or a scalar of the document as the compiler reads it.
type scalar struct {
	// node is an opaque scalar's own node, which alone it equals.
	node *yaml.Node
	text string
	num  number
	kind scalarKind
	b    bool
}

// number is a decimal compared by value without being expanded. class orders
// what no Decimal holds: a value whose power of ten passes ±ir.MaxDecimalExp
// compares by its sign and side alone, past every Decimal (±3) or between zero
// and every Decimal of its sign (±1); a Decimal's class is twice its sign.
type number struct {
	d     ir.Decimal
	class int8
}

// newNumber reads text, a decimal literal, as a number, and reports whether
// it is one.
func newNumber(text string) (number, bool) {
	d, err := ir.ParseDecimal(text)
	if err == nil {
		return number{d: d, class: int8(2 * d.Sign())}, true
	}
	if past, ok := errors.AsType[*ir.ExponentRangeError](err); ok {
		return outOfRange(past), true
	}
	return number{}, false
}

// outOfRange is the number a decimal past ±ir.MaxDecimalExp reads as.
func outOfRange(past *ir.ExponentRangeError) number {
	side := int8(1)
	if past.IsInt() {
		side = 3
	}
	return number{class: side * int8(past.Sign())}
}

// countOf is the number n.
func countOf(n int) scalar {
	num, _ := newNumber(strconv.Itoa(n)) // a run of digits is a decimal
	return scalar{kind: kindNumber, num: num}
}

// compare orders two numbers.
func (n number) compare(o number) int {
	if c := cmp.Compare(n.class, o.class); c != 0 || n.class*n.class != 4 {
		return c
	}
	return n.d.Cmp(o.d)
}

// readScalar reads the scalar node n as the compiler reads it
// (value.Classify): null, a boolean, a number, a string, a timestamp as the
// string it spells, and anything else opaque.
func readScalar(n *yaml.Node) scalar {
	s, err := value.Classify(n)
	if err != nil {
		if past, ok := errors.AsType[*ir.ExponentRangeError](err); ok {
			return scalar{kind: kindNumber, num: outOfRange(past)}
		}
		return scalar{kind: kindOpaque, node: n}
	}
	switch s.Kind {
	case value.ScalarNull:
		return scalar{kind: kindNull}
	case value.ScalarBool:
		return scalar{kind: kindBool, b: s.Bool}
	case value.ScalarNumber:
		num, _ := newNumber(string(s.Num)) // a BigVal is a decimal in range
		return scalar{kind: kindNumber, num: num}
	case value.ScalarString, value.ScalarTimestamp:
		return scalar{kind: kindString, text: s.Text}
	default:
		return scalar{kind: kindOpaque, node: n}
	}
}

// sameScalar reports whether two scalars are equal.
func sameScalar(a, b scalar) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case kindNull:
		return true
	case kindBool:
		return a.b == b.b
	case kindNumber:
		return a.num.compare(b.num) == 0
	case kindString:
		return a.text == b.text
	default:
		return a.node == b.node
	}
}

// valueKind says what a value is: no value, a node of the document, or a
// scalar of the query's own.
type valueKind uint8

const (
	valueNothing valueKind = iota
	valueNode
	valueScalar
)

// jsonValue is what a comparable or a function of ValueType yields: Nothing, a
// node of the document, or a literal or computed scalar.
type jsonValue struct {
	node *yaml.Node
	lit  scalar
	kind valueKind
}

// asScalar returns v as a scalar, and false for Nothing, an object or an
// array.
func (v jsonValue) asScalar() (scalar, bool) {
	switch {
	case v.kind == valueScalar:
		return v.lit, true
	case v.kind == valueNode && v.node.Kind == yaml.ScalarNode:
		return readScalar(v.node), true
	default:
		return scalar{}, false
	}
}

// container reports whether v is an object or an array of the document.
func (v jsonValue) container() bool {
	return v.kind == valueNode && v.node.Kind != yaml.ScalarNode
}

// equal reports whether a and b are equal by RFC 9535 section 2.3.5.2.2: two
// Nothings are, Nothing and a value are not, and values compare by kind and
// content, an object's members whatever their order.
func (r *reader) equal(ctx context.Context, a, b jsonValue) (bool, error) {
	if err := r.spend(ctx, 1); err != nil {
		return false, err
	}
	switch {
	case a.kind == valueNothing || b.kind == valueNothing:
		return a.kind == b.kind, nil
	case a.container() && b.container():
		return r.deepEqual(ctx, a.node, b.node)
	case a.container() || b.container():
		return false, nil
	}
	sa, _ := a.asScalar()
	sb, _ := b.asScalar()
	return sameScalar(sa, sb), nil
}

// less reports whether a orders before b: two numbers by value, two strings
// by their characters' code points. Nothing else is ordered.
func (r *reader) less(ctx context.Context, a, b jsonValue) (bool, error) {
	if err := r.spend(ctx, 1); err != nil {
		return false, err
	}
	sa, ok := a.asScalar()
	sb, okB := b.asScalar()
	if !ok || !okB || sa.kind != sb.kind {
		return false, nil
	}
	switch sa.kind {
	case kindNumber:
		return sa.num.compare(sb.num) < 0, nil
	case kindString:
		// UTF-8 orders as the code points it encodes.
		return sa.text < sb.text, nil
	default:
		return false, nil
	}
}

// nodePair is two nodes deepEqual has yet to compare.
type nodePair struct{ a, b *yaml.Node }

// deepEqual compares two nodes of the document by value, with a stack of its
// own rather than recursion, each pair compared a step.
func (r *reader) deepEqual(ctx context.Context, a, b *yaml.Node) (bool, error) {
	stack := []nodePair{{a, b}}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := r.spend(ctx, 1); err != nil {
			return false, err
		}
		same, more, err := r.shallowEqual(ctx, p.a, p.b)
		if err != nil || !same {
			return false, err
		}
		stack = append(stack, more...)
	}
	return true, nil
}

// shallowEqual compares a and b at their own level, returning the pairs of
// children left to compare.
func (r *reader) shallowEqual(ctx context.Context, a, b *yaml.Node) (bool, []nodePair, error) {
	switch {
	case a == nil || b == nil:
		return a == b, nil, nil
	case a.Kind != b.Kind:
		return false, nil, nil
	case a.Kind == yaml.MappingNode:
		return r.sameMembers(ctx, a, b)
	case a.Kind == yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false, nil, nil
		}
		pairs := make([]nodePair, len(a.Content))
		for i := range a.Content {
			pairs[i] = nodePair{nodeview.Deref(a.Content[i]), nodeview.Deref(b.Content[i])}
		}
		return true, pairs, nil
	default:
		return sameScalar(readScalar(a), readScalar(b)), nil, nil
	}
}

// sameMembers compares the keys of two mappings, returning their values
// paired by key.
func (r *reader) sameMembers(ctx context.Context, a, b *yaml.Node) (bool, []nodePair, error) {
	am, err := r.members(ctx, a)
	if err != nil {
		return false, nil, err
	}
	bm, err := r.members(ctx, b)
	if err != nil {
		return false, nil, err
	}
	if len(am) != len(bm) {
		return false, nil, nil
	}
	byKey := make(map[string]*yaml.Node, len(bm))
	for _, p := range bm {
		byKey[p.Key] = p.Val
	}
	pairs := make([]nodePair, 0, len(am))
	for _, p := range am {
		v, found := byKey[p.Key]
		if !found {
			return false, nil, nil
		}
		pairs = append(pairs, nodePair{p.Val, v})
	}
	return true, pairs, nil
}
