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
// its properties and elements, read once a branch nests a claim. One admission
// reads all of it, so a body's branches share one budget, and each place they
// restate is read once, so repeating a restatement reads no more of the body.
// What each branch writes is still read within it, an enum's members included.
type bodyReading struct {
	w     admission
	body  *oas3.Schema
	held  admitted
	decls *declarationSet
}

// readBody reads s, the schema writing a union, for its branches to be judged
// against.
func readBody(c lowering.Ctx, s *oas3.Schema) *bodyReading {
	r := &bodyReading{w: newAdmission(c), body: s}
	r.held = r.w.written(s, r.w.nullable)
	for _, b := range s.GetAllOf() {
		r.held = r.held.and(r.w.whole(b))
	}
	return r
}

// declared returns what the body declares of its properties and elements,
// read the first time a branch nests a claim, so a verdict needing none spends
// none of the budget on them.
func (r *bodyReading) declared() declarationSet {
	if r.decls == nil {
		d := r.w.declarations(r.body)
		r.decls = &d
	}
	return *r.decls
}

// unionBranchesDeclareShape reports whether any oneOf/anyOf branch declares
// data shape s does not. When none does — `oneOf: [{required: [a]},
// {required: [b]}]`, the commonest instance — the union narrows what the body
// accepts without reshaping it, which is validation logic
// (docs/ir/boundaries.md#validation-only-constructs). A $ref branch always
// declares shape, so the body is read only once an inline branch needs it, and
// a reading its budget cut short says nothing, so it counts as one that does,
// even where the body's own keywords would settle it (GitHub #1423).
func unionBranchesDeclareShape(c lowering.Ctx, s *oas3.Schema) bool {
	return setsDeclareShape(c, s, s.GetOneOf(), s.GetAnyOf())
}

// setsDeclareShape is unionBranchesDeclareShape for the branches of sets, a
// union on an allOf branch of s among them, judged against s as the body.
func setsDeclareShape(c lowering.Ctx, s *oas3.Schema, sets ...[]*oas3.JSONSchema[oas3.Referenceable]) bool {
	var r *bodyReading
	body := func() *bodyReading {
		if r == nil {
			r = readBody(c, s)
		}
		return r
	}
	declares := func(b *oas3.JSONSchema[oas3.Referenceable]) bool {
		return isRefBranch(b) || branchDeclaresShape(body, b)
	}
	found := slices.ContainsFunc(sets, func(set []*oas3.JSONSchema[oas3.Referenceable]) bool {
		return slices.ContainsFunc(set, declares)
	})
	return found || r != nil && r.w.incomplete()
}

// branchDeclaresShape reports whether the inline union branch b declares data
// shape beside the body body reads. A boolean never does, and any other does
// where one keyword it writes does: bare or beside a repeated `type`, a keyword
// is judged alike, so restating the body changes no verdict. A branch writing
// no key judged against the body, `required` alone the commonest, leaves it
// unread.
func branchDeclaresShape(body func() *bodyReading, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	s := b.GetSchema()
	if s == nil {
		return false
	}
	writesJudged := slices.ContainsFunc(nodeview.Pairs(s.GetRootNode()), func(kv nodeview.Pair) bool {
		return judged(s, kv.Key)
	})
	if !writesJudged {
		return false
	}
	r := body()
	return !r.restates(claim{written: b, held: r.held})
}

// judged reports whether s's key is judged against what the body says: it is
// neither shapeless nor a `type` naming only null, which contributes the null.
func judged(s *oas3.Schema, key string) bool {
	return !shapeless(s, key) && (key != "type" || len(effectiveTypes(s)) > 0)
}

// claim is a schema a branch writes, for itself, a property or an element, or
// as an allOf conjunct of one, with what the body admits there and the place
// the body declares it, nil for the branch itself, which restates the body
// whole.
type claim struct {
	written *oas3.JSONSchema[oas3.Referenceable]
	held    admitted
	at      *place
}

// nested is a property or an element a claim writes, and the place the body
// declares it, nil where the body declares none.
type nested struct {
	written *oas3.JSONSchema[oas3.Referenceable]
	at      *place
}

