// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"
)

// maxPatterns bounds the patterns read from the document one evaluation keeps
// compiled; past it a pattern is compiled at each call, and charged each time.
const maxPatterns = 64

// paramType is the type of a function's parameter or result (RFC 9535
// section 2.4.1).
type paramType uint8

const (
	typeValue paramType = iota
	typeLogical
	typeNodes
)

// function is one function RFC 9535 section 2.4 defines: its parameters'
// types and its result's.
type function struct {
	name   string
	params []paramType
	result paramType
}

// lookup returns the function name names, and whether RFC 9535 defines one.
func lookup(name string) (function, bool) {
	switch name {
	case "length":
		return function{name: name, params: []paramType{typeValue}, result: typeValue}, true
	case "count", "value":
		return function{name: name, params: []paramType{typeNodes}, result: typeValue}, true
	case "match", "search":
		return function{name: name, params: []paramType{typeValue, typeValue}, result: typeLogical}, true
	default:
		return function{}, false
	}
}

// call is one function call. A match or search whose pattern is a literal
// has it compiled once, at parse: its pattern holds no program where the
// literal is no pattern the compiler reads, which matches nothing.
type call struct {
	pattern pattern
	args    []operand
	fn      function
	literal bool
}

// stepBytes is how many instructions of a pattern's program a byte of input
// matched against it costs a step: a byte costs the engine up to a step of
// each instruction.
const stepBytes = 8

// pattern is a compiled pattern and the steps each byte of input matched
// against it costs: one per stepBytes instructions of its program.
type pattern struct {
	re     *regexp.Regexp
	weight int
}

// call reads a call of the function name, written at start, after its name.
// Each argument must be of its parameter's type (RFC 9535 section 2.4.3).
func (p *parser) call(name string, start int) (operand, error) {
	fn, found := lookup(name)
	if !found {
		return operand{}, &SyntaxError{Offset: start, Reason: fmt.Sprintf("%s is no function RFC 9535 defines", name)}
	}
	if err := p.enter(nestCall); err != nil {
		return operand{}, err
	}
	defer p.leave(nestCall)
	p.pos++ // the parenthesis
	p.blank()
	callValue := &call{fn: fn}
	for !p.eat(')') {
		if len(callValue.args) > 0 && !p.eat(',') {
			return operand{}, p.fail("an argument is followed by a comma or )")
		}
		p.blank()
		arg, err := p.argument(fn, len(callValue.args))
		if err != nil {
			return operand{}, err
		}
		callValue.args = append(callValue.args, arg)
		p.blank()
	}
	if len(callValue.args) != len(fn.params) {
		return operand{}, &SyntaxError{Offset: start, Reason: fmt.Sprintf("%s() takes %d arguments", name, len(fn.params))}
	}
	if note, found := callValue.compileLiteral(); found {
		p.notes = append(p.notes, note)
	}
	return operand{kind: opCall, call: callValue}, nil
}

// argument reads the argument at index i of a call of fn: a literal, a query,
// a call, or a logical expression, which a parameter of LogicalType alone
// takes and none RFC 9535 defines has.
func (p *parser) argument(fn function, i int) (operand, error) {
	start := p.pos
	if i >= len(fn.params) {
		return operand{}, p.fail(fmt.Sprintf("%s() takes %d arguments", fn.name, len(fn.params)))
	}
	var arg operand
	if c := p.peek(); c == '!' || c == '(' {
		expr, err := p.logicalOr()
		if err != nil {
			return operand{}, err
		}
		arg = operand{kind: opLogical, logic: expr}
	} else {
		op, err := p.operand()
		if err != nil {
			return operand{}, err
		}
		if arg, err = p.argumentFrom(op, start); err != nil {
			return operand{}, err
		}
	}
	if !fits(arg, fn.params[i]) {
		return operand{}, &SyntaxError{Offset: start, Reason: fmt.Sprintf("argument %d of %s() is not of the type it takes", i+1, fn.name)}
	}
	return arg, nil
}

