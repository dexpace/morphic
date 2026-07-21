package graphql_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/graphql"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// conformanceDir is the corpus of one minimal schema per capability row of
// ir-spec-matrix.md that GraphQL (and Apollo Federation) can express.
const conformanceDir = "../../testdata/conformance/graphql"

// TestConformance drives one minimal schema per GraphQL-expressible capability
// through the full compiler and asserts lossless capture: a focused
// capability-specific assertion plus a byte-exact golden IR snapshot. Regenerate
// the goldens with `go test ./compilers/graphql -run TestConformance -update`.
func TestConformance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file   string
		assert func(*testing.T, *ir.Document, []ir.Diagnostic)
	}{
		{"object-types", assertObjectTypes},
		{"interfaces", assertInterfaces},
		{"unions", assertUnions},
		{"oneof-input", assertOneOfInput},
		{"enums", assertEnums},
		{"custom-scalars", assertCustomScalars},
		{"input-objects", assertInputObjects},
		{"field-arguments", assertFieldArguments},
		{"nullability", assertNullability},
		{"recursive", assertRecursive},
		{"operations", assertOperations},
		{"deprecation", assertDeprecation},
		{"directives", assertDirectives},
		{"docs", assertDocs},
		{"federation-v1", assertFederationV1},
		{"federation-v2", assertFederationV2},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseCorpus(t, tc.file)
			assertNoErrorDiags(t, diags)
			tc.assert(t, doc, diags)
			irtest.CompareGolden(t, filepath.Join(conformanceDir, tc.file+".golden.json"), doc)
		})
	}
}

// parseCorpus reads and parses one corpus schema through the full compiler.
func parseCorpus(t *testing.T, name string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".graphql"))
	require.NoError(t, err)
	doc, diags, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".graphql", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// assertNoErrorDiags fails when any diagnostic has error severity.
func assertNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

// namedID is the stable TypeID of a named GraphQL type.
func namedID(name string) ir.TypeID { return ir.TypeID("t/graphql/types/" + name) }

// idScalarID is the stable TypeID of the built-in ID scalar.
const idScalarID ir.TypeID = "t/graphql/scalars/ID"

// allOperations flattens every operation across a document's service groups.
func allOperations(doc *ir.Document) []ir.Operation {
	var out []ir.Operation
	for _, svc := range doc.Services {
		for _, g := range svc.Groups {
			out = append(out, g.Operations...)
		}
	}
	return out
}

// opByName finds an operation by its source field name.
func opByName(doc *ir.Document, source string) (ir.Operation, bool) {
	for _, op := range allOperations(doc) {
		if op.Name.Source == source {
			return op, true
		}
	}
	return ir.Operation{}, false
}

// groupByName finds a service group by its source name.
func groupByName(doc *ir.Document, source string) (ir.OperationGroup, bool) {
	for _, svc := range doc.Services {
		for _, g := range svc.Groups {
			if g.Name.Source == source {
				return g, true
			}
		}
	}
	return ir.OperationGroup{}, false
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

func assertObjectTypes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	user, ok := doc.Types[namedID("User")].(*ir.Model)
	require.True(t, ok, "named object lowers to a Model under its pointer-derived ID")
	assert.False(t, user.Anonymous)
	assert.False(t, user.Abstract)
	id, ok := propByWire(user, "id")
	require.True(t, ok)
	assert.Equal(t, idScalarID, id.Type.Target, "ID maps to the named ID scalar")
	assert.True(t, id.Required)
	addr, ok := propByWire(user, "address")
	require.True(t, ok)
	assert.Equal(t, namedID("Address"), addr.Type.Target)
	assert.True(t, addr.Type.Nullable, "a field without ! is nullable")
	_, ok = doc.Types[namedID("Address")].(*ir.Model)
	assert.True(t, ok, "referenced Address resolves in the registry")
}

func assertInterfaces(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	node, ok := doc.Types[namedID("Node")].(*ir.Model)
	require.True(t, ok)
	assert.True(t, node.Abstract, "interface lowers to an Abstract model")
	ts, ok := doc.Types[namedID("Timestamped")].(*ir.Model)
	require.True(t, ok)
	assert.True(t, ts.Abstract)
	require.Len(t, ts.Implements, 1, "interface implementing an interface records it")
	assert.Equal(t, namedID("Node"), ts.Implements[0].Target)
	art, ok := doc.Types[namedID("Article")].(*ir.Model)
	require.True(t, ok)
	assert.False(t, art.Abstract)
	require.Len(t, art.Implements, 2, "implements A & B is N-ary conformance")
	assert.Equal(t, namedID("Node"), art.Implements[0].Target)
	assert.Equal(t, namedID("Timestamped"), art.Implements[1].Target)
}

func assertUnions(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	media, ok := doc.Types[namedID("Media")].(*ir.Union)
	require.True(t, ok, "union lowers to a Union node, never collapsed")
	assert.True(t, media.Exclusive)
	assert.True(t, media.WireTagged, "__typename tags the variant on the wire")
	require.Len(t, media.Variants, 2)
	require.NotNil(t, media.Discriminator)
	assert.Equal(t, "__typename", media.Discriminator.PropertyName)
	assert.Equal(t, namedID("Photo"), media.Discriminator.Mapping["Photo"])
	assert.Equal(t, namedID("Video"), media.Discriminator.Mapping["Video"])
}

