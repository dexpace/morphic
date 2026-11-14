// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestReader_ReadsEachBodyAndModelOnce pins the reader's memo: operations
// naming one body read its page once per walk they send, and a model several
// members hold, or several bodies, is read once.
func TestReader_ReadsEachBodyAndModelOnce(t *testing.T) {
	t.Parallel()
	info := &ir.Model{ID: "t/info", Properties: []ir.Property{
		{ID: "p/info/total_count", Name: ir.Naming{Source: "total_count", Canonical: "total_count"},
			Type: ir.TypeRef{Target: "t/prim/integer"}},
	}}
	body := func(id ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Properties: []ir.Property{
			{ID: ir.PropID(id + "/result"), Name: ir.Naming{Source: "result", Canonical: "result"},
				Type: ir.TypeRef{Target: "t/list"}},
			{ID: ir.PropID(id + "/a"), Name: ir.Naming{Source: "a", Canonical: "a"}, Type: ir.TypeRef{Target: "t/info"}},
			{ID: ir.PropID(id + "/b"), Name: ir.Naming{Source: "b", Canonical: "b"}, Type: ir.TypeRef{Target: "t/info"}},
		}}
	}
	r := newReader(lowering.NewWhole(&ir.Document{Types: ir.TypeRegistry{
		"t/list": &ir.List{ID: "t/list"}, "t/info": info, "t/one": body("t/one"), "t/two": body("t/two"),
		"t/prim/integer": &ir.Primitive{ID: "t/prim/integer", Prim: ir.PrimInteger},
	}}))
	for range 3 {
		for _, id := range []ir.TypeID{"t/one", "t/two"} {
			pg := r.page(pageKey{body: id})
			assert.Equal(t, []ir.PropID{ir.PropID(id + "/result")}, pg.items)
			assert.Nil(t, pg.totalCount, "a total two members hold counts neither")
		}
	}
	assert.Len(t, r.pages, 2, "a page per body")
	assert.Len(t, r.models, 3, "a reading per model: the two bodies and the one their members hold")
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
