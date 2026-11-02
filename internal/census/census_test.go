// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package census_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/census"
	"github.com/dexpace/morphic/internal/census/testdata/fixture"
	"github.com/dexpace/morphic/ir"
)

// facts reads a report into what it says of each fact, its docs and instance
// counts as "docs instances", keyed by the rest of its line. The three counts
// of runs are keyed by their word.
func facts(t *testing.T, report string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for line := range strings.Lines(report) {
		kind, rest, found := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		require.True(t, found, "line %q", line)
		if kind == "documents" || kind == "no-document" || kind == "truncated" {
			out[kind] = rest
			continue
		}
		docs, rest, _ := strings.Cut(rest, " ")
		instances, name, _ := strings.Cut(rest, " ")
		key := kind + " " + name
		require.NotContains(t, out, key, "a fact is listed once")
		out[key] = docs + " " + instances
	}
	return out
}

// counted returns what a census of the fixture's universe reports after
// counting each root.
func counted(t *testing.T, roots ...*fixture.Root) map[string]string {
	t.Helper()
	c := census.New(fixtureUniverse(t))
	for _, r := range roots {
		require.NoError(t, c.AddAny(r))
	}
	return facts(t, c.Report())
}

func TestCensus_CountsAMemberOnlyWhenSet(t *testing.T) {
	t.Parallel()
	level := fixture.Zero
	for _, tc := range []struct {
		name   string
		root   fixture.Root
		member string
		want   string
	}{
		{"a zero string", fixture.Root{}, "Root.name", "0 0"},
		{"a string", fixture.Root{Name: "n"}, "Root.name", "1 1"},
		{"false", fixture.Root{}, "Root.flag", "0 0"},
		{"true", fixture.Root{Flag: true}, "Root.flag", "1 1"},
		{"a nil pointer", fixture.Root{}, "Root.level", "0 0"},
		{"a pointer to a zero value", fixture.Root{Level: &level}, "Root.level", "1 1"},
		{"a pointer to an empty struct", fixture.Root{Leaf: &fixture.Leaf{}}, "Root.leaf", "1 1"},
		{"an empty struct held by value", fixture.Root{Inline: fixture.Leaf{}}, "Root.inline", "0 0"},
		{"a struct held by value", fixture.Root{Inline: fixture.Leaf{Label: "x"}}, "Root.inline", "1 1"},
		{"a nil sum", fixture.Root{}, "Root.sum", "0 0"},
		{"a sum", fixture.Root{Sum: fixture.Square{}}, "Root.sum", "1 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, counted(t, &tc.root)["member "+tc.member])
		})
	}
}

func TestCensus_CountsAnEmptyCollectionOnlyWhereEmptinessIsAFact(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		root       fixture.Root
		member     string
		set, empty string
		emptiable  bool
	}{
		{"a nil omitempty slice", fixture.Root{}, "Root.leaves", "0 0", "", false},
		{"an empty omitempty slice", fixture.Root{Leaves: []fixture.Leaf{}}, "Root.leaves", "0 0", "", false},
		{"an omitempty slice", fixture.Root{Leaves: []fixture.Leaf{{}}}, "Root.leaves", "1 1", "", false},
		{"an empty omitempty map", fixture.Root{Labels: map[string]fixture.Level{}}, "Root.labels", "0 0", "", false},
		{"a nil omitzero slice", fixture.Root{}, "Root.grants", "0 0", "0 0", true},
		{"an empty omitzero slice", fixture.Root{Grants: []string{}}, "Root.grants", "1 1", "1 1", true},
		{"an omitzero slice", fixture.Root{Grants: []string{"g"}}, "Root.grants", "1 1", "0 0", true},
		{"a nil omitzero map", fixture.Root{}, "Root.index", "0 0", "0 0", true},
		{"an empty omitzero map", fixture.Root{Index: map[fixture.Color]fixture.Leaf{}}, "Root.index", "1 1", "1 1", true},
		{"an omitzero map", fixture.Root{Index: map[fixture.Color]fixture.Leaf{fixture.Red: {}}}, "Root.index", "1 1", "0 0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := counted(t, &tc.root)
			assert.Equal(t, tc.set, got["member "+tc.member])
			if !tc.emptiable {
				assert.NotContains(t, got, "empty "+tc.member)
				return
			}
			assert.Equal(t, tc.empty, got["empty "+tc.member])
		})
	}
}

func TestCensus_CountsAnEmbeddingsMembersAsItsOwn(t *testing.T) {
	t.Parallel()
	got := counted(t, &fixture.Root{ID: "x", Spread: &fixture.Leaf{Label: "l"}})

	assert.Equal(t, "1 1", got["member Common.id"])
	assert.Equal(t, "1 1", got["member Leaf.label"], "an embedding by the embed option is crossed too")
	assert.NotContains(t, got, "member Root.id")
	assert.NotContains(t, got, "member Root.label")
}

