// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// duplicateKey is the code of a key a mapping writes twice.
const duplicateKey = diag.Validation + "/validation-duplicate-key"

// otherDocDiags compiles main.yaml with the files beside it, read from disk.
func otherDocDiags(t *testing.T, files map[string]string) []ir.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	layout(t, dir, files)
	_, diags := compileDir(t, dir, openapi.Options{})
	return diags
}

// TestCompile_AKeyWrittenTwiceInAnotherDocumentIsReported holds a key written
// twice in a mapping on the way to what a reference reaches in another document
// to a warning in that document's entry, at the repeat, whether the target is a
// schema or a path item.
func TestCompile_AKeyWrittenTwiceInAnotherDocumentIsReported(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		files map[string]string
		key   string
		at    ir.Position
	}{
		"a schema": {
			files: map[string]string{
				"main.yaml": "openapi: 3.1.0\ninfo: {title: M, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A: {$ref: \"s.yaml#/S\"}\n",
				"s.yaml":    "S: {type: string}\nS: {type: integer}\n",
			},
			key: "S", at: ir.Position{Line: 2, Column: 1},
		},
		"a path item": {
			files: map[string]string{
				"main.yaml":  "openapi: 3.1.0\ninfo: {title: M, version: \"1\"}\npaths:\n  /a: {$ref: \"items.yaml#/B\"}\n",
				"items.yaml": "B:\n  get:\n    operationId: first\n    responses: {\"200\": {description: OK}}\nB:\n  get:\n    operationId: second\n    responses: {\"200\": {description: OK}}\n",
			},
			key: "B", at: ir.Position{Line: 5, Column: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			diags := otherDocDiags(t, tc.files)

			dups := byCode(diags, duplicateKey)
			require.Len(t, dups, 1, "%+v", diags)
			assert.Equal(t, ir.SeverityWarning, dups[0].Severity)
			assert.NotZero(t, dups[0].Provenance.Source, "in the other document's entry")
			assert.Equal(t, tc.at, dups[0].Provenance.Position)
			assert.Equal(t, `a mapping writes the key "`+tc.key+`" more than once, and only the last copy is read in OpenAPI 3.1 (Format)`,
				dups[0].Message)
		})
	}
}

// TestCompile_AKeyWrittenTwiceInsideAReachedObjectIsReportedOnce holds a key an
// object a reference reaches writes twice to the one finding the object's check
// makes, in its wording, and not a second from the whole-document check.
func TestCompile_AKeyWrittenTwiceInsideAReachedObjectIsReportedOnce(t *testing.T) {
	t.Parallel()

	diags := otherDocDiags(t, map[string]string{
		"main.yaml": "openapi: 3.1.0\ninfo: {title: M, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A: {$ref: \"s.yaml#/S\"}\n",
		"s.yaml":    "S: {type: string, type: integer}\n",
	})

	dups := byCode(diags, duplicateKey)
	require.Len(t, dups, 1, "%+v", diags)
	assert.Contains(t, dups[0].Message, `Schema Object writes the key "type" more than once`)
	assert.NotContains(t, dups[0].Message, "a mapping writes the key")
}

// TestCompile_AKeyThatIsNoScalarInContentNoReferenceReachesIsNotReported holds
// the whole-document check of another document to the one rule it adds: a key
// that is no scalar in a part of it no reference reaches fails no compile.
func TestCompile_AKeyThatIsNoScalarInContentNoReferenceReachesIsNotReported(t *testing.T) {
	t.Parallel()

	diags := otherDocDiags(t, map[string]string{
		"main.yaml": "openapi: 3.1.0\ninfo: {title: M, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A: {$ref: \"s.yaml#/S\"}\n",
		"s.yaml":    "S: {type: string}\nunused:\n  ? [a]\n  : 1\n",
	})

	assert.Empty(t, diags)
}
