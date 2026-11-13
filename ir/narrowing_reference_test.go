// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestNarrowedShapes_MatchesACopyOfTheRegistry holds every narrowing of
// random registries holding unions to narrowingReference, which builds each
// shape as a copy of the registry with every composition entry standing for
// a union rewritten to the variant read there, and flattens the copy: the
// union, each shape's Over and flattening in order, and why none is listed.
// It covers each way the rules end, and every root of each registry.
func TestNarrowedShapes_MatchesACopyOfTheRegistry(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	for seed := range uint64(400) {
		doc, roots := narrowRandom(seed)
		fresh, _ := narrowRandom(seed)
		for _, root := range roots {
			what := fmt.Sprintf("seed %d, root %q", seed, root)
			got := narrowed(t, doc.Types, root)
			want := narrowingReference(t, doc.Types, root)
			require.Equal(t, want, renderNarrowing(got), "%s", what)
			seen[outcome(got)]++
		}
		require.Empty(t, cmp.Diff(fresh, doc), "seed %d: narrowing writes nothing to the registry", seed)
	}
	for _, o := range []string{"shapes", "nested", "no variant", "none", "several", "unending", "not_values"} {
		assert.Greater(t, seen[o], 10, "the comparison must reach roots of each outcome to mean anything: %v", seen)
	}
}

// outcome names what a narrowing gives, for counting the cases compared.
func outcome(n ir.Narrowing) string {
	switch {
	case n.Undistributed != "":
		return string(n.Undistributed)
	case n.Union == "":
		return "none"
	case len(n.Shapes) == 0:
		return "no variant"
	}
	for _, s := range n.Shapes {
		if len(s.Over) > 1 {
			return "nested"
		}
	}
	return "shapes"
}

// renderNarrowing renders n as narrowingReference does.
func renderNarrowing(n ir.Narrowing) []string {
	return append([]string{fmt.Sprintf("union %q undistributed %q", n.Union, n.Undistributed)}, shapeLines(n)...)
}

// narrowRandom builds a registry of up to sixteen types: models in flatRandom's
// shape whose entries name unions often, alias scalars, and unions of up to
// three variants, some tagged by a key, an envelope or a tuple element, beside
// a type that is no model, one the registry lacks, a typed nil model and union,
// and an unset reference. It returns every type as a root.
func narrowRandom(seed uint64) (*ir.Document, []ir.TypeID) {
	rng := rand.New(rand.NewPCG(seed, seed+29))
	n := 2 + rng.IntN(15)
	all := make([]ir.TypeID, 0, n+5)
	for i := range n {
		all = append(all, ir.TypeID(fmt.Sprintf("t%02d", i)))
	}
	ids := all[:n:n]
	all = append(all, "enum", "ghost", "nilm", "nilu", "")
	pick := func() ir.TypeID { return all[rng.IntN(len(all))] }
	doc := irtest.DocOf(&ir.Enum{ID: "enum"})
	doc.Types["nilm"] = (*ir.Model)(nil)
	doc.Types["nilu"] = (*ir.Union)(nil)
	index := 0
	for _, id := range ids {
		switch r := rng.IntN(10); {
		case r < 2:
			doc.Types[id] = irtest.ScalarOf(id, pick())
		case r < 5:
			u := unionOf(id)
			for range rng.IntN(4) {
				u.Variants = append(u.Variants, ir.Variant{Type: ir.TypeRef{Target: pick()}})
			}
			switch rng.IntN(12) {
			case 0:
				u.WireTagged = true
			case 1:
				u.WireTagged, u.Discriminator = true, &ir.Discriminator{PropertyName: "k", Envelope: "object"}
			case 2:
				u.WireTagged, u.Discriminator = true, &ir.Discriminator{Index: &index}
			case 3:
				u.WireTagged, u.Discriminator = true, &ir.Discriminator{PropertyName: "k"}
			default:
			}
			doc.Types[id] = u
		default:
			doc.Types[id] = randomModel(rng, id, pick)
		}
	}
	return doc, all
}

