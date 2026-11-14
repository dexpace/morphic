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
	str := ir.TypeRef{Target: "t/prim/string"}
	doc.Types["t/map"] = &ir.MapT{ID: "t/map", Value: str}
	doc.Types["t/open"] = &ir.Model{ID: "t/open", AdditionalProps: &ir.AdditionalProps{
		Value: &str, Patterns: []ir.PatternProps{{Pattern: "^f_", Value: str}},
	}}
	doc.Types["t/patterns"] = &ir.Model{ID: "t/patterns", AdditionalProps: &ir.AdditionalProps{
		Patterns: []ir.PatternProps{{Pattern: "^f_", Value: str}},
	}}
	doc.Types["t/derived"] = &ir.Model{ID: "t/derived", Base: &ir.TypeRef{Target: "t/open"}}
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
	file := ir.PartEncoding{ContentTypes: []string{"application/octet-stream"}, Filename: true}
	for _, tc := range []struct {
		name   string
		target ir.TypeID
		catch  map[ir.TypeID]ir.CatchAllParts
		bad    bool
	}{
		{"a map's value", "t/map", map[ir.TypeID]ir.CatchAllParts{"t/map": {Value: value}}, false},
		{"a model's value and pattern", "t/open", map[ir.TypeID]ir.CatchAllParts{
			"t/open": {Value: value, Patterns: map[string]ir.PartEncoding{"^f_": file}},
		}, false},
		{"a base model's, through the flattening", "t/derived", map[ir.TypeID]ir.CatchAllParts{
			"t/open": {Value: value},
		}, false},
		{"a pattern alone, where the model lists it", "t/patterns", map[ir.TypeID]ir.CatchAllParts{
			"t/patterns": {Patterns: map[string]ir.PartEncoding{"^f_": file}},
		}, false},
		{"a model with no catch-all", "t/m", map[ir.TypeID]ir.CatchAllParts{"t/m": {Value: value}}, true},
		{"a type outside the body's shapes", "t/m", map[ir.TypeID]ir.CatchAllParts{"t/open": {Value: value}}, true},
		{"a value where the holder has none", "t/patterns", map[ir.TypeID]ir.CatchAllParts{
			"t/patterns": {Value: value},
		}, true},
		{"a pattern the holder does not list", "t/open", map[ir.TypeID]ir.CatchAllParts{
			"t/open": {Patterns: map[string]ir.PartEncoding{"^g_": file}},
		}, true},
		{"a type that exists nowhere", "t/map", map[ir.TypeID]ir.CatchAllParts{"t/ghost": {Value: value}}, true},
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
