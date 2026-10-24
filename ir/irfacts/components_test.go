package irfacts_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// partRoles say what each field of a type that names another type is to a value
// of it, keyed by path of Type.Field steps: "held", "composed", "alias" or
// "args". It is a decision per field, so a field cannot join a kind without
// someone saying what it is to the type graph.
var partRoles = map[string]string{
	"Model.Properties/Property.Type":                                    "held",
	"Model.Properties/Property.Encoding/Encoding.Schema":                "held",
	"Model.Properties/Property.Args/Parameter.Type":                     "args",
	"Model.AdditionalProps/AdditionalProps.Value":                       "held",
	"Model.AdditionalProps/AdditionalProps.Key":                         "held",
	"Model.AdditionalProps/AdditionalProps.Patterns/PatternProps.Value": "held",
	"Model.Base":                      "composed",
	"Model.Implements":                "composed",
	"Model.Mixins":                    "composed",
	"Union.Variants/Variant.Type":     "held",
	"List.Elem":                       "held",
	"List.Encoding/Encoding.Schema":   "held",
	"MapT.Key":                        "held",
	"MapT.Value":                      "held",
	"Tuple.Elems":                     "held",
	"Tuple.Rest":                      "held",
	"Scalar.Base":                     "alias",
	"Scalar.Encoding/Encoding.Schema": "held",
}

// notParts are the types under which a type a type names is none of its parts,
// each with the reason.
var notParts = map[reflect.Type]string{
	reflect.TypeFor[ir.Value]():                 "a value names the type it was written against",
	reflect.TypeFor[ir.Example]():               "an example illustrates a value; it holds none",
	reflect.TypeFor[ir.Availability]():          "a type changed from is one an older version used",
	reflect.TypeFor[ir.PropPath]():              "a path locates a member inside a value another position carries",
	reflect.TypeFor[ir.Discriminator]():         "a discriminator routes to a type; only a subtype is one a value may be",
	reflect.TypeFor[ir.TemplateInstantiation](): "a template argument made the type; a value of it holds none",
}

// notPartFields are fields that name a type that is none of the parts, with the
// reason, where their type alone does not say so.
var notPartFields = map[string]string{
	"Encoding.WireType": "a wire type says how a value is written, not a value it holds",
}

// TestIndex_EveryKindNamesItsPartsByRole fills every field of each kind and
// holds what the type graph reads of it to partRoles: every position a role
// names is read in that role, and nothing else is read.
func TestIndex_EveryKindNamesItsPartsByRole(t *testing.T) {
	t.Parallel()
	for _, k := range declaredKinds(t) {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td, ok := ir.NewTypeDef(k)
			require.True(t, ok, "no concrete type for kind %q", k)
			irtest.Fill(t, td)
			w := &positionWalk{types: notParts, fields: notPartFields, out: map[ir.TypeID]position{}}
			w.walk(reflect.ValueOf(td).Elem(), nil, "")
			delete(w.out, td.Common().ID)

			want := map[string][]ir.TypeID{}
			for id, at := range w.out {
				if at.excluded != "" {
					continue
				}
				role, decided := partRoles[at.path]
				require.True(t, decided, "%s names a type: give it a role in partRoles or say why it is none", at.path)
				want[role] = append(want[role], id)
			}
			held, composed, alias, args := irfacts.Parts(td)
			got := map[string][]ir.TypeID{"held": held, "composed": composed, "args": args}
			if alias != "" {
				got["alias"] = []ir.TypeID{alias}
			}
			for role, ids := range got {
				if len(ids) == 0 {
					delete(got, role)
				}
			}
			sorted := cmp.Transformer("sort", func(ids []ir.TypeID) []ir.TypeID { return slices.Sorted(slices.Values(ids)) })
			assert.Empty(t, cmp.Diff(want, got, sorted))
		})
	}
}

func TestIndex_SelfContainmentIsRecursive(t *testing.T) {
	t.Parallel()
	for name, edge := range usageEdgeCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			facts := irfacts.New(irtest.DocOf(edge.build("x")))
			assert.True(t, facts.Recursive("x"), "a type holding or including itself by this edge is recursive")
			assert.Equal(t, []ir.TypeID{"x"}, facts.Component("x"))
		})
	}
}

