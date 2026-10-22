package oas

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// MaxDepth bounds how many objects deep the check descends. Each object it
// descends into is a mapping nested below the one holding it, and the
// pre-parse scan admits no document nested past 1,024 levels with its aliases
// expanded (scan.MaxNesting, to which a test of the load phase holds this), so
// the bound stops the check of no document the compiler admits.
const MaxDepth = 1024

// DefaultBudget is how many objects, entries and items a check reads when
// Options.Budget is not set.
const DefaultBudget = 1 << 22

// Locator returns the provenance of the source position at a pointer.
type Locator func(pointer jsontext.Pointer) ir.Provenance

// Options configures one check.
type Options struct {
	// Locate places a finding in the source. Unset, findings carry no
	// position.
	Locate Locator
	// Start is the kind of the object the root node is. Unset, the root is
	// the OpenAPI Object.
	Start Kind
	// Budget bounds the work; unset, DefaultBudget.
	Budget int
	// Seen, when set, is told each mapping read as an object of a kind and
	// not as a Reference Object standing in its place. The node is the
	// mapping itself, its aliases followed.
	Seen func(kind Kind, node *yaml.Node)
	// ReadAt, when set, has a finding Locate places elsewhere than the
	// pointer it was read at, through an alias or a merge key, name that
	// pointer, under Root.
	ReadAt bool
	// Root is the pointer the root is read at in the document Locate answers
	// in, empty for that document's own root.
	Root jsontext.Pointer
}

// Checker judges documents against the table.
type Checker struct{ spec Spec }

// NewChecker returns a Checker over the table.
func NewChecker() Checker { return Checker{spec: Table()} }

// Check judges the tree root, an object of the kind Options.Start names, as an
// OpenAPI document declaring version, and returns what it finds. A version the
// table does not cover is held to nothing. Findings come in a fixed order: one
// object after another, its fields as the table lists them, then its patterned
// keys, then its rules.
func (c Checker) Check(root *yaml.Node, declared string, opts Options) []ir.Diagnostic {
	minor, ok := ParseMinor(declared)
	if !ok {
		return nil
	}
	w := &walker{spec: c.spec, minor: minor, view: nodeview.New(), budget: opts.Budget}
	w.locate, w.declared, w.seen = opts.Locate, declared, opts.Seen
	w.readAt, w.rootAt, w.said = opts.ReadAt, opts.Root, map[ir.Diagnostic]bool{}
	if w.budget <= 0 {
		w.budget = DefaultBudget
	}
	if w.locate == nil {
		w.locate = func(jsontext.Pointer) ir.Provenance { return ir.Provenance{} }
	}
	start := opts.Start
	if start == 0 {
		start = Document
	}
	w.root = nodeview.DocumentRoot(root)
	w.object(site{kind: start, node: w.root}, 0)
	return w.diags
}

// walker is one check in progress.
type walker struct {
	spec    Spec
	minor   Minor
	view    *nodeview.View
	root    *yaml.Node
	budget  int
	stopped bool
	seen    func(Kind, *yaml.Node)
	reporter
}

// site is an object to read: its kind, node, pointer, the key its parent
// holds it under, and the kind of that parent, zero at the root.
type site struct {
	kind Kind
	node *yaml.Node
	ptr  jsontext.Pointer
	held string
	in   Kind
}

// frame is a mapping read as an object.
type frame struct {
	obj   Object
	at    site
	pairs []nodeview.Pair
	byKey map[string]*yaml.Node
}

// spend charges one unit of work and reports whether the walk may go on. The
// first time the depth or the budget runs out it says so, an error as any
// crossed budget is, and stops reading.
func (w *walker) spend(depth int, ptr jsontext.Pointer) bool {
	if w.stopped {
		return false
	}
	w.budget--
	if depth <= MaxDepth && w.budget >= 0 {
		return true
	}
	w.stopped = true
	w.diags = append(w.diags, diag.Newf(ir.SeverityError, diag.BudgetExceeded, w.locate(ptr),
		"the OpenAPI %s check stopped at this depth or work budget; what lies below is unchecked", w.minor))
	return false
}

// object checks the mapping s holds as an object of its kind, and what lies
// below it.
func (w *walker) object(s site, depth int) {
	obj, ok := w.spec.Object(s.kind)
	node := nodeview.Deref(s.node)
	if !ok || node == nil || !w.spend(depth, s.ptr) {
		return
	}
	if node.Kind != yaml.MappingNode {
		w.notMapping(obj, node, s.ptr)
		return
	}
	f := w.frameOf(obj, s, node)
	w.repeated(node, s.ptr, obj.Name)
	if w.reference(f, depth) {
		return
	}
	if w.seen != nil {
		w.seen(s.kind, node)
	}
	w.fields(f, depth)
	w.patterned(f, depth)
	w.rules(f)
}

// frameOf reads node, a mapping, as an object.
func (w *walker) frameOf(obj Object, s site, node *yaml.Node) frame {
	pairs := w.view.MappingPairs(node)
	byKey := make(map[string]*yaml.Node, len(pairs))
	for _, p := range pairs {
		byKey[p.Key] = p.Val
	}
	return frame{obj: obj, at: s, pairs: pairs, byKey: byKey}
}

// notMapping reports a node that is no mapping where the object's kind needs
// one, unless the kind admits a boolean there.
func (w *walker) notMapping(obj Object, node *yaml.Node, ptr jsontext.Pointer) {
	if obj.Boolean.Holds(w.minor) && matches(Boolean, node) {
		return
	}
	w.report(ir.SeverityError, classType, ptr, "%s must be an object in OpenAPI %s (%s)", obj.Name, w.minor, obj.Name)
}

