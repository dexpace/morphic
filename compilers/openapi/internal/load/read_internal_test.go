// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestLoad_ADocumentDeclaringAnotherYAMLVersionIsWarnedInItsEntry holds the
// three warnings a document read as YAML 1.2 draws: a plain scalar and a
// line-break character, in its text and in a comment, where it declares 1.1,
// and the directive where it declares a later minor.
func TestLoad_ADocumentDeclaringAnotherYAMLVersionIsWarnedInItsEntry(t *testing.T) {
	t.Parallel()
	src := "%YAML 1.1\n---\nopenapi: 3.1.0\ninfo: {title: t, version: \"1\", description: \"a\u0085b\"} # c d\n" +
		"paths: {}\nx-a: 0644\n"

	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(src), Options{})

	require.NoError(t, err)
	var got []string
	for _, d := range diags {
		if d.Code == diag.YAMLVersionReading {
			got = append(got, string(d.Provenance.Pointer)+" "+d.Message)
		}
	}
	const tail = "a character in YAML 1.2, which the compiler reads; YAML 1.1, which the document declares, ends a line there"
	assert.ElementsMatch(t, []string{
		"/info/description the scalar holds U+0085, " + tail,
		"/info a comment here holds U+2028, " + tail,
		`/x-a plain scalar "0644" is the integer 644 in YAML 1.2, which the compiler reads; ` +
			"YAML 1.1, which the document declares, reads the integer 420",
	}, got)

	long := "1_" + strings.Repeat("0", 200)
	_, diags, err = Load(t.Context(), 0, openapitest.SourceOf("%YAML 1.1\n---\n"+minimal31+"x-l: "+long+"\n"), Options{})
	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, "a plain scalar of 202 characters is a string in YAML 1.2, which the compiler reads; "+
		"YAML 1.1, which the document declares, reads an integer", diags[0].Message, "worded by kind, unquoted")

	_, diags, err = Load(t.Context(), 0, openapitest.SourceOf("%YAML 1.4\n---\n"+minimal31), Options{})
	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.Position{Line: 1, Column: 1}, diags[0].Provenance.Position)
}

// TestLoad_ASourceIsRefusedAsItIsReadPastTheNodeBudget holds a source past the
// node budget to the refusal its read words: a JSON text stopped where it
// crossed, a YAML text counted whole, neither of them a parse error.
func TestLoad_ASourceIsRefusedAsItIsReadPastTheNodeBudget(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ data, want string }{
		"JSON": {`{"openapi":"3.1.0","info":{"title":"t","version":"1"},"paths":{}}`,
			"source document holds more than 3 nodes, the node budget; reading stopped there"},
		"YAML": {minimal31, "source document parses to "},
	} {
		doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "s", Data: []byte(tc.data)},
			Options{MaxSourceNodes: budget.AtMost(3)})

		require.NoError(t, err, name)
		assert.Nil(t, doc, name)
		require.Len(t, diags, 1, name)
		assert.Contains(t, diags[0].Message, tc.want, name)
	}
	_, past := OverNodeBudget(ir.Provenance{}, errors.New("no budget"))
	assert.False(t, past, "an error of another kind is no refusal")
}

// TestLoad_AStreamOfEmptyDocumentsPastTheBoundIsReported holds a tail cut at
// the bound with no document counted holding content to its own wording.
func TestLoad_AStreamOfEmptyDocumentsPastTheBoundIsReported(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0,
		openapitest.SourceOf(minimal31+strings.Repeat("---\n", 1025)+"---\nfoo: bar\n"), Options{})

	require.NoError(t, err)
	got := streamDiags(diags)
	require.Len(t, got, 1)
	assert.Equal(t, "only one document of the YAML stream was lowered; "+
		"the 1024 after it hold no content, and what follows them was not read", got[0].Message)
}

// TestExternal_AJSONDocumentPastTheNodeBudgetIsRefusedAsItIsRead holds a
// document a reference reads to the budget as it is read, with the reason the
// read words.
func TestExternal_AJSONDocumentPastTheNodeBudgetIsRefusedAsItIsRead(t *testing.T) {
	t.Parallel()
	e := newExternal(Options{MaxSourceNodes: budget.AtMost(2)}, newExternalReads(sourceDocument{}, budget.Unbounded()))

	_, err := e.prepare(t.Context(), "doc.json", "doc.json", []byte(`{"a":[1,2,3]}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "it holds more than 2 nodes, the node budget")
}
