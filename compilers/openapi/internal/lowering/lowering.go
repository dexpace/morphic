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
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/naming"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
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
	promoter *Promoter

	// schemas is the set of component-schema names the document declares, read
	// through DeclaresSchema and handed to each scope RefScope builds. Every copy
	// shares it, and a resolve.Names lets none write to it.
	schemas resolve.Names

	// NullableWidening is the nullable-widening policy. It sits before foreign so
	// the two share a word.
	NullableWidening NullableWidening

	// foreign marks a lowering of content another document holds, read in that
	// document's scope, and holder is the path the resolver read it by (see
	// Within). Unexported and set only by Within, InScope and InDocument, which
	// scope them to the subtree that copy is threaded through.
	foreign bool
	holder  string

	// home is the Document.Sources index of the document other than the
	// source whose positions this lowering reads, or zero for the source's
	// own, and homeDoc that document in the bindings' table. Set only by
	// InDocument, beside the scope it reads that document's references in, and
	// cleared by InScope, which reads at the source's.
	home    int
	homeDoc bind.DocID

	// overlay attributes a position to the overlay document that introduced it,
	// where one was applied. Its zero value answers for every compile without an
	// overlay, so nothing below has to ask whether there was one.
	//
	// It is unexported for the reason streaming is — it holds a map, which a
	// struct copy would share — and it is read only through Place and Sources,
	// which keeps "provenance is built in exactly one place" true of the source
	// index as well as of the pointer.
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
	// others is every document a reference read beside the source, rows what
	// the table says at the source's minor in its dialect (load.Document.Rows),
	// and dialects the rows each schema resource root naming its dialect
	// governs: the load phase read them all off the one table it built, so no
	// context builds another. Copies share them on purpose: nothing writes any
	// after the load, so no lowering changes what another reads.
	others   load.Others
	rows     load.Rows
	dialects load.Dialects
	// reads holds those readings in the scope fields they fill: Holds over the
	// raw YAML, Beside and Writes over siblings, Bindings over the schema
	// $refs.
	reads resolve.Scope
	// readings remembers each answer Remembered read, in a map per type of key
	// and answer (memo). Copies share it on purpose: an answer reads only the
	// documents and their bindings, which no lowering changes, so what one copy
	// remembers is what another would read.
	readings map[any]any
}

// ScopedPosition is a position as a context reads it: with the scope it is read
// in (see Within) and the document the context places positions in. It keys a
// reading that turns on where a position is read (Remembered), and only Scoped
// builds one, as the scope is the context's own and unexported.
type ScopedPosition struct {
	foreign bool
	holder  string
	home    int
	at      bind.Position
}

