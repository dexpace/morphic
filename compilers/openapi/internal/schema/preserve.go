// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// How a construct is kept verbatim is annotation's: the key its entry takes, the
// position it is located at, the conversion of its value, and the report when
// it cannot be kept. This package calls annotation for that directly, with
// c.Place(). What it decides is what a position keeps and why: which
// keywords, under which reason, announced how. PreserveUnknownKeywords is one
// such decision, reading the dialect a schema is read in. A preservation
// function that decides nothing and only keys, locates or converts belongs in
// annotation, or the same call gains a second spelling.

// PreserveUnknownKeywords records every keyword s writes that no field of the
// schema model names, or that the model names and the dialect does not define
// (undefinedKeywords), and no reader above it already kept (GitHub #297).
//
// It is the last thing an attachment does, as annotation.UnknownKeywordsIn
// requires, so it is called here rather than inside annotation.Read:
// $dynamicRef has no field in the schema model either and is decided by
// recordUnexpandedDynamicRef, which runs after Read; a draft's `dependencies`
// is Read's (annotation.Dialect), and `additionalItems` beside a list the
// census's (keywordHome). td is the node the position lowered to, nil for none
// of its own.
func PreserveUnknownKeywords(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer, td ir.TypeDef) []ir.Diagnostic {
	var taken []string
	if _, listed := listedItems(s); listed {
		taken = append(taken, "additionalItems")
	}
	if lowering.SchemaRulesAt(c, pointer).Takes("definitions", oas.Map) {
		// The drafts' spelling of $defs, whose entries lower where a $ref names one.
		taken = append(taken, "definitions")
	}
	diags := annotation.UnknownKeywordsIn(p, s, pointer, c.Place(), taken...)
	return append(diags, annotation.UndefinedKeywordsIn(p, s, undefinedKeywords(c, s), pointer, c.Place())...)
}

// undefinedKeywords returns the keywords s writes that the schema model names
// but nothing reads, because the dialect s is read in does not define them:
// `nullable` while the nullable-widening policy is off, in a schema whose
// dialect defines no such keyword, as 3.1's and 3.2's do not. Where it is
// defined, as in a 3.0 schema, one beside its $ref included, it is read by its
// declared effect (docs/compilers/openapi.md#nullable), and the policy reads it
// everywhere.
func undefinedKeywords(c lowering.Ctx, s *oas3.Schema) []string {
	if c.NullableWidening.Honours() || c.SchemaRules().Defines("nullable") ||
		annotation.RawPropertyNode(s, "nullable") == nil {
		return nil
	}
	return []string{"nullable"}
}
