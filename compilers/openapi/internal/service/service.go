// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package service

import (
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// Ctx is everything a service lowering may read: the schema lowering's
// context, which it embeds, and what only the service lowerings read beside
// it. New builds one; the zero Ctx is invalid.
//
// It is a value, as lowering.Ctx is, and its maps stay unexported for the
// same reason: a copy shares a map, which an exported one would let a callee
// write through. Of what copies share, only expansion changes an answer: past
// its budget, no $ref resolves.
type Ctx struct {
	lowering.Ctx

	// Doc is the parsed, reference-resolved source document. Lowering reads it
	// and never writes through it.
	Doc *soa.OpenAPI
	// SrcIndex is the source's index within the compile.
	SrcIndex int
	// Grouping selects how operations are grouped into OperationGroups, and
	// GroupingExtension is what GroupByExtension reads. Both arrive as the
	// caller wrote them: the compiler's Options fills an unset one in, and a
	// strategy the operation lowering does not recognize groups by tags, which
	// is what makes the unnormalized zero value harmless.
	Grouping          GroupingStrategy
	GroupingExtension GroupingExtension

	// streaming and binary are the media-type policies, normalized into the
	// sets MediaTypeStreams and MediaTypeBinary answer from, and nil when the
	// caller disabled one.
	streaming, binary map[string]bool

	// loaded is what the load phase resolved with the document that only the
	// service lowerings read. It sits behind one pointer for the reason
	// lowering.Ctx's shared state does: every lowering takes a copy.
	loaded *loaded

	// expansion charges each $ref Declared resolves what the declarations its
	// chain names weigh, and each additionalOperations entry what it copies of
	// its path item (ChargeExpansion). Copies share it on purpose: the budget is
	// the run's, so once it is crossed no copy lowers a $ref or such an entry,
	// and the run is refused (ExpansionRefusal).
	expansion *expansion
}

// loaded holds what a service lowering reads of the load phase: the fields
// the path items write beside their $refs (Siblings), each media type written
// as a $ref (MediaTypeUse), every document a reference read beside the
// source, and the rows each document's Encoding Objects are read with. Copies
// share it on purpose: nothing writes it after the load, so no lowering
// changes what another reads.
type loaded struct {
	siblings load.Siblings
	media    load.MediaTypes
	others   load.Others
	encoding map[bind.DocID]oas.EncodingRules
}

// Policy is the caller's choice of how a source is lowered, as the compiler's
// Options resolve it: the schema lowering's, and what only the service
// lowerings honour.
type Policy struct {
	Schema                lowering.Policy
	Grouping              GroupingStrategy
	GroupingExtension     GroupingExtension
	MaxReferenceExpansion budget.Limit
	StreamingInference    StreamingInference
	BinaryInference       BinaryInference
}

// New derives the context for doc, at srcIndex, under p. doc must not be nil:
// the compiler lowers only a document its load returned. The streaming and
// binary policies are normalized here so readers cannot disagree about a
// media type.
//
// It is the one place the load phase's document and the schema lowering's
// values meet: what lowering.New reads of the load is built here.
func New(srcIndex int, doc *load.Document, p Policy) Ctx {
	documents, encoding := documentsOf(srcIndex, doc)
	streaming, binary := p.StreamingInference, p.BinaryInference
	return Ctx{
		Ctx: lowering.New(lowering.Loaded{Location: doc.Location, Places: doc.Places, Bindings: doc.Bindings,
			Declares:  resolve.NewNames(slices.Collect(doc.Doc.Components.GetSchemas().Keys())...),
			Documents: documents, Rules: doc.Dialects, Sources: sourcesOf(doc)}, p.Schema),
		Doc:               doc.Doc,
		SrcIndex:          srcIndex,
		Grouping:          p.Grouping,
		GroupingExtension: p.GroupingExtension,
		streaming:         mediaTypeSet(streaming.Disabled, streaming.MediaTypes, DefaultStreamingMediaTypes()),
		binary:            mediaTypeSet(binary.Disabled, binary.MediaTypes, DefaultBinaryMediaTypes()),
		loaded:            &loaded{siblings: doc.Siblings, media: doc.MediaTypes, others: doc.Others, encoding: encoding},
		expansion:         newExpansion(p.MaxReferenceExpansion),
	}
}

// documentsOf returns each document of doc's bindings' table the compile
// read, as a lowering places and names what it reads there, and the rows each
// reads its Encoding Objects with: the source at srcIndex, where the overlay
// is named as the writer of a position it introduced or rewrote, read with its
// own rows, and each other whose name the documents a reference read beside
// the source know (load.Others.Named), at the minor and in the dialect it is
// read in (load.Other.Rows). Two names one document is read by are two
// entries naming one Document.Sources entry.
func documentsOf(srcIndex int, doc *load.Document) (map[bind.DocID]lowering.Document, map[bind.DocID]oas.EncodingRules) {
	documents := map[bind.DocID]lowering.Document{
		bind.Source: {Place: provenance.Doc{Source: srcIndex, Overlay: doc.Overlay.Attribution()}},
	}
	encoding := map[bind.DocID]oas.EncodingRules{bind.Source: doc.Rows.Encoding}
	table := doc.Bindings.Table()
	if table == nil {
		return documents, encoding
	}
	for id := range bind.DocID(table.Documents()) {
		name := table.Document(id).Name
		o, ok := doc.Others.Named(name)
		if !ok || id == bind.Source {
			continue
		}
		documents[id] = lowering.Document{Name: name, Place: provenance.Doc{Source: o.Index}, IDs: ids.In(o.Label)}
		encoding[id] = o.Rows.Encoding
	}
	return documents, encoding
}

// sourcesOf returns doc's input files, in the order Provenance.Source indexes
// them: the source being lowered, then the overlay applied to it if there was
// one, then each document a reference read beside it (load.Others).
func sourcesOf(doc *load.Document) []ir.SourceInfo {
	out := []ir.SourceInfo{doc.Source}
	if doc.Overlay.Applied() {
		out = append(out, doc.Overlay.Source())
	}
	for _, o := range doc.Others.List() {
		out = append(out, o.Info)
	}
	return out
}

// InDocument returns c for lowering what document id of the bindings' table
// declares, at that document's own coordinates (lowering.InDocument), and
// false, c unchanged, for a document the compile did not read beside the
// source.
func InDocument(c Ctx, id bind.DocID) (Ctx, bool) {
	in, ok := lowering.InDocument(c.Ctx, id)
	if !ok {
		return c, false
	}
	c.Ctx = in
	return c, true
}

// ExpansionRefusal returns the error refusing the document when the copies its
// $refs and path items made weighed past [Policy.MaxReferenceExpansion], and
// false when they did not. It is meant to be asked once every lowering has
// returned: a copy past the budget lowers nothing, so a crossing after the
// question goes unreported.
func (c Ctx) ExpansionRefusal() (ir.Diagnostic, bool) {
	return c.expansion.refusal(c.SrcIndex)
}

// OtherIDs returns how the IDs of what each document a reference read beside
// the source declares name it, in the order Document.Sources lists them: the
// documents whose namespaces the compile declares beside the source's.
func OtherIDs(c Ctx) []ids.Document {
	others := c.loaded.others.List()
	out := make([]ids.Document, 0, len(others))
	for _, o := range others {
		out = append(out, ids.In(o.Label))
	}
	return out
}

// Siblings returns the fields the load phase modelled beside the document's
// path item $refs (see New). A load.Siblings only answers questions, so
// handing it out lets no copy of c change what another reads.
func (c Ctx) Siblings() load.Siblings {
	return c.loaded.siblings
}

// MediaTypeUse returns the model the load phase built of media, a content
// entry, and whether media is written as a $ref (load.MediaTypes.Use).
func (c Ctx) MediaTypeUse(media *soa.MediaType) (*resolve.ReferencedMediaType, bool) {
	return c.loaded.media.Use(media)
}

// EncodingRules returns the Encoding Object's rows an encoding c lowers is read
// with, those of the document whose content c reads, as c.SchemaRules returns
// the Schema Object's: what a part takes where its encoding leaves a field
// unwritten.
func EncodingRules(c Ctx) oas.EncodingRules { return c.loaded.encoding[c.Position("").Doc] }

// Declared returns what the entry ref, written at usePtr in c's document,
// resolves to, with the context its declaration is lowered in and the pointer
// declaring it there: the position the chain of its Reference Object ends on,
// in whichever document (InDocument), as the bindings' table places it, or,
// for an inline entry, c and usePtr themselves. Past
// [Policy.MaxReferenceExpansion] a $ref resolves to nothing
// ([Ctx.ExpansionRefusal]).
func Declared[T, S any, R chainable[T, S], P interface {
	*T
	GetRootNode() *yaml.Node
}](c Ctx, ref R, usePtr jsontext.Pointer) (*T, Ctx, jsontext.Pointer) {
	// Past the budget a $ref resolves to nothing without reading its chain,
	// which costs the chain's length at every use and is charged nothing.
	if _, isRef := c.Bindings().Reference(ref); isRef && ExpansionCrossed(c) {
		return nil, c, usePtr
	}
	use := c.Position(usePtr)
	obj, decl := resolve.ObjectAt[T, S](c.RefScope(), ref, use)
	if obj == nil || decl == use {
		return obj, c, usePtr
	}
	// The caller lowers a copy of what a $ref's chain names, so each is charged
	// what that weighs. One past the budget resolves to nothing, as an unresolved
	// $ref does, so no caller lowers another copy and none costs anything. An
	// inline entry is not charged here: a path item's own inline parameters are
	// charged where its additionalOperations entries take them
	// (ChargeExpansionAt).
	if !c.expansion.admit(readingOf(c, usePtr), P(obj).GetRootNode(), copied[T, S](c, ref)...) {
		return nil, c, usePtr
	}
	there, ok := InDocument(c, decl.Doc)
	if !ok {
		return nil, c, usePtr
	}
	return obj, there, decl.Pointer
}
