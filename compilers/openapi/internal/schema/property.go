// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// FillPropertyDetail enriches a property from its schema: the property-scoped
// facts a type node has no field for (default, visibility, constraints), then
// the declaration's annotations, secrecy among them. Annotations present at a
// $ref use-site override the target's (docs/compilers/openapi.md#ref-use-site).
//
// The property holds the constraints where its schema lowered to no node of
// its own, and none where it did: the node holds them, or keeps them verbatim
// where its kind takes none, so each is written once (CarrierConstraintsOf).
func FillPropertyDetail(loweringContext lowering.Ctx, ts *compile.Types, property *ir.Property, js *statement.Statement, pointer jsontext.Pointer) []ir.Diagnostic {
	if js.Truth != statement.Stated {
		return nil
	}
	use := UseOf(loweringContext, js, property.Type, pointer)
	def, diags := use.Default(loweringContext, js.Schema, pointer)
	property.Default = def
	diags = append(diags, fillPropertyVisibility(loweringContext, property, use.Site(js.Schema).Chain(), pointer)...)
	diags = append(diags, fillPropertyConstraints(loweringContext, property, js.Schema, use.FoldedAllOfEntries, pointer)...)
	diags = append(diags, fillPropertyAnnotations(loweringContext, property, js.Schema, use, pointer)...)
	return append(diags, PreserveRefSiteKeywords(loweringContext, ts, &property.Unmodeled, js, property.Type, pointer)...)
}

// ownDetails returns the merge.Details the declaration of property, js, writes
// itself, read as FillPropertyDetail and PromoteUse read them but for the
// referents its use falls back to (Use.own): its own keywords and keys and
// those of the allOf entries read as written beside it. A $ref's referent
// lends the rest.
func ownDetails(loweringContext lowering.Ctx, js *statement.Statement, property ir.Property) merge.Details {
	chain := readUse(loweringContext, js, property.Type, property.Provenance.Pointer).own().Site(js.Schema).Chain()
	var docs ir.Docs
	annotation.FillCarrierDocs(&docs, chain)
	var details merge.Details
	for _, written := range []struct {
		detail merge.Details
		is     bool
	}{
		{merge.DetailTitle, docs.Summary != ""},
		{merge.DetailDescription, docs.Description != ""},
		{merge.DetailExternalDocs, len(docs.ExternalDocs) > 0},
		{merge.DetailDeprecation, annotation.EffectiveDeprecated(chain) || loweringContext.Promoter().WritesDeprecation(property.Unmodeled)},
		{merge.DetailDefault, slices.ContainsFunc(chain, func(schema *statement.Schema) bool { return schema.Default() != nil })},
	} {
		if written.is {
			details |= written.detail
		}
	}
	return details
}

// fillPropertyVisibility lowers readOnly/writeOnly onto the property and reports
// the pairing that leaves it visible nowhere.
//
// The report is made here rather than inside the reader for the reason the
// allOf merge reports its own disjoint intersection from outside
// mergeVisibility: provenance is built in one place (lowering.Ctx), and this is
// the caller that knows the position being filled. It is the same finding under
// the same code as the allOf spelling, so a consumer filtering on
// diag.DisjointVisibility sees both.
func fillPropertyVisibility(loweringContext lowering.Ctx, property *ir.Property, chain []*statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	visibility, disjoint := annotation.EffectiveVisibility(chain)
	property.Visibility = visibility
	if !disjoint {
		return nil
	}
	return []ir.Diagnostic{loweringContext.DiagAt(ir.SeverityWarning, diag.DisjointVisibility, pointer,
		"readOnly and writeOnly are both in force for field %q; they admit disjoint lifecycles, "+
			"so the field is visible in none", property.WireName)}
}

// fillPropertyConstraints attaches the property's constraints, and the exclusive
// modifier that reached none of them, to the property itself. ir.Property is the
// carrier at this position: a property's schema is read through CarriedRef, so
// it hoists no node of its own to hold either.
//
// It reads ref and its foldedEntries and never the $ref target, which is why
// no referent reaches it: bounds conjoin rather than override, so a referent's
// bound merged here under use-site precedence would publish the wider of the
// two as the whole truth. It stays on the node the reference points at instead
// (docs/ir/annotations.md#what-a-reference-carries-down).
func fillPropertyConstraints(loweringContext lowering.Ctx, property *ir.Property, ref *statement.Schema, foldedEntries []annotation.FoldedAllOfEntry, pointer jsontext.Pointer) []ir.Diagnostic {
	cons, diags := CarrierConstraintsOf(loweringContext, &property.Unmodeled, ref, foldedEntries, property.Type, pointer)
	property.Constraints = cons
	return diags
}

