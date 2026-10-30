// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestFlattenedProperties_AgreesWithAnOrderFreeReading holds every flattening
// of random registries to what a reading that orders nothing says it must
// hold: the declarations ExposedProps reaches, grouped by wire name; a
// requirement wherever a declaration or a Requires states one; the names and
// entries left over; and no member taken by a declaration of a model that the
// overridden declaration's model composes, unless each composes the other.
// Flattening every root leaves the registry as a fresh build of it.
func TestFlattenedProperties_AgreesWithAnOrderFreeReading(t *testing.T) {
	t.Parallel()
	for seed := range uint64(300) {
		doc, types := flatRandom(seed)
		for _, root := range types {
			what := fmt.Sprintf("seed %d, root %q", seed, root)
			f := ir.FlattenedProperties(doc.Types, root)
			require.Empty(t, cmp.Diff(f, ir.FlattenedProperties(doc.Types, root)), "%s: a second walk agrees", what)
			models, unflattened := composition(doc.Types, root)
			requireDeclarations(t, doc, root, f, models, what)
			requireRequirements(t, f, models, what)
			assert.ElementsMatch(t, slices.Collect(maps.Keys(unflattened)), f.Unflattened, "%s: unflattened", what)
			requireOverrides(t, doc.Types, f, what)
		}
		fresh, _ := flatRandom(seed)
		require.Empty(t, cmp.Diff(fresh, doc), "seed %d: flattening writes nothing to the registry", seed)
	}
}

// requireDeclarations holds f's members to one per wire name, each declaration
// sharing it and held by its owner, and every declaration together to the set
// ExposedProps reaches.
func requireDeclarations(t *testing.T, doc *ir.Document, root ir.TypeID, f ir.Flattening,
	models map[ir.TypeID]*ir.Model, what string,
) {
	t.Helper()
	declared := map[ir.PropID]bool{}
	wires := map[string]bool{}
	for _, fp := range f.Properties {
		wire := fp.Property.EffectiveWireName()
		require.False(t, wires[wire], "%s: wire name %q is one member", what, wire)
		wires[wire] = true
		listed := map[ir.PropID]bool{}
		for _, d := range append([]ir.PropertyDecl{fp.PropertyDecl}, fp.Overridden...) {
			require.False(t, listed[d.Property.ID], "%s: %s is listed once in its member", what, d.Property.ID)
			listed[d.Property.ID] = true
			require.Equal(t, wire, d.Property.EffectiveWireName(), "%s: a member's declarations share its name", what)
			require.Contains(t, models, d.Owner, "%s: %s's owner is a model the root composes", what, d.Property.ID)
			require.True(t, holds(models[d.Owner], d.Property), "%s: %s points into its owner", what, d.Property.ID)
			declared[d.Property.ID] = true
		}
	}
	assert.Equal(t, ir.ExposedProps(doc, root), declared, "%s: the declarations are those ExposedProps reaches", what)
}

// holds reports whether p points at one of m's properties.
func holds(m *ir.Model, p *ir.Property) bool {
	for i := range m.Properties {
		if &m.Properties[i] == p {
			return true
		}
	}
	return false
}

// requireRequirements holds each member's Required to its declarations and the
// Requires of the composed models, and Undeclared to the names those list that
// no member carries, once each.
func requireRequirements(t *testing.T, f ir.Flattening, models map[ir.TypeID]*ir.Model, what string) {
	t.Helper()
	listed := map[string]bool{}
	for _, m := range models {
		for _, name := range m.Requires {
			listed[name] = true
		}
	}
	carried := map[string]bool{}
	for _, fp := range f.Properties {
		wire := fp.Property.EffectiveWireName()
		carried[wire] = true
		want := listed[wire]
		for _, d := range append([]ir.PropertyDecl{fp.PropertyDecl}, fp.Overridden...) {
			want = want || d.Property.Required
		}
		assert.Equal(t, want, fp.Required, "%s: %q is required where a declaration or a Requires says so", what, wire)
	}
	var undeclared []string
	for name := range listed {
		if !carried[name] {
			undeclared = append(undeclared, name)
		}
	}
	assert.ElementsMatch(t, undeclared, f.Undeclared, "%s: undeclared", what)
}

// requireOverrides holds every member to a declaration no overridden one's
// model composes, unless the two models compose each other.
func requireOverrides(t *testing.T, types ir.TypeRegistry, f ir.Flattening, what string) {
	t.Helper()
	for _, fp := range f.Properties {
		for _, d := range fp.Overridden {
			below := composedBelow(types, d.Owner)
			if below[fp.Owner] {
				assert.True(t, composedBelow(types, fp.Owner)[d.Owner],
					"%s: %s@%s composes %s@%s, so it must take the member", what,
					d.Property.ID, d.Owner, fp.Property.ID, fp.Owner)
			}
		}
	}
}
