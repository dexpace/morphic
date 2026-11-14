// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// dynamicRef is the dynamic reference a schema writes: 2020-12's $dynamicRef,
// or 2019-09's $recursiveRef where the rows the schema is read with define it,
// the keyword it is written under and its value.
type dynamicRef struct {
	keyword string
	node    *yaml.Node
}

// dynamicRefOf returns the dynamic reference s, read at pointer of c's
// document, writes, and false for none: a $dynamicRef in any dialect, as
// 2020-12 reads one in a schema whose dialect lacks it, else a $recursiveRef
// where the rows of the dialect s is read in define it, as 2019-09's do
// (docs/compilers/openapi.md#dynamic-ref).
func dynamicRefOf(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (dynamicRef, bool) {
	if n := annotation.RawPropertyNode(s, "$dynamicRef"); n != nil {
		return dynamicRef{keyword: "$dynamicRef", node: n}, true
	}
	n := annotation.RawPropertyNode(s, "$recursiveRef")
	if n == nil || !lowering.SchemaRulesAt(c, pointer).Defines("$recursiveRef") {
		return dynamicRef{}, false
	}
	return dynamicRef{keyword: "$recursiveRef", node: n}, true
}

// dynamicTarget is the position the dynamic reference s writes at pointer
// names, as dynamicExpansion reads it but for the check that its expansions
// close no cycle, which walks their whole chain. A reader spending a step on
// each schema it reads, as boundTypes does, ends such a cycle on its budget.
func dynamicTarget(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (bind.Position, bool) {
	ref, _, ok := expandableRef(c, s, pointer)
	if !ok {
		return bind.Position{}, false
	}
	at, _, ok := dynamicTargetOf(c, ref, c.Position(pointer))
	return at, ok
}

// dynamicExpansion resolves the dynamic reference s writes at pointer to the
// position it names, or reports why it is irreducible. Expansion and
// preservation both decide through it.
//
// Dynamic scope is static per document (docs/compilers/openapi.md#dynamic-ref):
// the target is what the reference names in its own schema resource
// (dynamicTargetOf), read against the anchors of the document it is written
// in, so one another document holds names its own (GitHub #762), and it is what
// a $ref to that position resolves to (resolvesAt). One closing a cycle of
// expansions is irreducible (dynamicChainVerdict).
func dynamicExpansion(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (at bind.Position, why string, ok bool) {
	ref, why, ok := expandableRef(c, s, pointer)
	if !ok {
		return bind.Position{}, why, false
	}
	from := c.Position(pointer)
	if at, why, ok = dynamicTargetOf(c, ref, from); !ok {
		return bind.Position{}, why, false
	}
	if why, ok = dynamicChainVerdict(c, at, from); !ok {
		return bind.Position{}, why, false
	}
	return at, "", true
}

// expandableRef returns the dynamic reference s, read at pointer, writes, or
// why none can expand: none is written, it is co-declared with what an
// expansion cannot carry, or its value is no string.
func expandableRef(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (dynamicRef, string, bool) {
	ref, ok := dynamicRefOf(c, s, pointer)
	switch {
	case !ok:
		return dynamicRef{}, "no dynamic reference is written here", false
	case dynamicRefSiblings(s):
		return dynamicRef{}, "it is co-declared with another applicator, which the IR cannot intersect with it", false
	case ref.node.Kind != yaml.ScalarNode:
		return dynamicRef{}, "its value is not a reference string", false
	default:
		return ref, "", true
	}
}

// dynamicTargetOf returns the position the dynamic reference ref, written at
// from, names, or why it names none that expands: for a $dynamicRef the one
// $dynamicAnchor of its name its document declares, in its own resource, and
// for a $recursiveRef the root of its resource (recursiveTarget); each where a
// schema stands to resolve to.
func dynamicTargetOf(c lowering.Ctx, ref dynamicRef, from bind.Position) (bind.Position, string, bool) {
	if ref.keyword == "$recursiveRef" {
		return recursiveTarget(c, ref.node.Value, from)
	}
	name, why, ok := bind.DynamicFragment(ref.node.Value)
	if !ok {
		return bind.Position{}, why, false
	}
	at, why, ok := soleAnchorSite(c, name)
	if !ok {
		return bind.Position{}, why, false
	}
	if why, ok = sameResource(c, from, at); !ok {
		return bind.Position{}, why, false
	}
	if !resolvesAt(c, at) {
		return bind.Position{}, fmt.Sprintf("$dynamicAnchor %q is declared at %q, where no schema could be built",
			name, at.Pointer), false
	}
	return at, "", true
}

// recursiveTarget returns the position a $recursiveRef whose value is value,
// written at from, expands to, or why it does not. JSON Schema 2019-09 defines
// it only for "#", resolved as a $ref is, to the root of from's resource. Where
// that root declares $recursiveAnchor true, the target is the outermost schema
// of the dynamic scope declaring it too, which is that root only where every
// declaration of the document lies in its resource (recursiveScope).
func recursiveTarget(c lowering.Ctx, value string, from bind.Position) (bind.Position, string, bool) {
	if strings.TrimSpace(value) != "#" {
		return bind.Position{}, fmt.Sprintf(`its value is %q, and JSON Schema 2019-09 defines it only for "#"`, value), false
	}
	root, known := c.Bindings().Table().Resource(from)
	switch {
	case !known:
		return bind.Position{}, fmt.Sprintf("a schema at or above %q merges past the levels the compiler expands, which "+
			"may hide an identifier starting a schema resource of its own", from.Pointer), false
	case !resolvesAt(c, root):
		return bind.Position{}, unresolvedRoot(root), false
	}
	if why, ok := recursiveScope(c, root); !ok {
		return bind.Position{}, why, false
	}
	return root, "", true
}

// unresolvedRoot says why a $recursiveRef whose resource's root is root, where
// no schema could be built, does not expand: at a document's root, as a schema
// under no identifier finds its OpenAPI Object, what an identifier on the
// schema would change.
func unresolvedRoot(root bind.Position) string {
	if root.Pointer == "" {
		return `"#" names the root of the document it is written in, which holds no schema; under an identifier ` +
			`of its own, the schema it means to recurse to would be that root`
	}
	return fmt.Sprintf(`"#" names %q, where no schema could be built`, root.Pointer)
}

// recursiveScope reports whether a $recursiveRef reaching root, a resource's
// root, takes it as its target whatever the evaluation path, or says why not:
// root declares no $recursiveAnchor true, so the reference reads as the $ref it
// spells, or every schema of its document declaring one lies in root's
// resource, so the outermost on any path is root. A document whose
// identifiers were not read whole may hide another, which must not read as
// none. Two unrelated resources each declaring one are not told apart from a
// path through both (GitHub #1884).
func recursiveScope(c lowering.Ctx, root bind.Position) (string, bool) {
	declared, resources, known := c.Bindings().Table().RecursiveAnchor(root)
	switch {
	case !known:
		return "this document's identifiers were not read whole, so $recursiveAnchor is not known to be declared " +
			"in one schema resource", false
	case !declared:
		return "", true
	case resources > 1:
		return fmt.Sprintf("$recursiveAnchor: true is declared in %d schema resources of this document, so the "+
			"target depends on the evaluation path", resources), false
	default:
		return "", true
	}
}

// dynamicRefSiblings reports whether s writes anything beside its $dynamicRef
// that the expansion would have to intersect with. JSON Schema conjoins
// keywords and the IR has no node that intersects a reference with a shape, so
// any of these makes the reference irreducible; expanding regardless would
// assert the target's shape and drop the sibling in silence.
//
// It counts narrowingKeywords as well as shapeKeywords, for the reason the
// former record: an expansion takes the target whole, so a keyword it cannot
// carry is lost whether or not it declares a shape by itself.
func dynamicRefSiblings(s *oas3.Schema) bool {
	if s.Ref != nil || len(s.GetOneOf()) > 0 || len(s.GetAnyOf()) > 0 {
		return true
	}
	return writesSiblingKeyword(s)
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
// declared at at, following the target's own expansions in turn. A chain
// returning to from would make the position's own type its base, a loop no
// emitter can resolve; every member of such a cycle reaches this verdict
// independently, so the whole cycle is preserved.
//
// The loop is bounded: cur only takes anchor positions, and each turn returns
// or adds one to seen.
func dynamicChainVerdict(c lowering.Ctx, at, from bind.Position) (string, bool) {
	seen := map[bind.Position]bool{}
	for cur := at; !seen[cur]; {
		if cur == from {
			return fmt.Sprintf("expanding it closes a cycle of $dynamicRef expansions back onto %q, "+
				"leaving a type whose own base chain never terminates", from.Pointer), false
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

// sameResource reports whether the schemas at from and at lie in one schema
// resource, or says why they do not, or may not: a merge past the bound can
// hide an identifier, which must not read as none. A dynamic reference is
// first resolved against its own resource's base (JSON Schema 2020-12
// §8.2.3.2), so an anchor in another resource is not the one it names.
func sameResource(c lowering.Ctx, from, at bind.Position) (string, bool) {
	table := c.Bindings().Table()
	own, knownOwn := table.Resource(from)
	theirs, knownTheirs := table.Resource(at)
	switch {
	case !knownOwn || !knownTheirs:
		return fmt.Sprintf("a schema at or above %q or %q merges past the levels the compiler expands, which "+
			"may hide an identifier starting a schema resource of its own", from.Pointer, at.Pointer), false
	case own != theirs:
		return fmt.Sprintf("an identifier between it and %q puts the two in different schema resources, and a "+
			"dynamic reference names an anchor of its own resource", at.Pointer), false
	default:
		return "", true
	}
}

// dynamicHop returns the position the schema at at would itself expand to,
// where it writes an expandable dynamic reference of its own, as
// dynamicTargetOf names it. Anything else ends the chain: a position that
// lowers to a shape rather than to another reference cannot extend a cycle of
// references.
func dynamicHop(c lowering.Ctx, at bind.Position) (bind.Position, bool) {
	s := annotation.At(c.Bindings().At(at)).Node
	if s == nil {
		return bind.Position{}, false
	}
	ref, _, ok := expandableRef(c, s, at.Pointer)
	if !ok {
		return bind.Position{}, false
	}
	next, _, ok := dynamicTargetOf(c, ref, at)
	return next, ok
}

// recordUnexpandedDynamicRef keeps a dynamic reference verbatim on p, under
// the keyword it is written as, when it was not expanded, so the reference
// survives the position lowering without it (the irreducible half of
// docs/compilers/openapi.md#dynamic-ref). An expanded one is already the
// position's type and must not also be preserved — that would tell a consumer
// the compiler ignored it.
func recordUnexpandedDynamicRef(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	ref, written := dynamicRefOf(c, s, pointer)
	if !written {
		return nil
	}
	_, why, expanded := dynamicExpansion(c, s, pointer)
	if expanded {
		return nil
	}
	kept, diags := annotation.PreserveSchemaKeyword(p, s, ref.keyword, ir.ReasonDegradedLowering, pointer, c.Place())
	if kept {
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(ref.keyword),
			"%s was not expanded because %s; it is kept verbatim under Unmodeled", ref.keyword, why))
	}
	return diags
}

// dynamicExpanded reports the expansion of the dynamic reference s writes at
// pointer to target, at the keyword it is written as.
func dynamicExpanded(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer, target ir.TypeID) ir.Diagnostic {
	ref, _ := dynamicRefOf(c, s, pointer)
	what := "the one matching $dynamicAnchor in this document"
	if ref.keyword == "$recursiveRef" {
		what = "the root of the schema resource it is written in"
	}
	return c.DiagAt(ir.SeverityInfo, diag.DynamicRefExpanded, pointer+ids.Ptr(ref.keyword), "%s expanded to %q, %s",
		ref.keyword, target, what)
}

// declaresDynamicRef reports whether s writes a $dynamicRef or a $recursiveRef,
// so a position that wrote one owns a node to keep an unexpanded reference on.
// It reads either keyword in any dialect, so a reader asking it with no
// context to read the dialect by takes a $recursiveRef 2020-12 does not define
// as dynamic too, which each errs safely on: it keeps a composition it would
// refine, or a node it would leave the alias to.
func declaresDynamicRef(s *oas3.Schema) bool {
	return annotation.RawPropertyNode(s, "$dynamicRef") != nil || annotation.RawPropertyNode(s, "$recursiveRef") != nil
}