// argumentFrom returns op as an argument, or the logical expression it starts
// where an operator follows it.
func (p *parser) argumentFrom(op operand, start int) (operand, error) {
	save := p.pos
	p.blank()
	if c := p.peek(); c == 0 || !strings.ContainsRune("=!<>&|", rune(c)) {
		p.pos = save
		return op, nil
	}
	p.pos = save
	first, err := p.compareOrTest(op, start)
	if err != nil {
		return operand{}, err
	}
	expr, err := p.continueLogical(first)
	return operand{kind: opLogical, logic: expr}, err
}

// fits reports whether arg is of type want: a ValueType parameter takes a
// literal, a singular query or a call returning a value, and a NodesType one
// a query or a call returning nodes.
func fits(arg operand, want paramType) bool {
	if want == typeValue {
		return arg.kind == opLiteral || arg.kind == opQuery && arg.query.singular() ||
			arg.kind == opCall && arg.call.fn.result == typeValue
	}
	return arg.kind == opQuery || arg.kind == opCall && arg.call.fn.result == typeNodes
}

// compileLiteral compiles the pattern of a match or search written as a
// literal, and returns what a writer should be told of it, where there is
// something: that it matches no string, with why, as no I-Regexp the compiler
// reads or one whose anchors leave no string to match, or else that it writes
// an anchor where RFC 9485's grammar reads a character. A literal that is no
// string is no pattern.
func (c *call) compileLiteral() (PatternNote, bool) {
	if c.fn.result != typeLogical || c.args[1].kind != opLiteral {
		return PatternNote{}, false
	}
	c.literal = true
	lit := c.args[1].lit
	if lit.kind != kindString {
		return PatternNote{}, false
	}
	tr, ok := patternSource(lit.text, c.fn.name == "match")
	if ok {
		c.pattern = compileSource(tr)
	}
	switch {
	case c.pattern.re == nil:
		return PatternNote{Pattern: lit.text, Dead: true, Reason: fmt.Sprintf(
			"it reads as no I-Regexp (RFC 9485) within the %d bytes and %d instructions the compiler reads one in",
			maxPatternBytes, maxProgram)}, true
	case tr.empty != "":
		return PatternNote{Pattern: lit.text, Reason: tr.empty, Dead: true}, true
	case tr.anchored != "":
		return PatternNote{Pattern: lit.text, Reason: tr.anchored}, true
	default:
		return PatternNote{}, false
	}
}

// patternSource returns what an I-Regexp pattern reads as in Go's syntax,
// anchored at both ends for match, and false for a pattern that is no
// I-Regexp or is past maxPatternBytes or maxProgram.
func patternSource(text string, anchored bool) (translation, bool) {
	tr, ok := translate(text)
	if ok && anchored {
		tr.source = `\A(?:` + tr.source + `)\z`
	}
	return tr, ok
}

// compileSource compiles tr. A source Go's engine cannot compile, such as a
// repetition past its count, is no pattern.
func compileSource(tr translation) pattern {
	re, err := regexp.Compile(tr.source)
	if err != nil {
		return pattern{}
	}
	return pattern{re: re, weight: 1 + tr.size/stepBytes}
}

// callValue evaluates a call whose result is a value.
func (e *evaluator) callValue(ctx context.Context, callValue *call, at Located) (jsonValue, error) {
	if callValue.fn.name == "length" {
		jsonValueValue, err := e.valueOf(ctx, callValue.args[0], at)
		if err != nil {
			return jsonValue{}, err
		}
		return e.length(ctx, jsonValueValue)
	}
	nodes, err := e.nodesOf(ctx, callValue.args[0].query, at)
	e.release(len(nodes))
	switch {
	case err != nil:
		return jsonValue{}, err
	case callValue.fn.name == "count":
		return jsonValue{kind: valueScalar, lit: countOf(len(nodes))}, nil
	case len(nodes) == 1:
		return jsonValue{kind: valueNode, node: nodes[0].Node}, nil
	default:
		return jsonValue{}, nil
	}
}

