// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"context"
	"fmt"
	"strings"
)

// logical is a filter's logical expression, evaluated with at as the current
// node. Its depth is the parse's, which maxDepth bounds, so evaluating it by
// recursion is bounded too.
type logical interface {
	holds(ctx context.Context, evaluatorValue *evaluator, at Located) (bool, error)
}

// anyOf holds where one of its terms does, read in order.
type anyOf []logical

func (l anyOf) holds(ctx context.Context, evaluatorValue *evaluator, at Located) (bool, error) {
	for _, term := range l {
		if held, err := term.holds(ctx, evaluatorValue, at); err != nil || held {
			return held, err
		}
	}
	return false, nil
}

// allOf holds where every one of its terms does, read in order.
type allOf []logical

func (l allOf) holds(ctx context.Context, evaluatorValue *evaluator, at Located) (bool, error) {
	for _, term := range l {
		if held, err := term.holds(ctx, evaluatorValue, at); err != nil || !held {
			return false, err
		}
	}
	return true, nil
}

// negation holds where inner does not.
type negation struct{ inner logical }

func (n negation) holds(ctx context.Context, evaluatorValue *evaluator, at Located) (bool, error) {
	held, err := n.inner.holds(ctx, evaluatorValue, at)
	return !held && err == nil, err
}

// exists is a test of a query: it holds where the query selects a node.
type exists struct{ query subquery }

func (x exists) holds(ctx context.Context, evaluatorValue *evaluator, at Located) (bool, error) {
	nodes, err := evaluatorValue.nodesOf(ctx, x.query, at)
	evaluatorValue.release(len(nodes))
	return len(nodes) > 0, err
}

// truth is a test of a function whose result is LogicalType.
type truth struct{ call *call }

func (t truth) holds(ctx context.Context, evaluatorValue *evaluator, at Located) (bool, error) {
	return evaluatorValue.callTruth(ctx, t.call, at)
}

// compareOperator is a comparison's operator.
type compareOperator uint8

const (
	operatorEqual compareOperator = iota
	operatorNotEqual
	operatorLess
	operatorLessEqual
	operatorGreater
	operatorGreaterEqual
)

// comparison compares two comparables.
type comparison struct {
	left, right operand
	operator    compareOperator
}

func (c comparison) holds(ctx context.Context, evaluatorValue *evaluator, at Located) (bool, error) {
	a, err := evaluatorValue.valueOf(ctx, c.left, at)
	if err != nil {
		return false, err
	}
	b, err := evaluatorValue.valueOf(ctx, c.right, at)
	if err != nil {
		return false, err
	}
	switch c.operator {
	case operatorEqual:
		return evaluatorValue.equal(ctx, a, b)
	case operatorNotEqual:
		same, err := evaluatorValue.equal(ctx, a, b)
		return !same && err == nil, err
	case operatorLess:
		return evaluatorValue.less(ctx, a, b)
	case operatorGreater:
		return evaluatorValue.less(ctx, b, a)
	case operatorLessEqual:
		return evaluatorValue.lessOrEqual(ctx, a, b)
	default:
		return evaluatorValue.lessOrEqual(ctx, b, a)
	}
}

// lessOrEqual reports whether a orders before b or equals it.
func (r *reader) lessOrEqual(ctx context.Context, a, b jsonValue) (bool, error) {
	if lt, err := r.less(ctx, a, b); err != nil || lt {
		return lt, err
	}
	return r.equal(ctx, a, b)
}

// operandKind says what an operand is.
type operandKind uint8

const (
	operatorLiteral operandKind = iota
	operatorQuery
	operatorCall
	operatorLogical
)

// operand is what a comparison compares or a function is handed: a literal, a
// query, a function call, or, as an argument, a logical expression.
type operand struct {
	logic logical
	call  *call
	query subquery
	lit   scalar
	kind  operandKind
}

// subquery is a query inside a filter, from the current node or the root.
type subquery struct {
	segments []segment
	relative bool
}

// singular reports whether q selects at most one node by its form alone.
func (q subquery) singular() bool {
	for _, segment := range q.segments {
		if !segment.singular {
			return false
		}
	}
	return true
}

