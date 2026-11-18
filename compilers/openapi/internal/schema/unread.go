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

// unreadKeys returns the keys of censusDomain s writes that read does not
// hold, in censusDomain order: what the node a position lowered to keeps
// verbatim, as no build read it into a field and no election accounted for it.
func unreadKeys(s *statement.Schema, read keySet) []string {
	var out []string
	for _, k := range censusDomain {
		if hasKey(s, k) && !read.has(k) {
			out = append(out, k)
		}
	}
	return out
}

// unreadByReference is unreadKeys for a position s lowered to a reference,
// whose alias or carrier reads what referenceReads says, beside what is read
// outside a node's build (readElsewhere) and what the position's lowering
// accounted for itself, handled.
func unreadByReference(c lowering.Ctx, s *statement.Schema, handled keySet) []string {
	return unreadKeys(s, referenceReads(c, s)|readElsewhere(s)|handled)
}

// What a position lowering to a reference lowered to, for the diagnostic
// recordUnhomedAt reports.
const (
	collapsedShape = "a nullable reference to its one non-null branch"
	expandedShape  = "a reference to the type its $dynamicRef expands to"
)

// expandedShapeOf is what s, written as a dynamic reference that expanded,
// lowered to, naming the keyword it expanded through: a $dynamicRef wherever s
// writes one, as dynamicRefOf reads it, else its $recursiveRef.
func expandedShapeOf(s *statement.Schema) string {
	if hasKey(s, "$dynamicRef") {
		return expandedShape
	}
	return "a reference to the type its $recursiveRef expands to"
}

// referenceOutcomeKeywords returns the keys s writes that the position keeps
// where it lowers to a reference to a node that is not its own, and what it
// lowered to: a collapsed union (lowerOneOfAnyOf), or an expanded $dynamicRef,
// which a lowering of s decides first. It is nil for any other outcome. The
// target belongs to its declaration as a $ref site's does to the referent, so
// only what the position's alias or carrier reads is homed (unreadByReference).
// Those two write few: neither expands or collapses beside a shape (GitHub #595).
func referenceOutcomeKeywords(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) ([]string, string) {
	if _, _, expanded := dynamicExpansion(c, s, pointer); expanded {
		return unreadByReference(c, s, 0), expandedShapeOf(s)
	}
	if hasUnionSiblings(c, s) {
		return nil, ""
	}
	if _, _, collapses := nullUnionCollapse(c, s, ""); !collapses {
		return nil, ""
	}
	return unreadByReference(c, s, keySetOf(branchesSay(c, s, s)...)), collapsedShape
}

// keepingUnread lowers the position s, written at pointer, through build, the
// node build interns there finished by keepUnread before fin, the outer
// frames' finisher. One lowering to a shared node keeps what it did not
// read on an alias (keepUnreadShared). It returns the position's resulting ID
// and the build's findings, the keeping's after them, as they always stood.
// docs/compilers/openapi.md#shape-applicator covers a contradictory schema's
// dropped half and an untyped applicator such as `items`. build returns, beside
// the ID, what a lowering to a shared node read.
func keepingUnread(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, d dispatch, fin finisher, st stand,
	build func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic),
) (ir.TypeID, []ir.Diagnostic) {
	var kept finished
	id, shared, diags := build(func(td ir.TypeDef, read keySet) {
		kept.ran = true
		kept.diags = keepUnread(c, ts, s, pointer, td, d, read)
		if fin != nil {
			fin(td, read)
		}
	})
	if kept.ran || LoweredToOwnNode(c, st.pointer(pointer), ir.TypeRef{Target: id}) {
		// A node the position owns that this visit found built was finished by
		// the visit building it.
		return id, append(diags, kept.diags...)
	}
	owner, sharedDiags := keepUnreadShared(c, ts, s, pointer, id, d, shared, fin, st)
	return owner, append(diags, sharedDiags...)
}

