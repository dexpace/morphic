// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// TestDiags_DedupesByFullIdentity pins that the collapse is by identity and
// nothing coarser: two findings differing in any one field both survive.
func TestDiags_DedupesByFullIdentity(t *testing.T) {
	t.Parallel()
	base := ir.Diagnostic{
		Severity:   ir.SeverityError,
		Code:       "x/y",
		Message:    "m",
		Provenance: ir.Provenance{Source: 0, Pointer: "/p"},
	}
	differs := map[string]func(diagnostic *ir.Diagnostic){
		"severity":        func(diagnostic *ir.Diagnostic) { diagnostic.Severity = ir.SeverityInfo },
		"code":            func(diagnostic *ir.Diagnostic) { diagnostic.Code = "x/z" },
		"message":         func(diagnostic *ir.Diagnostic) { diagnostic.Message = "other" },
		"source":          func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Source = 1 },
		"pointer":         func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Pointer = "/q" },
		"inferredBy":      func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Inferred = "guess" },
		"position line":   func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Position.Line = 5 },
		"position column": func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Position.Column = 5 },
		"node":            func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Node = "operation/x" },
	}
	for name, mutate := range differs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var diags compile.Diags
			diags.Append(base)
			diags.Append(base)
			require.Len(t, diags.List(), 1, "an identical repeat collapses")

			other := base
			mutate(&other)
			diags.Append(other)
			assert.Len(t, diags.List(), 2, "a diagnostic differing in %s is a distinct finding", name)
		})
	}
}

func TestDiags_AppendAllPreservesFirstSeenOrder(t *testing.T) {
	t.Parallel()
	mk := func(code string) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityError, Code: code}
	}
	var diags compile.Diags
	diags.AppendAll([]ir.Diagnostic{mk("a"), mk("b"), mk("a"), mk("c")})

	got := make([]string, 0, len(diags.List()))
	for _, diagnostic := range diags.List() {
		got = append(got, diagnostic.Code)
	}
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

func TestDiags_ZeroValueIsUsable(t *testing.T) {
	t.Parallel()
	var diags compile.Diags
	assert.Empty(t, diags.List())
}
