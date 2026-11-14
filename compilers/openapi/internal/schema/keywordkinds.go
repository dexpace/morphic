// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// keywordKinds maps each keyword JSON Schema applies to instances of some types
// alone to the kinds of those types: the numeric bounds to numbers, the string
// bounds and the content vocabulary to strings, and the property counts to
// objects (JSON Schema Validation 2020-12 §6.2, §6.3, §6.5 and §8, which the
// earlier drafts group alike). An instance of another kind satisfies such a
// keyword (JSON Schema Core 2020-12 §7.6.1), so it says nothing of a value only
// other kinds can be. The collection bounds are left to the List and Tuple
// readers, which ask the node.
var keywordKinds = map[string]kindSet{
	"exclusiveMaximum": kindNumber,
	"exclusiveMinimum": kindNumber,
	"maximum":          kindNumber,
	"minimum":          kindNumber,
	"multipleOf":       kindNumber,
	"maxLength":        kindString,
	"minLength":        kindString,
	"pattern":          kindString,
	"contentEncoding":  kindString,
	"contentMediaType": kindString,
	"contentSchema":    kindString,
	"maxProperties":    kindObject,
	"minProperties":    kindObject,
}

// typeKinds returns the kinds st, one type a type array lists, admits, and every
// kind but null for a spelling no dialect defines, as declaredInstanceTypes
// reads one.
func typeKinds(st oas3.SchemaType) kindSet {
	if kinds := kindOfType(st) &^ kindNull; kinds != 0 {
		return kinds
	}
	return anyInstance
}

// positionTypes returns the kinds a reader of the bounds written at pointer
// takes: those s's own type admits that every schema s names admits too
// (docs/compilers/openapi.md#constraints). It reads no value set's kinds, so one
// writing no type takes every bound (GitHub #1919), nor a type beside a $ref
// inside a named schema (GitHub #1390), and a read past maxBoundTypeReads takes
// every bound. A bound beside an enum, a const or a union at a property is
// also kept on its node (GitHub #1924), redeclarations each keeping one meet
// GitHub #1889's fold, and one left is reported at its position, not its key.
func positionTypes(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) kindSet {
	own := declaredInstanceTypes(s)
	if s == nil || own == 0 || !namesSchema(s) || !writesBound(s) {
		return own
	}
	return own & namedTypes(c, s, pointer)
}

// namesSchema reports whether s names a schema whose types conjoin with its own:
// through a $ref, an allOf entry, a union branch or a dynamic reference.
func namesSchema(s *oas3.Schema) bool {
	return len(s.GetAllOf()) > 0 || s.Ref != nil || declaresUnion(s) || declaresDynamicRef(s)
}

// writesBound reports whether s, or an inline branch of its flattened allOf,
// writes a value or collection bound: where none does, no reader asks its types.
// A bound beside a branch's own $ref is that branch's.
func writesBound(s *oas3.Schema) bool {
	if declaresBound(s) {
		return true
	}
	for st := range walkAllOf(s) {
		if bs := st.schema.GetSchema(); bs != nil && isInlineBranch(st.schema) && declaresBound(bs) {
			return true
		}
	}
	return false
}

// declaresBound reports whether s writes a value bound or a collection bound.
func declaresBound(s *oas3.Schema) bool {
	return declaresValueConstraints(s) || annotation.DeclaresAny(s, collectionBoundKeywords)
}

// namedTypes reads what the schemas s, written at pointer, names admit: its
// allOf entries, the end of its $ref chain, the target its dynamic reference
// expands to, and its union's branches, the one non-null branch where the
// union collapses onto it. A read past its budget answers anyInstance, as a cut
// read's answer would turn on the order it read in.
func namedTypes(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) kindSet {
	r := boundTypes{refs: c.Bindings(), steps: budget.New(budget.AtMost(maxBoundTypeReads))}
	out := r.conjuncts(s)
	if s.Ref != nil {
		out &= r.schema(c.Bindings().End(heldAt(c, pointer)).GetSchema())
	}
	if at, _, expanded := dynamicExpansion(c, s, pointer); expanded {
		out &= r.schema(c.Bindings().At(at).GetSchema())
	}
	if nonNull, _, collapses := nullUnionCollapse(c, s, pointer); collapses {
		out &= r.branch(nonNull) // its null branch may write no type, which would admit every kind
	} else {
		out &= r.unions(s)
	}
	if r.steps.Exhausted() {
		return anyInstance
	}
	return out
}