func assertOneOfInput(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	lookup, ok := doc.Types[namedID("LookupInput")].(*ir.Union)
	require.True(t, ok, "@oneOf input lowers to a Union, never a model of optional fields")
	assert.True(t, lookup.Exclusive)
	assert.True(t, lookup.WireTagged)
	assert.Nil(t, lookup.Discriminator, "@oneOf inputs are key-tagged: no internal discriminator")
	require.Len(t, lookup.Variants, 2)
	assert.Equal(t, "byId", lookup.Variants[0].WireName)
	assert.True(t, lookup.Variants[0].Type.Nullable)
	assert.Contains(t, lookup.Extensions, "graphql:oneOfInput")
}

func assertEnums(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	status, ok := doc.Types[namedID("Status")].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, status.ValueType)
	assert.True(t, status.Closed, "GraphQL enums are closed")
	require.Len(t, status.Members, 3)
	assert.Equal(t, "DRAFT", status.Members[0].Value.Str)
	assert.NotNil(t, status.Members[2].Deprecation, "@deprecated on an enum value survives")
}

func assertCustomScalars(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	dt, ok := doc.Types[namedID("DateTime")].(*ir.Scalar)
	require.True(t, ok, "custom scalar lowers to a Scalar")
	assert.Nil(t, dt.Base, "custom scalars are opaque: nil Base")
	assert.Contains(t, dt.Extensions, "graphql:@specifiedBy")
	js, ok := doc.Types[namedID("JSON")].(*ir.Scalar)
	require.True(t, ok)
	assert.Nil(t, js.Base)
}

func assertInputObjects(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	np, ok := doc.Types[namedID("NewPost")].(*ir.Model)
	require.True(t, ok)
	assert.True(t, np.InputOnly, "input objects carry InputOnly")
	title, ok := propByWire(np, "title")
	require.True(t, ok)
	assert.True(t, title.Required)
	tag, ok := propByWire(np, "tag")
	require.True(t, ok)
	assert.False(t, tag.Required, "an input field with a default is not required")
	require.NotNil(t, tag.Default)
	assert.Equal(t, "general", tag.Default.Str)
	draft, ok := propByWire(np, "draft")
	require.True(t, ok)
	require.NotNil(t, draft.Default)
	assert.Equal(t, ir.ValueBool, draft.Default.Kind)
	assert.True(t, draft.Default.Bool)
	tags, ok := propByWire(np, "tags")
	require.True(t, ok)
	require.NotNil(t, tags.Default)
	assert.Equal(t, ir.ValueList, tags.Default.Kind, "a list default lands as a list Value")
	priority, ok := propByWire(np, "priority")
	require.True(t, ok)
	require.NotNil(t, priority.Default)
	assert.Equal(t, ir.ValueSymbol, priority.Default.Kind, "an enum default is a symbol, not a string")
	origin, ok := propByWire(np, "origin")
	require.True(t, ok)
	require.NotNil(t, origin.Default)
	assert.Equal(t, ir.ValueObject, origin.Default.Kind, "an object default lands as an object Value")
}

func assertFieldArguments(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	post, ok := doc.Types[namedID("Post")].(*ir.Model)
	require.True(t, ok)
	comments, ok := propByWire(post, "comments")
	require.True(t, ok)
	require.Len(t, comments.Args, 2, "field arguments lower to Property.Args")
	first := comments.Args[0]
	assert.Equal(t, "first", first.Name.Source)
	require.NotNil(t, first.Default)
	assert.Equal(t, ir.BigVal("20"), first.Default.Num, "numeric default is an exact BigVal")
	assert.False(t, first.Required)
	list, ok := doc.Types[comments.Type.Target].(*ir.List)
	require.True(t, ok, "[Comment!]! hoists a List node")
	assert.Equal(t, namedID("Comment"), list.Elem.Target)
	assert.False(t, list.Elem.Nullable, "Comment! is a non-null element")
}

