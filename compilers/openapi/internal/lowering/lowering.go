// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/anchors"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/naming"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// Ctx is everything a lowering may read and none may change: the parsed
// document, the caller's policies, the identity of the source, and the indexes
// derived from them once at entry.
//
// It is a value, so a function that takes one takes a copy, and every lowering
// takes it as a parameter (#177): there is no shared holder to write through.
// The maps stay unexported because a copy shares a map, so an exported one
// would let a callee write through it.
//
// Call sites bind it to c, never ctx, which the styleguide reserves for the
// context.Context a Compile takes.
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
	// of the caller policies the context carries, beside the budgets, the
	// streaming media list, the promotion mapping and NullableWidening; everything
	// else here is a fact about the document.
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

	// promotions is the vendor-extension promotion policy, normalized into the
	// map PromoteDeprecation reads, and nil when the caller disabled it.
	//
	// Unexported for the reason streaming is: it holds a map.
	promotions map[string]ExtensionTarget

	// schemas is the set of component-schema names the document declares.
	//
	// It is unexported and read through DeclaresSchema because a struct copy
	// shares a map rather than copying it: as an exported field it would be the
	// one part of a by-value context a callee could write to, and the write would
	// be visible to its caller's caller. An accessor makes that unrepresentable
	// rather than merely discouraged, which is what TestCtx_HasNoExportedMap
	// holds the struct to.
	schemas map[string]bool

	// NullableWidening is the nullable-widening policy. It sits before foreign so
	// the two share a word: RefScope copies the whole context into a closure,
	// whose size class a field more would move.
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

	// auth is the document's declared security schemes, keyed by the IDs a
	// requirement names. It is unexported, and read through a predicate rather
	// than handed back, for the reason schemas is.
	//
	// Unlike every other field it is not derived at entry: resolving the schemes
	// is a lowering that reports, and micro-compiler-design §4.1 keeps such work
	// out of New so it cannot report on documents that never ask. The compiler
	// resolves it once and extends the context with WithAuth, so the derivation
	// stays a lowering and only its result becomes context.
	auth map[ir.AuthID]ir.AuthScheme

	// overlay attributes a position to the overlay document that introduced it,
	// where one was applied. Its zero value answers for every compile without an
	// overlay, so nothing below has to ask whether there was one.
	//
	// It is unexported for the reason schemas is — it holds a map, which a struct
	// copy would share — and it is read only through ProvenanceAt, which keeps
	// "provenance is built in exactly one place" true of the source index as well
	// as of the pointer.
	overlay overlay.Origin

	// loaded is what the load phase resolved with the document, set only by
	// WithLoaded and nil in a context given none. It sits behind one pointer
	// because RefScope copies the whole context into a closure for every
	// reference a lowering reads, and with it every word the context holds.
	loaded *loadResult

	// typePositions remembers each answer TypePosition read. Copies share it on
	// purpose: an answer reads only the document and its bindings, which no
	// lowering changes, so what one copy remembers is what another would read.
	// Nil in a zero context.
	typePositions map[scopedPosition]bind.Position

	// anchors is the source's $dynamicAnchor index, built on first use
	// (DynamicAnchorSites). Copies share it on purpose, as typePositions: it
	// reads only the document. Nil in a zero context, which builds it on
	// every ask.
	anchors *anchorMemo

	// reached records where each reference a lowering resolved landed in the
	// source. Copies share it on purpose: only the document's lowering reads it,
	// once every other lowering has returned (Reached), so it cannot change their
	// answers. Nil in a zero context, which records nothing.
	reached *resolve.Reached

	// kept holds what the run keeps for the Document rather than a node. Copies
	// share it on purpose, as reached: only run reads what it keeps, once every
	// lowering has returned, so it cannot change their answers. Nil in a zero
	// context, which keeps nothing.
	kept *KeptOnce
}

