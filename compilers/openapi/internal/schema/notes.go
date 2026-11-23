// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// carriedNotes is which of the notes a schema writes, its documentation,
// deprecation and examples, the carrier holding its position holds, the node
// the position builds keeping the rest (notesOnCarrier).
type carriedNotes struct {
	docs, deprecation, examples bool
	// deprecatedAsSchema is that the carrier is deprecated wherever the schema
	// is, by its keyword or a key, even where the node keeps the schema's
	// deprecation: a carrier writing only a member of its own is deprecated
	// still, holding that member alone.
	deprecatedAsSchema bool
}

// notesOnCarrier returns which notes schema writes the carrier holding its
// position, pointer, holds, though typeRef names the node that position builds:
// those carriedNotesAt gives it. Node and carrier both ask it, so each note has
// one home (docs/ir/annotations.md#one-structural-home-per-declaration).
//
// A position naming a $ref among its conjuncts (allOfRefEntries) holds none
// unless it is a use of that $ref once its carrier holds every note
// (usesOneRefHolding). So a referent's own notes, at a position writing another
// conjunct, an enum's among them, stay on that position's node.
func notesOnCarrier(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer, typeRef ir.TypeRef) carriedNotes {
	if !stated(schema) || !LoweredToOwnNode(loweringContext, pointer, typeRef) {
		return carriedNotes{}
	}
	for range allOfRefEntries(loweringContext, schema) {
		if usesOneRefHolding(loweringContext, schema, allOfEntries(loweringContext, schema, ""), carriedNotes{docs: true, deprecation: true, examples: true}) {
			break
		}
		return carriedNotes{}
	}
	return carriedNotesAt(loweringContext, schema)
}

// carriedNotesAt returns which notes schema writes the carrier holding its
// position holds: none unless it holds the position (carriedAt) and no
// reference names it (reftable.ReferenceTargetTable.Referenced), and then
// each note nothing at its position writes over (writtenOver), deprecated
// where the schema is either way. It reads where schema is written alone, so
// the node's build, its carrier and the decision on a $ref beside notes read
// one answer.
func carriedNotesAt(loweringContext lowering.Ctx, schema *statement.Schema) carriedNotes {
	if !carriedAt(loweringContext, schema) {
		return carriedNotes{}
	}
	at, _ := siteOf(schema)
	if loweringContext.Bindings().Table().Referenced(at) {
		return carriedNotes{}
	}
	docs, deprecation, examples := writtenOver(loweringContext, at)
	return carriedNotes{docs: !docs, deprecation: !deprecation, examples: !examples, deprecatedAsSchema: true}
}

// holds reports whether key writes a note notes gives the carrier: a keyword
// that documents, deprecates or exemplifies the instance.
func (notes carriedNotes) holds(key statement.Key) bool {
	switch key.Name {
	case "title", "description", "externalDocs":
		return notes.docs
	case "deprecated":
		return notes.deprecation
	case "example", "examples":
		return notes.examples
	default:
		return false
	}
}

// writtenOver reports which notes of the schema at, a carried position, the
// carrier writes over with its own, each written by the Parameter or Header
// Object holding at: its documentation, where that object writes a
// description or a Reference Object naming it writes docs beside its $ref
// (documentedAtUse); its deprecation, where that object or the content entry
// at lies in states one (writesDeprecation); and its examples, where either
// writes an example. The schema's stay on its node, the carrier holding its
// own. A property's holder is its schema, which writes over nothing.
func writtenOver(loweringContext lowering.Ctx, at reftable.DocumentPointer) (docs, deprecation, examples bool) {
	holder := reftable.DocumentPointer{Doc: at.Doc, Pointer: at.Pointer.Parent()}
	entry := ""
	if holder.Pointer.Parent().LastToken() == "content" {
		entry = holder.Pointer.LastToken()
		holder.Pointer = holder.Pointer.Parent().Parent()
	}
	object := componentlookup.WrittenAt(loweringContext.Bindings(), holder, reftable.RefParameter)
	if object == nil {
		object = componentlookup.WrittenAt(loweringContext.Bindings(), holder, reftable.RefHeader)
	}
	if object == nil {
		return false, false, false
	}
	var entryObject *yaml.Node
	if entry != "" {
		entryObject = nodeview.Child(nodeview.Child(object, "content"), entry)
	}
	_, docs = componentlookup.DocText(object, "description")
	docs = docs || documentedAtUse(loweringContext)[holder]
	deprecation = writesDeprecation(loweringContext.Promoter(), object, entryObject)
	return docs, deprecation, writesExample(object) || writesExample(entryObject)
}

// writesDeprecation reports whether object, a Parameter or Header Object's
// mapping, or entry, the content entry it elects or nil, states a deprecation
// of its own: object writes `deprecated: true` as a boolean, as the parser
// reads it, a value of another type stating none, even a string an alias
// keeps from load.turnRefused where another field accepts it; or either writes
// a key promoter reads a deprecation or one of its members from, whatever its
// value says (promotion.DeprecationKey). A Reference Object states none: the
// keys beside its $ref are promoted nowhere
// (docs/compilers/openapi.md#reference-object-siblings), so every use agrees.
func writesDeprecation(promoter promotion.Promoter, object, entry *yaml.Node) bool {
	flag := nodeview.Deref(nodeview.Child(object, "deprecated"))
	if flag != nil && flag.ShortTag() == "!!bool" && strings.EqualFold(flag.Value, "true") {
		return true
	}
	for _, mapping := range [...]*yaml.Node{object, entry} {
		for _, pair := range nodeview.Pairs(mapping) {
			if promotion.DeprecationKey(promoter, pair.Key) {
				return true
			}
		}
	}
	return false
}

