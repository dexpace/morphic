// This file is a package-level suite, not a per-source-file test: it runs the
// capability-conformance corpus (one minimal spec per ir-spec-matrix.md row)
// through the whole compiler to keep "lossless by default" honest, so it has no
// single source file to pair with.
package openapi_test // external test package — exercises only the public API

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// conformanceDir is the corpus of one minimal spec per capability row of
// ir-spec-matrix.md, addressed relative to this test file.
const conformanceDir = "../../testdata/conformance/openapi"

// TestConformance drives one minimal spec per OpenAPI-expressible capability
// through the full compiler and asserts lossless capture: a focused
// capability-specific assertion plus a byte-exact golden IR snapshot. Regenerate
// the goldens with `go test ./compilers/openapi -run TestConformance -update`.
//
// What this corpus does *not* cover is derived rather than described:
// TestConformance_UnwitnessedIRFields snapshots every ir field no committed spec
// drives to a non-zero value, so the gap is a file in the corpus that -update
// recomputes and review reads as a diff. A case landing here shrinks it; a new IR
// field, or a compiler that stops writing one, grows it.
//
// Some of what remains listed there no spec can reach — a field only another
// source format writes, or one the compiler assigns a zero value that IsZero
// cannot tell from never being written. That test's doc comment says which
// weaknesses are structural; the point of the file is that nothing about the
// corpus's reach is claimed here by hand.
func TestConformance(t *testing.T) {
	t.Parallel()
	for _, tc := range conformanceCases() {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseCorpus(t, tc.file)
			assertNoErrorDiags(t, diags)
			tc.assert(t, doc, diags)
			// A failed capability assertion must not reach the golden. require
			// aborts, but assert does not, and under -update a non-fatal failure
			// would rewrite the snapshot with the regressed document — leaving a
			// loud run behind a file that now agrees with the regression.
			require.False(t, t.Failed(), "capability assertion failed; not comparing or rewriting the golden")
			irtest.CompareGolden(t, filepath.Join(conformanceDir, tc.file+".golden.json"), doc)
			assertJSONRoundTrip(t, doc)
		})
	}
}

// TestConformance_TableNamesEveryCorpusSpec requires the table and the corpus
// directory to name the same specs. Both directions matter: a spec with no row
// gets neither a capability assertion nor a golden while the corpus-wide sweeps
// (dangling references, the fuzz seed, the unwitnessed walk) still read it, so
// it looks covered; a row naming a deleted spec fails the other way. Comparing
// sorted lists rather than sets also catches a spec named by two rows.
func TestConformance_TableNamesEveryCorpusSpec(t *testing.T) {
	t.Parallel()
	onDisk := corpusSpecNames(t)
	cases := conformanceCases()
	inTable := make([]string, 0, len(cases))
	for _, tc := range cases {
		inTable = append(inTable, tc.file)
	}
	slices.Sort(onDisk)
	slices.Sort(inTable)
	if diff := cmp.Diff(onDisk, inTable); diff != "" {
		t.Errorf("corpus and table disagree (-on disk +in table):\n%s", diff)
	}
}

// corpusSpecNames returns the base name of every spec in the corpus directory,
// failing on any file that is neither a .yaml spec nor a golden beside one.
//
// The .yaml-only restriction is asserted here rather than assumed by a narrower
// glob. parseCorpus reads "<name>.yaml", so a spec committed as .yml or .json
// cannot be named by a table row at all — while the corpus-wide sweep in
// conformance_unwitnessed_test.go reads all three extensions and counts the
// fields such a spec exercises as witnessed. Left to a glob, that spec would
// pass through carrying neither a capability assertion nor a golden: the hole
// this test exists to close, reached by a different spelling.
func corpusSpecNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(conformanceDir)
	require.NoError(t, err)

	specs := make([]string, 0, len(entries))
	goldens := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		require.False(t, e.IsDir(), "corpus directory holds a subdirectory %q; no sweep descends into one", name)
		switch {
		case name == filepath.Base(unwitnessedGolden):
			// The derived never-witnessed snapshot: a corpus-wide artifact, not
			// a spec's golden, so it pairs with nothing.
		case strings.HasSuffix(name, ".golden.json"):
			goldens = append(goldens, strings.TrimSuffix(name, ".golden.json"))
		case strings.HasSuffix(name, ".yaml"):
			specs = append(specs, strings.TrimSuffix(name, ".yaml"))
		default:
			t.Errorf("corpus file %q is neither a .yaml spec nor a golden; "+
				"parseCorpus reads only .yaml, so a spec in any other extension gets no table row", name)
		}
	}
	require.NotEmpty(t, specs, "the corpus directory must hold specs")
	slices.Sort(specs)
	slices.Sort(goldens)
	if diff := cmp.Diff(specs, goldens); diff != "" {
		t.Errorf("corpus specs and goldens disagree (-specs +goldens):\n%s", diff)
	}
	return specs
}

// conformanceCase pairs one corpus spec with the assertion that says what
// capturing its capability losslessly means.
type conformanceCase struct {
	file   string
	assert func(*testing.T, *ir.Document, []ir.Diagnostic)
}

// conformanceCases is the corpus table: one row per spec, each naming the file
// and the assertion that reads it.
func conformanceCases() []conformanceCase {
	return []conformanceCase{
		{"named-types", assertNamedTypes},
		{"neutral-naming", assertNeutralNaming},
		{"empty-names", assertEmptyNames},
		{"inline-types", assertInlineTypes},
		{"component-reuse", assertComponentReuse},
		{"allof-inheritance", assertAllOfInheritance},
		{"allof-mixins", assertAllOfMixins},
		{"allof-inline-merge", assertAllOfInlineMerge},
		{"allof-required-only", assertAllOfRequiredOnly},
		{"allof-oneof-cooccurrence", assertAllOfOneOfCooccurrence},
		{"allof-inline-residue", assertAllOfInlineResidue},
		{"allof-ref-branch-siblings", assertAllOfRefBranchSiblings},
		{"allof-boolean-branch", assertAllOfBooleanBranch},
		{"oneof-discriminated", assertOneOfDiscriminated},
		{"discriminator-inheritance", assertDiscriminatorInheritance},
		{"discriminator-default-mapping", assertDiscriminatorDefaultMapping},
		{"discriminator-transitive", assertDiscriminatorTransitive},
		{"unhomed-keywords", assertUnhomedKeywords},
		{"codeclared-keywords", assertCoDeclaredKeywords},
		{"anyof-untagged", assertAnyOfUntagged},
		{"negation-not", assertNegationNot},
		{"dependent-required", assertDependentRequired},
		{"dialect-keywords", assertDialectKeywords},
		{"dynamic-ref", assertDynamicRef},
		{"enum-string", assertEnumString},
		{"enum-numeric", assertEnumNumeric},
		{"scalar-format", assertScalarFormat},
		{"encoding-byte", assertEncodingByte},
		{"content-vocabulary", assertContentVocabulary},
		{"xml-hints", assertXMLHints},
		{"nullability-four-states", assertNullabilityFourStates},
		{"nullable-30", assertNullable30},
		{"nullable-31-ref", assertNullable31Ref},
		{"nullable-enum-31", assertNullableEnum31},
		{"defaults", assertDefaults},
		{"yaml-timestamp-scalars", assertYAMLTimestampScalars},
		{"constraints", assertConstraints},
		{"numeric-precision", assertNumericPrecision},
		{"readonly-writeonly", assertReadOnlyWriteOnly},
		{"recursive", assertRecursive},
		{"maps", assertMaps},
		{"tuples-prefixitems", assertTuples},
		{"literal-const", assertLiteralConst},
		{"tags-grouping", assertTagsGrouping},
		{"http-binding", assertHTTPBinding},
		{"param-styles", assertParamStyles},
		{"param-xml-residue", assertParamXMLResidue},
		{"param-ref-inheritance", assertParamRefInheritance},
		{"header-content-schema", assertHeaderContentSchema},
		{"multi-content", assertMultiContent},
		{"multipart-encoding", assertMultipartEncoding},
		{"file-body", assertFileBody},
		{"sequential-media", assertSequentialMedia},
		{"per-status-errors", assertPerStatusErrors},
		{"response-links", assertResponseLinks},
		{"webhooks", assertWebhooks},
		{"callbacks", assertCallbacks},
		{"path-item-docs", assertPathItemDocs},
		{"path-item-operations", assertPathItemOperations},
		{"deprecation", assertDeprecation},
		{"examples", assertExamples},
		{"docs-summary-desc", assertDocsSummaryDesc},
		{"extensions-x", assertExtensionsX},
		{"inline-annotations", assertInlineAnnotations},
		{"inline-residue", assertInlineResidue},
		{"servers-variables", assertServersVariables},
		{"security-schemes", assertSecuritySchemes},
		{"security-or-and", assertSecurityOrAnd},
	}
}

// parseCorpus reads and parses one corpus spec through the full compiler.
func parseCorpus(t *testing.T, name string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// assertJSONRoundTrip marshals doc, reads it back, and re-marshals, requiring
// the two encodings to be byte-identical — the serialized oracle fuzz_test.go
// and internal/harness already apply, compared as JSON so the unpreservable
// nil-vs-empty-collection distinction is ignored while real loss still shows.
//
// Invariant 7 is asserted per corpus document rather than only on the golden
// one because each spec reaches IR shapes the others never build — a reserved
// map key, a preserved raw payload, a sum-typed node — and only the shapes a
// document contains can round-trip wrongly.
func assertJSONRoundTrip(t *testing.T, doc *ir.Document) {
	t.Helper()
	first, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(&back)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "round-trip must preserve the document")
}

// assertNoErrorDiags fails when any diagnostic has error severity.
func assertNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

// namedID is the stable TypeID of a components-named schema.
func namedID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// allOperations flattens every operation across a document's service groups.
func allOperations(doc *ir.Document) []ir.Operation {
	var out []ir.Operation
	var walk func(gs []ir.OperationGroup)
	walk = func(gs []ir.OperationGroup) {
		for _, g := range gs {
			out = append(out, g.Operations...)
			walk(g.Groups)
		}
	}
	for _, svc := range doc.Services {
		walk(svc.Groups)
	}
	return out
}

// opByName finds an operation by its source operationId.
func opByName(doc *ir.Document, source string) (ir.Operation, bool) {
	for _, op := range allOperations(doc) {
		if op.Name.Source == source {
			return op, true
		}
	}
	return ir.Operation{}, false
}

// propByWire returns the property of m with the given wire name.
func propByWire(m *ir.Model, wire string) (ir.Property, bool) {
	for _, p := range m.Properties {
		if p.WireName == wire {
			return p, true
		}
	}
	return ir.Property{}, false
}

// paramByName returns the operation's logical parameter with the given source
// name.
func paramByName(op ir.Operation, name string) (ir.Parameter, bool) {
	for _, p := range op.Params {
		if p.Name.Source == name {
			return p, true
		}
	}
	return ir.Parameter{}, false
}

func assertNamedTypes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	user, ok := doc.Types[namedID("User")].(*ir.Model)
	require.True(t, ok, "named schema User present under its pointer-derived ID")
	assert.False(t, user.Anonymous)
	addr, ok := propByWire(user, "address")
	require.True(t, ok)
	assert.Equal(t, namedID("Address"), addr.Type.Target)
	_, ok = doc.Types[namedID("Address")].(*ir.Model)
	assert.True(t, ok, "referenced Address resolves in the registry")
}

