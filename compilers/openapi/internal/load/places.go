// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// places is where the load places a finding at a pointer of the source, which
// source places as the lowering does, or of another document a reference read
// beside it, in that document's own Document.Sources entry. A document no entry
// holds, which a document read for its content always has, is placed at the
// source's root rather than nowhere. table names the documents a position of
// the bindings is in; it is nil before binding.
type places struct {
	source provenance.Doc
	others Others
	table  *bind.Table
}

// site returns where a finding at the reference site s is placed.
func (p places) site(s refSite) ir.Provenance {
	if s.inSource() {
		return p.source.At(s.pointer)
	}
	return p.in(s.doc, s.pointer)
}

// position returns where a finding at the position at of the bindings' table
// is placed.
func (p places) position(at bind.Position) ir.Provenance {
	if at.Doc == bind.Source {
		return p.source.At(at.Pointer)
	}
	return p.in(p.table.Document(at.Doc).Name, at.Pointer)
}

// in returns where pointer of the document a reference reads by name is placed.
func (p places) in(name string, pointer jsontext.Pointer) ir.Provenance {
	o, ok := p.others.Named(name)
	if !ok {
		return p.source.At("")
	}
	return provenance.Doc{Source: o.Index}.At(pointer)
}
