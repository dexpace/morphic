// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Resolve fills, on every deprecated node of doc whose own keys name its
// replacements, the entities those names resolve to, each named as the
// document names an entity of the node's class: an operation by its
// operationId, a parameter by `<in>.<name>` or its name within its operation,
// a header by name among the headers beside it, a property by name within its
// model, a schema by its components name, and a security scheme by its key
// (docs/compilers/openapi.md#extension-promotion). An operation, a parameter
// or a property whose keys name none takes what its message names (message).
func Resolve(c lowering.Ctx, doc *ir.Document) []ir.Diagnostic {
	r := resolver{c: c, p: c.Promoter(), doc: doc}
	if !r.p.Maps(lowering.TargetDeprecationReplacedBy) && !c.ReplacementInference.Honours() {
		return nil
	}
	var diags []ir.Diagnostic
	for i := range doc.Services {
		diags = append(diags, r.service(&doc.Services[i])...)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		if td := doc.Types[id]; !ir.IsNilTypeDef(td) {
			diags = append(diags, r.typeDef(id, td)...)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Auth)) {
		scheme := doc.Auth[id]
		diags = append(diags, r.fill(scheme.Unmodeled, scheme.Deprecation, &scheme.Provenance,
			ir.EntityRef{Auth: id}, "security scheme", r.scheme)...)
		doc.Auth[id] = scheme
	}
	return diags
}

// resolver is what one Resolve reads its names against. A security scheme and
// a header are found by scanning their list for each name, so a list whose
// every entry names another costs its length squared: only a mapping reads
// their replacements, and 8,000 such headers add under half a second to a
// compile, which an index per list would not repay. A model's members, whose
// messages every compile reads, are indexed by name once (members).
type resolver struct {
	c   lowering.Ctx
	p   lowering.Promoter
	doc *ir.Document
}

// fill reads the names own gives the replacements of its node, self, into dep,
// marking prov, each resolved by find, which returns how many entities a name
// matches and the entity, read only where that is one. A node that is not
// deprecated takes none, as a deprecation member beside none annotates
// nothing. The names fill the field all or none: one naming nothing, several
// entities or self is reported and leaves it unset, so no list a consumer reads
// holds less than the source names, or one entity picked from several.
func (r *resolver) fill(own ir.Unmodeled, dep *ir.Deprecation, prov *ir.Provenance, self ir.EntityRef,
	class string, find func(string) (ir.EntityRef, int),
) []ir.Diagnostic {
	names, at, diags := r.p.Replacements(own)
	if len(names) == 0 || dep == nil {
		return diags
	}
	refs := make([]ir.EntityRef, 0, len(names))
	for _, name := range names {
		ref, matched := find(name)
		switch {
		case matched == 0:
			return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names no %s, so it does not fill %s", name, class, lowering.TargetDeprecationReplacedBy))
		case matched > 1:
			return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names more than one %s, so it does not fill %s", name, class,
				lowering.TargetDeprecationReplacedBy))
		case ref == self:
			return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names the %s it sits on, so it does not fill %s", name, class, lowering.TargetDeprecationReplacedBy))
		}
		refs = append(refs, ref)
	}
	dep.ReplacedBy = refs
	prov.Inferred = ir.JoinInferred(prov.Inferred, lowering.ExtensionPromotionHeuristic)
	return diags
}

// scheme finds the security schemes the document declares under name.
func (r *resolver) scheme(name string) (ir.EntityRef, int) {
	var found ir.EntityRef
	matched := 0
	for id, s := range r.doc.Auth {
		if s.Name.Source == name {
			found, matched = ir.EntityRef{Auth: id}, matched+1
		}
	}
	return found, matched
}

// schema finds the schema components/schemas declares under name.
func (r *resolver) schema(name string) (ir.EntityRef, int) {
	id := r.c.IDs().NamedType(ids.Ptr("components", "schemas", name))
	if _, declared := r.doc.Types[id]; !declared {
		return ir.EntityRef{}, 0
	}
	return ir.EntityRef{Type: id}, 1
}

// typeDef fills td's own replacements, and those of a model's properties. The
// model's members are indexed once, where a property first names one.
func (r *resolver) typeDef(id ir.TypeID, td ir.TypeDef) []ir.Diagnostic {
	common := td.Common()
	diags := r.fill(common.Unmodeled, common.Deprecation, &common.Provenance, ir.EntityRef{Type: id}, "schema", r.schema)
	m, ok := td.(*ir.Model)
	if !ok {
		return diags
	}
	var byName map[string]ir.PropID
	member := func(name string) (ir.EntityRef, int) {
		if byName == nil {
			byName = members(r.doc.Types, m.ID)
		}
		if p, found := byName[name]; found {
			return ir.EntityRef{Property: p}, 1
		}
		return ir.EntityRef{}, 0
	}
	for i := range m.Properties {
		p := &m.Properties[i]
		self := ir.EntityRef{Property: p.ID}
		diags = append(diags, r.fill(p.Unmodeled, p.Deprecation, &p.Provenance, self, "property", member)...)
		r.message(p.Unmodeled, p.Deprecation, &p.Provenance, self, codeSpans, member)
	}
	return diags
}

// members indexes the members the model id flattens to by the name each
// travels under, which a flattening holds one member of.
func members(types ir.TypeRegistry, id ir.TypeID) map[string]ir.PropID {
	flat := ir.FlattenedProperties(types, id)
	byName := make(map[string]ir.PropID, len(flat.Properties))
	for i := range flat.Properties {
		p := flat.Properties[i].Property
		byName[p.EffectiveWireName()] = p.ID
	}
	return byName
}

// headers fills each header's replacements, among the headers beside it. A
// name matches a header in any case, as HTTP compares header names, so one
// matching two headers whose names differ only in case (#635) names both.
func (r *resolver) headers(hs []ir.Header) []ir.Diagnostic {
	sibling := func(name string) (ir.EntityRef, int) {
		var found ir.EntityRef
		matched := 0
		for i := range hs {
			if strings.EqualFold(hs[i].EffectiveWireName(), name) {
				found, matched = ir.EntityRef{Property: hs[i].ID}, matched+1
			}
		}
		return found, matched
	}
	diags := make([]ir.Diagnostic, 0, len(hs))
	for i := range hs {
		h := &hs[i].Property
		diags = append(diags, r.fill(h.Unmodeled, h.Deprecation, &h.Provenance, ir.EntityRef{Property: h.ID}, "header", sibling)...)
	}
	return diags
}
