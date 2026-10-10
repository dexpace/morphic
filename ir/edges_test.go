package ir_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
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
// reaches from td, with the paths it reaches it at, except td's own ID.
func reflectedEdges(td ir.TypeDef) map[ir.TypeID][]string {
	want := map[ir.TypeID][]string{}
	ir.WalkValues(td, "<root>", func(v reflect.Value, path string) bool {
		if v.Kind() == reflect.String && v.Type() == typeIDReflect && v.String() != "" && path != "<root>.ID" {
			id := ir.TypeID(v.String())
			want[id] = append(want[id], path)
		}
		return true
	})
	return want
}

// edgeDiff compares the IDs TypeEdges yields for td with the oracle and returns
// one line per disagreement, naming the kind and the walk path.
func edgeDiff(td ir.TypeDef) []string {
	want := reflectedEdges(td)
	got := map[ir.TypeID]bool{}
	for id := range ir.TypeEdges(td) {
		got[id] = true
	}
	var out []string
	for id, paths := range want {
		if !got[id] {
			out = append(out, fmt.Sprintf("%s: missing %q at %s", td.Kind(), id, strings.Join(paths, ", ")))
		}
	}
	for id := range got {
		if _, ok := want[id]; !ok {
			out = append(out, fmt.Sprintf("%s: extra %q", td.Kind(), id))
		}
	}
	sort.Strings(out)
	return out
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
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		var doc ir.Document
		require.NoError(t, json.Unmarshal(data, &doc), f)
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
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: "self"}, Base: &ir.TypeRef{Target: "self"}}
	assert.Equal(t, []ir.TypeID{"self"}, slices.Collect(ir.TypeEdges(m)))
	assert.Empty(t, edgeDiff(m))
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

func TestTypeEdges_ValueNestingIsBounded(t *testing.T) {
	t.Parallel()
	deep := ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/deep"}}
	for range ir.MaxWalkDepth + 10 {
		deep = ir.Value{Kind: ir.ValueList, List: []ir.Value{deep}}
	}
	shallow := ir.Value{Kind: ir.ValueList, List: []ir.Value{{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/shallow"}}}}
	lit := &ir.Literal{Value: ir.Value{Kind: ir.ValueList, List: []ir.Value{deep, shallow}}}
	assert.Equal(t, []ir.TypeID{"t/shallow"}, slices.Collect(ir.TypeEdges(lit)))
}

func TestTypeEdges_ArgumentWithoutValueFromYieldsItsType(t *testing.T) {
	t.Parallel()
	m := &ir.Model{Properties: []ir.Property{{
		Args: []ir.Parameter{{Type: ir.TypeRef{Target: "t/arg"}}},
	}}}
	assert.Equal(t, []ir.TypeID{"t/arg"}, slices.Collect(ir.TypeEdges(m)))
}
