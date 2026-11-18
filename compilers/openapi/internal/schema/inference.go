// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// untypedKinds returns the kinds s, which writes no kind of its own, lowers to
// as its shape keywords speak of them (shapeKinds), and whether the
// kind-inference policy read them (lowersTo). Where its conjuncts, as a
// union kept beside it, admit a kind the keywords do not speak of, there are
// none, as where nothing names a kind. Else s lowers to the kinds they admit:
// the policy's where it read any, off s or a conjunct, the document's
// otherwise.
func untypedKinds(c lowering.Ctx, s *statement.Schema) (kindSet, bool) {
	claims := shapeKinds(s)
	if claims == 0 || !stated(s) {
		return 0, false
	}
	kinds, inferred := lowersTo(c, c.Statements().Read(s))
	if kinds&^claims != 0 {
		return 0, false
	}
	return kinds, inferred
}

// inferredKinds are the types a schema whose shape keywords speak of an object
// and an array lowers by, in the order a type array listing both writes them,
// whichever order the keywords are written in.
var inferredKinds = []statement.Kind{statement.KindObject, statement.KindArray}

// lowerUntypedUnion lowers s, which writes no type, by types, as a type list
// naming them beside its keywords lowers (lowerUnion): an object and an array
// its shape keywords speak of, or the kinds an allOf electing no entry admits
// (lowerUndecided). Where the policy read the kinds, the union and each
// variant are marked, as each variant's kind was read off the keywords too.
func lowerUntypedUnion(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer,
	types []statement.Kind, inferred bool, fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	var mark finisher
	if inferred {
		mark = markKindInferred
	}
	var census finished
	id, diags := lowerUnion(c, ts, depth, s, pointer, typeArray{types: types, mark: mark},
		mark.then(census.onRead(keepUnreadAcross(c, ts, s, nodeShape(ir.KindUnion), nil, pointer))).then(fin))
	return id, append(diags, census.diags...)
}

// claimedKinds returns the kinds c's kind-inference policy reads off the
// keywords s writes that give a type to the instances they apply to, a
// member's, an element's or a value's (siblingKeyword.infers), and none where
// the policy is off. The run's admission reads it as the kinds a statement
// declaring none lowers to (readOne), which lowerUntyped lowers by
// (untypedKinds).
func claimedKinds(c lowering.Ctx, s *statement.Schema) kindSet {
	if !c.KindInference.Honours() {
		return 0
	}
	var out kindSet
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, k := range set {
			if k.infers != nil && k.claims&^out != 0 && k.infers(c.Statements(), s) {
				out |= k.claims
			}
		}
	}
	return out
}

// shapeKinds returns the kinds of instance the shape keywords s writes apply
// to alone, whether or not they infer one: an object for a `discriminator` and
// for each keyword whose row in shapeKeywords or narrowingKeywords claims one,
// as its predicate reads it written, an array likewise
// (docs/compilers/openapi.md#shape-applicator). It is the one reading of what
// they speak of.
func shapeKinds(s *statement.Schema) kindSet {
	var out kindSet
	if s.Discriminator() != nil {
		out = kindObject
	}
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, k := range set {
			if k.claims&^out != 0 && k.writes(s) {
				out |= k.claims
			}
		}
	}
	return out
}

// markKindInferred names the kind-inference policy in the provenance of td,
// in hand, the node a position owns whose kind lowerUntyped read off its shape
// keywords (docs/compilers/openapi.md#policies).
func markKindInferred(td ir.TypeDef, _ keySet) {
	common := td.Common()
	common.Provenance.Inferred = ir.JoinInferred(common.Provenance.Inferred, lowering.KindInferenceHeuristic)
}

// siblingKeyword is a keyword whose presence beside a union or a $dynamicRef
// leaves something for the position to carry, with how a schema writes it, the
// kind of instance it speaks of where it applies to one kind alone, and, for a
// keyword giving the type of a member, an element or a value, where it does,
// which a schema naming no kind is read as (claimedKinds). infers is nil for a
// keyword that never does.
type siblingKeyword struct {
	key    string
	writes func(*statement.Schema) bool
	claims kindSet
	infers func(*statement.Table, *statement.Schema) bool
}

