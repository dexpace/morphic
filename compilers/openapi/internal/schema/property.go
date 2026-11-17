// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
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
func FillPropertyDetail(c lowering.Ctx, ts *compile.Types, p *ir.Property, js *statement.Statement, pointer jsontext.Pointer) []ir.Diagnostic {
	if js.Truth != statement.Stated {
		return nil
	}
	use := UseOf(c, js, p.Type, pointer)
	def, diags := use.Default(c, js.Schema, pointer)
	p.Default = def
	diags = append(diags, fillPropertyVisibility(c, p, use.Site(js.Schema).Chain(), pointer)...)
	diags = append(diags, fillPropertyConstraints(c, ts, p, js.Schema, use.Voices, pointer)...)
	diags = append(diags, fillPropertyAnnotations(c, p, js.Schema, use, pointer)...)
	return append(diags, PreserveRefSiteKeywords(c, ts, &p.Unmodeled, js, p.Type, pointer)...)
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
func fillPropertyVisibility(c lowering.Ctx, p *ir.Property, chain []*statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	visibility, disjoint := annotation.EffectiveVisibility(chain)
	p.Visibility = visibility
	if !disjoint {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DisjointVisibility, pointer,
		"readOnly and writeOnly are both in force for field %q; they admit disjoint lifecycles, "+
			"so the field is visible in none", p.WireName)}
}

// fillPropertyConstraints attaches the property's constraints, and the exclusive
// modifier that reached none of them, to the property itself. ir.Property is the
// carrier at this position: a property's schema is read through CarriedRef, so
// it hoists no node of its own to hold either.
//
// It reads ref and its voices and never the $ref target, which is why no referent
// reaches it: bounds conjoin rather than override, so a referent's bound merged
// here under use-site precedence would publish the wider of the two as the whole
// truth. It stays on the node the reference points at instead
// (docs/ir/annotations.md#what-a-reference-carries-down).
func fillPropertyConstraints(c lowering.Ctx, ts *compile.Types, p *ir.Property, ref *statement.Schema, voices []annotation.Voice, pointer jsontext.Pointer) []ir.Diagnostic {
	cons, diags := CarrierConstraintsOf(c, ts, &p.Unmodeled, ref, voices, p.Type, pointer)
	p.Constraints = cons
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
func fillPropertyAnnotations(c lowering.Ctx, p *ir.Property, ref *statement.Schema, use Use, pointer jsontext.Pointer) []ir.Diagnostic {
	if LoweredToOwnNode(c, pointer, p.Type) {
		return nil
	}
	a, diags := annotation.Read(use.SiteAt(c, ref, pointer), pointer, c.Place())

	p.Docs = a.Docs
	if a.Deprecated {
		p.Deprecation = &ir.Deprecation{}
	}
	if a.Sensitive {
		p.Secret = true
	}
	if a.XML != nil {
		p.XML = a.XML
	}
	if len(a.Examples) > 0 {
		p.Examples = a.Examples
	}
	p.Unmodeled = annotation.MergeUnmodeled(p.Unmodeled, a.Unmodeled)
	return append(diags, PreserveCarriedKeywords(c, &p.Unmodeled, ref, pointer)...)
}

// PreserveCarriedKeywords keeps on p, a carrier's Unmodeled, what a schema that
// lowered to no node of its own wrote and no field of the carrier holds: the
// content vocabulary, an unexpanded $dynamicRef and any keyword nothing reads,
// which attachDeclaredAnnotations keeps on a node. It is one call so that a
// property, a header and a parameter cannot differ in what they keep, and it runs
// last, as PreserveUnknownKeywords requires.
func PreserveCarriedKeywords(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	// nil node: only a schema with no node of its own comes here, so nothing here
	// can be carrying an Encoding.
	diags := recordUnplacedContent(c, p, s, nil, pointer)
	diags = append(diags, recordUnexpandedDynamicRef(c, p, s, pointer)...)
	return append(diags, PreserveUnknownKeywords(c, p, s, pointer, nil)...)
}

// PromoteUse reads the vendor extensions into the deprecation and redaction of a
// carrier: a property, a header or a parameter. Its own keys are the ones kept in
// unmodeled, and from (KeysLentTo) lends beneath them
// (docs/compilers/openapi.md#ref-use-site).
//
// A caller runs it once the carrier's keywords and keys are all on it, which for
// a header or a parameter means after the object around the schema: that object
// decides what is deprecated, and so what a member can fill. A key both write is
// one entry, the object's, so the schema's is not read (GitHub #396).
func PromoteUse(c lowering.Ctx, from promotion.Referent, unmodeled ir.Unmodeled, dep **ir.Deprecation, redact *bool, prov *ir.Provenance) []ir.Diagnostic {
	diags := c.Promoter().Deprecation(unmodeled, from, dep, prov)
	return append(diags, c.Promoter().Redaction(unmodeled, from, redact, prov)...)
}

// KeysLentTo returns what the mapped keys of what a carrier of js falls back to
// lend it, js written at pointer and lowered to t (Use.lentKeys). A position
// owning a node lends its carrier none, as it lends no keyword: the node takes
// them (attachDeclaredAnnotations).
func KeysLentTo(c lowering.Ctx, js *statement.Statement, t ir.TypeRef, pointer jsontext.Pointer) promotion.Referent {
	if js.Truth != statement.Stated || LoweredToOwnNode(c, pointer, t) {
		return promotion.Referent{}
	}
	return UseOf(c, js, t, pointer).lentKeys(c)
}

// LoweredToOwnNode reports whether t, what the declaration at pointer in c's
// document lowered to, is a type node of its own — the node
// attachDeclaredAnnotations then fills, leaving its carrier nothing to hold.
//
// A position's node, whatever its kind, takes the ID its pointer names
// (internNode), so t names it exactly when t names that ID. Nothing asks the
// registry what is interned there: a $ref naming an inline position hoists that
// position's home for its own use, and an answer read off the registry would
// turn on which lowering reached the position first (GitHub #1991).
func LoweredToOwnNode(c lowering.Ctx, pointer jsontext.Pointer, t ir.TypeRef) bool {
	return t.Target == c.IDs().ForPointer(pointer)
}
