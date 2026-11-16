// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/naming"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// Ctx is everything a lowering may read: the parsed document, the caller's
// policies, the source's identity, and the indexes derived from them once at
// entry. New builds one; the zero Ctx is invalid.
//
// It is a value: a function taking one takes a copy, and every lowering takes
// it as a parameter (#177). Of what copies share, only expansion changes an
// answer: past its budget, no $ref resolves. The maps stay unexported because
// a copy shares a map, which an exported one would let a callee write through.
//
// Call sites bind it to c: ctx names the context.Context a Compile takes.
type Ctx struct {
	// Site is the document whose content the context reads (InDocument).
	Site

	// Doc is the parsed, reference-resolved source document. Lowering reads it
	// and never writes through it.
	Doc *soa.OpenAPI
	// Source is the identity of the loaded source, stamped into Document.Sources.
	Source ir.SourceInfo
	// SrcIndex is this source's index within the compile, stamped into every
	// Provenance.
	SrcIndex int
	// Grouping selects how operations are grouped into OperationGroups. It is one
	// of the caller policies the context carries (Policy).
	//
	// It arrives as the caller wrote it, normalized or not — the compiler's
	// Options fills an unset one in before building a context, but nothing here
	// enforces that. A strategy the operation lowering does not recognize groups
	// by tags, which is what makes the unnormalized zero value harmless rather
	// than a second spelling of the default to keep in step.
	Grouping GroupingStrategy
	// GroupingExtension is what GroupByExtension reads, arriving resolved as
	// Grouping does: the compiler's Options fills in the default extension.
	GroupingExtension GroupingExtension
	// Limits is the caller's budget for the constructs the walk builds. Like
	// Grouping it arrives already resolved: the compiler's Options fills the
	// unset budgets in and translates its own spelling of "unbounded" before
	// building a context, so the zero value here simply bounds nothing.
	Limits Limits

	// streaming is the media-type streaming policy, normalized into the set
	// MediaTypeStreams answers from, and nil when the caller disabled it.
	//
	// Unlike Grouping it is unexported, because it holds a map, which a struct
	// copy would share, as with the other maps here.
	streaming map[string]bool

	// binary is the binary media-type policy, normalized into the set
	// MediaTypeBinary answers from, and nil when the caller disabled it. It is
	// unexported for the reason streaming is.
	binary map[string]bool

	// promoter is the vendor-extension promotion policy, normalized, and read
	// through Promoter. It is unexported for the reason streaming is: it holds a
	// map. It sits behind one pointer for the reason loaded does.
	promoter *promotion.Promoter

	// schemas is the set of component-schema names the document declares, read
	// through DeclaresSchema and handed to each scope RefScope builds. Every copy
	// shares it, and a resolve.Names lets none write to it.
	schemas resolve.Names

	// NullableInference is the nullable-inference policy, and
	// DiscriminatorInference the discriminator-inference policy.
	NullableInference      NullableInference
	DiscriminatorInference DiscriminatorInference

	// overlay attributes a position to the overlay document that introduced it,
	// where one was applied. Its zero value answers for every compile without an
	// overlay, so nothing below has to ask whether there was one.
	//
	// It is unexported for the reason streaming is — it holds a map, which a
	// struct copy would share — and it is read only through the source's Site
	// and Sources, which keeps "provenance is built in exactly one place" true
	// of the source index as well as of the pointer.
	overlay overlay.Origin

	// loaded is what the load phase resolved with the document. It sits behind
	// one pointer because every lowering takes a copy of the context, and a
	// closure over one that escapes moves it to the heap, with every word the
	// context holds.
	loaded *loadResult

	// reached records where each reference a lowering resolved landed in the
	// source. Copies share it on purpose: only the document's lowering reads it,
	// once every other lowering has returned (Reached), so it cannot change their
	// answers.
	reached *resolve.Reached

	// expansion charges each $ref Declared resolves what the declarations its
	// chain names weigh, and each additionalOperations entry what it copies of
	// its path item (ChargeExpansion). Copies share it on purpose: the budget is
	// the run's, so once it is crossed no copy lowers a $ref or such an entry,
	// which is the answer it changes, and the run is refused
	// (ExpansionRefusal).
	expansion *expansion
}