// length is the length of jsonValueValue: a string's characters, an array's elements or
// an object's members, and Nothing for anything else.
func (r *reader) length(ctx context.Context, jsonValueValue jsonValue) (jsonValue, error) {
	switch {
	case jsonValueValue.kind == valueNode && jsonValueValue.node.Kind == yaml.MappingNode:
		pairs, err := r.members(ctx, jsonValueValue.node)
		return jsonValue{kind: valueScalar, lit: countOf(len(pairs))}, err
	case jsonValueValue.kind == valueNode && jsonValueValue.node.Kind == yaml.SequenceNode:
		return jsonValue{kind: valueScalar, lit: countOf(len(jsonValueValue.node.Content))}, nil
	}
	scalar, ok, err := r.asScalar(ctx, jsonValueValue)
	if err != nil || !ok || scalar.kind != kindString {
		return jsonValue{}, err
	}
	if err := r.spend(ctx, scalar.size/compareBytes); err != nil {
		return jsonValue{}, err
	}
	return jsonValue{kind: valueScalar, lit: countOf(utf8.RuneCountInString(scalar.text))}, nil
}

// callTruth evaluates a match or search: whether its first argument is a
// string the pattern its second names matches, whole for match, anywhere for
// search. The call spends the input's length and one more, weighed by the
// pattern's size, in steps, since the engine runs its program at the input's
// end too, an empty input's included; a pattern read from the document is
// charged its compiling too.
func (e *evaluator) callTruth(ctx context.Context, callValue *call, at Located) (bool, error) {
	input, err := e.stringOf(ctx, callValue.args[0], at)
	if err != nil || input == nil {
		return false, err
	}
	pattern := callValue.pattern
	if !callValue.literal {
		text, err := e.stringOf(ctx, callValue.args[1], at)
		if err != nil || text == nil {
			return false, err
		}
		if pattern, err = e.compiled(ctx, *text, callValue.fn.name == "match"); err != nil {
			return false, err
		}
	}
	if pattern.re == nil {
		return false, nil
	}
	if err := e.spend(ctx, (len(*input)+1)*pattern.weight); err != nil {
		return false, err
	}
	return pattern.re.MatchString(*input), nil
}

// stringOf evaluates operandValue to the string it is, or nil where it is none.
func (e *evaluator) stringOf(ctx context.Context, operandValue operand, at Located) (*string, error) {
	jsonValue, err := e.valueOf(ctx, operandValue, at)
	if err != nil {
		return nil, err
	}
	scalar, ok, err := e.asScalar(ctx, jsonValue)
	if err != nil || !ok || scalar.kind != kindString {
		return nil, err
	}
	return &scalar.text, nil
}

// patternKey names a compiled pattern: its text, and whether match anchored it.
type patternKey struct {
	text     string
	anchored bool
}

// compiled returns text compiled, from the evaluation's cache where it is
// held. A compile is charged before it runs: the pattern's bytes, which its
// translation reads, then its program's instructions, which Go's compiler
// builds. A pattern that is no I-Regexp, or is past a bound, holds no program.
func (e *evaluator) compiled(ctx context.Context, text string, anchored bool) (pattern, error) {
	key := patternKey{text: text, anchored: anchored}
	if pattern, held := e.patterns[key]; held {
		return pattern, nil
	}
	if err := e.spend(ctx, len(text)); err != nil {
		return pattern{}, err
	}
	var patternValue pattern
	if tr, ok := patternSource(text, anchored); ok {
		if err := e.spend(ctx, tr.size); err != nil {
			return pattern{}, err
		}
		patternValue = compileSource(tr)
	}
	if len(e.patterns) < maxPatterns {
		if e.patterns == nil {
			e.patterns = map[patternKey]pattern{}
		}
		e.patterns[key] = patternValue
	}
	return patternValue, nil
}
