// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// filledDoc returns a document with every field of its own filled, holding the
// nodes treeDoc and registriesDoc hold and a type. Each call builds new nodes.
func filledDoc(t *testing.T) *ir.Document {
	t.Helper()
	doc := &ir.Document{}
	irtest.Fill(t, doc)
	doc.IRVersion = ir.IRVersion
	doc.Diagnostics = []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityWarning, "x/held", "held", ir.Provenance{})}
	reg := registriesDoc()
	doc.Services, doc.Servers, doc.Channels = treeDoc().Services, reg.Servers, reg.Channels
	doc.Messages, doc.Auth = reg.Messages, reg.Auth
	doc.Types = ir.TypeRegistry{"t/a": &ir.Any{ID: "t/a"}}
	return doc
}

// keyedFields are the fields of a Document holding nodes an edit changes by
// their own IDs, which UpdateDocument leaves out of the copy it hands over.
var keyedFields = []string{"Services", "Types", "Servers", "Channels", "Messages", "Auth"}

// TestEditor_KeyedFieldsAreEveryRegistry holds keyedFields, and so what the
// tests below hold UpdateDocument to leaving out, to the document's services
// and every registry its shape derives: a registry added to Document fails
// here until the edit reaches it.
func TestEditor_KeyedFieldsAreEveryRegistry(t *testing.T) {
	t.Parallel()
	classes := ir.KeyedClasses()
	got := []string{"Services"}
	for f := range reflect.TypeFor[ir.Document]().Fields() {
		if f.Type.Kind() == reflect.Map && classes[f.Type.Key()] {
			got = append(got, f.Name)
		}
	}
	assert.ElementsMatch(t, got, keyedFields)
}

// TestEditor_UpdateDocumentChangesItsOwnFields pins UpdateDocument: the change
// is handed every field of the document's own, the keyed parts left out, in a
// copy sharing nothing with the edited document, and what it leaves is held
// beside the keyed parts as the edit holds them.
func TestEditor_UpdateDocumentChangesItsOwnFields(t *testing.T) {
	t.Parallel()
	doc := filledDoc(t)
	before := irtest.Encoded(t, doc)

	e := ir.NewEditor(doc)
	e.RemoveOperation("o/a1")
	var handed map[uintptr]string
	e.UpdateDocument(func(d *ir.Document) {
		handed = references(d)
		for _, field := range keyedFields {
			assert.True(t, reflect.ValueOf(d).Elem().FieldByName(field).IsNil(), "the copy holds %s", field)
		}
		require.Len(t, d.TagDefs, 1)
		require.NotNil(t, d.Contact)
		d.Name = "renamed"
		d.TagDefs = nil
		d.Contact.Name = "changed"
		d.Versions = append(d.Versions, "v2")
	})
	e.RemoveChannel("c/b")
	out, diags := e.Finish()

	require.Empty(t, diags)
	assert.Equal(t, "renamed", out.Name)
	assert.Empty(t, out.TagDefs)
	assert.Equal(t, "changed", out.Contact.Name)
	assert.Equal(t, append(slices.Clip(doc.Versions), "v2"), out.Versions)
	assert.Equal(t, "s/a[g/a(o/a2 o/a3)[g/a/n(o/n)] g/b(o/b)] s/b[g/c(o/c)]", render(out))
	assert.NotContains(t, out.Channels, ir.ChannelID("c/b"))
	assert.Same(t, doc.Types["t/a"], out.Types["t/a"])
	assert.Equal(t, doc.Diagnostics, out.Diagnostics)
	assert.Equal(t, before, irtest.Encoded(t, doc), "the edited document changed")

	original := references(doc)
	require.NotEmpty(t, handed)
	for addr, path := range handed {
		if at, shared := original[addr]; shared {
			t.Errorf("the copy's %s is the edited document's %s", path, at)
		}
	}
}

// TestEditor_UpdateDocumentKeepsWhatItLeavesOut pins that the fields an update
// leaves out of its copy are back beside what it changed, in an edit that wrote
// none of them, so nothing but the update rebuilds them.
func TestEditor_UpdateDocumentKeepsWhatItLeavesOut(t *testing.T) {
	t.Parallel()
	doc := filledDoc(t)
	e := ir.NewEditor(doc)
	e.UpdateDocument(func(d *ir.Document) { d.Name = "renamed" })
	out, diags := e.Finish()

	require.Empty(t, diags)
	assert.Equal(t, "renamed", out.Name)
	for _, field := range keyedFields {
		got, want := reflect.ValueOf(out).Elem().FieldByName(field), reflect.ValueOf(doc).Elem().FieldByName(field)
		assert.Equal(t, want.Pointer(), got.Pointer(), "the update lost or copied %s", field)
	}
}

// TestEditor_UpdateDocumentRefusesSettingAKeyedPart pins that a change setting
// one of the fields UpdateDocument leaves out is refused whole: the edit's
// document keeps the field as the edit holds it, and the change's other
// writes too are dropped.
func TestEditor_UpdateDocumentRefusesSettingAKeyedPart(t *testing.T) {
	t.Parallel()
	for _, field := range keyedFields {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			doc := filledDoc(t)
			e := ir.NewEditor(doc)
			e.UpdateDocument(func(d *ir.Document) {
				d.Name = "renamed"
				f := reflect.ValueOf(d).Elem().FieldByName(field)
				f.Set(reflect.ValueOf(filledDoc(t)).Elem().FieldByName(field))
			})
			out, diags := e.Finish()

			assert.Equal(t, []string{"internal: edit: update rejected: the change of the document set its " + field +
				", whose nodes the edit changes by their own IDs"}, refusals(t, diags))
			assert.Equal(t, irtest.Encoded(t, doc), irtest.Encoded(t, out))
		})
	}
}

// TestEditor_UpdateDocumentRefusesNoChange pins that UpdateDocument with no
// change is refused, and the document left as it was.
func TestEditor_UpdateDocumentRefusesNoChange(t *testing.T) {
	t.Parallel()
	doc := filledDoc(t)
	e := ir.NewEditor(doc)
	e.UpdateDocument(nil)
	out, diags := e.Finish()

	assert.Equal(t, []string{"internal: edit: update rejected: no change for the document"}, refusals(t, diags))
	assert.Equal(t, irtest.Encoded(t, doc), irtest.Encoded(t, out))
}