func TestCensus_CountsEveryValueAnEnumerationHolds(t *testing.T) {
	t.Parallel()
	level := fixture.High
	got := counted(t, &fixture.Root{
		Color:  fixture.Blue,
		Level:  &level,
		Index:  map[fixture.Color]fixture.Leaf{fixture.Red: {}, "green": {}},
		Labels: map[string]fixture.Level{"a": fixture.High, "b": 5},
	})

	assert.Equal(t, "1 1", got[`value Color "blue"`])
	assert.Equal(t, "1 1", got[`value Color "red"`], "a map key is a value")
	assert.Equal(t, "1 2", got[`value Level 10`], "a pointer's target and a map's value")
	assert.Equal(t, "1 1", got[`value Level 5 undeclared`])
	assert.Equal(t, "1 1", got[`value Color "green" undeclared`])
	assert.Equal(t, "0 0", got[`value Level -20`])
	assert.NotContains(t, got, `value Color "blue" undeclared`, "a declared value is not marked")
}

func TestCensus_CountsTheZeroValueOfAnEnumerationThatDeclaresNoneAsUndeclared(t *testing.T) {
	t.Parallel()
	got := counted(t, &fixture.Root{})

	assert.Equal(t, "1 1", got[`value Color "" undeclared`], "the value is held though the member is unset")
	assert.Equal(t, "0 0", got["member Root.color"])
}

func TestCensus_CountsTheTagOfEachVariantASumHolds(t *testing.T) {
	t.Parallel()
	got := counted(t, &fixture.Root{
		Sum:    &fixture.Circle{Radius: 1},
		Shapes: []fixture.Shape{fixture.Square{}, &fixture.Circle{}, nil, (*fixture.Circle)(nil)},
	})

	assert.Equal(t, "1 2", got[`value Kind "circle"`], "through a pointer; a nil sum and a nil pointer hold none")
	assert.Equal(t, "1 1", got[`value Kind "square"`], "by value")
	assert.Equal(t, "0 0", got[`value Kind "hexagon"`], "a tag no variant returns")
	assert.Equal(t, "1 1", got["member Circle.radius"])
}

func TestCensus_CountsATypeOfAnotherPackageAsNoFact(t *testing.T) {
	t.Parallel()
	got := counted(t, &fixture.Root{Timeout: 5, Reflect: reflect.String})

	assert.Equal(t, "1 1", got["member Root.timeout"])
	assert.Equal(t, "1 1", got["member Root.reflect"])
	for fact := range got {
		assert.NotContains(t, fact, "Duration", "time.Duration is no enumeration of the fixture")
		assert.NotRegexp(t, `^value Kind \d`, fact, "nor is reflect.Kind, which shares the name of one")
	}
}

func TestCensus_RefusesAValueItsDeclarationsDoNotDescribe(t *testing.T) {
	t.Parallel()
	u := fixtureUniverse(t)
	for _, tc := range []struct {
		name     string
		universe census.Universe
		root     fixture.Root
		want     string
	}{
		{"a struct with no declaration", u.WithoutStruct("Leaf"), fixture.Root{}, "struct fixture.Leaf"},
		{"a struct with another number of fields", u.WithFields("Leaf", "Label", "Extra"), fixture.Root{},
			"struct fixture.Leaf"},
		{"a field under another name", u.WithFields("Leaf", "Text"), fixture.Root{},
			"field 0 of fixture.Leaf is Label, declared as Text"},
		{"a variant the sum does not declare", u.WithoutVariant("Shape", "Circle", true),
			fixture.Root{Sum: &fixture.Circle{}}, "fixture.Shape holds *fixture.Circle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := census.New(tc.universe)
			err := c.AddAny(&tc.root)
			require.ErrorIs(t, err, census.ErrNoDeclaration)
			assert.Contains(t, err.Error(), tc.want)

			got := facts(t, c.Report())
			assert.Equal(t, "0", got["documents"], "a refused document is not counted")
			assert.Equal(t, "0 0", got["member Root.name"])
		})
	}
}

func TestCensus_StopsAtTheFirstValueItCannotCount(t *testing.T) {
	t.Parallel()
	// The variant check fails at Sum, before the walk reaches Shapes, whose
	// Square would otherwise be counted.
	c := census.New(fixtureUniverse(t).WithoutVariant("Shape", "Circle", true))
	err := c.AddAny(&fixture.Root{Sum: &fixture.Circle{}, Shapes: []fixture.Shape{&fixture.Circle{}}})
	require.ErrorIs(t, err, census.ErrNoDeclaration)
	assert.Equal(t, 1, strings.Count(err.Error(), "holds"), "one error, the first: %v", err)
}

func TestCensus_CountsDocumentsAndInstancesApart(t *testing.T) {
	t.Parallel()
	got := counted(t,
		&fixture.Root{Leaves: []fixture.Leaf{{Label: "a"}, {Label: "b"}}},
		&fixture.Root{Leaf: &fixture.Leaf{Label: "c"}},
		&fixture.Root{},
	)

	assert.Equal(t, "2 3", got["member Leaf.label"], "two documents set it, three times between them")
	assert.Equal(t, "3", got["documents"])
}

