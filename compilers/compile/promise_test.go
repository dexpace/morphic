// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compile_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// builds returns a promise build that records its coordinate in order, interns
// a model there, and reports a diagnostic coded by it.
func builds(types *compile.Types, order *[]string, coordinate string) func() []ir.Diagnostic {
	return func() []ir.Diagnostic {
		*order = append(*order, coordinate)
		id := ir.TypeID("t/anon" + coordinate)
		types.Intern(coordinate, id, func() ir.TypeDef { return &ir.Model{ID: id} })
		return []ir.Diagnostic{{Code: coordinate}}
	}
}

// TestTypes_FulfilBuildsWhatNoLoweringBuiltInCoordinateOrder pins what a
// compiler relies on: a promised coordinate a lowering interned is left to
// that lowering, and every other is built once, in coordinate order whatever
// the order promised, with what each build reports returned in that order.
func TestTypes_FulfilBuildsWhatNoLoweringBuiltInCoordinateOrder(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	var order []string
	for _, coordinate := range []string{"/c", "/a", "/owned", "/b"} {
		types.Promise(coordinate, builds(types, &order, coordinate))
	}
	types.Intern("/owned", "t/anon/owned", func() ir.TypeDef { return &ir.Model{ID: "t/anon/owned"} })

	diags := types.Fulfil(t.Context())

	assert.Equal(t, []string{"/a", "/b", "/c"}, order, "the owned coordinate is its owner's")
	require.Len(t, diags, 3)
	assert.Equal(t, "/a", diags[0].Code)
	assert.Equal(t, "/c", diags[2].Code)
	for _, coordinate := range []string{"/a", "/b", "/c", "/owned"} {
		_, built := types.NodeAt(coordinate)
		assert.True(t, built, coordinate)
	}
	assert.Empty(t, types.Violations())
}

// TestTypes_FulfilBuildsWhatABuildPromisesInALaterRound pins that a build's
// own promises wait for the round after it, so which coordinate a build
// reached first decides no other build's turn: "/0" sorts before "/b", and is
// still built after it, while one a build interned itself is skipped.
func TestTypes_FulfilBuildsWhatABuildPromisesInALaterRound(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	var order []string
	types.Promise("/b", func() []ir.Diagnostic {
		order = append(order, "/b")
		types.Intern("/b", "t/anon/b", func() ir.TypeDef { return &ir.Model{ID: "t/anon/b"} })
		types.Intern("/b/x", "t/anon/b/x", func() ir.TypeDef { return &ir.Model{ID: "t/anon/b/x"} })
		types.Promise("/0", builds(types, &order, "/0"))
		types.Promise("/b/x", builds(types, &order, "/b/x"))
		return nil
	})

	types.Fulfil(t.Context())

	assert.Equal(t, []string{"/b", "/0"}, order)
	assert.Empty(t, types.Violations())
}

// TestTypes_PromiseKeepsTheFirstBuild pins that a coordinate promised again
// is queued once: its first build is the one taken, and it runs once.
func TestTypes_PromiseKeepsTheFirstBuild(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	var order []string
	types.Promise("/a", builds(types, &order, "/a"))
	types.Promise("/a", func() []ir.Diagnostic {
		order = append(order, "second")
		return nil
	})

	types.Fulfil(t.Context())

	assert.Equal(t, []string{"/a"}, order)
}

// TestTypes_PromiseRefusesWhatFulfilWouldNeverTake pins the refusals: a
// promise with no build or no coordinate, and one made once Fulfil has
// returned, which nothing would ever build.
func TestTypes_PromiseRefusesWhatFulfilWouldNeverTake(t *testing.T) {
	t.Parallel()
	for name, promise := range map[string]func(*compile.Types){
		"no build":      func(types *compile.Types) { types.Promise("/a", nil) },
		"no coordinate": func(types *compile.Types) { types.Promise("", func() []ir.Diagnostic { return nil }) },
		"after fulfilment": func(types *compile.Types) {
			types.Fulfil(t.Context())
			types.Promise("/a", func() []ir.Diagnostic { return nil })
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			types := compile.NewTypes()
			promise(types)
			require.Len(t, types.Violations(), 1)
			assert.Contains(t, types.Violations()[0], "promise rejected")
		})
	}
}

// TestTypes_FulfilRefusesABuildThatBuildsNothing pins that a build leaving its
// coordinate without a node is a breach: every reference holding the
// coordinate's ID would dangle.
func TestTypes_FulfilRefusesABuildThatBuildsNothing(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Promise("/a", func() []ir.Diagnostic { return nil })

	types.Fulfil(t.Context())

	require.Len(t, types.Violations(), 1)
	assert.Contains(t, types.Violations()[0], `promise for "/a" built no node`)
}

// TestTypes_FulfilStopsBetweenBuildsOnCancellation pins that Fulfil honours
// ctx as Settle does: once it is done, no further build is taken, and what
// the ones before it reported is returned.
func TestTypes_FulfilStopsBetweenBuildsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	types := compile.NewTypes()
	var order []string
	first := builds(types, &order, "/a")
	types.Promise("/a", func() []ir.Diagnostic { cancel(); return first() })
	types.Promise("/b", builds(types, &order, "/b"))

	diags := types.Fulfil(ctx)

	assert.Equal(t, []string{"/a"}, order, "the build after the cancellation is not taken")
	require.Len(t, diags, 1)
	assert.Empty(t, types.Violations(), "a stop is the caller's to act on, not a breach")
}
