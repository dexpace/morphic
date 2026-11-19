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

// readBody reads schema, the schema at the position of a union holder writes,
// for the union's branches to be judged against: holder's own keywords without
// its oneOf and anyOf, as where schema itself is the holder, so a union is
// judged by the rest whichever entry writes it. Where holder is an entry,
// another inline entry's union is no body either, as two unions beside the
// allOf judge each other by neither.
func readBody(loweringContext lowering.Ctx, schema, holder *statement.Schema) *bodyReading {
	bodyReadingValue := &bodyReading{w: newAdmissionReader(loweringContext), body: schema}
	bodyReadingValue.held = writtenLowered(&bodyReadingValue.w, schema, bodyReadingValue.w.nullable)
	for _, b := range conjunctsIn(loweringContext, schema) {
		if otherEntryUnion(b, schema, holder) {
			bodyReadingValue.held = bodyReadingValue.held.and(writtenLowered(&bodyReadingValue.w, b.Schema, bodyReadingValue.w.nullable))
			continue
		}
		bodyReadingValue.held = bodyReadingValue.held.and(conjunctWithout(&bodyReadingValue.w, b, holder))
	}
	return bodyReadingValue
}

// otherEntryUnion reports whether b, a conjunct of schema, is an inline entry
// writing a union of its own beside holder, another entry of schema, which
// neither it nor a $ref it writes beside its keywords holds.
func otherEntryUnion(b *statement.Statement, schema, holder *statement.Schema) bool {
	return holder != schema && b.Ref == nil && b.BesideRef == nil && b.Schema != holder && declaresUnion(b.Schema) &&
		!slices.ContainsFunc(b.Entries, func(allOfConjunct statement.AllOfConjunct) bool { return allOfConjunct.Schema == holder })
}

// readOwnBody reads only what schema itself writes: its own keywords, and its
// own properties and items as the declarations of what a branch nests, each
// read whole as readBody reads it. It spends a budget of its own, so what it
// reads the whole reading does not pay for.
func readOwnBody(loweringContext lowering.Ctx, schema *statement.Schema) *bodyReading {
	bodyReadingValue := &bodyReading{w: newAdmissionReader(loweringContext), body: schema, own: true}
	bodyReadingValue.held = writtenLowered(&bodyReadingValue.w, schema, bodyReadingValue.w.nullable)
	return bodyReadingValue
}

// declared returns what the body declares of its properties and elements,
// read the first time a branch nests a claim, so a verdict needing none spends
// none of the budget on them.
func (r *bodyReading) declared() declarationSet {
	if r.decls == nil {
		declarationSet := newDeclarationSet()
		if r.own {
			declarationSet.add(r.w.c.Statements(), r.body)
		} else {
			declarationSet = declarationsOf(&r.w, r.body)
		}
		r.decls = &declarationSet
	}
	return *r.decls
}

// unionBranchesDeclareShape reports whether any oneOf/anyOf branch declares
// data shape schema does not. When none does — `oneOf: [{required: [a]},
// {required: [b]}]`, the commonest instance — the union narrows what the body
// accepts without reshaping it, which is validation logic
// (docs/ir/boundaries.md#validation-only-constructs). A $ref branch always
// declares shape, so the body is read only once an inline branch needs it, and
// a reading its budget cut short says nothing, so it counts as one that does.
func unionBranchesDeclareShape(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	choice := schema.ChoiceKeywords()
	return setsDeclareShape(loweringContext, schema, schema, choice.OneOf, choice.AnyOf)
}

// setsDeclareShape is unionBranchesDeclareShape for the branches of sets,
// written by holder, schema or an allOf branch of it, judged against schema as
// the body, read without holder's unions; a nil holder reads the conjunction
// whole. The body's own reading settles first each branch restating what schema
// itself writes (ownRestatements), and only the rest are judged against the
// whole reading, which they would restate under too, as more declarations admit
// less.
func setsDeclareShape(loweringContext lowering.Ctx, schema, holder *statement.Schema, sets ...[]statement.Subschema) bool {
	branches := statementsOf(loweringContext, slices.Concat(sets...))
	if slices.ContainsFunc(branches, func(b *statement.Statement) bool { return referenceOf(loweringContext, b) != nil }) {
		return true
	}
	settled := ownRestatements(loweringContext, schema, branches)
	var bodyReadingValue *bodyReading
	body := func() *bodyReading {
		if bodyReadingValue == nil {
			bodyReadingValue = readBody(loweringContext, schema, holder)
		}
		return bodyReadingValue
	}
	found := false
	for i, b := range branches {
		if (settled == nil || !settled[i]) && branchDeclaresShape(loweringContext.Statements(), body, b) {
			found = true
			break
		}
	}
	return found || bodyReadingValue != nil && bodyReadingValue.w.incomplete()
}