// assertNeutralNaming covers invariant 4's second half: Naming.Canonical is a
// neutral word sequence whatever the source spelled the word boundaries as. Real
// specs name things with dots (a namespaced component, a versioned field, an
// enum value), brackets (a deep-object query parameter) and hyphens (a header),
// and each of those characters used to reach Canonical verbatim.
//
// The corpus needed a spec that writes them at all: every other fixture names
// things in plain identifiers, so the compiler and the goldens shared one blind
// spot and the segmentation could not be wrong in a way any of them saw.
//
// Naming.Hint is deliberately not covered: it is built from context strings
// rather than through the grammar, and the golden shows one ("rollout.state")
// still carrying the source punctuation. That is GitHub #54, left open.
func assertNeutralNaming(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	svc := doc.Services[0]
	assert.Equal(t, "neutral_naming_api", svc.Name.Canonical, "info.title")
	require.Len(t, svc.Groups, 1)
	assert.Equal(t, "inventory_core", svc.Groups[0].Name.Canonical, "tag")

	op, ok := opByName(doc, "widgets.list")
	require.True(t, ok, "operation present under its source operationId")
	assert.Equal(t, "widgets_list", op.Name.Canonical, "operationId")
	byParam := map[string]string{}
	for _, p := range op.Params {
		byParam[p.Name.Source] = p.Name.Canonical
	}
	assert.Equal(t, "widget_id", byParam["widget.id"], "path parameter")
	assert.Equal(t, "filter_name", byParam["filter[name]"], "bracketed query parameter")

	widget, ok := doc.Types[namedID("com.example.Widget")].(*ir.Model)
	require.True(t, ok, "namespaced component keeps its pointer-derived ID")
	assert.Equal(t, "com_example_widget", widget.Name.Canonical, "component name")
	assert.Equal(t, "com.example.Widget", widget.Name.Source, "the source spelling is kept beside it")
	name, ok := propByWire(widget, "widget.name")
	require.True(t, ok)
	assert.Equal(t, "widget_name", name.Name.Canonical, "property name")
	version, ok := propByWire(widget, "api.version.v2")
	require.True(t, ok)
	assert.Equal(t, "api_version_v_2", version.Name.Canonical,
		"the letter/digit boundary still splits inside a dotted segment")

	assertNeutralNamingLeaves(t, doc, widget, op)
}

// assertNeutralNamingLeaves checks the three canonicals that hang off another
// node rather than off the service walk: an enum member under a property, a
// header under a response, a scheme in the auth registry.
func assertNeutralNamingLeaves(t *testing.T, doc *ir.Document, widget *ir.Model, op ir.Operation) {
	t.Helper()
	state, ok := propByWire(widget, "rollout.state")
	require.True(t, ok)
	rollout, ok := doc.Types[state.Type.Target].(*ir.Enum)
	require.True(t, ok, "the enum property hoists an Enum node")
	require.NotEmpty(t, rollout.Members)
	assert.Equal(t, "in_progress", rollout.Members[0].Name.Canonical, "enum member")

	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	assert.Equal(t, "x_rate_limit", op.Responses[0].Headers[0].Name.Canonical, "response header")

	require.Len(t, doc.Auth, 1)
	for _, scheme := range doc.Auth {
		assert.Equal(t, "oauth_2_client", scheme.Name.Canonical, "security scheme")
	}
}

// assertEmptyNames is the complement of assertNeutralNaming: every position
// where the source names an entity with the empty string, which is a legal
// OpenAPI document at each of them. The compiler used to pass "" through as a
// Naming with nothing in any channel, leaving an emitter no identifier to render
// and satisfying every neutrality check vacuously (GitHub #251). Each of these
// now carries the minted hint instead, and irverify's presence rule — swept over
// this corpus by TestVerify_Corpus — is what holds the general case.
func assertEmptyNames(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	assert.Equal(t, "empty", doc.Services[0].Groups[0].Name.Hint, "tag declared as \"\"")

	require.Len(t, doc.Auth, 1)
	for _, scheme := range doc.Auth {
		assert.Equal(t, "empty", scheme.Name.Hint, "security scheme keyed \"\"")
	}

	blank, ok := doc.Types[ir.TypeID("t/anon/components/schemas/")]
	require.True(t, ok, "a component schema keyed \"\" is hoisted at its own pointer")
	assert.Equal(t, "empty", blank.Common().Name.Hint, "component schema keyed \"\"")

	assertEmptyEnumMember(t, doc)
	assertEmptyNamedProperty(t, doc)
	assertEmptyDerivedHints(t, doc)
}

// assertEmptyEnumMember covers the enum member the issue was reported against,
// and beside it the collision the minted word can cause: "" and "empty" are two
// distinct values, and a member named for each lands on the same word.
//
// The IR keeps them apart — two members, two values, one in each name channel —
// because uniquifying identifiers is an emitter's job and always has been:
// "red" and "Red" already canonicalize to the same words. This pins that the
// minted name does not merge or drop a member, which is the only part of the
// collision the IR is answerable for.
func assertEmptyEnumMember(t *testing.T, doc *ir.Document) {
	t.Helper()
	rollout, ok := doc.Types[namedID("Rollout")].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, rollout.Members, 3, "no member is merged away by the shared word")

	assert.Equal(t, "empty", rollout.Members[0].Name.Hint, "enum member whose value is \"\"")
	assert.Empty(t, rollout.Members[0].Name.Source, "nothing is fabricated into the source channel")
	assert.Empty(t, rollout.Members[0].Value.Str, "the value itself is kept as the empty string")

	declared := rollout.Members[1]
	assert.Equal(t, "empty", declared.Name.Source, "a member whose value really is \"empty\"")
	assert.Equal(t, "empty", declared.Name.Canonical, "named through the declared channels, not the hint")
	assert.Empty(t, declared.Name.Hint)

	assert.Equal(t, "done", rollout.Members[2].Name.Source, "the ordinary sibling is unaffected")
}

// assertEmptyNamedProperty covers the two entities one property keyed "" makes:
// the property, and the inline schema hoisted at its position, whose context
// hint is derived from the same empty name.
func assertEmptyNamedProperty(t *testing.T, doc *ir.Document) {
	t.Helper()
	widget, ok := doc.Types[namedID("Widget")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, widget.Properties, 1)
	prop := widget.Properties[0]
	assert.Equal(t, "empty", prop.Name.Hint, "property keyed \"\"")
	assert.Empty(t, prop.WireName, "the wire key itself is still the empty string")

	inline, ok := doc.Types[prop.Type.Target]
	require.True(t, ok, "the property's inline object is hoisted")
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Widget/properties/"), prop.Type.Target)
	assert.Equal(t, "empty", inline.Common().Name.Hint,
		"a hint derived from an empty position is minted, not passed through empty")
}

// composedHints is every node in the corpus spec whose hint is built out of an
// enclosing node's. Each pairs an unnamed position with the role or index that
// distinguishes the child from its siblings — a list element, a map value, a
// pattern group, a tuple slot, an enum branch, a composed union variant. A slice
// rather than a map so a failure names them in a fixed order.
var composedHints = []struct {
	id   ir.TypeID
	hint string
}{
	{"t/anon/components/schemas/List/properties//items", "empty_item"},
	{"t/anon/components/schemas/Maps/properties//additionalProperties", "empty_value"},
	{"t/anon/components/schemas/Patterns/properties//patternProperties/^x", "empty_pattern"},
	{"t/anon/components/schemas/Tuple/properties//prefixItems/0", "empty_0"},
	{"t/anon/components/schemas/Mixed/properties//enum/0", "empty_0"},
	{"t/anon/components/schemas/Mixed/properties//enum/1", "empty_1"},
	{"t/composed/components/schemas/Host/properties//oneOf/0", "empty_Alt"},
}

// assertEmptyDerivedHints is the case minting at the node alone does not reach.
// A child named after its position inside another has its hint built out of the
// enclosing node's, and concatenating onto an empty one used to yield a leading
// "_": non-empty, so the presence rule passes it, but a shape no grammar
// produces and one that disagrees with the node it hangs off. The enclosing hint
// is minted before the child is composed from it.
//
// The composition sites are the callers of compile.SubHint:
//
//	grep -rn 'compile\.SubHint(' compilers/openapi
//
// composedHints covers every one an unnamed position can reach. The single
// exception is the sequential media type's 3.2 itemSchema, and it is not a gap:
// both callers of lowerPayload pass an enclosing hint that cannot be empty —
// "response" is a literal, and the request-body side falls back to "request"
// because ids.ComponentEntry refuses a component keyed "". SubHint there is
// uniformity rather than a fix.
//
// A composed variant of an *inline* branch is likewise absent by construction,
// not by omission: ir-design §4.8 keeps that union verbatim instead of
// distributing it, so the $ref form the Host case uses is the only form that
// reaches composedVariant at all.
func assertEmptyDerivedHints(t *testing.T, doc *ir.Document) {
	t.Helper()
	mixed, ok := doc.Types[namedID("Mixed")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, mixed.Properties, 1)
	union, ok := doc.Types[mixed.Properties[0].Type.Target].(*ir.Union)
	require.True(t, ok, "a heterogeneous enum lowers to a union of literals")
	assert.Equal(t, "empty", union.Name.Hint, "the enclosing node keeps the plain minted hint")

	for _, tc := range composedHints {
		node, found := doc.Types[tc.id]
		require.True(t, found, "no node at %s; the composition site moved", tc.id)
		assert.Equal(t, tc.hint, node.Common().Name.Hint, "composed hint at %s", tc.id)
	}
}

func assertInlineTypes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	order, ok := doc.Types[namedID("Order")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, order.Properties, 1)
	shipping := order.Properties[0]
	// hoisted exactly once, under its pointer-derived anonymous ID.
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Order/properties/shipping"),
		shipping.Type.Target)
	inline, ok := doc.Types[shipping.Type.Target].(*ir.Model)
	require.True(t, ok, "the inline object was hoisted as its own type")
	assert.True(t, inline.Anonymous)
	assert.Equal(t, "shipping", inline.Name.Hint)
}

// assertComponentReuse covers the non-schema half of `$ref`: OpenAPI lets a
// parameter, requestBody, response, header, callback, or whole path item be
// declared once under components and referenced from many operations. Each
// lowers at its declaration, so the shared node is interned once however many
// operations reach it, while the operations that reach it stay distinct.
func assertComponentReuse(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	widgets := operationAt(t, doc, "GET", "/widgets")
	gadgets := operationAt(t, doc, "GET", "/gadgets")
	order := operationAt(t, doc, "POST", "/orders")
	assert.NotEqual(t, widgets.ID, gadgets.ID, "one path item mounted twice is two operations")
	assert.Equal(t, widgets.Provenance.Pointer, gadgets.Provenance.Pointer,
		"...both written at the one component that declares them")

	// Every hoisted type in this document belongs to a component declaration:
	// no operation contributes a pointer of its own, because none declares a
	// schema of its own.
	for id, td := range doc.Types {
		if td.Common().Provenance.Pointer == "" {
			continue // shared primitives are pointerless by construction
		}
		assert.True(t, strings.HasPrefix(td.Common().Provenance.Pointer, "/components/"),
			"type %s hoists at a component declaration, not a use site", id)
	}

	sortID := ir.TypeID("t/anon/components/parameters/Sort/schema")
	require.Len(t, widgets.Params, 1)
	require.Len(t, gadgets.Params, 1)
	assert.Equal(t, sortID, widgets.Params[0].Type.Target)
	assert.Equal(t, sortID, gadgets.Params[0].Type.Target, "the shared parameter interns once")

	listedID := ir.TypeID("t/anon/components/responses/Listed/content/application~1json/schema")
	assert.Equal(t, listedID, widgets.Responses[0].Payload.Contents[0].Type.Target)
	assert.Equal(t, listedID, order.Responses[0].Payload.Contents[0].Type.Target,
		"a response reused across unrelated operations interns once")

	bodyID := ir.TypeID("t/anon/components/requestBodies/OrderBody/content/application~1json/schema")
	require.NotNil(t, order.Request)
	assert.Equal(t, bodyID, order.Request.Contents[0].Type.Target)
	assert.Equal(t, "OrderBody", doc.Types[bodyID].Common().Name.Hint,
		"a shared body is named after its component, not the operation that reached it first")

	require.Len(t, widgets.Responses[0].Headers, 1)
	header := widgets.Responses[0].Headers[0]
	assert.Equal(t, ir.TypeID("t/anon/components/headers/RateUnit/schema"), header.Type.Target)
	assert.Equal(t, "/components/responses/Listed/headers/X-Rate-Unit", header.Provenance.Pointer,
		"the header's identity is the map entry that binds its name")

	require.Len(t, order.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, "{$request.body#/callbackUrl}", order.Bindings.HTTP[0].Callbacks[0].Expression)
	_, ok := opByName(doc, "onShipped")
	assert.True(t, ok, "the shared callback's operation is registered alongside its parent")
}

// operationAt finds the operation bound to one HTTP method and URI template.
// Operations reached through a shared path item have no distinguishing name of
// their own, so their binding is what tells them apart.
func operationAt(t *testing.T, doc *ir.Document, method, uri string) ir.Operation {
	t.Helper()
	for _, op := range allOperations(doc) {
		for _, hb := range op.Bindings.HTTP {
			if hb.Method == method && hb.URITemplate == uri {
				return op
			}
		}
	}
	t.Fatalf("no operation bound to %s %s", method, uri)
	return ir.Operation{}
}

func assertAllOfInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[namedID("Derived")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, d.Base, "sole allOf $ref becomes Base")
	assert.Equal(t, namedID("Base"), d.Base.Target)
	assert.Empty(t, d.Mixins)
	_, ok = propByWire(d, "extra")
	assert.True(t, ok, "property declared alongside allOf survives")
	// Model-level detail declared alongside allOf must be captured, not dropped.
	assert.Equal(t, "A Base specialized with one extra field.", d.Docs.Description,
		"composed schema keeps its description")
	assert.NotNil(t, d.Deprecation, "composed schema keeps deprecated")
	assert.Equal(t, ir.AdditionalClosed, d.Additional,
		"composed schema keeps additionalProperties: false")
	assert.Contains(t, d.Unmodeled, "openapi:x-team", "composed schema keeps x-* extensions")
}

func assertAllOfMixins(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	c, ok := doc.Types[namedID("C")].(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, c.Base, "multiple non-hierarchy refs stay Mixins, none is Base")
	require.Len(t, c.Mixins, 2)
	assert.Equal(t, namedID("A"), c.Mixins[0].Target)
	assert.Equal(t, namedID("B"), c.Mixins[1].Target)
}

func assertAllOfInlineMerge(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("Merged")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, m.Base)
	assert.Equal(t, namedID("Base"), m.Base.Target)
	_, ok = propByWire(m, "name")
	assert.True(t, ok, "inline allOf branch contributes its properties")
}

func assertAllOfRequiredOnly(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[namedID("Derived")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, d.Base, "the $ref branch still becomes Base")
	name, ok := propByWire(d, "name")
	require.True(t, ok, "the inline branch's own property survives")
	assert.True(t, name.Required,
		"a required-only allOf branch attaches across the whole composition, not just its own properties map (issue #29)")
}

// assertAllOfOneOfCooccurrence pins both halves of the co-declared composition
// rule: §4.3 distributes a union whose branches all name referents, and §4.8
// keeps one with an inline branch verbatim rather than distributing it halfway.
// The outside reference to a branch pointer pins the third thing: a composed
// variant is Morphic's own node, so it cannot be taken by, or take from, a $ref.
func assertAllOfOneOfCooccurrence(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	combo, ok := doc.Types[namedID("Combo")].(*ir.Union)
	require.True(t, ok, "the union is the value, not a preserved sibling")
	require.Len(t, combo.Variants, 2)
	for i, branch := range []string{"A", "B"} {
		v, ok := doc.Types[combo.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d composes as a model", i)
		require.NotNil(t, v.Base, "the allOf composition rides on every variant")
		assert.Equal(t, namedID("Base"), v.Base.Target)
		require.Len(t, v.Mixins, 1)
		assert.Equal(t, namedID(branch), v.Mixins[0].Target)
	}

	mixed, ok := doc.Types[namedID("MixedKinds")].(*ir.Model)
	require.True(t, ok, "the structural body survives")
	entry, ok := mixed.Unmodeled["openapi:oneOf"]
	require.True(t, ok, "and the union it could not absorb survives beside it")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)

	outsider, ok := doc.Types[namedID("Outsider")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, outsider.Properties, 1)
	branchNode, ok := doc.Types[outsider.Properties[0].Type.Target].(*ir.Scalar)
	require.True(t, ok, "the branch pointer hoists an alias of what the branch references")
	require.NotNil(t, branchNode.Base)
	assert.Equal(t, namedID("A"), branchNode.Base.Target,
		"the reference gets the branch schema, never the variant composed for that branch")

	assertInlineBranchHint(t, doc)
}

// assertInlineBranchHint pins the hint an inline composition branch takes when an
// outside reference names its pointer too.
//
// Both lowerings reach that node — the union through its composition, the
// reference through hoistSubSchema — and only the first to arrive interns it, so
// the two must derive the same hint. A $ref branch has a target to take a name
// from and both already read it; an inline branch has only its position, and the
// reference path used to name it after the bare ordinal, "0" (GitHub #181).
//
// The value is asserted rather than left to the golden because the golden records
// whichever hint won without saying the two agree, which is the property at
// stake. The corpus carries the shape so the two-order oracle covers it: the
// oracle detects this class, and until now no committed spec put it in reach.
func assertInlineBranchHint(t *testing.T, doc *ir.Document) {
	t.Helper()
	const branchID = ir.TypeID("t/anon/components/schemas/InlineHost/oneOf/0")

	host, ok := doc.Types[namedID("InlineHost")].(*ir.Union)
	require.True(t, ok, "the inline union is the value")
	require.Len(t, host.Variants, 2)
	assert.Equal(t, branchID, host.Variants[0].Type.Target,
		"the union's first variant is the node the branch pointer owns")

	branch, ok := doc.Types[branchID]
	require.True(t, ok, "the branch pointer owns a node")
	assert.Equal(t, "variant_0", branch.Common().Name.Hint,
		"named by position, which is all an inline branch has to be named by")

	outsider, ok := doc.Types[namedID("InlineOutsider")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, outsider.Properties, 1)
	assert.Equal(t, branchID, outsider.Properties[0].Type.Target,
		"the outside reference resolves to that same node rather than hoisting a second")
}

func assertOneOfDiscriminated(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[namedID("Pet")].(*ir.Union)
	require.True(t, ok, "oneOf survives as a Union node, never collapsed")
	assert.True(t, pet.Exclusive)
	require.Len(t, pet.Variants, 2)
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, "petType", pet.Discriminator.PropertyName)
	assert.Equal(t, namedID("Cat"), pet.Discriminator.Mapping["cat"])
	assert.Equal(t, namedID("Dog"), pet.Discriminator.Mapping["dog"])
}

// assertDiscriminatorInheritance covers the model-hierarchy form of a
// discriminator, which the union form (oneof-discriminated) does not reach: the
// tag is declared on the base, each subtype composes the base by $ref, and the
// wire value each subtype answers to is recorded on the subtype.
//
// Both spellings of that value are pinned, because they come from different
// places: a subtype the base's mapping names takes the mapping key, and one the
// mapping omits takes OpenAPI's implicit schema name.
func assertDiscriminatorInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[namedID("Pet")].(*ir.Model)
	require.True(t, ok, "the base is a Model, not a Union")
	require.NotNil(t, pet.Discriminator)
	tag, ok := propByWire(pet, "petType")
	require.True(t, ok, "the base declares the tag property itself")
	assert.Equal(t, tag.ID, pet.Discriminator.Property,
		"a declared tag property binds by identity, not by wire name")
	assert.Empty(t, pet.Discriminator.PropertyName,
		"the name spelling is for a tag no property declares")
	assert.Equal(t, namedID("Cat"), pet.Discriminator.Mapping["cat"])

	for name, value := range map[string]string{"Cat": "cat", "Dog": "Dog"} {
		sub, ok := doc.Types[namedID(name)].(*ir.Model)
		require.True(t, ok, "%s composes as a Model", name)
		require.NotNil(t, sub.Base, "the discriminated base becomes Base, not a Mixin")
		assert.Equal(t, namedID("Pet"), sub.Base.Target)
		assert.Equal(t, value, sub.DiscriminatorValue)
		assert.Nil(t, sub.Discriminator, "a subtype does not restate its base's discriminator")
	}
}

// assertDiscriminatorDefaultMapping pins Discriminator.Default, whose only source
// is the 3.2 discriminator.defaultMapping — and with it that a 3.2-only schema
// keyword compiles without an error diagnostic (GitHub #146). The library checks
// every schema object against the 3.1 meta-schema whatever the document says, so
// before this the spec below failed to compile and the field had no witness.
func assertDiscriminatorDefaultMapping(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, diagsAt(diags, "openapi/validation/validation-invalid-schema",
		"/components/schemas/Pet/discriminator/defaultMapping"),
		"a 3.2 keyword in a 3.2 document is not an invalid schema")

	pet, ok := doc.Types[namedID("Pet")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, namedID("Dog"), pet.Discriminator.Default,
		"defaultMapping names the variant an unrecognized tag falls back to")
	assert.Equal(t, namedID("Cat"), pet.Discriminator.Mapping["cat"],
		"and it is read separately from the mapping")
}

// assertDiscriminatorTransitive covers a discriminator tagging a descendant more
// than one hop away: Puppy composes Dog, which composes the discriminated Pet.
//
// The chain itself is what matters here — every other discriminator spec in the
// corpus is one level deep, so nothing witnessed a base routing a grandchild, and
// a consumer reading composition one hop deep called this valid document invalid.
func assertDiscriminatorTransitive(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[namedID("Pet")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, namedID("Puppy"), pet.Discriminator.Mapping["puppy"],
		"the base maps a wire value straight onto its grandchild")

	dog, ok := doc.Types[namedID("Dog")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, dog.Base)
	assert.Equal(t, namedID("Pet"), dog.Base.Target)

	puppy, ok := doc.Types[namedID("Puppy")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, puppy.Base, "the chain stays as declared rather than flattening onto the root")
	assert.Equal(t, namedID("Dog"), puppy.Base.Target)
	assert.Nil(t, puppy.Discriminator, "a subtype does not restate its base's discriminator")

	// Known gap, pinned so the corpus reddens when it is closed: the tag value is
	// read off the subtype's own base branch, which here names Dog rather than the
	// schema declaring the discriminator, so Pet's "puppy" key is dropped
	// (GitHub #305). Dog, one hop from the declaration, does carry its key.
	assert.Equal(t, "dog", doc.Types[namedID("Dog")].(*ir.Model).DiscriminatorValue)
	assert.Empty(t, puppy.DiscriminatorValue)
}

func assertAnyOfUntagged(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	u, ok := doc.Types[namedID("StringOrNumber")].(*ir.Union)
	require.True(t, ok)
	assert.False(t, u.Exclusive, "anyOf is a non-exclusive union")
	assert.Nil(t, u.Discriminator)
	assert.Len(t, u.Variants, 2)
}

func assertNegationNot(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[namedID("NotFoo")].(*ir.Model)
	require.True(t, ok)
	raw, ok := m.Unmodeled["openapi:not"]
	require.True(t, ok, "not-keyword kept verbatim under Unmodeled")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw.Value))
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)
	var found bool
	for _, d := range diags {
		if d.Code == "openapi/validation-only-keyword" {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}

func assertEnumString(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	e, ok := doc.Types[namedID("Color")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, e.ValueType)
	assert.True(t, e.Closed)
	require.Len(t, e.Members, 3)
	assert.Equal(t, "red", e.Members[0].Value.Str)
	assert.Equal(t, "blue", e.Members[2].Value.Str)
}

func assertEnumNumeric(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	e, ok := doc.Types[namedID("BigCodes")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimInteger, e.ValueType)
	require.Len(t, e.Members, 2)
	assert.Equal(t, ir.ValueNumber, e.Members[1].Value.Kind)
	// BigVal member value preserves the full 64-bit integer.
	assert.Equal(t, ir.BigVal("9007199254740993"), e.Members[1].Value.Num)
}

func assertScalarFormat(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	h, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	color, ok := propByWire(h, "color")
	require.True(t, ok)
	sc, ok := doc.Types[color.Type.Target].(*ir.Scalar)
	require.True(t, ok, "unknown format hoists a named Scalar")
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "hex-color", sc.Encoding.Name)
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.TypeID("t/prim/string"), sc.Base.Target)
	assert.False(t, color.Secret, "an ordinary format asks for no redaction")
	// The bound survives the hoist the unknown format forced (GitHub #147).
	require.NotNil(t, sc.Constraints, "the hoisted format scalar carries its own bounds")
	require.NotNil(t, sc.Constraints.MinLength)
	assert.Equal(t, int64(4), *sc.Constraints.MinLength)

	// format: password is a redaction request about a use of the value, so it
	// lands on the property rather than on the shared encoding node.
	token, ok := propByWire(h, "token")
	require.True(t, ok)
	assert.True(t, token.Secret)
}

