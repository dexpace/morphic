// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/ir"
)

// TestReader_ReadsEachBodyAndModelOnce pins the reader's memo: operations
// naming one body read its page once per walk they send, and a model several
// members hold, or several bodies, is read once.
func TestReader_ReadsEachBodyAndModelOnce(t *testing.T) {
	t.Parallel()
	info := &ir.Model{ID: "type/info", Properties: []ir.Property{
		{ID: "property/info/total_count", Name: ir.Naming{Source: "total_count", Canonical: "total_count"},
			Type: ir.TypeRef{Target: "type/primitive/integer"}},
	}}
	body := func(id ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Properties: []ir.Property{
			{ID: ir.PropertyID(id + "/result"), Name: ir.Naming{Source: "result", Canonical: "result"},
				Type: ir.TypeRef{Target: "type/list"}},
			{ID: ir.PropertyID(id + "/a"), Name: ir.Naming{Source: "a", Canonical: "a"}, Type: ir.TypeRef{Target: "type/info"}},
			{ID: ir.PropertyID(id + "/b"), Name: ir.Naming{Source: "b", Canonical: "b"}, Type: ir.TypeRef{Target: "type/info"}},
		}}
	}
	r := newReader(compileddoc.New(&ir.Document{Types: ir.TypeRegistry{
		"type/list": &ir.List{ID: "type/list"}, "type/info": info, "type/one": body("type/one"), "type/two": body("type/two"),
		"type/primitive/integer": &ir.Primitive{ID: "type/primitive/integer", PrimitiveKind: ir.PrimitiveInteger},
	}}))
	for range 3 {
		for _, id := range []ir.TypeID{"type/one", "type/two"} {
			pg := r.page(pageKey{body: id})
			assert.Equal(t, []ir.PropertyID{ir.PropertyID(id + "/result")}, pg.items)
			assert.Nil(t, pg.totalCount, "a total two members hold counts neither")
		}
	}
	assert.Len(t, r.pages, 2, "a page per body")
	assert.Len(t, r.models.read, 3, "a reading per model: the two bodies and the one their members hold")
}

// TestPageContents_ListsWhatCanBeAPage pins the contents a walk reads its page
// from: those of each success response that do not stream, in response order,
// none of a response with no body or whose every content streams, and none
// after the reader stops asking.
func TestPageContents_ListsWhatCanBeAPage(t *testing.T) {
	t.Parallel()
	content := func(mediaType string, streams bool) ir.Content {
		ct := ir.Content{MediaType: mediaType}
		if streams {
			ct.Stream = &ir.ContentStream{}
		}
		return ct
	}
	op := &ir.Operation{Responses: []ir.Response{
		{},
		{Payload: &ir.Payload{Contents: []ir.Content{content("text/event-stream", true)}}},
		{Payload: &ir.Payload{Contents: []ir.Content{content("application/json", false), content("text/event-stream", true),
			content("application/xml", false)}}},
		{Payload: &ir.Payload{Contents: []ir.Content{content("application/yaml", false)}}},
	}}
	var all []string
	for ct := range pageContents(op) {
		all = append(all, ct.MediaType)
	}
	assert.Equal(t, []string{"application/json", "application/xml", "application/yaml"}, all)

	var first []string
	for ct := range pageContents(op) {
		first = append(first, ct.MediaType)
		break
	}
	assert.Equal(t, []string{"application/json"}, first, "a reader stopping at the first reads no more")
}
