// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestMissingAssertions_AnOptionalInterfaceIsHeldForUnexportedTypesToo plants
// each shape of a type with the methods of ir.CycleVisitor, which a walk tells
// of cycles only if the type is one: a method renamed in a type that meant to
// be one still compiles, and the walk stops telling it. Each such type, exported
// or not, must carry the var that fails the build when the method goes.
func TestMissingAssertions_AnOptionalInterfaceIsHeldForUnexportedTypesToo(t *testing.T) {
	const head = `package pass

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

`
	const tail = "\nvar (\n\t_ reflect.Value\n\t_ ir.WalkPath\n)\n"
	const visit = "Visit(reflect.Value, ir.WalkPath) bool { return true }\n"
	const cycle = "Cycle(_, _ ir.WalkPath, _ reflect.Type) {}\n"
	const file = "zz_planted_cycle.go"
	const line = 9
	tests := []struct {
		name string
		src  string
		want []string // the lines reported, each after "<type> implements "
	}{
		{"an unexported type, unasserted",
			"type planted struct{}\n\nfunc (planted) " + visit + "\nfunc (planted) " + cycle,
			[]string{"pass.planted implements ir.CycleVisitor"}},
		{"an unexported type asserted as a visitor alone",
			"type planted struct{}\n\nfunc (planted) " + visit + "\nfunc (planted) " + cycle + "\nvar _ ir.Visitor = planted{}\n",
			[]string{"pass.planted implements ir.CycleVisitor"}},
		{"an exported type asserted as a visitor alone",
			"type P struct{}\n\nfunc (P) " + visit + "\nfunc (P) " + cycle + "\nvar _ ir.Visitor = P{}\n",
			[]string{"pass.P implements ir.CycleVisitor"}},
		{"an unexported type asserted",
			"type planted struct{}\n\nfunc (planted) " + visit + "\nfunc (planted) " + cycle + "\nvar _ ir.CycleVisitor = planted{}\n", nil},
		{"an unexported type asserted through a pointer",
			"type planted struct{}\n\nfunc (*planted) " + visit + "\nfunc (*planted) " + cycle +
				"\nvar _ ir.CycleVisitor = (*planted)(nil)\n", nil},
		{"an unexported visitor that is told of no cycle",
			"type planted struct{}\n\nfunc (planted) " + visit, nil},
		{"an unexported type whose cycle method has another signature",
			"type planted struct{}\n\nfunc (planted) " + visit + "\nfunc (planted) Cycle() {}\n", nil},
		{"a generic type, unasserted",
			"type planted[T any] struct{}\n\nfunc (planted[T]) " + visit + "\nfunc (planted[T]) " + cycle,
			[]string{"pass.planted implements ir.CycleVisitor"}},
		{"a generic type asserted through an instance",
			"type planted[T any] struct{}\n\nfunc (planted[T]) " + visit + "\nfunc (planted[T]) " + cycle +
				"\nvar _ ir.CycleVisitor = planted[int]{}\n", nil},
		{"a generic type of pointer receivers, unasserted",
			"type planted[T any] struct{}\n\nfunc (*planted[T]) " + visit + "\nfunc (*planted[T]) " + cycle,
			[]string{"pass.planted implements ir.CycleVisitor"}},
	}
	root := irtest.RepoRoot(t)
	base, err := decls.Load(module + "/pass")
	require.NoError(t, err)
	for _, tc := range tests {
		// Not in parallel: go/types can race when two checks share an
		// import (golang/go#81122).
		t.Run(tc.name, func(t *testing.T) {
			p := checkPlanted(t, base, filepath.Join(root, "pass", file), head+tc.src+tail)
			got, _ := missingAssertions(p, root)
			var want []string
			for _, what := range tc.want {
				want = append(want, fmt.Sprintf("pass/%s:%d: %s with no compile-time assertion in its package", file, line, what))
			}
			assert.Empty(t, cmp.Diff(want, got), "mismatch (-want +got)")
		})
	}
}
