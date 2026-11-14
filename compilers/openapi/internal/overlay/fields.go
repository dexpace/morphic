// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// form is the shape of value a fixed field of an overlay object takes.
type form uint8

const (
	formString form = iota
	formBool
	formAny
	formObject
	formList
)

// String names f in a finding.
func (f form) String() string {
	return [...]string{"a string", "a boolean", "any value", "an object", "a list"}[f]
}

// field is one fixed field of an object: its name, its form, the version that
// first defines it, and whether the object must write it.
type field struct {
	name     string
	form     form
	since    version
	required bool
}

// object names an object of the overlay document.
type object uint8

const (
	objOverlay object = iota
	objInfo
	objAction
	objReference
	objComponents
	objReusable
	objFields
)

// fieldsOf returns the fixed fields of o, as the Overlay Specification gives
// them, each with the version that first defines it. An overlay's version and
// its actions are read apart, since what they refuse is said otherwise.
func fieldsOf(o object) []field {
	switch o {
	case objOverlay:
		return []field{{"overlay", formString, v10, false}, {"info", formObject, v10, true},
			{"extends", formString, v10, false}, {"actions", formList, v10, false},
			{"$self", formString, v12, false}, {"components", formObject, v12, false}}
	case objInfo:
		return []field{{"title", formString, v10, true}, {"version", formString, v10, true}, {"description", formString, v11, false}}
	case objAction:
		return append([]field{{"target", formString, v10, true}}, fieldsOf(objFields)...)
	case objReference:
		return []field{{"$ref", formString, v12, true}, {"target", formString, v12, true}, {"description", formString, v12, false}}
	case objComponents:
		return []field{{"actions", formObject, v12, false}}
	case objReusable:
		return []field{{"description", formString, v12, false}, {"fields", formObject, v12, false}}
	default:
		return []field{{"description", formString, v10, false}, {"update", formAny, v10, false},
			{"remove", formBool, v10, false}, {"copy", formString, v11, false}}
	}
}

// fieldOf returns the field of o named name, and whether o has one.
func fieldOf(o object, name string) (field, bool) {
	for _, f := range fieldsOf(o) {
		if f.name == name {
			return f, true
		}
	}
	return field{}, false
}

// fits reports whether n, a field's value with its alias followed, takes form
// f, reading a scalar as the compiler reads one: a timestamp is the string it
// spells.
func fits(n *yaml.Node, f form) bool {
	switch f {
	case formAny:
		return n != nil
	case formObject:
		return n != nil && n.Kind == yaml.MappingNode
	case formList:
		return n != nil && n.Kind == yaml.SequenceNode
	default:
	}
	s, err := value.Classify(n)
	if err != nil {
		return false
	}
	if f == formBool {
		return s.Kind == value.ScalarBool
	}
	return s.Kind == value.ScalarString || s.Kind == value.ScalarTimestamp
}

// isTrue reports whether n is the boolean true.
func isTrue(n *yaml.Node) bool {
	s, err := value.Classify(n)
	return err == nil && s.Kind == value.ScalarBool && s.Bool
}

// reader reads one overlay document by the fields its version defines,
// collecting what it finds.
type reader struct {
	view     *nodeview.View
	declared string
	diags    []ir.Diagnostic
	at       ir.Provenance
	version  version
}

// invalid records an openapi/invalid-overlay error at the pointer at of the
// overlay document.
func (r *reader) invalid(at jsontext.Pointer, format string, args ...any) {
	r.diags = append(r.diags, diag.Newf(ir.SeverityError, diag.OverlayInvalid,
		ir.Provenance{Source: r.at.Source, Pointer: at}, "invalid overlay: "+format, args...))
}

