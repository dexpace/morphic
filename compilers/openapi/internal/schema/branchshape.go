// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
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
// its properties and elements, read once a branch nests a claim. One admissionReader
// reads all of it, so a body's branches share one budget, and each place they
// restate is read once, so repeating a restatement reads no more of the body.
// What each branch writes is still read within it, an enum's members included.
type bodyReading struct {
	w     admissionReader
	body  *statement.Schema
	held  admitted
	decls *declarationSet
	// own marks a reading of the body's own keywords and declarations alone,
	// nothing it composes (readOwnBody).
	own bool
}

// readBody reads s, the schema at the position of a union holder writes, for
// the union's branches to be judged against: holder's own keywords without its
// oneOf and anyOf, as where s itself is the holder, so a union is judged by the
// rest whichever entry writes it. Where holder is an entry, another inline
// entry's union is no body either, as two unions beside the allOf judge each
// other by neither.
func readBody(c lowering.Ctx, s, holder *statement.Schema) *bodyReading {
	r := &bodyReading{w: newAdmissionReader(c), body: s}
	r.held = writtenLowered(&r.w, s, r.w.nullable)
	for _, b := range conjunctsIn(c, s) {
		if otherEntryUnion(b, s, holder) {
			r.held = r.held.and(writtenLowered(&r.w, b.Schema, r.w.nullable))
			continue
		}
		r.held = r.held.and(conjunctWithout(&r.w, b, holder))
	}
	return r
}

// otherEntryUnion reports whether b, a conjunct of s, is an inline entry
// writing a union of its own beside holder, another entry of s, which neither
// it nor a $ref it writes beside its keywords holds.
func otherEntryUnion(b *statement.Statement, s, holder *statement.Schema) bool {
	return holder != s && b.Ref == nil && b.BesideRef == nil && b.Schema != holder && declaresUnion(b.Schema) &&
		!slices.ContainsFunc(b.Entries, func(e statement.Entry) bool { return e.Schema == holder })
}

// readOwnBody reads only what s itself writes: its own keywords, and its own
// properties and items as the declarations of what a branch nests, each read
// whole as readBody reads it. It spends a budget of its own, so what it reads
// the whole reading does not pay for.
func readOwnBody(c lowering.Ctx, s *statement.Schema) *bodyReading {
	r := &bodyReading{w: newAdmissionReader(c), body: s, own: true}
	r.held = writtenLowered(&r.w, s, r.w.nullable)
	return r
}

