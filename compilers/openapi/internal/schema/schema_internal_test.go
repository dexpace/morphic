package schema

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestLower_DepthCapExceeded(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("    Deep:\n")
	indent := "      "
	for range maxSchemaDepth + 4 {
		b.WriteString(indent + "type: array\n")
		b.WriteString(indent + "items:\n")
		indent += "  "
	}
	b.WriteString(indent + "type: string\n")
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(b.String()))
	require.NotNil(t, doc)
	var sawCap bool
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && strings.Contains(d.Message, "nesting exceeds") {
			sawCap = true
		}
	}
	assert.True(t, sawCap, "schema depth-cap diagnostic emitted")
}

func TestIsNullSchema_EmptyEitherFalse(t *testing.T) {
	t.Parallel()
	assert.False(t, isNullSchema(openapitest.EmptyEitherSchema()), "empty either is not a null schema")
}

func TestPreserveUnionSiblings_MissingNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// No node registered under the id: the union branches have nowhere to go, so
	// the guard reports the broken invariant instead of dropping them quietly.
	diags := preserveUnionSiblings(l.ctx, l.types, "t/anon/missing", &oas3.Schema{}, "/p", ir.ReasonDegradedLowering, "why")
	assertInternalInvariant(t, diags)
}

// TestAttachDeclaredAnnotations_MissingNode drives the invariant no source can
// break: a pointer owning an ID the registry never registered. Lowering records
// the two together, so this state is a compiler bug — and the annotations it
// would swallow are reported rather than lost.
func TestAttachDeclaredAnnotations_MissingNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// Reached through preserveUnionSiblings rather than attachDeclaredAnnotations:
	// the latter takes its ID from the coordinate map, and compile.Types records a
	// coordinate and its node together, so that caller can no longer present an ID
	// the registry does not hold. This one is handed an ID by its caller.
	diags := preserveUnionSiblings(l.ctx, l.types, "t/anon/missing", &oas3.Schema{}, "/p", ir.ReasonDegradedLowering, "why")
	assertInternalInvariant(t, diags)
}

func TestPreserveKeyword_NilRaw(t *testing.T) {
	t.Parallel()
	var ext ir.Unmodeled
	diags := preserveKeyword(lowering.Ctx{}, &ext, "openapi:not", nil, "/p", "/p/not", "not")
	assert.Nil(t, ext, "nil raw is a no-op")
	assert.Empty(t, diags)
}

// TestSchemaConstraints_NonSchemaInputs covers annotation.SchemaOf's nil and
// boolean early return, plus a well-formed reference besides: none of the three
// leaves a body with constraint keywords on it, so schemaConstraints reports
// none either way.
func TestSchemaConstraints_NonSchemaInputs(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	for _, js := range []*oas3.Schema{
		annotation.SchemaOf(nil),
		annotation.SchemaOf(oas3.NewJSONSchemaFromBool(true)),
		annotation.SchemaOf(oas3.NewJSONSchemaFromReference("#/components/schemas/Other")),
	} {
		var kept ir.Unmodeled
		cons, diags := schemaConstraints(l.ctx, &kept, js, "/p")
		assert.Nil(t, cons)
		assert.Empty(t, diags)
		assert.Empty(t, kept)
	}
}

// TestSchemaConstraints_EmptyRefSchema covers a $ref pointer that is present
// but empty: IsReference is false for it, so annotation.SchemaOf has no early
// return here — the Schema body reaches schemaConstraints, which finds no
// constraint keywords on a body that carries only Ref. The parser never emits
// this shape, so it is built by hand.
func TestSchemaConstraints_EmptyRefSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	emptyRef := references.Reference("")
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: &emptyRef})
	var kept ir.Unmodeled
	cons, diags := schemaConstraints(l.ctx, &kept, annotation.SchemaOf(js), "/p")
	assert.Nil(t, cons)
	assert.Empty(t, diags)
	assert.Empty(t, kept)
}