// loadResult holds what a context reads of the load phase (see New): the
// fields the path items write beside their $refs, and the readings RefScope
// hands every scope over them, over the raw YAML the model reads on in, and
// over the schema bindings. They come with the document rather than being
// derived from it, since resolving them reads every document the source names,
// which only the load phase may (GitHub #757). The readings are bound once,
// here: binding them in RefScope would allocate each time.
type loadResult struct {
	siblings load.Siblings
	// media is each components/mediaTypes entry and each content entry written
	// as a $ref, modelled and resolved with the document (MediaTypeUse).
	media load.MediaTypes
	// names names the nodes built at the positions of the documents the
	// bindings read, remembering each name (NameAt). Copies share it on
	// purpose: a name reads only those documents, which no lowering changes,
	// so what one copy remembers is what another would read.
	names *naming.Namer
	// documents is each document of the bindings' table the compile read, as
	// a lowering places and names what it reads there (InDocument), others
	// every document a reference read beside the source, encoding the rows
	// each document's Encoding Objects are read with, and dialects those each
	// schema is read with: the load phase read them all off the one table it
	// built, so no context builds another. Copies share them on purpose:
	// nothing writes any after the load, so no lowering changes what another
	// reads.
	documents map[bind.DocID]Document
	others    load.Others
	encoding  map[bind.DocID]oas.EncodingRules
	dialects  dialect.Table
	// reads holds those readings in the scope fields they fill: SelfPath, and
	// Bindings over every $ref.
	reads resolve.Scope
	// readings remembers each answer Remembered read, in a map per type of key
	// and answer (memo). Copies share it on purpose: an answer reads only the
	// documents and their bindings, which no lowering changes, so what one copy
	// remembers is what another would read.
	readings map[any]any
	// settled holds each store Settled built, by its type (store). Copies share
	// it on purpose, so every settlement of a run reads one store. Unlike an
	// answer in readings, what a store holds reads the registry, so it would
	// change a later settlement's answer were a settlement between the two to
	// rewrite what it read (Settled).
	settled map[any]any
}

// Policy is the caller's choice of how a source is lowered, as the compiler's
// Options resolve it.
type Policy struct {
	Grouping               GroupingStrategy
	GroupingExtension      GroupingExtension
	Limits                 Limits
	StreamingInference     StreamingInference
	BinaryInference        BinaryInference
	Promotions             promotion.ExtensionPromotions
	NullableInference      NullableInference
	DiscriminatorInference DiscriminatorInference
	TitleNames             TitleNames
}

// New derives the context for doc, at srcIndex, under p. doc must not be nil:
// the compiler lowers only a document its load returned.
//
// The schema-name index is built here, not on first use, so every reader sees
// the same set whatever the source order: a $ref resolved mid-lowering must see
// a component declared later. The streaming and binary policies are normalized
// here so readers cannot disagree about a media type, and the promotion policy
// is copied so no lowering can write through the context into the caller's map.
func New(srcIndex int, doc *load.Document, p Policy) Ctx {
	promoter := promotion.New(p.Promotions)
	documents := documentsOf(srcIndex, doc)
	return Ctx{
		Site:                   siteOf(bind.Source, documents[bind.Source]),
		Doc:                    doc.Doc,
		Source:                 doc.Source,
		SrcIndex:               srcIndex,
		Grouping:               p.Grouping,
		GroupingExtension:      p.GroupingExtension,
		Limits:                 p.Limits,
		schemas:                declaredSchemaNames(doc.Doc),
		streaming:              streamingSet(p.StreamingInference),
		binary:                 binarySet(p.BinaryInference),
		promoter:               &promoter,
		NullableInference:      p.NullableInference,
		DiscriminatorInference: p.DiscriminatorInference,
		overlay:                doc.Overlay,

		loaded: &loadResult{siblings: doc.Siblings, media: doc.MediaTypes, documents: documents, others: doc.Others,
			encoding: encodingOf(doc), dialects: doc.Dialects, names: naming.New(doc.Bindings.Table(), p.TitleNames.Honours()),
			reads:    resolve.Scope{SelfPath: doc.Location, Places: doc.Places, Bindings: doc.Bindings},
			readings: map[any]any{}, settled: map[any]any{}},
		reached:   resolve.NewReached(),
		expansion: newExpansion(p.Limits.MaxReferenceExpansion),
	}
}

// Document is one document of the bindings' table the compile read, as a
// lowering places and names what it reads there (Site): the name the bindings
// read it by, which a reference in it is resolved against, where its positions
// are recorded, and how the IDs of what it declares name it.
type Document struct {
	Name  string
	Place provenance.Doc
	IDs   ids.Document
}

// documentsOf returns each document of doc's bindings' table the compile
// read: the source at srcIndex, where the overlay is named as the writer of a
// position it introduced or rewrote, and each other whose name the documents
// a reference read beside the source know (load.Others.Named).
func documentsOf(srcIndex int, doc *load.Document) map[bind.DocID]Document {
	out := map[bind.DocID]Document{bind.Source: {Place: provenance.Doc{Source: srcIndex, Overlay: doc.Overlay.Attribution()}}}
	table := doc.Bindings.Table()
	if table == nil {
		return out
	}
	for id := range bind.DocID(table.Documents()) {
		name := table.Document(id).Name
		if o, ok := doc.Others.Named(name); ok && id != bind.Source {
			out[id] = Document{Name: name, Place: provenance.Doc{Source: o.Index}, IDs: ids.In(o.Label)}
		}
	}
	return out
}

