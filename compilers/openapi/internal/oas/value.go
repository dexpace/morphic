// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// value checks the value a field is written with: its shape, then what its
// shape allows, then the objects it holds. A value of a Schema Object field
// whose reading its dialect decides beyond its shape is judged as read
// instead (dialectShape).
func (w *walker) value(f frame, fld Field, val *yaml.Node, depth int) {
	node := nodeview.Deref(val)
	ptr := f.at.ptr.AppendToken(fld.Name)
	shared := f.shared || sharedNode(val)
	if f.rows.rows != nil && dialectShape(w, f, fld, node, ptr) {
		w.accept(shared, node)
		return
	}
	shape, ok := shapeOf(f, fld, node, w.minor)
	if !ok {
		w.refuse(node)
		w.mismatch(f, fld, node, ptr)
		return
	}
	// A field of any shape reads no type: its value is the values' to render as
	// written (nodeview.Written), so it has no say in a node another place
	// refuses.
	if fld.Shape != Any {
		w.accept(shared, node)
	}
	if shape == String {
		w.text(f, fmt.Sprintf("field %q", fld.Name), fld, node, ptr)
	}
	w.scalar(f, fld, node, ptr)
	w.entries(f, fld, shape, node, ptr, depth)
}

// dialectShape judges a value of a Schema Object field that the schema
// lowering reads otherwise than its shape says, read with f's rows, and
// reports whether it did: a numeric bound, read as the decimal it spells,
// quoted or not (decimalBound), bar a null, read as left out, and a number
// its range refuses, such as a multipleOf of 0; and an `items` written as a
// list where the rows read no tuple, which a JSON Schema draft reads as one,
// an error saying what the document's version writes instead.
func dialectShape(w *walker, f frame, fld Field, node *yaml.Node, ptr jsontext.Pointer) bool {
	switch {
	case fld.Decimal && !isNull(node) && !outsideRuleset(node) && (matches(fld.Shape, node) || !outOfRange(fld.Shape, node)):
		decimalBound(w, f, fld, node, ptr)
		return true
	case fld.Name == "items" && node.Kind == yaml.SequenceNode && !f.rows.Takes("items", List):
		w.add(diag.Newf(ir.SeverityError, diag.InvalidItemsList, w.locate(ptr),
			"items must be a schema in this schema's dialect, not a list; %s", itemsListRemedy(f.rows)), ptr)
		return true
	default:
		return false
	}
}

// decimalBound reports an exclusive bound written in the other form than f's
// rows give it, a boolean modifying the bound beside it or a number of its
// own, and a bound, a boolean exclusive bound aside, no decimal spells, quoted
// or not (value.NumericLiteral), each an error at the value.
func decimalBound(w *walker, f frame, fld Field, node *yaml.Node, ptr jsontext.Pointer) {
	if fld.Name == "exclusiveMinimum" || fld.Name == "exclusiveMaximum" {
		boolean := f.rows.Takes(fld.Name, Boolean)
		if written := matches(Boolean, node); written != boolean {
			want := map[bool]string{true: "a boolean", false: "a number"}[boolean]
			w.add(diag.Newf(ir.SeverityError, diag.ExclusiveBoundForm, w.locate(ptr),
				"%s must be %s in this schema's dialect", fld.Name, want), ptr)
			return
		}
		if boolean {
			return
		}
	}
	if _, err := value.NumericLiteral(node); err != nil {
		w.add(diag.Newf(ir.SeverityError, diag.NumericPrecision, w.locate(ptr),
			"%s literal %q: %s", fld.Name, node.Value, err.Error()), ptr)
	}
}

// itemsListRemedy says what a writer can do about an `items` list where rules
// read no tuple: write it as `prefixItems`, or name by `$schema` a draft that
// reads one, where the rows define `prefixItems`, as 3.1's and 3.2's own rows
// do; else move to 3.1, as 3.0's rows do not.
func itemsListRemedy(rules SchemaRules) string {
	if rules.Defines("prefixItems") {
		return "write a tuple as prefixItems, or name a JSON Schema draft that reads a list as one by $schema"
	}
	return "OpenAPI 3.0 writes no tuple, which 3.1 writes as prefixItems"
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
	w.report(ir.SeverityWarning, classType, ptr, "%s %s must be a string in %s (%s); it is %s %s",
		f.obj.Name, label, f.in(w.minor), citeFor(fld.Cites, w.minor).Section, what, node.Value)
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
	if tagged := taggedOutside(node); tagged != nil {
		null = fmt.Sprintf("; it is tagged %q, outside YAML 1.2's JSON schema ruleset, to which OpenAPI %s limits tags (Format)",
			tagged.Tag, w.minor)
	}
	w.report(sev, classType, ptr, "%s field %q must be %s in %s (%s)%s",
		f.obj.Name, fld.Name, describeShape(fld), f.in(w.minor), citeFor(fld.Cites, w.minor).Section, null)
}

