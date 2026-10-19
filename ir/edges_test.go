package ir_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// typeIDReflect is the reflect.Type of ir.TypeID.
var typeIDReflect = reflect.TypeFor[ir.TypeID]()

// fillDepth is how many times the filler enters one struct type on a single
// path, which is how deep it nests ir.Value.
const fillDepth = 3

// filler populates a value reflectively: every reachable TypeID gets a distinct
// ID, every pointer is allocated, and every slice and map gets one element.
type filler struct {
	next  int
	stack map[reflect.Type]int
}

func newFilled(t *testing.T, kind ir.TypeKind) ir.TypeDef {
	t.Helper()
	td, ok := ir.NewTypeDef(kind)
	require.True(t, ok, "no concrete type for kind %q", kind)
	f := &filler{stack: map[reflect.Type]int{}}
	f.fill(reflect.ValueOf(td).Elem())
	return td
}

func (f *filler) id() string {
	f.next++
	return fmt.Sprintf("t/filled-%d", f.next)
}

func (f *filler) fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.Type() == typeIDReflect {
			v.SetString(f.id())
		}
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		f.fill(v.Elem())
	case reflect.Struct:
		f.fillStruct(v)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		f.fill(v.Index(0))
	case reflect.Map:
		f.fillMap(v)
	default:
		// Numbers, bools and the like carry no TypeID.
	}
}

func (f *filler) fillStruct(v reflect.Value) {
	t := v.Type()
	if f.stack[t] >= fillDepth {
		return
	}
	f.stack[t]++
	defer func() { f.stack[t]-- }()
	for i := range v.NumField() {
		if t.Field(i).IsExported() {
			f.fill(v.Field(i))
		}
	}
}

func (f *filler) fillMap(v reflect.Value) {
	if v.Type().Key().Kind() != reflect.String {
		return
	}
	key := reflect.New(v.Type().Key()).Elem()
	key.SetString(fmt.Sprintf("key%d", f.next))
	elem := reflect.New(v.Type().Elem()).Elem()
	f.fill(elem)
	v.Set(reflect.MakeMap(v.Type()))
	v.SetMapIndex(key, elem)
}

// reflectedEdges is the oracle: every non-empty TypeID the reflection walk
// reaches from td except td's own ID, in the order it reaches them, with the
// path of each.
func reflectedEdges(td ir.TypeDef) (ids []ir.TypeID, paths []string) {
	ir.WalkValues(td, "<root>", func(v reflect.Value, path string) bool {
		if v.Kind() == reflect.String && v.Type() == typeIDReflect && v.String() != "" && path != "<root>.ID" {
			ids = append(ids, ir.TypeID(v.String()))
			paths = append(paths, path)
		}
		return true
	})
	return ids, paths
}

// edgeDiff compares the IDs TypeEdges yields for td with the oracle, ID for ID
// and in order, and returns "" or a line naming the first disagreement and the
// walk path it concerns.
func edgeDiff(td ir.TypeDef) string {
	want, paths := reflectedEdges(td)
	got := slices.Collect(ir.TypeEdges(td))
	if slices.Equal(want, got) {
		return ""
	}
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	switch {
	case i == len(got):
		return fmt.Sprintf("%s: edge %d missing: the walk reaches %q at %s", td.Kind(), i, want[i], paths[i])
	case i == len(want):
		return fmt.Sprintf("%s: edge %d extra: TypeEdges yields %q, the walk reaches nothing more", td.Kind(), i, got[i])
	default:
		return fmt.Sprintf("%s: edge %d is %q, but the walk reaches %q there, at %s", td.Kind(), i, got[i], want[i], paths[i])
	}
}

func TestTypeEdges_EveryKindHasAnArm(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td := newFilled(t, k)
			assert.Empty(t, edgeDiff(td))
			assert.NotEmpty(t, slices.Collect(ir.TypeEdges(td)), "a filled %s yields at least its common edges", k)
		})
	}
}

// corpusTypeDefs decodes every golden document under the OpenAPI corpora and
// returns their type definitions.
func corpusTypeDefs(t *testing.T) []ir.TypeDef {
	t.Helper()
	var files []string
	for _, glob := range []string{
		"../testdata/golden/openapi/*.golden.json",
		"../testdata/conformance/openapi/*.golden.json",
	} {
		matches, err := filepath.Glob(glob)
		require.NoError(t, err)
		require.NotEmpty(t, matches, "glob %s matched no file", glob)
		files = append(files, matches...)
	}
	defs := make([]ir.TypeDef, 0, len(files))
	for _, f := range files {
		doc, err := irtest.ReadGolden(f)
		require.NoError(t, err)
		for _, td := range doc.Types {
			defs = append(defs, td)
		}
	}
	return defs
}

