// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan_test

import (
	"encoding/json/jsontext"
	"maps"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/plan"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// namedModel builds the model id under the source name name.
func namedModel(id ir.TypeID, name string) *ir.Model {
	return irtest.ModelOf(id, func(m *ir.Model) { m.Name = ir.NamingFor(name) })
}

// variantsOf builds a variant referencing each of ids.
func variantsOf(ids ...ir.TypeID) []ir.Variant {
	variants := make([]ir.Variant, 0, len(ids))
	for _, id := range ids {
		variants = append(variants, ir.Variant{Type: ir.TypeRef{Target: id}})
	}
	return variants
}

// variantDocument builds a document holding the union type/union, over the
// variants a case names, beside the types those variants can reference: two
// named models, an anonymous one, and an entry holding no model.
func variantDocument(union *ir.Union) *ir.Document {
	anonymous := irtest.ModelOf("type/anonymous", func(m *ir.Model) {
		m.Anonymous = true
		m.Name = ir.Naming{Hint: "inline_pet"}
	})
	doc := irtest.DocOf(namedModel("type/cat", "Cat"), namedModel("type/dog", "Dog"), anonymous)
	doc.Types["type/nil"] = (*ir.Model)(nil)
	union.ID = "type/union"
	union.WireTagged = true
	doc.Types[union.ID] = union
	return doc
}

// TestBuild_RoutesAUnionsDiscriminator holds the whole of a discriminated
// union's DiscrimFacts: a Mapping holding an entry is the routes, and one
// holding none routes each variant by its type's source name
// (docs/emitters/refine/unions.md#the-tag-a-variants-value-carries).
func TestBuild_RoutesAUnionsDiscriminator(t *testing.T) {
	t.Parallel()
	index := 1
	tests := []struct {
		name  string
		union *ir.Union
		want  *plan.DiscrimFacts
	}{
		{"a mapping routing two tags to one variant, and a default", &ir.Union{
			Variants: variantsOf("type/cat", "type/dog"),
			Discriminator: &ir.Discriminator{PropertyName: "kind", Default: "type/dog",
				Mapping: map[string]ir.TypeID{"cat": "type/cat", "kitty": "type/cat", "dog": "type/dog"}},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Default: "type/dog",
			Mapping: map[string][]ir.TypeID{"cat": {"type/cat"}, "kitty": {"type/cat"}, "dog": {"type/dog"}},
		}},
		{"no mapping, over named variants", &ir.Union{
			Variants:      variantsOf("type/cat", "type/dog"),
			Discriminator: &ir.Discriminator{PropertyName: "kind"},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind",
			Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}, "Dog": {"type/dog"}},
		}},
		{"an empty mapping, which routes as none does", &ir.Union{
			Variants:      variantsOf("type/cat"),
			Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{}},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}},
		}},
		{"no mapping, over an anonymous variant with a hint alone", &ir.Union{
			Variants:      variantsOf("type/anonymous", "type/cat"),
			Discriminator: &ir.Discriminator{PropertyName: "kind"},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}},
		}},
		{"no mapping, over variants naming nothing, a missing type or no model", &ir.Union{
			Variants:      variantsOf("", "type/missing", "type/nil", "type/dog"),
			Discriminator: &ir.Discriminator{PropertyName: "kind"},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Mapping: map[string][]ir.TypeID{"Dog": {"type/dog"}},
		}},
		{"no mapping, over anonymous variants alone", &ir.Union{
			Variants:      variantsOf("type/anonymous"),
			Discriminator: &ir.Discriminator{PropertyName: "kind"},
		}, &plan.DiscrimFacts{Locator: plan.DiscrimPropertyName, WireKey: "kind"}},
		{"no mapping, over one type twice", &ir.Union{
			Variants:      variantsOf("type/cat", "type/cat"),
			Discriminator: &ir.Discriminator{PropertyName: "kind"},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}},
		}},
		{"a mapping leaving a variant out, which it routes nothing to", &ir.Union{
			Variants:      variantsOf("type/cat", "type/dog"),
			Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{"kitty": "type/cat"}},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Mapping: map[string][]ir.TypeID{"kitty": {"type/cat"}},
		}},
		{"a tag at a tuple element", &ir.Union{
			Variants:      variantsOf("type/cat"),
			Discriminator: &ir.Discriminator{TupleElementIndex: &index},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimIndex, TupleElementIndex: &index, Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}},
		}},
		{"an object envelope naming no value property", &ir.Union{
			Variants:      variantsOf("type/cat"),
			Discriminator: &ir.Discriminator{PropertyName: "kind", Envelope: "object"},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Envelope: "object", EnvelopeValueName: "value",
			Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}},
		}},
		{"an object envelope naming its value property", &ir.Union{
			Variants:      variantsOf("type/cat"),
			Discriminator: &ir.Discriminator{PropertyName: "kind", Envelope: "object", EnvelopeValueName: "data"},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Envelope: "object", EnvelopeValueName: "data",
			Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}},
		}},
		{"a value property named with no envelope, which names nothing", &ir.Union{
			Variants:      variantsOf("type/cat"),
			Discriminator: &ir.Discriminator{PropertyName: "kind", EnvelopeValueName: "data"},
		}, &plan.DiscrimFacts{
			Locator: plan.DiscrimPropertyName, WireKey: "kind", Mapping: map[string][]ir.TypeID{"Cat": {"type/cat"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := variantDocument(tt.union)

			got := build(t, doc, plan.Policy{}).Types["type/union"].Discrim

			assert.Empty(t, cmp.Diff(tt.want, got), "(-want +got)")
			require.NotNil(t, got)
			if tt.union.Discriminator.TupleElementIndex != nil {
				assert.Same(t, tt.union.Discriminator.TupleElementIndex, got.TupleElementIndex,
					"the plan points into the document")
			}
		})
	}
}

