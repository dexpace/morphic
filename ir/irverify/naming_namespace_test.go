package irverify

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// namespaceDoc builds a document whose one TypeCommon owner — a Model — and one
// Service owner carry the given namespace paths, and nothing else: the model is
// keyed by its own ID, the service is given a non-empty one, and each name is the
// neutral sequence the grammar derives from its Source, so the only violations
// the fixture draws are the namespace ones it is about.
func namespaceDoc(typeNamespace, serviceNamespace []string) *ir.Document {
	m := &ir.Model{
		TypeCommon: ir.TypeCommon{
			ID:        "t/x/A",
			Name:      ir.Naming{Source: "a", Canonical: "a"},
			Namespace: typeNamespace,
		},
	}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types:     ir.TypeRegistry{m.ID: m},
		Services: []ir.Service{{
			ID:        "s/x/S",
			Name:      ir.Naming{Source: "s", Canonical: "s"},
			Namespace: serviceNamespace,
		}},
	}
}

// TestVerify_BlankNamespaceSegmentIsAViolation covers the first of the two list
// rules a namespace has, on both owners. An entry made only of runes Unicode
// calls invisible is as blank as "", and each is reported at the segment itself
// so the repair names the entry to fill in rather than the owner to go looking
// through (GitHub #399).
func TestVerify_BlankNamespaceSegmentIsAViolation(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		doc  *ir.Document
		base string
	}{
		"TypeCommon": {namespaceDoc([]string{"", " ", "\u3164"}, nil), "doc.Types[t/x/A].Namespace"},
		"Service":    {namespaceDoc(nil, []string{"", " ", "\u3164"}), "doc.Services[0].Namespace"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := Verify(tc.doc)
			require.Len(t, got, 3, "one violation per blank segment")
			for i, v := range got {
				assert.Equal(t, "ir/namespace-blank", v.Code)
				assert.Equal(t, fmt.Sprintf("%s[%d]", tc.base, i), v.Path,
					"the violation names the offending segment, not just the owner")
				assert.Equal(t, "namespace segment is blank, so it names no package or module",
					v.Message, "the blank complaint is not the spelling, so nothing is quoted")
			}
		})
	}
}

// TestVerify_RepeatedNamespaceSegmentIsAViolation covers the second. A segment
// that repeats an earlier one admits nothing the earlier did, so the repair is
// to delete the later entry — which is why the violation is reported there and
// names the earlier index rather than merely counting the pair (GitHub #399).
func TestVerify_RepeatedNamespaceSegmentIsAViolation(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		doc  *ir.Document
		base string
	}{
		"TypeCommon": {namespaceDoc([]string{"dup", "dup"}, nil), "doc.Types[t/x/A].Namespace"},
		"Service":    {namespaceDoc(nil, []string{"dup", "dup"}), "doc.Services[0].Namespace"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := Verify(tc.doc)
			require.Len(t, got, 1, "the repeat is reported, not the first segment")
			assert.Equal(t, "ir/namespace-duplicate", got[0].Code)
			assert.Equal(t, tc.base+"[1]", got[0].Path)
			assert.Equal(t, `namespace segment "dup" is listed here and at index 0`, got[0].Message)
		})
	}
}

// TestVerify_NamespacePathIsSpelledAsTheWalkWould ties the hand-assembled
// namespace path to ir.WalkValues' own grammar, the way
// TestVerify_AliasPathIsSpelledAsTheWalkWould ties the alias one. The walk does
// reach a namespace segment — checkNaming does not prune at an owner — but the
// reported path is built by hand from path+namespaceField, so the two spellings
// can drift with nothing between them. Were ir's slice-index rendering to
// change, every walk-produced path would move while this code alone kept the old
// spelling, and this is what would say so.
//
// Past the single digits too, which is where a hand-built path and a formatted
// one last agree.
func TestVerify_NamespacePathIsSpelledAsTheWalkWould(t *testing.T) {
	t.Parallel()
	const size = 12
	segments := make([]string, size)
	for i := range segments {
		segments[i] = fmt.Sprintf("segment.%d", i) // distinct, so no segment repeats
	}
	doc := namespaceDoc(segments, nil)

	walked := map[string]string{} // segment → the path the walk renders for it
	ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() == reflect.String && v.String() != "" {
			walked[v.String()] = path
		}
		return true
	})
	for _, segment := range segments {
		require.Contains(t, walked, segment, "the walk reaches every segment when nothing prunes it")
	}

	// Blank every segment so the check reports one violation per index, then hold
	// each reported path to the one the walk rendered at that same index.
	got := Verify(namespaceDoc(make([]string, size), nil))
	require.Len(t, got, size)
	paths := make([]string, len(got))
	for i, v := range got {
		paths[i] = v.Path
	}
	want := make([]string, 0, size)
	for _, segment := range segments {
		want = append(want, walked[segment])
	}
	assert.ElementsMatch(t, want, paths)
}

// TestNamespaceOwners_CoverEveryNamespaceField holds namespaceOwners to the IR:
// every []string field named Namespace a Document can reach must have a
// declaring type with an entry in the map. Without this, a node type that
// declares a namespace path without an entry is verified by nothing — and
// nothing says so, because the map and the fields would simply disagree.
//
// A promoted field is its declaring type's, not the kind's: every TypeDef kind
// carries TypeCommon's Namespace at the owner path, and the walk matches the
// TypeCommon there, so the map is keyed by the declaring type. Reading the
// direct fields is what says so; FieldByName would report the eleven kinds as
// owners of a field they never declared. The precondition is the other half: a
// walk that reached no such field proves nothing about the ones it missed.
func TestNamespaceOwners_CoverEveryNamespaceField(t *testing.T) {
	t.Parallel()
	var found int
	for _, st := range reachableStructs(t) {
		for f := range st.Fields() {
			if f.Name != "Namespace" || f.Type.Kind() != reflect.Slice || f.Type.Elem().Kind() != reflect.String {
				continue
			}
			found++
			assert.True(t, namespaceOwners[st],
				"%s declares a []string Namespace with no owner in namespaceOwners", st)
		}
	}
	require.Positive(t, found, "the walk must reach the Namespace fields this holds the map to")
}

// reachableStructs returns every struct type reachable from Document and from
// each TypeDef kind through fields, pointers, slices, arrays and maps. The
// worklist is bounded by the finite set of types the ir package declares.
//
// The kinds are seeded because a TypeDef is an interface, which reflection
// cannot descend — the same reason ir/tags_test.go's twin seeds them, and the
// reason the seed is derived from the TypeKind constants rather than written out
// here: a kind added to the IR joins the walk the moment it exists.
func reachableStructs(t *testing.T) []reflect.Type {
	t.Helper()
	queue := []reflect.Type{reflect.TypeFor[ir.Document]()}
	for _, k := range declaredConstsOfType(t, "TypeKind") {
		td, ok := ir.NewTypeDef(ir.TypeKind(k.value))
		require.True(t, ok, "NewTypeDef(%s)", k.value)
		queue = append(queue, reflect.TypeOf(td))
	}
	seen := map[reflect.Type]bool{}
	var out []reflect.Type
	for len(queue) > 0 {
		typ := queue[0]
		queue = queue[1:]
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Map {
			queue = append(queue, typ.Key(), typ.Elem())
			continue
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			continue
		}
		seen[typ] = true
		out = append(out, typ)
		for f := range typ.Fields() {
			queue = append(queue, f.Type)
		}
	}
	return out
}
