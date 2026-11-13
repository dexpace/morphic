// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	// v12 is Overlay 1.2, which the compiler reads as valid and does not apply
	// (GitHub #1836), so no action of it reaches the grafter.
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
// declares, and its actions in order, each selector read.
type plan struct {
	// declared is the version the document writes, as written.
	declared string
	version  version
	actions  []action
}

// action is one action of a plan.
type action struct {
	// index is the action's place among the document's actions, and of how
	// many it holds.
	index, of int
	kind      kind
	// target is the target selector as written, and targets its reading.
	target  string
	targets soaoverlay.Queryable
	// sources reads the copy selector, set for a copy.
	sources soaoverlay.Queryable
	// payload is the update value, set for an update.
	payload *yaml.Node
	// overlay is the action's place in the overlay document, which a finding
	// about the action names.
	overlay ir.Provenance
}

// label names a in a finding: its place, what it does and what it targets,
// quoted, since RFC 9535 lets a selector hold a line break.
func (a action) label() string {
	return fmt.Sprintf("overlay action %d of %d (%s %q)", a.index+1, a.of, a.kind, a.target)
}

// read parses data as an overlay document and reads it for applying, at, the
// overlay's place in Document.Sources. A document that is no overlay this
// compiler applies is refused, each reason an openapi/invalid-overlay error,
// and so is one writing a selector the compiler cannot apply as RFC 9535 reads
// it; what reads but has no effect is a warning.
//
// Every selector is read as RFC 9535, which both versions of the
// specification name, so the document is set to that dialect before any is
// read: the library picks one by version otherwise
// (docs/parser-library.md#overlay-jsonpath-dialect).
func read(at ir.Provenance, data []byte) (plan, []ir.Diagnostic) {
	doc, err := soaoverlay.ParseReader(bytes.NewReader(data))
	if err != nil {
		return plan{}, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayInvalid, at,
			"cannot parse overlay: %s", diag.OneLine(err))}
	}
	v, diags := readHead(at, doc)
	if diag.HasError(diags) {
		return plan{}, diags
	}
	p := plan{declared: doc.Version, version: v, actions: make([]action, 0, len(doc.Actions))}
	doc.JSONPathVersion = soaoverlay.JSONPathRFC9535
	for i, written := range doc.Actions {
		a, found := readAction(at, doc, v, i, written)
		diags = append(diags, found...)
		p.actions = append(p.actions, a)
	}
	return p, diags
}

// readHead holds doc's fixed fields to the specification and returns the
// version it declares: 1.0 or 1.1 at any patch, an info object naming a title
// and a version, an extends that parses as a URI reference, and at least one
// action. A 1.2 document is valid and refused as one the compiler does not
// apply yet; any other version is invalid.
func readHead(at ir.Provenance, doc *soaoverlay.Overlay) (version, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	invalid := func(format string, args ...any) {
		diags = append(diags, diag.Newf(ir.SeverityError, diag.OverlayInvalid, at, format, args...))
	}
	v, known := versionOf(doc.Version)
	switch {
	case doc.Version == "":
		invalid("invalid overlay: it declares no overlay version")
	case !known:
		invalid("overlay version %q is not one the compiler applies, which are 1.0.x and 1.1.x", doc.Version)
	case v == v12:
		diags = append(diags, diag.Newf(ir.SeverityError, diag.OverlayUnsupported, at,
			"overlay version %q is Overlay 1.2, which the compiler does not apply yet (GitHub #1836); "+
				"it applies 1.0.x and 1.1.x", doc.Version))
	}
	if doc.Info.Title == "" {
		invalid("invalid overlay: the info object names no title")
	}
	if doc.Info.Version == "" {
		invalid("invalid overlay: the info object names no version")
	}
	if _, err := url.Parse(doc.Extends); err != nil {
		invalid("invalid overlay: extends %q is no URI reference", doc.Extends)
	}
	if len(doc.Actions) == 0 {
		invalid("invalid overlay: it holds no action")
	}
	return v, diags
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

// readAction reads the action written at index i of doc, which declares v:
// its kind, its selectors and what it writes. A field the specification gives
// no effect beside the kind the action takes is a warning, a copy in an
// Overlay 1.0 document, which has no such field, is refused, and so is an
// Overlay 1.1 action setting both update and copy, to each of which the
// specification gives no effect beside the other, leaving the action no
// meaning.
func readAction(at ir.Provenance, doc *soaoverlay.Overlay, v version, i int, written soaoverlay.Action,
) (action, []ir.Diagnostic) {
	a := action{index: i, of: len(doc.Actions), target: written.Target, kind: kindOf(written)}
	a.overlay = ir.Provenance{Source: at.Source, Pointer: jsontext.Pointer("/actions/" + strconv.Itoa(i))}
	if a.kind == kindUpdate {
		a.payload = &doc.Actions[i].Update
	}
	if written.Target == "" {
		return a, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayInvalid, a.overlay,
			"invalid overlay: action %d of %d names no target", i+1, a.of)}
	}
	var diags []ir.Diagnostic
	switch {
	case written.Copy != "" && v == v10:
		diags = append(diags, diag.Newf(ir.SeverityError, diag.OverlayInvalid, a.overlay,
			"%s sets copy, which Overlay 1.1 introduced; this overlay declares %s", a.label(), doc.Version))
	case a.kind == kindUpdate && written.Copy != "":
		diags = append(diags, diag.Newf(ir.SeverityError, diag.OverlayFailed, a.overlay,
			"%s sets both update and copy, and the specification gives each no effect beside the other; "+
				"write one of them", a.label()))
	}
	var found []ir.Diagnostic
	a.targets, found = readSelector(doc, a, "target", written.Target)
	diags = append(diags, found...)
	if written.Copy != "" {
		a.sources, found = readSelector(doc, a, "copy", written.Copy)
		diags = append(diags, found...)
	}
	return a, append(diags, idleFields(a, written)...)
}

// kindOf returns the kind an action takes: remove, then update, then copy.
func kindOf(written soaoverlay.Action) kind {
	switch {
	case written.Remove:
		return kindRemove
	case !written.Update.IsZero():
		return kindUpdate
	case written.Copy != "":
		return kindCopy
	default:
		return kindNone
	}
}

// idleFields warns of each field a writes that has no effect: an update or a
// copy beside a remove, and the action as a whole when it sets none.
func idleFields(a action, written soaoverlay.Action) []ir.Diagnostic {
	var diags []ir.Diagnostic
	warn := func(format string, args ...any) {
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay, format, args...))
	}
	if a.kind == kindNone {
		warn("%s sets none of update, remove and copy, so it changes nothing", a.label())
	}
	if a.kind == kindRemove && !written.Update.IsZero() {
		warn("%s also sets update, which has no effect beside remove", a.label())
	}
	if a.kind == kindRemove && written.Copy != "" {
		warn("%s also sets copy, which has no effect beside remove", a.label())
	}
	return diags
}
