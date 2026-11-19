// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// unreadKeys returns the keys of censusDomain schema writes that read does not
// hold, in censusDomain order: what the node a position lowered to keeps
// verbatim, as no build read it into a field and no election accounted for it.
func unreadKeys(schema *statement.Schema, read keySet) []string {
	var out []string
	for _, k := range censusDomain {
		if hasKey(schema, k) && !read.has(k) {
			out = append(out, k)
		}
	}
	return out
}

// unreadByReference is unreadKeys for a position schema lowered to a reference,
// whose alias or carrier reads what referenceReads says, beside what is read
// outside a node's build (readElsewhere) and what the position's lowering
// accounted for itself, handled.
func unreadByReference(loweringContext lowering.Ctx, schema *statement.Schema, handled keySet) []string {
	return unreadKeys(schema, referenceReads(loweringContext, schema)|readElsewhere(schema)|handled)
}

// What a position lowering to a reference lowered to, for the diagnostic
// recordUnhomedAt reports.
const (
	collapsedShape = "a nullable reference to its one non-null branch"
	expandedShape  = "a reference to the type its $dynamicRef expands to"
)

// expandedShapeOf is what schema, written as a dynamic reference that expanded,
// lowered to, naming the keyword it expanded through: a $dynamicRef wherever schema
// writes one, as dynamicRefOf reads it, else its $recursiveRef.
func expandedShapeOf(schema *statement.Schema) string {
	if hasKey(schema, "$dynamicRef") {
		return expandedShape
	}
	return "a reference to the type its $recursiveRef expands to"
}

// referenceOutcomeKeywords returns the keys schema writes that the position keeps
// where it lowers to a reference to a node that is not its own, and what it
// lowered to: a collapsed union (lowerOneOfAnyOf), or an expanded $dynamicRef,
// which a lowering of schema decides first. It is nil for any other outcome. The
// target belongs to its declaration as a $ref site's does to the referent, so
// only what the position's alias or carrier reads is homed (unreadByReference).
// Those two write few: neither expands or collapses beside a shape (GitHub #595).
func referenceOutcomeKeywords(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) ([]string, string) {
	if _, _, expanded := dynamicExpansion(loweringContext, schema, pointer); expanded {
		return unreadByReference(loweringContext, schema, 0), expandedShapeOf(schema)
	}
	if hasUnionSiblings(loweringContext, schema) {
		return nil, ""
	}
	if _, _, collapses := nullUnionCollapse(loweringContext, schema, ""); !collapses {
		return nil, ""
	}
	return unreadByReference(loweringContext, schema, keySetOf(branchesSay(loweringContext, schema, schema)...)), collapsedShape
}

// keepingUnread lowers the position schema, written at pointer, through build, the
// node build interns there finished by keepUnread before fin, the outer
// frames' finisher. One lowering to a shared node keeps what it did not
// read on an alias (keepUnreadShared). It returns the position's resulting ID
// and the build's findings, the keeping's after them, as they always stood.
// docs/compilers/openapi.md#shape-applicator covers a contradictory schema's
// dropped half and an untyped applicator such as `items`. build returns, beside
// the ID, what a lowering to a shared node read.
func keepingUnread(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, pointer jsontext.Pointer, dispatchValue dispatch, fin finisher, st stand,
	build func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic),
) (ir.TypeID, []ir.Diagnostic) {
	var kept finished
	id, shared, diags := build(func(td ir.TypeDef, read keySet, held *principalKept) {
		kept.ran = true
		kept.diags = keepUnread(loweringContext, ts, schema, pointer, td, dispatchValue, read, st, held, fin)
	})
	if kept.ran || LoweredToOwnNode(loweringContext, st.pointer(pointer), ir.TypeRef{Target: id}) {
		// A node the position owns that this visit found built was finished by
		// the visit building it.
		return id, append(diags, kept.diags...)
	}
	owner, sharedDiags := keepUnreadShared(loweringContext, ts, schema, pointer, id, dispatchValue, shared, fin, st)
	return owner, append(diags, sharedDiags...)
}