// ownRestatements marks, by index, the inline branches of branches that restate
// schema read alone (readOwnBody), judged all, and none where that reading ran
// out: which branches it settled before running out would follow their order,
// and with it what the whole reading is left to read (GitHub #1423).
func ownRestatements(loweringContext lowering.Ctx, schema *statement.Schema, branches []*statement.Statement) []bool {
	var bodyReadingValue *bodyReading
	body := func() *bodyReading {
		if bodyReadingValue == nil {
			bodyReadingValue = readOwnBody(loweringContext, schema)
		}
		return bodyReadingValue
	}
	settled := make([]bool, len(branches))
	for i, b := range branches {
		if !branchDeclaresShape(loweringContext.Statements(), body, b) {
			settled[i] = true
		}
	}
	if bodyReadingValue != nil && bodyReadingValue.w.incomplete() {
		return nil
	}
	return settled
}

// branchDeclaresShape reports whether the inline union branch b declares data
// shape beside the body body reads, its schemas read from
// memoizingSchemaReader. A boolean never does, and any other does where one
// keyword it writes does: bare or beside a repeated `type`, a keyword is judged
// alike, so restating the body changes no verdict. A branch writing no key
// judged against the body, `required` alone the commonest, leaves it unread.
func branchDeclaresShape(memoizingSchemaReader *statement.MemoizingSchemaReader, body func() *bodyReading, b *statement.Statement) bool {
	if b.Truth != statement.Stated {
		return false
	}
	writesJudged := slices.ContainsFunc(b.Keys, func(key statement.Key) bool {
		return judged(memoizingSchemaReader, b.Schema, key.Name)
	})
	if !writesJudged {
		return false
	}
	bodyReadingValue := body()
	return !bodyReadingValue.restates(claim{written: b, held: bodyReadingValue.held})
}

