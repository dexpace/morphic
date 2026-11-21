// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr_test

import (
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// member is a property of a model, with the ID id and the wire name wire.
func member(id ir.PropertyID, wire string, target ir.TypeID) ir.Property {
	return ir.Property{ID: id, WireName: wire, Type: ir.TypeRef{Target: target}}
}

// registry holds a page, whose members are info and items, an info, whose one
// member is next, a rival of the page, whose own info is another declaration of
// that wire name, and an alias of the page, and of the rival, each a scalar.
func registry() ir.TypeRegistry {
	return ir.TypeRegistry{
		"type/page": &ir.Model{ID: "type/page", Properties: []ir.Property{
			member("property/info", "info", "type/info"), member("property/items", "items", "type/text"),
		}},
		"type/rival":      &ir.Model{ID: "type/rival", Properties: []ir.Property{member("property/rival-info", "info", "type/info")}},
		"type/info":       &ir.Model{ID: "type/info", Properties: []ir.Property{member("property/next", "next", "type/text")}},
		"type/text":       &ir.Scalar{ID: "type/text"},
		"type/pagealias":  irtest.ScalarOf("type/pagealias", "type/page"),
		"type/pagealias2": irtest.ScalarOf("type/pagealias2", "type/pagealias"),
		"type/rivalalias": irtest.ScalarOf("type/rivalalias", "type/rival"),
	}
}

// body is a payload holding contents.
func body(contents ...ir.Content) *ir.Payload { return &ir.Payload{Contents: contents} }

// content is a content of mediaType whose type is the model at target.
func content(mediaType string, target ir.TypeID) ir.Content {
	return ir.Content{MediaType: mediaType, Type: ir.TypeRef{Target: target}}
}
