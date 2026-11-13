// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"context"
	"encoding/json/jsontext"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
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

// Locator returns the provenance of the source position at a pointer.
type Locator func(pointer jsontext.Pointer) ir.Provenance

// Options configures one check.
type Options struct {
	// Locate places a finding in the source. Unset, findings carry no
	// position.
	Locate Locator
	// Seen, when set, is told each mapping read as an object of a kind and
	// not as a Reference Object standing in its place. The node is the
	// mapping itself, its aliases followed.
	Seen func(kind Kind, node *yaml.Node)
	// Root is the pointer the root is read at in the document Locate answers
	// in, empty for that document's own root.
	Root jsontext.Pointer
	// Budget bounds the work: how many objects, entries and items a check
	// reads. Unset, it is unbounded, as the zero budget.Limit is.
	Budget budget.Limit
	// Start is the kind of the object the root node is. Unset, the root is
	// the OpenAPI Object.
	Start Kind
	// ReadAt, when set, has a finding Locate places elsewhere than the
	// pointer it was read at, through an alias or a merge key, name that
	// pointer, under Root.
	ReadAt bool
	// Rows is the rows a Schema Object no schema holds is read with, its
	// document's; one a schema holds reads the rows of the schema holding
	// it. The zero Rows are the declared minor's own.
	Rows SchemaRules
	// RootRows, when set, returns the rows the schema at a pointer, Root
	// included, is read with where it roots a dialect of its own, which
	// stand over Rows and the rows of a schema holding it.
	RootRows func(at jsontext.Pointer) (SchemaRules, bool)
	// Later, when set, is handed each Schema Object no schema holds, which
	// the check then does not read: the rows it is read with are not known
	// yet. CheckPending reads them once they are.
	Later func(Pending)
}

// Pending is a Schema Object a check handed back unread (Options.Later): where
// it is read and how deep, so CheckPending reads it as the check would have.
type Pending struct {
	at    site
	depth int
}

// Checker judges documents against the table.
type Checker struct{ spec Spec }

// NewChecker returns a Checker over the table.
func NewChecker() Checker { return Checker{spec: Table()} }

// Spec returns the table c judges against, which the load phase reads a
// document's Schema Object rows from (Spec.SchemaRules) without building it
// again.
func (c Checker) Spec() Spec { return c.spec }

// Check judges the tree root, an object of the kind Options.Start names, as an
// OpenAPI document declaring version, and returns what it finds. A version the
// table does not cover is held to nothing. Findings come in a fixed order: one
// object after another, its fields as the table lists them, then its patterned
// keys, then its rules.
func (c Checker) Check(ctx context.Context, root *yaml.Node, declared string, opts Options) []ir.Diagnostic {
	w, ok := c.walker(ctx, declared, opts)
	if !ok {
		return nil
	}
	start := opts.Start
	if start == 0 {
		start = Document
	}
	w.root = nodeview.DocumentRoot(root)
	w.object(site{kind: start, node: w.root}, 0)
	return w.diags
}

// CheckPending judges each Schema Object a check handed back unread
// (Options.Later), in the order given, as that check would have read it, with
// opts' rows. One walk reads them all, so a node two of them mount draws one
// finding and one budget bounds them.
func (c Checker) CheckPending(ctx context.Context, pending []Pending, declared string, opts Options) []ir.Diagnostic {
	opts.Later = nil
	w, ok := c.walker(ctx, declared, opts)
	if !ok {
		return nil
	}
	for _, p := range pending {
		w.object(p.at, p.depth)
	}
	return w.diags
}

// walker starts a check of a document declaring declared under opts, and is
// false for a version the table does not cover.
func (c Checker) walker(ctx context.Context, declared string, opts Options) (*walker, bool) {
	minor, ok := ParseMinor(declared)
	if !ok {
		return nil, false
	}
	w := &walker{ctx: ctx, spec: c.spec, minor: minor, view: nodeview.New(), work: budget.New(opts.Budget)}
	w.locate, w.declared, w.seen, w.rows = opts.Locate, declared, opts.Seen, opts.Rows
	w.readAt, w.rootAt, w.said = opts.ReadAt, opts.Root, map[ir.Diagnostic]bool{}
	w.rootRows, w.later = opts.RootRows, opts.Later
	if w.rows.rows == nil {
		w.rows, _ = c.spec.SchemaRules(declared, "")
	}
	if w.locate == nil {
		w.locate = func(jsontext.Pointer) ir.Provenance { return ir.Provenance{} }
	}
	return w, true
}

