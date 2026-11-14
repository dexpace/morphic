// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// spineKeyLen is the size of each key a spine is nested under.
const spineKeyLen = 10_000

// spineWidth is how many short entries the deepest level of a spine holds.
const spineWidth = 1_500

// spine writes a mapping of depth levels, each keyed by a name of spineKeyLen bytes, over
// spineWidth short entries, indented by indent spaces. A level is a bare mapping
// keyed by the long name, and when schema is set a schema whose properties are
// that mapping, so every node of it is named when read as one.
func spine(indent, depth int, schema bool) string {
	var b strings.Builder
	pad := func() string { return strings.Repeat(" ", indent) }
	for i := range depth {
		if schema {
			b.WriteString(pad() + "type: object\n" + pad() + "properties:\n")
			indent += 2
		}
		fmt.Fprintf(&b, "%s? %s%d\n%s:\n", pad(), strings.Repeat("p", spineKeyLen), i%10, pad())
		indent += 2
	}
	if schema {
		b.WriteString(pad() + "type: object\n" + pad() + "properties:\n")
		indent += 2
	}
	for j := range spineWidth {
		if schema {
			fmt.Fprintf(&b, "%sk%d: {type: string}\n", pad(), j)
		} else {
			fmt.Fprintf(&b, "%sk%d: v\n", pad(), j)
		}
	}
	return b.String()
}

// pointerBoundDoc writes #1499's spine of 60 levels of 10,000-byte keys over
// 1,500 entries, placed where says.
func pointerBoundDoc(where string) string {
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
	switch where {
	case "ext":
		return head + "x-data:\n" + spine(2, 60, false)
	case "example":
		return head + "components:\n  schemas:\n    A:\n      type: object\n      example:\n" + spine(8, 60, false)
	default:
		return head + "components:\n  schemas:\n    A:\n" + spine(6, 60, true)
	}
}

// TestCompile_ARawValueNestingLongKeysIsNotRefusedAsPointerAmplification holds a
// valid document that keeps a deep value of long keys verbatim, in an extension
// or an example, to compiling, and the same shape placed as a schema, which a
// compile names node by node, to the refusal the bound exists for.
func TestCompile_ARawValueNestingLongKeysIsNotRefusedAsPointerAmplification(t *testing.T) {
	t.Parallel()
	for where, refused := range map[string]bool{"ext": false, "example": false, "schema": true} {
		t.Run(where, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, "spec.yaml", pointerBoundDoc(where), openapi.Options{})

			if refused {
				assert.Nil(t, doc)
				require.Len(t, diags, 1, "%+v", diags)
				assert.Equal(t, diag.PointerAmplification, diags[0].Code)
				return
			}
			require.NotNil(t, doc, "%+v", diags)
			assert.Empty(t, diags)
		})
	}
}

// pointerBoundRaw is a 12-level spine of 10,000-byte keys under x-data, a raw
// extension, over 1,500 schemas, followed by components using it as the rest of
// the document writes.
func pointerBoundRaw(rest string) string {
	return "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\nx-data:\n" + spine(2, 12, true) + rest
}

// TestCompile_ARawValueAReferenceLandsInIsNamedNodeByNode holds the exemption of
// a raw value to the documents that cannot route a reference into it: one a
// $ref or a discriminator mapping lands in is named node by node and refused, one
// holding a reference that leaves the document is refused as it cannot be shown
// closed, and one writing the reference inside a value kept raw, where it is
// data, is not.
func TestCompile_ARawValueAReferenceLandsInIsNamedNodeByNode(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		rest    string
		refused bool
	}{
		"a $ref lands in it": {"components:\n  schemas:\n    A: {$ref: \"#/x-data\"}\n", true},
		"a discriminator mapping lands in it": {
			"components:\n  schemas:\n    A: {type: object, discriminator: {propertyName: t, mapping: {a: \"#/x-data\"}}}\n", true,
		},
		"a $ref leaves the document": {"components:\n  schemas:\n    B: {$ref: \"other.yaml#/x\"}\n", true},
		"the $ref is inside an example": {
			"components:\n  schemas:\n    A: {type: object, example: {$ref: \"#/x-data\"}}\n", false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, "spec.yaml", pointerBoundRaw(tc.rest), openapi.Options{})

			if tc.refused {
				assert.Nil(t, doc)
				assert.Len(t, byCode(diags, diag.PointerAmplification), 1, "%+v", diags)
				return
			}
			require.NotNil(t, doc, "%+v", diags)
			assert.Empty(t, byCode(diags, diag.PointerAmplification))
		})
	}
}

// TestCompile_APropertyNamedExampleHoldsASchemaAndIsNamedNodeByNode holds the
// exemption to the grammar and not to a key's spelling: the deep spine written as
// the schema of a property called example is a schema, so it is refused.
func TestCompile_APropertyNamedExampleHoldsASchemaAndIsNamedNodeByNode(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    A:\n      type: object\n      properties:\n        example:\n" +
		spine(10, 60, true)

	doc, diags := compileText(t, "spec.yaml", spec, openapi.Options{})

	assert.Nil(t, doc)
	assert.Len(t, byCode(diags, diag.PointerAmplification), 1, "%+v", diags)
}