// judged reports whether schema's key is judged against what the body says: it
// is neither shapeless, its schemas read from memoizingSchemaReader, nor a
// `type` naming only null, which contributes the null.
func judged(memoizingSchemaReader *statement.MemoizingSchemaReader, schema *statement.Schema, key string) bool {
	return !shapeless(memoizingSchemaReader, schema, key) && (key != "type" || len(effectiveTypes(schema)) > 0)
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
		claimValue := claims[len(claims)-1]
		claims = claims[:len(claims)-1]
		conjuncts, inner, ok := r.keysRestate(claimValue)
		if !ok {
			return false
		}
		for _, subschema := range conjuncts {
			claims = append(claims, claim{written: r.w.c.Statements().Subschema(subschema), held: claimValue.held, at: claimValue.at})
		}
		for _, nested := range inner {
			sub, open, ok := r.claimOf(nested)
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

// keysRestate reports whether each key claimValue writes says nothing of shape
// the body does not, and returns the allOf conjuncts, which restate what
// claimValue does, and the properties and element it writes, for restates to
// judge. A key declares none where it is not judged, a `$ref` landing where
// claimValue's declarations compose, or a `type`, `format`, `enum` or `const`
// every value c.held admits holds to. A boolean conjunct says nothing or only
// forbids. Keys are the statement's, which hold the keywords the model has no
// field for (docs/parser-library.md#unmodeled-keywords).
func (r *bodyReading) keysRestate(claimValue claim) ([]statement.Subschema, []nested, bool) {
	if !stated(claimValue.written.Schema) {
		return nil, nil, true
	}
	memoizingSchemaReader := r.w.c.Statements()
	var inner []nested
	var conjuncts []statement.Subschema
	for _, key := range claimValue.written.Keys {
		switch {
		case !judged(memoizingSchemaReader, claimValue.written.Schema, key.Name):
		case key.Name == "allOf":
			conjuncts = claimValue.written.AllOf
		case key.Name == "properties":
			decls := r.declarationsAt(claimValue)
			for _, subschema := range claimValue.written.ObjectKeywords().Properties {
				inner = append(inner, nested{written: memoizingSchemaReader.Subschema(subschema), at: decls.property(memoizingSchemaReader, subschema.Name)})
			}
		case key.Name == "items":
			if _, listed := listedItems(claimValue.written.Schema); listed {
				// A tuple, or a malformed list, which no element the body declares restates.
				return nil, nil, false
			}
			inner = append(inner, nested{written: memoizingSchemaReader.Subschema(claimValue.written.ArrayKeywords().Items), at: r.declarationsAt(claimValue).items})
		case key.Name == refKeyword(claimValue.written):
			if !r.lands(claimValue) {
				return nil, nil, false
			}
		case !r.w.holds(claimValue.held, claimValue.written.Schema, key.Name):
			return nil, nil, false
		}
	}
	return conjuncts, inner, true
}

// declarationsAt returns what the body declares of what claimValue nests: its
// own declarations for the branch itself, and what claimValue's place declares
// otherwise, each read the first time a claim nests something.
func (r *bodyReading) declarationsAt(claimValue claim) declarationSet {
	if claimValue.at == nil {
		return r.declared()
	}
	return claimValue.at.declares(&r.w)
}

// claimOf returns the claim nestedValue makes, and false for open where
// nestedValue says nothing whatever its keys: a boolean says nothing or only
// forbids what it is written for, a declaration written alike against one base
// says the same, and declarations admitting nothing together leave nothing to
// say. ok is false where the body declares nothing there, so nestedValue is new
// shape. What nestedValue restates is what its declarations say together, each
// read as an admissionReader reads it, the keywords beside a $ref conjoining
// with it at every version (docs/compilers/openapi.md#ref-use-site).
func (r *bodyReading) claimOf(nestedValue nested) (sub claim, open, ok bool) {
	if nestedValue.at == nil {
		return claim{}, false, false
	}
	alike := func(schemaStatement *statement.Statement) bool {
		return sameText(r.w.c, schemaStatement, nestedValue.written)
	}
	if nestedValue.written.Truth != statement.Stated || slices.ContainsFunc(nestedValue.at.decls, alike) {
		return claim{}, false, true
	}
	held := nestedValue.at.admits(&r.w)
	if !held.reachable() {
		return claim{}, false, true
	}
	return claim{written: nestedValue.written, held: held, at: nestedValue.at}, true, true
}

// lands reports whether the $ref claimValue writes lands on a schema
// claimValue's declarations compose, through allOf conjuncts and $refs, so it
// names what they already imply. The branch itself restates no declaration:
// unionBranchesDeclareShape settles a $ref branch, and a $ref conjunct of one
// declares shape as it does.
func (r *bodyReading) lands(claimValue claim) bool {
	if claimValue.at == nil {
		return false
	}
	at, _ := declaredOf(r.w.c, claimValue.written)
	return at != nil && slices.Contains(claimValue.at.declares(&r.w).composed, at)
}

// shapeless reports whether schema's keyword key declares no shape whatever it
// holds: a null that reads as the keyword left out, an extension, a keyword
// shapelessKeywords names, or an unevaluatedProperties other than false, read
// from memoizingSchemaReader, which is validation logic where false closes the
// object (docs/ir/boundaries.md#validation-only-constructs).
func shapeless(memoizingSchemaReader *statement.MemoizingSchemaReader, schema *statement.Schema, key string) bool {
	if annotation.WritesKeywordLeftOut(schema, key) || strings.HasPrefix(key, "x-") || slices.Contains(shapelessKeywords, key) {
		return true
	}
	return key == "unevaluatedProperties" && !memoizingSchemaReader.Subschema(schema.ObjectKeywords().UnevaluatedProperties).Truth.ReadsFalse()
}

// conjunctWithout is what the conjunct b admits, as lowered reads it, but
// where b is holder or holds it among its flattened entries, nested in an
// allOf or written beside a $ref: there holder is read by its own keywords,
// its unions left out, and every other entry as it is.
func conjunctWithout(admissionReaderValue *admissionReader, b *statement.Statement, holder *statement.Schema) admitted {
	switch {
	case b.Schema == holder && b.Ref == nil:
		return writtenLowered(admissionReaderValue, holder, admissionReaderValue.nullable)
	case holder != nil && b.BesideRef != nil && holder.WrittenBeside == b.BesideRef:
		// An entry writing its union beside a $ref is listed as that reference
		// and the rest of its keywords
		// (statement.MemoizingSchemaReader.restOf), the holder.
		return lowered(admissionReaderValue, admissionReaderValue.c.Statements().Read(b.BesideRef)).and(writtenLowered(admissionReaderValue, holder, admissionReaderValue.nullable))
	}
	if holder == nil || !slices.ContainsFunc(b.Entries, func(allOfConjunct statement.AllOfConjunct) bool { return allOfConjunct.Schema == holder }) {
		return lowered(admissionReaderValue, b)
	}
	read := inlineConjunct(admissionReaderValue, b.Schema)
	for _, allOfConjunct := range b.Entries {
		st := admissionReaderValue.c.Statements().Read(allOfConjunct.Schema)
		switch {
		case allOfConjunct.Schema == holder:
			read = read.and(writtenLowered(admissionReaderValue, holder, admissionReaderValue.nullable))
		case allOfConjunct.Wrapper:
			read = read.and(inlineConjunct(admissionReaderValue, allOfConjunct.Schema))
		case st.Ref == nil && st.BesideRef == nil && declaresUnion(allOfConjunct.Schema):
			// Another entry's union is no body of holder's, as beside it
			// (otherEntryUnion).
			read = read.and(writtenLowered(admissionReaderValue, allOfConjunct.Schema, admissionReaderValue.nullable))
		default:
			read = read.and(lowered(admissionReaderValue, st))
		}
	}
	return read
}
