// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// maxConjunctReads bounds the schemas, references and property declarations the
// reading of foldedDeclarations takes in, and the types it queues, summed over
// the run (styleguide bounded-everything rule): a chain of compositions is read
// whole at each model of it.
const maxConjunctReads = 1 << 22

// foldedDeclarations is the property positions declaring a member that a
// composition may fold from several declarations
// (docs/compilers/openapi.md#allof-redeclared-property), or every property's
// where the reading was cut short.
type foldedDeclarations struct {
	at  map[reftable.DocumentPointer]bool
	cut bool
}

// hold reports whether at, a carried position, is among f: a parameter's or a
// header's never is, as neither folds.
func (f foldedDeclarations) hold(at reftable.DocumentPointer) bool {
	return f.at[at] || f.cut && at.Pointer.Parent().LastToken() == "properties"
}

// foldedDeclarationsOf keys foldedDeclarations' one reading.
type foldedDeclarationsOf struct{}

// mayFold reports whether the property declared at at is one of several
// declarations of its member that some composition folds, so its notes stay on
// the node its schema builds, where the fold reads them (notesOnCarrier), and a
// reader of the member alone meets none of them (GitHub #2150). It may report
// one no fold meets, and never misses one a fold does. Every property asks, so
// the documents are read for it once a run.
func mayFold(loweringContext lowering.Ctx, at reftable.DocumentPointer) bool {
	return lowering.ReadOncePerRun(loweringContext, foldedDeclarationsOf{}, func() foldedDeclarations {
		return readFoldedDeclarations(loweringContext, budget.AtMost(maxConjunctReads))
	}).hold(at)
}

// readFoldedDeclarations reads foldedDeclarations off every schema the bindings
// hold, within limit: first each that composes, then, as a fold judges and
// builds the types of the declarations it meets, the declarations those types
// make of one member in turn.
func readFoldedDeclarations(loweringContext lowering.Ctx, limit budget.Limit) foldedDeclarations {
	reader := conjunctReader{c: loweringContext, reads: budget.New(limit), folded: map[reftable.DocumentPointer]bool{},
		numbers: map[*statement.Statement]int{}, met: map[string]bool{}}
	for at := range loweringContext.Bindings().Schemas() {
		reader.readComposition(loweringContext.Statements().At(at))
	}
	for len(reader.meeting) > 0 {
		types := reader.meeting[len(reader.meeting)-1]
		reader.meeting = reader.meeting[:len(reader.meeting)-1]
		reader.readTypes(types)
	}
	return foldedDeclarations{at: reader.folded, cut: reader.reads.Exhausted()}
}

// conjunctReader is one reading of foldedDeclarations in progress: what it
// marked, the groups of types left to read against each other, and each group
// queued so far (met), by the numbers of its schemas.
type conjunctReader struct {
	c       lowering.Ctx
	reads   budget.Budget
	folded  map[reftable.DocumentPointer]bool
	meeting [][]*statement.Statement
	numbers map[*statement.Statement]int
	met     map[string]bool
}

// conjunction is what several schemas declare together: each member's
// declarations, the schema objects making them, and whether a reference among
// them names nothing read. Two models may hold one declaration, as one does
// inlining an entry a reference makes a model of its own, so it is listed at
// each.
type conjunction struct {
	members map[string][]*statement.Statement
	objects []*statement.Schema
	unread  bool
}

// readComposition marks the members st declares several times, through every
// depth of its allOf, beside each branch of its unions in turn: a variant
// composes one branch with what is written beside the union.
func (r *conjunctReader) readComposition(st *statement.Statement) {
	branches := unionBranchesIn(r.c, inlineConjuncts(st))
	if len(st.Entries) == 0 && len(branches) == 0 {
		return
	}
	if len(branches) == 0 {
		r.mark(r.conjunctionOf([]*statement.Statement{st}, false))
	}
	for _, branch := range branches {
		r.mark(r.conjunctionOf([]*statement.Statement{st, branch}, false))
	}
}

// readTypes marks the members types, those of one member's declarations or of
// one position their values hold, declare several times: a fold may compose
// them, or judge a variant of one against another.
func (r *conjunctReader) readTypes(types []*statement.Statement) {
	conjunctionValue := r.conjunctionOf(types, true)
	r.mark(conjunctionValue)
	elements := map[string][]*statement.Statement{}
	for _, schema := range conjunctionValue.objects {
		r.addElements(elements, schema)
	}
	for _, held := range elements {
		r.meet(held)
	}
}

// mark marks each member of conjunctionValue declared several times, or, where
// a reference in it names nothing read, every member, and queues the types of
// its declarations.
func (r *conjunctReader) mark(conjunctionValue conjunction) {
	for _, declarations := range conjunctionValue.members {
		if len(declarations) < 2 && !conjunctionValue.unread {
			continue
		}
		for _, declaration := range declarations {
			if at, placed := siteOf(declaration.Schema); placed {
				r.folded[at] = true
			}
		}
		r.meet(declarations)
	}
}

// meet queues types, where several are among them, to be read against each
// other, once however many compositions meet them.
func (r *conjunctReader) meet(types []*statement.Statement) {
	if len(types) < 2 || !r.reads.Spend(len(types)) {
		return
	}
	if key := r.keyOf(types); !r.met[key] {
		r.met[key] = true
		r.meeting = append(r.meeting, types)
	}
}