// petHierarchy builds a hierarchy under type/pet, whose tag property travels
// as pet_type, its Mapping routing cat to Cat and zulu to Whale. Below it are
// subtypes carrying a DiscriminatorValue by each relation a subtype has, Cat's
// keying the entry naming it, and models carrying one that are no subtype.
func petHierarchy() *ir.Document {
	pet := irtest.ModelOf("type/pet", func(m *ir.Model) {
		m.Properties = []ir.Property{property(m.ID, "petType", func(p *ir.Property) { p.WireName = "pet_type" })}
		m.Discriminator = &ir.Discriminator{
			Property: "type/pet/petType",
			Mapping:  map[string]ir.TypeID{"cat": "type/cat", "zulu": "type/whale"},
		}
	})
	subtype := func(id ir.TypeID, value string, mutate func(*ir.Model)) *ir.Model {
		return irtest.ModelOf(id, func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: pet.ID}
			m.DiscriminatorValue = value
			if mutate != nil {
				mutate(m)
			}
		})
	}
	return irtest.DocOf(
		&ir.Primitive{ID: "type/string", PrimitiveKind: ir.PrimitiveString},
		pet,
		subtype("type/cat", "cat", nil),
		subtype("type/dog", "Dog", nil),
		subtype("type/whale", "alpha", nil),
		subtype("type/silent", "", nil),
		subtype("type/puppy", "Puppy", func(m *ir.Model) { m.Base.Target = "type/dog" }),
		subtype("type/bird", "Bird", func(m *ir.Model) { m.Base, m.Implements = nil, irtest.RefsTo("type/pet") }),
		subtype("type/mixer", "Mixer", func(m *ir.Model) { m.Base, m.Mixins = nil, irtest.RefsTo("type/pet") }),
		subtype("type/stranger", "Stranger", func(m *ir.Model) { m.Base = nil }),
	)
}

