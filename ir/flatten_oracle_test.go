package ir_test

import (
	"fmt"
	"maps"
	"math/rand/v2"
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

// composition returns the live models root composes, read through alias
// scalars, root's own included, and the targets of their entries that name no
// model: a breadth-first reading, so it orders nothing.
func composition(types ir.TypeRegistry, root ir.TypeID) (map[ir.TypeID]*ir.Model, map[ir.TypeID]bool) {
	models := map[ir.TypeID]*ir.Model{}
	unflattened := map[ir.TypeID]bool{}
	queue := []ir.TypeID{standsFor(types, root)}
	if m, ok := types[queue[0]].(*ir.Model); !ok || m == nil {
		return models, unflattened
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, seen := models[id]; seen {
			continue
		}
		m := types[id].(*ir.Model)
		models[id] = m
		for _, target := range entries(m) {
			end := standsFor(types, target)
			if pm, ok := types[end].(*ir.Model); ok && pm != nil {
				queue = append(queue, end)
				continue
			}
			unflattened[target] = true
		}
	}
	return models, unflattened
}

// composedBelow returns the models the model id composes at any distance, id
// itself only when a cycle leads back.
func composedBelow(types ir.TypeRegistry, id ir.TypeID) map[ir.TypeID]bool {
	below := map[ir.TypeID]bool{}
	m, _ := types[id].(*ir.Model)
	queue := entriesOf(types, m)
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if below[next] {
			continue
		}
		below[next] = true
		pm, _ := types[next].(*ir.Model)
		queue = append(queue, entriesOf(types, pm)...)
	}
	return below
}

// entriesOf returns the live models m's entries compose in, read through alias
// scalars.
func entriesOf(types ir.TypeRegistry, m *ir.Model) []ir.TypeID {
	if m == nil {
		return nil
	}
	var out []ir.TypeID
	for _, target := range entries(m) {
		end := standsFor(types, target)
		if pm, ok := types[end].(*ir.Model); ok && pm != nil {
			out = append(out, end)
		}
	}
	return out
}

// entries returns the targets of m's set Base, Implements and Mixins.
func entries(m *ir.Model) []ir.TypeID {
	var out []ir.TypeID
	if m.Base != nil && m.Base.Target != "" {
		out = append(out, m.Base.Target)
	}
	for _, r := range append(slices.Clone(m.Implements), m.Mixins...) {
		if r.Target != "" {
			out = append(out, r.Target)
		}
	}
	return out
}

// standsFor follows id's alias scalars to the first type that is none, or to
// the first met twice.
func standsFor(types ir.TypeRegistry, id ir.TypeID) ir.TypeID {
	seen := map[ir.TypeID]bool{}
	for {
		base, alias := ir.AliasBase(types, id)
		if !alias || seen[id] {
			return id
		}
		seen[id] = true
		id = base
	}
}

// flatRandom is withinRandom with a wire name on every property, three of its
// property IDs to a name, Required on two of them, and a Requires on each
// model declaring one property: one name some model may declare, one none
// does.
func flatRandom(seed uint64) (*ir.Document, []ir.TypeID) {
	doc, types, _ := withinRandom(rand.New(rand.NewPCG(seed, seed+11)))
	for _, td := range doc.Types {
		m, ok := td.(*ir.Model)
		if !ok || m == nil {
			continue
		}
		for i := range m.Properties {
			digit := int(m.Properties[i].ID[len(m.Properties[i].ID)-1] - '0')
			m.Properties[i].WireName = fmt.Sprintf("w%d", digit%3)
			m.Properties[i].Required = digit == 1 || digit == 4
		}
		if len(m.Properties) == 1 {
			m.Requires = []string{"w2", "zz"}
		}
	}
	return doc, types
}
