// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

// valueSite is a place a value is read and what the value is held to: the type
// the place declares, the bounds it declares beside it, and whether its value
// is written in an encoding of its own.
type valueSite struct {
	doc *ir.Document
	// node and from place a finding at the entity that holds the value.
	node string
	from ir.Provenance
	ref  ir.TypeRef
	own  *ir.Constraints
	// encoded says the value is written in an encoding, so its kind says
	// nothing of what the type holds.
	encoded bool
	// writtenShapes are the structured shapes the position's style can write
	// as text, so a scalar example of one of them may be that value written
	// out.
	writtenShapes shapeSet
	// nullAbsent says a null value stands for an example that is absent and
	// is never judged: an example of the type itself, and one of a parameter
	// or a header that is not required, the value left out.
	nullAbsent bool
}

// checkValues reports each default and each example whose value its type does
// not admit (pass/default-outside-type, pass/example-outside-type,
// docs/ir/values.md): two codes, since a default reaches the code an emitter
// writes and an example the docs. The judgement is shallow, of kind, enum
// members, a literal, null, whole numbers and declared bounds. What it leaves
// alone, such as a content's examples (#1949), is listed in docs/ir/values.md.
func checkValues(doc *ir.Document, live []ir.TypeID) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range live {
		typeCommon := doc.Types[id].Common()
		site := valueSite{doc: doc, node: string(id), from: typeCommon.Provenance, ref: ir.TypeRef{Target: id}, nullAbsent: true}
		diags = site.appendExamples(diags, typeCommon.Examples)
	}
	var scratch positionScratch
	eachPosition(doc, live, &scratch, func(positionValue *position) {
		site := valueSite{doc: doc, node: positionValue.node, from: positionValue.from, ref: positionValue.typ, own: positionValue.own, encoded: positionValue.encoded}
		if why := site.fits(positionValue.def); why != "" {
			diags = append(diags, site.finding("pass/default-outside-type", "default "+why))
		}
		// The examples alone: a default is a value of the type.
		site.encoded = site.encoded || positionValue.mediaTyped
		site.writtenShapes, site.nullAbsent = positionValue.writtenShapes, positionValue.nullExampleLeftOut
		diags = site.appendExamples(diags, positionValue.examples)
	})
	var params []ir.ParameterID
	ir.ForEachOperation(doc, func(operation ir.Operation) {
		diags = appendScenarioValues(diags, doc, &operation, &params)
	})
	return diags
}

// finding is the warning a value of the site draws.
func (s *valueSite) finding(code, message string) ir.Diagnostic {
	return diagAt(code, message, s.node, s.from)
}

// appendExamples reports each of examples whose value the site's type does not
// admit.
func (s *valueSite) appendExamples(diags []ir.Diagnostic, examples []ir.Example) []ir.Diagnostic {
	for i := range examples {
		if why := s.fits(examples[i].Value); why != "" {
			diags = append(diags, s.finding("pass/example-outside-type", "example "+exampleLabel(&examples[i], i)+" "+why))
		}
	}
	return diags
}

// exampleLabel names example i by its name where it has one, and its index
// where not.
func exampleLabel(ex *ir.Example, i int) string {
	if ex.Name != "" {
		return strconv.Quote(ex.Name)
	}
	return strconv.Itoa(i)
}

// appendScenarioValues reports the values of operation's scenarios the type of
// what they stand for does not admit: each input parameter, against the
// parameter of its ID, and the content of the error a scenario ends in, against
// the error's type. params holds the sorted IDs of one input.
func appendScenarioValues(diags []ir.Diagnostic, doc *ir.Document, operation *ir.Operation, params *[]ir.ParameterID) []ir.Diagnostic {
	for i := range operation.Examples {
		ex := &operation.Examples[i]
		if ex.Input != nil {
			diags = appendInputValues(diags, doc, operation, ex, i, params)
		}
		if ex.Error != nil {
			site := valueSite{doc: doc, node: string(operation.ID), from: operation.Provenance, ref: ex.Error.Type}
			if why := site.fits(&ex.Error.Content); why != "" {
				diags = append(diags, site.finding("pass/example-outside-type",
					"scenario "+exampleLabel(ex, i)+" error "+why))
			}
		}
	}
	return diags
}

// appendInputValues reports each parameter of scenario ex, the ith of
// operation, whose value the parameter's type does not admit, in the order of
// their IDs. An input naming a parameter operation does not declare is skipped:
// pass/param-binding-mismatch has it.
func appendInputValues(diags []ir.Diagnostic, doc *ir.Document, operation *ir.Operation, ex *ir.Example, i int,
	params *[]ir.ParameterID,
) []ir.Diagnostic {
	*params = (*params)[:0]
	for id := range ex.Input.Params {
		*params = append(*params, id)
	}
	slices.Sort(*params)
	for _, id := range *params {
		pm := parameterOf(operation, id)
		if pm == nil {
			continue
		}
		site := valueSite{doc: doc, node: string(operation.ID), from: operation.Provenance, ref: pm.Type, own: pm.Constraints}
		value := ex.Input.Params[id]
		if why := site.fits(&value); why != "" {
			diags = append(diags, site.finding("pass/example-outside-type",
				"scenario "+exampleLabel(ex, i)+" parameter "+strconv.Quote(string(id))+" "+why))
		}
	}
	return diags
}