// assertXMLHints covers the XML wire shape, whose hints attach at two carriers
// with different scopes: a type node holds the root element shape, a property
// holds a per-use override. A position that owns a node keeps them there, so
// which carrier a hint lands on follows what the position lowered to.
func assertXMLHints(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	book, ok := doc.Types[namedID("Book")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, book.XML)
	assert.Equal(t, ir.XMLHints{Name: "book", Namespace: "urn:example:books", Prefix: "bk"}, *book.XML)

	isbn, ok := propByWire(book, "isbn")
	require.True(t, ok)
	require.NotNil(t, isbn.XML, "a scalar property has no node of its own, so the property carries them")
	assert.Equal(t, ir.XMLHints{Namespace: "urn:example:books", Prefix: "bk", NodeType: "attribute"},
		*isbn.XML, "attribute: true is the node type, not a flag of its own")

	authors, ok := propByWire(book, "authors")
	require.True(t, ok)
	assert.Nil(t, authors.XML, "an array owns a node, which is where its hints go")
	list, ok := doc.Types[authors.Type.Target].(*ir.List)
	require.True(t, ok)
	require.NotNil(t, list.XML)
	assert.True(t, list.XML.Wrapped, "wrapping is a property of the collection")
	elem, ok := doc.Types[list.Elem.Target]
	require.True(t, ok)
	require.NotNil(t, elem.Common().XML)
	assert.Equal(t, "author", elem.Common().XML.Name)
}

func assertEncodingByte(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	h, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, h.Properties, 1)
	sc, ok := doc.Types[h.Properties[0].Type.Target].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, sc.Encoding)
	assert.Equal(t, "base64", sc.Encoding.Name)
	require.NotNil(t, sc.Encoding.WireType)
	assert.Equal(t, ir.TypeID("t/prim/string"), sc.Encoding.WireType.Target)
	require.NotNil(t, sc.Base)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), sc.Base.Target)

	// A scalar that hoists a node because of its format must keep the bounds it
	// wrote beside it: owning the pointer is what stops the shared alias path
	// attaching them afterwards (GitHub #147).
	require.NotNil(t, sc.Constraints, "the hoisted byte scalar carries its own bounds")
	require.NotNil(t, sc.Constraints.MinLength)
	assert.Equal(t, int64(5), *sc.Constraints.MinLength)
	require.NotNil(t, sc.Constraints.MaxLength)
	assert.Equal(t, int64(9), *sc.Constraints.MaxLength)
}

func assertNullabilityFourStates(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 4)
	states := openapitest.PropsByWire(m.Properties)
	assert.True(t, states["reqPlain"].Required)
	assert.False(t, states["reqPlain"].Type.Nullable)
	assert.True(t, states["reqNull"].Required)
	assert.True(t, states["reqNull"].Type.Nullable)
	assert.False(t, states["optPlain"].Required)
	assert.False(t, states["optPlain"].Type.Nullable)
	assert.False(t, states["optNull"].Required)
	assert.True(t, states["optNull"].Type.Nullable)
}

func assertNullable30(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	assert.True(t, m.Properties[0].Type.Nullable, "3.0 nullable lowers to the same IR bit")
}

func assertNullable31Ref(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("Owner")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 2)
	byName := openapitest.PropsByWire(m.Properties)

	assert.True(t, byName["p"].Type.Nullable,
		"3.1's type-array null spelling normalizes to the same IR bit at a $ref site")
	assert.Equal(t, namedID("Target"), byName["p"].Type.Target, "the ref resolves to the named component")

	assert.True(t, byName["q"].Type.Nullable,
		"a union's null branch is stripped into the ref's Nullable bit, so the ref must carry it")
	assert.Equal(t, namedID("UnionTarget"), byName["q"].Type.Target, "the ref resolves to the union component")

	u, ok := doc.Types[namedID("UnionTarget")].(*ir.Union)
	require.True(t, ok)
	assert.Len(t, u.Variants, 2, "the null branch lifts to the ref rather than becoming a variant")
}

// assertNullableEnum31 covers 3.1's spelling of a nullable enum: `null` listed
// among the members of a null-admitting type array. The member is normalized
// onto the Nullable bit of every reference to the enum rather than degrading the
// declaration to a union of literals, so the capability being claimed is the
// pair — a closed Enum of the scalar members, and null admitted at each use.
//
// The golden beside this case cannot pin the strip on its own: the type array
// already carries the bit, so deleting the `null` member from the spec leaves
// the IR body byte-identical and moves only the source hash. The member count
// below is what pins it.
func assertNullableEnum31(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	e, ok := doc.Types[namedID("Color")].(*ir.Enum)
	require.True(t, ok, "the null member does not cost the declaration its enum-ness")
	assert.True(t, e.Closed)
	assert.Equal(t, ir.PrimString, e.ValueType, "null contributes no value type of its own")
	require.Len(t, e.Members, 2, "the null member is normalized away, the scalar members are not")
	assert.Equal(t, "red", e.Members[0].Value.Str)
	assert.Equal(t, "green", e.Members[1].Value.Str)
	for _, d := range diags {
		assert.NotEqual(t, "openapi/degraded-construct", d.Code, "normalizing is not degrading: %+v", d)
	}

	m, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	one, ok := propByWire(m, "one")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: namedID("Color"), Nullable: true}, one.Type,
		"a property $ref admits the null the members no longer carry")

	many, ok := propByWire(m, "many")
	require.True(t, ok)
	list, ok := doc.Types[many.Type.Target].(*ir.List)
	require.True(t, ok, "the array property hoists a List")
	assert.Equal(t, ir.TypeRef{Target: namedID("Color"), Nullable: true}, list.Elem,
		"and so does a list element, which holds its own reference")
	assert.False(t, many.Type.Nullable, "the list itself was never declared nullable")

	op, ok := opByName(doc, "pick")
	require.True(t, ok)
	p, ok := paramByName(op, "color")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: namedID("Color"), Nullable: true}, p.Type,
		"a parameter reaches the same declaration through the operation walk")
}

func assertDefaults(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	n, ok := propByWire(m, "n")
	require.True(t, ok)
	require.NotNil(t, n.Default)
	assert.Equal(t, ir.ValueNumber, n.Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), n.Default.Num)

	// A declaration-site default reaches the referencing property, and the
	// declaration keeps it verbatim because the type node has no field for it.
	viaRef, ok := propByWire(m, "viaRef")
	require.True(t, ok)
	require.NotNil(t, viaRef.Default, "the referent's default binds the reference site")
	assert.Equal(t, ir.BigVal("41"), viaRef.Default.Num)

	decl, ok := doc.Types[namedID("DeclaredDefault")].(*ir.Scalar)
	require.True(t, ok)
	entry, ok := decl.Unmodeled["openapi:default"]
	require.True(t, ok, "the declaration's own default is kept as residue")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `41`, string(entry.Value))

	assertNonScalarDefaults(t, m)
}

// assertNonScalarDefaults pins the Value payloads a default reaches beyond string
// and number: a boolean, a base64 !!binary scalar, and a sequence. Each selects a
// different payload field, and Kind is what says which one carries meaning.
func assertNonScalarDefaults(t *testing.T, m *ir.Model) {
	t.Helper()
	flag, ok := propByWire(m, "flag")
	require.True(t, ok)
	require.NotNil(t, flag.Default)
	assert.Equal(t, ir.Value{Kind: ir.ValueBool, Bool: true}, *flag.Default)

	blob, ok := propByWire(m, "blob")
	require.True(t, ok)
	require.NotNil(t, blob.Default)
	assert.Equal(t, ir.ValueBytes, blob.Default.Kind)
	assert.Equal(t, []byte("hi"), blob.Default.Bytes, "a !!binary default is decoded, not kept as text")

	list, ok := propByWire(m, "list")
	require.True(t, ok)
	require.NotNil(t, list.Default)
	assert.Equal(t, ir.ValueList, list.Default.Kind)
	require.Len(t, list.Default.List, 2)
	assert.Equal(t, ir.BigVal("1"), list.Default.List[0].Num)
	assert.Equal(t, ir.BigVal("2"), list.Default.List[1].Num)
}

// assertYAMLTimestampScalars covers a YAML 1.1 quirk: an unquoted date like
// 2021-01-01 resolves to tag !!timestamp, not !!str. It must survive as the
// literal string everywhere OpenAPI's JSON data model can carry one — enum,
// const, a property default, a schema-level example, and a media-type example —
// with nothing dropped or degraded to null.
//
// "Everywhere" spans both channels a date can land in. It used to mean only the
// ir.Value one, and the raw-JSON one went on rewriting a date to RFC 3339 with
// this fixture green (GitHub #242); assertRawPreservedDates is the other half.
func assertYAMLTimestampScalars(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	// R's `not` announces the §4.7 carve-out, and that notice is the only thing
	// any site here is allowed to say: a date that was dropped or degraded
	// reports itself as a warning, which is what this fixture exists to catch.
	for _, d := range diags {
		assert.Equal(t, ir.SeverityInfo, d.Severity,
			"every unquoted date converts cleanly; nothing is dropped or degraded: %+v", d)
		assert.Equal(t, "openapi/validation-only-keyword", d.Code,
			"the §4.7 carve-out is the only notice this fixture expects: %+v", d)
	}

	d, ok := doc.Types[namedID("D")].(*ir.Enum)
	require.True(t, ok, "D stays a closed Enum of the real dates, not a union of null literals")
	assert.Equal(t, ir.PrimString, d.ValueType)
	require.Len(t, d.Members, 2)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, d.Members[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2022-02-02"}, d.Members[1].Value)

	k, ok := doc.Types[namedID("K")].(*ir.Literal)
	require.True(t, ok, "K's const hoists a real Literal, not the schemaless top type")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, k.Value)

	s, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	prop := s.Properties[0]
	require.NotNil(t, prop.Default, "the property default is preserved")
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *prop.Default)
	require.Len(t, prop.Examples, 1, "the schema-level example is preserved")
	require.NotNil(t, prop.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *prop.Examples[0].Value)

	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	mediaExamples := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, mediaExamples, 1, "the media-type example is preserved")
	require.NotNil(t, mediaExamples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "2021-01-01"}, *mediaExamples[0].Value)

	assertRawPreservedDates(t, doc)
}

// assertRawPreservedDates pins the source spelling of a date kept as raw JSON,
// at both sites that channel has: a vendor extension and the §4.7
// validation-only carve-out. Each wants the text the source wrote — resolving
// the tag and re-rendering it hands the construct a padding, a time and a zone
// nobody asked for, and the resolved form cannot be read back to the spelling.
func assertRawPreservedDates(t *testing.T, doc *ir.Document) {
	t.Helper()
	r, ok := doc.Types[namedID("R")].(*ir.Model)
	require.True(t, ok)
	kept := r.Unmodeled

	for _, tc := range []struct{ key, want string }{
		{"openapi:x-effective", `"2021-1-1"`},
		{"openapi:x-window", `{"from":"2021-1-1","to":"2022-2-2"}`},
		{"openapi:not", `{"const":"2021-1-1"}`},
	} {
		entry, found := kept[tc.key]
		require.True(t, found, "%s is preserved; got %v", tc.key, kept)
		assert.Equal(t, tc.want, string(entry.Value),
			"%s keeps the date as written, not as RFC 3339 spells it", tc.key)
	}
}

func assertConstraints(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	ratio, ok := propByWire(m, "ratio")
	require.True(t, ok)
	c := ratio.Constraints
	require.NotNil(t, c)
	// Exact decimal strings — a float64 path would corrupt all three.
	require.NotNil(t, c.Min)
	require.NotNil(t, c.Max)
	require.NotNil(t, c.MultipleOf)
	assert.Equal(t, ir.BigVal("0.30000000000000004"), *c.Min)
	assert.Equal(t, ir.BigVal("9007199254740993"), *c.Max)
	assert.Equal(t, ir.BigVal("0.1"), *c.MultipleOf)

	// The object's own cardinality bound lands on the Model, not on a property
	// and not nowhere (GitHub #129).
	require.NotNil(t, m.Constraints)
	require.NotNil(t, m.Constraints.MinProps)
	require.NotNil(t, m.Constraints.MaxProps)
	assert.Equal(t, int64(1), *m.Constraints.MinProps)
	assert.Equal(t, int64(4), *m.Constraints.MaxProps)

	assertLengthAndCollectionBounds(t, doc, m)
	assertCoDeclaredBounds(t, m, diags)
}