// nodesOf evaluates subqueryValue with at as the current node, returning a
// nodelist the caller releases once it has read it.
func (e *evaluator) nodesOf(ctx context.Context, subqueryValue subquery, at Located) ([]Located, error) {
	from := at
	if !subqueryValue.relative {
		from = Located{Node: e.root}
	}
	return e.run(ctx, subqueryValue.segments, from)
}

// valueOf evaluates operandValue, a comparable or a ValueType argument, to its
// value.
func (e *evaluator) valueOf(ctx context.Context, operandValue operand, at Located) (jsonValue, error) {
	switch operandValue.kind {
	case operatorLiteral:
		return jsonValue{kind: valueScalar, lit: operandValue.lit}, nil
	case operatorQuery:
		nodes, err := e.nodesOf(ctx, operandValue.query, at)
		e.release(len(nodes))
		if err != nil || len(nodes) == 0 {
			return jsonValue{}, err
		}
		return jsonValue{kind: valueNode, node: nodes[0].Node}, nil
	default:
		return e.callValue(ctx, operandValue.call, at)
	}
}

// logicalOr reads a logical expression: terms joined by && within terms
// joined by ||.
func (p *parser) logicalOr() (logical, error) {
	first, err := p.basic()
	if err != nil {
		return nil, err
	}
	return p.continueLogical(first)
}

// continueLogical reads the rest of a logical expression whose first basic
// expression is first.
func (p *parser) continueLogical(first logical) (logical, error) {
	var or []logical
	and := []logical{first}
	for {
		save := p.pos
		p.blank()
		switch {
		case p.eatText("&&"):
		case p.eatText("||"):
			or, and = append(or, joined(and, false)), nil
		default:
			p.pos = save
			return joined(append(or, joined(and, false)), true), nil
		}
		p.blank()
		next, err := p.basic()
		if err != nil {
			return nil, err
		}
		and = append(and, next)
	}
}

// joined returns terms joined by || (or) or && (and), or the one term alone.
func joined(terms []logical, or bool) logical {
	switch {
	case len(terms) == 1:
		return terms[0]
	case or:
		return anyOf(terms)
	default:
		return allOf(terms)
	}
}

// basic reads a parenthesized expression, a comparison or a test, each but a
// comparison perhaps negated.
func (p *parser) basic() (logical, error) {
	negated := p.eat('!')
	if negated {
		p.blank()
	}
	if p.peek() == '(' {
		inner, err := p.paren()
		if err != nil || !negated {
			return inner, err
		}
		return negation{inner: inner}, nil
	}
	start := p.pos
	operator, err := p.operand()
	if err != nil {
		return nil, err
	}
	if !negated {
		return p.compareOrTest(operator, start)
	}
	test, err := testOf(operator, start)
	if err != nil {
		return nil, err
	}
	return negation{inner: test}, nil
}

// paren reads a parenthesized logical expression.
func (p *parser) paren() (logical, error) {
	if err := p.enter(nestParen); err != nil {
		return nil, err
	}
	defer p.leave(nestParen)
	p.pos++
	p.blank()
	inner, err := p.logicalOr()
	if err != nil {
		return nil, err
	}
	p.blank()
	if !p.eat(')') {
		return nil, p.fail("a parenthesis is closed by )")
	}
	return inner, nil
}

// compareOrTest reads a comparison whose left side is operator, written at
// start, or takes operator as a test where no comparison operator follows.
func (p *parser) compareOrTest(operator operand, start int) (logical, error) {
	save := p.pos
	p.blank()
	cmpOperator, found := p.comparisonOperator()
	if !found {
		p.pos = save
		return testOf(operator, start)
	}
	if err := comparableOperand(operator, start); err != nil {
		return nil, err
	}
	p.blank()
	rightAt := p.pos
	right, err := p.operand()
	if err != nil {
		return nil, err
	}
	if err := comparableOperand(right, rightAt); err != nil {
		return nil, err
	}
	return comparison{left: operator, right: right, operator: cmpOperator}, nil
}

// comparisonOperator reads a comparison operator, and reports whether one is
// next.
func (s *scanner) comparisonOperator() (compareOperator, bool) {
	for _, o := range []struct {
		text     string
		operator compareOperator
	}{{"==", operatorEqual}, {"!=", operatorNotEqual}, {"<=", operatorLessEqual}, {">=", operatorGreaterEqual}, {"<", operatorLess}, {">", operatorGreater}} {
		if s.eatText(o.text) {
			return o.operator, true
		}
	}
	return 0, false
}

