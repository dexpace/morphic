// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// namedSteps is the bound the totals below are taken within, far above any
// document here.
const namedSteps = 1 << 20

// namedHead opens each document.
const namedHead = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"

// namedSpine is a schema three levels deep under long names, so the pointers of
// its nodes are most of what a total counts.
const namedSpine = "{type: object, properties: {kkkkkkkkkkkk: {type: object, properties: {llllllllllll: {type: string}}}}}"

// namedTotal reads text and returns the total the grammar names, and whether it
// is complete.
func namedTotal(t *testing.T, text string) (int, bool) {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &doc))
	return bind.NamedPointerBytes(t.Context(), nodeview.DocumentRoot(&doc), namedSteps)
}

// TestNamedPointerBytes_AValueKeptRawAddsItsRootAlone holds an anchored spine
// mounted as a schema and as an example: read as a schema every node of it is
// named, read as an example only the root of the mount is, so mounting it as an
// example adds one pointer and mounting it as a schema adds the spine's.
func TestNamedPointerBytes_AValueKeptRawAddsItsRootAlone(t *testing.T) {
	t.Parallel()
	const base = namedHead + "components:\n  schemas:\n    S: &s " + namedSpine + "\n"
	without, complete := namedTotal(t, base+"    E: {type: string}\n")
	require.True(t, complete)

	asExample, complete := namedTotal(t, base+"    E: {type: string, example: *s}\n")
	require.True(t, complete)
	asSchema, complete := namedTotal(t, base+"    E: {type: object, properties: {x: *s}}\n")
	require.True(t, complete)
	asSchemaBase, _ := namedTotal(t, base+"    E: {type: object, properties: {}}\n")

	assert.Equal(t, 2*len("/components/schemas/E/example"), asExample-without,
		"only the root of the mount is named, with its key, each at the pointer it is mounted at")
	assert.Greater(t, asSchema-asSchemaBase, 5*(asExample-without), "mounted as a schema, every node of it is named")
}

// TestNamedPointerBytes_TheTotalDoesNotDependOnWhichMountIsWrittenFirst holds a
// node mounted as a schema and as an example to one total in both declaration
// orders: a memo keyed by the node alone would count whichever came first.
func TestNamedPointerBytes_TheTotalDoesNotDependOnWhichMountIsWrittenFirst(t *testing.T) {
	t.Parallel()
	schemaFirst, c1 := namedTotal(t, namedHead+"components:\n  schemas:\n    S: &s "+namedSpine+"\n    E: {type: string, example: *s}\n")
	exampleFirst, c2 := namedTotal(t, namedHead+"components:\n  schemas:\n    E: {type: string, example: &s "+namedSpine+"}\n    S: *s\n")

	assert.True(t, c1)
	assert.True(t, c2)
	assert.Equal(t, schemaFirst, exampleFirst)
}

// TestNamedPointerBytes_AReferenceIntoARawValueNamesItNodeByNode holds a raw
// value a $ref or a discriminator mapping lands in to being counted as the kind
// the reference expects, and a $ref written inside an example to being data.
func TestNamedPointerBytes_AReferenceIntoARawValueNamesItNodeByNode(t *testing.T) {
	t.Parallel()
	const raw = namedHead + "x-data: " + namedSpine + "\n"
	alone, complete := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object}\n")
	require.True(t, complete)

	byRef, c1 := namedTotal(t, raw+"components:\n  schemas:\n    A: {$ref: \"#/x-data\"}\n")
	byMapping, c2 := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object, discriminator: {propertyName: t, mapping: {a: \"#/x-data\"}}}\n")
	inExample, c3 := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object, example: {$ref: \"#/x-data\"}}\n")
	elsewhere, c4 := namedTotal(t, raw+"components:\n  schemas:\n    A: {type: object, example: {$ref: \"#/x-dxta\"}}\n")

	assert.True(t, c1 && c2 && c3 && c4)
	assert.Greater(t, byRef, alone+100, "the spine is named once a $ref lands in it")
	assert.Greater(t, byMapping, alone+100, "and once a discriminator mapping does")
	assert.Equal(t, elsewhere, inExample, "a $ref inside an example is data")
}

// TestNamedPointerBytes_IsIncompleteWhereTheDocumentIsNotClosed holds the
// answer's completeness: a reference that leaves the document, a bound spent
// before the document is read, and a closed document tell it apart.
func TestNamedPointerBytes_IsIncompleteWhereTheDocumentIsNotClosed(t *testing.T) {
	t.Parallel()
	closed := namedHead + "components:\n  schemas:\n    A: {type: string}\n"
	leaving := namedHead + "components:\n  schemas:\n    A: {$ref: \"other.yaml#/x\"}\n"
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(closed), &doc))

	_, whole := namedTotal(t, closed)
	_, left := namedTotal(t, leaving)
	_, nowhere := namedTotal(t, namedHead+"components:\n  schemas:\n    A: {$ref: \"#/x-nowhere\"}\n")
	_, underID := namedTotal(t, namedHead+"x-data: {}\ncomponents:\n  schemas:\n    A: {$id: \"https://e.example/a\", properties: {p: {$ref: \"#/x-data\"}}}\n")
	_, spent := bind.NamedPointerBytes(t.Context(), nodeview.DocumentRoot(&doc), 1)

	assert.True(t, whole)
	assert.False(t, left, "another document could route back into a raw value")
	assert.False(t, nowhere, "the binder may answer a pointer naming nothing by a rule of its own")
	assert.False(t, underID, "an $id moves where a $ref under it lands")
	assert.False(t, spent, "past the step bound")
}

