package openapi

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// lowerCtx is everything a lowering may read and none may change: the parsed
// document, the caller's options, the identity of the source being lowered, and
// the indexes derived from them once at entry.
//
// It is a value rather than a pointer, so a function that takes one takes a
// copy. That is what will make "immutable" enforceable rather than conventional
// once the lowering methods become functions over it (GitHub #173 onward);
// today it is held by the lowerer and read through l.ctx, and no lowering
// writes to it. What already holds either way is the map below: a copy shares
// it, so it is not reachable to write at all.
//
// The name is deliberately not ctx. This package already spends that identifier
// twice — on the context.Context Compile takes, which the styleguide reserves it
// for, and on an opContext local in operations.go — so a third meaning would be
// shadowed at both sites and ambiguous to a reader everywhere else.
//
// It stays unexported for now: micro-compiler-design §5.1 gives compilers/openapi
// the compiler's public face and nothing else, and every lowering that reads this
// still lives here. It becomes an exported Ctx when it first has to cross a
// package boundary, which is the reference-resolution extraction (GitHub #174).
type lowerCtx struct {
	// Doc is the parsed, reference-resolved source document. Lowering reads it
	// and never writes through it.
	Doc *soa.OpenAPI
	// Opts is the caller's compiler options.
	Opts Options
	// Source is the identity of the loaded source, stamped into Document.Sources.
	Source ir.SourceInfo
	// SrcIndex is this source's index within the compile, stamped into every
	// Provenance.
	SrcIndex int

	// schemas is the set of component-schema names the document declares.
	//
	// It is unexported and read through DeclaresSchema because a struct copy
	// shares a map rather than copying it: as an exported field it would be the
	// one part of a by-value context a callee could write to, and the write would
	// be visible to its caller's caller. An accessor makes that unrepresentable
	// rather than merely discouraged, which is what
	// TestLowerCtx_HasNoExportedMap holds the struct to.
	schemas map[string]bool
}

// newLowerCtx derives the immutable context for one loaded source.
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
func newLowerCtx(srcIndex int, doc *load.Document, opts Options) lowerCtx {
	return lowerCtx{
		Doc:      doc.Doc,
		Opts:     opts,
		Source:   doc.Source,
		SrcIndex: srcIndex,
		schemas:  declaredSchemaNames(doc.Doc),
	}
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
func (c lowerCtx) DeclaresSchema(name string) bool { return c.schemas[name] }

// exclusiveBoundIsBoolean reports whether this document's dialect spells
// exclusiveMinimum/exclusiveMaximum as a boolean modifier (OpenAPI 3.0) rather
// than a numeric bound (the 2020-12 dialect of 3.1 and 3.2). An unrecognized
// version defaults to the 2020-12 numeric form.
//
// It reads the document version, which makes it a question about the context
// rather than about any schema — annotation.Constraints takes the answer as a
// parameter precisely so the reader never has to ask it.
func (c lowerCtx) exclusiveBoundIsBoolean() bool {
	minor, _ := load.SupportedMinor(c.Doc.OpenAPI)
	return minor == "3.0"
}

// refScope is the context seen as a reference-resolution scope: the document's
// own path, and what it declares.
//
// It is derived on use rather than stored beside the context. A stored copy
// would be a second place the same two facts live, free to disagree with the
// context after any change to it — and the whole point of the context is that
// there is one answer.
func (c lowerCtx) refScope() resolve.Scope {
	return resolve.Scope{SelfPath: c.Source.Path, Declares: c.DeclaresSchema}
}

// diagAt builds one diagnostic at pointer, stamped with this compile's source
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
func (c lowerCtx) diagAt(sev ir.Severity, code, pointer, format string, args ...any) ir.Diagnostic {
	return diag.Newf(sev, code, c.provenanceAt(pointer), format, args...)
}

// provenanceAt is where a Provenance is built, and the only place this compiler
// spells the source index into one.
//
// It covers the entities as well as the diagnostics. GitHub #86 scoped itself to
// diagnostic sites because that is where the defect it chased showed up, but the
// defect is hand-writing the pair at all: a Provenance whose source index is
// wrong misattributes a node just as surely as it misattributes a report.
func (c lowerCtx) provenanceAt(pointer string) ir.Provenance {
	return ir.Provenance{Source: c.SrcIndex, Pointer: pointer}
}