// reference reads a mapping that writes $ref as a Reference Object where the
// object's kind admits one, and reports whether it did. Its target is checked
// where the target is written. A kind that admits the reference only from a
// later minor draws the later-version warning for the $ref, and one whose
// $ref takes siblings only from a later minor (RefSiblings) draws it for each
// key beside the $ref, which the declared minor ignores and the later one
// applies.
func (w *walker) reference(f frame, depth int) bool {
	if _, ok := f.byKey["$ref"]; !ok {
		return false
	}
	switch {
	case f.obj.Referenceable.Holds(w.minor):
	case f.obj.Referenceable.Later(w.minor):
		w.later(f.obj, "$ref", "", f.obj.Referenceable.Since, f.at.ptr.AppendToken("$ref"))
	default:
		for _, p := range f.pairs {
			if p.Key != "$ref" && f.obj.RefSiblings.Later(w.minor) {
				w.laterSibling(f.obj, p.Key, w.minor, f.obj.RefSiblings.Since, f.at.ptr.AppendToken(p.Key))
			}
		}
		return false
	}
	rf := f
	rf.obj = w.spec.must(Reference)
	w.fields(rf, depth)
	return true
}

// fields checks each field the object defines, and each a later minor added.
func (w *walker) fields(f frame, depth int) {
	for _, fld := range f.obj.Fields {
		if !fld.Defined.Holds(w.minor) {
			continue
		}
		val, present := f.byKey[fld.Name]
		if !present {
			w.missing(f, fld)
			continue
		}
		w.value(f, fld, val, depth)
	}
	for _, p := range f.pairs {
		if fld, ok := w.addedLater(f.obj, p.Key); ok {
			w.later(f.obj, p.Key, "", fld.Defined.Since, f.at.ptr.AppendToken(p.Key))
			w.value(f, fld, p.Val, depth)
		}
	}
}

// addedLater finds the field of obj named key that only a later minor than the
// declared one defines, unless a JSON Schema keyword, which the schema
// lowering reads as it finds it.
func (w *walker) addedLater(obj Object, key string) (Field, bool) {
	var found Field
	for _, fld := range obj.Fields {
		if fld.Name != key {
			continue
		}
		if fld.Defined.Holds(w.minor) {
			return Field{}, false
		}
		if fld.Defined.Later(w.minor) && found.Name == "" {
			found = fld
		}
	}
	return found, found.Name != "" && !found.Keyword
}

// missing reports a required field the object does not write.
func (w *walker) missing(f frame, fld Field) {
	if !fld.Required.Holds(w.minor) || fld.Owner != "" {
		return
	}
	c := citeFor(fld.Cites, w.minor)
	w.report(ir.SeverityError, classRequired, f.at.ptr, "%s field %q is REQUIRED in OpenAPI %s (%s)",
		f.obj.Name, fld.Name, w.minor, c.Section)
}

// patterned checks the keys the object admits beside its fixed fields: every
// key that is neither a field nor, where the object takes them, an extension.
func (w *walker) patterned(f frame, depth int) {
	pat, ok := w.patternHeld(f.obj)
	if !ok {
		return
	}
	for _, p := range f.pairs {
		if f.defines(p.Key) || (f.obj.Extensible && strings.HasPrefix(p.Key, "x-")) {
			continue
		}
		if pat.Keys != FormatNone && !validFormat(pat.Keys, p.Key) {
			w.report(ir.SeverityError, classFormat, f.at.ptr.AppendToken(p.Key),
				"%s has the key %q, which must be %s in OpenAPI %s (%s)",
				f.obj.Name, p.Key, describeFormat(pat.Keys), w.minor, citeFor(pat.Cites, w.minor).Section)
		}
		fld := Field{Name: p.Key, Shape: pat.Shape, Of: pat.Of, Defined: Always(), Cites: pat.Cites}
		w.value(f, fld, p.Val, depth)
	}
}

// patternHeld returns the pattern of obj that holds at the declared minor.
func (w *walker) patternHeld(obj Object) (Pattern, bool) {
	for _, p := range obj.Patterned {
		if p.Span.Holds(w.minor) {
			return p, true
		}
	}
	return Pattern{}, false
}

// fieldAt returns the field of o named name that minor m defines, else the
// first so named, which a later minor added, else the zero Field, whose Any
// shape admits every value.
func (o Object) fieldAt(name string, m Minor) Field {
	var found Field
	for _, fld := range o.Fields {
		if fld.Name != name {
			continue
		}
		if fld.Defined.Holds(m) {
			return fld
		}
		if found.Name == "" {
			found = fld
		}
	}
	return found
}

// defines reports whether the object names key as a field at any minor.
func (f frame) defines(key string) bool {
	for _, fld := range f.obj.Fields {
		if fld.Name == key {
			return true
		}
	}
	return false
}

// itemPointer is the pointer of item i of the sequence at ptr.
func itemPointer(ptr jsontext.Pointer, i int) jsontext.Pointer {
	return ptr.AppendToken(strconv.Itoa(i))
}

// citeFor picks the citation to name for a finding at minor m: the latest one
// the minor has reached, else the first.
func citeFor(cites []Cite, m Minor) Cite {
	var best Cite
	for _, c := range cites {
		if c.Minor <= m && c.Minor >= best.Minor {
			best = c
		}
	}
	if best.Minor == 0 && len(cites) > 0 {
		return cites[0]
	}
	return best
}