// restates reports whether top, and each conjunct, property and element it
// writes at any depth, says nothing of shape the body does not. They are judged
// off a stack, so the walk adds no recursion cycle, and it visits the branch's
// tree once: bounded by the size of a tree the load phase holds
// (docs/compilers/openapi.md#source-budgets), while what each restates is read
// within admission's budget. Comparing a nested schema's text renders it again
// at each level, so restating one d levels deep renders about d squared nodes.
func (r *bodyReading) restates(top claim) bool {
	claims := []claim{top}
	for len(claims) > 0 {
		c := claims[len(claims)-1]
		claims = claims[:len(claims)-1]
		conjuncts, inner, ok := r.keysRestate(c)
		if !ok {
			return false
		}
		for _, j := range conjuncts {
			claims = append(claims, claim{written: j, held: c.held, at: c.at})
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
// does not, and returns the allOf conjuncts, which restate what c does, and the
// properties and element it writes, for restates to judge. A key declares none
// where it is not judged, a `$ref` landing where c's declarations compose, or a
// `type`, `format`, `enum` or `const` every value c.held admits holds to. A
// boolean conjunct says nothing or only forbids. Keys are read off the raw
// node, which holds the keywords the model has no field for
// (docs/parser-library.md#unmodeled-keywords).
func (r *bodyReading) keysRestate(c claim) ([]*oas3.JSONSchema[oas3.Referenceable], []nested, bool) {
	s := c.written.GetSchema()
	if s == nil {
		return nil, nil, true
	}
	var inner []nested
	var conjuncts []*oas3.JSONSchema[oas3.Referenceable]
	for _, kv := range nodeview.Pairs(s.GetRootNode()) {
		switch {
		case !judged(s, kv.Key):
		case kv.Key == "allOf":
			conjuncts = s.GetAllOf()
		case kv.Key == "properties":
			decls := r.declarationsAt(c)
			for name, p := range s.GetProperties().All() {
				inner = append(inner, nested{written: p, at: decls.property(name)})
			}
		case kv.Key == "items":
			if _, listed := listedItems(s); listed {
				// A tuple, or a malformed list, which no element the body declares restates.
				return nil, nil, false
			}
			inner = append(inner, nested{written: s.GetItems(), at: r.declarationsAt(c).items})
		case kv.Key == "$ref":
			if !r.lands(c) {
				return nil, nil, false
			}
		case !r.w.holds(c.held, s, kv.Key):
			return nil, nil, false
		}
	}
	return conjuncts, inner, true
}

// declarationsAt returns what the body declares of what c nests: its own
// declarations for the branch itself, and what c's place declares otherwise,
// each read the first time a claim nests something.
func (r *bodyReading) declarationsAt(c claim) declarationSet {
	if c.at == nil {
		return r.declared()
	}
	return r.w.innerAt(c.at)
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
	if n.at == nil {
		return claim{}, false, false
	}
	alike := func(d *oas3.JSONSchema[oas3.Referenceable]) bool { return r.w.sameText(d, n.written) }
	if n.written.IsBool() || slices.ContainsFunc(n.at.decls, alike) {
		return claim{}, false, true
	}
	held := r.w.heldAt(n.at)
	if !held.reachable() {
		return claim{}, false, true
	}
	return claim{written: n.written, held: held, at: n.at}, true, true
}

// lands reports whether the $ref c writes lands on a schema c's declarations
// compose, through allOf conjuncts and $refs, so it names what they already
// imply. The branch itself restates no declaration: unionBranchesDeclareShape
// settles a $ref branch, and a $ref conjunct of one declares shape as it does.
func (r *bodyReading) lands(c claim) bool {
	if c.at == nil {
		return false
	}
	at := r.w.c.Bindings().Declared(c.written)
	return at != nil && slices.Contains(r.w.innerAt(c.at).composed, at)
}

// shapeless reports whether s's keyword key declares no shape whatever it
// holds: a null that reads as the keyword left out, an extension, a keyword
// shapelessKeywords names, or an unevaluatedProperties other than false, which
// is validation logic where false closes the object
// (docs/ir/boundaries.md#validation-only-constructs).
func shapeless(s *oas3.Schema, key string) bool {
	if annotation.WritesKeywordLeftOut(s, key) || strings.HasPrefix(key, "x-") || slices.Contains(shapelessKeywords, key) {
		return true
	}
	return key == "unevaluatedProperties" && !annotation.IsFalseSchema(s.GetUnevaluatedProperties())
}