func assertNullability(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	shape, ok := doc.Types[namedID("Shape")].(*ir.Model)
	require.True(t, ok)
	states := map[string]ir.Property{}
	for _, p := range shape.Properties {
		states[p.WireName] = p
	}
	assert.True(t, states["reqPlain"].Required)
	assert.False(t, states["reqPlain"].Type.Nullable)
	assert.False(t, states["optPlain"].Required)
	assert.True(t, states["optPlain"].Type.Nullable)
	// [Int!]! : outer non-null, inner non-null.
	assert.False(t, states["reqListReqItem"].Type.Nullable)
	rl, ok := doc.Types[states["reqListReqItem"].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.False(t, rl.Elem.Nullable)
	// [Int] : outer nullable, inner nullable.
	assert.True(t, states["optListOptItem"].Type.Nullable)
	ol, ok := doc.Types[states["optListOptItem"].Type.Target].(*ir.List)
	require.True(t, ok)
	assert.True(t, ol.Elem.Nullable)
}

func assertRecursive(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	node, ok := doc.Types[namedID("TreeNode")].(*ir.Model)
	require.True(t, ok)
	parent, ok := propByWire(node, "parent")
	require.True(t, ok)
	assert.Equal(t, namedID("TreeNode"), parent.Type.Target, "self-reference terminates on the interned ID")
	children, ok := propByWire(node, "children")
	require.True(t, ok)
	list, ok := doc.Types[children.Type.Target].(*ir.List)
	require.True(t, ok)
	assert.Equal(t, namedID("TreeNode"), list.Elem.Target)
}

func assertOperations(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	for _, name := range []string{"query", "mutation", "subscription"} {
		_, ok := groupByName(doc, name)
		assert.True(t, ok, "root type yields a %q group", name)
	}
	latest, ok := opByName(doc, "latest")
	require.True(t, ok)
	require.NotNil(t, latest.Bindings.GraphQL)
	assert.Equal(t, "query", latest.Bindings.GraphQL.Kind)
	assert.Equal(t, ir.IdempotencySafe, latest.Idempotency.Kind, "query fields are side-effect-free")
	send, ok := opByName(doc, "send")
	require.True(t, ok)
	assert.Equal(t, "mutation", send.Bindings.GraphQL.Kind)
	sub, ok := opByName(doc, "messageAdded")
	require.True(t, ok)
	assert.Equal(t, "subscription", sub.Bindings.GraphQL.Kind)
	assert.Equal(t, ir.StreamingServer, sub.Streaming, "subscriptions stream server-to-client")
	assert.NotNil(t, sub.ResponseStream)
}

func assertDeprecation(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	legacy, ok := doc.Types[namedID("Legacy")].(*ir.Model)
	require.True(t, ok)
	assert.NotNil(t, legacy.Deprecation, "@deprecated on a type survives")
	old, ok := propByWire(legacy, "old")
	require.True(t, ok)
	assert.NotNil(t, old.Deprecation)
	op, ok := opByName(doc, "legacy")
	require.True(t, ok)
	require.Len(t, op.Params, 1)
	assert.NotNil(t, op.Params[0].Deprecation, "@deprecated on an argument survives")
}

func assertDirectives(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	secret, ok := doc.Types[namedID("Secret")].(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, secret.Extensions, "graphql:@auth", "directive applications are namespaced")
	value, ok := propByWire(secret, "value")
	require.True(t, ok)
	raw, ok := value.Extensions["graphql:@auth"]
	require.True(t, ok)
	assert.JSONEq(t, `[{"role":"reader"},{"role":"writer"}]`, string(raw),
		"repeatable applications accumulate in an ordered array")
	cfg, ok := secret.Extensions["graphql:@config"]
	require.True(t, ok, "directive arguments of every value kind are preserved")
	assert.JSONEq(t, `[{"tags":["a","b"],"opts":{"retries":3},"level":"HIGH"}]`, string(cfg))
	assert.Contains(t, doc.Extensions, "graphql:directive-definitions")
}

func assertDocs(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[namedID("Documented")].(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, d.Docs.Description, "documented type")
	id, ok := propByWire(d, "id")
	require.True(t, ok)
	assert.Equal(t, "The identifier.", id.Docs.Description)
}

func assertFederationV1(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	raw, ok := doc.Extensions["federation:version"]
	require.True(t, ok)
	assert.JSONEq(t, `"1"`, string(raw), "v1 is detected from federation directives without @link")
	product, ok := doc.Types[namedID("Product")].(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, product.Extensions, "federation:@key", "entities carry @key")
	reviews, ok := propByWire(product, "reviews")
	require.True(t, ok)
	assert.Contains(t, reviews.Extensions, "federation:@requires")
	user, ok := doc.Types[namedID("User")].(*ir.Model)
	require.True(t, ok, "an extend-only type still lands in the registry")
	assert.Contains(t, user.Extensions, "graphql:extends", "the extend occurrence is recorded")
	assert.Contains(t, user.Extensions, "federation:@key")
}

func assertFederationV2(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	raw, ok := doc.Extensions["federation:version"]
	require.True(t, ok)
	assert.JSONEq(t, `"2"`, string(raw), "v2 is detected from the federation @link")
	assert.Contains(t, doc.Extensions, "federation:@link")
	product, ok := doc.Types[namedID("Product")].(*ir.Model)
	require.True(t, ok)
	sku, ok := propByWire(product, "sku")
	require.True(t, ok)
	assert.Contains(t, sku.Extensions, "federation:@shareable")
	internal, ok := propByWire(product, "internalName")
	require.True(t, ok)
	assert.True(t, internal.Visibility.None, "@inaccessible hides the field from every projection")
	assert.Contains(t, internal.Extensions, "federation:@inaccessible")
	price, ok := propByWire(product, "price")
	require.True(t, ok)
	assert.Contains(t, price.Extensions, "federation:@override")
	metrics, ok := doc.Types[namedID("Metrics")].(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, metrics.Extensions, "federation:@interfaceObject")
	_, ok = opByName(doc, "bestSeller")
	assert.True(t, ok, "extend type Query contributes its field as an operation")
}
