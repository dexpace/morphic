package nodeview_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// TestMappingPairs_ReadsWhatTheModelReads holds MappingPairs and Child to the
// parser's model, which is what they exist to agree with
// (docs/parser-library.md#merge-keys, #duplicate-key, #non-scalar-key). Each
// case is a schema's properties, written after the anchors it merges: the
// model's property keys, in order, and the title of each one's schema are what
// the view must present.
func TestMappingPairs_ReadsWhatTheModelReads(t *testing.T) {
	t.Parallel()
	const anchors = "  m0: &m0 {k: {title: m0k}, z: {title: m0z}}\n" +
		"  m1: &m1 {z: {title: m1z}, y: {title: m1y}}\n" +
		"  m2: &m2 {<<: *m0, w: {title: m2w}}\n" +
		"  dup: &dup {d: {title: first}, d: {title: second}}\n"
	cases := []struct {
		name, props string
		want        []string
	}{
		{"merged pairs come first", "{a: {title: a}, <<: *m0}", []string{"k=m0k", "z=m0z", "a=a"}},
		{"an own key overrides a merged one", "{<<: *m0, k: {title: own}}", []string{"z=m0z", "k=own"}},
		{"an earlier source wins a shared key", "{<<: [*m0, *m1]}", []string{"k=m0k", "z=m0z", "y=m1y"}},
		{"every merge key merges, in order", "{<<: *m1, a: {title: a}, <<: *m0}", []string{"z=m1z", "y=m1y", "k=m0k", "a=a"}},
		{"a source's own merge comes first in it", "{<<: *m2}", []string{"k=m0k", "z=m0z", "w=m2w"}},
		{"an own key written twice is read at its last copy", "{d: {title: first}, b: {title: b}, d: {title: second}}",
			[]string{"b=b", "d=second"}},
		{"a source's key written twice is read at its first copy", "{<<: *dup}", []string{"d=first"}},
		{"a merge naming a scalar merges nothing", "{<<: 5, a: {title: a}}", []string{"a=a"}},
		{"a key that is no scalar is the empty string", "{? [x] : {title: list}, a: {title: a}}",
			[]string{"=list", "a=a"}},
		{"an alias key is the key it names", "{*ka : {title: aliased}, b: {title: b}}", []string{"a=aliased", "b=b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := propertiesSource(anchors, tc.props)
			assert.Equal(t, tc.want, modelPairs(t, src), "the model's reading")
			assertViewReadsTheModel(t, src)
		})
	}
}

// FuzzMappingPairs_ReadsWhatTheModelReads holds the view to the model over
// properties built at random from the shapes the table above names: own keys,
// alias keys, keys repeated and keys that are no scalar, and `<<` keys naming
// an anchored mapping, a list of them, a mapping written in place or a scalar,
// anchors merging anchors before them. A library release that reads one shape
// otherwise reddens it.
func FuzzMappingPairs_ReadsWhatTheModelReads(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		g := &propsGen{rng: rand.New(rand.NewPCG(seed, 799))}
		assertViewReadsTheModel(t, propertiesSource(g.anchors(), g.mapping(true)))
	})
}

// assertViewReadsTheModel requires MappingPairs to present the pairs the model
// holds for src's properties, in order, and Child to answer each key as they
// do and a key they hold none for with nil.
func assertViewReadsTheModel(t *testing.T, src string) {
	t.Helper()
	want := modelPairs(t, src)
	props := propertiesNode(t, src)
	pairs := nodeview.New().MappingPairs(props)
	got := make([]string, 0, len(pairs))
	for _, p := range pairs {
		got = append(got, p.Key+"="+titleOf(p.Val))
		assert.Same(t, p.Val, nodeview.Child(props, p.Key), "Child(%q) answers as the pairs do\n%s", p.Key, src)
	}
	require.Equal(t, want, got, "the view reads the model's pairs\n%s", src)
	assert.Nil(t, nodeview.Child(props, "absent"), "a key nothing writes holds nothing\n%s", src)
}

