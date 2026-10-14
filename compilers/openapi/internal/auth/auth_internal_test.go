package auth

import (
	"reflect"
	"slices"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
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

// TestFlowFields_AccountForEverySourceField holds flowFields to the OAuth Flow
// model it lists, so a field the upstream model gains fails here rather than
// being dropped from the IR without trace (GitHub #865). Each field must be
// read for every flow or be listed by wire name, and every field a flow kind
// defines must be one of those listed. The flow keywords come from presentFlows
// over a fully populated object, so a flow the model gains is named here too.
func TestFlowFields_AccountForEverySourceField(t *testing.T) {
	t.Parallel()
	// alwaysRead are the fields oauthFlow reads whatever the flow is.
	alwaysRead := map[string]bool{"RefreshURL": true, "Scopes": true, "Extensions": true}
	// perKind are the fields only some flows define, keyed by Go field name and
	// valued by the wire name flowFields must list.
	perKind := map[string]string{
		"AuthorizationURL":       "authorizationUrl",
		"DeviceAuthorizationURL": "deviceAuthorizationUrl",
		"TokenURL":               "tokenUrl",
	}

	var want []string
	for f := range reflect.TypeFor[soa.OAuthFlow]().Fields() {
		if f.Anonymous || !f.IsExported() {
			continue // the embedded marshaller model is not a document field
		}
		wire, isPerKind := perKind[f.Name]
		require.True(t, alwaysRead[f.Name] || isPerKind,
			"OAuth flow field %q is neither read for every flow nor listed as a per-kind field, "+
				"so a document writing it loses it: add it to one or the other", f.Name)
		if isPerKind {
			want = append(want, wire)
		}
	}
	slices.Sort(want)
	fields := flowFields()
	listed := make([]string, 0, len(fields))
	for _, field := range fields {
		listed = append(listed, field.name)
	}
	assert.Equal(t, want, listed, "flowFields must list every per-kind field, sorted")

	var all soa.OAuthFlows
	for f := range reflect.TypeFor[soa.OAuthFlows]().Fields() {
		if f.Anonymous || !f.IsExported() || f.Name == "Extensions" {
			continue
		}
		reflect.ValueOf(&all).Elem().FieldByIndex(f.Index).Set(reflect.ValueOf(&soa.OAuthFlow{}))
	}
	declared := presentFlows(&all)
	keywords := make([]string, 0, len(declared))
	for _, f := range declared {
		keywords = append(keywords, f.keyword)
	}
	require.ElementsMatch(t,
		[]string{"implicit", "authorizationCode", "password", "clientCredentials", "deviceAuthorization"}, keywords,
		"a flow the model gains needs a row in flowFieldsDefinedBy and here")
	for _, keyword := range keywords {
		defined := flowFieldsDefinedBy(keyword)
		assert.NotEmpty(t, defined, "every flow defines some endpoint: %s", keyword)
		var fl ir.OAuthFlow
		slots := map[*string]string{}
		for _, field := range fields {
			if !slices.Contains(defined, field.name) {
				continue
			}
			slot := field.slot(&fl)
			assert.Empty(t, slots[slot], "%s defines %s and %s, which lower to one field", keyword, slots[slot], field.name)
			slots[slot] = field.name
		}
		for _, name := range defined {
			assert.Contains(t, listed, name, "%s defines a field flowFields does not list", keyword)
		}
	}
}

// TestFlowFields_ReadTheFieldTheirNameSays holds each flowField's reader to the
// source field its wire name spells: it returns that field's value, and no
// other field's. A pair crossed between two names would lower one endpoint
// under another's flow kind with every other test still agreeing.
func TestFlowFields_ReadTheFieldTheirNameSays(t *testing.T) {
	t.Parallel()
	goName := map[string]string{
		"authorizationUrl":       "AuthorizationURL",
		"deviceAuthorizationUrl": "DeviceAuthorizationURL",
		"tokenUrl":               "TokenURL",
	}
	require.Len(t, flowFields(), len(goName))
	for _, field := range flowFields() {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()
			name, ok := goName[field.name]
			require.True(t, ok, "%s names no known source field", field.name)
			value := "https://a.example/" + field.name
			var src soa.OAuthFlow
			reflect.ValueOf(&src).Elem().FieldByName(name).Set(reflect.ValueOf(&value))

			assert.Equal(t, value, field.read(&src))
			for _, other := range flowFields() {
				if other.name != field.name {
					assert.Empty(t, other.read(&src), "%s must not read %s", other.name, field.name)
				}
			}
		})
	}
}