// assertCoDeclaredBounds pins the 2020-12 rule that a side declaring both of
// its keywords keeps the tighter of the two: the property bounded below keeps
// its minimum, the one bounded above keeps its exclusiveMaximum, and each side
// names the keyword that did not reach the IR (GitHub #33).
//
// Both directions are here on purpose. A case where only the exclusive keyword
// survives passes just as well on the reader that always took it, so on its own
// it would say nothing about the fix.
func assertCoDeclaredBounds(t *testing.T, m *ir.Model, diags []ir.Diagnostic) {
	t.Helper()
	low, ok := propByWire(m, "atLeastTen")
	require.True(t, ok)
	require.NotNil(t, low.Constraints)
	require.NotNil(t, low.Constraints.Min)
	assert.Equal(t, ir.BigVal("10"), *low.Constraints.Min, "minimum is the tighter bound")
	assert.False(t, low.Constraints.ExclusiveMin, "and it is inclusive as written")

	high, ok := propByWire(m, "underTen")
	require.True(t, ok)
	require.NotNil(t, high.Constraints)
	require.NotNil(t, high.Constraints.Max)
	assert.Equal(t, ir.BigVal("10"), *high.Constraints.Max, "exclusiveMaximum is the tighter bound")
	assert.True(t, high.Constraints.ExclusiveMax)

	for _, want := range []string{"dropped exclusiveMinimum", "dropped maximum"} {
		assert.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
			return strings.Contains(d.Message, want)
		}), "a keyword the IR does not carry is reported, not dropped in silence: %q", want)
	}
}

// assertLengthAndCollectionBounds pins the non-numeric bounds: a string length
// pair on the declaring property, and a collection bound on the List the array
// position hoisted, which is the node that describes the collection.
func assertLengthAndCollectionBounds(t *testing.T, doc *ir.Document, m *ir.Model) {
	t.Helper()
	label, ok := propByWire(m, "label")
	require.True(t, ok)
	require.NotNil(t, label.Constraints)
	require.NotNil(t, label.Constraints.MinLength)
	require.NotNil(t, label.Constraints.MaxLength)
	assert.Equal(t, int64(2), *label.Constraints.MinLength)
	assert.Equal(t, int64(8), *label.Constraints.MaxLength)

	tags, ok := propByWire(m, "tags")
	require.True(t, ok)
	list, ok := doc.Types[tags.Type.Target].(*ir.List)
	require.True(t, ok, "an array position hoists a List")
	require.NotNil(t, list.Constraints)
	require.NotNil(t, list.Constraints.MinItems)
	require.NotNil(t, list.Constraints.MaxItems)
	assert.Equal(t, int64(1), *list.Constraints.MinItems)
	assert.Equal(t, int64(5), *list.Constraints.MaxItems)
	assert.True(t, list.Constraints.UniqueItems)
}

func assertNumericPrecision(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)

	// Bounds beyond float64 range and a huge integer beyond int64, exact.
	bounded, ok := propByWire(m, "bounded")
	require.True(t, ok)
	require.NotNil(t, bounded.Constraints)
	require.NotNil(t, bounded.Constraints.Min)
	require.NotNil(t, bounded.Constraints.Max)
	require.NotNil(t, bounded.Constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("1.8e308"), *bounded.Constraints.Min)
	assert.Equal(t, ir.BigVal("123456789012345678901234567890"), *bounded.Constraints.Max)
	assert.Equal(t, ir.BigVal("1e-30"), *bounded.Constraints.MultipleOf)

	// A leading-dot spelling is canonicalized to JSON form; a high-precision
	// decimal is kept to the last digit.
	exclusive, ok := propByWire(m, "exclusive")
	require.True(t, ok)
	require.NotNil(t, exclusive.Constraints)
	require.NotNil(t, exclusive.Constraints.Min)
	require.NotNil(t, exclusive.Constraints.Max)
	assert.True(t, exclusive.Constraints.ExclusiveMin)
	assert.True(t, exclusive.Constraints.ExclusiveMax)
	assert.Equal(t, ir.BigVal("0.5"), *exclusive.Constraints.Min)
	assert.Equal(t, ir.BigVal("0.12345678901234567890123456789"), *exclusive.Constraints.Max)

	// A default beyond float64 range is captured as a number, not a string.
	withDefault, ok := propByWire(m, "withDefault")
	require.True(t, ok)
	require.NotNil(t, withDefault.Default)
	assert.Equal(t, ir.ValueNumber, withDefault.Default.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), withDefault.Default.Num)

	// A const beyond float64 range hoists a Literal over the exact number.
	pinned, ok := propByWire(m, "pinned")
	require.True(t, ok)
	lit, ok := doc.Types[pinned.Type.Target].(*ir.Literal)
	require.True(t, ok)
	assert.Equal(t, ir.ValueNumber, lit.Value.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), lit.Value.Num)

	// Numeric enum members keep their exact value past int64 range.
	choice, ok := propByWire(m, "choice")
	require.True(t, ok)
	enum, ok := doc.Types[choice.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("123456789012345678901234567890"), enum.Members[1].Value.Num)

	// A leading-dot example is canonicalized losslessly.
	sampled, ok := propByWire(m, "sampled")
	require.True(t, ok)
	require.Len(t, sampled.Examples, 1)
	require.NotNil(t, sampled.Examples[0].Value)
	assert.Equal(t, ir.BigVal("0.5"), sampled.Examples[0].Value.Num)

	assertYAMLIntegerBases(t, doc, m)
	assertRawPreservedNumbers(t, m)
}

// assertRawPreservedNumbers pins the second numeric channel: the constructs kept
// verbatim as raw JSON rather than lowered into a Value. It rounded every one of
// these through float64 until GitHub #32 — a 30-digit extension came back as
// 1.2345678901234568e+29 — which no case in this corpus could see, because none
// of them carried a number a float64 cannot hold.
func assertRawPreservedNumbers(t *testing.T, m *ir.Model) {
	t.Helper()
	preserved, ok := propByWire(m, "preserved")
	require.True(t, ok)
	kept := preserved.Unmodeled

	for _, tc := range []struct{ key, want string }{
		{"openapi:x-precise-limit", "123456789012345678901234567890"},
		{"openapi:x-precise-step", "1.000000000000000000001"},
		{"openapi:x-precise-scale", "1.10"},
		{"openapi:not", `{"const":123456789012345678901234567890}`},
	} {
		entry, found := kept[tc.key]
		require.True(t, found, "%s is preserved; got %v", tc.key, kept)
		assert.Equal(t, tc.want, string(entry.Value),
			"%s is preserved as written, not as a float64 can spell it", tc.key)
	}
}

// assertYAMLIntegerBases pins the value every numeric site stores for an integer
// whose YAML base comes from its prefix. Reading the source spelling as base 10
// would store 644 where the document said 420, and would do it silently.
func assertYAMLIntegerBases(t *testing.T, doc *ir.Document, m *ir.Model) {
	t.Helper()
	mode, ok := propByWire(m, "mode")
	require.True(t, ok)
	require.NotNil(t, mode.Default)
	assert.Equal(t, ir.BigVal("420"), mode.Default.Num, "0644 is octal in YAML")

	enum, ok := doc.Types[mode.Type.Target].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, enum.Members, 2)
	assert.Equal(t, ir.BigVal("420"), enum.Members[0].Value.Num)
	assert.Equal(t, ir.BigVal("493"), enum.Members[1].Value.Num)

	bounds, ok := propByWire(m, "bounds")
	require.True(t, ok)
	require.NotNil(t, bounds.Constraints)
	require.NotNil(t, bounds.Constraints.Min)
	require.NotNil(t, bounds.Constraints.Max)
	require.NotNil(t, bounds.Constraints.MultipleOf)
	assert.Equal(t, ir.BigVal("15"), *bounds.Constraints.Min, "017 is octal")
	assert.Equal(t, ir.BigVal("31"), *bounds.Constraints.Max, "0x1f is hex")
	assert.Equal(t, ir.BigVal("10"), *bounds.Constraints.MultipleOf, "0b1010 is binary")
	require.Len(t, bounds.Examples, 1)
	require.NotNil(t, bounds.Examples[0].Value)
	assert.Equal(t, ir.BigVal("30"), bounds.Examples[0].Value.Num, "0b11110 is binary")

	// "_" is a separator, and a leading zero YAML resolves as a decimal float
	// still has to come out as a JSON-valid literal.
	counted, ok := propByWire(m, "counted")
	require.True(t, ok)
	require.NotNil(t, counted.Default)
	assert.Equal(t, ir.BigVal("1000000"), counted.Default.Num)
	loose, ok := propByWire(m, "loose")
	require.True(t, ok)
	require.NotNil(t, loose.Default)
	assert.Equal(t, ir.BigVal("9"), loose.Default.Num)
}

func assertReadOnlyWriteOnly(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	r, ok := propByWire(m, "r")
	require.True(t, ok)
	w, ok := propByWire(m, "w")
	require.True(t, ok)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, r.Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, w.Visibility)

	// A declaration-site readOnly reaches the referencing property, and the
	// declaration keeps it verbatim because the type node has no field for it.
	viaRef, ok := propByWire(m, "viaRef")
	require.True(t, ok)
	assert.Equal(t, r.Visibility, viaRef.Visibility, "the referent's readOnly binds the reference site")

	decl, ok := doc.Types[namedID("DeclaredReadOnly")].(*ir.Scalar)
	require.True(t, ok)
	entry, ok := decl.Unmodeled["openapi:readOnly"]
	require.True(t, ok, "the declaration's own readOnly is kept as residue")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `true`, string(entry.Value))

	assertBothFlagsAreVisibleNowhere(t, m, diags)
}

// assertBothFlagsAreVisibleNowhere pins the contradictory pairing in both its
// spellings: written on one schema, and written across a $ref that already
// carries the other flag. Each admits no lifecycle at all and each is reported,
// where declaring both used to lower exactly as readOnly alone did and say
// nothing (GitHub #276).
func assertBothFlagsAreVisibleNowhere(t *testing.T, m *ir.Model, diags []ir.Diagnostic) {
	t.Helper()
	for _, wire := range []string{"both", "bothViaRef"} {
		p, ok := propByWire(m, wire)
		require.True(t, ok)
		assert.Equal(t, ir.Visibility{None: true}, p.Visibility,
			"readOnly and writeOnly both in force admit no lifecycle: %s", wire)
		assert.Equal(t, []ir.Severity{ir.SeverityWarning},
			diagsAt(diags, diag.DisjointVisibility, "/components/schemas/S/properties/"+wire),
			"the contradiction is reported once, at the position that wrote it: %s", wire)
	}
}

func assertRecursive(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	n, ok := doc.Types[namedID("Node")].(*ir.Model)
	require.True(t, ok)
	next, ok := propByWire(n, "next")
	require.True(t, ok)
	assert.Equal(t, namedID("Node"), next.Type.Target, "self-reference terminates on the interned ID")
}

func assertMaps(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	om, ok := doc.Types[namedID("OpenMap")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, om.AdditionalProps)
	assert.Equal(t, ir.TypeID("t/prim/integer"), om.AdditionalProps.Value.Target)
	pm, ok := doc.Types[namedID("PatternMap")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, pm.AdditionalProps)
	require.Len(t, pm.AdditionalProps.Patterns, 1)
	assert.Equal(t, "^x-", pm.AdditionalProps.Patterns[0].Pattern)
	cr, ok := doc.Types[namedID("ClosedRecord")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosed, cr.Additional)
	ca, ok := doc.Types[namedID("ClosedAfter")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosedAfterComposition, ca.Additional)
}

func assertTuples(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	p, ok := doc.Types[namedID("Pair")].(*ir.Tuple)
	require.True(t, ok, "prefixItems hoists a Tuple")
	require.Len(t, p.Elems, 2)
	assert.Equal(t, ir.TypeID("t/prim/string"), p.Elems[0].Target)
	assert.Equal(t, ir.TypeID("t/prim/integer"), p.Elems[1].Target)
}

func assertLiteralConst(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	l, ok := doc.Types[namedID("Version")].(*ir.Literal)
	require.True(t, ok, "const hoists a Literal")
	assert.Equal(t, ir.ValueString, l.Value.Kind)
	assert.Equal(t, "v1", l.Value.Str)

	// An unrepresentable const still hoists a node, and a node that admits any
	// value is the only honest one: a Literal here would have to invent a value.
	_, ok = doc.Types[namedID("Opaque")].(*ir.Any)
	assert.True(t, ok, "an unconvertible const hoists the top type; got %T", doc.Types[namedID("Opaque")])
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Opaque"),
		"degrading a declared value is a warning, not silent")
}

func assertTagsGrouping(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	assert.Equal(t, "pets", doc.Services[0].Groups[0].Name.Source)
	require.Len(t, doc.TagDefs, 1)
	assert.Equal(t, "pets", doc.TagDefs[0].Name)
}

func assertHTTPBinding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	hb := op.Bindings.HTTP[0]
	assert.Equal(t, "GET", hb.Method)
	assert.Equal(t, "/items/{id}", hb.URITemplate)
}

