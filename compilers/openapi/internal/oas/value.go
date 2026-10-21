package oas

import (
	"encoding/json/jsontext"
	"math/big"
	"slices"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// value checks the value a field is written with: its shape, then what its
// shape allows, then the objects it holds.
func (w *walker) value(f frame, fld Field, val *yaml.Node, depth int) {
	node := nodeview.Deref(val)
	ptr := f.at.ptr.AppendToken(fld.Name)
	shape, ok := w.shapeOf(fld, node)
	if !ok {
		w.report(ir.SeverityError, classType, ptr, "%s field %q must be %s in OpenAPI %s (%s)",
			f.obj.Name, fld.Name, describeShape(fld), w.minor, citeFor(fld.Cites, w.minor).Section)
		return
	}
	w.scalar(f, fld, node, ptr)
	w.entries(f, fld, shape, node, ptr, depth)
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
	if fld.Shape == Nested && w.booleanHeld(fld.Of) && matches(Boolean, node) {
		return Boolean, true
	}
	return fld.Shape, false
}

// booleanHeld reports whether the kind admits a boolean in its place at the
// declared minor.
func (w *walker) booleanHeld(k Kind) bool {
	obj, ok := w.spec.Object(k)
	return ok && obj.Boolean.Holds(w.minor)
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
		w.object(site{kind: fld.Of, node: node, ptr: ptr, held: fld.Name}, depth+1)
	case Map:
		w.repeated(node, ptr, "field "+strconv.Quote(fld.Name)+" of the "+f.obj.Name)
		for _, p := range w.view.MappingPairs(node) {
			if fld.Keys == ComponentName && !validFormat(ComponentName, p.Key) {
				w.report(ir.SeverityError, classSyntax, ptr.AppendToken(p.Key),
					"%s field %q has the key %q, which must be %s in OpenAPI %s (%s)",
					f.obj.Name, fld.Name, p.Key, describeFormat(fld.Keys), w.minor, citeFor(fld.Cites, w.minor).Section)
			}
			w.object(site{kind: fld.Of, node: p.Val, ptr: ptr.AppendToken(p.Key), held: p.Key}, depth+1)
		}
	case List:
		for i, item := range node.Content {
			w.object(site{kind: fld.Of, node: item, ptr: itemPointer(ptr, i)}, depth+1)
		}
	case StringList:
		for i, item := range node.Content {
			if len(fld.Values) > 0 {
				w.enumerated(f, fld, nodeview.Deref(item).Value, itemPointer(ptr, i))
			}
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
	n, ok := parseNumber(node.Value)
	switch {
	case !ok:
		return false
	case shape == PositiveNumber:
		return n.Sign() > 0
	case shape == Natural:
		return n.IsInt() && n.Sign() >= 0
	default:
		return true
	}
}

// parseNumber reads a YAML number exactly: an integer in any base the YAML
// resolver admits, else a decimal.
func parseNumber(text string) (*big.Rat, bool) {
	if i, err := strconv.ParseInt(strings.ReplaceAll(text, "_", ""), 0, 64); err == nil {
		return new(big.Rat).SetInt64(i), true
	}
	return new(big.Rat).SetString(text)
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