func TestResolveSchemaRef_ReusesInternedSubSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Intern(deepPointer, "t/anon/prev", func() ir.TypeDef { return &ir.Any{} })

	id, ok, diags := resolveSchemaRef(l.ctx, l.types, &l.anchors, TopLevelDepth, openapitest.EmptyEitherSchema(), "#"+deepPointer)
	require.True(t, ok, "a $ref to an already-hoisted sub-schema reuses its ID")
	assert.Equal(t, ir.TypeID("t/anon/prev"), id)
	assert.Empty(t, diags, "reusing an interned node reports nothing")
}

func TestResolveSchemaRef_UnresolvedDeepRefDropped(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// A same-file $ref to a deep pointer the library never resolved: no interned
	// node, GetResolvedSchema is nil, so the reference is dropped (ok=false).
	js := oas3.NewJSONSchemaFromReference("#" + deepPointer)

	_, ok, diags := resolveSchemaRef(l.ctx, l.types, &l.anchors, TopLevelDepth, js, "#"+deepPointer)
	assert.False(t, ok, "an unresolved deep sub-schema $ref is dropped, not synthesized")
	assert.Empty(t, diags, "the drop is reported by refTypeRef, which owns the pointer to report it at")
}

func TestHoistSubSchema_NilSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	_, ok, diags := hoistSubSchema(l.ctx, l.types, &l.anchors, TopLevelDepth, nil, deepPointer)
	assert.False(t, ok, "a nil resolved sub-schema cannot be hoisted")
	assert.Empty(t, diags)
}

func TestHoistSubSchema_BodyInternsAtPointer(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	// An object body interns a node at the sub-schema's own pointer, so the
	// pointer-owns-a-node branch returns that node rather than aliasing it.
	object := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](
		&oas3.Schema{Type: oas3.NewTypeFromString(oas3.SchemaTypeObject)})

	id, ok, diags := hoistSubSchema(l.ctx, l.types, &l.anchors, TopLevelDepth, object, deepPointer)
	require.True(t, ok)
	assert.Empty(t, diags, "an object body hoists cleanly")
	assert.Equal(t, ids.AnonType(deepPointer), id)
	seeded, _ := l.types.Lookup(deepPointer)
	assert.Equal(t, ids.AnonType(deepPointer), seeded)
}

func TestIsRefBranch_Nil(t *testing.T) {
	t.Parallel()
	assert.False(t, isRefBranch(nil))
}

// TestPreserve_EmptyRawIsRejectedLikeNil pins both halves of the guard. nil and a
// zero-length slice are distinct states, and only nil was screened — so an empty
// payload was written into Unmodeled, where it makes json.Marshal fail for the
// whole document rather than for the entry that carried it.
//
// No committed spec reaches this: every call site passes a value re-encoded from
// a parsed YAML node, which is never zero-length. The guard is exercised directly
// because the hazard is a second compiler copying an asymmetric screen, not a
// live defect in this one.
func TestPreserve_EmptyRawIsRejectedLikeNil(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]ir.RawValue{
		"nil":           nil,
		"empty non-nil": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := &lowerer{}
			var p ir.Unmodeled
			Preserve(l.ctx, &p, "openapi:k", raw, ir.ReasonVendorExtension, "/p/k")
			assert.Nil(t, p, "a payload with no bytes preserves no construct")

			var q ir.Unmodeled
			kwDiags := preserveKeyword(lowering.Ctx{}, &q, "openapi:not", raw, "/p", "/p/not", "not")
			assert.Nil(t, q, "and the validation-only wrapper screens the same states")
			assert.Empty(t, kwDiags, "nothing was preserved, so nothing is announced")
		})
	}
}

// TestDeclaresResourceIDAbove_WithoutARawTree pins the guard on the path walk.
// The walk reads the raw source because $id has no oas3.Schema field, so a
// document with no raw tree behind it — one built in memory, or a pointer naming
// a position the source does not have — must read as "no resource boundary
// found" rather than wander off the tree.
//
// No committed spec reaches this: every pointer a $dynamicRef is lowered at was
// built by walking the very tree this re-walks. The guard is exercised directly
// because the hazard is a later caller passing a synthesized pointer, not a live
// defect.
func TestDeclaresResourceIDAbove_WithoutARawTree(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})

	assert.False(t, declaresResourceIDAbove(l.ctx, "/components/schemas/A"),
		"a document with no raw tree declares no resource anywhere")
}

