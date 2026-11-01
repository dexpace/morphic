// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// value checks the value a field is written with: its shape, then what its
// shape allows, then the objects it holds. A field whose shape another package
// owns is left to it, bar a number outside the field's range, such as a
// multipleOf of 0, which that package reads as the number it is, quoted or not.
func (w *walker) value(f frame, fld Field, val *yaml.Node, depth int) {
	node := nodeview.Deref(val)
	ptr := f.at.ptr.AppendToken(fld.Name)
	shape, ok := w.shapeOf(fld, node)
	if !ok {
		if fld.ShapeOwner == "" || outOfRange(fld.Shape, node) {
			w.mismatch(f, fld, node, ptr)
		}
		return
	}
	if shape == String {
		w.text(f, fmt.Sprintf("field %q", fld.Name), fld, node, ptr)
	}
	w.scalar(f, fld, node, ptr)
	w.entries(f, fld, shape, node, ptr, depth)
}

// text warns of a number or a boolean written where a string belongs: the
// document breaks its version's typing, which is JSON's, while the text it
// writes says what it means, which a string field reads. label names the
// value: a field, or an item or entry of one.
func (w *walker) text(f frame, label string, fld Field, node *yaml.Node, ptr jsontext.Pointer) {
	what := map[string]string{"!!int": "the number", "!!float": "the number", "!!bool": "the boolean"}[node.Tag]
	if what == "" {
		return
	}
	w.report(ir.SeverityWarning, classType, ptr, "%s %s must be a string in OpenAPI %s (%s); it is %s %s",
		f.obj.Name, label, w.minor, citeFor(fld.Cites, w.minor).Section, what, node.Value)
}

// mismatch reports a value that has none of the shapes its field admits, an
// error unless it is a null read otherwise (frame.nullRead).
func (w *walker) mismatch(f frame, fld Field, node *yaml.Node, ptr jsontext.Pointer) {
	sev, null := ir.SeverityError, ""
	if isNull(node) {
		switch f.nullRead(fld, w.minor) {
		case leftOut:
			sev, null = ir.SeverityWarning, "; it is null, which is read as the field left out"
		case noScopes:
			sev, null = ir.SeverityWarning, "; it is null, which is read as listing no scopes"
		case missingElsewhere:
			return
		}
	}
	w.report(sev, classType, ptr, "%s field %q must be %s in OpenAPI %s (%s)%s",
		f.obj.Name, fld.Name, describeShape(fld), w.minor, citeFor(fld.Cites, w.minor).Section, null)
}

// nullReading is how the compiler reads a null written for a field.
type nullReading uint8

const (
	// wrongValue: as a value of the wrong shape, an error.
	wrongValue nullReading = iota
	// leftOut: as the field left out, a warning.
	leftOut
	// missingElsewhere: as a missing field, which another package reports.
	missingElsewhere
	// noScopes: as a security requirement's name listing no scopes, a warning.
	// The name is no field to leave out: it stands whatever its value
	// (docs/compilers/openapi.md#security-requirements).
	noScopes
)

// nullRead says how a null written for the field is read at minor m. The schema
// lowering reads one for a keyword whose value is a schema as left out, and one
// for another object's schema as the empty schema, a value. It reads one for a
// Schema Object's other keywords several ways, so that is the wrong value
// (GitHub #1069). A null for an optional field is otherwise left out, and one
// for a security requirement's name lists no scopes; for a required field it is
// the value that is missing, reported by whichever package owns the rule.
func (f frame) nullRead(fld Field, m Minor) nullReading {
	holdsSchema := fld.Shape == Nested && fld.Of == Schema
	if holdsSchema != (f.obj.Kind == Schema) {
		return wrongValue
	}
	if fld.Required.Holds(m) {
		return wrongValue
	}
	for _, r := range f.obj.Rules {
		if r.Kind != RuleRequiredWhen || r.Field != fld.Name || !r.Span.Holds(m) || f.test(r.When) != yes {
			continue
		}
		if r.Owner != "" {
			return missingElsewhere
		}
		return wrongValue
	}
	if f.obj.Kind == SecurityRequirement {
		return noScopes
	}
	return leftOut
}

// writes reports whether the object writes the field name at minor m. A null
// the field's own check reads as left out, or as a field missing (nullRead),
// writes nothing, so no rule holds it against the object either; one the field
// takes as its value, as only a field of any shape does, or as a value of the
// wrong shape, is written.
func (f frame) writes(name string, m Minor) bool {
	node := nodeview.Deref(f.byKey[name])
	switch {
	case node == nil:
		return false
	case !isNull(node):
		return true
	}
	fld := f.obj.fieldAt(name, m)
	return matches(fld.Shape, node) || f.nullRead(fld, m) == wrongValue
}

// isNull reports whether the node is a null.
func isNull(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}