// propertiesSource is a document whose schema S writes props as its
// properties, after anchors under x-anchors and the scalar anchors &ka..&ke
// an alias key names.
func propertiesSource(anchors, props string) string {
	return "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"x-keys: [&ka a, &kb b, &kc c, &kd d, &ke e]\n" +
		"x-anchors:\n  none: {}\n" + anchors +
		"components:\n  schemas:\n    S:\n      properties: " + props + "\n"
}

// modelPairs returns S's properties as the parser's model holds them, each as
// key=title, in order.
func modelPairs(t *testing.T, src string) []string {
	t.Helper()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err, src)
	s, ok := doc.Components.Schemas.Get("S")
	require.True(t, ok, src)
	// Empty, not nil, where the model holds none, as the view's reading is.
	out := make([]string, 0, s.GetSchema().GetProperties().Len())
	for key, prop := range s.GetSchema().GetProperties().All() {
		out = append(out, key+"="+prop.GetSchema().GetTitle())
	}
	return out
}

// propertiesNode returns the raw properties mapping of S in src.
func propertiesNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	path, complete := nodeview.New().DocumentPath(nodeview.YAMLDoc(t, src), "/components/schemas/S/properties")
	require.True(t, complete, src)
	return path[len(path)-1]
}

// titleOf is the title a property's schema node writes, as the view reads it.
func titleOf(schema *yaml.Node) string {
	if title := nodeview.Child(schema, "title"); title != nil {
		return title.Value
	}
	return ""
}

// propsGen writes random properties mappings and the anchors they merge. Every
// title is unique, so a pair the view reads from the wrong copy or source
// shows in what it presents.
type propsGen struct {
	rng    *rand.Rand
	titles int
	named  []string
}

// title returns a property schema with a title no other one has.
func (g *propsGen) title() string {
	g.titles++
	return fmt.Sprintf("{title: t%d}", g.titles)
}

// anchors writes up to five anchored mappings, each of which may merge those
// before it, and returns them as x-anchors entries.
func (g *propsGen) anchors() string {
	var b strings.Builder
	for i := range g.rng.IntN(5) + 1 {
		name := fmt.Sprintf("m%d", i)
		fmt.Fprintf(&b, "  %s: &%s %s\n", name, name, g.mapping(false))
		g.named = append(g.named, name)
	}
	return b.String()
}

// mapping writes one flow mapping of up to six pairs. Only the properties
// themselves write a key that is no scalar: an anchored source writing one
// would merge a pair the view keys alike, adding nothing a case lacks.
func (g *propsGen) mapping(top bool) string {
	pairs := make([]string, 0, 6)
	for range g.rng.IntN(6) + 1 {
		pairs = append(pairs, g.pair(top))
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// pair writes one pair: an own key, plain or aliased, from a small alphabet so
// keys repeat, or a `<<` merge.
func (g *propsGen) pair(top bool) string {
	keys := []string{"a", "b", "c", "d", "e"}
	switch roll := g.rng.IntN(10); {
	case roll < 4:
		return keys[g.rng.IntN(len(keys))] + ": " + g.title()
	case roll < 6:
		return "*k" + keys[g.rng.IntN(len(keys))] + " : " + g.title()
	case roll < 7 && top:
		return "? [x] : " + g.title()
	default:
		return "<<: " + g.mergeValue()
	}
}

// mergeValue writes what a `<<` names: an anchor written before, a list of
// them, a mapping written in place, or a scalar, which merges nothing.
func (g *propsGen) mergeValue() string {
	switch roll := g.rng.IntN(8); {
	case roll < 3 && len(g.named) > 0:
		return "*" + g.named[g.rng.IntN(len(g.named))]
	case roll < 5 && len(g.named) > 0:
		names := slices.Clone(g.named)
		g.rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
		names = names[:g.rng.IntN(len(names))+1]
		return "[*" + strings.Join(names, ", *") + "]"
	case roll < 7:
		return "{" + []string{"a", "b", "c"}[g.rng.IntN(3)] + ": " + g.title() + "}"
	default:
		return "5"
	}
}
