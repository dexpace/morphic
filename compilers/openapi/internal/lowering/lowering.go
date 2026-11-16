// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/naming"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// Ctx is everything the schema lowering may read: the document whose content
// it reads (Site), the caller's policies, and the indexes derived from the
// load once at entry. New builds one; the zero Ctx is invalid.
//
// It is a value: a function taking one takes a copy, and every lowering takes
// it as a parameter (#177). Copies share what the load read, which no lowering
// changes. The maps stay unexported because a copy shares a map, which an
// exported one would let a callee write through.
//
// Call sites bind it to c: ctx names the context.Context a Compile takes.
type Ctx struct {
	// Site is the document whose content the context reads (InDocument).
	Site

	// Limits is the caller's budget for the constructs the walk builds. It
	// arrives already resolved: the compiler's Options fills the unset budgets
	// in and translates its own spelling of "unbounded" before building a
	// context, so the zero value here simply bounds nothing.
	Limits Limits

	// promoter is the vendor-extension promotion policy, normalized, and read
	// through Promoter. It is unexported because it holds a map, which a struct
	// copy would share. It sits behind one pointer for the reason shared does.
	promoter *promotion.Promoter

	// schemas is the set of component-schema names the document declares, read
	// through DeclaresSchema and handed to each scope RefScope builds. Every copy
	// shares it, and a resolve.Names lets none write to it.
	schemas resolve.Names

	// NullableInference is the nullable-inference policy, and
	// DiscriminatorInference the discriminator-inference policy.
	NullableInference      NullableInference
	DiscriminatorInference DiscriminatorInference

	// shared is what the load phase resolved with the document. It sits behind
	// one pointer because every lowering takes a copy of the context, and a
	// closure over one that escapes moves it to the heap, with every word the
	// context holds.
	shared *shared

	// reached records where each reference a lowering resolved landed in the
	// source. Copies share it on purpose: only the document's lowering reads it,
	// once every other lowering has returned (Reached), so it cannot change their
	// answers.
	reached *resolve.Reached
}

