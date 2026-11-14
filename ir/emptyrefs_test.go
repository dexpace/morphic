// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// emptyReferencesOf walks doc with an EmptyReferences visitor and returns what
// it collected, in walk order.
func emptyReferencesOf(doc *ir.Document) []ir.EmptyReference {
	var found ir.EmptyReferences
	ir.WalkValues(doc, ir.DocumentPath, &found)
	return found.List()
}

// emptyRefService wraps s as the one service of a document.
func emptyRefService(s ir.Service) *ir.Document {
	return &ir.Document{Services: []ir.Service{s}}
}

// emptyRefOperation wraps op in one service and group.
func emptyRefOperation(op ir.Operation) *ir.Document {
	return emptyRefService(ir.Service{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}}})
}

// TestEmptyReferences_ReadsEachShapeAnIDIsHeldIn walks one document per shape
// an ID-typed position holds its value in (a scalar, a pointer, a slice
// element, a map key and a map value), each with one empty reference, and
// expects exactly one EmptyReference: the class of ID the position holds and
// the path of the walk that reaches it, spelled as irverify spells it.
func TestEmptyReferences_ReadsEachShapeAnIDIsHeldIn(t *testing.T) {
	t.Parallel()
	overload := ir.OpID("")
	for _, tc := range []struct {
		name  string
		doc   *ir.Document
		class reflect.Type
		path  string
	}{
		{
			name:  "scalar",
			doc:   emptyRefService(ir.Service{Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ""}}}}}),
			class: reflect.TypeFor[ir.AuthID](),
			path:  "doc.Services[0].Auth[0].Schemes[0].Scheme",
		},
		{
			name:  "pointer",
			doc:   emptyRefOperation(ir.Operation{OverloadOf: &overload}),
			class: reflect.TypeFor[ir.OpID](),
			path:  "doc.Services[0].Groups[0].Operations[0].OverloadOf",
		},
		{
			name:  "slice element",
			doc:   emptyRefService(ir.Service{Servers: []ir.ServerID{""}}),
			class: reflect.TypeFor[ir.ServerID](),
			path:  "doc.Services[0].Servers[0]",
		},
		{
			name:  "map key",
			doc:   emptyRefService(ir.Service{Renames: map[ir.TypeID]ir.Naming{"": {Source: "x", Canonical: "x"}}}),
			class: reflect.TypeFor[ir.TypeID](),
			path:  "doc.Services[0].Renames[].key",
		},
		{
			name: "map value",
			doc: emptyRefService(ir.Service{Groups: []ir.OperationGroup{{
				Resource: &ir.ResourceInfo{Lifecycle: map[string]ir.OpID{"read": ""}},
			}}}),
			class: reflect.TypeFor[ir.OpID](),
			path:  "doc.Services[0].Groups[0].Resource.Lifecycle[read]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := emptyReferencesOf(tc.doc)
			require.Len(t, got, 1)
			assert.Equal(t, tc.class, got[0].Class)
			assert.Equal(t, tc.path, got[0].Path)
			assert.False(t, got[0].Locator, "only a discriminator's tag property is a locator")
		})
	}
}

// TestEmptyReferences_ADiscriminatorLocatingNothingIsALocator pins the one
// position whose empty reference is told apart: the tag property of a
// discriminator that names no property, no wire name and no index.
func TestEmptyReferences_ADiscriminatorLocatingNothingIsALocator(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/Base": &ir.Model{ID: "t/x/Base", Discriminator: &ir.Discriminator{}}}}

	got := emptyReferencesOf(doc)

	require.Len(t, got, 1)
	assert.Equal(t, reflect.TypeFor[ir.PropID](), got[0].Class)
	assert.Equal(t, "doc.Types[t/x/Base].Discriminator.Property", got[0].Path)
	assert.True(t, got[0].Locator)
}

// TestEmptyReferences_ADocumentedEmptyIsNone holds the positions whose comments
// document an empty value as their own spelling of "none": a discriminator's
// default, an idempotency token parameter, and a tag property another field
// locates. A sound document, with a reference at each, yields nothing too.
func TestEmptyReferences_ADocumentedEmptyIsNone(t *testing.T) {
	t.Parallel()
	index := 0
	for _, tc := range []struct {
		name string
		doc  *ir.Document
	}{
		{
			name: "Discriminator.Default",
			doc: &ir.Document{Types: ir.TypeRegistry{"t/x/Base": &ir.Model{ID: "t/x/Base", Discriminator: &ir.Discriminator{
				PropertyName: "k", Default: "",
			}}}},
		},
		{
			name: "Discriminator.Property located by PropertyName",
			doc: &ir.Document{Types: ir.TypeRegistry{"t/x/Base": &ir.Model{ID: "t/x/Base", Discriminator: &ir.Discriminator{
				PropertyName: "k", Mapping: map[string]ir.TypeID{"a": "t/x/A"},
			}}}},
		},
		{
			name: "Discriminator.Property located by Index",
			doc: &ir.Document{Types: ir.TypeRegistry{"t/x/Base": &ir.Model{ID: "t/x/Base", Discriminator: &ir.Discriminator{
				Index: &index,
			}}}},
		},
		{
			name: "Idempotency.TokenParam",
			doc:  emptyRefOperation(ir.Operation{Idempotency: ir.Idempotency{Kind: ir.IdempotencySafe, TokenParam: ""}}),
		},
		{
			name: "a nil pointer, an empty slice and an empty map",
			doc: emptyRefService(ir.Service{
				Servers: []ir.ServerID{}, Renames: map[ir.TypeID]ir.Naming{},
				Groups: []ir.OperationGroup{{Operations: []ir.Operation{{OverloadOf: nil}}}},
			}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, emptyReferencesOf(tc.doc))
		})
	}
}

// TestEmptyReferences_ListIsInWalkOrder pins that several empty references are
// listed in the order the walk meets them, each with its own path, and that a
// sound reference beside them adds nothing.
func TestEmptyReferences_ListIsInWalkOrder(t *testing.T) {
	t.Parallel()
	doc := emptyRefService(ir.Service{
		Servers: []ir.ServerID{"server/x/a", "", ""},
		Auth:    []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ""}}}},
	})

	first := emptyReferencesOf(doc)
	paths := make([]string, 0, len(first))
	for _, e := range first {
		paths = append(paths, e.Path)
	}

	assert.Len(t, paths, 3)
	assert.Contains(t, paths, "doc.Services[0].Servers[1]")
	assert.Contains(t, paths, "doc.Services[0].Servers[2]")
	assert.Contains(t, paths, "doc.Services[0].Auth[0].Schemes[0].Scheme")
	second := emptyReferencesOf(doc)
	assert.Equal(t, first, second, "a second walk reads the same list")
}
