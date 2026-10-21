package oas

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// tri is the answer to a question about siblings that the document may not
// settle.
type tri uint8

// The answers. unknown, the zero value, means a sibling the question needs is
// not written, or not a scalar: a rule that rests on it does not fire.
const (
	unknown tri = iota
	yes
	no
)

// test reports whether every term holds on the object's siblings. One term
// that fails settles it; a term that cannot be read leaves it unknown.
func (f frame) test(terms []Term) tri {
	answer := yes
	for _, t := range terms {
		got, ok := f.text(t.Field)
		switch {
		case !ok:
			answer = unknown
		case !slices.Contains(t.In, got):
			return no
		}
	}
	return answer
}

// text reads a sibling as a scalar. The field "$key" is the key the parent
// holds the object under.
func (f frame) text(name string) (string, bool) {
	if name == "$key" {
		return f.at.held, f.at.held != ""
	}
	return textOf(f.byKey[name])
}

// textOf reads a node as a scalar.
func textOf(node *yaml.Node) (string, bool) {
	n := nodeview.Deref(node)
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

// rules applies the object's rules that hold at the declared minor, bar those
// another package reports.
func (w *walker) rules(f frame) {
	r := ruler{w: w, f: f}
	for _, rule := range f.obj.Rules {
		if rule.Span.Holds(w.minor) && rule.Owner == "" {
			r.apply(rule)
		}
	}
}

// ruler applies rules to one object.
type ruler struct {
	w *walker
	f frame
}

// apply applies one rule.
func (r ruler) apply(rule Rule) {
	switch rule.Kind {
	case RuleExclusive:
		r.exclusive(rule)
	case RuleExactlyOne:
		r.exactlyOne(rule)
	case RuleAnyOf:
		r.anyOf(rule)
	case RuleCount:
		r.count(rule)
	case RuleRequiredWhen:
		r.requiredWhen(rule)
	case RuleForbiddenWhen:
		r.forbiddenWhen(rule)
	case RuleMustBe:
		r.mustBe(rule)
	case RuleMember:
		r.member(rule)
	case RuleUnique:
		r.unique(rule)
	case RuleInapplicable:
		r.inapplicable(rule)
	case RuleCode:
		r.code(rule)
	}
}

// fail reports a finding of the rule at ptr, graded by the rule's level and
// naming the object, the minor and the section the rule cites.
func (r ruler) fail(rule Rule, class string, ptr jsontext.Pointer, detail string) {
	r.w.report(severity(rule.Level), class, ptr, "%s %s in OpenAPI %s (%s)",
		r.f.obj.Name, detail, r.w.minor, citeFor(rule.Cites, r.w.minor).Section)
}

// present returns those of names the object writes, in the order given.
func (r ruler) present(names []string) []string {
	var out []string
	for _, n := range names {
		if _, ok := r.f.byKey[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// quoted spells names as a list of quoted fields.
func quoted(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = `"` + n + `"`
	}
	return strings.Join(parts, ", ")
}

// exclusive reports an object that writes more than one of the fields.
func (r ruler) exclusive(rule Rule) {
	if got := r.present(rule.Fields); len(got) > 1 {
		r.fail(rule, classExclusive, r.f.at.ptr, "fields "+quoted(got)+" are mutually exclusive")
	}
}

// exactlyOne reports an object that writes none of the fields, or several.
func (r ruler) exactlyOne(rule Rule) {
	switch got := r.present(rule.Fields); {
	case len(got) == 0:
		r.fail(rule, classRequired, r.f.at.ptr, "needs exactly one of the fields "+quoted(rule.Fields))
	case len(got) > 1:
		r.fail(rule, classExclusive, r.f.at.ptr, "fields "+quoted(got)+" are mutually exclusive")
	}
}

// anyOf reports an object that writes none of the fields.
func (r ruler) anyOf(rule Rule) {
	if len(r.present(rule.Fields)) == 0 {
		r.fail(rule, classRequired, r.f.at.ptr, "needs at least one of the fields "+quoted(rule.Fields))
	}
}

// count reports a map or list holding too few or too many entries. The field
// "" is the object itself, its extensions aside.
func (r ruler) count(rule Rule) {
	n, ptr, ok := r.entryCount(rule.Field)
	if !ok {
		return
	}
	switch {
	case n < rule.Min:
		r.fail(rule, classCount, ptr, r.named(rule.Field)+"must hold at least "+plural(rule.Min))
	case rule.Max > 0 && n > rule.Max:
		r.fail(rule, classCount, ptr, r.named(rule.Field)+"must hold at most "+plural(rule.Max))
	}
}

// plural spells n entries.
func plural(n int) string {
	return strconv.Itoa(n) + map[bool]string{true: " entry", false: " entries"}[n == 1]
}

// named spells the thing a count rule counts, ready to precede a verb: the
// field, or nothing where the object itself is counted.
func (r ruler) named(field string) string {
	if field == "" {
		return ""
	}
	return `field "` + field + `" `
}

// entryCount counts the entries of the field, where it is a map or a list.
func (r ruler) entryCount(field string) (int, jsontext.Pointer, bool) {
	if field == "" {
		n := 0
		for _, p := range r.f.pairs {
			if !strings.HasPrefix(p.Key, "x-") {
				n++
			}
		}
		return n, r.f.at.ptr, true
	}
	node := nodeview.Deref(r.f.byKey[field])
	ptr := r.f.at.ptr.AppendToken(field)
	switch {
	case node == nil:
		return 0, ptr, false
	case node.Kind == yaml.MappingNode:
		return len(r.w.view.MappingPairs(node)), ptr, true
	case node.Kind == yaml.SequenceNode:
		return len(node.Content), ptr, true
	}
	return 0, ptr, false
}

// requiredWhen reports a field missing where the conditions make it required.
func (r ruler) requiredWhen(rule Rule) {
	if _, ok := r.f.byKey[rule.Field]; ok || r.f.test(rule.When) != yes {
		return
	}
	r.fail(rule, classRequired, r.f.at.ptr,
		`field "`+rule.Field+`" is REQUIRED when `+describeTerms(rule.When))
}

// forbiddenWhen reports each of the fields written where the conditions bar it.
func (r ruler) forbiddenWhen(rule Rule) {
	if r.f.test(rule.When) != yes {
		return
	}
	where := ""
	if len(rule.When) > 0 {
		where = " when " + describeTerms(rule.When)
	}
	for _, name := range r.present(rule.Fields) {
		r.fail(rule, classForbidden, r.f.at.ptr.AppendToken(name), `field "`+name+`" MUST NOT be written`+where)
	}
}

// mustBe reports a field written with another value than the rule demands,
// where the conditions hold.
func (r ruler) mustBe(rule Rule) {
	got, ok := r.f.text(rule.Field)
	if !ok || r.f.test(rule.When) != yes || strings.EqualFold(got, rule.Value) {
		return
	}
	r.fail(rule, classValues, r.f.at.ptr.AppendToken(rule.Field),
		`field "`+rule.Field+`" must be `+rule.Value+` when `+describeTerms(rule.When))
}

// member reports a field whose value is not among the list the rule names.
func (r ruler) member(rule Rule) {
	got, ok := r.f.text(rule.Field)
	list := nodeview.Deref(r.f.byKey[rule.Of])
	if !ok || list == nil || list.Kind != yaml.SequenceNode || len(list.Content) == 0 {
		return
	}
	for _, item := range list.Content {
		if n := nodeview.Deref(item); n != nil && n.Value == got {
			return
		}
	}
	r.fail(rule, classValues, r.f.at.ptr.AppendToken(rule.Field),
		`field "`+rule.Field+`" must be one of the values of "`+rule.Of+`"`)
}

// unique reports each item of the list that repeats the key of an earlier one.
func (r ruler) unique(rule Rule) {
	list := nodeview.Deref(r.f.byKey[rule.Field])
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	seen := map[string]bool{}
	for i, item := range list.Content {
		name, ok := textOf(r.keysOf(item)[rule.Key])
		if !ok {
			continue
		}
		if seen[name] {
			ptr := itemPointer(r.f.at.ptr.AppendToken(rule.Field), i).AppendToken(rule.Key)
			r.fail(rule, classDuplicate, ptr, `field "`+rule.Field+`" repeats the `+rule.Key+" "+strconv.Quote(name))
		}
		seen[name] = true
	}
}

// keysOf reads a node as the entries of a mapping.
func (r ruler) keysOf(node *yaml.Node) map[string]*yaml.Node {
	out := map[string]*yaml.Node{}
	for _, p := range r.w.view.MappingPairs(node) {
		out[p.Key] = p.Val
	}
	return out
}

// inapplicable warns of a field written where the conditions give it no effect.
func (r ruler) inapplicable(rule Rule) {
	if _, ok := r.f.byKey[rule.Field]; !ok || r.f.test(rule.When) != yes {
		return
	}
	r.w.diags = append(r.w.diags, diag.Newf(ir.SeverityWarning, diag.InapplicableLocationKeyword,
		r.w.locate(r.f.at.ptr.AppendToken(rule.Field)),
		"%s field %q has no effect when %s in OpenAPI %s (%s); lowered as declared",
		r.f.obj.Name, rule.Field, describeTerms(rule.When), r.w.minor, citeFor(rule.Cites, r.w.minor).Section))
}
