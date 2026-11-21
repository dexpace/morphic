// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
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
	// Skip, when set, is asked of each mapping about to be read as an object
	// of a kind, its aliases followed; one it answers true for is not read, so
	// a caller handing it what Seen recorded reads each (node, kind) once.
	Skip func(kind Kind, node *yaml.Node) bool
	// Judged, when set, is told each value the check refuses as the wrong type
	// where it is written (a value with none of the shapes its field admits, or
	// no mapping where an object belongs; never a null), with refused true, and
	// each value it accepts inside a frame an alias, an anchor or a `<<` merge
	// shares, with refused false. The node is the value, its alias followed.
	Judged func(node *yaml.Node, refused bool)
	// Referenced, when set, is told each mapping read as a Reference Object
	// standing in the place of an object of kind, its aliases followed.
	Referenced func(kind Kind, node *yaml.Node)
	// Shared, when set, reads the root as an object other places may read as
	// other kinds, so Judged is told every value the check accepts, not only
	// those an alias, an anchor or a `<<` merge shares.
	Shared bool
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
	at    objectToCheck
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
	walker, ok := c.walker(ctx, declared, opts)
	if !ok {
		return nil
	}
	start := opts.Start
	if start == 0 {
		start = Document
	}
	walker.root = nodeview.DocumentRoot(root)
	walker.object(objectToCheck{kind: start, node: walker.root, shared: opts.Shared}, 0)
	return walker.diags
}

// CheckPending judges each Schema Object a check handed back unread
// (Options.Later), in the order given, as that check would have read it, with
// opts' rows. One walk reads them all, so a node two of them mount draws one
// finding and one budget bounds them.
func (c Checker) CheckPending(ctx context.Context, pending []Pending, declared string, opts Options) []ir.Diagnostic {
	opts.Later = nil
	walker, ok := c.walker(ctx, declared, opts)
	if !ok {
		return nil
	}
	for _, pendingValue := range pending {
		walker.object(pendingValue.at, pendingValue.depth)
	}
	return walker.diags
}

// walker starts a check of a document declaring declared under opts, and is
// false for a version the table does not cover.
func (c Checker) walker(ctx context.Context, declared string, opts Options) (*walker, bool) {
	minor, ok := ParseMinor(declared)
	if !ok {
		return nil, false
	}
	walkerValue := &walker{ctx: ctx, spec: c.spec, minor: minor, view: nodeview.New(), work: budget.New(opts.Budget)}
	walkerValue.locate, walkerValue.declared, walkerValue.seen, walkerValue.rows = opts.Locate, declared, opts.Seen, opts.Rows
	walkerValue.readAt, walkerValue.rootAt, walkerValue.said = opts.ReadAt, opts.Root, map[ir.DiagnosticKey]saying{}
	walkerValue.rootRows, walkerValue.later, walkerValue.judged, walkerValue.referenced = opts.RootRows, opts.Later, opts.Judged, opts.Referenced
	walkerValue.skip = opts.Skip
	if walkerValue.rows.rows == nil {
		walkerValue.rows, _ = c.spec.SchemaRules(declared, "")
	}
	if walkerValue.locate == nil {
		walkerValue.locate = func(jsontext.Pointer) ir.Provenance { return ir.Provenance{} }
	}
	return walkerValue, true
}

// walker is one check in progress. It holds the context the check runs under
// for spend, which every read of an object asks first: a check stops reading
// once its context is done, as it stops at its budget, though silently, since
// what stopped it is no finding about the document.
type walker struct {
	ctx        context.Context
	spec       Spec
	minor      Minor
	view       *nodeview.View
	root       *yaml.Node
	work       budget.Budget
	stopped    bool
	seen       func(Kind, *yaml.Node)
	judged     func(*yaml.Node, bool)
	referenced func(Kind, *yaml.Node)
	skip       func(Kind, *yaml.Node) bool
	// rows is the rows a Schema Object no schema holds is read with
	// (Options.Rows), rootRows those of a dialect root (Options.RootRows),
	// and later what is handed a schema left unread (Options.Later).
	rows     SchemaRules
	rootRows func(jsontext.Pointer) (SchemaRules, bool)
	later    func(Pending)
	// raw is the steps from the root of the raw value being read down to the
	// node read (rawRepeats).
	raw []rawStep
	reporter
}