// loadResult holds what a context reads of the load phase (see WithLoaded): the
// fields the path items write beside their $refs, and the readings RefScope
// hands every scope over them, over the raw YAML the model reads on in, and
// over the schema bindings. The readings are bound once, here: binding them
// in RefScope would allocate each time.
type loadResult struct {
	siblings load.Siblings
	// names names the nodes built at the positions of the documents the
	// bindings read, remembering each name (NameAt).
	names *naming.Namer
	// others is every document a reference read beside the source.
	others load.Others
	// reads holds those readings in the scope fields they fill: Holds over the
	// raw YAML, Beside and Writes over siblings, Bindings over the schema
	// $refs.
	reads resolve.Scope
}

// scopedPosition is a position, the document it is in, and the scope it is
// read in (see Within), which a TypePosition answer is kept under.
type scopedPosition struct {
	foreign bool
	holder  string
	home    int
	at      bind.Position
}

// New derives the immutable context for one loaded source.
//
// The schema-name index is built here, not on first use, so every reader sees
// the same set whatever the source order: a $ref resolved mid-lowering must see
// a component declared later. The streaming policy is normalized here so
// readers cannot disagree about a media type, and the promotion policy is
// copied so no lowering can write through the context into the caller's map.
//
// The $dynamicAnchor index is built on first use instead (GitHub #172): its
// bounds diagnostic would warn about documents that never write $dynamicRef.
func New(srcIndex int, doc *soa.OpenAPI, src ir.SourceInfo, grouping GroupingStrategy, limits Limits, streaming StreamingMedia, promotions ExtensionPromotions, nullable NullableWidening, origin overlay.Origin) Ctx {
	return Ctx{
		Doc:              doc,
		Source:           src,
		SrcIndex:         srcIndex,
		Grouping:         grouping,
		Limits:           limits,
		schemas:          declaredSchemaNames(doc),
		streaming:        streamingSet(streaming),
		promotions:       promotionSet(promotions),
		NullableWidening: nullable,
		overlay:          origin,

		typePositions: map[scopedPosition]bind.Position{},
		anchors:       &anchorMemo{},
		reached:       resolve.NewReached(),
		kept:          &KeptOnce{read: map[string]bool{}, entries: ir.Unmodeled{}},
	}
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
	if c.loaded != nil {
		for _, o := range c.loaded.others.List() {
			out = append(out, o.Info)
		}
	}
	return out
}

// WithAuth returns a copy of c carrying the resolved security schemes.
//
// Only the service walk is given the extended value; the phases that run before
// it keep the plain one. That is what keeps "populated partway through" from
// being a trap: the lowerings that run before the schemes are resolved never
// hold a context that could answer this, so there is no window in which it reads
// empty.
//
// A copy, not a fresh context: everything the service walk is about to lower —
// the document, its identity and index, and the declared-name index derived at
// entry — has to survive the extension.
func (c Ctx) WithAuth(auth map[ir.AuthID]ir.AuthScheme) Ctx {
	c.auth = auth
	return c
}

// WithLoaded returns a copy of c carrying what the load phase resolved with
// doc: where each schema $ref lands (its bindings), and the fields its path
// items write beside their $refs. They come with the document rather than
// being derived from it, since resolving either reads every document the
// source names, which only the load phase may (GitHub #757). One call sets
// both, so no context carries one without the other. The copy remembers no
// TypePosition answer or name of c's, which read c's bindings and documents.
func (c Ctx) WithLoaded(doc *load.Document) Ctx {
	c.loaded = &loadResult{siblings: doc.Siblings, others: doc.Others, names: naming.New(doc.Bindings.Table()),
		reads: resolve.Scope{Holds: doc.Holds, Beside: doc.Siblings.Beside, Writes: doc.Siblings.Writes,
			Bindings: doc.Bindings}}
	c.typePositions = map[scopedPosition]bind.Position{}
	return c
}

// Bindings returns where the document's schema $refs land, as the load phase
// resolved them, or nil in a context given none (see WithLoaded).
func (c Ctx) Bindings() *typed.Bindings {
	if c.loaded == nil {
		return nil
	}
	return c.loaded.reads.Bindings
}

