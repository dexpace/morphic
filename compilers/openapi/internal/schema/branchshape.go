// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// shapelessKeywords are the keywords a union branch, or a property one
// declares, may write whatever they hold and declare no shape: each documents
// the instance, names the schema or holds schemas it does not apply, or narrows
// what the body already shapes. `nullable` is among them because admitting null
// or not is no shape here, as admission's kindsWithin reads it. Every other
// keyword declares shape unless the body's reading holds it
// (docs/compilers/openapi.md#union-beside-structure).
var shapelessKeywords = slices.Concat(
	[]string{"title", "description", "$comment", "deprecated", "examples", "example", "externalDocs"},
	[]string{"$anchor", "$dynamicAnchor", "$defs"},
	[]string{
		"required", "nullable", "not", "if", "then", "else", "dependentSchemas", "dependentRequired",
		"propertyNames", "contains", "minContains", "maxContains", "unevaluatedItems",
	},
	valueConstraintKeywords, collectionBoundKeywords,
)

// bodyReading is what a union's branches are judged against: what the body's
// own keywords and its allOf admit, its unions apart, and every declaration of
// each property it makes. One admission reads all of it, so a body's branches
// share one budget.
type bodyReading struct {
	w     admission
	held  admitted
	decls map[string][]*oas3.JSONSchema[oas3.Referenceable]
}

// readBody reads s, the schema writing a union, for its branches to be judged
// against.
func readBody(c lowering.Ctx, s *oas3.Schema) *bodyReading {
	r := &bodyReading{w: newAdmission(c)}
	r.held = r.w.written(s)
	for _, b := range s.GetAllOf() {
		r.held = r.held.and(r.w.whole(b))
	}
	r.decls = r.w.declarations(s)
	return r
}

// unionBranchesDeclareShape reports whether any oneOf/anyOf branch declares
// data shape s does not. When none does — `oneOf: [{required: [a]},
// {required: [b]}]`, the commonest instance — the union narrows what the body
// accepts without reshaping it, which is validation logic
// (docs/ir/boundaries.md#validation-only-constructs). A $ref branch always
// declares shape, so the body is read only for an inline one, and a reading its
// budget cut short says nothing, so it counts as one that does.
func unionBranchesDeclareShape(c lowering.Ctx, s *oas3.Schema) bool {
	var r *bodyReading
	declares := func(b *oas3.JSONSchema[oas3.Referenceable]) bool {
		if isRefBranch(b) {
			return true
		}
		if r == nil {
			r = readBody(c, s)
		}
		return branchDeclaresShape(r, b)
	}
	found := slices.ContainsFunc(s.GetOneOf(), declares) || slices.ContainsFunc(s.GetAnyOf(), declares)
	return found || r != nil && r.w.incomplete()
}

// branchDeclaresShape reports whether the inline union branch b declares data
// shape beside the body r read. A boolean never does, and any other does where
// one keyword it writes does: bare or beside a repeated `type`, a keyword is
// judged alike, so restating the body changes no verdict. The keys are read off
// the raw node, which holds the keywords the model has no field for
// (docs/parser-library.md#unmodeled-keywords).
func branchDeclaresShape(r *bodyReading, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	s := b.GetSchema()
	if s == nil {
		return false
	}
	return slices.ContainsFunc(nodeview.Pairs(s.GetRootNode()), func(p nodeview.Pair) bool {
		return r.keywordDeclaresShape(s, p.Key)
	})
}

// keywordDeclaresShape reports whether s, a union branch, declares shape by
// writing key. A `type` naming only null contributes the null and nothing else,
// and `properties` declares none where each property restates the body's; any
// other keyword declares none where it is shapeless or every value the body
// admits holds to it, so a oneOf or an allOf in a branch always declares some.
func (r *bodyReading) keywordDeclaresShape(s *oas3.Schema, key string) bool {
	switch {
	case shapeless(s, key):
		return false
	case key == "properties":
		return !r.restatesProperties(s)
	case key == "type" && len(effectiveTypes(s)) == 0:
		return false
	default:
		return !r.w.holds(r.held, s, key)
	}
}

// shapeless reports whether s's keyword key declares no shape whatever it
// holds: a null that reads as the keyword left out, an extension, a keyword
// shapelessKeywords names, or an unevaluatedProperties other than false, which
// is validation logic where false closes the object
// (docs/ir/boundaries.md#validation-only-constructs).
func shapeless(s *oas3.Schema, key string) bool {
	if annotation.WritesKeywordLeftOut(s, key) || strings.HasPrefix(key, "x-") ||
		slices.Contains(shapelessKeywords, key) {
		return true
	}
	return key == "unevaluatedProperties" && !annotation.IsFalseSchema(s.GetUnevaluatedProperties())
}

// restatesProperties reports whether each property s, a union branch,
// declares restates one of the body's declarations of it. Each declaration is
// compared alone, so a property restating what two say together, a `type` in
// one and a `format` in another, still declares shape (#1407).
func (r *bodyReading) restatesProperties(s *oas3.Schema) bool {
	for name, p := range s.GetProperties().All() {
		restates := func(d *oas3.JSONSchema[oas3.Referenceable]) bool { return r.restatesDeclaration(d, p) }
		if !slices.ContainsFunc(r.decls[name], restates) {
			return false
		}
	}
	return true
}

// restatesDeclaration reports whether p, a union branch's declaration of a
// property, says nothing of shape d, a declaration of it the body makes, does
// not. A boolean p says nothing or only forbids the property. Otherwise the two
// are written alike, or agree on a $ref first, since admission reads what
// stands beside one as 3.1 does, and then d admits nothing, or every other key
// p's raw node writes (docs/parser-library.md#unmodeled-keywords) is shapeless
// or holds for every value d admits. holds reads no `properties`, so one p
// nests restates only written alike (#1407).
func (r *bodyReading) restatesDeclaration(d, p *oas3.JSONSchema[oas3.Referenceable]) bool {
	if p.IsBool() || r.w.sameText(d, p) {
		return true
	}
	if !r.sameReference(d, p) {
		return false
	}
	held := r.w.whole(d)
	if !held.reachable() {
		return true
	}
	ps := p.GetSchema()
	return !slices.ContainsFunc(nodeview.Pairs(ps.GetRootNode()), func(kv nodeview.Pair) bool {
		return kv.Key != "$ref" && !shapeless(ps, kv.Key) && !r.w.holds(held, ps, kv.Key)
	})
}

// sameReference reports whether d and p agree on a $ref: neither writes one,
// or both land on one schema. OpenAPI 3.0 sets aside what stands beside a
// $ref, so a branch leaving out the body's, or adding one, reads its other
// keywords against another schema. Landings are compared, not spellings, since
// a declaration the body composes may be written where a $ref reads elsewhere.
func (r *bodyReading) sameReference(d, p *oas3.JSONSchema[oas3.Referenceable]) bool {
	writes := func(js *oas3.JSONSchema[oas3.Referenceable]) bool { return resolve.IsRefSite(js, js.GetSchema()) }
	if writes(d) != writes(p) {
		return false
	}
	if !writes(p) {
		return true
	}
	at := r.w.c.Bindings().Declared(p)
	return at != nil && at == r.w.c.Bindings().Declared(d)
}