func TestIndex_ReferencesThatAreNoPartAreNotRecursive(t *testing.T) {
	t.Parallel()
	self := ir.TypeRef{Target: "x"}
	selfValue := &ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "x"}}
	for name, td := range map[string]ir.TypeDef{
		"model discriminator mapping": irtest.ModelOf("x", func(m *ir.Model) {
			m.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": "x"}, Default: "x"}
		}),
		"union discriminator mapping": &ir.Union{ID: "x", Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": "x"}}},
		"property default":            irtest.ModelOf("x", func(m *ir.Model) { m.Properties = []ir.Property{{Default: selfValue}} }),
		"property wire type": irtest.ModelOf("x", func(m *ir.Model) {
			m.Properties = []ir.Property{{Encoding: &ir.Encoding{WireType: &self}}}
		}),
		"field argument":       irtest.ModelOf("x", func(m *ir.Model) { m.Properties = []ir.Property{{Args: []ir.Parameter{{Type: self}}}} }),
		"scalar wire type":     &ir.Scalar{ID: "x", Encoding: &ir.Encoding{WireType: &self}},
		"list wire type":       &ir.List{ID: "x", Encoding: &ir.Encoding{WireType: &self}},
		"literal value":        &ir.Literal{ID: "x", Value: *selfValue},
		"example value":        &ir.Any{ID: "x", Examples: []ir.Example{{Value: selfValue}}},
		"template argument":    &ir.Any{ID: "x", Instantiation: &ir.TemplateInstantiation{Args: []ir.TemplateArg{{Type: &self}}}},
		"availability history": &ir.Any{ID: "x", Availability: &ir.Availability{TypeChangedFrom: []ir.VersionedType{{Type: self}}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			facts := irfacts.New(irtest.DocOf(td))
			assert.False(t, facts.Recursive("x"), "the reference names a type without making a value hold one")
			assert.Equal(t, []ir.TypeID{"x"}, facts.Component("x"))
		})
	}
}

// componentsTree is a discriminated base "node" with subtypes "branch" and
// "leaf"; "branch" holds whatever the reference built from "node" names.
func componentsTree(hold func() ir.Property, extra ...ir.TypeDef) *ir.Document {
	defs := make([]ir.TypeDef, 0, 3+len(extra))
	defs = append(defs,
		irtest.ModelOf("node", func(m *ir.Model) { m.Discriminator = &ir.Discriminator{PropertyName: "kind"} }),
		irtest.ModelOf("branch", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "node"}
			m.Properties = []ir.Property{hold()}
		}),
		irtest.ModelOf("leaf", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "node"} }),
	)
	return irtest.DocOf(append(defs, extra...)...)
}

func TestIndex_RecursionThroughAPolymorphicReference(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]*ir.Document{
		"held directly":      componentsTree(func() ir.Property { return useProp("node") }),
		"held in a list":     componentsTree(func() ir.Property { return useProp("children") }, &ir.List{ID: "children", Elem: useRef("node")}),
		"held through alias": componentsTree(func() ir.Property { return useProp("nodeAlias") }, irtest.ScalarOf("nodeAlias", "node")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			facts := irfacts.New(doc)
			assert.True(t, facts.Recursive("branch"), "a branch may hold another branch, through its base")
			assert.False(t, facts.Recursive("node"), "the base's own shape holds nothing")
			assert.False(t, facts.Recursive("leaf"))
			assert.Contains(t, facts.Component("branch"), ir.TypeID("branch"))
			assert.NotContains(t, facts.Component("branch"), ir.TypeID("node"), "components group shapes, and the base's is on no cycle")
		})
	}

	t.Run("a plain base is no route back", func(t *testing.T) {
		t.Parallel()
		doc := componentsTree(func() ir.Property { return useProp("node") })
		doc.Types["node"].(*ir.Model).Discriminator = nil
		assert.False(t, irfacts.New(doc).Recursive("branch"), "a value of a plain base is of exactly its shape")
	})
}

func TestIndex_ABaseAndItsSubtypesAreNotRecursive(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(usagePets(func(id ir.TypeID, m *ir.Model) {
		if id == "pet" {
			m.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"cat": "cat", "dog": "dog"}}
		}
	})...)
	facts := irfacts.New(doc)
	for id := range doc.Types {
		assert.False(t, facts.Recursive(id), "%s includes its base's members and routes to its subtypes; neither holds a value", id)
		assert.Equal(t, []ir.TypeID{id}, facts.Component(id), id)
	}
}

func TestIndex_MutualContainmentIsOneComponent(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		useHolder("a", "items"),
		&ir.List{ID: "items", Elem: useRef("b")},
		useHolder("b", "a", "leaf"),
		useType("leaf"),
	)
	facts := irfacts.New(doc)
	for _, id := range []ir.TypeID{"a", "b", "items"} {
		assert.True(t, facts.Recursive(id), id)
		assert.Equal(t, []ir.TypeID{"a", "b", "items"}, facts.Component(id), id)
	}
	assert.False(t, facts.Recursive("leaf"))
	assert.Equal(t, []ir.TypeID{"leaf"}, facts.Component("leaf"))
}