// shared holds what every copy of a context reads of the load phase (see
// New): the readings RefScope hands every scope over the raw YAML the model
// reads on in, and over the schema bindings. They come with the document rather than being
// derived from it, since resolving them reads every document the source names,
// which only the load phase may (GitHub #757). The readings are bound once,
// here: binding them in RefScope would allocate each time.
type shared struct {
	// names names the nodes built at the positions of the documents the
	// bindings read, remembering each name (NameAt). Copies share it on
	// purpose: a name reads only those documents, which no lowering changes,
	// so what one copy remembers is what another would read.
	names *naming.Namer
	// documents is each document of the bindings' table the compile read, as
	// a lowering places and names what it reads there (InDocument), and
	// dialects the rows each schema is read with: the load phase read them off
	// the one table it built, so no context builds another. Copies share them
	// on purpose: nothing writes either after the load, so no lowering changes
	// what another reads.
	documents map[bind.DocID]Document
	dialects  dialect.Table
	// sources is the compile's input files, in the order Provenance.Source
	// indexes them (Sources).
	sources []ir.SourceInfo
	// reads holds those readings in the scope fields they fill: SelfPath, and
	// Bindings over every $ref.
	reads resolve.Scope
	// readings remembers each answer Remembered read, in a map per type of key
	// and answer (memo). Copies share it on purpose: an answer reads only the
	// documents and their bindings, which no lowering changes, so what one copy
	// remembers is what another would read.
	readings map[any]any
	// statements reads each schema the bindings hold into its statement, once
	// for the run (Statements). Copies share it on purpose: a statement reads
	// only the documents, their bindings and their rows, which no lowering
	// changes, so what one copy reads is what another would.
	statements *statement.Table
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
	Limits                 Limits
	Promotions             promotion.ExtensionPromotions
	NullableInference      NullableInference
	DiscriminatorInference DiscriminatorInference
	TitleNames             TitleNames
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

// Loaded is what New reads of the load phase, as values of the packages
// below: where the source lies and where the compile's files lie, as its
// references are read against; where each $ref lands; the component schemas
// the source declares; each document of the bindings' table the compile read,
// the source at bind.Source; the rows each schema is read with; and the
// compile's input files, in the order Provenance.Source indexes them.
type Loaded struct {
	Location  string
	Places    *resolve.Places
	Bindings  *typed.Bindings
	Declares  resolve.Names
	Documents map[bind.DocID]Document
	Rules     dialect.Table
	Sources   []ir.SourceInfo
}

// New derives the context for in under p, reading the source's own content:
// in.Documents holds the source at bind.Source.
//
// The schema-name index arrives built, not built on first use, so every reader
// sees the same set whatever the source order: a $ref resolved mid-lowering
// must see a component declared later. The promotion policy is copied so no
// lowering can write through the context into the caller's map.
func New(in Loaded, p Policy) Ctx {
	promoter := promotion.New(p.Promotions)
	return Ctx{
		Site:                   siteOf(bind.Source, in.Documents[bind.Source]),
		Limits:                 p.Limits,
		schemas:                in.Declares,
		promoter:               &promoter,
		NullableInference:      p.NullableInference,
		DiscriminatorInference: p.DiscriminatorInference,

		shared: &shared{documents: maps.Clone(in.Documents), dialects: in.Rules, sources: slices.Clone(in.Sources),
			names:    naming.New(in.Bindings.Table(), p.TitleNames.Honours()),
			reads:    resolve.Scope{SelfPath: in.Location, Places: in.Places, Bindings: in.Bindings},
			readings: map[any]any{}, settled: map[any]any{},
			statements: statement.NewTable(statement.Source{Bindings: in.Bindings, Rules: in.Rules})},
		reached: resolve.NewReached(),
	}
}

// siteOf returns the site of document id, which d is.
func siteOf(id bind.DocID, d Document) Site {
	return Site{place: d.Place, ids: d.IDs, doc: id}
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

// Sources is the compile's input files, in the order Provenance.Source
// indexes them: the source being lowered, then the overlay applied to it if
// there was one, then each document a reference read beside it.
//
// It is read here rather than paired by the compiler that assembles the
// Document because the source index each Site places a position in is the
// context's, and a second place that pairs a source index with a SourceInfo
// is a second place they can disagree — which would misattribute every node
// rather than fail.
func (c Ctx) Sources() []ir.SourceInfo {
	return slices.Clone(c.shared.sources)
}

// Bindings returns where the document's schema $refs land, as the load phase
// resolved them.
func (c Ctx) Bindings() *typed.Bindings {
	return c.shared.reads.Bindings
}

// Statements returns the run's statements: what each schema the documents
// write says, read once for every copy of c.
func (c Ctx) Statements() *statement.Table {
	return c.shared.statements
}

// Remembered returns read's answer for key, read once and remembered for every
// copy of c. read must turn only on the documents and their bindings, which no
// lowering changes, and key on all else it turns on; a package's own key type
// keeps its readings from another's. Each type of key and answer has a map of
// its own, so neither is boxed to be found or kept, and a key read for two
// types of answer is read once for each.
func Remembered[K comparable, V any](c Ctx, key K, read func() V) V {
	answers, ok := c.shared.readings[memo[K, V]{}].(map[K]V)
	if !ok {
		// Kept before read runs, so a reading that asks Remembered for answers of
		// the same types keeps them here rather than in a map this one replaces.
		answers = map[K]V{}
		c.shared.readings[memo[K, V]{}] = answers
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
	if s, ok := c.shared.settled[store[S]{}].(*S); ok {
		return s
	}
	s := build()
	c.shared.settled[store[S]{}] = s
	return s
}

// store is what Settled keeps the store of type S under. It holds nothing, so
// it is boxed without allocating.
type store[S any] struct{}

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
func (c Ctx) Rules() dialect.Table { return c.shared.dialects }

// RefScope is the context seen as a reference-resolution scope: the document's
// own path, what it declares, and the parsed document itself.
//
// It is derived on use rather than stored beside the context. A stored copy
// would be a second place the same facts live, free to disagree with the
// context after any change to it — and the whole point of the context is that
// there is one answer.
func (c Ctx) RefScope() resolve.Scope {
	r := c.shared.reads
	scope := resolve.Scope{SelfPath: r.SelfPath, Places: r.Places, Declares: c.schemas, Reached: c.reached,
		Bindings: r.Bindings}
	if c.doc != bind.Source {
		scope.Foreign, scope.Holder = true, c.shared.documents[c.doc].Name
	}
	return scope
}

// NameAt returns the hint the node built at pointer in c's document is named
// by (naming.Namer.At): a function of the documents and the coordinate alone,
// whichever lowering builds the node, read once for every copy of c.
func NameAt(c Ctx, pointer jsontext.Pointer) string {
	return c.shared.names.At(c.Position(pointer))
}

// NameTitle returns the name the title of the schema at pointer in c's
// document gives the node built there (naming.Namer.Title), and false where it
// gives none.
func NameTitle(c Ctx, pointer jsontext.Pointer) (string, bool) {
	return c.shared.names.Title(c.Position(pointer))
}

// NameThrough returns the name the $ref written at pointer in c's document
// suggests (naming.Namer.Through), and "" where it suggests none.
func NameThrough(c Ctx, pointer jsontext.Pointer) string {
	return c.shared.names.Through(c.Position(pointer))
}

// NameOfDocument returns the hint what is declared at the root of document doc
// of the bindings' table is named by (naming.Namer.Document).
func NameOfDocument(c Ctx, doc bind.DocID) string {
	return c.shared.names.Document(doc)
}

// InDocument returns c for lowering what document id of the bindings' table
// declares, at that document's own coordinates (Site): the source's, or
// another's, whose references are read in its scope, whose positions are its
// own, so each Provenance names it, and whose IDs it qualifies. It is the one
// way to another document, or back to the source. It is false, c unchanged,
// for a document the compile did not read beside the source.
func InDocument(c Ctx, id bind.DocID) (Ctx, bool) {
	d, ok := c.shared.documents[id]
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