// maxBoundTypeReads bounds one boundTypes read (styleguide bounded-recursion
// rule), a step per schema it reads, so its depth and breadth: an allOf naming
// its own schema ends on it.
const maxBoundTypeReads = 256

// boundTypes reads the instance types schemas declare, for the bounds written
// beside them: an allOf conjoins its entries, a union admits what any branch
// does, and a $ref is what its chain ends on. Unlike conjunctTypes, an empty
// answer stays empty at any depth, and the walk never stops early, so whether
// it runs past its budget does not turn on the order schemas are listed in.
type boundTypes struct {
	refs  *typed.Bindings
	steps budget.Budget
}

// schema is what s admits: its own type, conjoined with its allOf's entries and
// its union's branches. A schema it cannot read, or one past the budget,
// excludes nothing; namedTypes discards a read that ran past it.
func (r *boundTypes) schema(s *oas3.Schema) kindSet {
	if s == nil || !r.steps.Spend(1) {
		return anyInstance
	}
	return declaredInstanceTypes(s) & r.conjuncts(s) & r.unions(s)
}

// conjuncts is what every entry of s's allOf admits.
func (r *boundTypes) conjuncts(s *oas3.Schema) kindSet {
	out := anyInstance
	for _, b := range s.GetAllOf() {
		out &= r.branch(b)
	}
	return out
}

// unions is what s's oneOf and anyOf each admit, conjoined: what any branch of
// one admits.
func (r *boundTypes) unions(s *oas3.Schema) kindSet {
	out := anyInstance
	for _, branches := range [][]*oas3.JSONSchema[oas3.Referenceable]{s.GetOneOf(), s.GetAnyOf()} {
		if len(branches) == 0 {
			continue
		}
		var some kindSet
		for _, b := range branches {
			some |= r.branch(b)
		}
		out &= some
	}
	return out
}

// branch is what one entry or branch admits: an inline one's own schema, a
// $ref's the schema its chain ends on. A keyword beside the $ref is not read
// (GitHub #1390).
func (r *boundTypes) branch(b *oas3.JSONSchema[oas3.Referenceable]) kindSet {
	if isRefBranch(b) {
		return r.schema(refBranchTarget(r.refs, b))
	}
	return r.schema(b.GetSchema())
}

// writerTakes returns the kinds whose value bounds a reader of the kinds takes
// reads of w, a position's schema or an allOf branch folding there: those w's
// own types admit too, as a branch declaring a type bounds no value of another
// kind. A reader's kinds are those its position admits (positionTypes), within
// what the position's own schema declares, so that schema leaves them as they
// are.
func writerTakes(takes kindSet, w *oas3.Schema) kindSet {
	return takes & declaredInstanceTypes(w)
}

// taken reports whether a reader of values of the kinds takes reads keyword:
// whether keyword applies to a value of one of them. One the table does not list
// applies to every value.
func taken(takes kindSet, keyword string) bool {
	kinds, listed := keywordKinds[keyword]
	return !listed || takes&kinds != 0
}

// untakenBounds returns the value constraints s writes that a reader of the
// kinds takes does not read, in valueConstraintKeywords order, but a count the
// load phase refuses, which it reports and no reader holds (refusedCount).
func untakenBounds(s *oas3.Schema, takes kindSet) []string {
	return slices.DeleteFunc(declaredConstraints(s), func(keyword string) bool {
		return taken(takes, keyword) || refusedCount(s, keyword)
	})
}

// boundsFor returns the groups of value bounds annotation.Constraints reads for
// a reader of the kinds takes: the numeric bounds for a number, the string
// bounds for a string and the property counts for an object, as keywordKinds
// gives each keyword its kinds.
func boundsFor(takes kindSet) annotation.Bounds {
	var b annotation.Bounds
	if takes&kindNumber != 0 {
		b |= annotation.NumberBounds
	}
	if takes&kindString != 0 {
		b |= annotation.StringBounds
	}
	if takes&kindObject != 0 {
		b |= annotation.ObjectBounds
	}
	return b
}