func assertParamStyles(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "search")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	byParam := map[string]ir.HTTPParamBinding{}
	for _, pb := range op.Bindings.HTTP[0].ParamBindings {
		byParam[pb.Param] = pb
	}
	q := byParam["q"]
	assert.Equal(t, "form", q.Style, "query default style is form")
	require.NotNil(t, q.Explode)
	assert.True(t, *q.Explode, "form default explodes")
	assert.Equal(t, "deepObject", byParam["filter"].Style)
	assert.Equal(t, "application/json", byParam["payload"].ContentType, "content-style param records its media type")
	assert.True(t, byParam["path"].AllowReserved,
		"reserved characters passing through unescaped is a wire fact, not a style")
	assert.False(t, q.AllowReserved, "and the default is to escape them")

	assertAllowEmptyValueKept(t, op)

	// The path item's shared parameter merges into both of its operations and
	// interns its schema once, at the path item's own pointer (issue #36).
	clearSearch, ok := opByName(doc, "clearSearch")
	require.True(t, ok, "the path item's second operation is registered")
	searchReqID, ok := paramByName(op, "requestId")
	require.True(t, ok, "search inherits the path-item-level parameter")
	clearReqID, ok := paramByName(clearSearch, "requestId")
	require.True(t, ok, "clearSearch inherits the same path-item-level parameter")
	assert.Equal(t, searchReqID.Type.Target, clearReqID.Type.Target,
		"both operations resolve the shared path-item parameter to the same interned schema")
	assert.Equal(t, ir.TypeID("t/anon/paths/~1search/parameters/0/schema"), searchReqID.Type.Target,
		"the shared schema is hoisted at the path item's own pointer, not a per-operation one")
}

// assertAllowEmptyValueKept covers the one serialization flag ir.HTTPParamBinding
// has no field for. Style, explode, allowReserved and the content-style media
// type all land on the binding above; allowEmptyValue used to be read nowhere at
// all, so a document declaring it produced IR indistinguishable from one that
// did not (GitHub #39). It is kept on the logical Parameter, which is the
// carrier at this position with an Unmodeled map.
func assertAllowEmptyValueKept(t *testing.T, op ir.Operation) {
	t.Helper()
	bare, ok := paramByName(op, "bare")
	require.True(t, ok, "the allowEmptyValue parameter still lowers")
	entry, ok := bare.Unmodeled["openapi:allowEmptyValue"]
	require.True(t, ok, "and its declared flag is kept beside it")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `true`, string(entry.Value))
}

// assertParamRefInheritance pins ir-design §14 at a parameter whose schema is a
// $ref: docs, deprecation and default come from the referent when the use site is
// silent, and from the use site when it is not. Constraints inherit at neither
// carrier, so the identical property is asserted beside it — a parameter must not
// take more from a referent than a property does (GitHub #131).
func assertParamRefInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "listItems")
	require.True(t, ok)
	cursor, ok := paramByName(op, "cursor")
	require.True(t, ok)
	assert.Equal(t, ir.Docs{Summary: "Cursor", Description: "Opaque pagination cursor."}, cursor.Docs)
	assert.NotNil(t, cursor.Deprecation)
	require.NotNil(t, cursor.Default)
	assert.Equal(t, "0", cursor.Default.Str)
	assert.Nil(t, cursor.Constraints, "the referent's bound stays on the referent")

	override, ok := paramByName(op, "override")
	require.True(t, ok)
	assert.Equal(t, "Cursor for this endpoint only.", override.Docs.Description,
		"a description beside the $ref wins over the referent's")
	assert.Equal(t, "Cursor", override.Docs.Summary,
		"...and a keyword the use site is silent about still inherits")
	require.NotNil(t, override.Default)
	assert.Equal(t, "9", override.Default.Str)

	holder, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	prop, ok := propByWire(holder, "cursor")
	require.True(t, ok)
	assert.Equal(t, cursor.Docs, prop.Docs, "the property inherits exactly what the parameter does")
	assert.NotNil(t, prop.Deprecation)
	require.NotNil(t, prop.Default)
	assert.Equal(t, *cursor.Default, *prop.Default)
	assert.Nil(t, prop.Constraints, "neither carrier inherits the referent's constraints")

	decl, ok := doc.Types[namedID("Cursor")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, decl.Constraints)
	require.NotNil(t, decl.Constraints.MaxLength)
	assert.Equal(t, int64(64), *decl.Constraints.MaxLength,
		"a consumer that wants the bound reads it off the referent")
}

// assertHeaderContentSchema pins that both spellings of a header's type lower
// alike. The content spelling used to reach lowerHeader with no schema at all —
// the header became t/prim/any and its constraints and xml hints went with it,
// without a diagnostic (GitHub #139).
//
// The two headers declare the same type deliberately: comparing them against
// each other, rather than against a written-out expectation, is what makes the
// assertion about the spellings agreeing rather than about one of them.
func assertHeaderContentSchema(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "getReport")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	headers := op.Responses[0].Headers
	require.Len(t, headers, 4)

	bySchema, ok := headerByWire(headers, "X-Report-Schema")
	require.True(t, ok)
	byContent, ok := headerByWire(headers, "X-Report-Content")
	require.True(t, ok)

	assert.Equal(t, bySchema.Type, byContent.Type,
		"a content-style header resolves the same type as the schema spelling")
	assert.NotEqual(t, ir.TypeID("t/prim/any"), byContent.Type.Target,
		"the type is resolved rather than degraded to the top type")
	assert.Equal(t, bySchema.Constraints, byContent.Constraints,
		"the constraints written under content are not dropped with the schema")
	require.NotNil(t, byContent.XML)
	assert.Equal(t, "ReportContent", byContent.XML.Name,
		"the xml hints written under content reach the header")

	require.NotNil(t, byContent.Encoding)
	assert.Equal(t, "application/xml", byContent.Encoding.MediaType,
		"the media type serializing the header's value is kept, which the schema spelling has none of")
	assert.Nil(t, bySchema.Encoding, "the schema spelling names no media type")

	byRef, ok := headerByWire(headers, "X-Report-Ref")
	require.True(t, ok)
	assert.Equal(t, namedID("ReportID"), byRef.Type.Target,
		"a $ref under content resolves to the named component, not to an anonymous copy")

	assertHeaderStyleAndExplodeKept(t, headers)
	assertNoErrorDiags(t, diags)
}

// assertHeaderStyleAndExplodeKept covers the header keywords ir.Property has no
// field for. A header's explode decides whether a collection value is written as
// one repeated field or one joined value, so dropping it left the IR unable to
// say how the header goes on the wire — and it was dropped without a word
// (GitHub #39). Kept verbatim, since the IR can close the gap by adding fields.
func assertHeaderStyleAndExplodeKept(t *testing.T, headers []ir.Property) {
	t.Helper()
	list, ok := headerByWire(headers, "X-Report-List")
	require.True(t, ok)

	for key, want := range map[string]string{
		"openapi:style":   `"simple"`,
		"openapi:explode": `true`,
	} {
		entry, found := list.Unmodeled[key]
		require.True(t, found, "%s is kept", key)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		assert.JSONEq(t, want, string(entry.Value))
	}

	plain, ok := headerByWire(headers, "X-Report-Schema")
	require.True(t, ok)
	assert.NotContains(t, plain.Unmodeled, "openapi:explode",
		"a header that declares neither keyword records neither")
}

// headerByWire returns the response header with the given wire name.
func headerByWire(headers []ir.Property, wire string) (ir.Property, bool) {
	for _, h := range headers {
		if h.WireName == wire {
			return h, true
		}
	}
	return ir.Property{}, false
}

func assertMultiContent(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getData")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 2, "both media types kept, no primary selection")
	assert.Equal(t, "application/json", op.Responses[0].Payload.Contents[0].MediaType)
	assert.Equal(t, "application/xml", op.Responses[0].Payload.Contents[1].MediaType)
}

func assertMultipartEncoding(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "upload")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	enc := op.Request.Contents[0].Encoding
	require.NotEmpty(t, enc, "multipart parts carry PartEncoding keyed by PropID")
	var sawFile, sawMulti bool
	var fileHeaders []ir.Property
	for _, pe := range enc {
		if pe.Filename {
			sawFile = true
			fileHeaders = pe.Headers
		}
		if pe.Multi {
			sawMulti = true
		}
	}
	assert.True(t, sawFile, "binary file part carries Filename")
	assert.True(t, sawMulti, "array part carries Multi")
	assertPartHeaders(t, doc, fileHeaders)
	assertFormPartStyle(t, doc)
	assertComposedPartEncoding(t, doc)
}

// assertComposedPartEncoding pins the body that composes its parts rather than
// declaring them. Reading only the schema node's own properties found none on an
// allOf wrapper, so the whole encoding block was discarded with no diagnostic and
// a Content that still looked well-formed, just smaller (GitHub #140).
//
// The keys are asserted to be the properties the IR actually holds: §4.3 keeps a
// composed model's inherited parts on its Base, so a key derived from the
// composed node's own pointer would name a property that exists nowhere.
func assertComposedPartEncoding(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "uploadComposed")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)

	enc := op.Request.Contents[0].Encoding
	require.NotEmpty(t, enc, "a composed body keeps the encoding a declared one does")

	inherited := ir.PropID("p/openapi/components/schemas/UploadForm/properties/file")
	file, ok := enc[inherited]
	require.True(t, ok, "the inherited part is keyed on the Base that holds it; got keys %v", enc)
	assert.Equal(t, []string{"image/png"}, file.ContentTypes)
	assert.True(t, file.Filename, "the structural flag still comes from the part's own schema")

	// Every key must name a property some type in the document declares. A part
	// keyed by a PropID nothing holds is the failure this lowering can produce
	// while still looking well-formed.
	for id := range enc {
		assert.True(t, propIDExists(doc, id), "encoding key %s names no property in the document", id)
	}
}

// propIDExists reports whether any type in doc declares a property with the
// given ID.
func propIDExists(doc *ir.Document, id ir.PropID) bool {
	for _, td := range doc.Types {
		m, ok := td.(*ir.Model)
		if !ok {
			continue
		}
		for _, p := range m.Properties {
			if p.ID == id {
				return true
			}
		}
	}
	return false
}

// assertFormPartStyle pins the form-serialization half of a part's encoding: a
// urlencoded body's parts carry the same style/explode pair a query parameter
// does, on the part rather than on any binding.
func assertFormPartStyle(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "submitForm")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	assert.Equal(t, "application/x-www-form-urlencoded", op.Request.Contents[0].MediaType)
	enc := op.Request.Contents[0].Encoding
	require.Len(t, enc, 1)
	for _, pe := range enc {
		assert.Equal(t, "form", pe.Style)
		require.NotNil(t, pe.Explode)
		assert.True(t, *pe.Explode)
		assert.True(t, pe.Multi, "the structural flag still comes from the part's own schema")
	}
}

// assertFileBody pins a binary body: the payload's type degrades to bytes and the
// media types it is carried as move onto Content.File, which is what tells an
// emitter to generate a stream rather than a model. Both routes to that lowering
// are covered — a media type with no schema at all, and a string+binary schema.
func assertFileBody(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "putBlob")
	require.True(t, ok)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	req := op.Request.Contents[0]
	require.NotNil(t, req.File, "application/octet-stream with no schema is a file body")
	assert.Equal(t, []string{"application/octet-stream"}, req.File.ContentTypes)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), req.Type.Target)

	resp := firstContent(t, op)
	require.NotNil(t, resp.File, "a string+binary schema is a file body whatever the media type")
	assert.Equal(t, []string{"image/png"}, resp.File.ContentTypes)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), resp.Type.Target)
}

// assertPartHeaders pins encoding.<part>.headers, a TypeRef position reachable
// only through a PartEncoding and, before this case, exercised by no corpus
// spec. It covers both hops a header takes — an inline schema and one behind a
// $ref through components/headers — and requires each target to be in the
// registry, which is what gives the referential-closure walker something to
// check here.
func assertPartHeaders(t *testing.T, doc *ir.Document, headers []ir.Property) {
	t.Helper()
	require.Len(t, headers, 2, "the file part declares two per-part headers")
	byWire := make(map[string]ir.Property, len(headers))
	for _, h := range headers {
		byWire[h.WireName] = h
	}

	id, ok := byWire["X-Part-Id"]
	require.True(t, ok, "inline part header lowered; got %v", byWire)
	assert.Equal(t, ir.TypeID("t/prim/uuid"), id.Type.Target)
	assert.True(t, id.Required, "a required part header keeps its presence bit")
	assert.Equal(t, "per-part correlation id", id.Docs.Description)

	sum, ok := byWire["X-Part-Checksum"]
	require.True(t, ok, "$ref'd part header lowered; got %v", byWire)
	assert.Equal(t, namedID("Digest"), sum.Type.Target, "a $ref'd header takes the referent's schema")
	assert.Equal(t, "sha-256 digest of this part", sum.Docs.Description)
	for _, h := range headers {
		assert.Contains(t, doc.Types, h.Type.Target, "part header %q must reference a live node", h.WireName)
	}
}

