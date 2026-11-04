// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// fillSequential lowers 3.2 sequential-media fields: itemSchema becomes the
// element type, and itemEncoding becomes Content.ItemEncoding, with Multi set
// because the construct describes a repeated tail by definition, and Filename
// where each item is a file (itemFile). A multipart body of files gets an item
// encoding saying so where it writes none, as a file part gets an entry.
//
// That lowering only holds when no prefixEncoding accompanies it: prefixes make
// itemEncoding govern the items *after* them rather than every item, which a
// single every-item encoding would misstate. Those documents take
// positionalEncoding instead.
func fillSequential(c lowering.Ctx, ts *compile.Types, content *ir.Content, media *soa.MediaType, mediaPtr jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if item := media.GetItemSchema(); item != nil {
		ref, itemDiags := schema.Ref(c, ts, schema.TopLevelDepth, item, mediaPtr+ids.Ptr("itemSchema"))
		diags = append(diags, itemDiags...)
		content.Item = &ref
		diags = append(diags, itemsOfOneDocument(c, content.MediaType, mediaPtr)...)
	}
	if len(media.GetPrefixEncoding()) > 0 {
		return append(diags, positionalEncoding(c, content, media, mediaPtr)...)
	}
	enc := media.GetItemEncoding()
	pe := ir.PartEncoding{}
	if enc != nil {
		var unmodeled ir.Unmodeled
		var encDiags []ir.Diagnostic
		pe, unmodeled, encDiags = encodingConfig(c, ts, enc, mediaPtr+ids.Ptr("itemEncoding"), "itemEncoding")
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, unmodeled)
		diags = append(diags, encDiags...)
	}
	file := itemFile(c, media.GetItemSchema(), content.MediaType, pe.ContentTypes)
	if enc == nil && !file.file {
		return diags
	}
	pe.Multi = true
	pe = file.apply(pe)
	content.ItemEncoding = &pe
	return diags
}

// itemFile reads the items of a sequential body under mt, each declared by
// item, as files, as a multipart part's declaration reads (fileFor): an item is
// one value, not the repetition an array part declares. Only a multipart body
// carries raw bytes, and an item no schema describes is said to be nothing.
func itemFile(c lowering.Ctx, item *oas3.JSONSchema[oas3.Referenceable], mt string, contentTypes []string) partFile {
	if !isMultipart(mt) || !annotation.Written(item) {
		return partFile{}
	}
	return fileFor(c, readFile(c, item), contentTypes)
}

// itemsOfOneDocument reports an itemSchema on a media type whose body is one
// document. Which media types repeat a structure is no closed set, so it fires only
// where the syntax says, and never on one the caller's policy calls a stream.
func itemsOfOneDocument(c lowering.Ctx, mediaType string, mediaPtr jsontext.Pointer) []ir.Diagnostic {
	if !lowering.HoldsOneDocument(mediaType) || c.MediaTypeStreams(mediaType) {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.ItemSchemaOnDocument, mediaPtr+ids.Ptr("itemSchema"),
		"itemSchema describes the items of a sequential media type, and a body of %q is one document; "+
			"it lowers as a stream, as declared", mediaType)}
}

// positionalEncoding keeps 3.2 positional prefixEncoding — and the itemEncoding
// that governs the tail after it — verbatim, with one info diagnostic.
// Content.ItemEncoding states one encoding for every item, so it cannot say
// "these two in order, then the rest alike": lowering only the tail into it
// would drop the prefixes and assert their encoding governs every item. The
// ordinals a positional form needs are a gap the IR can close later, so the
// entries carry ReasonNoIRHome rather than a degraded lowering.
func positionalEncoding(c lowering.Ctx, content *ir.Content, media *soa.MediaType, mediaPtr jsontext.Pointer) []ir.Diagnostic {
	root := media.GetRootNode()
	// The announcement follows prefixEncoding, the construct that brought this
	// lowering here: an itemEncoding beside it is optional, so its absence must not
	// suppress the message, and its own conversion failure reports separately.
	at := mediaPtr + ids.Ptr("prefixEncoding")
	kept, diags := annotation.PreserveNodeInto(&content.Unmodeled, "openapi:prefixEncoding",
		nodeview.Child(root, "prefixEncoding"), ir.ReasonNoIRHome, c.ProvenanceAt(at))
	_, itemDiags := annotation.PreserveNodeInto(&content.Unmodeled, "openapi:itemEncoding",
		nodeview.Child(root, "itemEncoding"), ir.ReasonNoIRHome, c.ProvenanceAt(mediaPtr+ids.Ptr("itemEncoding")))
	diags = append(diags, itemDiags...)
	if !kept {
		// Reaching here means prefixEncoding is declared — that is the only reason
		// this lowering runs — yet nothing of it was written. Unlike every other
		// preservation site, an empty payload here cannot mean "there was no
		// construct", so it is reported rather than passed over (GitHub #144).
		return append(diags, c.DiagAt(ir.SeverityError, diag.UnpreservableConstruct, at,
			"prefixEncoding is declared but its source node could not be read; it is "+
				"represented in the IR in no form at all"))
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, mediaPtr,
		"prefixEncoding is positional and has no per-item IR home; it and any itemEncoding are kept under Unmodeled"))
}