// object reads n, at the pointer at, as the object o, named what in a
// finding: each fixed field the version defines in its form, each x- field
// taken as an extension, and any other refused, as is a required field
// missing. It returns the fields written, a field of the wrong form held as
// nil, and nil where n is no object.
func (r *reader) object(n *yaml.Node, at jsontext.Pointer, o object, what string) map[string]*yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		r.invalid(at, "%s is no object", what)
		return nil
	}
	pairs := r.view.MappingPairs(n)
	if !r.view.Expanded(n) {
		r.invalid(at, "%s merges through `<<` deeper than %d levels", what, nodeview.MergeDepthLimit)
		return nil
	}
	got := map[string]*yaml.Node{}
	for _, p := range pairs {
		if !strings.HasPrefix(p.Key, "x-") {
			got[p.Key] = r.field(o, what, at.AppendToken(p.Key), p)
		}
	}
	for _, f := range fieldsOf(o) {
		if _, written := got[f.name]; f.required && f.since <= r.version && !written {
			r.invalid(at, "%s names no %s", what, f.name)
		}
	}
	return got
}

// field returns the value p writes for a field of o, or nil, refusing it,
// where o defines no such field at the version read or p's value is of the
// wrong form.
func (r *reader) field(o object, what string, at jsontext.Pointer, p nodeview.Pair) *yaml.Node {
	f, defined := fieldOf(o, p.Key)
	switch {
	case !defined:
		r.invalid(at, "%s sets %q, a field Overlay 1.%d does not define for it", what, p.Key, r.version)
	case f.since > r.version:
		r.invalid(at, "%s sets %s, which Overlay 1.%d introduced; this overlay declares %s", what, p.Key, f.since, r.declared)
	case !fits(p.Val, f.form):
		r.invalid(at, "the %s %s sets is not %s", p.Key, what, f.form)
	default:
		return p.Val
	}
	return nil
}

// overlay reads the document's root as an overlay: its version first, which
// decides what the rest may write, then each of its fields.
func (r *reader) overlay(root *yaml.Node) plan {
	if root == nil || root.Kind != yaml.MappingNode {
		r.invalid("", "the document is no object")
		return plan{}
	}
	declared := r.view.ChildByToken(root, "overlay")
	switch {
	case declared == nil:
		r.invalid("", "it declares no overlay version")
		return plan{}
	case !fits(declared, formString):
		r.invalid("/overlay", "its overlay version, %s, is not a string", declared.Value)
		return plan{}
	}
	v, known := versionOf(declared.Value)
	if !known {
		r.invalid("/overlay", "overlay version %q is not one the compiler applies, which are 1.0.x, 1.1.x and 1.2.x", declared.Value)
		return plan{}
	}
	r.version, r.declared = v, declared.Value
	fields := r.object(root, "", objOverlay, "the overlay")
	if info := fields["info"]; info != nil {
		r.object(info, "/info", objInfo, "the info object")
	}
	p := plan{at: r.at, declared: declared.Value, version: v, selfURI: r.uri(fields, "$self", v12)}
	r.uri(fields, "extends", v12)
	p.actions = r.actions(fields["actions"], r.components(fields["components"]))
	return p
}

// uri reads the field name as a URI reference, which from version noFragment
// on holds no fragment, and returns it as written.
func (r *reader) uri(fields map[string]*yaml.Node, name string, noFragment version) string {
	n := fields[name]
	if n == nil {
		return ""
	}
	at := jsontext.Pointer("").AppendToken(name)
	if _, err := url.Parse(n.Value); err != nil {
		r.invalid(at, "%s %q is no URI reference", name, n.Value)
		return ""
	}
	if r.version >= noFragment && strings.Contains(n.Value, "#") {
		r.invalid(at, "%s %q holds a fragment, which Overlay 1.2 does not admit", name, n.Value)
		return ""
	}
	return n.Value
}

// components reads the components object and returns the fields of each
// reusable action it declares, by key.
func (r *reader) components(n *yaml.Node) map[string]map[string]*yaml.Node {
	if n == nil {
		return nil
	}
	actions := r.object(n, "/components", objComponents, "the components object")["actions"]
	if actions == nil {
		return nil
	}
	out := map[string]map[string]*yaml.Node{}
	for _, p := range r.view.MappingPairs(actions) {
		at := jsontext.Pointer("/components/actions").AppendToken(p.Key)
		what := fmt.Sprintf("reusable action %q", p.Key)
		fields := map[string]*yaml.Node{}
		if f := r.object(p.Val, at, objReusable, what)["fields"]; f != nil {
			fields = r.object(f, at.AppendToken("fields"), objFields, "the fields of "+what)
		}
		out[p.Key] = fields
	}
	return out
}

