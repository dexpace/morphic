// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// dynamicExpansion resolves the $dynamicRef s writes at pointer to the position
// it names, or reports why it is irreducible. Expansion and preservation both
// decide through it.
//
// Dynamic scope is static per document (docs/compilers/openapi.md#dynamic-ref),
// so a $dynamicAnchor declared once is the only match, provided the document is
// one schema resource (dynamicChainVerdict). The anchors are those of the
// document the reference is written in, so one another document holds names its
// own (GitHub #762). A $dynamicRef co-declared with a $ref, oneOf/anyOf or a
// shape is irreducible too, as is one in content no position of a document
// places.
func dynamicExpansion(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (at bind.Position, why string, ok bool) {
	name, why, ok := dynamicRefName(s)
	if !ok {
		return bind.Position{}, why, false
	}
	from, placed := c.Position(pointer)
	if !placed {
		return bind.Position{}, "it is written in what a reference resolved to, which no position of its document " +
			"places, so nothing says whose $dynamicAnchor it names", false
	}
	at, why, ok = soleAnchorSite(c, name)
	if !ok {
		return bind.Position{}, why, false
	}
	if why, ok = anchorHolds(c, name, at); !ok {
		return bind.Position{}, why, false
	}
	if why, ok = dynamicChainVerdict(c, at, from); !ok {
		return bind.Position{}, why, false
	}
	return at, "", true
}

// anchorHolds reports whether the position declaring the anchor is a target the
// IR can name, or says why not. In the source only a top-level component's is,
// since only its TypeID is stable whatever lowers first
// (docs/compilers/openapi.md#dynamic-ref). What another document declares has
// IDs in that document's namespace from its pointer, so a position of it is one
// where a schema is held.
func anchorHolds(c lowering.Ctx, name string, at bind.Position) (string, bool) {
	if at.Doc != bind.Source {
		if resolve.Hoistable(c.Bindings().At(at)) {
			return "", true
		}
		return fmt.Sprintf("$dynamicAnchor %q is declared at %q, where no schema could be built", name, at.Pointer), false
	}
	switch _, state := c.RefScope().ComponentRef(at.Pointer); state {
	case ids.NamedComponent:
		return "", true
	case ids.UnnamedComponent:
		return fmt.Sprintf(`$dynamicAnchor %q is declared on the component schema keyed "" at %q, `+
			"and an empty name earns no named type to expand to", name, at.Pointer), false
	default:
		return fmt.Sprintf("$dynamicAnchor %q is declared at %q rather than on a component schema", name, at.Pointer), false
	}
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
	return bind.DynamicFragment(node.Value)
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

// soleAnchorSite returns the one position of c's document declaring the named
// $dynamicAnchor, or the reason no single declaration answers for it. Two
// declarations put the target back under the evaluation path's control, which no
// static lowering can resolve, and an incomplete read may hide a second, which an
// expansion would then rest on unverified (docs/compilers/openapi.md#dynamic-ref).
func soleAnchorSite(c lowering.Ctx, name string) (bind.Position, string, bool) {
	sites, complete := c.DynamicAnchorSites(name)
	switch {
	case len(sites) > 1:
		return bind.Position{}, fmt.Sprintf("$dynamicAnchor %q is declared %d times, so the target depends on the "+
			"evaluation path", name, len(sites)), false
	case !complete:
		return bind.Position{}, fmt.Sprintf("this document's identifiers were not read whole, so $dynamicAnchor %q "+
			"is not known to be declared once", name), false
	case len(sites) == 0:
		return bind.Position{}, fmt.Sprintf("no $dynamicAnchor %q is declared in this document", name), false
	default:
		return sites[0], "", true
	}
}

// dynamicChainVerdict reports whether from may take the expansion to the anchor
// declared at at, following the target's own expansions in turn. It refuses in
// two cases. A chain returning to from would make the position's own type its
// base, a loop no emitter can resolve; every member of such a cycle reaches this
// verdict independently, so the whole cycle is preserved. An $id on the path to
// from or to any link puts the ends in different schema resources.
//
// The loop is bounded: cur only takes anchor positions, and each turn returns
// or adds one to seen.
func dynamicChainVerdict(c lowering.Ctx, at, from bind.Position) (string, bool) {
	if why, ok := oneResource(c, from); !ok {
		return why, false
	}
	seen := map[bind.Position]bool{}
	for cur := at; !seen[cur]; {
		if cur == from {
			return fmt.Sprintf("expanding it closes a cycle of $dynamicRef expansions back onto %q, "+
				"leaving a type whose own base chain never terminates", from.Pointer), false
		}
		if why, ok := oneResource(c, cur); !ok {
			return why, false
		}
		seen[cur] = true
		next, hops := dynamicHop(c, cur)
		if !hops {
			break
		}
		cur = next
	}
	return "", true
}

// oneResource reports whether the schema at p lies in its document's own schema
// resource, or says why it does not, or may not: a merge past the bound can hide
// an $id, which must not read as none, or the reference would expand across a
// boundary the IR cannot express (GitHub #401).
func oneResource(c lowering.Ctx, p bind.Position) (string, bool) {
	own, known := c.Bindings().Table().InOwnResource(p)
	switch {
	case !known:
		return fmt.Sprintf("a schema at or above %q merges past the levels the compiler expands, which may hide an "+
			"$id starting a schema resource of its own", p.Pointer), false
	case own:
		return fmt.Sprintf("an $id at or above %q starts a schema resource of its own, "+
			"and the IR resolves no resource base URIs", p.Pointer), false
	default:
		return "", true
	}
}

// dynamicHop returns the anchor declaration the schema at a position would
// itself expand to, when it writes an expandable $dynamicRef of its own.
// Anything else ends the chain: a position that lowers to a shape rather than to
// another reference cannot extend a cycle of references.
func dynamicHop(c lowering.Ctx, at bind.Position) (bind.Position, bool) {
	s := annotation.At(c.Bindings().At(at)).Node
	if s == nil {
		return bind.Position{}, false
	}
	name, _, ok := dynamicRefName(s)
	if !ok {
		return bind.Position{}, false
	}
	sites, _ := c.DynamicAnchorSites(name)
	if len(sites) != 1 {
		return bind.Position{}, false
	}
	return sites[0], true
}

// recordUnexpandedDynamicRef keeps a $dynamicRef verbatim on p when it was not
// expanded, so the reference survives the position lowering without it
// (the irreducible half of docs/compilers/openapi.md#dynamic-ref). An expanded
// one is already the position's type and must not also be preserved — that
// would tell a consumer the compiler ignored it.
func recordUnexpandedDynamicRef(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	_, why, expanded := dynamicExpansion(c, s, pointer)
	if expanded {
		return nil
	}
	at := pointer + ids.Ptr("$dynamicRef")
	kept, diags := annotation.PreserveSchemaKeyword(p, s, "$dynamicRef", ir.ReasonDegradedLowering, pointer, c.ProvenanceAt)
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
