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
// (docs/compilers/openapi.md#extension-promotion), a carrier naming none
// reading its node's (nodeKeys). promoter is the run's promoter, and
// compiledDocumentView the document, which is whole.
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
// marking prov, each resolved by find (resolve).
func (r *resolver) fill(own ir.Unmodeled, dep *ir.Deprecation, prov *ir.Provenance, self ir.EntityRef,
	class string, find func(string) (ir.EntityRef, int),
) []ir.Diagnostic {
	names, at, diags := r.p.Replacements(own)
	return append(diags, r.resolve(names, at, dep, prov, self, class, find)...)
}

// resolve fills dep with the entities names, written at at, name, marking
// prov, each resolved by find, which returns how many entities a name matches
// and the entity, read only where that is one. A node that is not deprecated
// takes none, as a deprecation member beside none annotates nothing. The names
// fill the field all or none: one naming nothing, several entities or self is
// reported and leaves it unset, so no list a consumer reads holds less than
// the source names, or one entity picked from several.
func (r *resolver) resolve(names []string, at ir.Provenance, dep *ir.Deprecation, prov *ir.Provenance, self ir.EntityRef,
	class string, find func(string) (ir.EntityRef, int),
) []ir.Diagnostic {
	if len(names) == 0 || dep == nil {
		return nil
	}
	refs := make([]ir.EntityRef, 0, len(names))
	for _, name := range names {
		ref, matched := find(name)
		switch {
		case matched == 0:
			return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names no %s, so it does not fill %s", name, class, promotion.TargetDeprecationReplacedBy)}
		case matched > 1:
			return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names more than one %s, so it does not fill %s", name, class,
				promotion.TargetDeprecationReplacedBy)}
		case ref == self:
			return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
				"replacement %q names the %s it sits on, so it does not fill %s", name, class, promotion.TargetDeprecationReplacedBy)}
		}
		refs = append(refs, ref)
	}
	dep.ReplacedBy = refs
	prov.Inferred = ir.JoinInferred(prov.Inferred, promotion.ExtensionPromotionHeuristic)
	return nil
}

// fillCarrier fills the replacements of carrier, a property, a parameter or a
// header, as fill does, each resolved by find, unless it reads them off the
// keys of the node its own position built (nodeKeys). Those are a schema's
// keys, so each name resolves as a schema's, and the node, whose keys Resolve
// reads too, reports any it misreads, once.
func (r *resolver) fillCarrier(carrier deprecated, class string, find func(string) (ir.EntityRef, int)) []ir.Diagnostic {
	if id, keys, ok := r.nodeKeys(carrier); ok {
		names, at, _ := r.p.Replacements(keys)
		return r.resolve(names, at, carrier.dep, carrier.prov, ir.EntityRef{Type: id}, "schema", r.schema)
	}
	return r.fill(carrier.own, carrier.dep, carrier.prov, carrier.self, class, find)
}

// nodeKeys returns the ID and the keys of the node carrier's type names where
// carrier reads its replacements there: carrier is deprecated, its own keys
// write none, and the node, which writes some, is the one its declaration
// built (builtAt, or headerSchemaAt for a header) and holds no deprecation,
// its schema's being the carrier's
// (docs/compilers/openapi.md#docs-and-deprecation). Replacement inference asks
// it too, so a message is not read over those keys. Reading the IR alone, it
// cannot tell that node from one keeping its notes because a reference names
// it, so a carrier its own object deprecates reads either's keys.
func (r *resolver) nodeKeys(carrier deprecated) (ir.TypeID, ir.Unmodeled, bool) {
	if carrier.dep == nil || r.p.Writes(carrier.own, promotion.TargetDeprecationReplacedBy) {
		return "", nil, false
	}
	td := r.w.Types[carrier.typeRef.Target]
	if ir.IsNilTypeDef(td) {
		return "", nil, false
	}
	node := td.Common()
	built := builtAt(*carrier.prov, node.Provenance) || carrier.header && headerSchemaAt(node.Provenance)
	if node.Deprecation != nil || !built || !r.p.Writes(node.Unmodeled, promotion.TargetDeprecationReplacedBy) {
		return "", nil, false
	}
	return carrier.typeRef.Target, node.Unmodeled, true
}

// builtAt reports whether node, a type node's provenance, is the position of
// the carrier at carrier or of that carrier's schema: a property's node is
// written where the property is, and an inline parameter's or header's, or a
// parameter's a Reference Object reaches, beneath it, at `schema` or
// `content/<media type>/schema`.
func builtAt(carrier, node ir.Provenance) bool {
	if carrier.Source == ir.NoSource || carrier.Source != node.Source {
		return false
	}
	at := node.Pointer
	switch {
	case at == carrier.Pointer:
		return true
	case at.LastToken() != "schema":
		return false
	case at.Parent() == carrier.Pointer:
		return true
	default:
		return at.Parent().Parent().LastToken() == "content" && at.Parent().Parent().Parent() == carrier.Pointer
	}
}

// headerSchemaAt reports whether node, a type node's provenance, is a Header
// Object's own schema position, `<header>/schema` or
// `<header>/content/<media type>/schema` beneath a `headers` map, in any
// document. A header a Reference Object reaches keeps the provenance of its
// use, while its schema is built at its declaration, which may lie in another
// document (operation.lowerHeaders), so builtAt's comparison refuses its node.
func headerSchemaAt(node ir.Provenance) bool {
	at := node.Pointer
	if node.Source == ir.NoSource || at.LastToken() != "schema" {
		return false
	}
	declaration := at.Parent()
	if declaration.Parent().LastToken() == "content" {
		declaration = declaration.Parent().Parent()
	}
	return declaration.Parent().LastToken() == "headers"
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
		diags = append(diags, r.fillCarrier(deprecated{property.Unmodeled, property.Deprecation, &property.Provenance,
			ir.EntityRef{Property: property.ID}, property.Type, false}, "property", member)...)
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
		diags = append(diags, r.fillCarrier(deprecated{property.Unmodeled, property.Deprecation, &property.Provenance,
			ir.EntityRef{Property: property.ID}, property.Type, true}, "header", sibling)...)
	}
	return diags
}