// TestDynamicAnchors_WalksEveryNodeShape drives the raw-tree walk over the
// shapes a YAML document can present, rather than only the mappings a schema
// happens to be written as. The walk reads the raw tree because oas3.Schema has
// no $dynamicAnchor field, so it meets whatever the source wrote — a sequence, a
// bare scalar, a non-string key — and must index anchors under a sequence while
// declining the rest without wandering off the tree.
func TestDynamicAnchors_WalksEveryNodeShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		node *yaml.Node
		want map[string][]string
	}{
		{"a nil node yields nothing", nil, map[string][]string{}},
		{
			"a bare scalar declares no anchor",
			openapitest.YAMLNode(t, `just-a-string`),
			map[string][]string{},
		},
		{
			"a sequence indexes its elements by ordinal",
			openapitest.YAMLNode(t, "- {$dynamicAnchor: first}\n- {other: 1}\n- {$dynamicAnchor: third}\n"),
			map[string][]string{"first": {"/0"}, "third": {"/2"}},
		},
		{
			"a sequence element standing in for a mapping is followed",
			openapitest.YAMLNode(t, "- &a {$dynamicAnchor: first}\n- *a\n"),
			map[string][]string{"first": {"/0", "/1"}},
		},
		{
			"a non-string key cannot name a keyword and is skipped",
			openapitest.YAMLNode(t, "? [a, b]\n: {$dynamicAnchor: buried}\n$dynamicAnchor: reached\n"),
			map[string][]string{"reached": {""}},
		},
		{
			"an empty anchor name is not indexed",
			openapitest.YAMLNode(t, `{$dynamicAnchor: ""}`),
			map[string][]string{},
		},
		{
			"a non-scalar anchor value is not indexed",
			openapitest.YAMLNode(t, `{$dynamicAnchor: [a]}`),
			map[string][]string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, complete := dynamicAnchors(tc.node)
			assert.Empty(t, cmp.Diff(tc.want, got))
			assert.True(t, complete, "nothing here reaches a walk bound")
		})
	}
}

// TestDynamicAnchors_CountsWhatAnAliasBringsIn pins the count the whole
// expansion rests on. A YAML alias and a `<<` merge key each hand a second
// position the anchor the first one wrote, and the parser downstream reads both
// positions as declaring it — so an index that walked only the spelled-out tree
// would report one declaration where the document has three, and "declared
// exactly once" would expand a reference that is genuinely ambiguous.
func TestDynamicAnchors_CountsWhatAnAliasBringsIn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, source string
		want         []string
	}{
		{
			name:   "an alias re-declares the anchor at its own position",
			source: "A: &base {$dynamicAnchor: tail}\nB: *base\n",
			want:   []string{"/A", "/B"},
		},
		{
			name:   "a merge key does too",
			source: "A: &base {$dynamicAnchor: tail}\nB: {<<: *base, description: merged}\n",
			want:   []string{"/A", "/B"},
		},
		{
			name:   "an explicit key still beats the merged one",
			source: "A: &base {$dynamicAnchor: tail}\nB: {<<: *base, $dynamicAnchor: own}\n",
			want:   []string{"/A"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, complete := dynamicAnchors(openapitest.YAMLNode(t, tc.source))
			assert.True(t, complete)
			assert.Equal(t, tc.want, got["tail"])
		})
	}
}

// TestDynamicAnchors_DocumentNodeUnwraps pins that the walk accepts a whole
// document as well as a content node, since dynamicAnchors is handed whatever
// the loader kept.
func TestDynamicAnchors_DocumentNodeUnwraps(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("$dynamicAnchor: top\n"), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)

	got, complete := dynamicAnchors(&doc)
	assert.Equal(t, map[string][]string{"top": {""}}, got)
	assert.True(t, complete)
}

