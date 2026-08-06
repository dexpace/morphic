package auth

import (
	"reflect"
	"slices"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

func TestLowerSecurityRequirement_Nil(t *testing.T) {
	t.Parallel()
	got, ok, diags := lowerSecurityRequirement(lowering.Ctx{}, nil, "/security/0")

	assert.Empty(t, got.Schemes)
	assert.True(t, ok, "a nil requirement entry is not a resolution failure")
	assert.Empty(t, diags)
}

// TestMechanismFieldNames_AccountForEverySourceField holds mechanismFieldNames
// to the source model it is a list of, so a field the upstream model gains is a
// failure here rather than a construct the IR drops without trace.
//
// That is the shape of #294 itself: the lowering read the fields it knew about
// and dropped whatever else the entry declared, and no test compared the two
// lists. Reading the source struct is what makes the comparison possible at all
// — a hand-written list checked against another hand-written list would agree
// with itself forever.
//
// The wire names are spelled out rather than derived, because the two differ by
// more than a leading case change: OAuth2MetadataUrl is oauth2MetadataUrl.
func TestMechanismFieldNames_AccountForEverySourceField(t *testing.T) {
	t.Parallel()
	// alwaysRead are the fields lowerSecurityScheme reads whatever the type is,
	// so no type can leave one unread and none needs preserving.
	alwaysRead := map[string]bool{
		"Type": true, "Description": true, "Deprecated": true, "Extensions": true,
	}
	// perType are the fields only some types define, keyed by Go field name and
	// valued by the wire name mechanismFieldNames must list.
	perType := map[string]string{
		"Name": "name", "In": "in", "Scheme": "scheme", "BearerFormat": "bearerFormat",
		"Flows": "flows", "OpenIdConnectUrl": "openIdConnectUrl",
		"OAuth2MetadataUrl": "oauth2MetadataUrl",
	}

	var want []string
	st := reflect.TypeOf(soa.SecurityScheme{})
	for i := range st.NumField() {
		f := st.Field(i)
		if f.Anonymous || !f.IsExported() {
			continue // the embedded marshaller model is not a document field
		}
		wire, perTypeField := perType[f.Name]
		require.True(t, alwaysRead[f.Name] || perTypeField,
			"securityScheme field %q is neither read for every type nor listed as a mechanism "+
				"field, so a document declaring it loses it: add it to one or the other", f.Name)
		if perTypeField {
			want = append(want, wire)
		}
	}

	slices.Sort(want)
	assert.Equal(t, want, mechanismFieldNames(),
		"mechanismFieldNames must list every per-type field, sorted")
}
