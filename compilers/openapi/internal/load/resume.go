package load

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// resumable reports whether settle may resume r, whose resolution holds c and
// failed with err, and the error r fails with where only the budget stops it.
// Only what mending cures is resumed: a hop that reached a stand-in for the
// source's object (see external.holdObject), or a pointer walked through a
// document's bytes (GitHub #761). A cycle is reported as the library reports
// it, so its message follows the walk's order (GitHub #767). The stalled hop
// read its own record when r is a schema, and the previous hop's otherwise; a
// schema is resumed only where its remaining hops end (see ends).
func (e external) resumable(r resolvable, c chain, err error) (bool, error) {
	if e.read.stoodIn(c) {
		if !e.work.spend(1) {
			return false, e.work.refusal()
		}
		return true, nil
	}
	if !errors.Is(err, jsonpointer.ErrInvalidPath) || c.cut || len(c.records) == 0 ||
		c.stopped == "" || c.stopped.GetURI() != "" {
		return false, nil
	}
	last := c.records[len(c.records)-1]
	_, schema := r.(*schemaRef)
	data, isBytes := (*last.document).([]byte)
	if !isBytes || schema == last.reached {
		return false, nil
	}
	tree := e.read.treeFor(last.path, data)
	if tree == nil {
		return false, nil
	}
	reads := 1
	if schema {
		reads = 2 // ends reads each hop before the library does
	}
	if !e.work.charge(tree, c.stopped, reads) {
		return false, e.work.refusal()
	}
	return !schema || ends(tree, c.stopped), nil
}

// ends reports whether the hops a schema chain has left from ref end in the
// document tree holds: each $ref followed to a node carrying none, or to no
// node, within maxResolutionHops. A cycle never ends.
//
// The library restarts its cycle check at each schema hop it resolved before,
// so a resumed chain leading back through one recurses until the stack
// overflows, which nothing recovers from. A hop this walk cannot read as the
// library does is no answer (see withinDocument): a chain leaving the document
// is not resumed, and fails as it would without settle.
func ends(tree *yaml.Node, ref references.Reference) bool {
	view := nodeview.New()
	return endsWithin(ref, func(ref references.Reference) (references.Reference, hopKind) {
		pointer, ok := withinDocument(ref)
		if !ok {
			return "", hopUnread
		}
		return readHop(view, tree, pointer)
	})
}

// hopKind is what reading one hop of a chain found.
type hopKind int

const (
	hopEnds    hopKind = iota // the target carries no $ref, or there is no target
	hopFollows                // the target carries a $ref, which the read returns
	hopUnread                 // the walk cannot read the hop as the resolver does
	// hopPending is hopEnds for now: the pointer passes a reference the walk
	// has not resolved yet, which the model reads as no target until it does.
	hopPending
)

// endsWithin follows the chain from ref, reading each hop with read, and
// reports whether it ends within maxResolutionHops. A cycle never ends, nor
// does a hop the walk cannot read.
func endsWithin(ref references.Reference, read func(references.Reference) (references.Reference, hopKind)) bool {
	for range maxResolutionHops {
		next, found := read(ref)
		switch found {
		case hopEnds, hopPending:
			return true
		case hopUnread:
			return false
		default:
			ref = next
		}
	}
	return false
}

// readHop reads the hop naming pointer in document with the call the resolver
// makes, so it finds the target the resolver finds, and returns the $ref the
// target carries.
func readHop(view *nodeview.View, document any, pointer string) (references.Reference, hopKind) {
	target, err := readTarget(document, pointer)
	if err != nil {
		return "", settledRead(err)
	}
	return hopTo(view, target)
}