// TypePosition returns read's answer for at in c's scope, read once and
// remembered for every copy of c. read is where a discriminator mapping target
// naming at names a type, which must follow from the documents and the
// mapping targets alone. Each subtype asks it of every entry of its base's
// mapping, so n subtypes read each n times. A zero context reads it every time.
func (c Ctx) TypePosition(at bind.Position, read func() bind.Position) bind.Position {
	if c.typePositions == nil {
		return read()
	}
	key := scopedPosition{foreign: c.foreign, holder: c.holder, home: c.home, at: at}
	if named, seen := c.typePositions[key]; seen {
		return named
	}
	named := read()
	c.typePositions[key] = named
	return named
}

// Siblings returns the fields the load phase modelled beside the document's
// path item $refs (see WithLoaded). A load.Siblings only answers questions, so
// handing it out lets no copy of c change what another reads.
func (c Ctx) Siblings() load.Siblings {
	if c.loaded == nil {
		return load.Siblings{}
	}
	return c.loaded.siblings
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
// #751). A chain the table places no end for keeps the use site.
func Declared[T, S any, R interface {
	*S
	resolve.Referenced[T, S]
}](c Ctx, ref R, usePtr jsontext.Pointer) (*T, Ctx, jsontext.Pointer) {
	obj, decl := resolve.ObjectAt[T, S](c.RefScope(), ref, usePtr)
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
	if !elsewhere || c.loaded == nil {
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

// declaredSchemaNames collects the names under components/schemas, or nil when
// the document declares none.
func declaredSchemaNames(doc *soa.OpenAPI) map[string]bool {
	if doc == nil || doc.Components == nil {
		return nil
	}
	schemas := doc.Components.GetSchemas()
	if schemas == nil {
		return nil
	}
	names := make(map[string]bool, schemas.Len())
	for name := range schemas.Keys() {
		names[name] = true
	}
	return names
}

// DeclaresSchema reports whether the document declares a component schema of
// this name. A name it does not declare is not a resolvable $ref target.
func (c Ctx) DeclaresSchema(name string) bool { return c.schemas[name] }

// DeclaresAuth reports whether the document declares the security scheme a
// requirement names. It is a predicate rather than a getter for the reason
// DeclaresSchema is: handing back the map would make it writable by every
// caller, which is the one thing keeping it unexported was for.
func (c Ctx) DeclaresAuth(id ir.AuthID) bool { _, ok := c.auth[id]; return ok }

// Is30 reports whether this document's dialect is OpenAPI 3.0 rather than the
// 2020-12 dialect of 3.1 and 3.2. A reading that differs between the two, such
// as a boolean exclusiveMinimum, a required `items` or the effect `nullable`
// has, asks this rather than parsing the version itself. An unrecognized
// version, and a context with no document, answer false. It reads the entry
// document's version, whichever document holds the schema, so a 3.1 file
// reached by $ref from a 3.0 root is read as 3.0 (GitHub #839).
func (c Ctx) Is30() bool {
	minor, _ := load.SupportedMinor(c.Doc.GetOpenAPI())
	return minor == "3.0"
}

// RefScope is the context seen as a reference-resolution scope: the document's
// own path, what it declares, and the parsed document itself.
//
// It is derived on use rather than stored beside the context. A stored copy
// would be a second place the same facts live, free to disagree with the
// context after any change to it — and the whole point of the context is that
// there is one answer.
func (c Ctx) RefScope() resolve.Scope {
	scope := resolve.Scope{SelfPath: c.Source.Path, Declares: c.DeclaresSchema, Foreign: c.foreign, Holder: c.holder,
		Ends: resolve.ReferenceEnd, Reached: c.reached}
	if l := c.loaded; l != nil {
		r := l.reads
		scope.Holds, scope.Beside, scope.Writes, scope.Bindings = r.Holds, r.Beside, r.Writes, r.Bindings
	}
	if c.Doc != nil { // keep Doc a nil interface, not one holding a nil pointer
		scope.Doc = c.Doc
	}
	return scope
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
	return diag.Newf(sev, code, c.ProvenanceAt(pointer), format, args...)
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
	if c.loaded == nil {
		return nil
	}
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
	if c.loaded == nil {
		return ids.Document{}
	}
	if o, ok := c.loaded.others.At(source); ok {
		return ids.In(o.Label)
	}
	return ids.Document{}
}

// NameAt returns the hint the node built at pointer in c's document is named
// by (naming.Namer.At): a function of the documents and the coordinate alone,
// whichever lowering builds the node, read once for every copy of c. A
// context given no bindings names a position by its pointer alone.
func NameAt(c Ctx, pointer jsontext.Pointer) string {
	at := bind.Position{Doc: c.homeDoc, Pointer: pointer}
	if c.loaded == nil {
		return naming.New(nil).At(at)
	}
	return c.loaded.names.At(at)
}

// NameThrough returns the name the $ref written at pointer in c's document
// suggests (naming.Namer.Through), and "" where it suggests none. A context
// given no bindings resolves no $ref, so it suggests none.
func NameThrough(c Ctx, pointer jsontext.Pointer) string {
	if c.loaded == nil {
		return ""
	}
	return c.loaded.names.Through(bind.Position{Doc: c.homeDoc, Pointer: pointer})
}

// homeOf returns the document other than the source whose positions c reads,
// and false for the source's own.
func homeOf(c Ctx) (load.Other, bool) {
	if c.home == 0 || c.loaded == nil {
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
	if c.loaded == nil {
		return c, false
	}
	name := c.Bindings().Table().Document(id).Name
	o, ok := c.loaded.others.Named(name)
	if !ok {
		return c, false
	}
	c.foreign, c.holder, c.home, c.homeDoc = true, name, o.Index, id
	return c, true
}

// ProvenanceAt is where a Provenance is built, and the only place this compiler
// spells the source index into one.
//
// It serves entities as well as diagnostics (GitHub #86). Being the one place
// also traces an overlay's contribution without touching any lowering: a
// position the overlay introduced or rewrote names the overlay as its source.
//
// from is for a record no single position addresses, such as a validation-only
// entry folding several keywords. The entry is located at the declaring schema,
// and from lists the keywords' positions, so the entry names the document that
// wrote all of them, when one did (see overlay.Origin.IndexOf).
func (c Ctx) ProvenanceAt(pointer jsontext.Pointer, from ...jsontext.Pointer) ir.Provenance {
	if o, ok := homeOf(c); ok {
		return ir.Provenance{Source: o.Index, Pointer: pointer}
	}
	return ir.Provenance{Source: c.overlay.IndexOf(pointer, from, c.SrcIndex), Pointer: pointer}
}

// anchorMemo holds the source's $dynamicAnchor index once it is built.
type anchorMemo struct {
	built bool
	index anchors.Index
}

// DynamicAnchorSites returns the pointers of the source declaring the
// $dynamicAnchor name (anchors.Build), and, the first time any copy of c asks,
// a warning when the index walk stopped at a bound. It is built on first use
// rather than at entry because that warning would report on documents that
// never write $dynamicRef (GitHub #172). A context holding no document finds
// none. Building the index reads no context.Context, which the schema lowering
// asking for it does not hold, so a cancel waits for the walk
// (docs/architecture.md §2.1).
func (c Ctx) DynamicAnchorSites(name string) ([]jsontext.Pointer, []ir.Diagnostic) {
	if c.Doc == nil {
		return nil, nil
	}
	memo := c.anchors
	if memo == nil {
		memo = &anchorMemo{}
	}
	if memo.built {
		return memo.index.Sites(name), nil
	}
	memo.index, memo.built = anchors.Build(c.Doc.GetRootNode()), true
	if memo.index.Complete() {
		return memo.index.Sites(name), nil
	}
	return memo.index.Sites(name), []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, "",
		"the $dynamicAnchor index stopped at its walk bounds (%d levels, %d nodes); "+
			"a $dynamicRef expanded below is not verified to name the document's only anchor of its name",
		anchors.MaxDepth, anchors.MaxNodes)}
}
