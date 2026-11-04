// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// longKeysDeep is a schema nesting n objects, each holding the next as its one
// property, under a key of k bytes, written explicit as YAML's implicit keys
// stop at 1024 characters: its nodes' pointers grow with n times k, and the
// document only with their sum (GitHub #931).
func longKeysDeep(n, k int) string {
	key := strings.Repeat("p", k)
	return strings.Repeat("{type: object, properties: {? "+key+" : ", n) + "{type: string}" + strings.Repeat("}}", n)
}

// TestLoad_RefusesLongKeysNestedDeepBeforeBuildingTheModel pins GitHub #931: a
// document whose nodes' pointers total past the allowance its size gives is
// refused as one pointer-amplification error at the document, before its
// model is built and its pointers spelled, gigabytes of them. A shallower one
// of the same keys loads.
func TestLoad_RefusesLongKeysNestedDeepBeforeBuildingTheModel(t *testing.T) {
	t.Parallel()
	spec := func(n int) string {
		return minimal31 + "components:\n  schemas:\n    A: " + longKeysDeep(n, 10000) + "\n"
	}
	got, diags, err := Load(t.Context(), 4, openapitest.SourceOf(spec(20)), Options{})
	require.NoError(t, err)
	require.NotNil(t, got, "a document within the allowance loads: %+v", diags)
	assert.Zero(t, countErrorsAt(diags, diag.PointerAmplification))

	got, diags, err = Load(t.Context(), 4, openapitest.SourceOf(spec(60)), Options{})
	require.NoError(t, err, "a document too costly to name is a spec problem, not a Go error")
	assert.Nil(t, got, "nothing is built")
	require.Len(t, diags, 1)
	d := diags[0]
	assert.Equal(t, diag.PointerAmplification, d.Code)
	assert.Equal(t, ir.SeverityError, d.Severity)
	assert.Equal(t, ir.Provenance{Source: 4}, d.Provenance, "at the document")
	assert.Contains(t, d.Message, "the pointers naming this document's nodes total ")
}

// TestLoad_ReportsAShapeOnceItsNestingRefuses pins that a document refused for
// its nesting draws no pointer-amplification error beside it, though its
// pointers pass their allowance too: the nesting multiplies both, and one
// defect is reported once.
func TestLoad_ReportsAShapeOnceItsNestingRefuses(t *testing.T) {
	t.Parallel()
	src := minimal31 + "components:\n  schemas:\n    A: " + longKeysDeep(scan.MaxNesting/2, 2000) + "\n"
	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(src), Options{})
	require.NoError(t, err)
	assert.Nil(t, got)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.SourceTooDeep, diags[0].Code)
}

// TestLoad_RefusesAnOverlaySpellingPastItsAllowance pins that an overlay is held
// to the pointer bound as the source is, before the library applies it, and
// refused at its own root.
func TestLoad_RefusesAnOverlaySpellingPastItsAllowance(t *testing.T) {
	t.Parallel()
	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31),
		overlayOptions("  - target: $.info\n    update: {x-deep: "+longKeysDeep(60, 10000)+"}\n"))
	require.NoError(t, err)
	assert.Nil(t, got)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.PointerAmplification, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 1}, diags[0].Provenance, "the overlay is the document past its allowance")
}

// TestExternal_AdmitRefusesPointersPastTheAllowance pins that a document an
// external reference names is held to the source's pointer bound, its finding
// the reason the reference fails.
func TestExternal_AdmitRefusesPointersPastTheAllowance(t *testing.T) {
	t.Parallel()
	root, err := parseTree(t.Context(), []byte("a: "+longKeysDeep(60, 10000)+"\n"))
	require.NoError(t, err)

	err = newExternal(t.Context(), &soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), "the pointers naming this document's nodes total ")
}