// shapeOf finds the shape, among the field's own and the alternatives that hold
// at the declared minor, that the node has.
func (w *walker) shapeOf(fld Field, node *yaml.Node) (Shape, bool) {
	if matches(fld.Shape, node) {
		return fld.Shape, true
	}
	for _, alt := range fld.Alts {
		if alt.Span.Holds(w.minor) && matches(alt.Shape, node) {
			return alt.Shape, true
		}
	}
	// A kind that admits true and false in an object's place admits them
	// wherever it is held.
	if fld.Shape == Nested && w.spec.booleanHeld(fld.Of, w.minor) && matches(Boolean, node) {
		return Boolean, true
	}
	return fld.Shape, false
}

// booleanHeld reports whether the kind admits a boolean in its place at minor
// m.
func (s Spec) booleanHeld(k Kind, m Minor) bool {
	obj, ok := s.Object(k)
	return ok && obj.Boolean.Holds(m)
}

// scalar checks a scalar value: that it is not empty where the grammar refuses
// that, that it is one the field enumerates, and that it has its syntax.
func (w *walker) scalar(f frame, fld Field, node *yaml.Node, ptr jsontext.Pointer) {
	if node.Kind != yaml.ScalarNode {
		return
	}
	section := citeFor(fld.Cites, w.minor).Section
	switch {
	case fld.NonEmpty.Holds(w.minor) && node.Value == "":
		w.report(ir.SeverityError, classEmpty, ptr, "%s field %q must not be empty in OpenAPI %s (%s)",
			f.obj.Name, fld.Name, w.minor, section)
	case len(fld.Values) > 0:
		w.enumerated(f, fld, node.Value, ptr)
	case fld.Format != FormatNone && node.Value != "" && !validFormat(fld.Format, node.Value):
		w.report(ir.SeverityError, classFormat, ptr, "%s field %q must be %s in OpenAPI %s (%s)",
			f.obj.Name, fld.Name, describeFormat(fld.Format), w.minor, section)
	}
}

// enumerated checks text against the values the field admits.
func (w *walker) enumerated(f frame, fld Field, text string, ptr jsontext.Pointer) {
	for _, v := range fld.Values {
		if v.Name != text || f.test(v.When) == no {
			continue
		}
		if v.Span.Holds(w.minor) {
			return
		}
		if v.Span.Later(w.minor) {
			w.later(f.obj, fld.Name, text, v.Span.Since, ptr)
			return
		}
	}
	admitted := w.admitted(f, fld)
	if admitted == "" {
		return // the sibling the values depend on names none, which it reports itself
	}
	w.report(ir.SeverityError, classValues, ptr, "%s field %q must be one of %s in OpenAPI %s (%s)",
		f.obj.Name, fld.Name, admitted, w.minor, citeFor(fld.Cites, w.minor).Section)
}

// admitted lists the values the field admits at the declared minor, as the
// object's own fields stand.
func (w *walker) admitted(f frame, fld Field) string {
	var names []string
	for _, v := range fld.Values {
		if v.Span.Holds(w.minor) && f.test(v.When) != no {
			names = append(names, strconv.Quote(v.Name))
		}
	}
	return strings.Join(names, ", ")
}

// entries checks what a container value holds: the object, or the objects of a
// map or list, each read as the kind the field names.
func (w *walker) entries(f frame, fld Field, shape Shape, node *yaml.Node, ptr jsontext.Pointer, depth int) {
	switch shape {
	case Nested:
		w.object(site{kind: fld.Of, node: node, ptr: ptr, held: fld.Name, in: f.obj.Kind}, depth+1)
	case Map:
		w.repeated(node, ptr, "field "+strconv.Quote(fld.Name)+" of the "+f.obj.Name)
		for _, p := range w.view.MappingPairs(node) {
			if fld.Keys == ComponentName && !validFormat(ComponentName, p.Key) {
				w.report(ir.SeverityWarning, classSyntax, ptr.AppendToken(p.Key),
					"%s field %q has the key %q, which must be %s in OpenAPI %s (%s)",
					f.obj.Name, fld.Name, p.Key, describeFormat(fld.Keys), w.minor, citeFor(fld.Cites, w.minor).Section)
			}
			w.object(site{kind: fld.Of, node: p.Val, ptr: ptr.AppendToken(p.Key), held: p.Key, in: f.obj.Kind}, depth+1)
		}
	case List:
		for i, item := range node.Content {
			w.object(site{kind: fld.Of, node: item, ptr: itemPointer(ptr, i), in: f.obj.Kind}, depth+1)
		}
	case StringList:
		for i, item := range node.Content {
			item = nodeview.Deref(item)
			w.text(f, fmt.Sprintf("field %q item %d", fld.Name, i), fld, item, itemPointer(ptr, i))
			if len(fld.Values) > 0 {
				w.enumerated(f, fld, item.Value, itemPointer(ptr, i))
			}
		}
	case StringMap:
		for _, p := range w.view.MappingPairs(node) {
			w.text(f, fmt.Sprintf("field %q entry %q", fld.Name, p.Key), fld, nodeview.Deref(p.Val), ptr.AppendToken(p.Key))
		}
	default:
		// A scalar, or a container of values the table leaves unread.
	}
}