// keepUnread keeps verbatim on td, the node the position owns, in hand, each
// key of censusDomain s, written at pointer, writes that the build of td did
// not read, nor a reader beside it (readElsewhere), nor d's election skipped or
// said, and the families it skipped.
//
// A multipart body keys no part for `properties` the node lacks
// (docs/compilers/openapi.md#multipart-encoding).
func keepUnread(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, td ir.TypeDef, d dispatch,
	read keySet,
) []ir.Diagnostic {
	unread := unreadKeys(s, read|readBeside(s, td)|d.handled())
	var diags []ir.Diagnostic
	if len(unread) > 0 {
		diags = recordUnhomedAt(c, ts, &td.Common().Unmodeled, s, unread, positionShape(td, s), pointer)
	}
	return append(diags, recordSkippedFamilies(c, td, s, d, pointer)...)
}

// keepUnreadShared is keepUnread for a position that lowered to id, a shared
// primitive, which read shared of s: the alias hoisted over it where st says,
// finished by the keeping and then fin, holds what neither read, the alias
// reading the bounds the types the position admits take. It returns id
// itself, fin unfinished, where nothing is kept.
func keepUnreadShared(c lowering.Ctx, ts *compile.Types, s *statement.Schema, pointer jsontext.Pointer, id ir.TypeID, d dispatch,
	shared keySet, fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	unread := unreadKeys(s, shared|boundsRead(s, st.takes(c, s), nil)|readElsewhere(s)|d.handled())
	if len(unread) == 0 && len(d.skipped) == 0 {
		return id, nil
	}
	keep := keepUnhomedOn(c, ts, s, unread, nodeShape(ir.KindPrimitive), pointer)
	var kept finished
	owner, aliasDiags := internAlias(c, ts, s, pointer, ir.TypeRef{Target: id}, kept.on(func(alias ir.TypeDef) []ir.Diagnostic {
		return append(keep(alias), recordSkippedFamilies(c, alias, s, d, pointer)...)
	}).then(fin), st)
	return owner, append(aliasDiags, kept.diags...)
}

// keepUnhomedOn returns what keeps unhomed, the keywords s, written at pointer,
// declares that no build read into the position's node, on that node in hand,
// reporting what the position lowered to as shape.
func keepUnhomedOn(c lowering.Ctx, ts *compile.Types, s *statement.Schema, unhomed []string, shape string, pointer jsontext.Pointer,
) func(td ir.TypeDef) []ir.Diagnostic {
	return func(td ir.TypeDef) []ir.Diagnostic {
		return recordUnhomedAt(c, ts, &td.Common().Unmodeled, s, unhomed, shape, pointer)
	}
}

// refSiteShape describes what a $ref position's own declaration lowers to, for
// the diagnostic recordUnhomedAt reports.
const refSiteShape = "an alias over its $ref target"

// nodeShape describes a node of kind k, for that same diagnostic.
func nodeShape(kind ir.TypeKind) string { return fmt.Sprintf("a node of kind %q", kind) }

// positionShape is nodeShape for the node td, the position's declaration s
// lowered to. An Enum's ValueType is the home of its type and of any format the
// table pairs, so an enum that left one verbatim says its members are why: its
// kind alone would deny the home. So does a node that reads s's discriminator
// and holds none (taglessShape): a Model, or a Union s's oneOf or anyOf builds,
// as the Union an enum lowers to reads none, whatever its tag.
func positionShape(td ir.TypeDef, s *statement.Schema) string {
	if e, ok := td.(*ir.Enum); ok {
		if !membersTyped(e.Members, s) {
			return "an enum whose members are not all values of its declared type"
		}
		if prim, paired := formatPrimitive(s); paired && !enumMembersAre(e.Members, prim) {
			return fmt.Sprintf("an enum whose members are not all values of %s", prim)
		}
	}
	shape := nodeShape(td.Kind())
	if m, ok := td.(*ir.Model); ok {
		return taglessShape(m, s, shape)
	}
	if isKind(td, ir.KindUnion) && declaresUnion(s) {
		return taglessShape(nil, s, shape)
	}
	return shape
}