// TestBuild_RoutesAHierarchysDiscriminator holds a polymorphic base's routes
// to its Mapping and to the DiscriminatorValue of each subtype it is the
// nearest discriminated ancestor of, through a model holding none, and through
// Implements but never through Mixins, and holds the plan's tag key to the
// wire name of the property it names (docs/ir/models.md#discriminator).
func TestBuild_RoutesAHierarchysDiscriminator(t *testing.T) {
	t.Parallel()
	doc := petHierarchy()

	built := build(t, doc, plan.Policy{})

	want := &plan.DiscrimFacts{
		Locator: plan.DiscrimProperty,
		WireKey: "pet_type",
		Mapping: map[string][]ir.TypeID{
			"cat": {"type/cat"}, "zulu": {"type/whale"}, "alpha": {"type/whale"},
			"Dog": {"type/dog"}, "Puppy": {"type/puppy"}, "Bird": {"type/bird"},
		},
	}
	assert.Empty(t, cmp.Diff(want, built.Types["type/pet"].Discrim), "(-want +got)")
	for id, typePlan := range built.Types {
		if id != "type/pet" {
			assert.Nil(t, typePlan.Discrim, "%s holds no discriminator", id)
		}
	}
}

// subtypeModel builds the model id, carrying the DiscriminatorValue value, its Base
// base and each of implements among its Implements.
func subtypeModel(id ir.TypeID, value string, base ir.TypeID, implements ...ir.TypeID) *ir.Model {
	return irtest.ModelOf(id, func(m *ir.Model) {
		m.DiscriminatorValue = value
		if base != "" {
			m.Base = &ir.TypeRef{Target: base}
		}
		m.Implements = irtest.RefsTo(implements...)
	})
}

// discriminated builds the model id, holding a discriminator at the wire name
// key.
func discriminated(id ir.TypeID, key string) *ir.Model {
	return irtest.ModelOf(id, func(m *ir.Model) { m.Discriminator = &ir.Discriminator{PropertyName: key} })
}

