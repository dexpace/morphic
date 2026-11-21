// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
func outcome(narrowing ir.Narrowing) string {
	switch {
	case narrowing.Undistributed != "":
		return string(narrowing.Undistributed)
	case narrowing.Union == "":
		return "none"
	case len(narrowing.Shapes) == 0:
		return "no variant"
	}
	for _, narrowedShape := range narrowing.Shapes {
		if len(narrowedShape.Over) > 1 {
			return "nested"
		}
	}
	return "shapes"
}

// renderNarrowing renders narrowing as narrowingReference does.
func renderNarrowing(narrowing ir.Narrowing) []string {
	return append([]string{fmt.Sprintf("union %q undistributed %q", narrowing.Union, narrowing.Undistributed)}, shapeLines(narrowing)...)
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
			union := unionOf(id)
			for range rng.IntN(4) {
				union.Variants = append(union.Variants, ir.Variant{Type: ir.TypeRef{Target: pick()}})
			}
			switch rng.IntN(12) {
			case 0:
				union.WireTagged = true
			case 1:
				union.WireTagged, union.Discriminator = true, &ir.Discriminator{PropertyName: "k", Envelope: "object"}
			case 2:
				union.WireTagged, union.Discriminator = true, &ir.Discriminator{TupleElementIndex: &index}
			case 3:
				union.WireTagged, union.Discriminator = true, &ir.Discriminator{PropertyName: "k"}
			default:
			}
			doc.Types[id] = union
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
	return irtest.ModelOf(id, func(model *ir.Model) {
		for range rng.IntN(4) {
			digit := rng.IntN(8)
			property := ir.Property{
				ID: ir.PropertyID(fmt.Sprintf("p%d", digit)), WireName: fmt.Sprintf("w%d", digit%3),
				Required: digit == 1 || digit == 4, Secret: digit == 6,
			}
			if digit == 3 || digit == 5 {
				property.Deprecation = &ir.Deprecation{Message: fmt.Sprintf("%s@%s", property.ID, id)}
			}
			model.Properties = append(model.Properties, property)
		}
		if rng.IntN(3) > 0 {
			model.Base = &ir.TypeRef{Target: pick()}
		}
		model.Implements = irtest.RefsTo(pickSome(rng, pick)...)
		model.Mixins = irtest.RefsTo(pickSome(rng, pick)...)
		if rng.IntN(4) == 0 {
			model.Requires = []string{fmt.Sprintf("w%d", rng.IntN(4))}
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
	nr.t.Helper()
	require.Less(nr.t, depth, 32, "no union repeats along a distribution")
	unionValue := nr.types[union].(*ir.Union)
	if unionValue.WireTagged && (unionValue.Discriminator == nil || unionValue.Discriminator.Envelope != "" || unionValue.Discriminator.TupleElementIndex != nil) {
		return ir.UndistributedNotValues
	}
	for _, variant := range unionValue.Variants {
		if variant.Type.Target == "" {
			continue
		}
		next := append(append([]sub{}, over...), sub{union, variant.Type.Target})
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
			for i, sub := range next {
				variants[i] = sub.variant
			}
			flattening := ir.FlattenedProperties(copied, nr.root)
			nr.out = append(nr.out, fmt.Sprintf("over %v: %v undeclared %v unflattened %v",
				variants, flatLines(flattening), flattening.Undeclared, flattening.Unflattened))
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
	fix := func(typeRef ir.TypeRef) ir.TypeRef {
		seen := map[ir.TypeID]bool{}
		for typeRef.Target != "" {
			end := standsFor(nr.types, typeRef.Target)
			i := indexOf(over, end)
			if i < 0 {
				break
			}
			if seen[end] {
				unending = true
				break
			}
			seen[end] = true
			typeRef.Target = over[i].variant
		}
		return typeRef
	}
	for id, td := range nr.types {
		model, ok := td.(*ir.Model)
		if !ok || model == nil {
			continue
		}
		modelValue := *model
		if model.Base != nil {
			b := fix(*model.Base)
			modelValue.Base = &b
		}
		modelValue.Implements, modelValue.Mixins = nil, nil
		for _, typeRef := range model.Implements {
			modelValue.Implements = append(modelValue.Implements, fix(typeRef))
		}
		for _, typeRef := range model.Mixins {
			modelValue.Mixins = append(modelValue.Mixins, fix(typeRef))
		}
		copied[id] = &modelValue
	}
	return copied, unending && nr.reachesRewrite(copied, over)
}

// reachesRewrite reports whether the root's composition in copied holds an
// entry whose chain of variants reached a union twice: one still standing for
// a union over reads a variant of, where rewrite leaves such an entry.
func (nr *narrowingRef) reachesRewrite(copied ir.TypeRegistry, over []sub) bool {
	models, _ := composition(copied, nr.root)
	for _, model := range models {
		for _, target := range entries(model) {
			if target != "" && indexOf(over, standsFor(copied, target)) >= 0 {
				return true
			}
		}
	}
	return false
}

// indexOf returns where over reads a variant of the union id, or -1.
func indexOf(over []sub, id ir.TypeID) int {
	for i, sub := range over {
		if sub.union == id {
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
			union, ok := types[end].(*ir.Union)
			if !ok || union == nil || listed[end] || indexOf(over, end) >= 0 {
				continue
			}
			listed[end] = true
			if !listsRoot(types, union, nr.root) {
				unions = append(unions, end)
			}
		}
	}
	return unions, nr.placedReachesRoot(types, models, over)
}

// placedReachesRoot reports whether a model one of over's variants stands for
// in the root's composition composes the root in turn.
func (nr *narrowingRef) placedReachesRoot(types ir.TypeRegistry, models map[ir.TypeID]*ir.Model, over []sub) bool {
	for _, sub := range over {
		end := standsFor(types, sub.variant)
		if _, in := models[end]; in && composedBelow(types, end)[nr.root] {
			return true
		}
	}
	return false
}

// listsRoot reports whether union has a variant standing for root.
func listsRoot(types ir.TypeRegistry, union *ir.Union, root ir.TypeID) bool {
	for _, variant := range union.Variants {
		if standsFor(types, variant.Type.Target) == root {
			return true
		}
	}
	return false
}

func TestNarrowingReference_ReadsTheDocumentsExample(t *testing.T) {
	t.Parallel()
	doc := ruleDoc()
	assert.Equal(t, renderNarrowing(narrowed(t, doc.Types, "stored")), narrowingReference(t, doc.Types, "stored"))
}