// TestDynamicAnchors_StopsAtTheDepthCap pins the bound. A raw tree deeper than
// the cap is a legal document, so the walk must stop rather than recur to
// exhaustion; anchors above the cap are still indexed, and the truncation is
// reported rather than passed off as a complete index.
func TestDynamicAnchors_StopsAtTheDepthCap(t *testing.T) {
	t.Parallel()
	deep, shallow := nestedAnchor(maxDynamicAnchorDepth+2), nestedAnchor(0)

	deepIndex, deepComplete := dynamicAnchors(deep)
	assert.NotContains(t, deepIndex, "toodeep",
		"the walk stops at the cap instead of recurring to exhaustion")
	assert.False(t, deepComplete, "and says so, since an unseen anchor undercounts every name")

	shallowIndex, shallowComplete := dynamicAnchors(shallow)
	assert.Contains(t, shallowIndex, "toodeep",
		"an anchor within the cap is still indexed, so the cap is what excluded the other")
	assert.True(t, shallowComplete)
}

// TestAnchorWalk_StopsAtTheNodeBudget pins the other bound. Depth alone stopped
// bounding the walk once it began following aliases, since an alias DAG presents
// many more paths than the tree has nodes; the budget is what caps the total.
func TestAnchorWalk_StopsAtTheNodeBudget(t *testing.T) {
	t.Parallel()
	source := openapitest.YAMLNode(t, "a: {$dynamicAnchor: first}\nb: {$dynamicAnchor: second}\n")

	w := newAnchorWalk(2) // the root mapping and its first value, and no more
	w.walk(source, "", 0)

	assert.True(t, w.truncated, "the budget ran out mid-walk")
	assert.NotContains(t, w.out, "second", "so the anchors past it are missing")
}

// TestDynamicAnchorIndex_ReportsATruncatedWalk pins what a reader is told when
// the index is partial. Truncation only ever drops declarations, and a dropped
// duplicate reads as "declared exactly once" — the count that expands — so an
// unreported truncation would turn an ambiguous reference into a confident one.
func TestDynamicAnchorIndex_ReportsATruncatedWalk(t *testing.T) {
	t.Parallel()
	// The walk descends into every key, so an extension at the document root
	// nests the tree past the cap without the schema lowering ever seeing it.
	deep := strings.Repeat("{a: ", maxDynamicAnchorDepth) + "1" + strings.Repeat("}", maxDynamicAnchorDepth)
	l, diags := loweredFor(t, openapitest.ComponentSpec("    A: {type: string}\n")+"x-deep: "+deep+"\n")
	openapitest.RequireNoErrorDiags(t, diags)

	_, got := l.anchors.sites(l.ctx, "absent")
	require.NotNil(t, l.anchors.byName, "the index is built even when partial")
	require.Len(t, got, 1, "the truncation is announced exactly once: %+v", got)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
	assert.Contains(t, got[0].Message, "not verified")

	_, again := l.anchors.sites(l.ctx, "absent")
	assert.Empty(t, again, "and the cached index does not announce it again")
}

// TestPreserveUnhomedKeywords_MissingNode drives the invariant no source can
// break: an ID a lowering returned that the registry does not hold. The keywords
// it would keep have nowhere to go, so the guard reports the broken invariant
// rather than preserving them onto nothing.
func TestPreserveUnhomedKeywords_MissingNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	got, diags := preserveUnhomedKeywords(l.ctx, l.types, &oas3.Schema{}, "/p", "h", "t/anon/missing", dispatch{})
	assert.Equal(t, ir.TypeID("t/anon/missing"), got, "the lowering's own ID still stands")
	assertInternalInvariant(t, diags)
}

// TestRecordUnhomedKeywords_MissingOwner drives the same invariant one step
// later, where the owning node is the one absent. Its caller cannot produce this
// state — the owner is either the node it just looked up or one internAlias just
// interned — so it is reached directly, as preserveUnionSiblings' guard is.
func TestRecordUnhomedKeywords_MissingOwner(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	diags := recordUnhomedKeywords(l.ctx, l.types, "t/anon/missing", &oas3.Schema{}, []string{"items"}, ir.KindPrimitive, "/p")
	assertInternalInvariant(t, diags)
}

// TestRecordSkippedFamilies_MissingOwner drives the same invariant on the
// keyword families the dispatch passed over. It is reached directly for the
// reason its unhomed-applicator twin is: the caller hands it either the node it
// just looked up or one internAlias just interned, so no source reaches this.
func TestRecordSkippedFamilies_MissingOwner(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	diags := recordSkippedFamilies(l.ctx, l.types, "t/anon/missing", &oas3.Schema{},
		dispatch{won: "const", skipped: []string{"enum"}}, "/p")
	assertInternalInvariant(t, diags)
}

