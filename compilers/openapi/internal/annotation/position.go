// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
)

// SchemaOf returns the schema body written at this position, including one
// that also carries a $ref — an example or bound written beside a $ref binds
// the position, not the referent, the same rule fillPropertyAnnotations and
// fillPropertyConstraints apply at a property. It returns nil only where no
// body is written: a nil either, or a boolean schema, which admits no
// annotations.
//
// Every position modeled as a site reads it through At: a named component
// (lowerComponentSchema) and a $ref'd internal sub-schema (hoistSubSchema, fed
// one hop at a time by the schema bindings).
func SchemaOf(js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || js.IsBool() {
		return nil
	}
	return js.GetSchema()
}