// objectToCheck is an object to read: its kind, node, pointer, the key its
// parent holds it under, and the kind of that parent, zero at the root. rows is
// the rows of the schema holding it, where one does, and shared says its
// parent's frame is shared (frame.shared).
type objectToCheck struct {
	node   *yaml.Node
	ptr    jsontext.Pointer
	held   string
	rows   SchemaRules
	kind   Kind
	in     Kind
	shared bool
}

// frame is a mapping read as an object, how deep it is, and for a Schema
// Object the rows it is read with, which its object is. booleans reports that
// true and false stand as the schemas its fields hold: by its rows for a
// Schema Object, which decides for those it holds, else by its minor, where
// the rows a schema is read with have the last word (shapeOf). where,
// unless empty, names what a finding about it holds it to in place of its
// minor.
type frame struct {
	obj      Object
	at       objectToCheck
	pairs    []nodeview.Pair
	byKey    map[string]*yaml.Node
	rows     SchemaRules
	where    string
	depth    int
	booleans bool
	// shared says another place may read what f holds: its parent's frame is
	// shared, or its node is an alias, anchored, or writes a `<<` key.
	shared bool
}

// sharedNode reports whether another place may read node: it is an alias, or
// the node it names is anchored.
func sharedNode(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	return node.Kind == yaml.AliasNode || nodeview.Deref(node).Anchor != ""
}

// writesMerge reports whether the mapping node writes a `<<` key, whose sources
// another mapping may merge too.
func writesMerge(node *yaml.Node) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if nodeview.IsMergeKey(node.Content[i]) {
			return true
		}
	}
	return false
}

// refuse tells Options.Judged that node, the value a field holds, is refused
// as the wrong type where it is written, unless it is a null, which every
// reader already takes for the field left out.
func (w *walker) refuse(node *yaml.Node) {
	if w.judged != nil && node != nil && !isNull(node) {
		w.judged(node, true)
	}
}

// accept tells Options.Judged that node, its alias followed, is accepted, when
// another place may read it too: a place that reads a node as well-typed keeps
// it as written whatever another refuses.
func (w *walker) accept(shared bool, node *yaml.Node) {
	if w.judged != nil && shared && node != nil {
		w.judged(node, false)
	}
}

// in names what a finding about f holds it to: the dialect a JSON Schema
// draft's rows read, else the minor minor, or what where names.
func (f frame) in(minor Minor) string {
	if f.where != "" {
		return f.where
	}
	return "OpenAPI " + minor.String()
}

// spend charges one unit of work and reports whether the walk may go on. The
// first time the depth or the budget runs out it says so, an error as any
// crossed budget is, and stops reading.
func (w *walker) spend(depth int, ptr jsontext.Pointer) bool {
	ok, stops := w.charge(depth)
	if stops {
		reportStop(w, ptr)
	}
	return ok
}

// charge charges one unit of work at depth and reports whether the walk may
// go on, and whether this charge is the one the depth or the budget runs out
// at, which its caller reports (reportStop): a walk that spells no pointer a
// node asks it, and spells the one it stops at.
func (w *walker) charge(depth int) (ok, stops bool) {
	if w.stopped {
		return false, false
	}
	if w.ctx.Err() != nil {
		w.stopped = true
		return false, false
	}
	if depth <= MaxDepth && w.work.Spend(1) {
		return true, false
	}
	w.stopped = true
	return false, true
}

// reportStop reports that walkerValue's check stopped at ptr.
func reportStop(walkerValue *walker, ptr jsontext.Pointer) {
	walkerValue.diags = append(walkerValue.diags, diag.Newf(ir.SeverityError, diag.BudgetExceeded, walkerValue.locate(ptr),
		"the OpenAPI %s check stopped at this depth or work budget; what lies below is unchecked", walkerValue.minor))
}

