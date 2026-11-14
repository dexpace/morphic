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
	// typeLevel says the value is an example of the type itself, for which
	// null stands for an example that is absent and is never judged.
	typeLevel bool
}

// checkValues reports each default and each example whose value its type does
// not admit (pass/default-outside-type, pass/example-outside-type,
// docs/ir/values.md): two codes, since a default reaches the code an emitter
// writes and an example the docs. The judgement is shallow, of kind, enum
// members, a literal, null, whole numbers and declared bounds. What it leaves
// alone, such as a content's examples (#1949), is listed in docs/ir/values.md.
func checkValues(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range liveTypeIDs(doc) {
		c := doc.Types[id].Common()
		site := valueSite{doc: doc, node: string(id), from: c.Provenance, ref: ir.TypeRef{Target: id}, typeLevel: true}
		diags = site.appendExamples(diags, c.Examples)
	}
	var keys []ir.PropID
	eachPosition(doc, &keys, func(p *position) {
		site := valueSite{doc: doc, node: p.node, from: p.from, ref: p.typ, own: p.own, encoded: p.encoded}
		if why := site.fits(p.def); why != "" {
			diags = append(diags, site.finding("pass/default-outside-type", "default "+why))
		}
		diags = site.appendExamples(diags, p.examples)
	})
	var params []ir.ParamID
	ir.ForEachOperation(doc, func(op ir.Operation) {
		diags = appendScenarioValues(diags, doc, &op, &params)
	})
	return diags
}

