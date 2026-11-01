// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// dynamicExpansion resolves the $dynamicRef s writes at pointer to the type it
// names, or reports why it is irreducible. Expansion and preservation both
// decide through it.
//
// Dynamic scope is static per document (docs/compilers/openapi.md#dynamic-ref),
// so a $dynamicAnchor declared once is the only match, provided the document is
// one schema resource: an $id on the path to either end makes the reference
// irreducible (dynamicChainVerdict).
//
// Only an anchor on a top-level component schema resolves, since only its
// TypeID is stable whatever lowers first
// (docs/compilers/openapi.md#dynamic-ref). A $dynamicRef co-declared with a
// $ref, oneOf/anyOf or a shape is irreducible too, as is one another document
// holds.
func dynamicExpansion(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (target ir.TypeID, why string, ok bool, diags []ir.Diagnostic) {
	name, why, ok := dynamicRefName(s)
	if !ok {
		return "", why, false, nil
	}
	if c.RefScope().Foreign {
		// Its fragment names that document's anchor, and the index holds the
		// source's alone: expanding it read the source's anchor of that name
		// (GitHub #762). Indexing each document's is GitHub #1031.
		return "", "it is written in another document, whose $dynamicAnchor it names, and only the source's " +
			"anchors are indexed", false, nil
	}
	at, why, ok, diags := soleAnchorSite(c, name)
	if !ok {
		return "", why, false, diags
	}
	id, resolved, handled := c.RefScope().ComponentRef(at)
	if !handled || !resolved {
		return "", unnamedAnchorSiteWhy(name, at), false, diags
	}
	chainWhy, chainOK, chainDiags := dynamicChainVerdict(c, at, pointer)
	diags = append(diags, chainDiags...)
	if !chainOK {
		return "", chainWhy, false, diags
	}
	return id, "", true, diags
}

// unnamedAnchorSiteWhy words why the position declaring an anchor is not a
// target the IR can name. Two shapes reach it: a pointer deeper than a
// top-level component schema, which has no stable TypeID, and a component
// schema keyed "", which earns none because an empty name is not a name. The
// second must not be worded "rather than on a component schema", which the
// document contradicts. The verdict is the same; only the reason differs.
func unnamedAnchorSiteWhy(name string, at jsontext.Pointer) string {
	if ids.ComponentSchemaNamedEmpty(at) {
		return fmt.Sprintf(`$dynamicAnchor %q is declared on the component schema keyed "" at %q, `+
			"and an empty name earns no named type to expand to", name, at)
	}
	return fmt.Sprintf("$dynamicAnchor %q is declared at %q rather than on a component schema", name, at)
}

// dynamicRefName returns the $dynamicAnchor name the $dynamicRef s writes
// addresses, or the reason it addresses none. It stops short of the index, so
// the chain walk and the lowering path ask one function what a schema requests.
func dynamicRefName(s *oas3.Schema) (name, why string, ok bool) {
	node := annotation.RawPropertyNode(s, "$dynamicRef")
	if node == nil {
		return "", "no $dynamicRef is written here", false
	}
	if dynamicRefSiblings(s) {
		return "", "it is co-declared with another applicator, which the IR cannot intersect with it", false
	}
	if node.Kind != yaml.ScalarNode {
		return "", "its value is not a reference string", false
	}
	return dynamicFragment(node.Value)
}

// dynamicRefSiblings reports whether s writes anything beside its $dynamicRef
// that the expansion would have to intersect with. JSON Schema conjoins
// keywords and the IR has no node that intersects a reference with a shape, so
// any of these makes the reference irreducible; expanding regardless would
// assert the target's shape and drop the sibling in silence.
//
// It is broader than declaresShape by narrowsInstance's keywords, for the reason
// that predicate records: an expansion takes the target whole, so a keyword it
// cannot carry is lost whether or not it declares a shape by itself.
func dynamicRefSiblings(s *oas3.Schema) bool {
	if s.Ref != nil || len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0 {
		return true
	}
	return narrowsInstance(s) || declaresShape(s)
}

// dynamicFragment returns the anchor name a $dynamicRef addresses, or the
// reason it addresses none. Only the same-document `#name` spelling resolves; a
// URI part or a `#/…` pointer addresses no anchor.
//
// The name is the fragment percent-decoded once (RFC 3986): `#my%2Danchor` and
// `#my-anchor` name one anchor, and `#my%252Danchor` names `my%2Danchor`
// (GitHub #233). PathUnescape keeps `+` literal, as a fragment needs, and a
// fragment it cannot decode is refused, not matched raw.
//
// The $dynamicAnchor side is not decoded: JSON Schema §8.2.2 anchors admit no
// `%`, so decoding could only merge distinct names such as `a-b` and `a%2Db`,
// and the declaration count per name decides whether a reference expands.
func dynamicFragment(ref string) (name, why string, ok bool) {
	fragment, found := strings.CutPrefix(ref, "#")
	if !found || fragment == "" || strings.Contains(fragment, "/") {
		return "", fmt.Sprintf("%q is not a plain same-document fragment", ref), false
	}
	name, err := url.PathUnescape(fragment)
	if err != nil {
		return "", fmt.Sprintf("%q is not valid percent-encoded text: %s", ref, err.Error()), false
	}
	return name, "", true
}

// soleAnchorSite returns the one pointer declaring the named $dynamicAnchor, or
// the reason no single declaration answers for it. Two declarations put the
// target back under the evaluation path's control, which no static lowering can
// resolve (docs/compilers/openapi.md#dynamic-ref).
//
// The index's diagnostics come back on every path, including the two answering
// "no single declaration": the walk bound they report is a fact about the
// document, not about this lookup's verdict, so a failed lookup must not
// swallow it.
func soleAnchorSite(c lowering.Ctx, name string) (at jsontext.Pointer, why string, ok bool, diags []ir.Diagnostic) {
	sites, diags := c.DynamicAnchorSites(name)
	if len(sites) == 0 {
		return "", fmt.Sprintf("no $dynamicAnchor %q is declared in this document", name), false, diags
	}
	if len(sites) > 1 {
		return "", fmt.Sprintf("$dynamicAnchor %q is declared %d times, so the target depends on the evaluation path",
			name, len(sites)), false, diags
	}
	return sites[0], "", true, diags
}

// dynamicChainVerdict reports whether from may take the expansion to the anchor
// declared at at, following the target's own expansions in turn. It refuses in
// two cases. A chain returning to from would make the position's own type its
// base, a loop no emitter can resolve; every member of such a cycle reaches this
// verdict independently, so the whole cycle is preserved. An $id on the path to
// from or to any link puts the ends in different schema resources.
//
// The loop is bounded: cur only takes anchor-index values, and each turn
// returns or adds one to seen.
func dynamicChainVerdict(c lowering.Ctx, at, from jsontext.Pointer) (why string, ok bool, diags []ir.Diagnostic) {
	if declaresResourceIDAbove(c, from) {
		return resourceBoundaryWhy(from), false, nil
	}
	seen := map[jsontext.Pointer]bool{}
	for cur := at; !seen[cur]; {
		if cur == from {
			return fmt.Sprintf("expanding it closes a cycle of $dynamicRef expansions back onto %q, "+
				"leaving a type whose own base chain never terminates", from), false, diags
		}
		if declaresResourceIDAbove(c, cur) {
			return resourceBoundaryWhy(cur), false, diags
		}
		seen[cur] = true
		next, hops, hopDiags := dynamicHop(c, cur)
		diags = append(diags, hopDiags...)
		if !hops {
			break
		}
		cur = next
	}
	return "", true, diags
}

// resourceBoundaryWhy words the one irreducible case that is about resources
// rather than shapes, for either end of a chain.
func resourceBoundaryWhy(at jsontext.Pointer) string {
	return fmt.Sprintf("an $id at or above %q starts a schema resource of its own, "+
		"and the IR resolves no resource base URIs", at)
}

// dynamicHop returns the anchor declaration the schema at a component pointer
// would itself expand to, when it writes an expandable $dynamicRef of its own.
// Anything else ends the chain: a position that lowers to a shape rather than to
// another reference cannot extend a cycle of references.
//
// The diagnostics are the index's, so the paths that return before consulting
// it carry none. Once it is consulted they come back even if the chain ends
// there: whether the hop happened and whether the index was complete are
// independent answers.
func dynamicHop(c lowering.Ctx, at jsontext.Pointer) (jsontext.Pointer, bool, []ir.Diagnostic) {
	s := componentSchemaAt(c, at)
	if s == nil {
		return "", false, nil
	}
	name, _, ok := dynamicRefName(s)
	if !ok {
		return "", false, nil
	}
	next, nextDiags := c.DynamicAnchorSites(name)
	if len(next) != 1 {
		return "", false, nextDiags
	}
	return next[0], true, nextDiags
}

// componentSchemaAt returns the schema body of the top-level component pointer
// addresses, and nil for any other pointer. Every accessor on the way is
// nil-safe and annotation.At reads a missing entry as "no body written", so the
// one guard is what distinguishes a component pointer from a deeper one.
func componentSchemaAt(c lowering.Ctx, pointer jsontext.Pointer) *oas3.Schema {
	name, ok := ids.ComponentSchemaName(pointer)
	if !ok {
		return nil
	}
	js, _ := c.Doc.GetComponents().GetSchemas().Get(name)
	return annotation.At(js).Node
}

// declaresResourceIDAbove reports whether any mapping on the path from the root
// down to pointer writes $id, which §8.2.1 makes a schema resource's root.
//
// It reads every step, so a property literally named "$id" reads as a boundary
// that is not there. That errs safely: a false boundary costs an expansion that
// would have been safe, where a missed one mints an inexpressible reference.
//
// The view is built per call; sharing one is a cost question (GitHub #338).
// Known gap: a path node whose merge chain exceeds MergeDepthLimit expands to
// nothing, so an $id there is missed, the unsafe direction (GitHub #401).
func declaresResourceIDAbove(c lowering.Ctx, pointer jsontext.Pointer) bool {
	view := nodeview.New()
	root := nodeview.DocumentRoot(nodeview.Deref(c.Doc.GetRootNode()))
	// An incomplete walk needs no arm: path holds the nodes reached, and an $id
	// above a pointer that falls off the tree still binds.
	path, _ := view.DocumentPath(root, pointer)
	return slices.ContainsFunc(path, func(n *yaml.Node) bool { return view.ChildByToken(n, "$id") != nil })
}

// recordUnexpandedDynamicRef keeps a $dynamicRef verbatim on p when it was not
// expanded, so the reference survives the position lowering without it
// (the irreducible half of docs/compilers/openapi.md#dynamic-ref). An expanded
// one is already the position's type and must not also be preserved — that
// would tell a consumer the compiler ignored it.
func recordUnexpandedDynamicRef(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	_, why, expanded, diags := dynamicExpansion(c, s, pointer)
	if expanded {
		return diags
	}
	at := pointer + ids.Ptr("$dynamicRef")
	kept, keptDiags := annotation.PreserveSchemaKeyword(p, s, "$dynamicRef", ir.ReasonDegradedLowering, pointer, c.ProvenanceAt)
	diags = append(diags, keptDiags...)
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"$dynamicRef was not expanded because %s; it is kept verbatim under Unmodeled", why))
	}
	return diags
}

// declaresDynamicRef reports whether s writes a $dynamicRef, so a position that
// wrote one owns a node to keep an unexpanded reference on.
func declaresDynamicRef(s *oas3.Schema) bool {
	return annotation.RawPropertyNode(s, "$dynamicRef") != nil
}
