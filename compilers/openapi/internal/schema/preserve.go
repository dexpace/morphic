// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// How a construct is kept verbatim is annotation's: keying, locating, raw
// conversion, and unpreservable diagnostics. The helpers below wrap those
// calls at schema positions solely to feed failed conversions into
// RecordDiscardedSite so post-settlement pruning removes landings beneath an
// unpreservable construct without inspecting diagnostics by code.

// ReadAnnotations reads site's annotations at pointer and records any
// unpreservable construct provenances as discarded sites for pruning.
func ReadAnnotations(loweringContext lowering.Ctx, site annotation.SchemaPositionAnnotationSources, pointer jsontext.Pointer) (annotation.Set, []ir.Diagnostic) {
	set, diags := annotation.Read(site, pointer, loweringContext.Place())
	recordDiscardedSites(loweringContext, set.Unpreserved)
	return set, diags
}

// preserveNode converts node into unmodeled at at and records at as a
// discarded site when conversion fails.
func preserveNode(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, key string, node *yaml.Node,
	reason ir.UnmodeledReason, at jsontext.Pointer,
) (bool, []ir.Diagnostic) {
	prov := loweringContext.Place().At(at)
	kept, diags := annotation.PreserveNodeInto(unmodeled, key, node, reason, prov)
	if !kept && len(diags) > 0 {
		RecordDiscardedSite(loweringContext, prov)
	}
	return kept, diags
}

// preserveSchemaKeyword keeps keyword of schema at pointer in unmodeled and
// records the keyword's provenance as a discarded site when conversion fails.
func preserveSchemaKeyword(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, keyword string,
	reason ir.UnmodeledReason, pointer jsontext.Pointer,
) (bool, []ir.Diagnostic) {
	kept, diags := annotation.PreserveSchemaKeyword(unmodeled, schema, keyword, reason, pointer, loweringContext.Place())
	if !kept && len(diags) > 0 {
		RecordDiscardedSite(loweringContext, loweringContext.Place().At(pointer+ids.Ptr(keyword)))
	}
	return kept, diags
}

// preserveValidationOnlyKeyword keeps a validation-only keyword of schema at
// pointer in unmodeled and records the keyword's provenance as a discarded
// site when conversion fails.
func preserveValidationOnlyKeyword(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, keyword string,
	pointer jsontext.Pointer,
) []ir.Diagnostic {
	kept, diags := annotation.PreserveValidationOnlyKeyword(unmodeled, schema, keyword, pointer, loweringContext.Place())
	if !kept && len(diags) > 0 {
		RecordDiscardedSite(loweringContext, loweringContext.Place().At(pointer+ids.Ptr(keyword)))
	}
	return diags
}

// PreserveUnknownKeywords records every keyword schema writes that no field of
// the schema model names, or that the model names and the dialect does not
// define (undefinedKeywords), and no reader above it already kept (GitHub #297).
//
// It is the last thing an attachment does, as annotation.UnknownKeywordsIn
// requires, so it is called here rather than inside annotation.Read: a
// dynamic reference, which the model names no field for, is decided by
// recordUnexpandedDynamicRef, run after Read; a draft's `dependencies` is
// Read's (annotation.Dialect), and `additionalItems` beside a list the
// tuple's, its rest. td is the node the position lowered to, nil for none.
func PreserveUnknownKeywords(loweringContext lowering.Ctx, unmodeled *ir.Unmodeled, schema *statement.Schema, pointer jsontext.Pointer, td ir.TypeDef,
	left ...string,
) []ir.Diagnostic {
	unpreserved, diags := annotation.UnknownKeywordsIn(unmodeled, schema, pointer, loweringContext.Place(), slices.Concat(unknownTaken(loweringContext, schema, pointer), left)...)
	recordDiscardedSites(loweringContext, unpreserved)
	return append(diags, annotation.UndefinedKeywordsIn(unmodeled, schema, undefinedKeywords(loweringContext, schema), pointer, loweringContext.Place())...)
}

// unknownTaken returns the keys schema, written at pointer, writes that the
// schema model names no field for and a reader other than
// PreserveUnknownKeywords reads, by the dialect it is read in.
func unknownTaken(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) []string {
	var taken []string
	if _, listed := listedItems(schema); listed {
		taken = append(taken, "additionalItems")
	}
	rules := loweringContext.Rules().At(loweringContext.Position(pointer))
	if rules.Takes("definitions", specrules.Map) {
		// The drafts' spelling of $defs, whose entries lower where a $ref names one.
		taken = append(taken, "definitions")
	}
	if rules.Defines("$recursiveRef") {
		// 2019-09's dynamic reference, which recordUnexpandedDynamicRef decides
		// where no $dynamicRef stands beside it, and the anchor it reads, which
		// the binder consumes as a $dynamicAnchor.
		taken = append(taken, "$recursiveAnchor")
		if !hasKey(schema, "$dynamicRef") {
			taken = append(taken, "$recursiveRef")
		}
	}
	return taken
}

// writesUnknown reports whether key, a key schema writes at pointer, is one
// PreserveUnknownKeywords keeps there.
func writesUnknown(loweringContext lowering.Ctx, schema *statement.Schema, key string, pointer jsontext.Pointer) bool {
	if slices.Contains(undefinedKeywords(loweringContext, schema), key) {
		return true
	}
	return annotation.UnknownKeyword(schema, key) && !slices.Contains(unknownTaken(loweringContext, schema, pointer), key)
}

// undefinedKeywords returns the keywords schema writes that the schema model
// names but nothing reads, because the dialect schema is read in does not
// define them: `nullable`, in a schema whose dialect defines no such keyword,
// as 3.1's and 3.2's do not, unless the nullable-inference policy reads this
// one (nullableRead). Where the dialect defines it, as in a 3.0 schema, one
// beside its $ref included, it is read by its declared effect
// (docs/compilers/openapi.md#nullable).
func undefinedKeywords(loweringContext lowering.Ctx, schema *statement.Schema) []string {
	if !hasKey(schema, "nullable") || nullableRead(loweringContext, schema) {
		return nil
	}
	return []string{"nullable"}
}

// nullableRead reports whether the `nullable` schema writes is read: by the
// dialect that defines the keyword, or, where none does, by the
// nullable-inference policy, which reads a boolean true and no other value. A
// keyword neither reads is one the dialect does not define, kept as such
// (docs/compilers/openapi.md#nullable).
func nullableRead(loweringContext lowering.Ctx, schema *statement.Schema) bool {
	return loweringContext.SchemaRules().Defines("nullable") || loweringContext.NullableInference.Honours() && nullableTrue(schema)
}
