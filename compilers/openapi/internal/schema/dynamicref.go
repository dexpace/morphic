// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// dynamicRef is the dynamic reference a schema writes: 2020-12's $dynamicRef,
// or 2019-09's $recursiveRef where the rows the schema is read with define it,
// the keyword it is written under and its value.
type dynamicRef struct {
	keyword string
	node    *yaml.Node
}

// dynamicRefOf returns the dynamic reference schema, read at pointer of
// loweringContext's document, writes, and false for none: a $dynamicRef in any
// dialect, as 2020-12 reads one in a schema whose dialect lacks it, else a
// $recursiveRef where the rows of the dialect schema is read in define it, as
// 2019-09's do (docs/compilers/openapi.md#dynamic-ref).
func dynamicRefOf(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (dynamicRef, bool) {
	if node := keyNode(schema, "$dynamicRef"); node != nil {
		return dynamicRef{keyword: "$dynamicRef", node: node}, true
	}
	recursiveRefNode := keyNode(schema, "$recursiveRef")
	if recursiveRefNode == nil || !loweringContext.Rules().At(loweringContext.Position(pointer)).Defines("$recursiveRef") {
		return dynamicRef{}, false
	}
	return dynamicRef{keyword: "$recursiveRef", node: recursiveRefNode}, true
}

// dynamicTarget is the position the dynamic reference schema writes at pointer
// names, as dynamicExpansion reads it but for the check that its expansions
// close no cycle, which walks their whole chain. A reader following it as a
// conjunct edge, as admissionOf does, cuts such a cycle as it cuts one of
// $refs.
func dynamicTarget(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (reftable.DocumentPointer, bool) {
	ref, _, ok := expandableRef(loweringContext, schema, pointer)
	if !ok {
		return reftable.DocumentPointer{}, false
	}
	at, _, ok := dynamicTargetOf(loweringContext, ref, loweringContext.Position(pointer))
	return at, ok
}

// dynamicExpansion resolves the dynamic reference schema writes at pointer to
// the position it names, or reports why it is irreducible. Expansion and
// preservation both decide through it.
//
// Dynamic scope is static per document (docs/compilers/openapi.md#dynamic-ref):
// the target is what the reference names in its own schema resource
// (dynamicTargetOf), read against the anchors of the document it is written
// in, so one another document holds names its own (GitHub #762), and it is what
// a $ref to that position resolves to (resolvesAt). One closing a cycle of
// expansions is irreducible (dynamicChainVerdict). One naming no
// $dynamicAnchor is a reference site (typed.Bindings.RefKeyword).
func dynamicExpansion(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (at reftable.DocumentPointer, why string, ok bool) {
	ref, why, ok := expandableRef(loweringContext, schema, pointer)
	if !ok {
		return reftable.DocumentPointer{}, why, false
	}
	from := loweringContext.Position(pointer)
	if at, why, ok = dynamicTargetOf(loweringContext, ref, from); !ok {
		return reftable.DocumentPointer{}, why, false
	}
	if why, ok = dynamicChainVerdict(loweringContext, at, from); !ok {
		return reftable.DocumentPointer{}, why, false
	}
	return at, "", true
}

// expandableRef returns the dynamic reference schema, read at pointer, writes,
// or why none can expand: none is written, it is co-declared with what an
// expansion cannot carry, or its value is no string.
func expandableRef(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) (dynamicRef, string, bool) {
	ref, ok := dynamicRefOf(loweringContext, schema, pointer)
	switch {
	case !ok:
		return dynamicRef{}, "no dynamic reference is written here", false
	case dynamicRefSiblings(schema):
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
func dynamicTargetOf(loweringContext lowering.Ctx, ref dynamicRef, from reftable.DocumentPointer) (reftable.DocumentPointer, string, bool) {
	if ref.keyword == "$recursiveRef" {
		return recursiveTarget(loweringContext, ref.node.Value, from)
	}
	name, why, ok := reftable.DynamicFragment(ref.node.Value)
	var at reftable.DocumentPointer
	if ok {
		at, why, ok = soleAnchorSite(loweringContext, name)
	}
	if !ok {
		return reftable.DocumentPointer{}, why, false
	}
	if why, ok = sameResource(loweringContext, from, at); !ok {
		return reftable.DocumentPointer{}, why, false
	}
	if !resolvesAt(loweringContext, at) {
		return reftable.DocumentPointer{}, fmt.Sprintf("$dynamicAnchor %q is declared at %q, where no schema could be built",
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
func recursiveTarget(loweringContext lowering.Ctx, value string, from reftable.DocumentPointer) (reftable.DocumentPointer, string, bool) {
	if strings.TrimSpace(value) != "#" {
		return reftable.DocumentPointer{}, fmt.Sprintf(`its value is %q, and JSON Schema 2019-09 defines it only for "#"`, value), false
	}
	root, known := loweringContext.Bindings().Table().Resource(from)
	switch {
	case !known:
		return reftable.DocumentPointer{}, fmt.Sprintf("a schema at or above %q merges past the levels the compiler expands, which "+
			"may hide an identifier starting a schema resource of its own", from.Pointer), false
	case !resolvesAt(loweringContext, root):
		return reftable.DocumentPointer{}, unresolvedRoot(root), false
	}
	if why, ok := recursiveScope(loweringContext, root); !ok {
		return reftable.DocumentPointer{}, why, false
	}
	return root, "", true
}

// unresolvedRoot says why a $recursiveRef whose resource's root is root, where
// no schema could be built, does not expand: at a document's root, as a schema
// under no identifier finds its OpenAPI Object, what an identifier on the
// schema would change.
func unresolvedRoot(root reftable.DocumentPointer) string {
	if root.Pointer == "" {
		return `"#" names the root of the document it is written in, which holds no schema; under an identifier ` +
			`of its own, the schema it means to recurse to would be that root`
	}
	return fmt.Sprintf(`"#" names %q, where no schema could be built`, root.Pointer)
}

// recursiveScope reports whether a $recursiveRef reaching root, a resource's
// root, takes it as its target whatever the evaluation path, or says why not:
// root declares no $recursiveAnchor true, so the reference reads as the $ref it
// spells, or no other schema resource declaring one, in any document, reaches
// root, so none is in its dynamic scope and the outermost on any path is root.
// A document whose identifiers were not read whole may hide another, which
// must not read as none.
func recursiveScope(loweringContext lowering.Ctx, root reftable.DocumentPointer) (string, bool) {
	declared, reached, known := loweringContext.Bindings().Table().RecursiveAnchor(root)
	switch {
	case !known:
		return "this document's identifiers were not read whole, so $recursiveAnchor is not known to be declared " +
			"in one schema resource", false
	case !declared:
		return "", true
	case reached:
		return "$recursiveAnchor: true is also declared in a schema resource that reaches this one, so the target " +
			"depends on the evaluation path", false
	default:
		return "", true
	}
}

// dynamicRefSiblings reports whether schema writes anything beside its
// $dynamicRef that the expansion would have to intersect with. JSON Schema
// conjoins keywords and the IR has no node that intersects a reference with a
// shape, so any of these makes the reference irreducible; expanding regardless
// would assert the target's shape and drop the sibling in silence.
//
// It counts narrowingKeywords as well as shapeKeywords, for the reason the
// former record: an expansion takes the target whole, so a keyword it cannot
// carry is lost whether or not it declares a shape by itself.
func dynamicRefSiblings(schema *statement.Schema) bool {
	if refWritten(schema) || declaresUnion(schema) {
		return true
	}
	return writesSiblingKeyword(schema)
}

// soleAnchorSite returns the one position of loweringContext's document
// declaring the named $dynamicAnchor, or the reason no single declaration
// answers for it. Two declarations put the target back under the evaluation
// path's control, which no static lowering can resolve, as does a resource of
// another document declaring the name that reaches it, and an incomplete read
// may hide a second, which an expansion would then rest on unverified
// (docs/compilers/openapi.md#dynamic-ref).
func soleAnchorSite(loweringContext lowering.Ctx, name string) (reftable.DocumentPointer, string, bool) {
	anchor := loweringContext.DynamicAnchor(name)
	switch {
	case anchor.Count > 1:
		return reftable.DocumentPointer{}, fmt.Sprintf("$dynamicAnchor %q is declared %d times, so the target depends on the "+
			"evaluation path", name, anchor.Count), false
	case !anchor.Complete:
		return reftable.DocumentPointer{}, fmt.Sprintf("this document's identifiers were not read whole, so $dynamicAnchor %q "+
			"is not known to be declared once", name), false
	case anchor.Count == 0:
		return reftable.DocumentPointer{}, fmt.Sprintf("no $dynamicAnchor %q is declared in this document", name), false
	case anchor.Reached:
		return reftable.DocumentPointer{}, fmt.Sprintf("$dynamicAnchor %q is also declared in a schema resource of another "+
			"document that reaches this one, so the target depends on the evaluation path", name), false
	default:
		return anchor.Site, "", true
	}
}

// dynamicChainVerdict reports whether from may take the expansion to the anchor
// declared at at, following the target's own expansions in turn, and the
// chain of each reference site it meets (dynamicHop). A chain returning to
// from would make the position's own type its base, a loop no emitter can
// resolve; every member of such a cycle reaches this verdict independently,
// so the whole cycle is preserved.
//
// The loop is bounded: each turn returns or adds a position to seen.
func dynamicChainVerdict(loweringContext lowering.Ctx, at, from reftable.DocumentPointer) (string, bool) {
	seen := map[reftable.DocumentPointer]bool{}
	for cur := at; !seen[cur]; {
		if cur == from {
			return fmt.Sprintf("expanding it closes a cycle of $dynamicRef expansions back onto %q, "+
				"leaving a type whose own base chain never terminates", from.Pointer), false
		}
		seen[cur] = true
		next, hops := dynamicHop(loweringContext, cur)
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
func sameResource(loweringContext lowering.Ctx, from, at reftable.DocumentPointer) (string, bool) {
	table := loweringContext.Bindings().Table()
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

// dynamicHop returns the position the schema at at would itself lower to a
// reference to: where its chain ends, for a reference site the table holds an
// entry for, a $ref's or a static $dynamicRef's, else where an expandable
// dynamic reference of its own expands, as dynamicTargetOf names it. Anything
// else ends the chain: a position that lowers to a shape rather than to
// another reference cannot extend a cycle of references.
func dynamicHop(loweringContext lowering.Ctx, at reftable.DocumentPointer) (reftable.DocumentPointer, bool) {
	if referenceSite, ok := loweringContext.Bindings().Table().Entry(at); ok {
		return referenceSite.Chain.End, referenceSite.Chain.Status == reftable.Resolved
	}
	schema := annotation.At(loweringContext.Statements().At(at)).Node
	if !stated(schema) {
		return reftable.DocumentPointer{}, false
	}
	ref, _, ok := expandableRef(loweringContext, schema, at.Pointer)
	if !ok {
		return reftable.DocumentPointer{}, false
	}
	next, _, ok := dynamicTargetOf(loweringContext, ref, at)
	return next, ok
}

// recordUnexpandedDynamicRef keeps a dynamic reference verbatim on unmodeled,
// under the keyword it is written as, when it was not expanded, so the
// reference survives the position lowering without it (the irreducible half of
// docs/compilers/openapi.md#dynamic-ref). An expanded one is already the
// position's type and must not also be preserved — that would tell a consumer
// the compiler ignored it — and one schema is a reference site under
// (typed.Bindings.RefKeyword) is the $ref it spells, resolved as one.
func recordUnexpandedDynamicRef(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	ref, written := dynamicRefOf(loweringContext, schema, pointer)
	if !written || schema.Ref != nil && schema.Ref.Keyword == ref.keyword {
		return nil
	}
	_, why, expanded := dynamicExpansion(loweringContext, schema, pointer)
	if expanded {
		return nil
	}
	kept, diags := annotation.PreserveSchemaKeyword(unmodeled, schema, ref.keyword, ir.ReasonDegradedLowering, pointer, loweringContext.Place())
	if kept {
		diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(ref.keyword),
			"%s was not expanded because %s; it is kept verbatim under Unmodeled", ref.keyword, why))
	}
	return diags
}

// dynamicExpanded reports the expansion of the dynamic reference schema writes
// at pointer to target, at the keyword it is written as.
func dynamicExpanded(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeID) ir.Diagnostic {
	ref, _ := dynamicRefOf(loweringContext, schema, pointer)
	what := "the one matching $dynamicAnchor in this document"
	if ref.keyword == "$recursiveRef" {
		what = "the root of the schema resource it is written in"
	}
	return loweringContext.DiagAt(ir.SeverityInfo, diag.DynamicRefExpanded, pointer+ids.Ptr(ref.keyword), "%s expanded to %q, %s",
		ref.keyword, target, what)
}

// declaresDynamicRef reports whether schema writes a $dynamicRef or a
// $recursiveRef, so a position that wrote one owns a node to keep an unexpanded
// reference on. It reads either keyword in any dialect, so a reader asking it
// with no context to read the dialect by takes a $recursiveRef 2020-12 does not
// define as dynamic too, which each errs safely on: it keeps a composition it
// would refine, or a node it would leave the alias to.
func declaresDynamicRef(schema *statement.Schema) bool {
	return hasKey(schema, "$dynamicRef") || hasKey(schema, "$recursiveRef")
}