// encodingOf returns the rows each document of doc's bindings' table the
// compile read reads its Encoding Objects with: the source's own, and each
// other's at the minor and in the dialect it is read in (load.Other.Rows).
func encodingOf(doc *load.Document) map[bind.DocID]oas.EncodingRules {
	out := map[bind.DocID]oas.EncodingRules{bind.Source: doc.Rows.Encoding}
	table := doc.Bindings.Table()
	if table == nil {
		return out
	}
	for id := range bind.DocID(table.Documents()) {
		if o, ok := doc.Others.Named(table.Document(id).Name); ok && id != bind.Source {
			out[id] = o.Rows.Encoding
		}
	}
	return out
}

// siteOf returns the site of document id, which d is.
func siteOf(id bind.DocID, d Document) Site {
	return Site{place: d.Place, ids: d.IDs, doc: id}
}

// ExpansionRefusal returns the error refusing the document when the copies its
// $refs and path items made weighed past [Limits.MaxReferenceExpansion], and
// false when they did not. It is meant to be asked once every lowering has
// returned: a copy past the budget lowers nothing, so a crossing after the
// question goes unreported.
func (c Ctx) ExpansionRefusal() (ir.Diagnostic, bool) {
	return c.expansion.refusal(c.SrcIndex)
}

// Promoter returns the vendor-extension promotion policy this context carries.
func (c Ctx) Promoter() promotion.Promoter {
	return *c.promoter
}

// Reached reports whether a lowering run under this context, or any copy of it,
// resolved a reference to pointer in the source. It is meant to be asked once
// every lowering that follows a reference has returned: before that it answers
// for those that have.
func (c Ctx) Reached(pointer jsontext.Pointer) bool {
	return c.reached.Has(pointer)
}

// Sources is the document's input files, in the order Provenance.Source indexes
// them: the source being lowered, then the overlay applied to it if there was
// one, then each document a reference read beside it (load.Others).
//
// It is built here rather than by the compiler that assembles the Document
// because the two facts it joins are already the context's, and a second place
// that pairs a source index with a SourceInfo is a second place they can
// disagree — which would misattribute every node rather than fail.
func (c Ctx) Sources() []ir.SourceInfo {
	out := []ir.SourceInfo{c.Source}
	if c.overlay.Applied() {
		out = append(out, c.overlay.Source())
	}
	for _, o := range c.loaded.others.List() {
		out = append(out, o.Info)
	}
	return out
}

// Bindings returns where the document's schema $refs land, as the load phase
// resolved them.
func (c Ctx) Bindings() *typed.Bindings {
	return c.loaded.reads.Bindings
}

// Remembered returns read's answer for key, read once and remembered for every
// copy of c. read must turn only on the documents and their bindings, which no
// lowering changes, and key on all else it turns on; a package's own key type
// keeps its readings from another's. Each type of key and answer has a map of
// its own, so neither is boxed to be found or kept, and a key read for two
// types of answer is read once for each.
func Remembered[K comparable, V any](c Ctx, key K, read func() V) V {
	answers, ok := c.loaded.readings[memo[K, V]{}].(map[K]V)
	if !ok {
		// Kept before read runs, so a reading that asks Remembered for answers of
		// the same types keeps them here rather than in a map this one replaces.
		answers = map[K]V{}
		c.loaded.readings[memo[K, V]{}] = answers
	}
	if answer, seen := answers[key]; seen {
		return answer
	}
	answer := read()
	answers[key] = answer
	return answer
}

// memo is what the map of Remembered's answers of type V, to keys of type K, is
// kept under. It holds nothing, so it is boxed without allocating.
type memo[K comparable, V any] struct{}

// Settled returns the run's store of type S, which build makes on the first
// ask of any copy of c, for what settlements (compile.Settlement) read of the
// registry. Remembered may not keep such a reading, as it reads what the
// settlements before it left: it is sound only while no settlement after it
// rewrites what it read. The run takes every completion
// (compile.Types.Complete) before any deferred settlement, so a store only
// deferred settlements fill is sound where only completions rewrite what it
// reads. S's documentation names them, and each reports a store it finds
// filled.
func Settled[S any](c Ctx, build func() *S) *S {
	if s, ok := c.loaded.settled[store[S]{}].(*S); ok {
		return s
	}
	s := build()
	c.loaded.settled[store[S]{}] = s
	return s
}