// TestDeclaresFamily_AnUnknownNameDeclaresNothing reaches the arm familyOrder
// cannot reach today, the way TestRecordSkippedFamilies_MissingOwner reaches its
// own: every name dispatchOf passes in is one of the three with a case here.
//
// It guards the direction the default falls. Answering an unknown name from
// another family's guard would let a familyOrder entry added without a case be
// elected on schemas that never wrote it — and a winner lower() has no arm for
// is dropped in silence, which is what this file exists to prevent. Declaring
// nothing keeps such a name out of the election entirely.
func TestDeclaresFamily_AnUnknownNameDeclaresNothing(t *testing.T) {
	t.Parallel()
	withAllOf := &oas3.Schema{AllOf: []*oas3.JSONSchema[oas3.Referenceable]{
		oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{}),
	}}

	require.True(t, declaresFamily(withAllOf, "allOf"), "allOf is named and declared")
	assert.False(t, declaresFamily(withAllOf, "oneOf"),
		"an unknown name must not inherit allOf's guard")
	assert.False(t, declaresFamily(&oas3.Schema{}, "nosuchfamily"))
}

// TestRefNullable_AnUnresolvedRefIsNotNullable pins the guard on the second half
// of the question. A $ref site admits null when its own spelling says so or when
// its target does; a reference the loader never resolved has no target to ask,
// and must read as not-nullable rather than dereference nothing.
func TestRefNullable_AnUnresolvedRefIsNotNullable(t *testing.T) {
	t.Parallel()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(),
		strings.NewReader("$ref: '#/components/schemas/Missing'"), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	require.Nil(t, js.GetResolvedSchema(), "nothing resolved it, which is the state under test")

	assert.False(t, refNullable(js))
}

// TestSchemaNullVerdict_TheConjunctWalkIsBounded pins the budget the conjunct
// walk runs on. Whether a schema admits null is decided partly by its allOf
// conjuncts, each reached through a $ref whose target is asked the same
// question, so a schema conjoining itself would otherwise not terminate.
//
// A budget spent per schema visited caps depth as well as breadth, and an
// exhausted walk answers "silent" — the verdict that claims the least, so a
// spec too deep to read is never reported as admitting a null it does not.
func TestSchemaNullVerdict_TheConjunctWalkIsBounded(t *testing.T) {
	t.Parallel()
	nullable := &oas3.Schema{
		Type: oas3.NewTypeFromArray([]oas3.SchemaType{oas3.SchemaTypeString, oas3.SchemaTypeNull}),
	}

	budget := 1
	require.Equal(t, nullAdmitted, schemaNullVerdict(nullable, &budget),
		"the fixture admits null while there is budget to read it")

	spent := 0
	assert.Equal(t, nullSilent, schemaNullVerdict(nullable, &spent),
		"an exhausted budget stops the walk without claiming anything")
	assert.Positive(t, maxNullConjuncts, "the cap is a real bound, not zero")
}

// TestConjunctNullVerdict_ABranchWithNoSchemaSaysNothing pins the guard on an
// absent allOf entry. The parser never produces a nil branch, so nothing in the
// corpus reaches it; a conjunct that is not there constrains nothing, which is
// silence rather than a refusal — reading it as forbidding would let one
// missing entry strip a sibling's null.
func TestConjunctNullVerdict_ABranchWithNoSchemaSaysNothing(t *testing.T) {
	t.Parallel()
	budget := maxNullConjuncts
	assert.Equal(t, nullSilent, conjunctNullVerdict(nil, &budget))
	assert.Equal(t, nullSilent, conjunctNullVerdict(openapitest.EmptyEitherSchema(), &budget),
		"a branch whose either-value holds neither schema nor bool says nothing either")
}

