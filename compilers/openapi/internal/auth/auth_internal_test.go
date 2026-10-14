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

// TestLowerSecurityRequirement_Nil covers the half of the object guard a parsed
// document cannot reach: a hand-built slice holding a nil entry. It is refused
// like any other entry that is no object, rather than surviving as the empty
// option that reads as "no auth is one acceptable choice" — and silently, since
// there is no document position to place a report at.
func TestLowerSecurityRequirement_Nil(t *testing.T) {
	t.Parallel()
	got, ok, diags := lowerSecurityRequirement(lowering.Ctx{}, nil, "/security/0")

	assert.Empty(t, got.Schemes)
	assert.False(t, ok, "a nil entry is no requirement, so the caller drops it")
	assert.Empty(t, diags)
}

// TestMechanismFieldNames_AccountForEverySourceField holds mechanismFieldNames
// to the source model it lists, so a field the upstream model gains fails here
// rather than being dropped from the IR without trace, which is what #294 was.
//
// Reading the source struct is what makes the comparison possible: a
// hand-written list checked against another would agree with itself forever.
// The wire names are spelled out rather than derived, since they differ by more
// than leading case: OAuth2MetadataUrl is oauth2MetadataUrl.
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
	st := reflect.TypeFor[soa.SecurityScheme]()
	for f := range st.Fields() {
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

// TestPresentFlows_ReadsEverySourceFlowField holds presentFlows to the flow
// fields soa.OAuthFlows declares. The no-flow report reads an empty lowered list
// as "the document declares no flow", which is true only while every flow field
// is read: a field the upstream model gains would otherwise lower to nothing,
// losing the flow and drawing a false report. Each flow field is set alone
// through reflection, so the check reads the struct rather than a second
// hand-written list.
func TestPresentFlows_ReadsEverySourceFlowField(t *testing.T) {
	t.Parallel()
	flowType := reflect.TypeFor[*soa.OAuthFlow]()
	notFlows := map[string]bool{"Extensions": true}
	st := reflect.TypeFor[soa.OAuthFlows]()
	var seen int
	var all soa.OAuthFlows
	for f := range st.Fields() {
		if f.Anonymous || !f.IsExported() || notFlows[f.Name] {
			continue // the embedded marshaller model is not a document field
		}
		require.Equal(t, flowType, f.Type,
			"OAuthFlows field %q is neither a flow nor listed as one that is not", f.Name)
		var flows soa.OAuthFlows
		reflect.ValueOf(&flows).Elem().FieldByIndex(f.Index).Set(reflect.ValueOf(&soa.OAuthFlow{}))
		assert.Len(t, presentFlows(&flows), 1, "presentFlows does not read the %s flow", f.Name)
		reflect.ValueOf(&all).Elem().FieldByIndex(f.Index).Set(reflect.ValueOf(&soa.OAuthFlow{}))
		seen++
	}
	require.NotZero(t, seen, "the walk found no flow field at all")
	assert.Len(t, presentFlows(&all), seen, "each flow field is read exactly once")
}
