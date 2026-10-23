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
	differs := map[string]func(d *ir.Diagnostic){
		"severity":        func(d *ir.Diagnostic) { d.Severity = ir.SeverityInfo },
		"code":            func(d *ir.Diagnostic) { d.Code = "x/z" },
		"message":         func(d *ir.Diagnostic) { d.Message = "other" },
		"source":          func(d *ir.Diagnostic) { d.Provenance.Source = 1 },
		"pointer":         func(d *ir.Diagnostic) { d.Provenance.Pointer = "/q" },
		"inferredBy":      func(d *ir.Diagnostic) { d.Provenance.Inferred = "guess" },
		"position line":   func(d *ir.Diagnostic) { d.Provenance.Position.Line = 5 },
		"position column": func(d *ir.Diagnostic) { d.Provenance.Position.Column = 5 },
		"node":            func(d *ir.Diagnostic) { d.Provenance.Node = "op/x" },
	}
	for name, mutate := range differs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var d compile.Diags
			d.Append(base)
			d.Append(base)
			require.Equal(t, 1, d.Len(), "an identical repeat collapses")

			other := base
			mutate(&other)
			d.Append(other)
			assert.Equal(t, 2, d.Len(), "a diagnostic differing in %s is a distinct finding", name)
		})
	}
}

func TestDiags_AppendAllPreservesFirstSeenOrder(t *testing.T) {
	t.Parallel()
	mk := func(code string) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityError, Code: code}
	}
	var d compile.Diags
	d.AppendAll([]ir.Diagnostic{mk("a"), mk("b"), mk("a"), mk("c")})

	got := make([]string, 0, d.Len())
	for _, x := range d.List() {
		got = append(got, x.Code)
	}
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

func TestDiags_ZeroValueIsUsable(t *testing.T) {
	t.Parallel()
	var d compile.Diags
	assert.Zero(t, d.Len())
	assert.Empty(t, d.List())
}
