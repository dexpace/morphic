// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// Resolve fills, on every deprecated node of doc whose own keys name its
// replacements, the entities those names resolve to, each named as the document
// names an entity of the node's class: an operation by its operationId, a
// parameter by `<in>.<name>` or its name within its operation, a header by name
// among the headers beside it, a property by name within its model, a schema by
// its components name, and a security scheme by its key
// (docs/compilers/openapi.md#extension-promotion). promoter is the run's
// promoter, and compiledDocumentView the document, which is whole.
func Resolve(promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView) []ir.Diagnostic {
	resolverValue := resolver{p: promoter, w: compiledDocumentView}
	if !resolverValue.p.Maps(promotion.TargetDeprecationReplacedBy) {
		return nil
	}
	diags := resolverValue.service()
	for _, id := range slices.Sorted(maps.Keys(compiledDocumentView.Types)) {
		if td := compiledDocumentView.Types[id]; !ir.IsNilTypeDef(td) {
			diags = append(diags, resolverValue.typeDef(id, td)...)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(compiledDocumentView.Auth)) {
		scheme := compiledDocumentView.Auth[id]
		diags = append(diags, resolverValue.fill(scheme.Unmodeled, scheme.Deprecation, &scheme.Provenance,
			ir.EntityRef{Auth: id}, "security scheme", resolverValue.scheme)...)
		compiledDocumentView.Auth[id] = scheme
	}
	return diags
}

// resolver is what one Resolve or Infer reads its names against. A security
// scheme and a header are found by scanning their list for each name, so a list
// whose every entry names another costs its length squared: only a mapping
// reads their replacements, and 8,000 such headers add under half a second to a
// compile, which an index per list would not repay. A model's member, which
// messages every compile reads name, is read by its name alone
// (compileddoc.CompiledDocumentView.Member), which indexes a model's own
// properties once.
type resolver struct {
	p promotion.Promoter
	w *compileddoc.CompiledDocumentView
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
				"replacement %q names no %s, so it does not fill %s", name, class, promotion.TargetDeprecationReplacedBy))
		case matched > 1:
			return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names more than one %s, so it does not fill %s", name, class,
				promotion.TargetDeprecationReplacedBy))
		case ref == self:
			return append(diags, diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names the %s it sits on, so it does not fill %s", name, class, promotion.TargetDeprecationReplacedBy))
		}
		refs = append(refs, ref)
	}
	dep.ReplacedBy = refs
	prov.Inferred = ir.JoinInferred(prov.Inferred, promotion.ExtensionPromotionHeuristic)
	return diags
}

// scheme finds the security schemes the document declares under name.
func (r *resolver) scheme(name string) (ir.EntityRef, int) {
	var found ir.EntityRef
	matched := 0
	for id, authScheme := range r.w.Auth {
		if authScheme.Name.Source == name {
			found, matched = ir.EntityRef{Auth: id}, matched+1
		}
	}
	return found, matched
}

// schema finds the schema the source's components/schemas declares under name.
func (r *resolver) schema(name string) (ir.EntityRef, int) {
	id := ids.DocumentIDNamespace{}.NamedType(ids.Ptr("components", "schemas", name))
	if _, declared := r.w.Types[id]; !declared {
		return ir.EntityRef{}, 0
	}
	return ir.EntityRef{Type: id}, 1
}

// typeDef fills td's own replacements, and those of a model's properties.
func (r *resolver) typeDef(id ir.TypeID, td ir.TypeDef) []ir.Diagnostic {
	common := td.Common()
	diags := r.fill(common.Unmodeled, common.Deprecation, &common.Provenance, ir.EntityRef{Type: id}, "schema", r.schema)
	model, ok := td.(*ir.Model)
	if !ok {
		return diags
	}
	member := r.memberFinder(model.ID)
	for i := range model.Properties {
		property := &model.Properties[i]
		diags = append(diags, r.fill(property.Unmodeled, property.Deprecation, &property.Provenance, ir.EntityRef{Property: property.ID}, "property", member)...)
	}
	return diags
}

// memberFinder returns what finds a member of the model id by name: one of
// the members the model flattens to, by the name it travels under, which a
// flattening holds one member of (compileddoc.CompiledDocumentView.Member).
func (r *resolver) memberFinder(id ir.TypeID) func(string) (ir.EntityRef, int) {
	return func(name string) (ir.EntityRef, int) {
		if property, found := r.w.Member(id, name); found {
			return ir.EntityRef{Property: property.ID}, 1
		}
		return ir.EntityRef{}, 0
	}
}

// headers fills each header's replacements, among the headers beside it. A
// name matches a header in any case, as HTTP compares header names, so one
// matching two headers whose names differ only in case names both, a repeat
// reported where its map writes it (docs/compilers/openapi.md#header-style).
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
		property := &hs[i].Property
		diags = append(diags, r.fill(property.Unmodeled, property.Deprecation, &property.Provenance, ir.EntityRef{Property: property.ID}, "header", sibling)...)
	}
	return diags
}