// readTarget returns what document holds at pointer, read with the call the
// resolver makes.
func readTarget(document any, pointer string) (any, error) {
	return jsonpointer.GetTarget(document, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
}

// hopTo returns the $ref target, a hop's target, carries. A target that is no
// schema is one the resolver fails to cast, so the chain stops there.
func hopTo(view *nodeview.View, target any) (references.Reference, hopKind) {
	var next string
	switch t := target.(type) {
	case *yaml.Node:
		next = refOf(view, t)
	case *schemaRef:
		if t.IsReference() {
			next = string(t.GetRef())
		}
	default:
		// Not a schema, so it carries no $ref the resolver follows.
	}
	if next == "" {
		return "", hopEnds
	}
	return references.Reference(next), hopFollows
}

// settledRead is what a read that failed with err found: no target, unless the
// pointer passes a reference the walk has not resolved yet, which the model
// refuses to read through until it does, and which a later read passes.
func settledRead(err error) hopKind {
	if errors.Is(err, jsonpointer.ErrNotFound) || errors.Is(err, jsonpointer.ErrInvalidPath) ||
		errors.Is(err, jsonpointer.ErrValidation) {
		return hopEnds
	}
	return hopPending
}

// withinDocument returns the pointer ref names in the document holding it,
// decoded as the library decodes it, or false for a reference the library
// reads otherwise: one naming a document, which may be read for the first time
// or resolved through a $id, an anchor, and a "#/$defs/..." pointer, which it
// may read relative to the schema spelling it.
func withinDocument(ref references.Reference) (string, bool) {
	pointer := string(ref.GetJSONPointer())
	if ref.GetURI() != "" || !strings.HasPrefix(pointer, "/") || defs.IsPointer(jsontext.Pointer(pointer)) {
		return "", false
	}
	return pointer, true
}

// refOf returns the $ref node carries as the library reads it, through an
// alias or a merge key, or "" for none or one that is not a string.
func refOf(view *nodeview.View, node *yaml.Node) string {
	v := nodeview.Deref(view.ChildByToken(nodeview.Deref(node), "$ref"))
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// withoutStall returns c less the record of the hop it stalled at, which a
// schema hop holds of its own. That record stays as the library wrote it
// unless the hop is resumed: a later resolution can reach the hop through hops
// resolved before, across which the library checks for no cycle, and a mended
// record would let it take the chain up past the stall unchecked.
func (c chain) withoutStall() chain {
	if n := len(c.records); n > 0 && !c.records[n-1].reached {
		c.records = c.records[:n-1]
	}
	return c
}

// maxResumeWork bounds the steps settle takes in a resolution pass: one for
// each resolution it resumes, one for each hop the resumed resolution reads in
// a document's tree, the steps the library's read of that hop takes there (see
// treeReads), each time it is read, and the keys indexed to count them. The library finds a key by comparing a
// mapping's keys in order, so n chains into one mapping of n keys take n
// squared steps (GitHub #773).
const maxResumeWork = 1 << 28

// resumeWork is what settle has spent resuming resolutions, against limit, and
// the counter and view it reads the hops it charges through. Every copy of a
// reader shares one.
type resumeWork struct {
	spent, limit int
	reads        *treeReads
	view         *nodeview.View
}

// newResumeWork returns a budget of maxResumeWork steps with none spent.
func newResumeWork() *resumeWork {
	return &resumeWork{limit: maxResumeWork, reads: newTreeReads(), view: nodeview.New()}
}

// spend charges n steps, and reports whether they are within limit.
func (w *resumeWork) spend(n int) bool {
	w.spent += n
	return w.spent <= w.limit
}

// charge spends the steps of the hops a resumed resolution reads in tree from
// ref, before it reads them, and reports whether they are within limit. reads
// is how many times each hop is read. The hops are followed as the library
// follows them while they stay in the document, each once; a hop naming
// another document is resolved, and charged, as any reference to it is: not at
// all.
func (w *resumeWork) charge(tree *yaml.Node, ref references.Reference, reads int) bool {
	if !w.spend(1) {
		return false
	}
	read := map[references.Reference]bool{}
	for ref.GetURI() == "" && !read[ref] {
		read[ref] = true
		steps, target := w.reads.cost(tree, string(ref.GetJSONPointer()), (w.limit-w.spent)/reads)
		if !w.spend(1 + reads*steps + w.reads.drain()) {
			return false
		}
		next := refOf(w.view, target)
		if next == "" {
			return true
		}
		ref = references.Reference(next)
	}
	return true
}

// refusal returns the error a resolution settle stopped for the budget fails
// with.
func (w *resumeWork) refusal() error {
	return fmt.Errorf("resolving it further takes more than the %d steps budgeted "+
		"for chains through referenced documents", w.limit)
}

// exhausted reports the budget crossed, once, at the document: which
// reference crossed it follows the walk's order.
func (w *resumeWork) exhausted(at func(jsontext.Pointer) ir.Provenance) []ir.Diagnostic {
	if w.spent <= w.limit {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, at(""),
		"resolving reference chains through referenced documents takes more than %d steps; "+
			"the $refs past them are reported unresolved", w.limit)}
}