// assertSequentialMedia pins the 3.2 sequential-media lowering, including the
// two forms per-item encoding takes: one Encoding governing every item lands in
// Content.ItemEncoding, while a positional prefixEncoding — which a single
// every-item encoding has no ordinals for — takes itself and the tail encoding
// beside it into Unmodeled instead.
func assertSequentialMedia(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	events, ok := opByName(doc, "streamEvents")
	require.True(t, ok)
	c := firstContent(t, events)
	require.NotNil(t, c.Item, "itemSchema becomes the element type")
	require.NotNil(t, c.ItemEncoding, "an encoding governing every item lowers structurally")
	assert.Equal(t, []string{"application/json"}, c.ItemEncoding.ContentTypes)
	assert.True(t, c.ItemEncoding.Multi, "the construct describes a repeated tail")
	assert.Empty(t, c.Unmodeled, "nothing is left over once it lowers")

	parts, ok := opByName(doc, "streamParts")
	require.True(t, ok)
	pc := firstContent(t, parts)
	assert.Nil(t, pc.ItemEncoding, "positional prefixes have no every-item form")
	for _, key := range []string{"openapi:prefixEncoding", "openapi:itemEncoding"} {
		entry, ok := pc.Unmodeled[key]
		require.True(t, ok, "%s kept verbatim; got %v", key, pc.Unmodeled)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	}
}

// firstContent returns an operation's single success-response content.
func firstContent(t *testing.T, op ir.Operation) ir.Content {
	t.Helper()
	require.Len(t, op.Responses, 1)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	return op.Responses[0].Payload.Contents[0]
}

func assertPerStatusErrors(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getWidgets")
	require.True(t, ok)
	require.Len(t, op.Responses, 1, "the 2xx success response")
	faults := map[string]ir.StatusRange{}
	byRange := map[ir.StatusRange]ir.ErrorCase{}
	var sawDefault bool
	for _, ec := range op.Errors {
		require.Len(t, ec.Conditions.StatusCodes, 1)
		rng := ec.Conditions.StatusCodes[0]
		byRange[rng] = ec
		if rng.From == 0 && rng.To == 0 {
			sawDefault = true
			assert.Empty(t, ec.Fault, "the default catch-all is unclassified")
			continue
		}
		faults[ec.Fault] = rng
	}
	assert.Equal(t, ir.StatusRange{From: 404, To: 404}, faults["client"])
	assert.Equal(t, ir.StatusRange{From: 500, To: 599}, faults["server"])
	assert.True(t, sawDefault, "the default response becomes a catch-all error case")

	assertErrorMediaTypeKept(t, byRange)
}

// assertErrorMediaTypeKept covers what ir.ErrorCase cannot say. It holds one
// TypeRef and no media type, so an error declared as application/problem+json
// reached the IR indistinguishable from one declared as application/json — the
// single-entry half of a gap whose multi-entry half was already kept, which is
// why it read as a deliberate asymmetry rather than a loss (GitHub #39). Both
// halves are now the same rule.
//
// The 5XX case is the control: an error response with no content at all keeps
// nothing, so the entry marks a declaration rather than appearing on every error.
func assertErrorMediaTypeKept(t *testing.T, byRange map[ir.StatusRange]ir.ErrorCase) {
	t.Helper()
	notFound, ok := byRange[ir.StatusRange{From: 404, To: 404}]
	require.True(t, ok)
	entry, ok := notFound.Unmodeled["openapi:content"]
	require.True(t, ok, "a single-media error keeps the map that names its media type")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t,
		`{"application/json":{"schema":{"$ref":"#/components/schemas/Err"}}}`, string(entry.Value))

	serverErr, ok := byRange[ir.StatusRange{From: 500, To: 599}]
	require.True(t, ok)
	assert.NotContains(t, serverErr.Unmodeled, "openapi:content",
		"an error response declaring no content keeps no content map")
}

func assertWebhooks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "onNewPet")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook, "webhook operation carries IsWebhook")
	assertPathItemServersKept(t, op, "https://hooks.example.com")
	assertOwnServersKeptBesideThem(t, op, "https://hooks-override.example.com")
	assertWebhookGroupIsAHint(t, doc)
}

// assertOwnServersKeptBesideThem pins the overriding half of the servers pair in
// the corpus, so the two-order oracle and the JSON round-trip see it. OpenAPI
// says an operation's own servers override its path item's, so keeping only the
// path item's recorded the superseded list and dropped the effective one.
//
// The two keys are asserted together because the hazard is that they collapse
// into one: a single key holding both would leave the surviving list depending
// on which lowering ran last, which only a two-order diff can see.
func assertOwnServersKeptBesideThem(t *testing.T, op ir.Operation, url string) {
	t.Helper()
	entry, ok := op.Unmodeled["openapi:operationServers"]
	require.True(t, ok, "the operation's own servers are kept beside its path item's")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `[{"url":"`+url+`"}]`, string(entry.Value))
	assert.NotEqual(t, entry.Value, op.Unmodeled["openapi:servers"].Value,
		"and the two keys hold different declarations, not one overwriting the other")
}

// assertPathItemServersKept pins that a path item's servers survive whichever
// parent the path item hangs from. The preserve-plus-diagnostic path used to be
// reached only from the `paths` walk, so the same override written on a webhook
// or a callback disappeared with nothing said (GitHub #39).
func assertPathItemServersKept(t *testing.T, op ir.Operation, url string) {
	t.Helper()
	entry, ok := op.Unmodeled["openapi:servers"]
	require.True(t, ok, "the path item's servers are kept on the operation")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `[{"url":"`+url+`"}]`, string(entry.Value))
}

// assertWebhookGroupIsAHint pins which of the two things the group's name is.
// The compiler synthesizes the group to hold webhook operations, so no document
// declares it and Naming.Source — the spelling the source used — is the wrong
// channel; the "default" group the tag walk synthesizes carries a Hint for the
// same reason (GitHub #184).
//
// Asserted here rather than left to the golden because the golden records the
// value either way: a reader diffing it sees a name move between two fields
// without being told which one is correct.
func assertWebhookGroupIsAHint(t *testing.T, doc *ir.Document) {
	t.Helper()
	require.Len(t, doc.Services, 1)
	for _, g := range doc.Services[0].Groups {
		if g.Name.Hint != "webhooks" {
			continue
		}
		assert.Empty(t, g.Name.Source, "a group no document declares carries no source spelling")
		assert.Empty(t, g.Name.Canonical, "and no words derived from one")
		return
	}
	t.Fatal("no group carries the webhooks hint")
}

func assertCallbacks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "subscribe")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, "{$request.body#/callbackUrl}", op.Bindings.HTTP[0].Callbacks[0].Expression)
	assert.NotEmpty(t, op.Bindings.HTTP[0].Callbacks[0].Operations)
	cb, ok := opByName(doc, "onEvent")
	require.True(t, ok, "the callback operation is registered alongside its parent")
	assertPathItemServersKept(t, cb, "https://callbacks.example.com")
	assert.NotContains(t, op.Unmodeled, "openapi:servers",
		"the callback's own servers stay on the callback, not on the parent")
}

// assertPathItemDocs pins that a path item's own documentation survives at every
// route that reaches a path item, and that keeping it costs the operation
// nothing it declared for itself.
//
// It is kept rather than merged: ir.Docs holds the operation's summary and
// description, a path item's pair documents the path, and merging the two would
// need a precedence rule and would attach to an operation text its author never
// wrote. So the assertion is that both subjects survive side by side.
func assertPathItemDocs(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	for _, tc := range []struct{ op, summary, description string }{
		{"listPets", "Pet collection", "Everything addressable at /pets."},
		{"createPet", "Pet collection", "Everything addressable at /pets."},
		{"onPetCreated", "Creation callback", "Delivered once the pet exists."},
		{"onPetDeleted", "Pet deleted", "Delivered when a pet is removed."},
	} {
		op, ok := opByName(doc, tc.op)
		require.True(t, ok, "operation %s", tc.op)
		assertPathItemDocsKept(t, op, tc.summary, tc.description)
	}

	listPets, ok := opByName(doc, "listPets")
	require.True(t, ok)
	assert.Equal(t, "List pets", listPets.Docs.Summary,
		"the operation's own summary is what Docs holds")
	assert.Equal(t, "Returns every pet.", listPets.Docs.Description)

	createPet, ok := opByName(doc, "createPet")
	require.True(t, ok)
	assert.Empty(t, createPet.Docs.Summary,
		"and an operation that documents nothing has nothing invented for it")

	// And the decision is announced at the operation that carries the entry,
	// once, rather than being taken silently.
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/paths/~1pets/get"))
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/webhooks/petDeleted/post"))
}

// assertPathItemDocsKept checks one operation kept the pair its own path item
// declared, under the keys that name which object the text came from.
func assertPathItemDocsKept(t *testing.T, op ir.Operation, summary, description string) {
	t.Helper()
	kept, ok := op.Unmodeled["openapi:pathItemSummary"]
	require.True(t, ok, "the path item's summary is kept on %s", op.ID)
	assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
	assert.JSONEq(t, `"`+summary+`"`, string(kept.Value))

	kept, ok = op.Unmodeled["openapi:pathItemDescription"]
	require.True(t, ok, "the path item's description is kept on %s", op.ID)
	assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
	assert.JSONEq(t, `"`+description+`"`, string(kept.Value))
}

// assertPathItemOperations pins that every operation a 3.2 path item declares
// becomes an ir.Operation, whichever field declared it and whichever of the
// three routes reaches the path item.
//
// The walk read the fixed method fields and nothing else, so an
// additionalOperations entry was dropped entire — and with it every type
// reachable only through it, which is why the request body's schema is asserted
// to be in the registry rather than merely referenced from an operation.
func assertPathItemOperations(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	for _, tc := range []struct{ op, method string }{
		{"queryIndex", "QUERY"},
		{"purgeIndex", "PURGE"},
		{"mixedCaseIndex", "mIxEdCase"},
		{"purgeCallback", "PURGE"},
		{"onFlush", "FLUSH"},
	} {
		op, ok := opByName(doc, tc.op)
		require.True(t, ok, "operation %s", tc.op)
		require.Len(t, op.Bindings.HTTP, 1)
		assert.Equal(t, tc.method, op.Bindings.HTTP[0].Method,
			"%s binds the method as the source spelled it", tc.op)
	}

	onFlush, ok := opByName(doc, "onFlush")
	require.True(t, ok)
	assert.True(t, onFlush.Bindings.HTTP[0].IsWebhook,
		"a webhook mount marks the binding whichever field declared the operation")

	subscribe, ok := opByName(doc, "subscribeIndex")
	require.True(t, ok)
	require.Len(t, subscribe.Bindings.HTTP, 1)
	require.Len(t, subscribe.Bindings.HTTP[0].Callbacks, 1)
	purgeCallback, ok := opByName(doc, "purgeCallback")
	require.True(t, ok)
	assert.Equal(t, []ir.OpID{purgeCallback.ID}, subscribe.Bindings.HTTP[0].Callbacks[0].Operations,
		"an expression declaring only additionalOperations still binds to its parent")

	queryIndex, ok := opByName(doc, "queryIndex")
	require.True(t, ok)
	require.NotNil(t, queryIndex.Request)
	require.Len(t, queryIndex.Request.Contents, 1)
	assert.NotNil(t, doc.Types[queryIndex.Request.Contents[0].Type.Target],
		"the request body schema is interned, not merely referenced")
}

func assertDeprecation(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "oldOp")
	require.True(t, ok)
	assert.NotNil(t, op.Deprecation)
	m, ok := doc.Types[namedID("OldModel")].(*ir.Model)
	require.True(t, ok)
	assert.NotNil(t, m.Deprecation)
	require.Len(t, m.Properties, 1)
	assert.NotNil(t, m.Properties[0].Deprecation)

	// A parameter is its own carrier: deprecated written on the parameter object
	// reaches it without any schema being involved.
	legacy, ok := paramByName(op, "legacy")
	require.True(t, ok)
	assert.NotNil(t, legacy.Deprecation)
}