func TestTypeEdges_MatchesTheReflectionWalk(t *testing.T) {
	t.Parallel()
	total := 0
	for _, td := range corpusTypeDefs(t) {
		assert.Empty(t, edgeDiff(td), "type %s", td.Common().ID)
		total += len(slices.Collect(ir.TypeEdges(td)))
	}
	require.Positive(t, total, "the corpus must exercise at least one edge")
}

func TestTypeEdges_SelfReferenceStillCounts(t *testing.T) {
	t.Parallel()
	m := &ir.Model{ID: "self", Base: &ir.TypeRef{Target: "self"}}
	assert.Equal(t, []ir.TypeID{"self"}, slices.Collect(ir.TypeEdges(m)))
	assert.Empty(t, edgeDiff(m))
}

// TestTypeEdges_SharedPointerIsFollowedFromEachField pins where TypeEdges and
// its oracle differ: the reflection walk enters a shared pointer once, while
// TypeEdges reads the tree the document's JSON encodes, one edge per field.
func TestTypeEdges_SharedPointerIsFollowedFromEachField(t *testing.T) {
	t.Parallel()
	v := &ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/shared"}}
	m := &ir.Model{Properties: []ir.Property{{Default: v}, {Default: v}}}
	assert.Equal(t, []ir.TypeID{"t/shared", "t/shared"}, slices.Collect(ir.TypeEdges(m)))
	assert.NotEmpty(t, edgeDiff(m), "the reflection walk reaches the shared value once")
}

func TestTypeEdges_StopsWhenTheConsumerBreaks(t *testing.T) {
	t.Parallel()
	for _, k := range allKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			td := newFilled(t, k)
			full := slices.Collect(ir.TypeEdges(td))
			for n := 1; n <= len(full); n++ {
				var got []ir.TypeID
				stopped := false
				ir.TypeEdges(td)(func(id ir.TypeID) bool {
					require.False(t, stopped, "yield called again after it returned false")
					got = append(got, id)
					stopped = len(got) == n
					return !stopped
				})
				require.Equal(t, full[:n], got, "stopping after %d edges", n)
			}
		})
	}
}

func TestTypeEdges_NilYieldsNothing(t *testing.T) {
	t.Parallel()
	var nilModel *ir.Model
	var nilAny *ir.Any
	assert.Empty(t, slices.Collect(ir.TypeEdges(nil)))
	assert.Empty(t, slices.Collect(ir.TypeEdges(nilModel)))
	assert.Empty(t, slices.Collect(ir.TypeEdges(nilAny)))
}

func TestTypeEdges_NeverYieldsAnEmptyTarget(t *testing.T) {
	t.Parallel()
	m := &ir.Model{
		Base:       &ir.TypeRef{},
		Implements: []ir.TypeRef{{}},
		Properties: []ir.Property{{Default: &ir.Value{Ref: &ir.ValueRef{}, Ctor: &ir.CtorValue{}}}},
		Discriminator: &ir.Discriminator{
			Mapping: map[string]ir.TypeID{"x": ""},
		},
	}
	assert.Empty(t, slices.Collect(ir.TypeEdges(m)))
}

func TestTypeEdges_DiscriminatorMappingInSortedOrder(t *testing.T) {
	t.Parallel()
	u := &ir.Union{Discriminator: &ir.Discriminator{
		Mapping: map[string]ir.TypeID{"b": "t/b", "a": "t/a", "c": "t/c"},
		Default: "t/default",
	}}
	assert.Equal(t, []ir.TypeID{"t/a", "t/b", "t/c", "t/default"}, slices.Collect(ir.TypeEdges(u)))
}

// nestingArms wraps a value one level deeper through each way a Value holds
// another.
var nestingArms = map[string]func(ir.Value) ir.Value{
	"list": func(v ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: []ir.Value{v}} },
	"object": func(v ir.Value) ir.Value {
		return ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "k", Value: v}}}
	},
	"ctor": func(v ir.Value) ir.Value {
		return ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{Args: []ir.Value{v}}}
	},
}

// nestedLiteral returns a literal whose value reaches a ref to "t/deep" through
// levels nested values.
func nestedLiteral(wrap func(ir.Value) ir.Value, levels int) *ir.Literal {
	v := ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/deep"}}
	for range levels {
		v = wrap(v)
	}
	return &ir.Literal{Value: v}
}

func TestTypeEdges_ValueNestingStopsPastTheBound(t *testing.T) {
	t.Parallel()
	for arm, wrap := range nestingArms {
		t.Run(arm, func(t *testing.T) {
			t.Parallel()
			at := nestedLiteral(wrap, ir.MaxWalkDepth)
			assert.Equal(t, []ir.TypeID{"t/deep"}, slices.Collect(ir.TypeEdges(at)), "a value at the bound is entered")
			past := nestedLiteral(wrap, ir.MaxWalkDepth+1)
			assert.Empty(t, slices.Collect(ir.TypeEdges(past)), "a value past the bound is not")
		})
	}
}