// writesExample reports whether object, an object's mapping, writes an
// example, under `example` or as an entry of `examples`.
func writesExample(object *yaml.Node) bool {
	return nodeview.Child(object, "example") != nil || len(nodeview.Pairs(nodeview.Child(object, "examples"))) > 0
}

// documentedUses keys documentedAtUse's reading.
type documentedUses struct{}

// documentedAtUse returns the Parameter and Header Objects a Reference Object
// whose chain resolves to one writes a summary or a description beside, read
// once for the run: a carrier reached through it takes them over its
// declaration's (componentlookup.UseDocs). Its schema's node keeps the docs
// for every use, so a use writing none holds none on its carrier. Each hop of
// a chain is a Reference Object of its own, ending where the chain does.
func documentedAtUse(loweringContext lowering.Ctx) map[reftable.DocumentPointer]bool {
	return lowering.ReadOncePerRun(loweringContext, documentedUses{}, func() map[reftable.DocumentPointer]bool {
		documented := map[reftable.DocumentPointer]bool{}
		for _, reference := range loweringContext.Bindings().Table().References() {
			if reference.Chain.Status != reftable.Resolved || reference.Kind != reftable.RefParameter && reference.Kind != reftable.RefHeader {
				continue
			}
			written := componentlookup.WrittenAt(loweringContext.Bindings(), reference.Site, reference.Kind)
			_, summary := componentlookup.DocText(written, "summary")
			_, description := componentlookup.DocText(written, "description")
			if summary || description {
				documented[reference.Chain.End] = true
			}
		}
		return documented
	})
}

// CarriedAnnotations returns what a carrier holds of the annotations schema,
// written at pointer and lowered to typeRef, declares, read through use, and
// whether the position owns the node typeRef names. Where it owns none, the
// carrier holds them all. Where it owns one, the node keeps everything but the
// notes notesOnCarrier gives the carrier, which is deprecated wherever the
// schema is, holding the schema's members only where the node holds none.
//
// It is the one reading a property, a header and a parameter share, so the
// three cannot differ in what they hold.
func CarriedAnnotations(loweringContext lowering.Ctx, schema *statement.Schema, use Use, typeRef ir.TypeRef, pointer jsontext.Pointer,
) (annotation.Set, bool, []ir.Diagnostic) {
	if !LoweredToOwnNode(loweringContext, pointer, typeRef) {
		a, diags := annotation.Read(use.SiteAt(loweringContext, schema, pointer), pointer, loweringContext.Place())
		return a, false, diags
	}
	notes := notesOnCarrier(loweringContext, schema, pointer, typeRef)
	if notes == (carriedNotes{}) {
		return annotation.Set{}, true, nil
	}
	// The node's build read the same declaration and drew its findings, so this
	// reading draws none. Naming no $ref, the position falls back to nothing, so
	// use speaks as the node's use does (nodeUse).
	a, _ := annotation.Read(use.SiteAt(loweringContext, schema, pointer), pointer, loweringContext.Place())
	carried := annotation.Set{Deprecated: a.Deprecated}
	if notes.docs {
		carried.Docs = a.Docs
	}
	if notes.examples {
		carried.Examples = a.Examples
	}
	return carried, true, nil
}

// carriedNotesOf keys what carriedNoteKeys reads of one schema.
type carriedNotesOf struct {
	schema *statement.Schema
}

// carriedNoteKeys returns what the mapped keys the node at pointer keeps of
// schema, which lowered to typeRef, lend the carrier holding its deprecation: a
// deprecation a key states, and its members, read as the keys of an object the
// carrier carries are (promotion.Promoter.WithOwnCarriedState). The node keeps
// its redaction, so a key asking for one lends nothing here. A replacement is
// read once the document is whole, the carrier reading the key its node keeps
// (docs/compilers/openapi.md#extension-promotion). A $ref target's key is
// not among them, so it deprecates no carrier where the target's keyword would,
// a case left open (docs/compilers/openapi.md#ref-use-site).
func carriedNoteKeys(loweringContext lowering.Ctx, schema *statement.Schema, typeRef ir.TypeRef, pointer jsontext.Pointer) promotion.Referent {
	lent := loweringContext.Promoter().Referent(carriedNotesOf{schema}, true, func() ir.Unmodeled {
		a, _ := annotation.Read(nodeUse(loweringContext, schema, pointer, typeRef).SiteAt(loweringContext, schema, pointer), pointer, loweringContext.Place())
		return a.Unmodeled
	})
	lent.Redacted = false
	return lent
}

// placeNotes writes the notes a holds on common, the node schema, written at
// pointer and lowered to target, finishes, but those its carrier holds
// (notesOnCarrier). It returns where the promotion writes the node's
// deprecation: common's own, or one nothing keeps where the carrier holds it,
// so the promotion still reports a key it misreads, once, at the node keeping
// the key, while the carrier reads the key in silence (carriedNoteKeys).
//
// So a position a reference names keeps every note here, and one any of whose
// carriers writes a note of its own keeps the schema's, for every use.
func placeNotes(loweringContext lowering.Ctx, common *ir.TypeCommon, a annotation.Set, schema *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef,
) (**ir.Deprecation, *ir.Provenance) {
	notes := notesOnCarrier(loweringContext, schema, pointer, target)
	if !notes.docs {
		common.Docs = a.Docs
	}
	if !notes.examples && len(a.Examples) > 0 {
		common.Examples = a.Examples
	}
	var withheld *ir.Deprecation
	if a.Deprecated {
		withheld = &ir.Deprecation{}
	}
	if notes.deprecation {
		return &withheld, &ir.Provenance{}
	}
	if withheld != nil {
		common.Deprecation = withheld
	}
	return &common.Deprecation, &common.Provenance
}
