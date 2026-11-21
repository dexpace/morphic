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

// untypedKinds returns the kinds schema, which writes no kind of its own,
// lowers to as its shape keywords speak of them (shapeKinds), and whether the
// kind-inference policy read them (lowersTo). Where its conjuncts, as a union
// kept beside it, admit a kind the keywords do not speak of, there are none, as
// where nothing names a kind. Else schema lowers to the kinds they admit: the
// policy's where it read any, off schema or a conjunct, the document's
// otherwise.
func untypedKinds(loweringContext lowering.Ctx, schema *statement.Schema) (kindSet, bool) {
	claims := shapeKinds(schema)
	if claims == 0 || !stated(schema) {
		return 0, false
	}
	kinds, inferred := lowersTo(loweringContext, loweringContext.Statements().Read(schema))
	if kinds&^claims != 0 {
		return 0, false
	}
	return kinds, inferred
}

// inferredKinds are the types a schema whose shape keywords speak of an object
// and an array lowers by, in the order a type array listing both writes them,
// whichever order the keywords are written in.
var inferredKinds = []statement.JSONSchemaTypeName{statement.KindObject, statement.KindArray}

// lowerTypeList lowers s, at pointer, by the several types typingValue names,
// one variant per type, each from this same schema (lowerUnion): the types it
// lists, an object and an array its shape keywords speak of, or the kinds an
// allOf electing no entry admits (typingOf). The homes are the variants', not
// the Union's, and a type list is what built them, so its `type` is read. Where
// the policy read the kinds, the union and each variant are marked, as each
// variant's kind was read off the keywords too. The union stands where st says.
func lowerTypeList(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, typingValue typing,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	var mark finisher
	if typingValue.inferred {
		mark = markKindInferred
	}
	var read keySet
	if typingValue.by == byType {
		read = keyBit("type")
	}
	var kept finished
	id, diags := lowerUnion(loweringContext, ts, depth, schema, pointer, typeArray{types: typingValue.types, mark: mark},
		mark.then(keepUnreadAcross(loweringContext, ts, schema, nodeShape(ir.KindUnion), read, pointer, st, &kept, fin)), st)
	return id, append(diags, kept.diags...)
}

// claimedKinds returns the kinds c's kind-inference policy reads off the
// keywords s writes that give a type to the instances they apply to, a
// member's, an element's or a value's (siblingKeyword.infers), and none where
// the policy is off. The run's admission reads it as the kinds a statement
// declaring none lowers to (readOne), which lowerTyping lowers by
// (untypedKinds).
func claimedKinds(loweringContext lowering.Ctx, schema *statement.Schema) kindSet {
	if !loweringContext.KindInference.Honours() {
		return 0
	}
	var out kindSet
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, siblingKeyword := range set {
			if siblingKeyword.infers != nil && siblingKeyword.claims&^out != 0 && siblingKeyword.infers(loweringContext.Statements(), schema) {
				out |= siblingKeyword.claims
			}
		}
	}
	return out
}

// shapeKinds returns the kinds of instance the shape keywords schema writes
// apply to alone, whether or not they infer one: an object for a
// `discriminator` and for each keyword whose row in shapeKeywords or
// narrowingKeywords claims one, as its predicate reads it written, an array
// likewise (docs/compilers/openapi.md#shape-applicator). It is the one reading
// of what they speak of.
func shapeKinds(schema *statement.Schema) kindSet {
	var out kindSet
	if schema.Discriminator() != nil {
		out = kindObject
	}
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, siblingKeyword := range set {
			if siblingKeyword.claims&^out != 0 && siblingKeyword.writes(schema) {
				out |= siblingKeyword.claims
			}
		}
	}
	return out
}

// markKindInferred names the kind-inference policy in the provenance of td,
// in hand, the node a position owns whose kind lowerTyping read off its shape
// keywords (docs/compilers/openapi.md#policies).
func markKindInferred(td ir.TypeDef, _ keySet, _ *principalKept) {
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
	infers func(*statement.MemoizingSchemaReader, *statement.Schema) bool
}

