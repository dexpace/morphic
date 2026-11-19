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
// hold: the declarations ExposedProperties reaches, grouped by wire name; a
// requirement wherever a declaration or a Requires states one; a redaction
// wherever a declaration asks for one; the names and entries left over; and no
// member taken by a declaration of a model that the overridden declaration's
// model composes, unless each composes the other.
// Flattening every root leaves the registry as a fresh build of it.
func TestFlattenedProperties_AgreesWithAnOrderFreeReading(t *testing.T) {
	t.Parallel()
	for seed := range uint64(300) {
		doc, types := flatRandom(seed)
		for _, root := range types {
			what := fmt.Sprintf("seed %d, root %q", seed, root)
			flattening := ir.FlattenedProperties(doc.Types, root)
			require.Empty(t, cmp.Diff(flattening, ir.FlattenedProperties(doc.Types, root)), "%s: a second walk agrees", what)
			models, unflattened := composition(doc.Types, root)
			requireDeclarations(t, doc, root, flattening, models, what)
			requireRequirements(t, flattening, models, what)
			requireRedactions(t, doc.Types, flattening, what)
			requireDeprecations(t, flattening, what)
			assert.ElementsMatch(t, slices.Collect(maps.Keys(unflattened)), flattening.Unflattened, "%s: unflattened", what)
			requireOverrides(t, doc.Types, flattening, what)
		}
		fresh, _ := flatRandom(seed)
		require.Empty(t, cmp.Diff(fresh, doc), "seed %d: flattening writes nothing to the registry", seed)
	}
}

// requireDeclarations holds flattening's members to one per wire name, each
// declaration sharing it and held by its owner, and every declaration together
// to the set ExposedProperties reaches.
func requireDeclarations(t *testing.T, doc *ir.Document, root ir.TypeID, flattening ir.Flattening,
	models map[ir.TypeID]*ir.Model, what string,
) {
	t.Helper()
	declared := map[ir.PropertyID]bool{}
	wires := map[string]bool{}
	for _, fp := range flattening.Properties {
		wire := fp.Property.EffectiveWireName()
		require.False(t, wires[wire], "%s: wire name %q is one member", what, wire)
		wires[wire] = true
		listed := map[ir.PropertyID]bool{}
		for _, propertyDecl := range append([]ir.PropertyDecl{fp.PropertyDecl}, fp.Overridden...) {
			require.False(t, listed[propertyDecl.Property.ID], "%s: %s is listed once in its member", what, propertyDecl.Property.ID)
			listed[propertyDecl.Property.ID] = true
			require.Equal(t, wire, propertyDecl.Property.EffectiveWireName(), "%s: a member's declarations share its name", what)
			require.Contains(t, models, propertyDecl.Owner, "%s: %s's owner is a model the root composes", what, propertyDecl.Property.ID)
			require.True(t, holds(models[propertyDecl.Owner], propertyDecl.Property), "%s: %s points into its owner", what, propertyDecl.Property.ID)
			declared[propertyDecl.Property.ID] = true
		}
	}
	assert.Equal(t, ir.ExposedProperties(doc, root), declared, "%s: the declarations are those ExposedProperties reaches", what)
}

// holds reports whether property points at one of model's properties.
func holds(model *ir.Model, property *ir.Property) bool {
	for i := range model.Properties {
		if &model.Properties[i] == property {
			return true
		}
	}
	return false
}

// requireRequirements holds each member's Required to its declarations and the
// Requires of the composed models, and Undeclared to the names those list that
// no member carries, once each.
func requireRequirements(t *testing.T, flattening ir.Flattening, models map[ir.TypeID]*ir.Model, what string) {
	t.Helper()
	listed := map[string]bool{}
	for _, model := range models {
		for _, name := range model.Requires {
			listed[name] = true
		}
	}
	carried := map[string]bool{}
	for _, fp := range flattening.Properties {
		wire := fp.Property.EffectiveWireName()
		carried[wire] = true
		want := listed[wire]
		for _, propertyDecl := range append([]ir.PropertyDecl{fp.PropertyDecl}, fp.Overridden...) {
			want = want || propertyDecl.Property.Required
		}
		assert.Equal(t, want, fp.Required, "%s: %q is required where a declaration or a Requires says so", what, wire)
	}
	var undeclared []string
	for name := range listed {
		if !carried[name] {
			undeclared = append(undeclared, name)
		}
	}
	assert.ElementsMatch(t, undeclared, flattening.Undeclared, "%s: undeclared", what)
}

// requireRedactions holds each member's Secret to its declarations: redacted
// where one is Secret or of a type reachesSensitive says is.
func requireRedactions(t *testing.T, types ir.TypeRegistry, flattening ir.Flattening, what string) {
	t.Helper()
	for _, fp := range flattening.Properties {
		var want bool
		for _, propertyDecl := range append([]ir.PropertyDecl{fp.PropertyDecl}, fp.Overridden...) {
			want = want || propertyDecl.Property.Secret || reachesSensitive(types, propertyDecl.Property.Type.Target)
		}
		assert.Equal(t, want, fp.Secret, "%s: %q is redacted where a declaration asks for it", what,
			fp.Property.EffectiveWireName())
	}
}

// requireDeprecations holds each member's Deprecation to its declarations:
// one where any of them states one, and the taken one's where it does.
func requireDeprecations(t *testing.T, flattening ir.Flattening, what string) {
	t.Helper()
	for _, fp := range flattening.Properties {
		var stated []*ir.Deprecation
		for _, propertyDecl := range append([]ir.PropertyDecl{fp.PropertyDecl}, fp.Overridden...) {
			if propertyDecl.Property.Deprecation != nil {
				stated = append(stated, propertyDecl.Property.Deprecation)
			}
		}
		wire := fp.Property.EffectiveWireName()
		if len(stated) == 0 {
			assert.Nil(t, fp.Deprecation, "%s: %q is deprecated by no declaration", what, wire)
			continue
		}
		require.NotNil(t, fp.Deprecation, "%s: %q is deprecated where a declaration says so", what, wire)
		if fp.Property.Deprecation != nil {
			assert.Same(t, fp.Property.Deprecation, fp.Deprecation, "%s: %q carries the taken declaration's", what, wire)
		}
	}
}

// requireOverrides holds every member to a declaration no overridden one's
// model composes, unless the two models compose each other.
func requireOverrides(t *testing.T, types ir.TypeRegistry, flattening ir.Flattening, what string) {
	t.Helper()
	for _, fp := range flattening.Properties {
		for _, propertyDecl := range fp.Overridden {
			below := composedBelow(types, propertyDecl.Owner)
			if below[fp.Owner] {
				assert.True(t, composedBelow(types, fp.Owner)[propertyDecl.Owner],
					"%s: %s@%s composes %s@%s, so it must take the member", what,
					propertyDecl.Property.ID, propertyDecl.Owner, fp.Property.ID, fp.Owner)
			}
		}
	}
}