// parameterOf returns the parameter of operation with the ID id, or nil.
func parameterOf(operation *ir.Operation, id ir.ParameterID) *ir.Parameter {
	for i := range operation.Params {
		if operation.Params[i].ID == id {
			return &operation.Params[i]
		}
	}
	return nil
}

// fits returns why value is no value of the site's type, or "" when it is one
// or the check leaves it alone: a value that is a reference or a constructor, a
// type nothing declares, one holding anything, a union or an external type, the
// kind, whole numbers and bounds of a value written in an encoding, whose enum
// membership, literal value and null are judged all the same, and a scalar that
// may be a list or an object written out in the site's style, as writtenOut
// says.
func (s *valueSite) fits(value *ir.Value) string {
	if value == nil || value.Kind == ir.ValueRefKind || value.Kind == ir.ValueConstructor {
		return ""
	}
	var b bounds
	b.and(s.own)
	sh, td, enc := settle(s.doc, s.ref.Target, &b)
	if td == nil || sh == shapeAnything {
		return ""
	}
	target := s.ref.Target
	nullOK := s.ref.Nullable || s.nullAbsent
	switch t := td.(type) {
	case *ir.Union, *ir.External:
		return ""
	case *ir.Enum:
		if slices.ContainsFunc(t.Members, func(enumMember ir.EnumMember) bool { return same(&enumMember.Value, value) }) ||
			(value.Kind == ir.ValueNull && nullOK) {
			return ""
		}
		return "is not a member of enum " + string(target)
	case *ir.Literal:
		if same(&t.Value, value) || (value.Kind == ir.ValueNull && nullOK) {
			return ""
		}
		return "is not the value of literal " + string(target)
	default:
		// a value of any other type is judged by its kind below
	}
	if value.Kind == ir.ValueNull {
		if nullOK {
			return ""
		}
		return "is null, and " + string(target) + " is not nullable here"
	}
	if s.encoded || enc || sh == shapeNotChecked || s.writtenOut(value, sh, td, &b) {
		return ""
	}
	return fitsShape(value, sh, target, &b)
}

// writtenOut reports whether value may be td, of shape sh, written out in a
// style the site writes sh in, b holding td's bounds: a string as its text,
// and a number or a bool, holding no delimiter, only as the one item of a list
// whose item counts admit one and whose element type holds that item, as
// oneItem judges it. So neither is an object's or a tuple's text
// (docs/ir/values.md#values-held-to-their-type).
func (s *valueSite) writtenOut(value *ir.Value, sh shape, td ir.TypeDef, b *bounds) bool {
	if !s.writtenShapes.has(sh) {
		return false
	}
	if value.Kind == ir.ValueString {
		return true
	}
	if value.Kind != ir.ValueNumber && value.Kind != ir.ValueBool {
		return false
	}
	list, ok := td.(*ir.List)
	if !ok || beyond(1, b.minItems, b.maxItems) {
		return false
	}
	return oneItem(s.doc, value, list.ElementType.Target)
}

// oneItem reports whether value, a number or a bool, is a value of the element
// type target, as fitsShape judges one, bounds included: as its text where the
// element settles to text, and as itself otherwise, whatever encoding the
// element names, so bytes hold neither. An element holding anything holds it,
// and an enum, a literal, a union or another type the check reads no further
// holds none, since the check judges no item against such a type.
func oneItem(doc *ir.Document, value *ir.Value, target ir.TypeID) bool {
	var elementBounds bounds
	item, _, _ := settle(doc, target, &elementBounds)
	switch item {
	case shapeAnything:
		return true
	case shapeNotChecked:
		return false
	case shapeText:
		written := ir.Value{Kind: ir.ValueString, String: string(value.Number)}
		if value.Kind == ir.ValueBool {
			written.String = strconv.FormatBool(value.Bool)
		}
		return fitsShape(&written, item, target, &elementBounds) == ""
	default:
		return fitsShape(value, item, target, &elementBounds) == ""
	}
}

// fitsShape returns why value, which is not null, is no value of shape sh, the
// shape of target, or lies outside the bounds b holds.
func fitsShape(value *ir.Value, sh shape, target ir.TypeID, b *bounds) string {
	if !admits(sh, value.Kind) {
		return "is " + kindWords(value.Kind) + ", and " + string(target) + " holds " + shapeWords(sh)
	}
	if sh == shapeInteger {
		if decimal, err := value.Number.Decimal(); err == nil && !decimal.IsInt() {
			return "is no integer, and " + string(target) + " holds one"
		}
	}
	if outside(value, sh, b) {
		return "lies outside the bounds declared for it"
	}
	return ""
}

