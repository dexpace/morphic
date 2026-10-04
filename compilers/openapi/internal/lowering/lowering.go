// Package lowering holds the immutable context every OpenAPI lowering reads.
//
// It is the substrate the lowering packages share, not a stage: it lowers and
// reports nothing itself. It owns the answer to "what is being lowered, and
// what does the document say about itself": the parsed document, the source's
// identity, the indexes derived once at entry, and the constructors that stamp
// provenance.
//
// It is a package because the schema walk and the operation walk both need
// those answers and neither may reach the other (micro-compiler-design §5.1).
package lowering

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
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
	// of the caller policies the context carries — the budgets, the streaming
	// media list and the promotion mapping are the others; everything else here
	// is a fact about the document.
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

	// foreign marks a lowering of content another document holds, read in that
	// document's scope, and holder is the path the resolver read it by (see
	// Within). Unexported and set only by Within and InSource, which scope them
	// to the subtree that copy is threaded through.
	foreign bool
	holder  string

	// namesByReference marks a lowering running under a $ref that named a
	// coordinate, whose names are placeholders. Unexported and read through
	// NamesByReference so it can only be set by NamingByReference, which is what
	// keeps it scoped to the subtree that copy is threaded through.
	namesByReference bool

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

	// defsReader reads the document's "#/$defs/..." pointers and remembers what
	// it has read. Copies of the context share it on purpose: it only caches
	// reads of a document lowering never writes, so what one copy remembers
	// cannot change another's answer, and it keeps a reference's cost from
	// growing with how deep its schema sits. Nil when there is no document.
	defsReader *defs.Reader

	// targets holds the schema each discriminator mapping target names, which
	// the load phase resolved as a $ref to it is. Its zero value holds none.
	// Unexported for the reason schemas is, and read only through RefScope.
	targets load.MappingTargets
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
func New(srcIndex int, doc *soa.OpenAPI, src ir.SourceInfo, grouping GroupingStrategy, limits Limits, streaming StreamingMedia, promotions ExtensionPromotions, origin overlay.Origin) Ctx {
	var reader *defs.Reader
	if doc != nil {
		reader = defs.NewReader(doc)
	}
	return Ctx{
		Doc:        doc,
		Source:     src,
		SrcIndex:   srcIndex,
		Grouping:   grouping,
		Limits:     limits,
		schemas:    declaredSchemaNames(doc),
		streaming:  streamingSet(streaming),
		promotions: promotionSet(promotions),
		overlay:    origin,
		defsReader: reader,
	}
}

// Sources is the document's input files, in the order Provenance.Source indexes
// them: the source being lowered, then the overlay applied to it if there was
// one.
//
// It is built here rather than by the compiler that assembles the Document
// because the two facts it joins are already the context's, and a second place
// that pairs a source index with a SourceInfo is a second place they can
// disagree — which would misattribute every node rather than fail.
func (c Ctx) Sources() []ir.SourceInfo {
	if !c.overlay.Applied() {
		return []ir.SourceInfo{c.Source}
	}
	return []ir.SourceInfo{c.Source, c.overlay.Source()}
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

// WithMappingTargets returns a copy of c carrying the schemas the load phase
// resolved for the document's discriminator mapping targets. They come with
// the document rather than being derived from it, since resolving one runs the
// resolver, which only the load phase may (GitHub #757).
func (c Ctx) WithMappingTargets(targets load.MappingTargets) Ctx {
	c.targets = targets
	return c
}

// NamingByReference returns a copy of c marking everything lowered under it as
// reached through a reference naming a coordinate, not through the declaration
// that owns it.
//
// A $ref can spell a pointer inside another declaration's body, so the node
// interned there would be named by whichever lowering arrives first. A name
// belongs to a declaration, so a lowering under this context names
// provisionally and the declaration replaces the name when it arrives (GitHub
// #372).
//
// It marks the whole subtree: a referenced object body interns its children
// too, named from the enclosing one, so they are placeholders as well.
func (c Ctx) NamingByReference() Ctx {
	c.namesByReference = true
	return c
}

// NamesByReference reports whether names minted under c are placeholders a
// declaration replaces. See NamingByReference.
func (c Ctx) NamesByReference() bool { return c.namesByReference }

// NamingByReferenceAt is NamingByReference for a reference-or-declaration
// position: it marks c only when declPtr differs from usePtr.
//
// They differ when a $ref reached a declaration another position owns, such as
// a request body written under another operation: a use-site hint (an
// operationId, a headers-map key) would name a node the use site does not own.
//
// $ref is resolved at these positions before lowering starts, so nothing
// downstream can tell a reference reached the declaration, unlike a
// schema-level $ref, which hoistSubSchema marks. Without the mark, whichever
// lowering ran first would name the shared node, since Intern is
// first-write-wins (GitHub #433).
func (c Ctx) NamingByReferenceAt(usePtr, declPtr jsontext.Pointer) Ctx {
	if declPtr == usePtr {
		return c
	}
	return c.NamingByReference()
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

// InSource returns c for lowering what the source holds at a position a
// reference names: a copy no longer Foreign (resolve.Scope.InSource).
func (c Ctx) InSource() Ctx {
	c.foreign, c.holder = false, ""
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
	for name := range schemas.All() {
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

// ExclusiveBoundIsBoolean reports whether this document's dialect spells
// exclusiveMinimum/exclusiveMaximum as a boolean modifier (OpenAPI 3.0) rather
// than a numeric bound (the 2020-12 dialect of 3.1 and 3.2). An unrecognized
// version defaults to the 2020-12 numeric form.
//
// It reads the document version, which makes it a question about the context
// rather than about any schema — annotation.Constraints takes the answer as a
// parameter precisely so the reader never has to ask it.
//
// The version is read through its accessor so a context with no document answers
// like an unrecognized version rather than panicking, which is how the other two
// readers behave on a zero value.
func (c Ctx) ExclusiveBoundIsBoolean() bool {
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
	scope := resolve.Scope{SelfPath: c.Source.Path, Declares: c.DeclaresSchema, Mapped: c.targets.At,
		Foreign: c.foreign, Holder: c.holder}
	if c.Doc != nil { // keep Doc a nil interface, not one holding a nil pointer
		scope.Doc, scope.Defs = c.Doc, c.defsReader
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

// ProvenanceAt is where a Provenance is built, and the only place this compiler
// spells the source index into one.
//
// It serves entities as well as diagnostics (GitHub #86). Being the one place
// also traces an overlay's contribution without touching any lowering: a
// position the overlay introduced or rewrote names the overlay as its source.
//
// from is for a record no single position addresses, such as a §4.7 entry
// folding several keywords. The entry is located at the declaring schema, and
// from lists the keywords' positions, so the entry names the document that
// wrote all of them, when one did (see overlay.Origin.IndexOf).
func (c Ctx) ProvenanceAt(pointer jsontext.Pointer, from ...jsontext.Pointer) ir.Provenance {
	return ir.Provenance{Source: c.overlay.IndexOf(pointer, from, c.SrcIndex), Pointer: pointer}
}
