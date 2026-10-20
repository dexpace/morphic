package census_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/census"
	"github.com/dexpace/morphic/ir"
)

// newCensus returns an empty census of the IR's universe.
func newCensus(t *testing.T) *census.Census {
	t.Helper()
	u, err := universe()
	require.NoError(t, err)
	return census.New(u)
}

// add counts doc, failing the test if the census refuses it.
func add(t *testing.T, c *census.Census, doc *ir.Document) {
	t.Helper()
	require.NoError(t, c.Add(doc))
}

// facts reads a report into what it says of each fact: its docs and instance
// counts, as "docs instances", keyed by the rest of its line. The two header
// lines are keyed by their first word.
func facts(t *testing.T, report string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for line := range strings.Lines(strings.TrimSuffix(report, "\n")) {
		kind, rest, found := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		require.True(t, found, "line %q", line)
		if kind == "documents" || kind == "no-document" {
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

// docOf returns a document holding the given type nodes, each filed under the
// ID it carries or, if it has none, one made from its kind and position.
func docOf(nodes ...ir.TypeDef) *ir.Document {
	reg := ir.TypeRegistry{}
	for i, n := range nodes {
		id := n.Common().ID
		if id == "" {
			id = ir.TypeID(fmt.Sprintf("t/%s%d", n.Kind(), i))
			n.Common().ID = id
		}
		reg[id] = n
	}
	return &ir.Document{Types: reg}
}

// nestedList returns a value nested depth lists deep.
func nestedList(depth int) ir.Value {
	v := ir.Value{Kind: ir.ValueNull}
	for range depth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	return v
}

func TestCensus_CountsAMemberOnlyWhenSet(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	add(t, c, docOf(&ir.Model{Abstract: true}))

	got := facts(t, c.Report())
	assert.Equal(t, "1 1", got["member Model.abstract"])
	assert.Equal(t, "0 0", got["member Model.positional"], "false is the zero value")
	assert.Equal(t, "1 1", got["member Document.types"])
}

func TestCensus_CountsASliceOrMapOnlyWhenNotEmpty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		model *ir.Model
		want  string
	}{
		{"a nil slice", &ir.Model{}, "0 0"},
		{"an empty slice that is not nil", &ir.Model{Properties: []ir.Property{}}, "0 0"},
		{"a slice with an element", &ir.Model{Properties: []ir.Property{{}}}, "1 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newCensus(t)
			add(t, c, docOf(tc.model))
			assert.Equal(t, tc.want, facts(t, c.Report())["member Model.properties"])
		})
	}

	t.Run("a map", func(t *testing.T) {
		t.Parallel()
		c := newCensus(t)
		add(t, c, &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{}})
		add(t, c, &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{"a/x": {}}})
		assert.Equal(t, "1 1", facts(t, c.Report())["member Document.auth"], "only the document with an entry sets it")
	})
}

func TestCensus_CountsAPointerToAnEmptyStructAsSet(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	add(t, c, docOf(&ir.Model{AdditionalProps: &ir.AdditionalProps{}}))

	got := facts(t, c.Report())
	assert.Equal(t, "1 1", got["member Model.additionalProps"])
	assert.Equal(t, "0 0", got["member AdditionalProps.value"], "the empty struct's own members are not set")
	assert.Equal(t, "0 0", got["member AdditionalProps.key"])
}

func TestCensus_CountsTheKindOfEveryTypeDefTheRegistryHolds(t *testing.T) {
	t.Parallel()
	c := newCensus(t)

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
	require.Len(t, nodes, 11, "the universe declares the kinds the IR registers")
	add(t, c, docOf(nodes...))

	got := facts(t, c.Report())
	for _, node := range nodes {
		key := fmt.Sprintf("value TypeKind %q", node.Kind())
		assert.Equal(t, "1 1", got[key], key)
	}
	assert.Equal(t, "1 11", got["member TypeCommon.id"], "every node carries one, wherever it is embedded")
}