// randomModel is a model id with up to three properties of flatRandom's eight
// IDs, Required on two, Secret on one and deprecated on two, a Requires on
// one name, and entries pick names.
func randomModel(rng *rand.Rand, id ir.TypeID, pick func() ir.TypeID) *ir.Model {
	return irtest.ModelOf(id, func(m *ir.Model) {
		for range rng.IntN(4) {
			digit := rng.IntN(8)
			p := ir.Property{
				ID: ir.PropID(fmt.Sprintf("p%d", digit)), WireName: fmt.Sprintf("w%d", digit%3),
				Required: digit == 1 || digit == 4, Secret: digit == 6,
			}
			if digit == 3 || digit == 5 {
				p.Deprecation = &ir.Deprecation{Message: fmt.Sprintf("%s@%s", p.ID, id)}
			}
			m.Properties = append(m.Properties, p)
		}
		if rng.IntN(3) > 0 {
			m.Base = &ir.TypeRef{Target: pick()}
		}
		m.Implements = irtest.RefsTo(pickSome(rng, pick)...)
		m.Mixins = irtest.RefsTo(pickSome(rng, pick)...)
		if rng.IntN(4) == 0 {
			m.Requires = []string{fmt.Sprintf("w%d", rng.IntN(4))}
		}
	})
}

// narrowingReference narrows the model root names as the rules read: a shape
// for each set variant of each union a copy's composition narrows, depth first,
// each copy's flattening read by FlattenedProperties, and the first rule
// ending the distribution, in the order the shapes are listed. It renders the
// result as renderNarrowing does.
func narrowingReference(t *testing.T, types ir.TypeRegistry, root ir.TypeID) []string {
	t.Helper()
	nr := narrowingRef{t: t, types: types, root: standsFor(types, root)}
	unions, _ := nr.composition(types, nil)
	head := func(union ir.TypeID, why ir.Undistributed) []string {
		return []string{fmt.Sprintf("union %q undistributed %q", union, why)}
	}
	switch len(unions) {
	case 0:
		return head("", "")
	case 1:
	default:
		return head("", ir.UndistributedSeveral)
	}
	why := nr.distribute(nil, unions[0], 0)
	if why != "" {
		return head(unions[0], why)
	}
	return append(head(unions[0], ""), nr.out...)
}

// narrowingRef is one reference narrowing: the registry, the root read
// through alias scalars, and the shapes rendered so far.
type narrowingRef struct {
	t     *testing.T
	types ir.TypeRegistry
	root  ir.TypeID
	out   []string
}

// distribute renders a shape for each set variant of union in turn, over the
// substitutions over already reads, and returns why the distribution ends
// without its shapes. Depth is bounded by the unions a registry of sixteen
// types holds, since no union repeats along over.
func (nr *narrowingRef) distribute(over []sub, union ir.TypeID, depth int) ir.Undistributed {
	require.Less(nr.t, depth, 32, "no union repeats along a distribution")
	u := nr.types[union].(*ir.Union)
	if u.WireTagged && (u.Discriminator == nil || u.Discriminator.Envelope != "" || u.Discriminator.Index != nil) {
		return ir.UndistributedNotValues
	}
	for _, v := range u.Variants {
		if v.Type.Target == "" {
			continue
		}
		next := append(append([]sub{}, over...), sub{union, v.Type.Target})
		copied, unending := nr.rewrite(next)
		unions, placed := nr.composition(copied, next)
		switch {
		case unending || placed:
			return ir.UndistributedUnending
		case len(unions) > 1:
			return ir.UndistributedSeveral
		case len(unions) == 1:
			if why := nr.distribute(next, unions[0], depth+1); why != "" {
				return why
			}
		default:
			variants := make([]ir.TypeID, len(next))
			for i, s := range next {
				variants[i] = s.variant
			}
			f := ir.FlattenedProperties(copied, nr.root)
			nr.out = append(nr.out, fmt.Sprintf("over %v: %v undeclared %v unflattened %v",
				variants, flatLines(f), f.Undeclared, f.Unflattened))
		}
	}
	return ""
}

// sub is a variant read in a union's place.
type sub struct{ union, variant ir.TypeID }