// walker is one check in progress. It holds the context the check runs under
// for spend, which every read of an object asks first: a check stops reading
// once its context is done, as it stops at its budget, though silently, since
// what stopped it is no finding about the document.
type walker struct {
	ctx     context.Context
	spec    Spec
	minor   Minor
	view    *nodeview.View
	root    *yaml.Node
	work    budget.Budget
	stopped bool
	seen    func(Kind, *yaml.Node)
	// rows is the rows a Schema Object no schema holds is read with
	// (Options.Rows), rootRows those of a dialect root (Options.RootRows),
	// and later what is handed a schema left unread (Options.Later).
	rows     SchemaRules
	rootRows func(jsontext.Pointer) (SchemaRules, bool)
	later    func(Pending)
	reporter
}

// site is an object to read: its kind, node, pointer, the key its parent
// holds it under, and the kind of that parent, zero at the root. rows is the
// rows of the schema holding it, where one does.
type site struct {
	node *yaml.Node
	ptr  jsontext.Pointer
	held string
	rows SchemaRules
	kind Kind
	in   Kind
}

// frame is a mapping read as an object, how deep it is, and for a Schema
// Object the rows it is read with, which its object is. booleans reports that
// true and false stand as the schemas its fields hold, and where, unless
// empty, names what a finding about it holds it to in place of its minor.
type frame struct {
	obj      Object
	at       site
	pairs    []nodeview.Pair
	byKey    map[string]*yaml.Node
	rows     SchemaRules
	where    string
	depth    int
	booleans bool
}

// in names what a finding about f holds it to: the dialect a JSON Schema
// draft's rows read, else the minor m, or what where names.
func (f frame) in(m Minor) string {
	if f.where != "" {
		return f.where
	}
	return "OpenAPI " + m.String()
}

// spend charges one unit of work and reports whether the walk may go on. The
// first time the depth or the budget runs out it says so, an error as any
// crossed budget is, and stops reading.
func (w *walker) spend(depth int, ptr jsontext.Pointer) bool {
	if w.stopped {
		return false
	}
	if w.ctx.Err() != nil {
		w.stopped = true
		return false
	}
	if depth <= MaxDepth && w.work.Spend(1) {
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
	if !ok || node == nil {
		return
	}
	if s.kind == Schema && s.in != Schema && w.later != nil {
		w.later(Pending{at: s, depth: depth})
		return
	}
	if !w.spend(depth, s.ptr) {
		return
	}
	f := frame{obj: obj, at: s, depth: depth, booleans: w.spec.booleanHeld(Schema, w.minor)}
	if s.kind == Schema {
		f.rows = w.rowsAt(s)
		f.obj, f.booleans = f.rows.rows.object, f.rows.Booleans()
		if f.rows.rows.draft {
			f.where = "this schema's dialect"
		}
	}
	if node.Kind != yaml.MappingNode {
		w.notMapping(f, node)
		return
	}
	f = w.frameOf(f, node)
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

// rowsAt returns the rows the Schema Object s names is read with: a dialect
// root's own (Options.RootRows), else those of the schema holding it, else its
// document's (Options.Rows).
func (w *walker) rowsAt(s site) SchemaRules {
	if w.rootRows != nil {
		if rows, root := w.rootRows(w.rootAt + s.ptr); root && rows.rows != nil {
			return rows
		}
	}
	if s.in == Schema && s.rows.rows != nil {
		return s.rows
	}
	return w.rows
}

// frameOf reads node, a mapping, as the object f is.
func (w *walker) frameOf(f frame, node *yaml.Node) frame {
	f.pairs = w.view.MappingPairs(node)
	f.byKey = make(map[string]*yaml.Node, len(f.pairs))
	for _, p := range f.pairs {
		f.byKey[p.Key] = p.Val
	}
	return f
}

// notMapping reports a node that is no mapping where f's kind needs one,
// unless the kind admits a boolean there.
func (w *walker) notMapping(f frame, node *yaml.Node) {
	if f.obj.Boolean.Holds(w.minor) && matches(Boolean, node) {
		return
	}
	w.report(ir.SeverityError, classType, f.at.ptr, "%s must be an object in %s (%s)", f.obj.Name, f.in(w.minor), f.obj.Name)
}

// reference reads a mapping that writes $ref as a Reference Object where the
// object's kind admits one, and reports whether it did. Its target is checked
// where the target is written. A kind that admits the reference only from a
// later minor draws the later-version warning for the $ref, and one whose
// $ref takes siblings only from a later minor (RefSiblings) draws it for each
// key beside the $ref, which the declared minor ignores and the later one
// applies.
func (w *walker) reference(f frame, depth int) bool {
	if !f.refers() {
		return false
	}
	switch {
	case f.obj.Referenceable.Holds(w.minor):
	case f.obj.Referenceable.Later(w.minor):
		w.laterField(f.obj, "$ref", "", f.obj.Referenceable.Since, f.at.ptr.AppendToken("$ref"))
	case f.obj.RefSiblings.Later(w.minor):
		for _, p := range f.pairs {
			if p.Key != "$ref" {
				w.laterSibling(f.obj, p.Key, w.minor, f.obj.RefSiblings.Since, f.at.ptr.AppendToken(p.Key))
			}
		}
		return false
	case f.rows.IgnoresRefFields():
		for _, p := range f.pairs {
			if readsBesideRef(f.rows, p.Key) {
				w.ignoredBesideRef(p.Key, f.at.ptr.AppendToken(p.Key))
			}
		}
		return false
	default:
		return false
	}
	rf := f
	rf.obj = w.spec.must(Reference)
	w.fields(rf, depth)
	return true
}

// refers reports whether f writes a $ref, one a Schema Object writes null
// aside, which is read as left out.
func (f frame) refers() bool {
	ref, ok := f.byKey["$ref"]
	return ok && (f.obj.Kind != Schema || !isNull(nodeview.Deref(ref)))
}

// fields checks each field the object defines, and each a later minor, or for
// a Schema Object the dialect the compiler reads natively, added: warned of
// once, bar a key beside a 3.0 schema's $ref, which reference warned of, and
// read as that minor or dialect reads it.
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
		fld, ok := f.addedLater(p.Key, w.minor)
		if !ok {
			continue
		}
		ptr := f.at.ptr.AppendToken(p.Key)
		if f.rows.rows == nil {
			w.laterField(f.obj, p.Key, "", fld.Defined.Since, ptr)
			w.value(f, fld, p.Val, depth)
			continue
		}
		if !f.refers() || !f.obj.RefSiblings.Later(w.minor) {
			w.laterKeyword(f.rows, p.Key, f.writesTuple(), ptr)
		}
		w.value(f.asLater(fld, w.minor), fld, p.Val, depth)
	}
}

