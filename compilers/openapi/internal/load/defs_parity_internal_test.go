package load

import (
	"encoding/json/jsontext"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
)

// parityDocs and parityDepth bound the grammar's sample: how many documents it
// draws and how deeply a schema nests.
const (
	parityDocs  = 250
	parityDepth = 3
)

// parityRefs are the "#/$defs/..." pointers the grammar spells: a definition, a
// path into one, and a definition nested in another.
var parityRefs = []string{"#/$defs/m", "#/$defs/n", "#/$defs/m/properties/p", "#/$defs/n/$defs/m", "#/$defs/m/$defs/n"}

// parityGrammar draws schemas that spell "#/$defs/..." references, $defs and
// $id at random, a reference as a property or as a schema keyword's own value.
// A definition spells no reference, so nothing it holds can close a cycle, and
// every $id is its own.
type parityGrammar struct {
	rng *rand.Rand
	ids int
}

// schema returns a flow-style schema nesting at most depth levels deeper.
func (g *parityGrammar) schema(depth int, inDefs bool) string {
	var fields []string
	if !inDefs && g.rng.Intn(2) == 0 {
		fields = append(fields, "$ref: "+strconv.Quote(parityRefs[g.rng.Intn(len(parityRefs))]))
	} else {
		fields = append(fields, "type: object")
	}
	if g.rng.Intn(3) == 0 {
		g.ids++
		fields = append(fields, "$id: "+strconv.Quote("https://x.test/s"+strconv.Itoa(g.ids)))
	}
	if depth > 0 && g.rng.Intn(2) == 0 {
		fields = append(fields, "$defs: "+g.keyed(depth, true, "m", "n"))
	}
	if depth > 0 && g.rng.Intn(2) == 0 {
		fields = append(fields, "properties: "+g.keyed(depth, inDefs, "p", "q"))
	}
	if depth > 0 && g.rng.Intn(4) == 0 { // a child that is a schema itself, whose parent is this one
		fields = append(fields, []string{"items", "not"}[g.rng.Intn(2)]+": "+g.schema(depth-1, inDefs))
	}
	return "{" + strings.Join(fields, ", ") + "}"
}

// keyed returns a mapping of a random, ordered subset of keys to schemas one
// level shallower than depth.
func (g *parityGrammar) keyed(depth int, inDefs bool, keys ...string) string {
	g.rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	entries := make([]string, 1+g.rng.Intn(len(keys)))
	for i := range entries {
		entries[i] = keys[i] + ": " + g.schema(depth-1, inDefs)
	}
	return "{" + strings.Join(entries, ", ") + "}"
}

// document returns an OpenAPI document of up to three such component schemas.
func (g *parityGrammar) document() string {
	names := []string{"A", "B", "C"}[:1+g.rng.Intn(3)]
	var sb strings.Builder
	sb.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n")
	for _, name := range names {
		sb.WriteString("    " + name + ": " + g.schema(parityDepth, false) + "\n")
	}
	return sb.String()
}

// resolverTarget returns where the resolver reads the idx'th held reference of
// spec when that reference is the first it meets, and whether it finds one. The
// document is parsed afresh, so nothing is cached from an earlier reference.
func resolverTarget(t *testing.T, spec string, idx int) (jsontext.Pointer, bool) {
	t.Helper()
	doc, root := buildDoc(t, spec)
	var at jsontext.Pointer
	var found bool
	var seen int
	for item := range soa.Walk(t.Context(), doc) {
		require.NoError(t, item.Match(soa.Matcher{Schema: func(js *schemaRef) error {
			if _, held := heldDefsPointer(js); !held {
				return nil
			}
			if seen == idx {
				_, err := js.Resolve(t.Context(), references.ResolveOptions{TargetLocation: "spec.yaml", RootDocument: doc})
				if info := js.GetReferenceResolutionInfo(); err == nil && info != nil && info.Object != nil {
					at, found = jsontext.Pointer(info.Object.GetCore().GetJSONPointer(root)), true
				}
			}
			seen++
			return nil
		}}))
	}
	return at, found
}

// TestHeldRefs_AgreeWithTheResolverOnTheFirstReferenceItMeets pins the claim the
// rule rests on: for a reference the resolver meets first, defs.Target names the
// definition the resolver reads, and none exactly where it reads none. What the
// resolver does with a later reference to the same pointer is order-dependent,
// which is why load holds these references out; this is the one reading that is
// not. A change to the library that moves the resolver's reading fails here
// rather than leaving the rule silently describing a resolver that is gone.
func TestHeldRefs_AgreeWithTheResolverOnTheFirstReferenceItMeets(t *testing.T) {
	t.Parallel()
	g := &parityGrammar{rng: rand.New(rand.NewSource(1))}
	var resolved, unresolved int
	for range parityDocs {
		spec := g.document()
		doc, _ := buildDoc(t, spec)
		for i, r := range heldRefs(t.Context(), doc, defs.NewReader(doc)) {
			want, found := resolverTarget(t, spec, i)
			assert.Equal(t, found, r.target != nil, "%s: whether %s finds a definition\n%s", r.site, *r.written, spec)
			if !found {
				unresolved++
				continue
			}
			resolved++
			assert.Equal(t, want, r.at, "%s: where %s finds it\n%s", r.site, *r.written, spec)
		}
	}
	assert.Greater(t, resolved, 100, "references the sample resolves")
	assert.Greater(t, unresolved, 100, "references the sample leaves unresolved")
}
