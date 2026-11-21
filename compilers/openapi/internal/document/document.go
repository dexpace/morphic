// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// Meta is the document-level metadata: what ir.Document carries that belongs
// to neither the type graph nor the service graph.
//
// It exists so the lowering can return what it read instead of writing into the
// document as it goes. The caller owns the document and does the assigning,
// which is what lets every lowering in this package be checked against a
// literal context and nothing else.
type Meta struct {
	Name           string
	Version        string
	TermsOfService string
	Docs           ir.Docs
	Contact        *ir.Contact
	License        *ir.License
	Unmodeled      ir.Unmodeled
}

// Lower lowers the document-level metadata that is not part of the type or
// service graph: info, and what the document writes that lowers to no node of
// its own, kept verbatim (docs/ir/unmodeled.md).
//
// It must run after every lowering that follows a reference, as which
// components it keeps depends on what they reached (service.Ctx.Reached). The
// promotions run after it, reading the root's kept keys, and follow none.
func Lower(serviceContext service.Ctx) (Meta, []ir.Diagnostic) {
	meta := lowerInfo(serviceContext)

	ext, diags := documentExtensions(serviceContext)
	meta.Unmodeled = ext

	retained, retainDiags := retainUnreferencedComponents(serviceContext)
	meta.Unmodeled = annotation.MergeUnmodeled(meta.Unmodeled, retained)
	diags = append(diags, retainDiags...)

	diags = append(diags, keepJSONSchemaDialect(serviceContext, &meta.Unmodeled)...)
	return meta, append(diags, documentUnknownKeys(serviceContext, &meta.Unmodeled)...)
}
