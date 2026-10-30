// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// FillPropertyDetail enriches a property from its schema: the property-scoped
// facts a type node has no field for (default, visibility, constraints), then
// the declaration's annotations, secrecy among them. Annotations present at a
// $ref use-site override the target's (docs/compilers/openapi.md#ref-use-site).
//
// Constraints stay unconditional: ir.Property is the only home every property
// has, while its schema may lower to a node of a kind with no Constraints field
// or to no node of its own at all. Restating them where the node also carries
// them is the safe half of that trade.
func FillPropertyDetail(c lowering.Ctx, ts *compile.Types, p *ir.Property, js *oas3.JSONSchema[oas3.Referenceable], pointer jsontext.Pointer) []ir.Diagnostic {
	ref := js.GetSchema()
	if ref == nil {
		return nil
	}
	tgt := c.RefScope().TargetSchema(js, ref)
	def, diags := annotation.EffectiveDefault(ref, tgt, pointer, c.ProvenanceAt)
	p.Default = def
	diags = append(diags, fillPropertyVisibility(c, p, ref, tgt, pointer)...)
	diags = append(diags, fillPropertyConstraints(c, p, ref, pointer)...)
	diags = append(diags, fillPropertyAnnotations(c, ts, p, ref, tgt, pointer)...)
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
func fillPropertyVisibility(c lowering.Ctx, p *ir.Property, ref, tgt *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	visibility, disjoint := annotation.EffectiveVisibility(ref, tgt)
	p.Visibility = visibility
	if !disjoint {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DisjointVisibility, pointer,
		"readOnly and writeOnly are both in force for field %q; they admit disjoint lifecycles, "+
			"so the field is visible in none", p.WireName)}
}

// fillPropertyConstraints attaches the property's scalar constraints, and the
// exclusive modifier that reached none of them, to the property itself.
// ir.Property is the carrier at this position: a property's schema is read
// through CarriedRef, so it hoists no node of its own to hold either.
//
// It reads ref alone and never the $ref target, which is why no tgt reaches it:
// bounds conjoin rather than override, so a referent's bound merged here under
// use-site precedence would publish the wider of the two as the whole truth. It
// stays on the node the reference points at instead (ir-design §12.2).
func fillPropertyConstraints(c lowering.Ctx, p *ir.Property, ref *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	cons, diags := ConstraintsOf(c, &p.Unmodeled, ref, pointer)
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
func fillPropertyAnnotations(c lowering.Ctx, ts *compile.Types, p *ir.Property, ref, tgt *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	if LoweredToOwnNode(c, ts, pointer, p.Type) {
		return nil
	}
	a, diags := annotation.Read(annotation.Site{Kind: annotation.Reference, Node: ref, Referent: tgt}, pointer, c.ProvenanceAt)

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
	diags = append(diags, c.PromoteDeprecation(p.Unmodeled, p.Deprecation, &p.Provenance)...)
	// nil node: this arm runs only when the schema lowered to no node of its own,
	// so nothing here can be carrying an Encoding.
	diags = append(diags, recordUnplacedContent(c, &p.Unmodeled, ref, nil, pointer)...)
	diags = append(diags, recordUnexpandedDynamicRef(c, &p.Unmodeled, ref, pointer)...)
	return append(diags, PreserveUnknownKeywords(c, &p.Unmodeled, ref, pointer)...)
}

// LoweredToOwnNode reports whether the declaration at pointer, in c's
// document, lowered to a type node of its own — the node
// attachDeclaredAnnotations then fills, leaving its carrier nothing to hold.
//
// It asks whether the node interned at pointer is the one the declaration
// lowered to, not merely whether a node is interned there. A $ref naming an
// inline position hoists that position's home for its own use, in either
// declaration order, and a carrier reading the registry alone would keep its
// schema's annotations only when it happened to lower first.
func LoweredToOwnNode(c lowering.Ctx, ts *compile.Types, pointer jsontext.Pointer, t ir.TypeRef) bool {
	id, owned := ts.Lookup(c.IDs().Coordinate(pointer))
	return owned && id == t.Target
}