func TestIndex_AnAcyclicDiamondIsNotRecursive(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(useHolder("top", "left", "right"), useHolder("left", "bottom"), useHolder("right", "bottom"), useType("bottom"))
	facts := irfacts.New(doc)
	for _, id := range []ir.TypeID{"top", "left", "right", "bottom"} {
		assert.False(t, facts.Recursive(id), id)
		assert.Equal(t, []ir.TypeID{id}, facts.Component(id), id)
	}
}

func TestIndex_ADanglingReferenceIsIgnored(t *testing.T) {
	t.Parallel()
	facts := irfacts.New(irtest.DocOf(useHolder("a", "ghost")))
	assert.False(t, facts.Recursive("a"))
	assert.Equal(t, []ir.TypeID{"a"}, facts.Component("a"))
	assert.False(t, facts.Recursive("ghost"), "the document declares no such type")
	assert.Nil(t, facts.Component("ghost"))
	assert.Nil(t, facts.Component(""))
}

func TestIndex_ATypedNilEntryIsATypeHoldingNothing(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(useHolder("a", "nilm"))
	doc.Types["nilm"] = (*ir.Model)(nil)
	facts := irfacts.New(doc)
	assert.False(t, facts.Recursive("nilm"))
	assert.Equal(t, []ir.TypeID{"nilm"}, facts.Component("nilm"))
}

func TestIndex_ComponentIsSortedAndTheCallersOwn(t *testing.T) {
	t.Parallel()
	// The search from "a" closes the ring with "c" ahead of "b", the reverse of
	// the order the component must be reported in.
	doc := irtest.DocOf(useHolder("a", "c"), useHolder("c", "b"), useHolder("b", "a"))
	facts := irfacts.New(doc)

	got := facts.Component("b")
	require.Equal(t, []ir.TypeID{"a", "b", "c"}, got)
	got[0] = "clobbered"
	assert.Equal(t, []ir.TypeID{"a", "b", "c"}, facts.Component("a"), "the index keeps its own copy")
}

// factsLongContainment builds n models, t000000 first, each holding the next
// through a property, and the last holding nothing.
func factsLongContainment(n int) *ir.Document {
	defs := make([]ir.TypeDef, 0, n)
	for i := range n {
		defs = append(defs, irtest.ModelOf(factsLongID(i), func(m *ir.Model) {
			if i+1 < n {
				m.Properties = []ir.Property{useProp(factsLongID(i + 1))}
			}
		}))
	}
	return irtest.DocOf(defs...)
}

func factsLongID(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("t%06d", i)) }

func TestIndex_AVeryLongChainNeedsNoRecursion(t *testing.T) {
	t.Parallel()
	const n = 100_000
	doc := factsLongContainment(n)

	chain := irfacts.New(doc)
	assert.False(t, chain.Recursive(factsLongID(0)))
	assert.Equal(t, []ir.TypeID{factsLongID(n / 2)}, chain.Component(factsLongID(n/2)))

	// Closing the chain on itself makes every model one component.
	last := doc.Types[factsLongID(n-1)].(*ir.Model)
	last.Properties = []ir.Property{useProp(factsLongID(0))}
	ring := irfacts.New(doc)
	assert.True(t, ring.Recursive(factsLongID(n-1)))
	members := ring.Component(factsLongID(n / 2))
	require.Len(t, members, n)
	assert.True(t, slices.IsSorted(members))
	assert.Equal(t, factsLongID(0), members[0])
	assert.Equal(t, factsLongID(n-1), members[n-1])
}

func TestIndex_AVeryDeepHierarchyNeedsNoRecursion(t *testing.T) {
	t.Parallel()
	const n = 100_000
	doc := irtest.DocOf(factsChain(n, func(_ int, m *ir.Model) { m.Discriminator = &ir.Discriminator{PropertyName: "k"} })...)
	facts := irfacts.New(doc)
	assert.True(t, facts.IsSubtype(factsChainID(n), factsChainID(0)))
	assert.False(t, facts.IsSubtype(factsChainID(0), factsChainID(n)))
	assert.Len(t, facts.Subtypes(factsChainID(0)), n)
	assert.False(t, facts.Recursive(factsChainID(n/2)))
}