// shapeKeywords are the keywords that declare data shape — a type, a property
// set, a value set, composition, or a catch-all — rather than only narrowing
// what an already-declared shape accepts (docs/compilers/openapi.md#union-beside-structure).
var shapeKeywords = []siblingKeyword{
	{"properties", func(schema *statement.Schema) bool { return len(schema.ObjectKeywords().Properties) > 0 }, kindObject,
		func(_ *statement.MemoizingSchemaReader, schema *statement.Schema) bool {
			return len(schema.ObjectKeywords().Properties) > 0
		}},
	{"const", func(schema *statement.Schema) bool { return schema.Const() != nil }, 0, nil},
	{"enum", enumWritten, 0, nil},
	{"allOf", writesConjuncts, 0, nil},
	{"additionalProperties", func(schema *statement.Schema) bool {
		return written(schema.ObjectKeywords().AdditionalProperties)
	}, kindObject, func(memoizingSchemaReader *statement.MemoizingSchemaReader, schema *statement.Schema) bool {
		return typesInstances(memoizingSchemaReader, schema.ObjectKeywords().AdditionalProperties)
	}},
	{"patternProperties", func(schema *statement.Schema) bool { return len(schema.ObjectKeywords().PatternProperties) > 0 }, kindObject,
		func(_ *statement.MemoizingSchemaReader, schema *statement.Schema) bool {
			return len(schema.ObjectKeywords().PatternProperties) > 0
		}},
	{"type", func(schema *statement.Schema) bool { return len(effectiveTypes(schema)) > 0 }, 0, nil},
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
	{"required", func(schema *statement.Schema) bool { return len(schema.ObjectKeywords().Required) > 0 }, kindObject, nil},
	{"items", func(schema *statement.Schema) bool {
		_, listed := listedItems(schema)
		return listed || written(schema.ArrayKeywords().Items)
	}, kindArray, func(memoizingSchemaReader *statement.MemoizingSchemaReader, schema *statement.Schema) bool {
		_, listed := listedItems(schema)
		return listed || typesInstances(memoizingSchemaReader, schema.ArrayKeywords().Items)
	}},
	{"prefixItems", func(schema *statement.Schema) bool { return len(schema.ArrayKeywords().PrefixItems) > 0 }, kindArray,
		func(_ *statement.MemoizingSchemaReader, schema *statement.Schema) bool {
			return len(schema.ArrayKeywords().PrefixItems) > 0
		}},
}

// typesInstances reports whether sub, the schema an `items` or an
// `additionalProperties` writes, gives a type to the instances it applies to: a
// schema object writing a keyword. `true` and `{}` state nothing, as leaving
// the keyword out does (JSON Schema Core 2020-12 §10.3.1.2, §10.3.2.3), and
// `false`, which admits no instance, types none either.
func typesInstances(memoizingSchemaReader *statement.MemoizingSchemaReader, sub statement.Subschema) bool {
	return sub.SchemaValueForm() == statement.Stated && len(memoizingSchemaReader.Subschema(sub).Keys) > 0
}

// siblingKeywords returns the keywords of shapeKeywords and narrowingKeywords
// that schema writes, in that order.
func siblingKeywords(schema *statement.Schema) []string {
	var out []string
	for _, set := range [...][]siblingKeyword{shapeKeywords, narrowingKeywords} {
		for _, siblingKeyword := range set {
			if siblingKeyword.writes(schema) {
				out = append(out, siblingKeyword.key)
			}
		}
	}
	return out
}

// writesSiblingKeyword reports whether siblingKeywords(schema) holds any
// keyword, without building the list, as most schemas a reader asks of write
// one.
func writesSiblingKeyword(schema *statement.Schema) bool {
	writes := func(siblingKeywordValue siblingKeyword) bool { return siblingKeywordValue.writes(schema) }
	return slices.ContainsFunc(shapeKeywords, writes) || slices.ContainsFunc(narrowingKeywords, writes)
}