// taggedOutside returns node, or for a list the first item, that is tagged
// outside the ruleset (outsideRuleset), or nil for none.
func taggedOutside(node *yaml.Node) *yaml.Node {
	if outsideRuleset(node) {
		return node
	}
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	for _, item := range node.Content {
		if item = nodeview.Deref(item); outsideRuleset(item) {
			return item
		}
	}
	return nil
}

// outsideRuleset reports whether node is a scalar or a sequence under a tag
// YAML 1.2's JSON schema ruleset lacks, which OpenAPI limits a document's tags
// to (Format): it has no shape a typed field admits. A tagged mapping is
// refused before the check reads the tree.
func outsideRuleset(node *yaml.Node) bool {
	switch {
	case node == nil:
		return false
	case node.Kind == yaml.SequenceNode:
		return node.ShortTag() != "!!seq"
	case node.Kind != yaml.ScalarNode || node.Style&yaml.TaggedStyle == 0:
		return false
	}
	switch node.ShortTag() {
	case "!!null", "!!bool", "!!int", "!!float", "!!str":
		return false
	default:
		return true
	}
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
// lowering reads one for another object's schema as the empty schema, a value
// of the wrong shape, and one for any keyword of a Schema Object as left out. A
// null for an optional field is otherwise left out; for a required field it is
// the value that is missing, reported by whichever package owns the rule.
func (f frame) nullRead(fld Field, m Minor) nullReading {
	if fld.Shape == Nested && fld.Of == Schema && f.obj.Kind != Schema {
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
	// A requirement's name stands whatever its value, so it is no optional field.
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
	fld := f.fieldAt(name, m)
	return matches(fld.Shape, node) || f.nullRead(fld, m) == wrongValue
}

// fieldAt returns the field named name f's object defines at minor m, else a
// later minor's, or for a Schema Object the dialect the compiler reads
// natively (SchemaRules.Later), else the zero Field (Object.fieldAt).
func (f frame) fieldAt(name string, m Minor) Field {
	if fld := f.obj.fieldAt(name, m); fld.Name != "" || f.rows.rows == nil {
		return fld
	}
	return f.rows.rows.later[name]
}

// isNull reports whether the node is a null.
func isNull(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}

// shapeOf finds the shape, among the field's own and the alternatives that hold
// at the declared minor, that the node has. A node tagged outside the ruleset
// has none, but where the field takes any value (outsideRuleset).
func shapeOf(f frame, fld Field, node *yaml.Node, minor Minor) (Shape, bool) {
	if fld.Shape != Any && outsideRuleset(node) {
		return fld.Shape, false
	}
	if matches(fld.Shape, node) {
		return fld.Shape, true
	}
	for _, alt := range fld.Alts {
		if alt.Span.Holds(minor) && matches(alt.Shape, node) {
			return alt.Shape, true
		}
	}
	// Only the Schema Object stands as a boolean, where a field holds one: a
	// schema decides for the schemas it holds, as for an entry of a list or a
	// map of them (frame.holdsBoolean), and one no schema holds is read as the
	// object it is, so the rows it is read with decide (walker.notMapping),
	// where the minor admits one at all.
	if fld.Shape == Nested && fld.Of == Schema && f.booleans && matches(Boolean, node) {
		if f.rows.rows != nil {
			return Boolean, true
		}
		return Nested, true
	}
	return fld.Shape, false
}

// holdsBoolean reports whether node, an entry of the list or map of schemas fld
// holds, is true or false that f, a Schema Object, admits as a schema there:
// by its rows, or as 2020-12 reads a keyword it added (frame.asLater). A schema
// decides for the schemas it holds, as they are read in its dialect.
func (f frame) holdsBoolean(fld Field, node *yaml.Node) bool {
	return fld.Of == Schema && f.rows.rows != nil && f.booleans && matches(Boolean, nodeview.Deref(node))
}

// booleanHeld reports whether the kind admits a boolean in its place at minor
// m.
func (s Spec) booleanHeld(k Kind, m Minor) bool {
	obj, ok := s.Object(k)
	return ok && obj.Boolean.Holds(m)
}

// scalar checks a scalar value: that it is not empty where the grammar refuses
// that, that it is one the field enumerates, and that it has its syntax. The
// empty string is judged as any text is, so a format it breaks, as an absolute
// URI's, refuses it, and one admitting it says so in validFormat.
func (w *walker) scalar(f frame, fld Field, node *yaml.Node, ptr jsontext.Pointer) {
	if node.Kind != yaml.ScalarNode {
		return
	}
	section := citeFor(fld.Cites, w.minor).Section
	switch {
	case fld.NonEmpty.Holds(w.minor) && node.Value == "":
		w.report(ir.SeverityError, classEmpty, ptr, "%s field %q must not be empty in %s (%s)",
			f.obj.Name, fld.Name, f.in(w.minor), section)
	case len(fld.Values) > 0:
		w.enumerated(f, fld, node.Value, ptr)
	case fld.Format != FormatNone && !validFormat(fld.Format, node.Value):
		w.report(ir.SeverityError, classFormat, ptr, "%s field %q must be %s in %s (%s)",
			f.obj.Name, fld.Name, describeFormat(fld.Format), f.in(w.minor), section)
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
			w.laterField(f.obj, fld.Name, text, v.Span.Since, ptr)
			return
		}
	}
	admitted := f.admitted(fld, w.minor)
	if admitted == "" {
		return // the sibling the values depend on names none, which it reports itself
	}
	w.report(ir.SeverityError, classValues, ptr, "%s field %q must be one of %s in %s (%s)",
		f.obj.Name, fld.Name, admitted, f.in(w.minor), citeFor(fld.Cites, w.minor).Section)
}

// admitted lists the values the field admits at minor m, as the object's own
// fields stand.
func (f frame) admitted(fld Field, m Minor) string {
	var names []string
	for _, v := range fld.Values {
		if v.Span.Holds(m) && f.test(v.When) != no {
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
		w.object(site{kind: fld.Of, node: node, ptr: ptr, held: fld.Name, rows: f.rows, in: f.obj.Kind,
			shared: f.shared}, depth+1)
	case Map:
		w.repeated(node, ptr, "field "+strconv.Quote(fld.Name)+" of the "+f.obj.Name)
		pairs := w.view.MappingPairs(node)
		places := newKeyPlacer(w, node, ptr)
		for _, k := range keyFindings(f.obj, fld, pairs, w.minor) {
			w.add(diag.Newf(k.sev, diag.Validation+"/"+k.class, places.place(k.key), "%s", k.message),
				ptr.AppendToken(k.key))
		}
		for _, p := range pairs {
			if !f.holdsBoolean(fld, p.Val) {
				w.object(site{kind: fld.Of, node: p.Val, ptr: ptr.AppendToken(p.Key), held: p.Key, rows: f.rows,
					in: f.obj.Kind, shared: f.shared}, depth+1)
			}
		}
	case List:
		for i, item := range node.Content {
			if !f.holdsBoolean(fld, item) {
				w.object(site{kind: fld.Of, node: item, ptr: itemPointer(ptr, i), rows: f.rows, in: f.obj.Kind,
					shared: f.shared}, depth+1)
			}
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
		// A scalar, or a container of values the table leaves unread, which the
		// lowering keeps verbatim or reads as a value.
		rawRepeats(w, node, ptr, depth+1)
	}
}

// rawRepeats warns of each key a mapping written in a value the table reads as
// no object writes more than once, at its later copy, as repeated warns of one
// an object writes: the lowering reads such a value at the copy the model
// reads. It spends the walk's own budget, one unit a mapping or list. ptr is
// where the value is, and the one pointer the walk spells unless it places a
// finding below (rawPointer): the pointer bound counts such a value by its
// root alone (docs/compilers/openapi.md#source-budgets).
func rawRepeats(w *walker, val *yaml.Node, ptr jsontext.Pointer, depth int) {
	node := nodeview.Deref(val)
	if node == nil || node.Kind != yaml.MappingNode && node.Kind != yaml.SequenceNode {
		return
	}
	ok, stops := w.charge(depth)
	if stops {
		reportStop(w, rawPointer(ptr, w.raw))
	}
	if !ok {
		return
	}
	if node.Kind == yaml.SequenceNode {
		for i, item := range node.Content {
			w.raw = append(w.raw, rawStep{index: i, item: true})
			rawRepeats(w, item, ptr, depth+1)
			w.raw = w.raw[:len(w.raw)-1]
		}
		return
	}
	if len(nodeview.RepeatedKeys(node)) > 0 {
		w.repeated(node, rawPointer(ptr, w.raw), "a mapping")
	}
	for _, p := range w.view.MappingPairs(node) {
		w.raw = append(w.raw, rawStep{key: p.Key})
		rawRepeats(w, p.Val, ptr, depth+1)
		w.raw = w.raw[:len(w.raw)-1]
	}
}

// rawStep names a node of a value the table reads as no object, under the
// node holding it: a mapping's key, or with item a list's index.
type rawStep struct {
	key   string
	index int
	item  bool
}

// rawPointer spells where steps lead from ptr, a raw value's root. A walk of
// one keeps its steps (walker.raw) in place of a pointer a node, which costs
// the bytes of every key above a node at each.
func rawPointer(ptr jsontext.Pointer, steps []rawStep) jsontext.Pointer {
	for _, s := range steps {
		if s.item {
			ptr = itemPointer(ptr, s.index)
			continue
		}
		ptr = ptr.AppendToken(s.key)
	}
	return ptr
}

// keyFinding is one finding about a key of a map (keyFindings).
type keyFinding struct {
	sev     ir.Severity
	class   string
	key     string
	message string
}

// keyFindings checks the keys of the map field fld of obj holds, pairs,
// against the syntax the table gives them at minor: a component name, which
// the specification writes as a pattern every key must match, and a media
// type, which a content map's key is, are held to it by a warning, and an HTTP
// field name by an error, as a header parameter's name is, beside what
// fieldNames finds.
func keyFindings(obj Object, fld Field, pairs []nodeview.Pair, minor Minor) []keyFinding {
	switch fld.Keys {
	case ComponentName, MediaTypeKey, FieldName:
	default:
		return nil
	}
	var found []keyFinding
	sev, class := ir.SeverityWarning, classSyntax
	if fld.Keys == FieldName {
		sev, class = ir.SeverityError, classFormat
	}
	for _, p := range pairs {
		if !validFormat(fld.Keys, p.Key) {
			found = append(found, keyFinding{sev: sev, class: class, key: p.Key,
				message: fmt.Sprintf("%s field %q has the key %q, which must be %s in OpenAPI %s (%s)", obj.Name,
					fld.Name, p.Key, describeFormat(fld.Keys), minor, citeFor(fld.Cites, minor).Section)})
		}
	}
	if fld.Keys == FieldName {
		found = append(found, fieldNames(obj, fld, pairs, minor)...)
	}
	return found
}

// fieldNames finds each key of the map field fld of obj holds, pairs, that
// names the header a key spelled lower in byte order names in
// another case, a warning: HTTP reads the two as one, and the model as two.
// Which key draws it follows the spellings rather than the order the map writes
// them in.
func fieldNames(obj Object, fld Field, pairs []nodeview.Pair, minor Minor) []keyFinding {
	lowest := make(map[string]string, len(pairs))
	for _, p := range pairs {
		folded := strings.ToLower(p.Key)
		if spelled, ok := lowest[folded]; !ok || p.Key < spelled {
			lowest[folded] = p.Key
		}
	}
	var found []keyFinding
	for _, p := range pairs {
		if first := lowest[strings.ToLower(p.Key)]; first != p.Key {
			found = append(found, keyFinding{sev: ir.SeverityWarning, class: classDuplicate, key: p.Key,
				message: fmt.Sprintf("%s field %q has the keys %q and %q, which name one header, as HTTP compares "+
					"field names in any case, and both are read in OpenAPI %s (%s)", obj.Name, fld.Name, first, p.Key,
					minor, citeFor(fld.Cites, minor).Section)})
		}
	}
	return found
}

// Admits reports whether node, a field's value, has the shape s as the check
// reads one, an alias read as the node it names, so a package reading a value
// the check owns can tell one it passed from one it reported. Only a scalar
// tagged !!bool is a Boolean, where the model reads a string as one
// (docs/parser-library.md#string-as-boolean).
func (s Shape) Admits(node *yaml.Node) bool {
	n := nodeview.Deref(node)
	return n != nil && matches(s, n)
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
// that is neither null nor tagged outside the ruleset (outsideRuleset).
func allScalar(node *yaml.Node) bool {
	for i, n := range node.Content {
		if node.Kind == yaml.MappingNode && i%2 == 0 {
			continue
		}
		n = nodeview.Deref(n)
		if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" || outsideRuleset(n) {
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
// validity does not hang on that range. A based integer is YAML 1.2's core
// schema's, 0o17 or 0x10 (docs/parser-library.md#scalar-resolution): 0b1010,
// 1_000 and +0x10 are text, as a fraction such as 4/2 or a binary exponent is.
func parseNumber(text string) (sign int, integral, ok bool) {
	d, err := ir.ParseDecimal(text)
	if err == nil {
		return d.Sign(), d.IsInt(), true
	}
	if past, isPast := errors.AsType[*ir.ExponentRangeError](err); isPast {
		return past.Sign(), past.IsInt(), true
	}
	if c, isCore := nodeview.ParseCoreInteger(text); isCore {
		return c.Sign(), true, true
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
