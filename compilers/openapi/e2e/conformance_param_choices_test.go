// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// choicesKey is the extension that states alternatives among parameters, and
// choicesPointer where param-choices writes it.
const (
	choicesKey     = "x-mutually-exclusive-parameter-groups"
	choicesPointer = "/paths/~1resources/get/" + choicesKey
)

// choiceParamID is the ID of the query parameter of listResources called name.
func choiceParamID(name string) ir.ParameterID {
	return ir.ParameterID("parameter/openapi/paths/~1resources/get/parameters/" + name + "/query")
}

// choiceNaming is the naming of a choice or variant written name, a
// lower-case word with underscores: its canonical form is its source.
func choiceNaming(name string) ir.Naming { return ir.Naming{Source: name, Canonical: name} }

// paramChoicesFilled is what listResources states once param-choices is read:
// the choices sorted by their source names, as are their variants, and each
// variant's parameters in the order its list writes them.
func paramChoicesFilled() []ir.ParamChoice {
	return []ir.ParamChoice{
		{
			Name: choiceNaming("cursor"),
			Variants: []ir.ParamVariant{
				{Name: choiceNaming("backward"), Params: []ir.ParameterID{choiceParamID("before")}},
				{Name: choiceNaming("forward"), Params: []ir.ParameterID{choiceParamID("after")}},
			},
		},
		{
			Name:     choiceNaming("parent"),
			Required: true,
			Variants: []ir.ParamVariant{
				{Name: choiceNaming("by_external_id"), Params: []ir.ParameterID{
					choiceParamID("parent_slug"), choiceParamID("parent_external_id"),
				}},
				{Name: choiceNaming("by_id"), Params: []ir.ParameterID{choiceParamID("parent_id")}},
			},
		},
	}
}

// listResources returns the document's one operation.
func listResources(t *testing.T, doc *ir.Document) ir.Operation {
	t.Helper()
	return linkOperation(t, doc, "listResources")
}

// declaredChoiceParams is the five parameters of listResources in the order
// the fixture declares them.
func declaredChoiceParams() []ir.ParameterID {
	return []ir.ParameterID{
		choiceParamID("parent_id"), choiceParamID("parent_slug"), choiceParamID("parent_external_id"),
		choiceParamID("before"), choiceParamID("after"),
	}
}

// operationParamIDs lists the IDs of operation's parameters in order.
func operationParamIDs(operation ir.Operation) []ir.ParameterID {
	ids := make([]ir.ParameterID, 0, len(operation.Params))
	for _, parameter := range operation.Params {
		ids = append(ids, parameter.ID)
	}
	return ids
}

// assertParamChoices is the matrix's exclusive-parameters row: the operation
// states two choices among its five parameters, which stay declared one by
// one, the operation records that a promotion filled them, and the key they
// were read from is kept as written.
func assertParamChoices(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	assert.Empty(t, diags, "a choice the convention writes well draws no finding")
	operation := listResources(t, doc)

	assert.Equal(t, paramChoicesFilled(), operation.ParamChoices)
	assert.Equal(t, ir.Inferred("extension-promotion"), operation.Provenance.Inferred)
	assert.Equal(t, declaredChoiceParams(), operationParamIDs(operation),
		"the parameters stay the operation's, in the order they are declared")

	entry, kept := operation.Unmodeled["openapi:"+choicesKey]
	require.True(t, kept, "the key is kept whatever is filled from it")
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
	assert.Equal(t, choicesPointer, string(entry.Provenance.Pointer))
}

// TestParamChoices_ConformanceReadsTheSameInEitherOrder compiles param-choices
// with every mapping's entries reversed, the key's choices and variants
// among them, and holds it to the same facts: the choices come out sorted by
// their source names whichever order the mapping is written in.
func TestParamChoices_ConformanceReadsTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(conformanceDir, "param-choices.yaml"))
	require.NoError(t, err)
	doc, diags := parseFull(t, string(reversed(t, string(src))))
	assertParamChoices(t, doc, diags)
}
