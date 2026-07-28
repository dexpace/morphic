package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestAuthScheme_JSONContract pins AuthScheme's omitempty contract — Name,
// Docs, and Provenance carry no omitempty (every scheme has a naming, a
// possibly-empty docs object, and provenance), while every scheme-kind-
// specific field (In, KeyName, Flows, …) stays optional since only the fields
// relevant to Kind are ever populated — and that a fully populated AuthScheme
// — covering the OAuth2 shape with multiple flows — round-trips.
func TestAuthScheme_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.AuthScheme{}, `{"name":{},"docs":{},"provenance":{"source":0}}`,
		ir.AuthScheme{
			ID:           "auth/openapi/oauth2",
			Name:         populatedNaming(),
			Kind:         ir.AuthKindOAuth2,
			Docs:         populatedDocs(),
			Deprecation:  populatedDeprecation(),
			In:           "header",
			KeyName:      "Authorization",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Flows: []ir.OAuthFlow{
				{
					Kind:             "authorization_code",
					AuthorizationURL: "https://example.com/authorize",
					TokenURL:         "https://example.com/token",
					RefreshURL:       "https://example.com/refresh",
					Scopes:           map[string]string{"read": "read access", "write": "write access"},
					Preserved:        populatedPreserved(),
				},
				{Kind: "client_credentials", TokenURL: "https://example.com/token"},
			},
			OAuth2MetadataURL: "https://example.com/.well-known/oauth-authorization-server",
			OpenIDConnectURL:  "https://example.com/.well-known/openid-configuration",
			Preserved:         populatedPreserved(),
			Provenance:        populatedProvenance(),
		})
}

// TestAuthKind_Constants pins the on-disk spelling of every AuthKind value.
// AuthKind strings are the wire format compilers write into Document.Auth; a
// typo fix later would be a silent breaking change to every golden IR
// snapshot, exactly like the PrimKind/PresenceKind constants in types.go.
func TestAuthKind_Constants(t *testing.T) {
	t.Parallel()
	assertConstantSpellings(t, map[ir.AuthKind]string{
		ir.AuthKindAPIKey:               "apiKey",
		ir.AuthKindHTTPBasic:            "http_basic",
		ir.AuthKindHTTPBearer:           "http_bearer",
		ir.AuthKindOAuth2:               "oauth2",
		ir.AuthKindOpenIDConnect:        "openid_connect",
		ir.AuthKindMutualTLS:            "mutual_tls",
		ir.AuthKindUserPassword:         "user_password",
		ir.AuthKindX509:                 "x509",
		ir.AuthKindSymmetricEncryption:  "symmetric_encryption",
		ir.AuthKindAsymmetricEncryption: "asymmetric_encryption",
		ir.AuthKindSASLPlain:            "sasl_plain",
		ir.AuthKindSASLSCRAMSHA256:      "sasl_scram_sha256",
		ir.AuthKindSASLSCRAMSHA512:      "sasl_scram_sha512",
		ir.AuthKindSASLGSSAPI:           "sasl_gssapi",
		ir.AuthKindCustom:               "custom",
	}, "unspecified")
}

// TestOAuthFlow_ZeroValueShape pins OAuthFlow's omitempty contract: every
// field is optional, since only the flow kinds present in Scheme.Flows carry
// meaning for a given AuthScheme.
func TestOAuthFlow_ZeroValueShape(t *testing.T) {
	t.Parallel()
	assertZeroValueShape(t, ir.OAuthFlow{}, `{}`)
}

// TestOAuthFlow_ScopesDeterministic pins Class C for OAuthFlow's map field:
// Scopes must marshal with keys in sorted order on every run.
func TestOAuthFlow_ScopesDeterministic(t *testing.T) {
	t.Parallel()
	flow := ir.OAuthFlow{
		Scopes: map[string]string{
			"z:scope": "z",
			"m:scope": "m",
			"a:scope": "a",
			"q:scope": "q",
			"b:scope": "b",
		},
	}
	got := assertDeterministicMarshal(t, flow)
	assert.Contains(t, got,
		`"scopes":{"a:scope":"a","b:scope":"b","m:scope":"m","q:scope":"q","z:scope":"z"}`)
}

// TestAuthRequirement_JSONContract pins AuthRequirement's omitempty contract
// — Schemes is optional, since an empty AuthRequirement ("no auth is one
// acceptable choice") is itself meaningful within the enclosing OR-slice and
// must marshal compactly — and that an AuthRequirement with multiple
// SchemeUses (an AND-of-schemes option) round-trips in source order.
func TestAuthRequirement_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.AuthRequirement{}, `{}`, ir.AuthRequirement{
		Schemes: []ir.SchemeUse{
			{Scheme: "auth/oauth2", Scopes: []string{"read", "write"}},
			{Scheme: "auth/apiKey"},
		},
	})
}

// TestSchemeUse_JSONContract pins SchemeUse's omitempty contract (both fields
// are optional) and that a populated SchemeUse round-trips with its scope
// list in source order.
func TestSchemeUse_JSONContract(t *testing.T) {
	t.Parallel()
	assertJSONContract(t, ir.SchemeUse{}, `{}`,
		ir.SchemeUse{Scheme: "auth/oauth2", Scopes: []string{"z-scope", "a-scope", "m-scope"}})
}
