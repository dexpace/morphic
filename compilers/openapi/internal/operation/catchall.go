// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// settleCatchAlls fills k.catchAll with the encoding of the parts each
// catch-all among the body's shapes admits
// (docs/compilers/openapi.md#multipart-encoding): each holder memberHolders
// gives, read by ir.CatchAllHolders as pass.Validate reads it, keyed by the
// map or model holding the catch-all. A holder whose parts say nothing gets no
// key. A part is read as a declared part with no encoding entry is
// (catchAllReading.part), since OpenAPI keys an Encoding Object by a property's name
// and a catch-all has none.
func (k partKeys) settleCatchAlls(reg ir.TypeRegistry, node func(ir.TypeID) (ir.TypeDef, bool)) {
	holders, _ := memberHolders(node, k.body)
	read := catchAllReading{
		unions:    partUnionsOf(k.c),
		node:      node,
		multipart: isMultipart(k.mediaType),
		rules:     service.EncodingRules(k.c),
		encodedBy: readContentEncoding(k.c, k.catchAllValues),
	}
	for _, h := range holders {
		for _, id := range ir.CatchAllHolders(reg, h) {
			if parts, says := read.parts(reg[id]); says {
				k.catchAll[id] = parts
			}
		}
	}
}

// catchAllReading is what reading one body's catch-all parts shares: the
// run's reading of the unions parts name, the registry's lookup, the Encoding
// Object's rows at the document's minor, whether the body is multipart, and
// what the catch-alls' value schemas write of contentEncoding.
type catchAllReading struct {
	unions    *partUnions
	node      func(ir.TypeID) (ir.TypeDef, bool)
	rules     specrules.EncodingRules
	encodedBy contentEncoding
	multipart bool
}

// parts returns the encoding of the parts holder, a map or a model holding a
// catch-all, admits: its value's, and each pattern's, each only where it says
// something (partEncodingEmpty). says is false where neither does.
func (r catchAllReading) parts(holder ir.TypeDef) (parts ir.CatchAllParts, says bool) {
	switch h := holder.(type) {
	case *ir.MapT:
		parts.Value = r.value(h.Value)
	case *ir.Model:
		if h.AdditionalProps.Value != nil {
			parts.Value = r.value(*h.AdditionalProps.Value)
		}
		for _, p := range h.AdditionalProps.Patterns {
			if pe := r.part(p.Value); !partEncodingEmpty(pe) {
				if parts.Patterns == nil {
					parts.Patterns = map[string]ir.PartEncoding{}
				}
				parts.Patterns[p.Pattern] = pe
			}
		}
	default:
		// ir.CatchAllHolders gives only maps and models.
	}
	return parts, parts.Value != nil || len(parts.Patterns) > 0
}

// value returns the encoding of a part of type ref, nil where it says
// nothing.
func (r catchAllReading) value(ref ir.TypeRef) *ir.PartEncoding {
	pe := r.part(ref)
	if partEncodingEmpty(pe) {
		return nil
	}
	return &pe
}

// part returns the encoding of a part of type ref with no encoding entry: its
// structural flags off its type (partTypeFlags), and the content type OpenAPI
// gives its values by default (withDefaultContentType). Style, Explode and
// Headers are the entry's to state, and a catch-all's part has none.
func (r catchAllReading) part(ref ir.TypeRef) ir.PartEncoding {
	var pe ir.PartEncoding
	pe.Multi, pe.Filename, _ = partTypeFlags(r.unions, r.node, ref, r.multipart)
	return withDefaultContentType(r.unions, r.node, r.rules, &ir.Property{Type: ref}, r.encodedBy, pe)
}