// taglessShape is shape, what s lowered to as a node reading s's
// discriminator, m where that is a Model and nil for a Union, unless the IR
// cannot locate the tag it names on the node (tagLocated), the reason the node
// holds none: then it says so, as shape alone would deny a node of that kind
// the field.
func taglessShape(m *ir.Model, s *statement.Schema, shape string) string {
	d := s.Discriminator()
	if d == nil || tagLocated(d, m) {
		return shape
	}
	kind := ir.KindUnion
	if m != nil {
		kind = ir.KindModel
	}
	return fmt.Sprintf("a %s routed by no tag (the IR's PropertyName cannot hold the empty name its "+
		"discriminator gives the tag property)", kind)
}

// distributedShape describes the Union a body distributed across its variants
// lowers to, for that same diagnostic.
const distributedShape = "a union of models conjoining its body with each branch"

// keepUnreadAcross returns the keeping of the Union, in hand, that a
// position's own schema s builds the variants of: a type array's (lowerUnion)
// and a distributed composition's (composedVariant). It keeps on the Union what
// s writes that neither it nor a variant read, read being what they did,
// nor its lowering accounted for, handled. A discriminator the Union holds is
// homed there, routing its variants. shape describes the Union.
func keepUnreadAcross(c lowering.Ctx, ts *compile.Types, s *statement.Schema, shape string, handled keySet, pointer jsontext.Pointer,
) func(td ir.TypeDef, read keySet) []ir.Diagnostic {
	return func(td ir.TypeDef, read keySet) []ir.Diagnostic {
		u, isUnion := td.(*ir.Union)
		if !isUnion {
			return []ir.Diagnostic{c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
				"internal: type %q is not the union its variants were built for", td.Common().ID)}
		}
		read |= readElsewhere(s) | handled
		if u.Discriminator != nil {
			read |= keyBit("discriminator")
		}
		unread := unreadKeys(s, read)
		if len(unread) == 0 {
			return nil
		}
		return recordUnhomedAt(c, ts, &u.Unmodeled, s, unread, shape, pointer)
	}
}

// isKind reports whether td is a live node of kind k.
func isKind(td ir.TypeDef, k ir.TypeKind) bool {
	return !ir.IsNilTypeDef(td) && td.Kind() == k
}

// recordUnhomedAt keeps each unhomed keyword verbatim in p and reports them once
// (keepUnhomed), saying what the position lowered to, its shape.
func recordUnhomedAt(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *statement.Schema, unhomed []string, shape string, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := keepUnhomed(c, ts, p, s, unhomed, pointer)
	if len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"this position lowered to %s, which has no home for %s declared beside it; kept verbatim under Unmodeled",
		shape, strings.Join(kept, ", ")))
}

// keepUnhomed keeps each unhomed keyword s, the schema at pointer, writes
// verbatim in p as degraded_lowering, reports a collection count no field states
// (keptCount) apart, as keepCounts reports a value count, and returns the rest
// it kept, for its caller to report once.
//
// It returns the keywords it stored, never the ones it was handed, so no report
// claims one that failed to convert (GitHub #144). A discriminator it is handed
// is no node's, so its mapping values are resolved here, as references
// (resolveDiscriminatorRoutes): every keeping of one comes through here.
func keepUnhomed(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *statement.Schema, unhomed []string, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	if slices.Contains(unhomed, "discriminator") {
		diags = resolveDiscriminatorRoutes(c, ts, s, pointer)
	}
	kept := make([]string, 0, len(unhomed))
	for _, keyword := range unhomed {
		ok, keptDiags := annotation.PreserveSchemaKeyword(p, s, keyword,
			ir.ReasonDegradedLowering, pointer, c.Place())
		diags = append(diags, keptDiags...)
		if ok {
			kept = append(kept, keyword)
		}
	}
	counts := slices.DeleteFunc(slices.Clone(kept), func(keyword string) bool {
		return keyword != "maxItems" && keyword != "minItems" || !keptCount(s, keyword, countOf(s, keyword))
	})
	kept = slices.DeleteFunc(kept, func(keyword string) bool { return slices.Contains(counts, keyword) })
	if len(counts) > 0 {
		diags = append(diags, countsKept(c, counts, pointer))
	}
	return kept, diags
}
