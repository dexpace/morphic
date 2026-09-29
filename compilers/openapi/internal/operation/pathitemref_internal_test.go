package operation

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestSiblingPairs_APointerThatNamesNothingHasNoSiblings pins the read's
// precondition: a use pointer the document's own text does not reach yields no
// pairs, so the mount is left exactly as it was rather than read from whatever
// node the walk happened to stop at.
func TestSiblingPairs_APointerThatNamesNothingHasNoSiblings(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, openapitest.PathsSpec(`  /p:
    get: {operationId: getP, responses: {"200": {description: ok}}}
`))

	assert.Nil(t, siblingPairs(l.ctx, "/paths/absent"),
		"a token the mapping does not hold ends the walk short of a node")
	assert.Nil(t, siblingPairs(l.ctx, "/paths/~1p"),
		"a path item with no $ref has nothing beside one")
}

// referenceKindFields returns the Matcher fields whose parameter is a reference
// wrapper or a schema, which are the kinds a walk yields as something to
// resolve. Every other field names an object the walk visits by value.
func referenceKindFields() []string {
	matcher := reflect.TypeOf(soa.Matcher{})
	var names []string
	for i := range matcher.NumField() {
		field := matcher.Field(i)
		if strings.HasPrefix(field.Name, "Referenced") || field.Name == "Schema" {
			names = append(names, field.Name)
		}
	}
	slices.Sort(names)
	return names
}

// TestSiblingRefMatcher_CoversTheLibraryVocabulary holds the reference kinds the
// seam resolves to the library's own Matcher fields.
//
// The set cannot be written down from this side and trusted: the library adds a
// Referenced* alias when the specification grows one, and a kind this matcher
// does not name is a reference the seam silently leaves unresolved. Deriving the
// expectation from the Matcher type makes that addition redden here rather than
// going unnoticed.
func TestSiblingRefMatcher_CoversTheLibraryVocabulary(t *testing.T) {
	t.Parallel()
	var called []string
	matcher := siblingRefMatcher(func(kind string, r resolvableRef) {
		require.Nil(t, r, "a kind is named without a reference to resolve")
		called = append(called, kind)
	})

	kinds := []struct {
		field string
		call  func(*soa.Matcher) error
	}{
		{"ReferencedPathItem", func(m *soa.Matcher) error { return m.ReferencedPathItem(nil) }},
		{"ReferencedParameter", func(m *soa.Matcher) error { return m.ReferencedParameter(nil) }},
		{"ReferencedHeader", func(m *soa.Matcher) error { return m.ReferencedHeader(nil) }},
		{"ReferencedExample", func(m *soa.Matcher) error { return m.ReferencedExample(nil) }},
		{"ReferencedRequestBody", func(m *soa.Matcher) error { return m.ReferencedRequestBody(nil) }},
		{"ReferencedResponse", func(m *soa.Matcher) error { return m.ReferencedResponse(nil) }},
		{"ReferencedLink", func(m *soa.Matcher) error { return m.ReferencedLink(nil) }},
		{"ReferencedCallback", func(m *soa.Matcher) error { return m.ReferencedCallback(nil) }},
		{"ReferencedSecurityScheme", func(m *soa.Matcher) error { return m.ReferencedSecurityScheme(nil) }},
		{"Schema", func(m *soa.Matcher) error { return m.Schema(nil) }},
	}

	covered := make([]string, 0, len(kinds))
	for _, k := range kinds {
		require.NoError(t, k.call(&matcher), k.field)
		covered = append(covered, k.field)
	}
	assert.Len(t, called, len(kinds), "one kind is named per field the matcher covers")
	slices.Sort(covered)
	assert.Equal(t, referenceKindFields(), covered,
		"the matcher must cover every reference kind the library's Matcher yields")
}