// keepUnread keeps verbatim on td, the node the position owns, in hand, each
// key of censusDomain schema, written at pointer, writes that the build of td did
// not read, nor a reader beside it (readElsewhere), nor dispatchValue's election skipped or
// said, and the families it skipped, then finishes td by fin, as keepAround
// orders the two where the node stands for st's position.
//
// A multipart body keys no part for `properties` the node lacks
// (docs/compilers/openapi.md#multipart-encoding).
func keepUnread(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, pointer jsontext.Pointer, td ir.TypeDef, dispatchValue dispatch,
	read keySet, st stand, held *principalKept, fin finisher,
) []ir.Diagnostic {
	unread := unreadKeys(schema, read|readBeside(schema, td)|dispatchValue.handled())
	if st.of == nil {
		// The schema's own node, the path every node takes: keep, then finish.
		var diags []ir.Diagnostic
		if len(unread) > 0 {
			diags = recordUnhomedAt(loweringContext, ts, &td.Common().Unmodeled, schema, unread, positionShape(td, schema), pointer)
		}
		diags = append(diags, recordSkippedFamilies(loweringContext, td, schema, dispatchValue, pointer)...)
		fin.run(td, read, held)
		return diags
	}
	var skipped finished
	fin = skipped.on(func(td ir.TypeDef) []ir.Diagnostic {
		return recordSkippedFamilies(loweringContext, td, schema, dispatchValue, pointer)
	}).then(fin)
	shape := positionShape(td, schema)
	keepingValue := keeping{keys: unread, reason: ir.ReasonDegradedLowering, shape: shape, keep: func(unmodeled *ir.Unmodeled, keys []string) []ir.Diagnostic {
		return recordUnhomedAt(loweringContext, ts, unmodeled, schema, keys, shape, pointer)
	}}
	return append(keepAround(loweringContext, ts, st, td, read, held, schema, pointer, keepingValue, fin), skipped.diags...)
}

// keepUnreadShared is keepUnread for a position that lowered to id, a shared
// primitive, which read shared of schema: the alias hoisted over it where st says,
// finished by the keeping and then fin, holds what neither read, the alias
// reading the bounds the types the position admits take. It returns id
// itself, fin unfinished, where nothing is kept.
func keepUnreadShared(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, pointer jsontext.Pointer, id ir.TypeID, dispatchValue dispatch,
	shared keySet, fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	unread := unreadKeys(schema, shared|boundsRead(schema, st.takes(loweringContext, schema), nil)|readElsewhere(schema)|dispatchValue.handled())
	if len(unread) == 0 && len(dispatchValue.skipped) == 0 {
		return id, nil
	}
	var kept, skipped finished
	fin = skipped.on(func(alias ir.TypeDef) []ir.Diagnostic {
		return recordSkippedFamilies(loweringContext, alias, schema, dispatchValue, pointer)
	}).then(fin)
	keep := keepUnhomedOn(loweringContext, ts, schema, unread, nodeShape(ir.KindPrimitive), pointer, st, &kept, fin)
	owner, aliasDiags := internAlias(loweringContext, ts, schema, pointer, ir.TypeRef{Target: id}, keep, st)
	return owner, slices.Concat(aliasDiags, kept.diags, skipped.diags)
}

// keepUnhomedOn returns the finisher keeping unhomed, the keywords schema, written
// at pointer, declares that no build read into the position's node, on that
// node in hand, reporting what the position lowered to as shape and recording
// into kept, then finishing it by fin, as keepAround orders the two where the
// node stands for st's position.
func keepUnhomedOn(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, unhomed []string, shape string, pointer jsontext.Pointer,
	st stand, kept *finished, fin finisher,
) finisher {
	keepingValue := keeping{keys: unhomed, reason: ir.ReasonDegradedLowering, shape: shape, keep: func(unmodeled *ir.Unmodeled, keys []string) []ir.Diagnostic {
		return recordUnhomedAt(loweringContext, ts, unmodeled, schema, keys, shape, pointer)
	}}
	return func(td ir.TypeDef, read keySet, held *principalKept) {
		kept.ran = true
		kept.diags = append(kept.diags, keepAround(loweringContext, ts, st, td, read, held, schema, pointer, keepingValue, fin)...)
	}
}

// refSiteShape describes what a $ref position's own declaration lowers to, for
// the diagnostic recordUnhomedAt reports.
const refSiteShape = "an alias over its $ref target"

// nodeShape describes a node of kind k, for that same diagnostic.
func nodeShape(kind ir.TypeKind) string { return fmt.Sprintf("a node of kind %q", kind) }

// positionShape is nodeShape for the node td, the position's declaration schema
// lowered to. An Enum's ValueType is the home of its type and of any format the
// table pairs, so an enum that left one verbatim says its members are why: its
// kind alone would deny the home. So does a node that reads schema's discriminator
// and holds none (taglessShape): a Model, or a Union schema's oneOf or anyOf builds,
// as the Union an enum lowers to reads none, whatever its tag.
func positionShape(td ir.TypeDef, schema *statement.Schema) string {
	if enum, ok := td.(*ir.Enum); ok {
		if !membersTyped(enum.Members, schema) {
			return "an enum whose members are not all values of its declared type"
		}
		if prim, paired := formatPrimitive(schema); paired && !enumMembersAre(enum.Members, prim) {
			return fmt.Sprintf("an enum whose members are not all values of %s", prim)
		}
	}
	shape := nodeShape(td.Kind())
	if model, ok := td.(*ir.Model); ok {
		return taglessShape(model, schema, shape)
	}
	if isKind(td, ir.KindUnion) && declaresUnion(schema) {
		return taglessShape(nil, schema, shape)
	}
	return shape
}