// matches reports whether the node has the shape.
func matches(shape Shape, node *yaml.Node) bool {
	switch shape {
	case Any:
		return true
	case String:
		return node.Kind == yaml.ScalarNode && node.Tag != "!!null"
	case Boolean:
		return node.Kind == yaml.ScalarNode && node.Tag == "!!bool"
	case Number, PositiveNumber, Natural:
		return numberOf(shape, node)
	case Nested, Map, StringMap, RawMap:
		return node.Kind == yaml.MappingNode && (shape != StringMap || allScalar(node))
	}
	// List, RawList and StringList.
	return node.Kind == yaml.SequenceNode && (shape != StringList || allScalar(node))
}

// allScalar reports whether every entry or item of the container is a scalar
// that is not null.
func allScalar(node *yaml.Node) bool {
	for i, n := range node.Content {
		if node.Kind == yaml.MappingNode && i%2 == 0 {
			continue
		}
		n = nodeview.Deref(n)
		if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
			return false
		}
	}
	return true
}

// stringStyles marks the styles that write a string whatever its text.
const stringStyles = yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle

// numberOf reports whether the node is a number of the shape: any number, one
// above zero, or an integer from zero up. A float that holds a whole number,
// 1.0, is an integer. A plain scalar is judged by its text, not its tag: the
// YAML resolver tags a number past float64's range a string, which JSON admits.
func numberOf(shape Shape, node *yaml.Node) bool {
	if node.Kind != yaml.ScalarNode || node.Style&stringStyles != 0 {
		return false
	}
	sign, integral, ok := parseNumber(node.Value)
	return ok && inRange(shape, sign, integral)
}

// outOfRange reports whether the node's text, quoted or not, is a number the
// shape's range refuses: one at or below zero for PositiveNumber, a fraction or
// a negative number for Natural.
func outOfRange(shape Shape, node *yaml.Node) bool {
	if node.Kind != yaml.ScalarNode {
		return false
	}
	sign, integral, ok := parseNumber(node.Value)
	return ok && !inRange(shape, sign, integral)
}

// inRange reports whether a number of the given sign and integrality falls in the
// range of the shape: above zero, an integer from zero up, or, for any other
// shape, anything.
func inRange(shape Shape, sign int, integral bool) bool {
	switch shape {
	case PositiveNumber:
		return sign > 0
	case Natural:
		return integral && sign >= 0
	default:
		return true
	}
}

// parseNumber reads the sign and integrality of a YAML number without
// materializing it. The decimal grammar is ir's, so 1e1000001 reads as the
// integer it is, and so does a literal past the range a BigVal holds: the spec's
// validity does not hang on that range. A based integer (0x10, 0b1010, 0o17) is
// YAML's. A literal with a leading zero reads as decimal or as octal to one sign
// and integrality, so trying the decimal grammar first is safe. A fraction such
// as 4/2 or a binary exponent is text, not a number, in YAML and JSON alike.
func parseNumber(text string) (sign int, integral, ok bool) {
	plain := strings.ReplaceAll(text, "_", "")
	d, err := ir.ParseDecimal(plain)
	if err == nil {
		return d.Sign(), d.IsInt(), true
	}
	if past, isPast := errors.AsType[*ir.ExponentRangeError](err); isPast {
		return past.Sign(), past.IsInt(), true
	}
	if i, isBased := new(big.Int).SetString(plain, 0); isBased {
		return i.Sign(), true, true
	}
	return 0, false, false
}

// describeShape says what a field's value must be.
func describeShape(fld Field) string {
	forms := make([]string, 1, 1+len(fld.Alts))
	forms[0] = shapeName(fld.Shape)
	for _, alt := range fld.Alts {
		forms = append(forms, shapeName(alt.Shape))
	}
	return strings.Join(slices.Compact(forms), " or ")
}

// shapeNames spell each shape with its article.
func shapeNames() map[Shape]string {
	return map[Shape]string{
		String: "a string", Boolean: "a boolean", Number: "a number", PositiveNumber: "a number above zero",
		Natural: "a non-negative integer", Nested: "an object", Map: "an object", StringMap: "an object",
		RawMap: "an object", List: "a list", RawList: "a list", StringList: "a list of strings",
	}
}

// shapeName spells a shape with its article.
func shapeName(shape Shape) string { return shapeNames()[shape] }
