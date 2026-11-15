// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// version is the minor of the Overlay Specification an overlay declares,
// which decides how its actions apply. The specification asks tooling not to
// tell patch versions apart.
type version uint8

const (
	// v10 is Overlay 1.0, whose targets select objects and arrays.
	v10 version = iota
	// v11 is Overlay 1.1: a target may select primitives, the nodes an update
	// or a copy selects are all of one kind, and an action may copy a node of
	// the document.
	v11
	// v12 is Overlay 1.2, which applies by 1.1's rules and adds reusable
	// actions and $self.
	v12
)

// kind is what an action does, the first of remove, update and copy it sets,
// which is the precedence the specification's "has no impact" sentences give.
type kind uint8

const (
	// kindNone sets no modifier, so the action changes nothing.
	kindNone kind = iota
	// kindRemove deletes each selected node from what holds it.
	kindRemove
	// kindUpdate merges the action's update value into each selected node.
	kindUpdate
	// kindCopy merges the one node the action's copy selector selects into
	// each selected node.
	kindCopy
)

// String names k as the specification's field does.
func (k kind) String() string {
	switch k {
	case kindRemove:
		return "remove"
	case kindUpdate:
		return "update"
	case kindCopy:
		return "copy"
	default:
		return "no modifier"
	}
}

// plan is an overlay document read for applying: the rules of the version it
// declares, its own URI, and its actions in order, each selector read.
type plan struct {
	// at is the overlay's place in Document.Sources, which a finding about it
	// as a whole names.
	at ir.Provenance
	// declared is the version the document writes, as written, and selfURI
	// its $self, as written.
	declared, selfURI string
	version           version
	actions           []action
}

// action is one action of a plan.
type action struct {
	// targets reads the target selector, and sources the copy selector, set
	// for a copy.
	targets, sources jsonpath.Query
	// payload is the update value, set for an update.
	payload *yaml.Node
	// target is the target selector as written.
	target string
	// overlay is the action's place in the overlay document, which a finding
	// about the action names.
	overlay ir.Provenance
	// index is the action's place among the document's actions, and of how
	// many it holds.
	index, of int
	kind      kind
}

// label names a in a finding: its place, what it does and what it targets,
// quoted, since RFC 9535 lets a selector hold a line break.
func (a action) label() string {
	return fmt.Sprintf("overlay action %d of %d (%s %q)", a.index+1, a.of, a.kind, a.target)
}

// read reads the overlay document doc, a document node or one of no kind for
// a stream holding none, by the fields its version's objects define (fields.go)
// for applying, at, the overlay's place in Document.Sources. A document that
// is no overlay this compiler applies is refused, each reason an
// openapi/invalid-overlay error at the field it is about, and nothing of it
// applies; what reads but has no effect is a warning.
func read(at ir.Provenance, doc *yaml.Node) (plan, []ir.Diagnostic) {
	r := &reader{at: at, view: nodeview.New()}
	p := r.overlay(nodeview.Deref(nodeview.DocumentRoot(doc)))
	if diag.HasError(r.diags) {
		var refused []ir.Diagnostic
		for _, d := range r.diags {
			if d.Severity == ir.SeverityError {
				refused = append(refused, d)
			}
		}
		return plan{}, refused
	}
	return p, r.diags
}

// versionOf reads a declared version as the minor it names: 1.0, 1.1 or 1.2,
// and a patch of digits, as the specification's own schemas spell it.
func versionOf(declared string) (version, bool) {
	rest, isOne := strings.CutPrefix(declared, "1.")
	minor, patch, cut := strings.Cut(rest, ".")
	if !isOne || !cut || patch == "" || strings.Trim(patch, "0123456789") != "" {
		return 0, false
	}
	switch minor {
	case "0":
		return v10, true
	case "1":
		return v11, true
	case "2":
		return v12, true
	default:
		return 0, false
	}
}

// kindOf returns the kind an action whose fields are written takes: remove,
// then update, then copy.
func kindOf(written map[string]*yaml.Node) kind {
	switch {
	case written["remove"] != nil && isTrue(written["remove"]):
		return kindRemove
	case written["update"] != nil:
		return kindUpdate
	case written["copy"] != nil:
		return kindCopy
	default:
		return kindNone
	}
}

// idleFields warns of each field a writes that has no effect: an update or a
// copy beside a remove, and the action as a whole when it sets none.
func idleFields(a action, written map[string]*yaml.Node) []ir.Diagnostic {
	var diags []ir.Diagnostic
	warn := func(format string, args ...any) {
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay, format, args...))
	}
	if a.kind == kindNone {
		warn("%s sets none of update, remove and copy, so it changes nothing", a.label())
	}
	if a.kind == kindRemove && written["update"] != nil {
		warn("%s also sets update, which has no effect beside remove", a.label())
	}
	if a.kind == kindRemove && written["copy"] != nil {
		warn("%s also sets copy, which has no effect beside remove", a.label())
	}
	return diags
}
