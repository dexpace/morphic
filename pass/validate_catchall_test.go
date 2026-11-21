// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// catchAllDoc is validDoc beside a map, a model with a value and a pattern
// catch-all, a model with a pattern catch-all alone and a model extending the
// first, with one operation taking a form body typed by target and carrying
// catch.
func catchAllDoc(target ir.TypeID, catch map[ir.TypeID]ir.CatchAllParts) *ir.Document {
	doc := validDoc()
	stringType := ir.TypeRef{Target: "type/primitive/string"}
	doc.Types["type/map"] = &ir.MapT{ID: "type/map", Value: stringType}
	doc.Types["type/open"] = &ir.Model{ID: "type/open", AdditionalProperties: &ir.AdditionalProperties{
		Value: &stringType, Patterns: []ir.PatternProperties{{Pattern: "^f_", Value: stringType}},
	}}
	doc.Types["type/patterns"] = &ir.Model{ID: "type/patterns", AdditionalProperties: &ir.AdditionalProperties{
		Patterns: []ir.PatternProperties{{Pattern: "^f_", Value: stringType}},
	}}
	doc.Types["type/derived"] = &ir.Model{ID: "type/derived", Base: &ir.TypeRef{Target: "type/open"}}
	doc.Services = docWithOperation(ir.Operation{Request: &ir.Payload{Contents: []ir.Content{{
		MediaType: "multipart/form-data", Type: ir.TypeRef{Target: target}, CatchAllEncoding: catch,
	}}}}).Services
	return doc
}

// TestValidate_CatchAllEncodingKeys holds a content's catch-all encoding to the
// catch-alls its body has: a key naming a holder among the body's shapes, with
// a value only where the holder has one and patterns only where it lists them,
// is clean; each of the others is an error, and each has a clean twin.
func TestValidate_CatchAllEncodingKeys(t *testing.T) {
	t.Parallel()
	value := &ir.PartEncoding{ContentTypes: []string{"text/plain"}}
	file := ir.PartEncoding{ContentTypes: []string{"application/octet-stream"}, HasFilename: true}
	for _, tc := range []struct {
		name   string
		target ir.TypeID
		catch  map[ir.TypeID]ir.CatchAllParts
		bad    bool
	}{
		{"a map's value", "type/map", map[ir.TypeID]ir.CatchAllParts{"type/map": {Value: value}}, false},
		{"a model's value and pattern", "type/open", map[ir.TypeID]ir.CatchAllParts{
			"type/open": {Value: value, Patterns: map[string]ir.PartEncoding{"^f_": file}},
		}, false},
		{"a base model's, through the flattening", "type/derived", map[ir.TypeID]ir.CatchAllParts{
			"type/open": {Value: value},
		}, false},
		{"a pattern alone, where the model lists it", "type/patterns", map[ir.TypeID]ir.CatchAllParts{
			"type/patterns": {Patterns: map[string]ir.PartEncoding{"^f_": file}},
		}, false},
		{"an empty key, which the empty-reference check reports", "type/map", map[ir.TypeID]ir.CatchAllParts{
			"": {Value: value},
		}, false},
		{"a model with no catch-all", "type/m", map[ir.TypeID]ir.CatchAllParts{"type/m": {Value: value}}, true},
		{"a type outside the body's shapes", "type/m", map[ir.TypeID]ir.CatchAllParts{"type/open": {Value: value}}, true},
		{"a value where the holder has none", "type/patterns", map[ir.TypeID]ir.CatchAllParts{
			"type/patterns": {Value: value},
		}, true},
		{"a pattern the holder does not list", "type/open", map[ir.TypeID]ir.CatchAllParts{
			"type/open": {Patterns: map[string]ir.PartEncoding{"^g_": file}},
		}, true},
		{"a type that exists nowhere, which the dangling check reports", "type/map",
			map[ir.TypeID]ir.CatchAllParts{"type/ghost": {Value: value}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found := withCode(validated(t, catchAllDoc(tc.target, tc.catch)), "ir/encoding-key-unknown-catch-all")
			if !tc.bad {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1, "exactly the planted entry is wrong")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
		})
	}
}

// TestValidate_ACatchAllKeyNamingNoTypeDrawsOneCode holds a key naming no type
// to the dangling reference's code alone: one defect, one code.
func TestValidate_ACatchAllKeyNamingNoTypeDrawsOneCode(t *testing.T) {
	t.Parallel()
	value := &ir.PartEncoding{ContentTypes: []string{"text/plain"}}
	diags := validated(t, catchAllDoc("type/map", map[ir.TypeID]ir.CatchAllParts{"type/ghost": {Value: value}}))
	codes := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		codes = append(codes, diagnostic.Code)
	}
	assert.Equal(t, []string{"ir/dangling-type-ref"}, codes)
}
