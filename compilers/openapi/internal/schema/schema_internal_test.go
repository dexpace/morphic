package schema

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestRecordSkippedFamilies_MissingOwner drives the same invariant on the
// keyword families the dispatch passed over. It is reached directly for the
// reason TestRecordUnhomedKeywords_MissingOwner is: the caller hands it either
// the node it just looked up or one internAlias just interned, so no source
// reaches this.
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