// addedLater finds the field named key that f's object does not define and a
// later minor's does, or for a Schema Object, the dialect the compiler reads
// natively (SchemaRules.Later).
func (f frame) addedLater(key string, m Minor) (Field, bool) {
	if f.rows.rows == nil {
		return f.obj.addedLater(key, m)
	}
	fld, ok := f.rows.rows.later[key]
	return fld, ok
}

// asLater returns f as it reads fld, a field the dialect the compiler reads
// natively added: true and false stand as schemas there, as from 3.1, and a
// finding holds it to that dialect's minor.
func (f frame) asLater(fld Field, m Minor) frame {
	f.booleans, f.where = true, "OpenAPI "+max(fld.Defined.Since, m).String()
	return f
}

// writesTuple reports whether the Schema Object f writes its dialect's own
// tuple, an `items` list its rows read as one, beside which a `prefixItems`
// is kept verbatim.
func (f frame) writesTuple() bool {
	items := nodeview.Deref(f.byKey["items"])
	return items != nil && items.Kind == yaml.SequenceNode && f.rows.Takes("items", List)
}

// addedLater finds the field of o named key that only a later minor than m
// defines, unless a JSON Schema keyword, which the schema lowering reads as it
// finds it.
func (o Object) addedLater(key string, m Minor) (Field, bool) {
	var found Field
	for _, fld := range o.Fields {
		if fld.Name != key {
			continue
		}
		if fld.Defined.Holds(m) {
			return Field{}, false
		}
		if fld.Defined.Later(m) && found.Name == "" {
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
	w.report(ir.SeverityError, classRequired, f.at.ptr, "%s field %q is REQUIRED in %s (%s)",
		f.obj.Name, fld.Name, f.in(w.minor), c.Section)
}

// patterned checks the keys the object admits beside its fixed fields: every
// key that is neither a field nor, where the object takes them, an extension.
func (w *walker) patterned(f frame, depth int) {
	pat, ok := f.obj.patternAt(w.minor)
	if !ok {
		return
	}
	for _, p := range f.pairs {
		if f.defines(p.Key) || (f.obj.Extensible && strings.HasPrefix(p.Key, "x-")) {
			continue
		}
		if pat.Keys != FormatNone && !validFormat(pat.Keys, p.Key) {
			w.report(ir.SeverityError, classFormat, f.at.ptr.AppendToken(p.Key),
				"%s has the key %q, which must be %s in %s (%s)",
				f.obj.Name, p.Key, describeFormat(pat.Keys), f.in(w.minor), citeFor(pat.Cites, w.minor).Section)
		}
		fld := Field{Name: p.Key, Shape: pat.Shape, Of: pat.Of, Defined: Always(), Cites: pat.Cites}
		w.value(f, fld, p.Val, depth)
	}
}

// patternAt returns the pattern of o that holds at minor m.
func (o Object) patternAt(m Minor) (Pattern, bool) {
	for _, p := range o.Patterned {
		if p.Span.Holds(m) {
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