// taglessShape is shape, what schema lowered to as a node reading schema's
// discriminator, model where that is a Model and nil for a Union, unless the IR
// cannot locate the tag it names on the node (tagLocated), the reason the node
// holds none: then it says so, as shape alone would deny a node of that kind
// the field.
func taglessShape(model *ir.Model, schema *statement.Schema, shape string) string {
	discriminator := schema.Discriminator()
	if discriminator == nil || tagLocated(discriminator, model) {
		return shape
	}
	kind := ir.KindUnion
	if model != nil {
		kind = ir.KindModel
	}
	return fmt.Sprintf("a %s routed by no tag (the IR's PropertyName cannot hold the empty name its "+
		"discriminator gives the tag property)", kind)
}

// distributedShape describes the Union a body distributed across its variants
// lowers to, for that same diagnostic.
const distributedShape = "a union of models conjoining its body with each branch"

// keepUnreadAcross returns the keeping of the Union, in hand, that a
// position's own schema schema builds the variants of: a type array's (lowerUnion)
// and a distributed composition's (composedVariant). It keeps on the Union what
// schema writes that neither it nor a variant read, read being what they did,
// nor its lowering accounted for, handled, recording into kept, then finishes
// it by fin, as keepAround orders the two where the Union stands for st's
// position. A discriminator the Union holds is homed there, routing its
// variants. shape describes the Union.
func keepUnreadAcross(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, shape string, handled keySet, pointer jsontext.Pointer,
	st stand, kept *finished, fin finisher,
) finisher {
	return func(td ir.TypeDef, built keySet, held *principalKept) {
		kept.ran = true
		union, isUnion := td.(*ir.Union)
		if !isUnion {
			kept.diags = append(kept.diags, loweringContext.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
				"internal: type %q is not the union its variants were built for", td.Common().ID))
			fin.run(td, built, held)
			return
		}
		read := built | readElsewhere(schema) | handled
		if union.Discriminator != nil {
			read |= keyBit("discriminator")
		}
		keepingValue := keeping{keys: unreadKeys(schema, read), reason: ir.ReasonDegradedLowering, shape: shape, keep: func(unmodeled *ir.Unmodeled, keys []string) []ir.Diagnostic {
			return recordUnhomedAt(loweringContext, ts, unmodeled, schema, keys, shape, pointer)
		}}
		kept.diags = append(kept.diags, keepAround(loweringContext, ts, st, td, built, held, schema, pointer, keepingValue, fin)...)
	}
}

// isKind reports whether td is a live node of kind typeKind.
func isKind(td ir.TypeDef, typeKind ir.TypeKind) bool {
	return !ir.IsNilTypeDef(td) && td.Kind() == typeKind
}

// recordUnhomedAt keeps each unhomed keyword verbatim in unmodeled and reports them once
// (keepUnhomed), saying what the position lowered to, its shape.
func recordUnhomedAt(loweringContext lowering.Ctx, ts *compile.Types, unmodeled *ir.Unmodeled, schema *statement.Schema, unhomed []string, shape string, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := keepUnhomed(loweringContext, ts, unmodeled, schema, unhomed, pointer)
	if len(kept) == 0 {
		return diags
	}
	return append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"this position lowered to %s, which has no home for %s declared beside it; kept verbatim under Unmodeled",
		shape, strings.Join(kept, ", ")))
}

// keepUnhomed keeps each unhomed keyword schema, the schema at pointer, writes
// verbatim in unmodeled as degraded_lowering, reports a collection count no field states
// (keptCount) apart, as keepCounts reports a value count, and returns the rest
// it kept, for its caller to report once.
//
// It returns the keywords it stored, never the ones it was handed, so no report
// claims one that failed to convert (GitHub #144). A discriminator it is handed
// is no node's, so its mapping values are resolved here, as references
// (resolveDiscriminatorRoutes): every keeping of one comes through here.
func keepUnhomed(loweringContext lowering.Ctx, ts *compile.Types, unmodeled *ir.Unmodeled, schema *statement.Schema, unhomed []string, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	if slices.Contains(unhomed, "discriminator") {
		diags = resolveDiscriminatorRoutes(loweringContext, ts, schema, pointer)
	}
	kept := make([]string, 0, len(unhomed))
	for _, keyword := range unhomed {
		ok, keptDiags := annotation.PreserveSchemaKeyword(unmodeled, schema, keyword,
			ir.ReasonDegradedLowering, pointer, loweringContext.Place())
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, keyword)
		}
	}
	counts := slices.DeleteFunc(slices.Clone(kept), func(keyword string) bool {
		return keyword != "maxItems" && keyword != "minItems" || !keptCount(schema, keyword, countOf(schema, keyword))
	})
	kept = slices.DeleteFunc(kept, func(keyword string) bool { return slices.Contains(counts, keyword) })
	if len(counts) > 0 {
		diags = append(diags, countsKept(loweringContext, counts, pointer))
	}
	return kept, diags
}
