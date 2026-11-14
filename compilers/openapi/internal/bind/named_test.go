// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
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
	_, spent := bind.NamedPointerBytes(t.Context(), nodeview.DocumentRoot(&doc), 1)

	assert.True(t, whole)
	assert.False(t, left, "another document could route back into a raw value")
	assert.False(t, spent, "past the step bound")
}
