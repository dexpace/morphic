// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIDClasses_AreDerivedOnceNotPerReader holds each reader of a walk that
// needs the classes of ID to the set derived once: building an IDReferences,
// and an EmptyReferences' first visit of a struct, each cost fewer allocations
// than deriving the classes does, which every one of them paid while each
// derived its own. It is not run in parallel: AllocsPerRun measures the
// process.
func TestIDClasses_AreDerivedOnceNotPerReader(t *testing.T) {
	deriving := testing.AllocsPerRun(5, func() { _ = idClassesOf(idClassRoots()) })
	require.Greater(t, deriving, 100.0, "deriving the classes walks the IR's types, which allocates")

	doc := &Document{}
	references := testing.AllocsPerRun(5, func() { _ = NewIDReferences(doc) })
	empty := testing.AllocsPerRun(5, func() {
		var e EmptyReferences
		e.Visit(reflect.ValueOf(Operation{}), WalkPath{})
	})

	assert.Less(t, references, deriving, "NewIDReferences: %v allocations, deriving the classes %v", references, deriving)
	assert.Less(t, empty, deriving, "EmptyReferences.Visit: %v allocations, deriving the classes %v", empty, deriving)
}