// rewrite copies the registry with every composition entry of the root's
// composition standing for a union over reads a variant of rewritten to that
// variant, read along a chain of them, and reports whether a chain reached a
// union twice. Only the root's composition is rewritten, since only its
// entries are read.
func (nr *narrowingRef) rewrite(over []sub) (ir.TypeRegistry, bool) {
	copied := maps.Clone(nr.types)
	var unending bool
	fix := func(r ir.TypeRef) ir.TypeRef {
		seen := map[ir.TypeID]bool{}
		for r.Target != "" {
			end := standsFor(nr.types, r.Target)
			i := indexOf(over, end)
			if i < 0 {
				break
			}
			if seen[end] {
				unending = true
				break
			}
			seen[end] = true
			r.Target = over[i].variant
		}
		return r
	}
	for id, td := range nr.types {
		m, ok := td.(*ir.Model)
		if !ok || m == nil {
			continue
		}
		c := *m
		if m.Base != nil {
			b := fix(*m.Base)
			c.Base = &b
		}
		c.Implements, c.Mixins = nil, nil
		for _, r := range m.Implements {
			c.Implements = append(c.Implements, fix(r))
		}
		for _, r := range m.Mixins {
			c.Mixins = append(c.Mixins, fix(r))
		}
		copied[id] = &c
	}
	return copied, unending && nr.reachesRewrite(copied, over)
}

// reachesRewrite reports whether the root's composition in copied holds an
// entry whose chain of variants reached a union twice: one still standing for
// a union over reads a variant of, where rewrite leaves such an entry.
func (nr *narrowingRef) reachesRewrite(copied ir.TypeRegistry, over []sub) bool {
	models, _ := composition(copied, nr.root)
	for _, m := range models {
		for _, target := range entries(m) {
			if target != "" && indexOf(over, standsFor(copied, target)) >= 0 {
				return true
			}
		}
	}
	return false
}

// indexOf returns where over reads a variant of the union id, or -1.
func indexOf(over []sub, id ir.TypeID) int {
	for i, s := range over {
		if s.union == id {
			return i
		}
	}
	return -1
}

// composition returns the unions the root's composition in types narrows,
// each a union an entry stands for that over reads no variant of and that
// does not list the root, in the order of first sight, and whether a model a
// variant over reads stands for composes the root.
func (nr *narrowingRef) composition(types ir.TypeRegistry, over []sub) ([]ir.TypeID, bool) {
	models, _ := composition(types, nr.root)
	var unions []ir.TypeID
	listed := map[ir.TypeID]bool{}
	for _, id := range slices.Sorted(maps.Keys(models)) {
		for _, target := range entries(models[id]) {
			end := standsFor(types, target)
			u, ok := types[end].(*ir.Union)
			if !ok || u == nil || listed[end] || indexOf(over, end) >= 0 {
				continue
			}
			listed[end] = true
			if !listsRoot(types, u, nr.root) {
				unions = append(unions, end)
			}
		}
	}
	return unions, nr.placedReachesRoot(types, models, over)
}

// placedReachesRoot reports whether a model one of over's variants stands for
// in the root's composition composes the root in turn.
func (nr *narrowingRef) placedReachesRoot(types ir.TypeRegistry, models map[ir.TypeID]*ir.Model, over []sub) bool {
	for _, s := range over {
		end := standsFor(types, s.variant)
		if _, in := models[end]; in && composedBelow(types, end)[nr.root] {
			return true
		}
	}
	return false
}

// listsRoot reports whether u has a variant standing for root.
func listsRoot(types ir.TypeRegistry, u *ir.Union, root ir.TypeID) bool {
	for _, v := range u.Variants {
		if standsFor(types, v.Type.Target) == root {
			return true
		}
	}
	return false
}

func TestNarrowingReference_ReadsTheDocumentsExample(t *testing.T) {
	t.Parallel()
	doc := ruleDoc()
	assert.Equal(t, renderNarrowing(narrowed(t, doc.Types, "stored")), narrowingReference(t, doc.Types, "stored"))
	_, _, ok := ir.NarrowedShapes(doc.Types, "stored", math.MaxInt)
	assert.True(t, ok)
}