// TestNamedPointerBytes_AFragmentLandsWhereItsDecodingNames holds a fragment
// written with a percent escape to the node its decoding names, as the binder
// reads it: the same total as the fragment written plain.
func TestNamedPointerBytes_AFragmentLandsWhereItsDecodingNames(t *testing.T) {
	t.Parallel()
	const raw = namedHead + "x-data: " + namedSpine + "\n"
	plain, c1 := namedTotal(t, raw+"components:\n  schemas:\n    A: {$ref: \"#/x-data\"}\n")
	escaped, c2 := namedTotal(t, raw+"components:\n  schemas:\n    A: {$ref: \"#/x%2Ddata\"}\n")

	assert.True(t, c1)
	assert.True(t, c2)
	assert.Equal(t, plain, escaped)
}

// TestNamedPointerBytes_ALandingNamedInPlaceIsCountedOnce holds a $ref whose
// target the walk already names where it is written to adding nothing past the
// $ref itself: the same total as a key of the same length the grammar keeps
// raw.
func TestNamedPointerBytes_ALandingNamedInPlaceIsCountedOnce(t *testing.T) {
	t.Parallel()
	const base = namedHead + "components:\n  schemas:\n    A: " + namedSpine + "\n"
	byRef, c1 := namedTotal(t, base+"    B: {$ref: \"#/components/schemas/A\"}\n")
	byRaw, c2 := namedTotal(t, base+"    B: {$reg: \"#/components/schemas/A\"}\n")

	assert.True(t, c1)
	assert.True(t, c2)
	assert.Equal(t, byRaw, byRef)
}

// TestNamedPointerBytes_NamesTheEntriesOfAListAndTheReferenceOfAnObject holds
// the nodes of a list, an `allOf` branch and a parameter list item among
// them, to the pointers they are named by: the total grows by what a list adds, a
// parameter that is a reference object among them.
func TestNamedPointerBytes_NamesTheEntriesOfAListAndTheReferenceOfAnObject(t *testing.T) {
	t.Parallel()
	const head = namedHead + "components:\n  parameters:\n    p: {name: p, in: query, schema: {type: string}}\n  schemas:\n"
	bare, complete := namedTotal(t, head+"    S: {type: object}\n")
	require.True(t, complete)

	withList, complete := namedTotal(t, head+"    S: {type: object, allOf: [{type: string}, {type: integer}]}\n")
	require.True(t, complete)
	const operation = namedHead + "paths:\n  /a:\n    get:\n      parameters: %s\n" +
		"components:\n  parameters:\n    p: {name: p, in: query, schema: {type: string}}\n"
	noParameters, complete := namedTotal(t, fmt.Sprintf(operation, "[]"))
	require.True(t, complete)
	withReference, complete := namedTotal(t, fmt.Sprintf(operation, `[{$ref: "#/components/parameters/p"}]`))
	require.True(t, complete)

	// Each pointer counts once for the key and once for the value that holds
	// it, under /components/schemas/S (21 bytes): the allOf key and its list,
	// 27 each, then each of the two entries, 29, with its type key and value, 34
	// each.
	assert.Equal(t, 27+27+2*(29+34+34), withList-bare, "the entries of a list are named, each at its index")
	assert.Greater(t, withReference, noParameters, "a parameter a list holds is named")
}

// TestNamedPointerBytes_NamesNothingForANodeThatIsAbsent holds a tree no parse
// produces, a nil root, a nil member and an alias naming no node, to a total
// that is complete and counts nothing of them: where an empty schema names a
// node, an absent one names none.
func TestNamedPointerBytes_NamesNothingForANodeThatIsAbsent(t *testing.T) {
	t.Parallel()
	key := func(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
	mapping := func(content ...*yaml.Node) *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Content: content} }
	schemas := func(entry *yaml.Node) *yaml.Node {
		return mapping(key("components"), mapping(key("schemas"), mapping(key("S"), entry)))
	}
	total := func(root *yaml.Node) (int, bool) { return bind.NamedPointerBytes(t.Context(), root, namedSteps) }
	empty, _ := total(schemas(mapping()))

	nilRoot, completeNil := total(nil)
	nilMember, completeMember := total(schemas(nil))
	danglingAlias, completeAlias := total(schemas(&yaml.Node{Kind: yaml.AliasNode}))

	assert.Zero(t, nilRoot)
	assert.True(t, completeNil)
	assert.True(t, completeMember)
	assert.True(t, completeAlias)
	assert.Less(t, nilMember, empty, "an absent schema names no node, an empty one names one")
	assert.Equal(t, nilMember, danglingAlias, "an alias naming no node names none either")
}

// TestSat_HoldsASumAndAProductAtTheLargestInt holds the counts the total adds
// and multiplies to the largest int, where they would wrap.
func TestSat_HoldsASumAndAProductAtTheLargestInt(t *testing.T) {
	t.Parallel()
	const most = int(^uint(0) >> 1)
	assert.Equal(t, 5, bind.Sat(2, 3))
	assert.Equal(t, most, bind.Sat(most, 1))
	assert.Equal(t, most, bind.Sat(1, most))
	assert.Equal(t, 6, bind.SatMul(2, 3))
	assert.Zero(t, bind.SatMul(0, most))
	assert.Equal(t, most, bind.SatMul(most, 2))
	assert.Equal(t, most, bind.SatMul(2, most))
}
