// Package lowering holds the immutable context every OpenAPI lowering reads.
//
// It is the substrate the lowering packages share rather than a stage of its
// own: it lowers nothing and reports nothing on its own behalf. What it owns is
// the answer to "what is being lowered, and what does the document say about
// itself" — the parsed document, the identity of the source, the indexes derived
// from it once at entry, and the two constructors that stamp provenance.
//
// It is a package because the schema walk and the operation walk both need those
// answers and neither may reach the other (micro-compiler-design §5.1). Keeping
// the context with either one would make the other import it.
package lowering

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// Ctx is everything a lowering may read and none may change: the parsed
// document, the grouping policy the caller chose, the identity of the source
// being lowered, and the indexes derived from them once at entry.
//
// It is a value rather than a pointer, so a function that takes one takes a
// copy. Every lowering takes it as a parameter (#177), which is what makes
// "immutable" enforceable rather than conventional: there is no shared holder
// left to write through. The maps below stay unexported for the other half of
// it — a copy shares a map rather than copying it, so an exported one would be
// the single part of a by-value context a callee could still reach.
//
// Call sites bind it to c, never to ctx: the styleguide reserves that identifier
// for the context.Context a Compile takes, and operations.go spends it again on
// an opContext local, so a third meaning would be shadowed at both sites.
type Ctx struct {
	// Doc is the parsed, reference-resolved source document. Lowering reads it
	// and never writes through it.
	Doc *soa.OpenAPI
	// Source is the identity of the loaded source, stamped into Document.Sources.
	Source ir.SourceInfo
	// SrcIndex is this source's index within the compile, stamped into every
	// Provenance.
	SrcIndex int
	// Grouping selects how operations are grouped into OperationGroups. It is the
	// only policy the context carries: everything else here is a fact about the
	// document, and this is a fact about the caller.
	//
	// It arrives as the caller wrote it, normalized or not — the compiler's
	// Options fills an unset one in before building a context, but nothing here
	// enforces that. A strategy the operation lowering does not recognize groups
	// by tags, which is what makes the unnormalized zero value harmless rather
	// than a second spelling of the default to keep in step.
	Grouping GroupingStrategy

	// schemas is the set of component-schema names the document declares.
	//
	// It is unexported and read through DeclaresSchema because a struct copy
	// shares a map rather than copying it: as an exported field it would be the
	// one part of a by-value context a callee could write to, and the write would
	// be visible to its caller's caller. An accessor makes that unrepresentable
	// rather than merely discouraged, which is what TestCtx_HasNoExportedMap
	// holds the struct to.
	schemas map[string]bool

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
}

// New derives the immutable context for one loaded source.
//
// It takes the document and its identity rather than the loader's own result
// type, which is what keeps this package below the loader for everything but
// the version grammar, and what lets a test build a context without a load.
//
// The schema-name index is built here rather than on first use so that every
// reader sees the same set regardless of source order: a $ref or a discriminator
// mapping resolved mid-lowering must see a component declared later in the
// document as a valid target. It stays nil for a document that declares no
// components, which reads the same as an empty set.
//
// The $dynamicAnchor index is deliberately not derived here, though GitHub #172
// asked for it. Building it emits a diagnostic when the walk hits its bounds, so
// building it is a lowering action rather than context: done at entry, that
// warning would reach documents that never write $dynamicRef, changing what the
// compiler reports about them. It stays where it is, built on first use.
func New(srcIndex int, doc *soa.OpenAPI, src ir.SourceInfo, grouping GroupingStrategy) Ctx {
	return Ctx{
		Doc:      doc,
		Source:   src,
		SrcIndex: srcIndex,
		Grouping: grouping,
		schemas:  declaredSchemaNames(doc),
	}
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
// own path, and what it declares.
//
// It is derived on use rather than stored beside the context. A stored copy
// would be a second place the same two facts live, free to disagree with the
// context after any change to it — and the whole point of the context is that
// there is one answer.
func (c Ctx) RefScope() resolve.Scope {
	return resolve.Scope{SelfPath: c.Source.Path, Declares: c.DeclaresSchema}
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
func (c Ctx) DiagAt(sev ir.Severity, code, pointer, format string, args ...any) ir.Diagnostic {
	return diag.Newf(sev, code, c.ProvenanceAt(pointer), format, args...)
}

// ProvenanceAt is where a Provenance is built, and the only place this compiler
// spells the source index into one.
//
// It covers the entities as well as the diagnostics. GitHub #86 scoped itself to
// diagnostic sites because that is where the defect it chased showed up, but the
// defect is hand-writing the pair at all: a Provenance whose source index is
// wrong misattributes a node just as surely as it misattributes a report.
func (c Ctx) ProvenanceAt(pointer string) ir.Provenance {
	return ir.Provenance{Source: c.SrcIndex, Pointer: pointer}
}
