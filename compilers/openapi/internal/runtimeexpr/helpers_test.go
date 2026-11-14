// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr_test

import "github.com/dexpace/morphic/ir"

// member is a property of a model, with the ID id and the wire name wire.
func member(id ir.PropID, wire string, target ir.TypeID) ir.Property {
	return ir.Property{ID: id, WireName: wire, Type: ir.TypeRef{Target: target}}
}

// registry holds a page, whose members are info and items, an info, whose one
// member is next, and a rival of the page, whose own info is another
// declaration of that wire name.
func registry() ir.TypeRegistry {
	return ir.TypeRegistry{
		"t/page": &ir.Model{ID: "t/page", Properties: []ir.Property{
			member("p/info", "info", "t/info"), member("p/items", "items", "t/text"),
		}},
		"t/rival": &ir.Model{ID: "t/rival", Properties: []ir.Property{member("p/rival-info", "info", "t/info")}},
		"t/info":  &ir.Model{ID: "t/info", Properties: []ir.Property{member("p/next", "next", "t/text")}},
		"t/text":  &ir.Scalar{ID: "t/text"},
	}
}

// body is a payload holding contents.
func body(contents ...ir.Content) *ir.Payload { return &ir.Payload{Contents: contents} }

// content is a content of mediaType whose type is the model at target.
func content(mediaType string, target ir.TypeID) ir.Content {
	return ir.Content{MediaType: mediaType, Type: ir.TypeRef{Target: target}}
}