// fillPropertyAnnotations records the annotations the property's schema
// declares on the property, but only when that schema lowered to no node of its
// own. A shared primitive must never carry one declaration's annotations, and a
// node keeps them itself (attachDeclaredAnnotations), so the two homes never
// double.
//
// A node another $ref hoisted at the property's pointer is not that node. It
// carries what the schema declares and so does the property: two IR entities
// reflecting one source schema, whichever is reached first (GitHub #116).
//
// A $ref position never hoists a node, so annotations written beside a
// property's $ref land here (GitHub #114).
func fillPropertyAnnotations(loweringContext lowering.Ctx, property *ir.Property, ref *statement.Schema, use Use, pointer jsontext.Pointer) []ir.Diagnostic {
	if LoweredToOwnNode(loweringContext, pointer, property.Type) {
		return nil
	}
	a, diags := annotation.Read(use.SiteAt(loweringContext, ref, pointer), pointer, loweringContext.Place())

	property.Docs = a.Docs
	if a.Deprecated {
		property.Deprecation = &ir.Deprecation{}
	}
	if a.Sensitive {
		property.Secret = true
	}
	if a.XML != nil {
		property.XML = a.XML
	}
	if len(a.Examples) > 0 {
		property.Examples = a.Examples
	}
	property.Unmodeled = annotation.MergeUnmodeled(property.Unmodeled, a.Unmodeled)
	return append(diags, PreserveCarriedKeywords(loweringContext, &property.Unmodeled, ref, pointer)...)
}

// PreserveCarriedKeywords keeps on unmodeled, a carrier's Unmodeled, what a
// schema that lowered to no node of its own wrote and no field of the carrier
// holds: the content vocabulary, an unexpanded $dynamicRef and any keyword
// nothing reads, which attachDeclaredAnnotations keeps on a node. It is one
// call so that a property, a header and a parameter cannot differ in what they
// keep, and it runs last, as PreserveUnknownKeywords requires.
func PreserveCarriedKeywords(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	// nil node: only a schema with no node of its own comes here, so nothing here
	// can be carrying an Encoding.
	diags := recordUnplacedContent(loweringContext, unmodeled, schema, nil, pointer)
	diags = append(diags, recordUnexpandedDynamicRef(loweringContext, unmodeled, schema, pointer)...)
	return append(diags, PreserveUnknownKeywords(loweringContext, unmodeled, schema, pointer, nil)...)
}

// PromoteUse reads the vendor extensions into the deprecation and redaction of a
// carrier: a property, a header or a parameter. Its own keys are the ones kept in
// unmodeled, and from (FallbackKeysForCarrier) lends beneath them
// (docs/compilers/openapi.md#ref-use-site).
//
// A caller runs it once the carrier's keywords and keys are all on it, which for
// a header or a parameter means after the object around the schema: that object
// decides what is deprecated, and so what a member can fill. A key both write is
// one entry, the object's, so the schema's is not read (GitHub #396).
func PromoteUse(loweringContext lowering.Ctx, from promotion.Referent, unmodeled ir.Unmodeled, dep **ir.Deprecation, redact *bool, prov *ir.Provenance) []ir.Diagnostic {
	diags := loweringContext.Promoter().Deprecation(unmodeled, from, dep, prov)
	return append(diags, loweringContext.Promoter().Redaction(unmodeled, from, redact, prov)...)
}

// FallbackKeysForCarrier returns what the mapped keys of what a carrier of js
// falls back to lend it, js written at pointer and lowered to typeRef
// (Use.fallbackKeys). A position owning a node lends its carrier none, as it
// lends no keyword: the node takes them (attachDeclaredAnnotations).
func FallbackKeysForCarrier(loweringContext lowering.Ctx, js *statement.Statement, typeRef ir.TypeRef, pointer jsontext.Pointer) promotion.Referent {
	if js.Truth != statement.Stated || LoweredToOwnNode(loweringContext, pointer, typeRef) {
		return promotion.Referent{}
	}
	return readUse(loweringContext, js, typeRef, pointer).fallbackKeys(loweringContext)
}

// LoweredToOwnNode reports whether typeRef, what the declaration at pointer in
// loweringContext's document lowered to, is a type node of its own — the node
// attachDeclaredAnnotations then fills, leaving its carrier nothing to hold.
//
// A position's node, whatever its kind, takes the ID its pointer names
// (internNode), so typeRef names it exactly when typeRef names that ID. Nothing
// asks the registry what is interned there: a $ref naming an inline position
// hoists that position's home for its own use, and an answer read off the
// registry would turn on which lowering reached the position first (GitHub #1991).
func LoweredToOwnNode(loweringContext lowering.Ctx, pointer jsontext.Pointer, typeRef ir.TypeRef) bool {
	return typeRef.Target == loweringContext.IDs().ForPointer(pointer)
}