// TestComponentSchemaAt_OnlyATopLevelComponentPointerHasABody pins the split
// the function exists for: a component pointer has a body, and a pointer into
// that same component does not.
//
// The fixture declares an empty-named component beside the real one, and that is
// the whole point of it. A deeper pointer's component name comes back empty, so
// with the guard removed the lookup asks for "" — and against a document that
// declares no such name it misses, hands back nil, and the misclassification
// looks exactly like the right answer. That masking is why this function read as
// covered while nothing held it to anything (#202). Declaring "" is what makes
// the wrong answer visible: it is a name like any other here.
func TestComponentSchemaAt_OnlyATopLevelComponentPointerHasABody(t *testing.T) {
	t.Parallel()
	l, diags := loweredFor(t, openapitest.ComponentSpec(
		"    Outer:\n      type: object\n      title: outer\n"+
			"      properties: {inner: {type: string, title: inner}}\n"+
			"    \"\": {type: string, title: empty}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	tests := []struct {
		name, pointer, wantTitle string
	}{
		{name: "the component itself", pointer: "/components/schemas/Outer", wantTitle: "outer"},
		// The empty name is a name the document may declare, but it is not one this
		// grammar addresses: a pointer ending at the slash names no entry, so the
		// component it declares is reached as an anonymous position instead.
		{name: "the pointer that names no entry", pointer: "/components/schemas/"},
		{name: "a property inside that component", pointer: "/components/schemas/Outer/properties/inner"},
		{name: "a component of another kind", pointer: "/components/parameters/Outer"},
		{name: "no component pointer at all", pointer: "/paths/~1a/get"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := componentSchemaAt(l.ctx, tc.pointer)
			if tc.wantTitle == "" {
				assert.Nil(t, got, "only a top-level component schema has a body here")
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.wantTitle, got.GetTitle(), "and it is that component's own body")
		})
	}
}

// TestDynamicHop_HopsOnlyWhenExactlyOneAnchorSiteIsNamed pins every way the
// chain walk can stop, which is the half of #202's "why it matters" that is
// about the verdict: a wrong one either expands what it should refuse or
// refuses what it should expand.
//
// A hop needs three things — a component at the pointer, a $dynamicRef written
// on it, and exactly one site declaring the anchor it names. Anything else ends
// the chain, and a chain that ends is a chain with no cycle in it, which
// expands. That is the direction sites already errs in. Reaching for a site when
// there is not exactly one would either pick an arbitrary branch, reporting a
// cycle the document does not have, or index an empty slice.
func TestDynamicHop_HopsOnlyWhenExactlyOneAnchorSiteIsNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, schemas, at, anchor string
		wantSites                 int
		wantNext                  string
	}{
		{
			name: "the anchor is declared twice, so it names no single target",
			schemas: "    A: {$dynamicAnchor: m, type: string}\n" +
				"    B: {$dynamicAnchor: m, type: integer}\n" +
				"    C: {$dynamicRef: '#m'}\n",
			at: "/components/schemas/C", anchor: "m", wantSites: 2,
		},
		{
			name:    "the anchor is declared nowhere, so it names nothing",
			schemas: "    C: {$dynamicRef: '#ghost'}\n",
			at:      "/components/schemas/C", anchor: "ghost", wantSites: 0,
		},
		{
			name:    "the component writes no $dynamicRef at all",
			schemas: "    C: {type: string}\n",
			at:      "/components/schemas/C",
		},
		{
			name:    "the pointer names no component",
			schemas: "    C: {type: string}\n",
			at:      "/components/schemas/C/properties/inner",
		},
		{
			name: "declared once, which is the only shape that hops",
			schemas: "    A: {$dynamicAnchor: m, type: string}\n" +
				"    C: {$dynamicRef: '#m'}\n",
			at: "/components/schemas/C", anchor: "m", wantSites: 1,
			wantNext: "/components/schemas/A",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, diags := loweredFor(t, openapitest.ComponentSpec(tc.schemas))
			openapitest.RequireNoErrorDiags(t, diags)
			if tc.anchor != "" {
				sites, siteDiags := l.anchors.sites(l.ctx, tc.anchor)
				require.Len(t, sites, tc.wantSites, "the fixture must set up the case it is named for")
				require.Empty(t, siteDiags)
			}

			next, hops, hopDiags := dynamicHop(l.ctx, &l.anchors, tc.at)

			assert.Equal(t, tc.wantNext != "", hops)
			assert.Equal(t, tc.wantNext, next)
			assert.Empty(t, hopDiags)
		})
	}
}
