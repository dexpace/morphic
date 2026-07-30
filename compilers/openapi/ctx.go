package openapi

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/ir"
)

// ctx is everything a lowering may read and none may change: the parsed
// document, the caller's options, the identity of the source being lowered, and
// the indexes derived from them once at entry.
//
// It is passed and copied by value. That is what makes "immutable" mean
// something here — a callee cannot hand a changed context back to its caller by
// writing to one, because it never holds the caller's.
//
// It is unexported for now. Every lowering that reads it lives in this package,
// and micro-compiler-design §5.1 gives compilers/openapi the compiler's public
// face and nothing else; it exports when it first has to cross a package
// boundary, which is the reference-resolution extraction (GitHub #174).
type ctx struct {
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
	// rather than merely discouraged, which is what TestCtx_HasNoExportedMap
	// holds the struct to.
	schemas map[string]bool
}

// newCtx derives the immutable context for one loaded source.
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
func newCtx(srcIndex int, doc *load.Document, opts Options) ctx {
	return ctx{
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
func (c ctx) DeclaresSchema(name string) bool { return c.schemas[name] }
