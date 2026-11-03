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
// own keywords and its allOf admit, its unions apart, and what it declares of
// its properties and elements. One admission reads all of it, so a body's
// branches share one budget.
type bodyReading struct {
	w     admission
	held  admitted
	decls declarationSet
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
// judged alike, so restating the body changes no verdict.
func branchDeclaresShape(r *bodyReading, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	if b.GetSchema() == nil {
		return false
	}
	return !r.restates(claim{written: b, held: r.held, decls: r.decls})
}

// claim is a schema a branch writes, for itself or for a property or element,
// with what the body admits there, what the body declares of its properties and
// elements there, and the declarations a $ref it writes may land with.
type claim struct {
	written *oas3.JSONSchema[oas3.Referenceable]
	held    admitted
	decls   declarationSet
	refs    []*oas3.JSONSchema[oas3.Referenceable]
}

// nested is a property or an element a claim writes, and the body's
// declarations of it.
type nested struct {
	written *oas3.JSONSchema[oas3.Referenceable]
	decls   []*oas3.JSONSchema[oas3.Referenceable]
}

// restates reports whether top, and each property and element it writes at any
// depth, says nothing of shape the body does not. They are judged off a stack,
// and each claim beyond top reads the declarations it restates, so admission's
// budget bounds the walk, which adds no recursion cycle.
func (r *bodyReading) restates(top claim) bool {
	claims := []claim{top}
	for len(claims) > 0 {
		c := claims[len(claims)-1]
		claims = claims[:len(claims)-1]
		inner, ok := r.keysRestate(c)
		if !ok {
			return false
		}
		for _, n := range inner {
			sub, open, ok := r.claimOf(n)
			if !ok {
				return false
			}
			if open {
				claims = append(claims, sub)
			}
		}
	}
	return true
}

// keysRestate reports whether each key c writes says nothing of shape the body
// does not, and returns the properties and element it writes for restates to
// judge. A key declares none where it is shapeless, a `type` naming only null,
// which contributes the null, a `$ref` landing where one of c's declarations
// does, or a `type`, `format`, `enum` or `const` every value c.held admits holds
// to. Keys are read off the raw node, which holds the keywords the model has no
// field for (docs/parser-library.md#unmodeled-keywords).
func (r *bodyReading) keysRestate(c claim) ([]nested, bool) {
	s := c.written.GetSchema()
	var inner []nested
	for _, kv := range nodeview.Pairs(s.GetRootNode()) {
		switch {
		case shapeless(s, kv.Key), kv.Key == "type" && len(effectiveTypes(s)) == 0:
		case kv.Key == "properties":
			for name, p := range s.GetProperties().All() {
				inner = append(inner, nested{written: p, decls: c.decls.properties[name]})
			}
		case kv.Key == "items":
			inner = append(inner, nested{written: s.GetItems(), decls: c.decls.items})
		case kv.Key == "$ref":
			if !r.lands(c) {
				return nil, false
			}
		case !r.w.holds(c.held, s, kv.Key):
			return nil, false
		}
	}
	return inner, true
}

// claimOf returns the claim n makes, and false for open where n says nothing
// whatever its keys: a boolean says nothing or only forbids what it is written
// for, a declaration written alike against one base says the same, and
// declarations admitting nothing together leave nothing to say. ok is false
// where the body declares nothing there, so n is new shape. What n restates is
// what its declarations say together, each read as admission reads it, the
// keywords beside a $ref conjoining with it at every version
// (docs/compilers/openapi.md#ref-use-site).
func (r *bodyReading) claimOf(n nested) (sub claim, open, ok bool) {
	if len(n.decls) == 0 {
		return claim{}, false, false
	}
	alike := func(d *oas3.JSONSchema[oas3.Referenceable]) bool { return r.w.sameText(d, n.written) }
	if n.written.IsBool() || slices.ContainsFunc(n.decls, alike) {
		return claim{}, false, true
	}
	held := r.w.whole(n.decls[0])
	for _, d := range n.decls[1:] {
		held = held.and(r.w.whole(d))
	}
	if !held.reachable() {
		return claim{}, false, true
	}
	return claim{written: n.written, held: held, decls: r.w.declarationsOf(n.decls), refs: n.decls}, true, true
}

// lands reports whether the $ref c writes lands on a schema one of the
// declarations it restates lands on, so it names what they already name. A
// branch writes none here: unionBranchesDeclareShape settles a $ref branch.
func (r *bodyReading) lands(c claim) bool {
	at := r.w.c.Bindings().Declared(c.written)
	return at != nil && slices.ContainsFunc(c.refs, func(d *oas3.JSONSchema[oas3.Referenceable]) bool {
		return r.w.c.Bindings().Declared(d) == at
	})
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
