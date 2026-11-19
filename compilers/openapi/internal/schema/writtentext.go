// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// sameText reports whether a and b, both written in one document, write the
// same JSON, read against one base where it writes a relative $ref: under an
// $id naming another URI, the same spelling resolves elsewhere. A copy in
// another document is read apart: its $refs resolve against that document, and
// its dialect may differ (docs/compilers/openapi.md#openapi-versions). A schema
// placed nowhere, a nil one among them, is written alike with none.
func sameText(c lowering.Ctx, a, b *statement.Statement) bool {
	at, placed := siteOf(a.Schema)
	bt, placedB := siteOf(b.Schema)
	if !placed || !placedB || at.Doc != bt.Doc {
		return false
	}
	text := writtenText(c, a)
	if text == nil || !bytes.Equal(text, writtenText(c, b)) {
		return false
	}
	// The texts are remembered, so comparing them first leaves a base, which
	// walks the document, to the rare pair written alike.
	if !annotation.WritesReference(text) {
		return true
	}
	base, known := c.Bindings().Table().Base(at)
	other, knownB := c.Bindings().Table().Base(bt)
	return known && knownB && base == other
}

// textOf keys what writtenText rendered of one schema.
type textOf struct {
	schema *statement.Statement
}

// writtenText renders js, nil where it cannot be rendered. Every position
// restating a shared schema asks, so it is rendered once a run.
func writtenText(c lowering.Ctx, js *statement.Statement) ir.RawValue {
	return lowering.ReadOncePerRun(c, textOf{js}, func() ir.RawValue {
		raw, err := annotation.RawFromNode(js.Node)
		if err != nil {
			return nil
		}
		return raw
	})
}