// store is what Settled keeps the store of type S under. It holds nothing, so
// it is boxed without allocating.
type store[S any] struct{}

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

// Declared returns what the entry ref, written at usePtr in c's document,
// resolves to, with the context its declaration is lowered in and the pointer
// declaring it there: the position the chain of its Reference Object ends on,
// in whichever document (InDocument), as the bindings' table places it, or,
// for an inline entry, c and usePtr themselves. Past
// [Limits.MaxReferenceExpansion] a $ref resolves to nothing
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

// declaredSchemaNames collects the names under components/schemas, none when
// the document declares none.
func declaredSchemaNames(doc *soa.OpenAPI) resolve.Names {
	return resolve.NewNames(slices.Collect(doc.Components.GetSchemas().Keys())...)
}

// DeclaresSchema reports whether the document declares a component schema of
// this name. A name it does not declare is not a resolvable $ref target.
func (c Ctx) DeclaresSchema(name string) bool { return c.schemas.Has(name) }

// SchemaRules returns the Schema Object's rows of the document whose content c
// reads, where no root names a dialect (dialect.Table.In). A reading only a
// minor decides, such as a required `items` or the effect `nullable` has, asks
// these rather than the version; a keyword's shape, which a schema's dialect
// decides, asks Rules.
func (c Ctx) SchemaRules() oas.SchemaRules { return c.Rules().In(c.doc) }

// Rules returns the rows each schema of the compile is read with, which the
// load phase read off the one table it judged the documents against.
func (c Ctx) Rules() dialect.Table { return c.loaded.dialects }

// EncodingRules returns the Encoding Object's rows an encoding c lowers is read
// with, those of the document whose content c reads, as SchemaRules returns
// the Schema Object's: what a part takes where its encoding leaves a field
// unwritten.
func EncodingRules(c Ctx) oas.EncodingRules { return c.loaded.encoding[c.doc] }

// RefScope is the context seen as a reference-resolution scope: the document's
// own path, what it declares, and the parsed document itself.
//
// It is derived on use rather than stored beside the context. A stored copy
// would be a second place the same facts live, free to disagree with the
// context after any change to it — and the whole point of the context is that
// there is one answer.
func (c Ctx) RefScope() resolve.Scope {
	r := c.loaded.reads
	scope := resolve.Scope{SelfPath: r.SelfPath, Places: r.Places, Declares: c.schemas, Reached: c.reached,
		Bindings: r.Bindings}
	if c.doc != bind.Source {
		scope.Foreign, scope.Holder = true, c.loaded.documents[c.doc].Name
	}
	return scope
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

// NameAt returns the hint the node built at pointer in c's document is named
// by (naming.Namer.At): a function of the documents and the coordinate alone,
// whichever lowering builds the node, read once for every copy of c.
func NameAt(c Ctx, pointer jsontext.Pointer) string {
	return c.loaded.names.At(c.Position(pointer))
}

// NameTitle returns the name the title of the schema at pointer in c's
// document gives the node built there (naming.Namer.Title), and false where it
// gives none.
func NameTitle(c Ctx, pointer jsontext.Pointer) (string, bool) {
	return c.loaded.names.Title(c.Position(pointer))
}

// NameThrough returns the name the $ref written at pointer in c's document
// suggests (naming.Namer.Through), and "" where it suggests none.
func NameThrough(c Ctx, pointer jsontext.Pointer) string {
	return c.loaded.names.Through(c.Position(pointer))
}

// NameOfDocument returns the hint what is declared at the root of document doc
// of the bindings' table is named by (naming.Namer.Document).
func NameOfDocument(c Ctx, doc bind.DocID) string {
	return c.loaded.names.Document(doc)
}

// InDocument returns c for lowering what document id of the bindings' table
// declares, at that document's own coordinates (Site): the source's, or
// another's, whose references are read in its scope, whose positions are its
// own, so each Provenance names it, and whose IDs it qualifies. It is the one
// way to another document, or back to the source. It is false, c unchanged,
// for a document the compile did not read beside the source.
func InDocument(c Ctx, id bind.DocID) (Ctx, bool) {
	d, ok := c.loaded.documents[id]
	if !ok {
		return c, false
	}
	c.Site = siteOf(id, d)
	return c, true
}

// DynamicAnchor returns what c's document's declarations of the
// $dynamicAnchor name say (bind.Table.DynamicAnchor).
func (c Ctx) DynamicAnchor(name string) bind.DynamicAnchor {
	return c.Bindings().Table().DynamicAnchor(c.doc, name)
}