// finding is the warning a value of the site draws.
func (s *valueSite) finding(code, message string) ir.Diagnostic {
	return diagAt(ir.SeverityWarning, code, message, s.node, s.from)
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

// appendScenarioValues reports the values of op's scenarios the type of what
// they stand for does not admit: each input parameter, against the parameter
// of its ID, and the content of the error a scenario ends in, against the
// error's type. An input naming a parameter op does not declare is skipped:
// pass/param-binding-mismatch has it. params holds the sorted IDs of one input.
func appendScenarioValues(diags []ir.Diagnostic, doc *ir.Document, op *ir.Operation, params *[]ir.ParamID) []ir.Diagnostic {
	for i := range op.Examples {
		ex := &op.Examples[i]
		if ex.Input != nil {
			*params = (*params)[:0]
			for id := range ex.Input.Params {
				*params = append(*params, id)
			}
			slices.Sort(*params)
			for _, id := range *params {
				pm := parameterOf(op, id)
				if pm == nil {
					continue
				}
				site := valueSite{doc: doc, node: string(op.ID), from: op.Provenance, ref: pm.Type, own: pm.Constraints}
				v := ex.Input.Params[id]
				if why := site.fits(&v); why != "" {
					diags = append(diags, site.finding("pass/example-outside-type",
						"scenario "+exampleLabel(ex, i)+" parameter "+strconv.Quote(string(id))+" "+why))
				}
			}
		}
		if ex.Error != nil {
			site := valueSite{doc: doc, node: string(op.ID), from: op.Provenance, ref: ex.Error.Type}
			if why := site.fits(&ex.Error.Content); why != "" {
				diags = append(diags, site.finding("pass/example-outside-type",
					"scenario "+exampleLabel(ex, i)+" error "+why))
			}
		}
	}
	return diags
}

// parameterOf returns the parameter of op with the ID id, or nil.
func parameterOf(op *ir.Operation, id ir.ParamID) *ir.Parameter {
	for i := range op.Params {
		if op.Params[i].ID == id {
			return &op.Params[i]
		}
	}
	return nil
}

// fits returns why v is no value of the site's type, or "" when it is one or
// the check leaves it alone: a value that is a reference or a constructor, a
// type nothing declares, one holding anything, a union or an external type, and
// a value written in an encoding.
func (s *valueSite) fits(v *ir.Value) string {
	if v == nil || v.Kind == ir.ValueRefKind || v.Kind == ir.ValueCtor {
		return ""
	}
	var b bounds
	b.and(s.own)
	sh, td, enc := settle(s.doc, s.ref.Target, &b)
	if td == nil || sh == shapeAnything {
		return ""
	}
	target := s.ref.Target
	nullOK := s.ref.Nullable || s.typeLevel
	switch t := td.(type) {
	case *ir.Union, *ir.External:
		return ""
	case *ir.Enum:
		if slices.ContainsFunc(t.Members, func(m ir.EnumMember) bool { return same(&m.Value, v) }) ||
			(v.Kind == ir.ValueNull && nullOK) {
			return ""
		}
		return "is not a member of enum " + string(target)
	case *ir.Literal:
		if same(&t.Value, v) || (v.Kind == ir.ValueNull && nullOK) {
			return ""
		}
		return "is not the value of literal " + string(target)
	default:
		// a value of any other type is judged by its kind below
	}
	if v.Kind == ir.ValueNull {
		if nullOK {
			return ""
		}
		return "is null, and " + string(target) + " is not nullable here"
	}
	if s.encoded || enc || sh == shapeUnjudged {
		return ""
	}
	return fitsShape(v, sh, target, &b)
}

// fitsShape returns why v, which is not null, is no value of shape sh, the
// shape of target, or lies outside the bounds b holds.
func fitsShape(v *ir.Value, sh shape, target ir.TypeID, b *bounds) string {
	if !admits(sh, v.Kind) {
		return "is " + kindWords(v.Kind) + ", and " + string(target) + " holds " + shapeWords(sh)
	}
	if sh == shapeInteger {
		if d, err := v.Num.Decimal(); err == nil && !d.IsInt() {
			return "is no integer, and " + string(target) + " holds one"
		}
	}
	if outside(v, sh, b) {
		return "lies outside the bounds declared for it"
	}
	return ""
}

// admits reports whether a value of kind k is one a type of shape sh holds.
func admits(sh shape, k ir.ValueKind) bool {
	switch sh {
	case shapeBoolean:
		return k == ir.ValueBool
	case shapeText:
		return k == ir.ValueString || k == ir.ValueSymbol
	case shapeBytes:
		return k == ir.ValueBytes || k == ir.ValueString
	case shapeNumber:
		return k == ir.ValueNumber || k == ir.ValueInf || k == ir.ValueNegInf || k == ir.ValueNaN
	case shapeInteger:
		return k == ir.ValueNumber
	case shapeList:
		return k == ir.ValueList
	default:
		return k == ir.ValueObject // shapeObject: no other shape is asked
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

// kindWords says what a value of kind k is.
func kindWords(k ir.ValueKind) string {
	switch k {
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
	case ir.ValueInf, ir.ValueNegInf:
		return "an infinity"
	case ir.ValueNaN:
		return "a NaN"
	default:
		return "a " + string(k)
	}
}

// outside reports whether v, a value its shape admits, lies outside the bounds
// b holds: a number beyond an end, a string, bytes, list or object of a count
// beyond a limit. A string for bytes is not measured.
func outside(v *ir.Value, sh shape, b *bounds) bool {
	switch {
	case sh == shapeText:
		return beyond(int64(utf8.RuneCountInString(v.Str)), b.minLen, b.maxLen)
	case sh == shapeBytes && v.Kind == ir.ValueBytes:
		return beyond(int64(len(v.Bytes)), b.minLen, b.maxLen)
	case sh == shapeList:
		return beyond(int64(len(v.List)), b.minItems, b.maxItems)
	case sh == shapeObject:
		return beyond(int64(len(v.Object)), b.minProps, b.maxProps)
	case (sh == shapeNumber || sh == shapeInteger) && v.Kind == ir.ValueNumber:
		d, err := v.Num.Decimal()
		return err == nil && ((b.loSet && below(d.Cmp(b.lo), b.loOpen)) || (b.hiSet && below(-d.Cmp(b.hi), b.hiOpen)))
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
	text := func(k ir.ValueKind) bool { return k == ir.ValueString || k == ir.ValueSymbol }
	switch {
	case text(a.Kind) && text(b.Kind):
		return a.Str == b.Str
	case a.Kind != b.Kind:
		return false
	case a.Kind == ir.ValueBool:
		return a.Bool == b.Bool
	case a.Kind == ir.ValueBytes:
		return string(a.Bytes) == string(b.Bytes)
	case a.Kind == ir.ValueNumber:
		x, errX := a.Num.Decimal()
		y, errY := b.Num.Decimal()
		return errX != nil || errY != nil || x.Cmp(y) == 0
	default:
		return true
	}
}