// componentsRandom builds up to twenty-five models holding each other, and
// undeclared types, through random properties, some with a Base and some
// discriminated. It returns the document, its models, and the parts it drew
// among them: what each holds and what each composes.
func componentsRandom(rng *rand.Rand) (doc *ir.Document, ids []ir.TypeID, held, composed map[ir.TypeID][]ir.TypeID) {
	n := 1 + rng.IntN(25)
	ids = make([]ir.TypeID, n)
	for i := range n {
		ids[i] = ir.TypeID(fmt.Sprintf("t%02d", i))
	}
	pick := func() ir.TypeID {
		if i := rng.IntN(n + 2); i < n {
			return ids[i]
		}
		return "ghost"
	}
	held, composed = map[ir.TypeID][]ir.TypeID{}, map[ir.TypeID][]ir.TypeID{}
	defs := make([]ir.TypeDef, 0, n)
	for _, id := range ids {
		m := useHolder(id)
		for range rng.IntN(4) {
			target := pick()
			m.Properties = append(m.Properties, useProp(target))
			held[id] = append(held[id], target)
		}
		if rng.IntN(3) == 0 {
			m.Base = &ir.TypeRef{Target: pick()}
			composed[id] = append(composed[id], m.Base.Target)
		}
		if rng.IntN(4) == 0 {
			m.Discriminator = &ir.Discriminator{PropertyName: "k"}
		}
		defs = append(defs, m)
	}
	return irtest.DocOf(defs...), ids, held, composed
}

// componentsOracle returns, for each model, the models whose shape its own
// reaches in one or more steps. A step goes from a model to each it composes
// and each it holds, and, with dispatch set, where what it holds is
// discriminated, to every subtype ir.IsSubtype finds of that one.
func componentsOracle(doc *ir.Document, ids []ir.TypeID, held, composed map[ir.TypeID][]ir.TypeID, dispatch bool) map[ir.TypeID]map[ir.TypeID]bool {
	step := func(from ir.TypeID) []ir.TypeID {
		next := slices.Clone(composed[from])
		for _, h := range held[from] {
			next = append(next, h)
			if m, ok := doc.Types[h].(*ir.Model); ok && m.Discriminator != nil && dispatch {
				for _, sub := range ids {
					if ir.IsSubtype(doc, sub, h) {
						next = append(next, sub)
					}
				}
			}
		}
		return next
	}
	reach := map[ir.TypeID]map[ir.TypeID]bool{}
	for _, from := range ids {
		reach[from] = map[ir.TypeID]bool{}
		pending := step(from)
		for len(pending) > 0 {
			to := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if _, declared := doc.Types[to]; declared && !reach[from][to] {
				reach[from][to] = true
				pending = append(pending, step(to)...)
			}
		}
	}
	return reach
}

func TestIndex_ComponentsAgreeWithReachability(t *testing.T) {
	t.Parallel()
	var cyclic, throughSubtypes int
	for seed := range uint64(400) {
		doc, ids, held, composed := componentsRandom(rand.New(rand.NewPCG(seed, seed+1)))
		reach := componentsOracle(doc, ids, held, composed, true)
		withoutDispatch := componentsOracle(doc, ids, held, composed, false)
		facts := irfacts.New(doc)
		for _, a := range ids {
			want := []ir.TypeID{a}
			for _, b := range ids {
				if b != a && reach[a][b] && reach[b][a] {
					want = append(want, b)
				}
			}
			slices.Sort(want)
			require.Equal(t, want, facts.Component(a), "seed %d: Component(%q)", seed, a)
			require.Equal(t, reach[a][a], facts.Recursive(a), "seed %d: Recursive(%q)", seed, a)
			if reach[a][a] {
				cyclic++
			}
			if reach[a][a] && !withoutDispatch[a][a] {
				throughSubtypes++
			}
		}
	}
	require.Positive(t, cyclic, "no random document held a cycle, so this compared nothing")
	require.Positive(t, throughSubtypes, "no random cycle ran through a subtype, so dispatch was not compared")
}

func TestIndex_ComponentsDoNotDependOnMapOrder(t *testing.T) {
	t.Parallel()
	doc, ids, _, _ := componentsRandom(rand.New(rand.NewPCG(5, 6)))
	first, second := irfacts.New(doc), irfacts.New(doc)
	for _, id := range ids {
		assert.Empty(t, cmp.Diff(first.Component(id), second.Component(id)), id)
		assert.Equal(t, first.Recursive(id), second.Recursive(id), id)
		assert.True(t, slices.IsSorted(first.Component(id)), id)
	}
}