func TestCensus_ReportsAnEmbeddedStructOnceUnderItsOwnName(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	add(t, c, docOf(&ir.Model{}, &ir.Model{}, &ir.Union{}))

	got := facts(t, c.Report())
	assert.Equal(t, "1 3", got["member TypeCommon.id"])
	assert.NotContains(t, got, "member Model.id")
	assert.NotContains(t, got, "member Union.id")
}

func TestCensus_CountsEnumValues(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	add(t, c, docOf(&ir.Primitive{Prim: ir.PrimString}, &ir.Primitive{Prim: ir.PrimString}, &ir.Primitive{Prim: ir.PrimBool}))

	got := facts(t, c.Report())
	assert.Equal(t, "1 2", got[`value PrimKind "string"`])
	assert.Equal(t, "1 1", got[`value PrimKind "bool"`])
	assert.Equal(t, "0 0", got[`value PrimKind "uuid"`])
}

func TestCensus_ReportsAValueTheTypeDeclaresNoConstantForAsUndeclared(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	doc := docOf(&ir.Primitive{Prim: "hologram"})
	doc.Services = []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{}}}}}}
	add(t, c, doc)

	got := facts(t, c.Report())
	assert.Equal(t, "1 1", got[`value PrimKind "hologram" undeclared`])
	assert.Equal(t, "1 1", got[`value StreamingMode "" undeclared`], "an absence no constant names is undeclared")
	assert.NotContains(t, got, `value PrimKind "hologram"`)
	assert.NotContains(t, got, `value PrimKind "string" undeclared`, "a declared value is not marked")
}

func TestCensus_CountsABitSetByTheBitsItHolds(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	add(t, c, docOf(
		&ir.Model{Usage: ir.UsageInput | ir.UsageOutput},
		&ir.Model{Usage: ir.UsageInput | ir.UsageFlags(16)},
		&ir.Model{},
	))

	got := facts(t, c.Report())
	assert.Equal(t, "1 2", got["value UsageFlags 1"])
	assert.Equal(t, "1 1", got["value UsageFlags 2"])
	assert.Equal(t, "0 0", got["value UsageFlags 4"])
	assert.Equal(t, "1 1", got["value UsageFlags 16 undeclared"])
	assert.NotContains(t, got, "value UsageFlags 0 undeclared", "no bit set is no value")
	assert.Equal(t, "1 2", got["member TypeCommon.usage"])
}

func TestCensus_CountsDocumentsAndInstancesApart(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	add(t, c, docOf(&ir.Model{Abstract: true}, &ir.Model{Abstract: true}))
	add(t, c, docOf(&ir.Model{Abstract: true}))
	add(t, c, docOf(&ir.Model{}))

	got := facts(t, c.Report())
	assert.Equal(t, "2 3", got["member Model.abstract"], "two documents set it, three times between them")
	assert.Equal(t, "3 truncated 0", got["documents"])
}

func TestCensus_CountsATruncatedWalkAndKeepsWhatItReached(t *testing.T) {
	t.Parallel()
	deep, shallow := nestedList(ir.MaxWalkDepth), nestedList(3)
	c := newCensus(t)
	add(t, c, docOf(&ir.Model{Examples: []ir.Example{{Value: &deep}}}))
	add(t, c, docOf(&ir.Model{Examples: []ir.Example{{Value: &shallow}}}))

	got := facts(t, c.Report())
	assert.Equal(t, "2 truncated 1", got["documents"])
	assert.Equal(t, "2 2", got["member TypeCommon.examples"], "the truncated document still counts what the walk reached")

	docs, instances := counts(t, got[`value ValueKind "list"`])
	assert.Equal(t, 2, docs)
	assert.Greater(t, instances, 3+1000, "the walk got deep into the one, and all the way into the other")
}

// counts splits a fact's "docs instances" into its numbers.
func counts(t *testing.T, fact string) (docs, instances int) {
	t.Helper()
	_, err := fmt.Sscanf(fact, "%d %d", &docs, &instances)
	require.NoError(t, err, fact)
	return docs, instances
}