// admits reports whether a value of kind valueKind is one a type of shape sh
// holds.
func admits(sh shape, valueKind ir.ValueKind) bool {
	switch sh {
	case shapeBoolean:
		return valueKind == ir.ValueBool
	case shapeText:
		return valueKind == ir.ValueString || valueKind == ir.ValueSymbol
	case shapeBytes:
		return valueKind == ir.ValueBytes || valueKind == ir.ValueString
	case shapeNumber:
		return valueKind == ir.ValueNumber || valueKind == ir.ValueInfinity || valueKind == ir.ValueNegativeInfinity || valueKind == ir.ValueNaN
	case shapeInteger:
		return valueKind == ir.ValueNumber
	case shapeList:
		return valueKind == ir.ValueList
	default:
		return valueKind == ir.ValueObject // shapeObject: no other shape is asked
	}
}

// shapeWords says what a type of shape sh holds.
func shapeWords(sh shape) string {
	switch sh {
	case shapeBoolean:
		return "a bool"
	case shapeText:
		return "a string"
	case shapeBytes:
		return "bytes"
	case shapeNumber:
		return "a number"
	case shapeInteger:
		return "an integer"
	case shapeList:
		return "a list"
	default:
		return "an object"
	}
}

// kindWords says what a value of kind valueKind is. Null and the two kinds that
// stand for another value are judged before it is asked, and are named by their
// kind.
func kindWords(valueKind ir.ValueKind) string {
	switch valueKind {
	case ir.ValueBool:
		return "a bool"
	case ir.ValueString:
		return "a string"
	case ir.ValueSymbol:
		return "a symbol"
	case ir.ValueNumber:
		return "a number"
	case ir.ValueBytes:
		return "bytes"
	case ir.ValueList:
		return "a list"
	case ir.ValueObject:
		return "an object"
	case ir.ValueInfinity, ir.ValueNegativeInfinity:
		return "an infinity"
	case ir.ValueNaN:
		return "a NaN"
	case ir.ValueNull, ir.ValueRefKind, ir.ValueConstructor:
		return "a " + string(valueKind)
	default:
		return "a " + string(valueKind)
	}
}

// outside reports whether value, a value its shape admits, lies outside the
// bounds b holds: a number beyond an end, a string, bytes, list or object of a
// count beyond a limit. A string for bytes is not measured.
func outside(value *ir.Value, sh shape, b *bounds) bool {
	switch {
	case sh == shapeText:
		return beyond(int64(utf8.RuneCountInString(value.String)), b.minLen, b.maxLen)
	case sh == shapeBytes && value.Kind == ir.ValueBytes:
		return beyond(int64(len(value.Bytes)), b.minLen, b.maxLen)
	case sh == shapeList:
		return beyond(int64(len(value.List)), b.minItems, b.maxItems)
	case sh == shapeObject:
		return beyond(int64(len(value.Object)), b.minProperties, b.maxProperties)
	case (sh == shapeNumber || sh == shapeInteger) && value.Kind == ir.ValueNumber:
		decimal, err := value.Number.Decimal()
		return err == nil && ((b.loSet && below(decimal.Cmp(b.lo), b.loOpen)) || (b.hiSet && below(-decimal.Cmp(b.hi), b.hiOpen)))
	default:
		return false
	}
}

// below reports whether a number whose comparison with an end of the range is c,
// negative when it lies on the wrong side, is outside it: equal counts where
// the end is open.
func below(c int, open bool) bool { return c < 0 || (c == 0 && open) }

// beyond reports whether n is under the minimum or over the maximum, either of
// which may be unset.
func beyond(n int64, minimum, maximum *int64) bool {
	return (minimum != nil && n < *minimum) || (maximum != nil && n > *maximum)
}

// same reports whether two values are the same one, as an enum member is a
// value of the type: two numbers by value, a string and a symbol by their text,
// bytes by content, two of one other kind alike (a container's contents are not
// judged), and two kinds differently. A number that does not parse is
// irverify's, and is not judged.
func same(a, b *ir.Value) bool {
	text := func(valueKind ir.ValueKind) bool { return valueKind == ir.ValueString || valueKind == ir.ValueSymbol }
	switch {
	case text(a.Kind) && text(b.Kind):
		return a.String == b.String
	case a.Kind != b.Kind:
		return false
	case a.Kind == ir.ValueBool:
		return a.Bool == b.Bool
	case a.Kind == ir.ValueBytes:
		return string(a.Bytes) == string(b.Bytes)
	case a.Kind == ir.ValueNumber:
		decimal, errX := a.Number.Decimal()
		irDecimal, errY := b.Number.Decimal()
		return errX != nil || errY != nil || decimal.Cmp(irDecimal) == 0
	default:
		return true
	}
}
