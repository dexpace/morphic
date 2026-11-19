// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// repeatsAt returns the pointer and position of each warning CheckRepeatedKeys
// draws over text, read as version, with the locator opts names.
func repeatsAt(t *testing.T, version, text string, opts specrules.Options) []string {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &root))
	found := specrules.CheckRepeatedKeys(t.Context(), &root, version, opts)
	var out []string
	if len(found) > 0 {
		out = make([]string, 0, len(found))
	}
	for _, d := range found {
		assert.Equal(t, ir.SeverityWarning, d.Severity)
		assert.Equal(t, diag.Validation+"/validation-duplicate-key", d.Code)
		out = append(out, fmt.Sprintf("%s@%d:%d", d.Provenance.Pointer, d.Provenance.Position.Line, d.Provenance.Position.Column))
	}
	return out
}

// TestCheckRepeatedKeys_ReportsEachKeyAMappingWritesAgainAtItsCopy holds every
// mapping of a tree, whatever holds it, to the rule that a key is written
// once: the warning is at the second copy alone, however many follow, with the
// pointer of the key and its line and column.
func TestCheckRepeatedKeys_ReportsEachKeyAMappingWritesAgainAtItsCopy(t *testing.T) {
	t.Parallel()
	locate := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"at the root", "a: 1\na: 2\n", []string{"/a@2:1"}},
		{"a third copy is not reported again", "a: 1\na: 2\na: 3\n", []string{"/a@2:1"}},
		{"in a mapping an extension holds", "x-e:\n  k: 1\n  k: 2\n", []string{"/x-e/k@3:3"}},
		{"in a mapping a list holds", "l:\n  - {k: 1}\n  - {k: 1, k: 2}\n", []string{"/l/1/k@3:12"}},
		{"no repeated key", "a: 1\nb: 2\n", nil},
		{"a key that is no scalar is no copy", "? [a]\n: 1\n? [a]\n: 2\nb: 1\nb: 2\n", []string{"/b@6:1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := repeatsAt(t, "3.1.0", tc.text, specrules.Options{Locate: locate})
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestCheckRepeatedKeys_SaysNothingWhereItCannotJudge holds the check to no
// finding for a version the table does not cover and for a walk its context
// ends, and to a finding with no placement when no Locate is given.
func TestCheckRepeatedKeys_SaysNothingWhereItCannotJudge(t *testing.T) {
	t.Parallel()
	const text = "a: 1\na: 2\n"
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &root))

	assert.Empty(t, specrules.CheckRepeatedKeys(t.Context(), &root, "swagger-2", specrules.Options{}), "an unknown version")
	var deep yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("l:\n  - {k: 1, k: 2}\n"), &deep))
	assert.Len(t, specrules.CheckRepeatedKeys(t.Context(), &deep, "3.1.0", specrules.Options{}), 1, "uncut, the repeat is found")
	assert.Empty(t, specrules.CheckRepeatedKeys(t.Context(), &deep, "3.1.0", specrules.Options{Budget: budget.AtMost(2)}),
		"a walk its budget cuts before the repeat says nothing of it")
	got := specrules.CheckRepeatedKeys(t.Context(), &root, "3.1.0", specrules.Options{})
	require.Len(t, got, 1, "a missing Locate places the finding nowhere")
	assert.Empty(t, got[0].Provenance.Pointer)
	assert.Equal(t, ir.Position{Line: 2, Column: 1}, got[0].Provenance.Position)
}

// TestCheckRepeatedKeys_ReportsNothingOnceTheContextIsDone holds the check to
// all of its findings or none: a context refused partway through a walk, after
// the first mapping's repeat is found, leaves nothing reported, not the
// findings of the part read.
func TestCheckRepeatedKeys_ReportsNothingOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	text := "a: 1\na: 2\nl: [" + strings.Repeat("1, ", 3*nodeview.AskEvery) + "1]\n"
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &root))
	live := &ctxtest.Countdown{Context: t.Context(), Left: 1 << 30}
	require.Len(t, specrules.CheckRepeatedKeys(live, &root, "3.1.0", specrules.Options{}), 1, "live, the repeat is found")

	partway := &ctxtest.Countdown{Context: t.Context(), Left: 1}
	assert.Empty(t, specrules.CheckRepeatedKeys(partway, &root, "3.1.0", specrules.Options{}))
	assert.True(t, partway.Refused(), "the walk was cut by the context")
}
