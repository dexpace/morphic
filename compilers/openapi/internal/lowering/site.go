// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// Site is one document of a compile as a lowering places and names what it
// reads there: the Document.Sources entry its positions are recorded in, how
// the IDs of what it declares are qualified, and its number in the bindings'
// table. A context holds the site of the document whose content it reads,
// which InDocument alone sets, so no lowering spells a source index.
type Site struct {
	place provenance.Doc
	ids   ids.Document
	doc   bind.DocID
}

// Place returns where the site places a pointer of its document: in another
// document's own Document.Sources entry, and otherwise in the source's, the
// overlay named as its writer for a position the overlay introduced or
// rewrote.
//
// Every lowering places a pointer through it, entities and diagnostics alike
// (GitHub #86), so an overlay's contribution is traced without touching any
// of them.
func (s Site) Place() provenance.Doc { return s.place }

// DiagAt builds one diagnostic at pointer of the site's document, placed as
// Place places it.
//
// It returns rather than records: GitHub #86 wants provenance built in one
// place, because hand-writing it is how a diagnostic shipped with none (GitHub
// #43), and micro-compiler-design §4 wants diagnostics returned rather than
// accumulated through a handle. A constructor that stamps and hands back
// satisfies both.
func (s Site) DiagAt(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) ir.Diagnostic {
	return diag.Newf(sev, code, s.place.At(pointer), format, args...)
}

// IDs returns how the IDs of what the site's document declares name it.
func (s Site) IDs() ids.Document { return s.ids }

// Position returns where pointer of the site's document is in the bindings'
// table.
func (s Site) Position(pointer jsontext.Pointer) bind.Position {
	return bind.Position{Doc: s.doc, Pointer: pointer}
}