// shapeKeywords are the keywords that declare data shape — a type, a property
// set, a value set, composition, or a catch-all — rather than only narrowing
// what an already-declared shape accepts (docs/compilers/openapi.md#union-beside-structure).
var shapeKeywords = []siblingKeyword{
	{"properties", func(s *statement.Schema) bool { return len(s.ObjectKeywords().Properties) > 0 }, kindObject,
		func(_ *statement.Table, s *statement.Schema) bool { return len(s.ObjectKeywords().Properties) > 0 }},
	{"const", func(s *statement.Schema) bool { return s.Const() != nil }, 0, nil},
	{"enum", enumWritten, 0, nil},
	{"allOf", writesConjuncts, 0, nil},
	{"additionalProperties", func(s *statement.Schema) bool {
		return written(s.ObjectKeywords().AdditionalProperties)
	}, kindObject, func(t *statement.Table, s *statement.Schema) bool {
		return typesInstances(t, s.ObjectKeywords().AdditionalProperties)
	}},
	{"patternProperties", func(s *statement.Schema) bool { return len(s.ObjectKeywords().PatternProperties) > 0 }, kindObject,
		func(_ *statement.Table, s *statement.Schema) bool {
			return len(s.ObjectKeywords().PatternProperties) > 0
		}},
	{"type", func(s *statement.Schema) bool { return len(effectiveTypes(s)) > 0 }, 0, nil},
}

// narrowingKeywords are the keywords that constrain an instance without
// declaring a shape to build. They sit on the narrowing side of shapeKeywords'
// line, and that line is about what to *build*. Wherever the question is
// instead whether a lowering can carry what the position wrote, the distinction
// does not apply: an ir.Union has no element type, so
// `{oneOf: [...], items: {...}}` loses the `items` as surely as it would lose a
// co-declared property set. An `items` list narrows too, read off the raw node,
// since nothing documents what the parser's model holds there.
var narrowingKeywords = []siblingKeyword{
	{"required", func(s *statement.Schema) bool { return len(s.ObjectKeywords().Required) > 0 }, kindObject, nil},
	{"items", func(s *statement.Schema) bool {
		_, listed := listedItems(s)
		return listed || written(s.ArrayKeywords().Items)
	}, kindArray, func(t *statement.Table, s *statement.Schema) bool {
		_, listed := listedItems(s)
		return listed || typesInstances(t, s.ArrayKeywords().Items)
	}},
	{"prefixItems", func(s *statement.Schema) bool { return len(s.ArrayKeywords().PrefixItems) > 0 }, kindArray,
		func(_ *statement.Table, s *statement.Schema) bool { return len(s.ArrayKeywords().PrefixItems) > 0 }},
}

// typesInstances reports whether sub, the schema an `items` or an
// `additionalProperties` writes, gives a type to the instances it applies to: a
// schema object writing a keyword. `true` and `{}` state nothing, as leaving
// the keyword out does (JSON Schema Core 2020-12 §10.3.1.2, §10.3.2.3), and
// `false`, which admits no instance, types none either.
func typesInstances(t *statement.Table, sub statement.Sub) bool {
	return sub.Truth() == statement.Stated && len(t.Sub(sub).Keys) > 0
}

// siblingKeywords returns the keywords of shapeKeywords and narrowingKeywords
// that s writes, in that order.
func siblingKeywords(s *statement.Schema) []string {
	var out []string
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, k := range set {
			if k.writes(s) {
				out = append(out, k.key)
			}
		}
	}
	return out
}

// writesSiblingKeyword reports whether siblingKeywords(s) holds any keyword,
// without building the list, as most schemas a reader asks of write one.
func writesSiblingKeyword(s *statement.Schema) bool {
	writes := func(k siblingKeyword) bool { return k.writes(s) }
	return slices.ContainsFunc(shapeKeywords, writes) || slices.ContainsFunc(narrowingKeywords, writes)
}
