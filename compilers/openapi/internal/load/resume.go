// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/references"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// resumable reports whether settle may resume r, whose resolution holds c and
// failed with err, and the error r fails with where only the budget stops it.
// Only what mending cures is resumed: a hop that reached a stand-in for the
// source's object (see external.holdObject), or a pointer walked through a
// document's bytes (GitHub #761), which the previous hop's record holds. A
// cycle is reported as the library reports it, so its message follows the
// walk's order (GitHub #767).
func (e external) resumable(c chain, err error) (bool, error) {
	if e.read.stoodIn(c) {
		if !e.work.steps.Spend(1) {
			return false, e.work.refusal()
		}
		return true, nil
	}
	if !errors.Is(err, jsonpointer.ErrInvalidPath) || c.cut || len(c.records) == 0 ||
		c.stopped == "" || c.stopped.GetURI() != "" {
		return false, nil
	}
	last := c.records[len(c.records)-1]
	data, isBytes := (*last.document).([]byte)
	if !isBytes {
		return false, nil
	}
	tree := e.read.treeFor(last.path, data)
	if tree == nil {
		return false, nil
	}
	if !e.work.charge(tree, c.stopped) {
		return false, e.work.refusal()
	}
	return true, nil
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

// maxResumeWork bounds the steps settle takes in a resolution pass: one for
// each resolution it resumes, one for each hop the resumed resolution reads in
// a document's tree, the steps the library's read of that hop takes there (see
// treeReads), and the keys indexed to count them. The library finds a key by
// comparing a mapping's keys in order, so n chains into one mapping of n keys
// take n squared steps (GitHub #773).
const maxResumeWork = 1 << 28

// resumeWork is the steps settle has spent resuming resolutions, against
// maxResumeWork, and the counter and view it reads the hops it charges
// through. Every copy of a reader shares one.
type resumeWork struct {
	steps budget.Budget
	reads *treeReads
	view  *nodeview.View
}

// newResumeWork returns a budget of maxResumeWork steps with none spent.
func newResumeWork() *resumeWork {
	return &resumeWork{steps: budget.New(budget.AtMost(maxResumeWork)), reads: newTreeReads(), view: nodeview.New()}
}

// charge spends the steps of the hops a resumed resolution reads in tree from
// ref, before it reads them, and reports whether they are within the budget.
// Each hop's read is priced no further than what is left. The hops are
// followed as the library follows them while they stay in the document, each
// once; a hop naming another document is resolved, and charged, as any
// reference to it is: not at all.
func (w *resumeWork) charge(tree *yaml.Node, ref references.Reference) bool {
	if !w.steps.Spend(1) {
		return false
	}
	read := map[references.Reference]bool{}
	for ref.GetURI() == "" && !read[ref] {
		read[ref] = true
		steps, target := w.reads.cost(tree, string(fragment.Pointer(string(ref))), w.steps.Remaining())
		if !w.steps.Spend(1 + steps + w.reads.drain()) {
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
		"for chains through referenced documents", w.steps.Limit().Cap())
}

// exhausted reports the budget crossed, once, at the document: which
// reference crossed it follows the walk's order.
func (w *resumeWork) exhausted(at func(jsontext.Pointer) ir.Provenance) []ir.Diagnostic {
	if !w.steps.Exhausted() {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, at(""),
		"resolving reference chains through referenced documents takes more than %d steps; "+
			"the $refs past them are reported unresolved", w.steps.Limit().Cap())}
}