// object checks the mapping objectToCheckValue holds as an object of its kind,
// and what lies below it.
func (w *walker) object(objectToCheckValue objectToCheck, depth int) {
	obj, ok := w.spec.Object(objectToCheckValue.kind)
	node := nodeview.Deref(objectToCheckValue.node)
	if !ok || node == nil {
		return
	}
	if objectToCheckValue.kind == Schema && objectToCheckValue.in != Schema && w.later != nil {
		w.later(Pending{at: objectToCheckValue, depth: depth})
		return
	}
	if w.skip != nil && node.Kind == yaml.MappingNode && w.skip(objectToCheckValue.kind, node) {
		return
	}
	if !w.spend(depth, objectToCheckValue.ptr) {
		return
	}
	frameValue := frame{obj: obj, at: objectToCheckValue, depth: depth, booleans: w.spec.booleanHeld(Schema, w.minor),
		shared: objectToCheckValue.shared || sharedNode(objectToCheckValue.node)}
	if objectToCheckValue.kind == Schema {
		frameValue.rows = w.rowsAt(objectToCheckValue)
		frameValue.obj, frameValue.booleans = frameValue.rows.rows.object, frameValue.rows.Booleans()
		if frameValue.rows.rows.draft {
			frameValue.where = "this schema's dialect"
		}
	}
	if node.Kind != yaml.MappingNode {
		w.notMapping(frameValue, node)
		return
	}
	frameValue = frameOf(frameValue, node, w.view)
	frameValue.shared = frameValue.shared || writesMerge(node)
	w.accept(frameValue.shared, node)
	w.repeated(node, objectToCheckValue.ptr, obj.Name)
	if w.reference(frameValue, depth) {
		return
	}
	if w.seen != nil {
		w.seen(objectToCheckValue.kind, node)
	}
	w.fields(frameValue, depth)
	w.patterned(frameValue, depth)
	w.unknown(frameValue)
	rawKeys(w, frameValue, depth)
	w.rules(frameValue)
}

// rawKeys reads for repeated keys the value of each key frameValue's object
// reads as no field and the lowering keeps verbatim: an extension, and a key
// the object defines at no minor, which unknown reports. An object taking
// patterned keys reads each other key as an entry, as a schema reads a keyword
// its dialect added later as a field.
func rawKeys(walkerValue *walker, frameValue frame, depth int) {
	_, patterned := frameValue.obj.patternAt(walkerValue.minor)
	for _, pair := range frameValue.pairs {
		extension := strings.HasPrefix(pair.Key, "x-") && frameValue.obj.Extensible
		if !extension && (patterned || frameValue.defines(pair.Key)) {
			continue
		}
		if _, later := frameValue.addedLater(pair.Key, walkerValue.minor); later {
			continue
		}
		rawRepeats(walkerValue, pair.Val, frameValue.at.ptr.AppendToken(pair.Key), depth+1)
	}
}

// unknown reports each key frameValue's mapping writes that its object neither
// defines at any minor nor admits: no field, no patterned key, no x- extension.
// A Schema Object's keywords are its dialect's, and an object taking patterned
// keys at the declared minor admits every key, so neither is judged here. A key
// merged through `<<` is placed where it is written, as every field is.
func (w *walker) unknown(frameValue frame) {
	if frameValue.rows.rows != nil {
		return
	}
	if _, patterned := frameValue.obj.patternAt(w.minor); patterned {
		return
	}
	places := newKeyPlacer(w, frameValue.at.node, frameValue.at.ptr)
	for _, pair := range frameValue.pairs {
		if frameValue.defines(pair.Key) || (frameValue.obj.Extensible && strings.HasPrefix(pair.Key, "x-")) {
			continue
		}
		message := fmt.Sprintf("key %q is not defined by the %s it is written on and is not an x- extension",
			pair.Key, frameValue.obj.Name)
		if frameValue.obj.Kind == Reference {
			message = fmt.Sprintf("key %q is written beside a $ref, where the Reference Object defines only $ref, "+
				"summary and description, so OpenAPI ignores it", pair.Key)
		}
		w.add(diag.Newf(ir.SeverityWarning, diag.UnknownObjectKey, places.place(pair.Key), "%s", message),
			frameValue.at.ptr.AppendToken(pair.Key))
	}
}

// rowsAt returns the rows the Schema Object objectToCheckValue names is read
// with: a dialect root's own (Options.RootRows), else those of the schema
// holding it, else its document's (Options.Rows).
func (w *walker) rowsAt(objectToCheckValue objectToCheck) SchemaRules {
	if w.rootRows != nil {
		if rows, root := w.rootRows(w.rootAt + objectToCheckValue.ptr); root && rows.rows != nil {
			return rows
		}
	}
	if objectToCheckValue.in == Schema && objectToCheckValue.rows.rows != nil {
		return objectToCheckValue.rows
	}
	return w.rows
}