// TestBuild_RoutesAValueFromTheDiscriminatorItIsATagOf holds a subtype below
// several discriminated bases: its DiscriminatorValue routes from the base
// whose Mapping keys it to the subtype, by that entry, and otherwise, a
// union's Mapping keying it too, from the nearest but itself, level by level,
// Base before Implements, and from no other
// (docs/emitters/plan/types.md#a-discriminators-routes). want holds each
// holder's Mapping.
func TestBuild_RoutesAValueFromTheDiscriminatorItIsATagOf(t *testing.T) {
	t.Parallel()
	petRoutes := func(more map[string][]ir.TypeID) map[string][]ir.TypeID {
		routes := map[string][]ir.TypeID{
			"cat": {"type/cat"}, "zulu": {"type/whale"}, "alpha": {"type/whale"},
			"Dog": {"type/dog"}, "Bird": {"type/bird"},
		}
		maps.Copy(routes, more)
		return routes
	}
	tests := []struct {
		name string
		doc  func() *ir.Document
		want map[ir.TypeID]map[string][]ir.TypeID
	}{
		{"two discriminators at two properties", func() *ir.Document {
			doc := petHierarchy()
			doc.Types["type/dog"].(*ir.Model).Discriminator = &ir.Discriminator{PropertyName: "breed"}
			return doc
		}, map[ir.TypeID]map[string][]ir.TypeID{
			"type/pet": petRoutes(nil), "type/dog": {"Puppy": {"type/puppy"}},
		}},
		{"two discriminators at one property", func() *ir.Document {
			doc := petHierarchy()
			doc.Types["type/dog"].(*ir.Model).Discriminator = &ir.Discriminator{Property: "type/pet/petType"}
			return doc
		}, map[ir.TypeID]map[string][]ir.TypeID{
			"type/pet": petRoutes(nil), "type/dog": {"Puppy": {"type/puppy"}},
		}},
		{"a value the farther base's Mapping keys", func() *ir.Document {
			doc := petHierarchy()
			doc.Types["type/dog"].(*ir.Model).Discriminator = &ir.Discriminator{PropertyName: "breed"}
			doc.Types["type/pet"].(*ir.Model).Discriminator.Mapping["pup"] = "type/puppy"
			doc.Types["type/puppy"].(*ir.Model).DiscriminatorValue = "pup"
			return doc
		}, map[ir.TypeID]map[string][]ir.TypeID{
			"type/pet": petRoutes(map[string][]ir.TypeID{"pup": {"type/puppy"}}), "type/dog": nil,
		}},
		{"a union's Mapping keying the subtype's value", func() *ir.Document {
			union := &ir.Union{ID: "type/union", WireTagged: true, Variants: variantsOf("type/cat"),
				Discriminator: &ir.Discriminator{PropertyName: "kind", Mapping: map[string]ir.TypeID{"cat": "type/cat"}}}
			return irtest.DocOf(discriminated("type/pet", "petType"), subtypeModel("type/cat", "cat", "type/pet"), union)
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/pet": {"cat": {"type/cat"}}}},
		{"two bases one hop up, the Base sorting after the other", func() *ir.Document {
			return irtest.DocOf(discriminated("type/a", "a"), discriminated("type/b", "b"),
				subtypeModel("type/leaf", "Leaf", "type/b", "type/a"))
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/a": nil, "type/b": {"Leaf": {"type/leaf"}}}},
		{"two bases one hop up, the Base sorting first", func() *ir.Document {
			return irtest.DocOf(discriminated("type/a", "a"), discriminated("type/b", "b"),
				subtypeModel("type/leaf", "Leaf", "type/a", "type/b"))
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/a": {"Leaf": {"type/leaf"}}, "type/b": nil}},
		{"two bases one hop up through Implements, the first", func() *ir.Document {
			return irtest.DocOf(discriminated("type/a", "a"), discriminated("type/b", "b"),
				subtypeModel("type/leaf", "Leaf", "", "type/b", "type/a"))
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/a": nil, "type/b": {"Leaf": {"type/leaf"}}}},
		{"a nearer base through Implements than through the Base", func() *ir.Document {
			return irtest.DocOf(discriminated("type/a", "a"), subtypeModel("type/middle", "", "type/a"),
				discriminated("type/b", "b"), subtypeModel("type/leaf", "Leaf", "type/middle", "type/b"))
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/a": nil, "type/b": {"Leaf": {"type/leaf"}}}},
		{"a cycle of bases below a base", func() *ir.Document {
			return irtest.DocOf(discriminated("type/x", "x"),
				subtypeModel("type/y", "Y", "type/z", "type/x"), subtypeModel("type/z", "Z", "type/y"))
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/x": {"Y": {"type/y"}, "Z": {"type/z"}}}},
		{"a cycle of bases through the base, never its own nearest", func() *ir.Document {
			base := subtypeModel("type/a", "A", "type/b")
			base.Discriminator = &ir.Discriminator{PropertyName: "a"}
			unrelated := &ir.Primitive{ID: "type/string", PrimitiveKind: ir.PrimitiveString}
			return irtest.DocOf(base, subtypeModel("type/b", "", "type/a"), unrelated)
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/a": nil}},
		{"a chain discriminated at both ends, which takes every turn", func() *ir.Document {
			last := subtypeModel("type/c", "C", "type/b")
			last.Discriminator = &ir.Discriminator{PropertyName: "c"}
			return irtest.DocOf(discriminated("type/a", "a"), subtypeModel("type/b", "", "type/a"), last)
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/a": {"C": {"type/c"}}, "type/c": nil}},
		{"a base named through an alias", func() *ir.Document {
			alias := &ir.Scalar{ID: "type/alias", Base: &ir.TypeRef{Target: "type/a"}}
			return irtest.DocOf(discriminated("type/a", "a"), alias, subtypeModel("type/leaf", "Leaf", "type/alias"))
		}, map[ir.TypeID]map[string][]ir.TypeID{"type/a": {"Leaf": {"type/leaf"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			built := build(t, tt.doc(), plan.Policy{})

			for holder, want := range tt.want {
				require.NotNil(t, built.Types[holder].Discrim, holder)
				assert.Empty(t, cmp.Diff(want, built.Types[holder].Discrim.Mapping), "%s (-want +got)", holder)
			}
		})
	}
}

// TestBuild_ReadsAHierarchysTagKey holds WireKey to the effective wire name of
// the flattened member one of whose declarations the discriminator's Property
// names, its source name where it declares no wire name, and to PropertyName
// where that locates the tag instead.
func TestBuild_ReadsAHierarchysTagKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		discriminator ir.Discriminator
		wireName      string
		want          string
	}{
		{"the declaration the flattening takes",
			ir.Discriminator{Property: "type/kennel/kind"}, "kennel_kind", "kennel_kind"},
		{"a declaration the flattening overrides",
			ir.Discriminator{Property: "type/shelter/kind"}, "kennel_kind", "kennel_kind"},
		{"a declaration naming no wire name", ir.Discriminator{Property: "type/kennel/kind"}, "", "kind"},
		{"a declaration no member has", ir.Discriminator{Property: "type/kennel/missing"}, "kennel_kind", ""},
		{"a wire name", ir.Discriminator{PropertyName: "tag"}, "kennel_kind", "tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wireName := func(p *ir.Property) { p.WireName = tt.wireName }
			shelter := irtest.ModelOf("type/shelter", func(m *ir.Model) {
				m.Properties = []ir.Property{property(m.ID, "kind", wireName)}
			})
			discriminator := tt.discriminator
			kennel := irtest.ModelOf("type/kennel", func(m *ir.Model) {
				m.Mixins = irtest.RefsTo(shelter.ID)
				m.Properties = []ir.Property{property(m.ID, "kind", wireName)}
				m.Discriminator = &discriminator
			})

			built := build(t, irtest.DocOf(shelter, kennel), plan.Policy{})

			require.NotNil(t, built.Types[kennel.ID].Discrim)
			assert.Equal(t, tt.want, built.Types[kennel.ID].Discrim.WireKey)
		})
	}
}

// holderProvenance returns where each holder of a discriminator in these
// tests is declared, so a finding placed at no holder differs from it.
func holderProvenance() ir.Provenance {
	return ir.Provenance{Source: 1, Pointer: "/components/schemas/Pet"}
}

// TestBuild_RoutesATagToEveryTypeItNames holds a tag the IR routes to
// several types, as a sub-subtype inheriting its parent's pinned tag does:
// the plan routes it to each, in ID order and none twice, and reports nothing
// (docs/emitters/plan/types.md#a-discriminators-routes).
func TestBuild_RoutesATagToEveryTypeItNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		doc    func() *ir.Document
		holder ir.TypeID
		want   map[string][]ir.TypeID
	}{
		{"variants whose types share a name, one listed twice", func() *ir.Document {
			doc := variantDocument(&ir.Union{
				Variants:      variantsOf("type/cat-twin", "type/dog", "type/cat", "type/cat-twin"),
				Discriminator: &ir.Discriminator{PropertyName: "kind"},
			})
			doc.Types["type/cat-twin"] = namedModel("type/cat-twin", "Cat")
			return doc
		}, "type/union", map[string][]ir.TypeID{"Cat": {"type/cat", "type/cat-twin"}, "Dog": {"type/dog"}}},
		{"two subtypes carrying one value", func() *ir.Document {
			doc := petHierarchy()
			doc.Types["type/dog"].(*ir.Model).DiscriminatorValue = "Puppy"
			return doc
		}, "type/pet", map[string][]ir.TypeID{
			"cat": {"type/cat"}, "zulu": {"type/whale"}, "alpha": {"type/whale"},
			"Puppy": {"type/dog", "type/puppy"}, "Bird": {"type/bird"},
		}},
		{"a subtype's value keying an entry naming a type sorting after it", func() *ir.Document {
			doc := petHierarchy()
			doc.Types["type/dog"].(*ir.Model).DiscriminatorValue = "zulu"
			return doc
		}, "type/pet", map[string][]ir.TypeID{
			"cat": {"type/cat"}, "zulu": {"type/dog", "type/whale"}, "alpha": {"type/whale"},
			"Puppy": {"type/puppy"}, "Bird": {"type/bird"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			built := build(t, tt.doc(), plan.Policy{})

			require.NotNil(t, built.Types[tt.holder].Discrim)
			assert.Empty(t, cmp.Diff(tt.want, built.Types[tt.holder].Discrim.Mapping), "(-want +got)")
		})
	}
}

// TestBuild_ReportsADiscriminatorNoLocatorPlaces holds a discriminator that
// sets no locator, or several, or on a union only Property, which locates a
// model's tag alone: the plan reports it at the holder and routes nothing.
func TestBuild_ReportsADiscriminatorNoLocatorPlaces(t *testing.T) {
	t.Parallel()
	index := 0
	tests := []struct {
		name          string
		model         bool
		discriminator ir.Discriminator
		message       string
	}{
		{"a union's, with no locator", false, ir.Discriminator{}, "located by no locator"},
		{"a union's, with two", false, ir.Discriminator{PropertyName: "kind", TupleElementIndex: &index},
			"located by property_name and index"},
		{"a union's, located by a property", false, ir.Discriminator{Property: "type/holder/kind"},
			"located by property,"},
		{"a model's, with no locator", true, ir.Discriminator{}, "located by no locator"},
		{"a model's, with two", true, ir.Discriminator{Property: "type/holder/kind", PropertyName: "kind"},
			"located by property and property_name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			discriminator := tt.discriminator
			var holder ir.TypeDef = &ir.Union{ID: "type/holder", WireTagged: true, Discriminator: &discriminator}
			if tt.model {
				holder = irtest.ModelOf("type/holder", func(m *ir.Model) {
					m.Properties = []ir.Property{property(m.ID, "kind", nil)}
					m.Discriminator = &discriminator
				})
			}
			holder.Common().Provenance = holderProvenance()
			doc := irtest.DocOf(holder)

			built, diags := plan.Build(doc, irfacts.New(doc), plan.Policy{})

			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, "emitters/plan/discriminator-unlocated", diags[0].Code)
			assert.Equal(t, holderProvenance(), diags[0].Provenance)
			assert.Contains(t, diags[0].Message, "type/holder")
			assert.Contains(t, diags[0].Message, tt.message)
			assert.Nil(t, built.Types["type/holder"].Discrim)
		})
	}
}

// TestBuild_ReportsInTypeOrder holds the plan's findings to one order, by the
// ID of the type each is about, however the registry's map is ranged, and
// ahead of the finding that the document does not encode.
func TestBuild_ReportsInTypeOrder(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Name: "not UTF-8: \xff", Types: ir.TypeRegistry{}}
	letters := []string{"a", "b", "c", "d", "e", "f"}
	want := make([]string, 0, len(letters)+1)
	for _, letter := range letters {
		union := &ir.Union{ID: ir.TypeID("type/" + letter), WireTagged: true, Discriminator: &ir.Discriminator{}}
		union.Provenance.Pointer = jsontext.Pointer("/" + letter)
		doc.Types[union.ID] = union
		want = append(want, "emitters/plan/discriminator-unlocated /"+letter)
	}
	want = append(want, "emitters/plan/unencodable ")

	for range 20 {
		_, diags := plan.Build(doc, irfacts.New(doc), plan.Policy{})

		got := make([]string, 0, len(diags))
		for _, diag := range diags {
			got = append(got, diag.Code+" "+string(diag.Provenance.Pointer))
		}
		require.Equal(t, want, got)
	}
}