// actions reads the list of actions, at least one, each an action or, in an
// Overlay 1.2 document, a reference to a reusable action.
func (r *reader) actions(n *yaml.Node, reusable map[string]map[string]*yaml.Node) []action {
	if n == nil || len(n.Content) == 0 {
		r.invalid("", "it holds no action")
		return nil
	}
	out := make([]action, len(n.Content))
	for i, entry := range n.Content {
		out[i] = r.action(i, len(n.Content), nodeview.Deref(entry), reusable)
	}
	return out
}

// action reads the action at index i of of, and its selectors.
func (r *reader) action(i, of int, n *yaml.Node, reusable map[string]map[string]*yaml.Node) action {
	at := jsontext.Pointer("/actions/" + strconv.Itoa(i))
	a := action{index: i, of: of, overlay: ir.Provenance{Source: r.at.Source, Pointer: at}}
	what := fmt.Sprintf("action %d of %d", i+1, of)
	var fields map[string]*yaml.Node
	if r.version == v12 && n != nil && n.Kind == yaml.MappingNode && r.view.ChildByToken(n, "$ref") != nil {
		fields = r.reference(n, at, what, reusable)
	} else {
		fields = r.object(n, at, objAction, what)
	}
	if target := fields["target"]; target != nil {
		a.target = target.Value
	}
	a.kind = kindOf(fields)
	if a.kind == kindUpdate {
		a.payload = fields["update"]
	}
	if a.kind == kindUpdate && fields["copy"] != nil {
		r.diags = append(r.diags, diag.Newf(ir.SeverityError, diag.OverlayFailed, a.overlay,
			"%s sets both update and copy, and the specification gives each no effect beside the other; "+
				"write one of them", a.label()))
	}
	r.selectors(&a, fields)
	r.diags = append(r.diags, idleFields(a, fields)...)
	return a
}

// selectors reads a's target and copy selectors as RFC 9535 queries.
func (r *reader) selectors(a *action, fields map[string]*yaml.Node) {
	var found []ir.Diagnostic
	if fields["target"] != nil {
		a.targets, found = readSelector(*a, "target", a.target)
		r.diags = append(r.diags, found...)
	}
	if c := fields["copy"]; c != nil {
		a.sources, found = readSelector(*a, "copy", c.Value)
		r.diags = append(r.diags, found...)
	}
}

// reference reads a Reusable Action Reference: its $ref names an entry of
// components.actions by a JSON pointer in the document's own fragment, and
// the action it stands for is that entry's fields at the reference's own
// target, with the reference's description where it writes one.
func (r *reader) reference(n *yaml.Node, at jsontext.Pointer, what string, reusable map[string]map[string]*yaml.Node,
) map[string]*yaml.Node {
	ref := r.object(n, at, objReference, what)
	text := ref["$ref"]
	if text == nil {
		return nil
	}
	key, ok := componentKey(text.Value)
	fields, found := reusable[key]
	if !ok || !found {
		r.invalid(at.AppendToken("$ref"), "%s refers to %q, which names no reusable action of this document's "+
			"components.actions", what, text.Value)
		return nil
	}
	out := maps.Clone(fields)
	out["target"] = ref["target"]
	if d := ref["description"]; d != nil {
		out["description"] = d
	}
	return out
}

// componentKey returns the key of components.actions the reference ref names,
// `#/components/actions/<key>` with the key escaped as RFC 6901 escapes a
// token, and whether it names one.
func componentKey(ref string) (string, bool) {
	u, err := url.Parse(ref)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != "" || u.Opaque != "" || u.RawQuery != "" {
		return "", false
	}
	token, named := strings.CutPrefix(u.Fragment, "/components/actions/")
	if !named || strings.Contains(token, "/") {
		return "", false
	}
	unescaped := strings.NewReplacer("~1", "/", "~0", "~").Replace(token)
	if strings.Count(token, "~") != strings.Count(token, "~0")+strings.Count(token, "~1") {
		return "", false
	}
	return unescaped, true
}