func assertExamples(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)

	// A schema that reduced to a shared primitive keeps its examples on the
	// declaring property; one that hoists a node of its own keeps them there.
	n, ok := propByWire(m, "n")
	require.True(t, ok)
	assert.Len(t, n.Examples, 2)
	inner, ok := propByWire(m, "inner")
	require.True(t, ok)
	assert.Empty(t, inner.Examples, "an inline object holds its own examples")
	require.Len(t, doc.Types[inner.Type.Target].Common().Examples, 1)

	// A component's example belongs to the component, not to a use site.
	c, ok := doc.Types[namedID("C")]
	require.True(t, ok)
	require.Len(t, c.Common().Examples, 1)
	require.NotNil(t, c.Common().Examples[0].Value)
	assert.Equal(t, "component-level", c.Common().Examples[0].Value.Str)

	// A parameter carries its own examples, keyed by their map key like a media
	// type's.
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	q, ok := paramByName(op, "q")
	require.True(t, ok)
	require.Len(t, q.Examples, 1)
	assert.Equal(t, "one", q.Examples[0].Name)
	require.NotNil(t, q.Examples[0].Value)
	assert.Equal(t, "first", q.Examples[0].Value.Str)

	assertResponseExamples(t, doc)
}

// assertResponseExamples pins the response-side example sites: the plural map on
// a media type, in every spelling the spec allows, and a response header.
func assertResponseExamples(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	ex := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, ex, 4, "every entry lowers, in source order")

	// An inline entry and one written as a $ref, which must resolve to the
	// referenced component's value. Both are named by their map key.
	require.NotNil(t, ex[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "hello"}, *ex[0].Value)
	assert.Equal(t, "inline", ex[0].Name)
	require.NotNil(t, ex[1].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "world"}, *ex[1].Value)
	assert.Equal(t, "shared", ex[1].Name)

	// The annotations that surround a value travel with it.
	assert.Equal(t, "a summary", ex[2].Summary)
	assert.Equal(t, "a description", ex[2].Description)

	// An externalValue entry carries no inline value, and is kept for the URL.
	assert.Equal(t, "https://example.com/e.json", ex[3].ExternalURL)
	assert.Nil(t, ex[3].Value)
	assert.Equal(t, "hosted elsewhere", ex[3].Summary)

	headers := op.Responses[0].Headers
	require.Len(t, headers, 1)
	require.Len(t, headers[0].Examples, 1)
	require.NotNil(t, headers[0].Examples[0].Value)
	assert.Equal(t, ir.BigVal("5"), headers[0].Examples[0].Value.Num)
}

func assertDocsSummaryDesc(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "ping")
	require.True(t, ok)
	assert.Equal(t, "Ping the server", op.Docs.Summary)
	assert.Equal(t, "Returns pong", op.Docs.Description)
	require.Len(t, op.Docs.ExternalDocs, 1)
	assert.Equal(t, ir.Link{URL: "https://example.com/docs/ping", Description: "Ping reference"},
		op.Docs.ExternalDocs[0])

	// A documented scalar alias is documented too: its docs must not depend on
	// what its body happens to lower to (GitHub #114).
	sc, ok := doc.Types[namedID("UserId")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, "User identifier", sc.Docs.Summary)
	assert.Equal(t, "Stable identifier for a user.", sc.Docs.Description)

	// info and the root externalDocs are one Docs on the document: summary and
	// description come from info, the link from beside it.
	assert.Equal(t, "A one-line summary of the API.", doc.Docs.Summary)
	assert.Equal(t, "The long-form description of the API.", doc.Docs.Description)
	require.Len(t, doc.Docs.ExternalDocs, 1)
	assert.Equal(t, ir.Link{URL: "https://example.com/docs", Description: "Full guide"},
		doc.Docs.ExternalDocs[0])
	require.NotNil(t, doc.License)
	assert.Equal(t, ir.License{Name: "MIT", Identifier: "MIT"}, *doc.License,
		"the 3.1 SPDX identifier is its own field, never folded into the name")
}

func assertExtensionsX(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	raw, ok := m.Unmodeled["openapi:x-rate-limit"]
	require.True(t, ok, "x-* extensions are namespaced under openapi:")
	assert.JSONEq(t, "100", string(raw.Value))
	assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)

	// The same rule applies at every object that admits an extension, so the
	// document root and an operation each keep their own.
	root, ok := doc.Unmodeled["openapi:x-audience"]
	require.True(t, ok, "a root extension lands on the document; got %v", doc.Unmodeled)
	assert.Equal(t, ir.ReasonVendorExtension, root.Reason)
	assert.JSONEq(t, `"public"`, string(root.Value))

	op, ok := opByName(doc, "listWidgets")
	require.True(t, ok)
	entry, ok := op.Unmodeled["openapi:x-internal"]
	require.True(t, ok, "an operation extension lands on the operation; got %v", op.Unmodeled)
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
	assert.JSONEq(t, `true`, string(entry.Value))

	assertRawPreservedBinary(t, m)
}

// assertRawPreservedBinary pins what a !!binary extension keeps: the base64 the
// source wrote. Decoding it and storing the bytes in a JSON string lost the
// spelling on every value and lost the data itself on any byte that is not
// valid UTF-8, since encoding/json rewrites those to U+FFFD (GitHub #242).
//
// The block-form row carries the line breaks with it, because they are part of
// what the source wrote; base64.StdEncoding skips them, so a consumer resolving
// the tag reads the same bytes the decode used to store.
func assertRawPreservedBinary(t *testing.T, m *ir.Model) {
	t.Helper()
	for _, tc := range []struct{ key, want string }{
		{"openapi:x-blob", `"aGVsbG8="`},
		{"openapi:x-raw", `"/w=="`},
		{"openapi:x-wrapped", `"aGVs\nbG8=\n"`},
	} {
		entry, found := m.Unmodeled[tc.key]
		require.True(t, found, "%s is preserved; got %v", tc.key, m.Unmodeled)
		assert.Equal(t, tc.want, string(entry.Value),
			"%s keeps its base64 text, not the bytes it names", tc.key)
	}
}

// assertInlineAnnotations covers the positions with no ir.Property or
// ir.Parameter to carry what a declaration writes: each keeps its docs, bounds
// and x-* on a node of its own, while a position declaring nothing still
// resolves straight to the shared primitive.
func assertInlineAnnotations(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	elem := assertAnnotatedScalar(t, doc, "t/anon/components/schemas/Codes/items", 3)
	require.NotNil(t, elem.XML)
	assert.Equal(t, "Code", elem.XML.Name)
	assert.Contains(t, elem.Unmodeled, "openapi:x-facet")

	assertAnnotatedScalar(t, doc, "t/anon/components/schemas/CodeIndex/additionalProperties", 64)
	assertAnnotatedScalar(t, doc,
		"t/anon/paths/~1codes/get/responses/200/content/application~1json/schema", 8192)

	bare, ok := doc.Types[namedID("Bare")].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/string"), bare.Elem.Target,
		"an element declaring nothing must not grow a node of its own")

	op, ok := opByName(doc, "listCodes")
	require.True(t, ok)
	require.Len(t, op.Params, 1)
	assertCarriedAnnotations(t, op.Params[0].Docs, op.Params[0].Constraints, op.Params[0].Unmodeled, 4)
	require.Len(t, op.Responses[0].Headers, 1)
	h := op.Responses[0].Headers[0]
	assertCarriedAnnotations(t, h.Docs, h.Constraints, h.Unmodeled, 64)
}

// assertAnnotatedScalar requires the node at id to be a Scalar carrying a
// description and the given maxLength.
func assertAnnotatedScalar(t *testing.T, doc *ir.Document, id ir.TypeID, maxLength int64) *ir.Scalar {
	t.Helper()
	sc, ok := doc.Types[id].(*ir.Scalar)
	require.True(t, ok, "%s must own a Scalar; got %v", id, doc.Types[id])
	assert.NotEmpty(t, sc.Docs.Description)
	require.NotNil(t, sc.Constraints)
	require.NotNil(t, sc.Constraints.MaxLength)
	assert.Equal(t, maxLength, *sc.Constraints.MaxLength)
	return sc
}

// assertCarriedAnnotations requires a parameter's or header's own carrier to
// hold what its schema declared.
func assertCarriedAnnotations(t *testing.T, docs ir.Docs, c *ir.Constraints, p ir.Unmodeled, maxLength int64) {
	t.Helper()
	assert.NotEmpty(t, docs.Description)
	require.NotNil(t, c)
	require.NotNil(t, c.MaxLength)
	assert.Equal(t, maxLength, *c.MaxLength)
	assert.NotEmpty(t, p, "the schema's x-* rides on the carrier too")
}

func assertServersVariables(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "https://{env}.example.com/v1", doc.Servers[0].URLTemplate)
	assert.Equal(t, "primary", doc.Servers[0].Name.Source,
		"a 3.2 server name is the server's naming, not part of its description")
	require.Len(t, doc.Servers[0].Variables, 1)
	v := doc.Servers[0].Variables[0]
	assert.Equal(t, "env", v.Name)
	assert.Equal(t, "api", v.Default)
	assert.Equal(t, []string{"api", "staging"}, v.Enum)
}

func assertSecuritySchemes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	kinds := map[ir.AuthKind]bool{}
	for _, s := range doc.Auth {
		kinds[s.Kind] = true
	}
	assert.True(t, kinds[ir.AuthKindAPIKey])
	assert.True(t, kinds[ir.AuthKindHTTPBasic])
	assert.True(t, kinds[ir.AuthKindHTTPBearer])
	assert.True(t, kinds[ir.AuthKindOAuth2])
	assert.True(t, kinds[ir.AuthKindOpenIDConnect])
	assert.True(t, kinds[ir.AuthKindMutualTLS])
	assertSchemeDetail(t, doc)
}

// assertSchemeDetail pins the per-scheme detail beyond the mechanism kind: the
// annotations any scheme can carry, the RFC 7235 token an unmodelled HTTP scheme
// degrades to, and the two OAuth2 endpoints beyond the token URL.
func assertSchemeDetail(t *testing.T, doc *ir.Document) {
	t.Helper()
	byName := make(map[string]ir.AuthScheme, len(doc.Auth))
	for _, s := range doc.Auth {
		byName[s.Name.Source] = s
	}

	key, ok := byName["apiKeyAuth"]
	require.True(t, ok)
	assert.Equal(t, "Static per-tenant key.", key.Docs.Description)
	assert.NotNil(t, key.Deprecation, "a scheme can be deprecated like any other entity")
	entry, ok := key.Unmodeled["openapi:x-rotation-days"]
	require.True(t, ok, "a scheme's x-* extensions ride on the scheme; got %v", key.Unmodeled)
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)

	digest, ok := byName["digestAuth"]
	require.True(t, ok)
	assert.Equal(t, ir.AuthKindCustom, digest.Kind, "only basic and bearer get first-class kinds")
	assert.Equal(t, "digest", digest.Scheme, "the token itself is kept rather than dropped")

	stray, ok := byName["strayFieldAuth"]
	require.True(t, ok)
	assert.Empty(t, stray.BearerFormat, "an apiKey has no bearer-token format to fill")
	kept, ok := stray.Unmodeled["openapi:bearerFormat"]
	require.True(t, ok, "a field the type does not define is kept, not dropped; got %v", stray.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
	assert.Equal(t, ir.RawValue(`"JWT"`), kept.Value, "kept as the document wrote it")

	oauth, ok := byName["oauth2Auth"]
	require.True(t, ok)
	assert.Equal(t, "https://example.com/.well-known/oauth-authorization-server",
		oauth.OAuth2MetadataURL)
	require.Len(t, oauth.Flows, 1)
	assert.Equal(t, "https://example.com/refresh", oauth.Flows[0].RefreshURL)
}

func assertSecurityOrAnd(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	auth := doc.Services[0].Auth
	require.Len(t, auth, 3, "OR of three options in source order")
	assert.Len(t, auth[0].Schemes, 1)
	assert.Len(t, auth[1].Schemes, 2, "the ANDed option requires two schemes together")
	assert.Empty(t, auth[2].Schemes, "the empty option means no auth is acceptable")
	op, ok := opByName(doc, "publicOp")
	require.True(t, ok)
	assert.Empty(t, op.Auth, "security: [] makes the operation explicitly public")
}