// TestTypeEdges_OnlyTruncatesWhereTheWalkAlreadyHas holds the reason TypeEdges
// can stop silently: every level of a value costs the reflection walk at least
// two steps against TypeEdges' one, so a value deep enough to cut TypeEdges has
// already cut WalkValues, whose truncation pass.Validate reports.
func TestTypeEdges_OnlyTruncatesWhereTheWalkAlreadyHas(t *testing.T) {
	t.Parallel()
	for arm, wrap := range nestingArms {
		t.Run(arm, func(t *testing.T) {
			t.Parallel()
			lit := nestedLiteral(wrap, ir.MaxWalkDepth+1)
			require.Empty(t, slices.Collect(ir.TypeEdges(lit)), "the value is past TypeEdges' bound")
			assert.True(t, ir.WalkValues(lit, "<root>", func(reflect.Value, string) bool { return true }),
				"WalkValues must have truncated on it too")
		})
	}
}

func TestTypeEdges_ArgumentWithoutValueFromYieldsItsType(t *testing.T) {
	t.Parallel()
	m := &ir.Model{Properties: []ir.Property{{
		Args: []ir.Parameter{{Type: ir.TypeRef{Target: "t/arg"}}},
	}}}
	assert.Equal(t, []ir.TypeID{"t/arg"}, slices.Collect(ir.TypeEdges(m)))
}

// edgeLiteral returns a literal holding v, and the edges TypeEdges yields from it.
func edgeLiteral(v ir.Value) []ir.TypeID {
	return slices.Collect(ir.TypeEdges(&ir.Literal{Value: v}))
}

// TestTypeEdges_ASelfContainingValueIsFollowedOnce holds TypeEdges to ending on a
// value that contains itself, for each of the three ways a value can: a
// constructor that is its own argument, a list that is its own element, an
// object that is its own member. Without the guard each is followed to the
// bound, yielding the edge beside the cycle once per level; with it, once.
func TestTypeEdges_ASelfContainingValueIsFollowedOnce(t *testing.T) {
	t.Parallel()
	leaf := ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/leaf"}}

	ctor := &ir.CtorValue{Scalar: "t/scalar"}
	ctor.Args = []ir.Value{leaf, {Kind: ir.ValueCtor, Ctor: ctor}}

	list := []ir.Value{leaf, {}}
	list[1] = ir.Value{Kind: ir.ValueList, List: list}

	object := []ir.Field{{Name: "a", Value: leaf}, {}}
	object[1].Value = ir.Value{Kind: ir.ValueObject, Object: object}

	for _, tt := range []struct {
		name string
		v    ir.Value
		want []ir.TypeID
	}{
		{"a constructor", ir.Value{Kind: ir.ValueCtor, Ctor: ctor}, []ir.TypeID{"t/scalar", "t/leaf"}},
		{"a list", ir.Value{Kind: ir.ValueList, List: list}, []ir.TypeID{"t/leaf"}},
		{"an object", ir.Value{Kind: ir.ValueObject, Object: object}, []ir.TypeID{"t/leaf"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, edgeLiteral(tt.v))
		})
	}
}

// TestTypeEdges_SharingWithoutACycleIsFollowedFromEachPlace is the other side:
// a constructor, a list or an object met twice, never from inside itself, is
// followed both times, as is a prefix of a list or an object from inside it.
func TestTypeEdges_SharingWithoutACycleIsFollowedFromEachPlace(t *testing.T) {
	t.Parallel()
	leaf := ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/leaf"}}
	twice := func(v ir.Value) ir.Value {
		return ir.Value{Kind: ir.ValueList, List: []ir.Value{v, v}}
	}

	ctor := ir.Value{Kind: ir.ValueCtor, Ctor: &ir.CtorValue{Scalar: "t/scalar"}}
	list := ir.Value{Kind: ir.ValueList, List: []ir.Value{leaf}}
	object := ir.Value{Kind: ir.ValueObject, Object: []ir.Field{{Name: "a", Value: leaf}}}

	prefix := []ir.Value{leaf, {}}
	prefix[1] = ir.Value{Kind: ir.ValueList, List: prefix[:1]}
	members := []ir.Field{{Name: "a", Value: leaf}, {Name: "b"}}
	members[1].Value = ir.Value{Kind: ir.ValueObject, Object: members[:1]}

	assert.Equal(t, []ir.TypeID{"t/scalar", "t/scalar"}, edgeLiteral(twice(ctor)))
	assert.Equal(t, []ir.TypeID{"t/leaf", "t/leaf"}, edgeLiteral(twice(list)))
	assert.Equal(t, []ir.TypeID{"t/leaf", "t/leaf"}, edgeLiteral(twice(object)))
	assert.Equal(t, []ir.TypeID{"t/leaf", "t/leaf"}, edgeLiteral(ir.Value{Kind: ir.ValueList, List: prefix}))
	assert.Equal(t, []ir.TypeID{"t/leaf", "t/leaf"}, edgeLiteral(ir.Value{Kind: ir.ValueObject, Object: members}))
}