func TestCensus_CountsRunsThatLoweredToNoDocument(t *testing.T) {
	t.Parallel()
	c := census.New(fixtureUniverse(t))
	c.NoDocument()
	c.NoDocument()
	require.NoError(t, c.AddAny(&fixture.Root{}))

	got := facts(t, c.Report())
	assert.Equal(t, "2", got["no-document"])
	assert.Equal(t, "1", got["documents"], "a run with no document is not a document")
}

func TestCensus_RefusesANilDocument(t *testing.T) {
	t.Parallel()
	u, err := universe()
	require.NoError(t, err)
	c := census.New(u)
	require.ErrorIs(t, c.Add(nil), census.ErrNilDocument)
	assert.Equal(t, "0", facts(t, c.Report())["documents"], "a refused document is not counted")
}

func TestCensus_OfTheZeroUniverseCountsNoFact(t *testing.T) {
	t.Parallel()
	c := census.New(census.Universe{})
	require.NoError(t, c.AddAny(&fixture.Root{Name: "n", Sum: &fixture.Circle{}}))
	require.NoError(t, c.AddAny(&struct{ V any }{V: struct{ W string }{W: "w"}}),
		"a value of no package is no fact of the zero universe either")

	assert.Equal(t, "documents 2\nno-document 0\ntruncated 0\n", c.Report())
}

// docOf returns a document holding the given type nodes, each filed under an
// ID made from its kind and position.
func docOf(nodes ...ir.TypeDef) *ir.Document {
	reg := ir.TypeRegistry{}
	for i, n := range nodes {
		id := ir.TypeID(fmt.Sprintf("t/%s%d", n.Kind(), i))
		n.Common().ID = id
		reg[id] = n
	}
	return &ir.Document{Types: reg}
}

// irCensus returns an empty census of the IR's universe.
func irCensus(t *testing.T) *census.Census {
	t.Helper()
	u, err := universe()
	require.NoError(t, err)
	return census.New(u)
}

func TestCensus_CountsTheKindOfEveryTypeDefTheIRDeclares(t *testing.T) {
	t.Parallel()
	c := irCensus(t)

	var nodes []ir.TypeDef
	for fact := range facts(t, c.Report()) {
		spelled, isKind := strings.CutPrefix(fact, "value TypeKind ")
		if !isKind {
			continue
		}
		node, registered := ir.NewTypeDef(ir.TypeKind(strings.Trim(spelled, `"`)))
		require.True(t, registered, fact)
		nodes = append(nodes, node)
	}
	require.NotEmpty(t, nodes)
	require.NoError(t, c.Add(docOf(nodes...)))

	got := facts(t, c.Report())
	for _, node := range nodes {
		key := fmt.Sprintf("value TypeKind %q", node.Kind())
		assert.Equal(t, "1 1", got[key], key)
	}
	assert.Equal(t, fmt.Sprintf("1 %d", len(nodes)), got["member TypeCommon.id"],
		"every node carries one, wherever it is embedded")
}

func TestCensus_TellsAnExplicitlyEmptyAuthFromAnAbsentOne(t *testing.T) {
	t.Parallel()
	c := irCensus(t)
	ops := []ir.Operation{
		{},
		{Auth: []ir.AuthRequirement{}},
		{Auth: []ir.AuthRequirement{{}}},
	}
	require.NoError(t, c.Add(&ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: ops}}}}}))

	got := facts(t, c.Report())
	assert.Equal(t, "1 2", got["member Operation.auth"], "public and secured, not inherited")
	assert.Equal(t, "1 1", got["empty Operation.auth"], "explicitly public")
	assert.Equal(t, "1 3", got[`value StreamingMode ""`], "a non-streaming operation holds a declared mode")
}

func TestCensus_CountsATruncatedWalkAndKeepsWhatItReached(t *testing.T) {
	t.Parallel()
	nested := func(depth int) *ir.Value {
		v := ir.Value{Kind: ir.ValueNull}
		for range depth {
			v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
		}
		return &v
	}
	c := irCensus(t)
	require.NoError(t, c.Add(docOf(&ir.Model{Examples: []ir.Example{{Value: nested(ir.MaxWalkDepth)}}})))
	require.NoError(t, c.Add(docOf(&ir.Model{Examples: []ir.Example{{Value: nested(3)}}})))

	got := facts(t, c.Report())
	assert.Equal(t, "2", got["documents"])
	assert.Equal(t, "1", got["truncated"])
	assert.Equal(t, "2 2", got["member TypeCommon.examples"], "the truncated document still counts what the walk reached")

	var docs, instances int
	_, err := fmt.Sscanf(got[`value ValueKind "list"`], "%d %d", &docs, &instances)
	require.NoError(t, err)
	assert.Equal(t, 2, docs)
	assert.Greater(t, instances, ir.MaxWalkDepth/4, "the walk got deep into the one, and all the way into the other")
	assert.Less(t, instances, ir.MaxWalkDepth+3, "but not to the bottom of the one it cut")
}