// comparableOperand refuses an operand a comparison may not compare: a query that
// may select more than one node, or a function whose result is no value.
func comparableOperand(operator operand, at int) error {
	switch {
	case operator.kind == operatorQuery && !operator.query.singular():
		return &SyntaxError{Offset: at, Reason: "a comparison compares a singular query, and this one may select several nodes"}
	case operator.kind == operatorCall && operator.call.fn.result != typeValue:
		return &SyntaxError{Offset: at, Reason: operator.call.fn.name + "() returns no value a comparison compares"}
	default:
		return nil
	}
}

// testOf returns operator as a test: a query, or a function returning
// LogicalType.
func testOf(operator operand, at int) (logical, error) {
	switch {
	case operator.kind == operatorQuery:
		return exists{query: operator.query}, nil
	case operator.kind == operatorCall && operator.call.fn.result == typeLogical:
		return truth{call: operator.call}, nil
	case operator.kind == operatorCall:
		return nil, &SyntaxError{Offset: at, Reason: operator.call.fn.name + "() returns a value, which a test does not take: compare it"}
	default:
		return nil, &SyntaxError{Offset: at, Reason: "a literal is no test: compare it"}
	}
}

// operand reads a literal, a query or a function call.
func (p *parser) operand() (operand, error) {
	switch c := p.peek(); {
	case c == '\'' || c == '"':
		text, err := p.stringLiteral()
		return operand{kind: operatorLiteral, lit: scalar{kind: kindString, text: text}}, err
	case c == '-' || c >= '0' && c <= '9':
		return p.numberLiteral()
	case c == '@' || c == '$':
		p.pos++
		segments, err := p.segments()
		return operand{kind: operatorQuery, query: subquery{segments: segments, relative: c == '@'}}, err
	case c >= 'a' && c <= 'z':
		return p.word()
	default:
		return operand{}, p.fail("a filter compares or tests a literal, a query or a function call")
	}
}

// word reads true, false, null or a function call.
func (p *parser) word() (operand, error) {
	start := p.pos
	for c := p.peek(); c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'; c = p.peek() {
		p.pos++
	}
	name := p.text[start:p.pos]
	if p.peek() == '(' {
		return p.call(name, start)
	}
	switch name {
	case "true", "false":
		return operand{kind: operatorLiteral, lit: scalar{kind: kindBool, b: name == "true"}}, nil
	case "null":
		return operand{kind: operatorLiteral, lit: scalar{kind: kindNull}}, nil
	default:
		return operand{}, &SyntaxError{Offset: start, Reason: fmt.Sprintf("%q is no literal, and no ( follows it to call it", name)}
	}
}

// numberLiteral reads a number: an int or -0, then perhaps a fraction and an
// exponent.
func (s *scanner) numberLiteral() (operand, error) {
	start := s.pos
	s.eat('-')
	if !s.digits() {
		return operand{}, s.fail("a number has a digit")
	}
	if lead := s.text[start:s.pos]; strings.TrimPrefix(lead, "-")[0] == '0' && len(strings.TrimPrefix(lead, "-")) > 1 {
		return operand{}, &SyntaxError{Offset: start, Reason: fmt.Sprintf("the number %s is spelled with a leading zero", lead)}
	}
	if s.eat('.') && !s.digits() {
		return operand{}, s.fail("a decimal point is followed by a digit")
	}
	if c := s.peek(); c == 'e' || c == 'E' {
		s.pos++
		if c := s.peek(); c == '+' || c == '-' {
			s.pos++
		}
		if !s.digits() {
			return operand{}, s.fail("an exponent has a digit")
		}
	}
	// The grammar read is a subset of the decimal one ParseDecimal reads.
	number, _ := newNumber(s.text[start:s.pos])
	return operand{kind: operatorLiteral, lit: scalar{kind: kindNumber, number: number}}, nil
}

// digits consumes a run of digits, and reports whether there was one.
func (s *scanner) digits() bool {
	start := s.pos
	for c := s.peek(); c >= '0' && c <= '9'; c = s.peek() {
		s.pos++
	}
	return s.pos > start
}