// Policy is the caller's choice of how a source is lowered, as the compiler's
// Options resolve it.
type Policy struct {
	Grouping         GroupingStrategy
	Limits           Limits
	Streaming        StreamingMedia
	Binary           BinaryMedia
	Promotions       ExtensionPromotions
	NullableWidening NullableWidening
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
	return Ctx{
		Doc:              doc.Doc,
		Source:           doc.Source,
		SrcIndex:         srcIndex,
		Grouping:         p.Grouping,
		Limits:           p.Limits,
		schemas:          declaredSchemaNames(doc.Doc),
		streaming:        streamingSet(p.Streaming),
		binary:           binarySet(p.Binary),
		promoter:         newPromoter(p.Promotions),
		NullableWidening: p.NullableWidening,
		overlay:          doc.Overlay,

		loaded: &loadResult{siblings: doc.Siblings, media: doc.MediaTypes, others: doc.Others, rows: doc.Rows,
			dialects: doc.Dialects, names: naming.New(doc.Bindings.Table()),
			reads: resolve.Scope{Holds: doc.Holds, Beside: doc.Siblings.Beside, Writes: doc.Siblings.Writes,
				Bindings: doc.Bindings}, readings: map[any]any{}},
		reached:   resolve.NewReached(),
		expansion: newExpansion(p.Limits.MaxReferenceExpansion),
	}
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
func (c Ctx) Promoter() Promoter {
	return *c.promoter
}

// Reached reports whether a lowering run under this context, or any copy of it,
// resolved a reference to pointer in the source. It is meant to be asked once
// every other lowering has returned: before that it answers for those that
// have.
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

// Scoped returns at as c reads it: in c's scope, and in the coordinates of the
// document c places its positions in.
func Scoped(c Ctx, at bind.Position) ScopedPosition {
	return ScopedPosition{foreign: c.foreign, holder: c.holder, home: c.home, at: at}
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

// Within returns c for lowering what the entry ref resolves to, in the scope
// resolve.ScopeOf reads it in: Foreign when another document holds it,
// InSource when its chain ends in the source, and c itself for an inline entry.
//
// A URI reference in another document's content is read against that
// document. Read as the source's, `#/...` resolved to whatever the source
// declared at the same pointer, when that had lowered first (GitHub #762). A
// name, such as a component's in a mapping, still names the entry document's,
// as the specification recommends for such implicit connections.
func Within[T, S any, R interface {
	*S
	resolve.Referenced[T, S]
}](c Ctx, ref R) Ctx {
	scope := resolve.ScopeOf[T, S](c.RefScope(), ref)
	c.foreign, c.holder = scope.Foreign, scope.Holder
	return c
}

// Declared returns what the entry ref, written at usePtr in c's document,
// resolves to, with the context its declaration is lowered in and the pointer
// declaring it there. For the source's own reference whose chain ends in the
// source, that is Within's context and the pointer resolve.ObjectAt reads.
// Otherwise it is the context of the document the chain ends in (InDocument)
// and the position the bindings' table places the end at (bind.Table.Through),
// the source's included for a reference another document writes (GitHub
// #751), or the use site where it places none. Past
// [Limits.MaxReferenceExpansion] a $ref resolves to nothing
// ([Ctx.ExpansionRefusal]).
func Declared[T, S any, R chainable[T, S], P interface {
	*T
	GetRootNode() *yaml.Node
}](c Ctx, ref R, usePtr jsontext.Pointer) (*T, Ctx, jsontext.Pointer) {
	obj, decl := resolve.ObjectAt[T, S](c.RefScope(), ref, usePtr)
	// The caller lowers a copy of what a $ref's chain names, so each is charged
	// what that weighs. One past the budget resolves to nothing, as an unresolved
	// $ref does, so no caller lowers another copy and none costs anything. An
	// inline entry is not charged here: a path item's own inline parameters are
	// charged where its additionalOperations entries take them
	// (ChargeExpansionAt).
	if obj != nil && ref.GetReference() != "" &&
		!c.expansion.admit(readingOf(c, usePtr), copied[T, S](c, ref, P(obj).GetRootNode())...) {
		return nil, c, usePtr
	}
	in := Within[T, S](c, ref)
	if there, at, ok := declaredElsewhere(c, obj != nil && (in.foreign || c.home != 0), usePtr); ok {
		return obj, there, at
	}
	return obj, in, decl
}

// declaredElsewhere returns the context and pointer of what the reference
// object at usePtr in c's document resolved to, where the table places its
// end, and false where Declared keeps Within's answer: elsewhere is false, or
// the end is the source's own reference's in the source, or the table places
// none.
func declaredElsewhere(c Ctx, elsewhere bool, usePtr jsontext.Pointer) (Ctx, jsontext.Pointer, bool) {
	if !elsewhere {
		return c, "", false
	}
	end, ok := c.Bindings().Table().Through(bind.Position{Doc: c.homeDoc, Pointer: usePtr})
	if !ok || end.Doc == bind.Source && c.home == 0 {
		return c, "", false
	}
	there, ok := InDocument(c, end.Doc)
	return there, end.Pointer, ok
}

// InScope returns c for lowering at the source's coordinates what is read in
// scope: the source's own content (resolve.Scope.InSource), or, as Within
// gives it, another document's that no position of that document places.
// What another document declares at a position of its own is lowered in its
// coordinates instead (InDocument).
func (c Ctx) InScope(scope resolve.Scope) Ctx {
	c.foreign, c.holder, c.home, c.homeDoc = scope.Foreign, scope.Holder, 0, bind.Source
	return c
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
// reads (rowsOf). A reading only a minor decides, such as a required `items` or
// the effect `nullable` has, asks these rather than the version; a keyword's
// shape, which a schema's dialect decides, asks SchemaRulesAt.
func (c Ctx) SchemaRules() oas.SchemaRules { return rowsOf(c).Schema }

// EncodingRules returns the Encoding Object's rows an encoding c lowers is read
// with (rowsOf), as SchemaRules returns the Schema Object's: what a part takes
// where its encoding leaves a field unwritten.
func EncodingRules(c Ctx) oas.EncodingRules { return rowsOf(c).Encoding }

// rowsOf returns what the table says at the minor of the document whose
// content c reads, in the dialect its jsonSchemaDialect names, which its scope
// names (load.Other.Rows), the source's for its own (load.Document.Rows).
//
// The scope, not home, names that document: Declared can read one document's
// content at another's positions.
func rowsOf(c Ctx) load.Rows {
	if c.foreign {
		if o, ok := c.loaded.others.Named(c.holder); ok {
			return o.Rows
		}
	}
	return c.loaded.rows
}

// SchemaRulesAt returns the rows the schema at pointer of c's document is read
// with: those of the dialect the innermost schema resource root at or above it
// names, where one does (load.Dialects.At), and c's otherwise (SchemaRules),
// as where c reads another document's content at the source's positions.
func SchemaRulesAt(c Ctx, pointer jsontext.Pointer) oas.SchemaRules {
	if at, placed := c.Position(pointer); placed {
		if r, ok := c.loaded.dialects.At(at); ok {
			return r
		}
	}
	return c.SchemaRules()
}

// SchemaRulesOf is SchemaRulesAt for a reading that has the schema's node n
// and not where it is: the rows load.Dialects.Of gives it, and the source's
// own for a node it gives none, whatever document c reads, so a reading that
// follows a $ref into another document still reads its schemas in their own
// dialect. An aliased node reads one dialect wherever it is placed.
func SchemaRulesOf(c Ctx, n *yaml.Node) oas.SchemaRules {
	if r, ok := c.loaded.dialects.Of(n); ok {
		return r
	}
	return c.loaded.rows.Schema
}

// SchemaRulesIn returns the rows the schemas document id of the bindings'
// table declares are read with, for a reading that crosses a $ref without a
// context of that document: the source's own for the source, and for one the
// compile did not read beside it.
func SchemaRulesIn(c Ctx, id bind.DocID) oas.SchemaRules {
	if _, o, ok := otherIn(c, id); ok {
		return o.Rows.Schema
	}
	return c.loaded.rows.Schema
}

// RefScope is the context seen as a reference-resolution scope: the document's
// own path, what it declares, and the parsed document itself.
//
// It is derived on use rather than stored beside the context. A stored copy
// would be a second place the same facts live, free to disagree with the
// context after any change to it — and the whole point of the context is that
// there is one answer.
func (c Ctx) RefScope() resolve.Scope {
	r := c.loaded.reads
	return resolve.Scope{Doc: c.Doc, SelfPath: c.Source.Path, Declares: c.schemas, Foreign: c.foreign,
		Holder: c.holder, Ends: resolve.ReferenceEnd, Reached: c.reached, Holds: r.Holds, Beside: r.Beside,
		Writes: r.Writes, Bindings: r.Bindings}
}

// DiagAt builds one diagnostic at pointer, stamped with this compile's source
// index.
//
// It returns rather than records. Those are two different jobs, and separating
// them is what lets both rules hold at once: GitHub #86 wants provenance built
// in exactly one place, because hand-writing it is how a diagnostic shipped with
// none (GitHub #43); micro-compiler-design §4 wants diagnostics returned rather
// than accumulated through a handle, because accumulation is the side effect the
// conversion exists to remove. A constructor that stamps and hands back satisfies
// both, and a lowering that has no accumulator yet can still be sure of its
// provenance.
func (c Ctx) DiagAt(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) ir.Diagnostic {
	return diag.Newf(sev, code, c.Place().At(pointer), format, args...)
}

// IDs returns the document c's positions are in, as the IDs of what it
// declares name it (see InDocument).
func (c Ctx) IDs() ids.Document {
	if o, ok := homeOf(c); ok {
		return ids.In(o.Label)
	}
	return ids.Document{}
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

// IDsAt returns how the IDs of what the Document.Sources entry at source
// declares name that document: ids.In its label for one a reference read
// beside the source, and the source's own for the source and its overlay.
func IDsAt(c Ctx, source int) ids.Document {
	if o, ok := c.loaded.others.At(source); ok {
		return ids.In(o.Label)
	}
	return ids.Document{}
}

// NameAt returns the hint the node built at pointer in c's document is named
// by (naming.Namer.At): a function of the documents and the coordinate alone,
// whichever lowering builds the node, read once for every copy of c.
func NameAt(c Ctx, pointer jsontext.Pointer) string {
	return c.loaded.names.At(bind.Position{Doc: c.homeDoc, Pointer: pointer})
}

// NameThrough returns the name the $ref written at pointer in c's document
// suggests (naming.Namer.Through), and "" where it suggests none.
func NameThrough(c Ctx, pointer jsontext.Pointer) string {
	return c.loaded.names.Through(bind.Position{Doc: c.homeDoc, Pointer: pointer})
}

// homeOf returns the document other than the source whose positions c reads,
// and false for the source's own.
func homeOf(c Ctx) (load.Other, bool) {
	if c.home == 0 {
		return load.Other{}, false
	}
	return c.loaded.others.At(c.home)
}

// InDocument returns c for lowering what document id of the bindings' table
// declares, at that document's own coordinates: the source's (InScope), or
// another's, whose references are read in its scope, whose positions are its
// own, so each Provenance names it, and whose IDs it qualifies. It is false, c
// unchanged, for a document the compile did not read beside the source.
func InDocument(c Ctx, id bind.DocID) (Ctx, bool) {
	if id == bind.Source {
		return c.InScope(c.RefScope().InSource()), true
	}
	name, o, ok := otherIn(c, id)
	if !ok {
		return c, false
	}
	c.foreign, c.holder, c.home, c.homeDoc = true, name, o.Index, id
	return c, true
}

// otherIn returns the name document id of the bindings' table is read by, and
// the document the compile read beside the source under it, and false for the
// source and for a document the compile did not read beside the source.
func otherIn(c Ctx, id bind.DocID) (string, load.Other, bool) {
	if id == bind.Source {
		return "", load.Other{}, false
	}
	name := c.Bindings().Table().Document(id).Name
	o, ok := c.loaded.others.Named(name)
	return name, o, ok
}

// Place returns where c places a pointer of its document: in another document's
// own Document.Sources entry, for one InDocument reads, and otherwise in the
// source's, or the overlay's for a position the overlay introduced or rewrote.
//
// Every lowering places a pointer through it, entities and diagnostics alike
// (GitHub #86), so no lowering spells a source index, and an overlay's
// contribution is traced without touching any of them.
func (c Ctx) Place() provenance.Doc {
	if o, ok := homeOf(c); ok {
		return provenance.Doc{Source: o.Index}
	}
	return provenance.Doc{Source: c.SrcIndex, Overlay: c.overlay.Attribution()}
}

// Position returns where pointer is in the bindings' table, and false where c
// reads what another document holds at the source's coordinates (InScope), which
// no position of the table places.
func (c Ctx) Position(pointer jsontext.Pointer) (bind.Position, bool) {
	return bind.Position{Doc: c.homeDoc, Pointer: pointer}, !c.foreign || c.home != 0
}

// DynamicAnchorSites returns the positions of c's document declaring the
// $dynamicAnchor name, and whether that is every one (bind.Table.DynamicAnchors).
func (c Ctx) DynamicAnchorSites(name string) ([]bind.Position, bool) {
	return c.Bindings().Table().DynamicAnchors(c.homeDoc, name)
}