// frameOf reads node, a mapping, as the object frameValue is.
func frameOf(frameValue frame, node *yaml.Node, view *nodeview.View) frame {
	frameValue.pairs = view.MappingPairs(node)
	frameValue.byKey = make(map[string]*yaml.Node, len(frameValue.pairs))
	for _, pair := range frameValue.pairs {
		frameValue.byKey[pair.Key] = pair.Val
	}
	return frameValue
}

// notMapping reports a node that is no mapping where frameValue's kind needs
// one, unless the kind admits a boolean there, and tells Options.Judged which.
func (w *walker) notMapping(frameValue frame, node *yaml.Node) {
	if frameValue.obj.Boolean.Holds(w.minor) && matches(Boolean, node) {
		w.accept(frameValue.shared, node)
		return
	}
	w.refuse(node)
	w.report(ir.SeverityError, classType, frameValue.at.ptr, "%s must be an object in %s (%s)", frameValue.obj.Name, frameValue.in(w.minor), frameValue.obj.Name)
}

// reference reads a mapping that writes $ref as a Reference Object where the
// object's kind admits one, and reports whether it did. Its target is checked
// where the target is written. A kind that admits the reference only from a
// later minor draws the later-version warning for the $ref, and one whose
// $ref takes siblings only from a later minor (RefSiblings) draws it for each
// key beside the $ref, which the declared minor ignores and the later one
// applies. Options.Referenced is told of each it reads.
func (w *walker) reference(frameValue frame, depth int) bool {
	if !frameValue.refers() {
		return false
	}
	switch {
	case frameValue.obj.Referenceable.Holds(w.minor):
	case frameValue.obj.Referenceable.Later(w.minor):
		w.laterField(frameValue.obj, "$ref", "", frameValue.obj.Referenceable.Since, frameValue.at.ptr.AppendToken("$ref"))
	case frameValue.obj.RefSiblings.Later(w.minor):
		for _, pair := range frameValue.pairs {
			if pair.Key != "$ref" {
				w.laterSibling(frameValue.obj, pair.Key, w.minor, frameValue.obj.RefSiblings.Since, frameValue.at.ptr.AppendToken(pair.Key))
			}
		}
		return false
	case frameValue.rows.IgnoresRefFields():
		for _, pair := range frameValue.pairs {
			if readsBesideRef(frameValue.rows, pair.Key) {
				w.ignoredBesideRef(pair.Key, frameValue.at.ptr.AppendToken(pair.Key))
			}
		}
		return false
	default:
		return false
	}
	rf := frameValue
	rf.obj = w.spec.must(Reference)
	w.fields(rf, depth)
	w.unknown(rf)
	rawKeys(w, rf, depth)
	if w.referenced != nil {
		w.referenced(frameValue.at.kind, nodeview.Deref(frameValue.at.node))
	}
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
func (w *walker) fields(frameValue frame, depth int) {
	for _, fld := range frameValue.obj.Fields {
		if !fld.Defined.Holds(w.minor) {
			continue
		}
		val, present := frameValue.byKey[fld.Name]
		if !present {
			w.missing(frameValue, fld)
			continue
		}
		w.value(frameValue, fld, val, depth)
	}
	for _, pair := range frameValue.pairs {
		fld, ok := frameValue.addedLater(pair.Key, w.minor)
		if !ok {
			continue
		}
		ptr := frameValue.at.ptr.AppendToken(pair.Key)
		if frameValue.rows.rows == nil {
			w.laterField(frameValue.obj, pair.Key, "", fld.Defined.Since, ptr)
			w.value(frameValue, fld, pair.Val, depth)
			continue
		}
		if !frameValue.refers() || !frameValue.obj.RefSiblings.Later(w.minor) {
			w.laterKeyword(frameValue.rows, pair.Key, frameValue.writesTuple(), ptr)
		}
		w.value(frameValue.asLater(fld, w.minor), fld, pair.Val, depth)
	}
}

// addedLater finds the field named key that f's object does not define and a
// later minor's does, or for a Schema Object, the dialect the compiler reads
// natively (SchemaRules.Later).
func (f frame) addedLater(key string, minor Minor) (Field, bool) {
	if f.rows.rows == nil {
		return f.obj.addedLater(key, minor)
	}
	fld, ok := f.rows.rows.later[key]
	return fld, ok
}

// asLater returns f as it reads fld, a field the dialect the compiler reads
// natively added: true and false stand as schemas there, as from 3.1, and a
// finding holds it to that dialect's minor. Only fld's row is that dialect's:
// the rules run at the declared minor, so at 3.0 none 3.1 states over the
// field holds, such as prefixItems holding an entry.
func (f frame) asLater(fld Field, minor Minor) frame {
	f.booleans, f.where = true, "OpenAPI "+max(fld.Defined.Since, minor).String()
	return f
}

// writesTuple reports whether the Schema Object f writes its dialect's own
// tuple, an `items` list its rows read as one, beside which a `prefixItems`
// is kept verbatim.
func (f frame) writesTuple() bool {
	items := nodeview.Deref(f.byKey["items"])
	return items != nil && items.Kind == yaml.SequenceNode && f.rows.Takes("items", List)
}

// addedLater finds the field of o named key that only a later minor than minor
// defines, unless a JSON Schema keyword, which the schema lowering reads as it
// finds it.
func (o Object) addedLater(key string, minor Minor) (Field, bool) {
	var found Field
	for _, fld := range o.Fields {
		if fld.Name != key {
			continue
		}
		if fld.Defined.Holds(minor) {
			return Field{}, false
		}
		if fld.Defined.Later(minor) && found.Name == "" {
			found = fld
		}
	}
	return found, found.Name != "" && !found.IsJSONSchemaKeyword
}

// missing reports a required field the object does not write.
func (w *walker) missing(frameValue frame, fld Field) {
	if !fld.Required.Holds(w.minor) || fld.Owner != "" {
		return
	}
	cite := citeFor(fld.Cites, w.minor)
	w.report(ir.SeverityError, classRequired, frameValue.at.ptr, "%s field %q is REQUIRED in %s (%s)",
		frameValue.obj.Name, fld.Name, frameValue.in(w.minor), cite.Section)
}

// patterned checks the keys the object admits beside its fixed fields: every
// key that is neither a field nor, where the object takes them, an extension.
func (w *walker) patterned(frameValue frame, depth int) {
	pat, ok := frameValue.obj.patternAt(w.minor)
	if !ok {
		return
	}
	for _, pair := range frameValue.pairs {
		if frameValue.defines(pair.Key) || (frameValue.obj.Extensible && strings.HasPrefix(pair.Key, "x-")) {
			continue
		}
		if pat.Keys != FormatNone && !ValidFormat(pat.Keys, pair.Key) {
			w.report(ir.SeverityError, classFormat, frameValue.at.ptr.AppendToken(pair.Key),
				"%s has the key %q, which must be %s in %s (%s)",
				frameValue.obj.Name, pair.Key, describeFormat(pat.Keys), frameValue.in(w.minor), citeFor(pat.Cites, w.minor).Section)
		}
		fld := Field{Name: pair.Key, Shape: pat.Shape, Of: pat.Of, Defined: Always(), Cites: pat.Cites}
		w.value(frameValue, fld, pair.Val, depth)
	}
}

// patternAt returns the pattern of o that holds at minor minor.
func (o Object) patternAt(minor Minor) (Pattern, bool) {
	for _, pattern := range o.Patterned {
		if pattern.Span.Holds(minor) {
			return pattern, true
		}
	}
	return Pattern{}, false
}

// fieldAt returns the field of o named name that minor minor defines, else the
// first so named, which a later minor added, else the zero Field, whose
// AnyShapeUnchecked shape admits every value.
func (o Object) fieldAt(name string, minor Minor) Field {
	var found Field
	for _, fld := range o.Fields {
		if fld.Name != name {
			continue
		}
		if fld.Defined.Holds(minor) {
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

// citeFor picks the citation to name for a finding at minor minor: the latest
// one the minor has reached, else the first.
func citeFor(cites []Cite, minor Minor) Cite {
	var best Cite
	for _, cite := range cites {
		if cite.Minor <= minor && cite.Minor >= best.Minor {
			best = cite
		}
	}
	if best.Minor == 0 && len(cites) > 0 {
		return cites[0]
	}
	return best
}