// declared returns what the body declares of its properties and elements,
// read the first time a branch nests a claim, so a verdict needing none spends
// none of the budget on them.
func (r *bodyReading) declared() declarationSet {
	if r.decls == nil {
		d := newDeclarationSet()
		if r.own {
			d.add(r.w.c.Statements(), r.body)
		} else {
			d = declarationsOf(&r.w, r.body)
		}
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
// a reading its budget cut short says nothing, so it counts as one that does.
func unionBranchesDeclareShape(c lowering.Ctx, s *statement.Schema) bool {
	choice := s.ChoiceKeywords()
	return setsDeclareShape(c, s, s, choice.OneOf, choice.AnyOf)
}

// setsDeclareShape is unionBranchesDeclareShape for the branches of sets,
// written by holder, s or an allOf entry of it, judged against s as the body,
// read without holder's unions; a nil holder reads the conjunction whole. The
// body's own reading settles first each branch restating what s itself writes
// (ownRestatements), and only the rest are judged against the whole reading,
// which they would restate under too, as more declarations admit less.
func setsDeclareShape(c lowering.Ctx, s, holder *statement.Schema, sets ...[]statement.Sub) bool {
	branches := statementsOf(c, slices.Concat(sets...))
	if slices.ContainsFunc(branches, func(b *statement.Statement) bool { return referenceOf(c, b) != nil }) {
		return true
	}
	settled := ownRestatements(c, s, branches)
	var r *bodyReading
	body := func() *bodyReading {
		if r == nil {
			r = readBody(c, s, holder)
		}
		return r
	}
	found := false
	for i, b := range branches {
		if (settled == nil || !settled[i]) && branchDeclaresShape(c.Statements(), body, b) {
			found = true
			break
		}
	}
	return found || r != nil && r.w.incomplete()
}

// ownRestatements marks, by index, the inline branches of branches that
// restate s read alone (readOwnBody), judged all, and none where that reading
// ran out: which branches it settled before running out would follow their
// order, and with it what the whole reading is left to read (GitHub #1423).
func ownRestatements(c lowering.Ctx, s *statement.Schema, branches []*statement.Statement) []bool {
	var r *bodyReading
	body := func() *bodyReading {
		if r == nil {
			r = readOwnBody(c, s)
		}
		return r
	}
	settled := make([]bool, len(branches))
	for i, b := range branches {
		if !branchDeclaresShape(c.Statements(), body, b) {
			settled[i] = true
		}
	}
	if r != nil && r.w.incomplete() {
		return nil
	}
	return settled
}

// branchDeclaresShape reports whether the inline union branch b declares data
// shape beside the body body reads, its schemas read from t. A boolean never does, and any other does
// where one keyword it writes does: bare or beside a repeated `type`, a keyword
// is judged alike, so restating the body changes no verdict. A branch writing
// no key judged against the body, `required` alone the commonest, leaves it
// unread.
func branchDeclaresShape(t *statement.Table, body func() *bodyReading, b *statement.Statement) bool {
	if b.Truth != statement.Stated {
		return false
	}
	writesJudged := slices.ContainsFunc(b.Keys, func(k statement.Key) bool {
		return judged(t, b.Schema, k.Name)
	})
	if !writesJudged {
		return false
	}
	r := body()
	return !r.restates(claim{written: b, held: r.held})
}

// judged reports whether s's key is judged against what the body says: it is
// neither shapeless, its schemas read from t, nor a `type` naming only null,
// which contributes the null.
func judged(t *statement.Table, s *statement.Schema, key string) bool {
	return !shapeless(t, s, key) && (key != "type" || len(effectiveTypes(s)) > 0)
}

// claim is a schema a branch writes, for itself, a property or an element, or
// as an allOf conjunct of one, with what the body admits there and the place
// the body declares it, nil for the branch itself, which restates the body
// whole.
type claim struct {
	written *statement.Statement
	held    admitted
	at      *place
}

// nested is a property or an element a claim writes, and the place the body
// declares it, nil where the body declares none.
type nested struct {
	written *statement.Statement
	at      *place
}

// restates reports whether top, and each conjunct, property and element it
// writes at any depth, says nothing of shape the body does not. They are judged
// off a stack, so the walk adds no recursion cycle, and it visits the branch's
// tree once: bounded by the size of a tree the load phase holds
// (docs/compilers/openapi.md#source-budgets), while what each restates is read
// within the admissionReader's budget. Comparing a nested schema's text renders it again
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
			claims = append(claims, claim{written: r.w.c.Statements().Sub(j), held: c.held, at: c.at})
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
// boolean conjunct says nothing or only forbids. Keys are the statement's,
// which hold the keywords the model has no field for
// (docs/parser-library.md#unmodeled-keywords).
func (r *bodyReading) keysRestate(c claim) ([]statement.Sub, []nested, bool) {
	if !stated(c.written.Schema) {
		return nil, nil, true
	}
	t := r.w.c.Statements()
	var inner []nested
	var conjuncts []statement.Sub
	for _, k := range c.written.Keys {
		switch {
		case !judged(t, c.written.Schema, k.Name):
		case k.Name == "allOf":
			conjuncts = c.written.AllOf
		case k.Name == "properties":
			decls := r.declarationsAt(c)
			for _, p := range c.written.ObjectKeywords().Properties {
				inner = append(inner, nested{written: t.Sub(p), at: decls.property(t, p.Name)})
			}
		case k.Name == "items":
			if _, listed := listedItems(c.written.Schema); listed {
				// A tuple, or a malformed list, which no element the body declares restates.
				return nil, nil, false
			}
			inner = append(inner, nested{written: t.Sub(c.written.ArrayKeywords().Items), at: r.declarationsAt(c).items})
		case k.Name == refKeyword(c.written):
			if !r.lands(c) {
				return nil, nil, false
			}
		case !r.w.holds(c.held, c.written.Schema, k.Name):
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
	return c.at.declares(&r.w)
}

// claimOf returns the claim n makes, and false for open where n says nothing
// whatever its keys: a boolean says nothing or only forbids what it is written
// for, a declaration written alike against one base says the same, and
// declarations admitting nothing together leave nothing to say. ok is false
// where the body declares nothing there, so n is new shape. What n restates is
// what its declarations say together, each read as an admissionReader reads it, the
// keywords beside a $ref conjoining with it at every version
// (docs/compilers/openapi.md#ref-use-site).
func (r *bodyReading) claimOf(n nested) (sub claim, open, ok bool) {
	if n.at == nil {
		return claim{}, false, false
	}
	alike := func(d *statement.Statement) bool {
		return sameText(r.w.c, d, n.written)
	}
	if n.written.Truth != statement.Stated || slices.ContainsFunc(n.at.decls, alike) {
		return claim{}, false, true
	}
	held := n.at.admits(&r.w)
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
	at, _ := declaredOf(r.w.c, c.written)
	return at != nil && slices.Contains(c.at.declares(&r.w).composed, at)
}

// shapeless reports whether s's keyword key declares no shape whatever it
// holds: a null that reads as the keyword left out, an extension, a keyword
// shapelessKeywords names, or an unevaluatedProperties other than false, read
// from t, which is validation logic where false closes the object
// (docs/ir/boundaries.md#validation-only-constructs).
func shapeless(t *statement.Table, s *statement.Schema, key string) bool {
	if annotation.WritesKeywordLeftOut(s, key) || strings.HasPrefix(key, "x-") || slices.Contains(shapelessKeywords, key) {
		return true
	}
	return key == "unevaluatedProperties" && !t.Sub(s.ObjectKeywords().UnevaluatedProperties).Truth.ReadsFalse()
}

// conjunctWithout is what the conjunct b admits, as lowered reads it, but
// where b is holder or holds it among its flattened entries, nested in an
// allOf or written beside a $ref: there holder is read by its own keywords,
// its unions left out, and every other entry as it is.
func conjunctWithout(w *admissionReader, b *statement.Statement, holder *statement.Schema) admitted {
	switch {
	case b.Schema == holder && b.Ref == nil:
		return writtenLowered(w, holder, w.nullable)
	case holder != nil && b.BesideRef != nil && holder.WrittenBeside == b.BesideRef:
		// An entry writing its union beside a $ref is listed as that reference
		// and the keywords beside it (statement.Table.besideRefOf), the holder.
		return lowered(w, w.c.Statements().Read(b.BesideRef)).and(writtenLowered(w, holder, w.nullable))
	}
	if holder == nil || !slices.ContainsFunc(b.Entries, func(e statement.Entry) bool { return e.Schema == holder }) {
		return lowered(w, b)
	}
	read := inlineConjunct(w, b.Schema)
	for _, e := range b.Entries {
		st := w.c.Statements().Read(e.Schema)
		switch {
		case e.Schema == holder:
			read = read.and(writtenLowered(w, holder, w.nullable))
		case e.Wrapper:
			read = read.and(inlineConjunct(w, e.Schema))
		case st.Ref == nil && st.BesideRef == nil && declaresUnion(e.Schema):
			// Another entry's union is no body of holder's, as beside it
			// (otherEntryUnion).
			read = read.and(writtenLowered(w, e.Schema, w.nullable))
		default:
			read = read.and(lowered(w, st))
		}
	}
	return read
}
