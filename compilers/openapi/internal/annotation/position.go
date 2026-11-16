// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/yml"
	yaml "go.yaml.in/yaml/v3"
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

// IsFalseSchema reports whether js is the boolean `false` schema, which no
// other value written in its place is, though the parser decodes a null, a
// string, a number or a list as `false`
// (docs/parser-library.md#null-schema-is-false).
// A statement's reading is statement.Truth.ReadsFalse; this one goes once no
// reader of a schema's children reads the parser's model (GitHub #1991).
func IsFalseSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && js.IsBool() && js.GetBool() != nil && !*js.GetBool() && !IsNoSchema(js)
}

// IsNoSchema reports whether js is written as no schema: its node, its alias
// followed, is neither a mapping nor a scalar tagged !!bool. The parser reads
// such a value as the boolean `false` schema
// (docs/parser-library.md#null-schema-is-false), so only the node tells them
// apart, on every route a schema is reached by.
func IsNoSchema(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	if js == nil {
		return false
	}
	n := yml.ResolveAlias(js.GetCore().GetRootNode())
	if n == nil || n.Kind == yaml.MappingNode {
		return false
	}
	return n.Kind != yaml.ScalarNode || n.ShortTag() != "!!bool"
}

// Written reports whether js is a schema written at its position: present,
// and not written as no schema, which states nothing about the values there
// (IsNoSchema). The load phase reports such a value; every reader takes it as
// left out (docs/compilers/openapi.md#null-schema).
// A statement's reading is its Truth other than statement.Unwritten; this one
// goes once no reader of a schema's children reads the parser's model (GitHub
// #1991).
func Written(js *oas3.JSONSchema[oas3.Referenceable]) bool {
	return js != nil && !IsNoSchema(js)
}
