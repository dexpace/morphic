// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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
	doc, diags := LowerSpec(t, openapitest.ComponentSpec(b.String()))
	require.NotNil(t, doc)
	var sawCap bool
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && strings.Contains(d.Message, "nesting exceeds") {
			sawCap = true
		}
	}
	assert.True(t, sawCap, "schema depth-cap diagnostic emitted")
}

// TestLower_ASchemaPastTheMendsIsTheTopType pins what the load phase leaves
// read as false past typed.MaxMends schemas one below another, each an items
// above a draft's list of items: the innermost left is the top type, reported
// as a degraded-construct error at it, and every one above it is the array it
// writes. One schema fewer lowers whole.
func TestLower_ASchemaPastTheMendsIsTheTopType(t *testing.T) {
	t.Parallel()
	chain := func(depth int) string {
		return "jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#'\n" + openapitest.ComponentSpec("    C: "+
			strings.Repeat("{type: array, items: ", depth)+"[{}]"+strings.Repeat("}", depth)+"\n")
	}
	past := "/components/schemas/C" + strings.Repeat("/items", typed.MaxMends+1)
	doc, diags := LowerSpec(t, chain(typed.MaxMends+2))
	require.NotNil(t, doc)
	assert.Equal(t, []jsontext.Pointer{jsontext.Pointer(past)}, openapitest.PointersOf(diags, diag.DegradedConstruct),
		"the innermost schema left false")
	assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "and no false the document writes")
	_, diags = LowerSpec(t, chain(typed.MaxMends+1))
	assert.Empty(t, openapitest.PointersOf(diags, diag.DegradedConstruct), "one fewer is mended whole")
}

// TestRecordSkippedFamilies_MissingOwner drives// TestRecordSkippedFamilies_MissingOwner drives the same invariant on the
// keyword families the dispatch passed over. It is reached directly for the
// reason TestRecordUnhomedKeywords_MissingOwner is: the caller hands it either
// the node it just looked up or one internAlias just interned, so no source
// reaches this.
func TestRecordSkippedFamilies_MissingOwner(t *testing.T) {
	t.Parallel()
	l := NewRawLowerer(&soa.OpenAPI{})
	diags := recordSkippedFamilies(l.Ctx, l.Types, "t/anon/missing", &oas3.Schema{},
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
// is dropped in silence (GitHub #35). Declaring nothing keeps such a name out
// of the election entirely.
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