func TestCensus_CountsRunsThatLoweredToNoDocument(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	c.NoDocument()
	c.NoDocument()
	add(t, c, &ir.Document{})

	got := facts(t, c.Report())
	assert.Equal(t, "2", got["no-document"])
	assert.Equal(t, "1 truncated 0", got["documents"], "a run with no document is not a document")
}

func TestCensus_RefusesWhatItCannotCount(t *testing.T) {
	t.Parallel()
	c := newCensus(t)
	require.ErrorIs(t, c.Add(nil), census.ErrNilDocument)
	assert.Equal(t, "0 truncated 0", facts(t, c.Report())["documents"], "a refused document is not counted")
}

func TestCensus_OfTheZeroUniverseCountsNoFact(t *testing.T) {
	t.Parallel()
	c := census.New(census.Universe{})
	add(t, c, docOf(&ir.Model{Abstract: true}, &ir.Primitive{Prim: ir.PrimString}))

	assert.Equal(t, "documents 1 truncated 0\nno-document 0\n", c.Report())
}

func TestReport_ListsEveryFactOfTheUniverseWhateverIsCounted(t *testing.T) {
	t.Parallel()
	empty := facts(t, newCensus(t).Report())

	c := newCensus(t)
	add(t, c, docOf(&ir.Model{Abstract: true}, &ir.Primitive{Prim: "hologram"}))
	counted := facts(t, c.Report())

	var zeros int
	for fact, n := range empty {
		if fact == "documents" || fact == "no-document" {
			continue
		}
		assert.Equal(t, "0 0", n, fact)
		zeros++
		assert.Contains(t, counted, fact, "counting a document removes no fact from the report")
	}
	assert.Greater(t, zeros, 500)
	assert.Contains(t, counted, `value PrimKind "hologram" undeclared`)
}

func TestReport_IsDeterministicAndSorted(t *testing.T) {
	t.Parallel()
	build := func() *census.Census {
		c := newCensus(t)
		add(t, c, docOf(&ir.Model{Abstract: true}, &ir.Primitive{Prim: "zzz"}, &ir.Primitive{Prim: "aaa"}))
		add(t, c, docOf(&ir.Union{}, &ir.Primitive{Prim: ir.PrimString}))
		return c
	}
	first, second := build().Report(), build().Report()
	if diff := cmp.Diff(first, second); diff != "" {
		t.Errorf("two runs differ (-first +second):\n%s", diff)
	}

	var members, values []string
	for line := range strings.Lines(first) {
		fields := strings.Fields(line)
		switch fields[0] {
		case "member":
			members = append(members, fields[3])
		case "value":
			values = append(values, fields[3]+" "+fields[4])
		}
	}
	assert.True(t, slices.IsSorted(members), "members are in name order")
	assert.True(t, slices.IsSorted(values), "values are in type order, then value order")
	assert.Contains(t, values, `PrimKind "aaa"`)
	assert.Less(t, slices.Index(values, `PrimKind "aaa"`), slices.Index(values, `PrimKind "bool"`),
		"an undeclared value sorts among the declared ones")
	assert.Less(t, slices.Index(values, `PrimKind "string"`), slices.Index(values, `PrimKind "zzz"`))
}

func TestCensus_ReadsOnlyWhatTheWalkMayRead(t *testing.T) {
	t.Parallel()
	type holder struct {
		Open   any
		Kinded ir.TypeDef
		hidden map[string]ir.TypeDef
	}
	c := newCensus(t)
	c.AddAny(&holder{
		Open:   &ir.Model{},
		Kinded: (*ir.Model)(nil),
		hidden: map[string]ir.TypeDef{"a": &ir.Model{}},
	})
	c.AddAny(&holder{})

	got := facts(t, c.Report())
	assert.Equal(t, "2 truncated 0", got["documents"])
	for fact, n := range got {
		if strings.HasPrefix(fact, "value TypeKind ") {
			assert.Equal(t, "0 0", n, "no TypeDef was both present and readable: %s", fact)
		}
	}
}