// keyOf names the schemas of types, whatever their order, by the number each is
// first asked of.
func (r *conjunctReader) keyOf(types []*statement.Statement) string {
	numbers := make([]int, 0, len(types))
	for _, st := range types {
		number, numbered := r.numbers[st]
		if !numbered {
			number = len(r.numbers)
			r.numbers[st] = number
		}
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	return fmt.Sprint(numbers)
}

// conjunctionOf returns what schemas declare together, each read with all it
// composes: its inline allOf entries, the schema each reference among them
// names, hop by hop, and, of its unions, every branch where asTypes holds, or
// else the one a union of null beside it collapses to. A schema is read once,
// so a cycle ends. The reading stops where the reads run out.
func (r *conjunctReader) conjunctionOf(schemas []*statement.Statement, asTypes bool) conjunction {
	out := conjunction{members: map[string][]*statement.Statement{}}
	seen := map[*statement.Statement]bool{}
	for queue := slices.Clone(schemas); len(queue) > 0; {
		st := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[st] {
			continue
		}
		seen[st] = true
		if !r.reads.Spend(1) {
			return out
		}
		named, read := r.namedBy(st)
		out.unread = out.unread || !read
		queue = append(queue, named...)
		listing := inlineConjuncts(st)
		for _, schema := range listing {
			properties := schema.ObjectKeywords().Properties
			r.reads.Spend(len(properties))
			for _, property := range properties {
				out.members[property.Name] = append(out.members[property.Name], r.c.Statements().Subschema(property))
			}
		}
		out.objects = append(out.objects, listing...)
		if asTypes {
			queue = append(queue, unionBranchesIn(r.c, listing)...)
		} else {
			queue = append(queue, collapsedBranches(r.c, listing)...)
		}
	}
	return out
}

// namedBy returns the schemas st's own reference and its allOf entries' name,
// an entry's as the reference it writes, a dynamic one as the position it
// expands to, and false where st's own names no schema the bindings hold.
func (r *conjunctReader) namedBy(st *statement.Statement) ([]*statement.Statement, bool) {
	if st.Ref != nil {
		if target, _ := declaredOf(r.c, st); target != nil {
			return []*statement.Statement{target}, true
		}
		return nil, false
	}
	var named []*statement.Statement
	for _, entry := range st.Entries {
		if entry.Ref != nil {
			named = append(named, r.c.Statements().Read(entry.Schema))
		}
	}
	for _, schema := range inlineConjuncts(st) {
		if there, pointer, placed := placedAt(r.c, schema); placed {
			if target, expands := dynamicTarget(there, schema, pointer); expands {
				named = append(named, r.c.Statements().At(target))
			}
		}
	}
	return named, true
}

// addElements adds to elements the schemas the values of schema hold beside
// its properties, by the keyword and the position writing each, as a fold
// judges them one against another: a list's element, a tuple's, what follows
// it, and what a map's or a pattern's values are.
func (r *conjunctReader) addElements(elements map[string][]*statement.Statement, schema *statement.Schema) {
	add := func(keyword string, subschemas ...statement.Subschema) {
		for i, subschema := range subschemas {
			if !written(subschema) {
				continue
			}
			at := keyword + "/" + cmp.Or(subschema.Name, strconv.Itoa(i))
			elements[at] = append(elements[at], r.c.Statements().Subschema(subschema))
		}
	}
	object := schema.ObjectKeywords()
	add("additionalProperties", object.AdditionalProperties)
	add("patternProperties", object.PatternProperties...)
	if entries, rest, listed := ListedTuple(r.c, r.c.Statements().Read(schema)); listed {
		for i, entry := range entries {
			at := "prefixItems/" + strconv.Itoa(i)
			elements[at] = append(elements[at], entry)
		}
		elements["items/0"] = append(elements["items/0"], rest)
		return
	}
	add("prefixItems", schema.PrefixItems()...)
	add("items", itemsSchema(schema))
}

// inlineConjuncts returns the schema objects a model built at st lists its own
// members from: st's, and each allOf entry's its statement flattens, the
// keywords beside a reference among them. A reference writes none beside it.
func inlineConjuncts(st *statement.Statement) []*statement.Schema {
	listing := make([]*statement.Schema, 0, 1+len(st.Entries))
	listing = append(listing, st.Schema)
	for _, entry := range st.Entries {
		listing = append(listing, entry.Schema)
	}
	return listing
}

// unionBranchesIn returns the branches of each oneOf and anyOf schemas write.
func unionBranchesIn(loweringContext lowering.Ctx, schemas []*statement.Schema) []*statement.Statement {
	var branches []*statement.Statement
	for _, schema := range schemas {
		choice := schema.ChoiceKeywords()
		for _, branch := range slices.Concat(choice.OneOf, choice.AnyOf) {
			branches = append(branches, loweringContext.Statements().Subschema(branch))
		}
	}
	return branches
}

// collapsedBranches returns, of each oneOf and anyOf schemas write, the one
// branch that is no null, where one alone is: such a union may lower as that
// branch (nullUnionCollapse), and any other as a Union, which declares no
// member.
func collapsedBranches(loweringContext lowering.Ctx, schemas []*statement.Schema) []*statement.Statement {
	var collapsed []*statement.Statement
	for _, schema := range schemas {
		choice := schema.ChoiceKeywords()
		for _, union := range [...][]statement.Subschema{choice.OneOf, choice.AnyOf} {
			live := make([]*statement.Statement, 0, len(union))
			for _, branch := range union {
				if st := loweringContext.Statements().Subschema(branch); !nullBranch(loweringContext, st) {
					live = append(live, st)
				}
			}
			if len(live) == 1 {
				collapsed = append(collapsed, live[0])
			}
		}
	}
	return collapsed
}
