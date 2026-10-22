package auth_test

import (
	"cmp"
	"encoding/json/jsontext"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestLowerSecuritySchemes_AFlowlessDeclarationIsReportedOnceWhereItIsWritten
// pins the placement lowerSecurityScheme's doc states: what is said of the
// fields is placed at decl. Two aliases are declared before their target, so
// the declaration is reached through an alias first; every entry interns, and
// the one flowless declaration is reported once, at the target.
func TestLowerSecuritySchemes_AFlowlessDeclarationIsReportedOnceWhereItIsWritten(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - alias: []
paths: {}
components:
  securitySchemes:
    alias: {$ref: '#/components/securitySchemes/target'}
    other: {$ref: '#/components/securitySchemes/target'}
    target: {type: oauth2, flows: {}}
`)
	for _, name := range []string{"alias", "other", "target"} {
		require.Contains(t, doc.Auth, ids.Auth(name), "%s interns a named scheme of its own (issue #107)", name)
	}
	require.Len(t, svc.Auth, 1)
	require.Len(t, svc.Auth[0].Schemes, 1)
	assert.Equal(t, ids.Auth("alias"), svc.Auth[0].Schemes[0].Scheme)

	assert.Equal(t, []string{"/components/securitySchemes/target"},
		sortedPointersAt(diags, diag.OAuth2NoFlow),
		"one report, at the declaration the aliases share: %+v", diags)
	assert.Empty(t, messagesAtPointer(diags, "/components/securitySchemes/alias"),
		"nothing is reported against an alias, which holds no flows either way")
	assert.Empty(t, messagesAtPointer(diags, "/components/securitySchemes/other"))
}

// TestLowerSecuritySchemes_AnOAuth2SchemeWithNoFlowIsReportedOnce pins who
// reports an oauth2 scheme that states no way to obtain a token (GitHub #646):
// the loader's rule table, where `flows` is missing or no object, and this
// lowering otherwise. Never both, and neither for a scheme stating a flow or a
// metadata URL. Each row compiles through the real loader. Every interned entry
// keeps the requirement naming it, and the table's report is placed where the
// scheme or its flows is written.
func TestLowerSecuritySchemes_AnOAuth2SchemeWithNoFlowIsReportedOnce(t *testing.T) {
	t.Parallel()
	const (
		byNobody = iota
		byLoader
		byLowering
	)
	cases := []struct {
		name string
		// version is the document's openapi field; "" means 3.1.0.
		version string
		// entry is the body written for the securitySchemes entry `s`.
		entry    string
		kind     ir.AuthKind
		reporter int
		// at is where the table reports flows, below the declaration.
		at string
	}{
		{"flows written empty (the issue repro)", "", `{type: oauth2, flows: {}}`, ir.AuthKindOAuth2, byLowering, ""},
		{"flows holding only an extension", "", `{type: oauth2, flows: {x-note: n}}`, ir.AuthKindOAuth2, byLowering, ""},
		{
			"flows naming only a key no flow has", "",
			`{type: oauth2, flows: {application: {tokenUrl: 'https://t', scopes: {}}}}`, ir.AuthKindOAuth2, byLowering, "",
		},
		{
			"an empty metadata url names no server", "3.2.0",
			`{type: oauth2, flows: {}, oauth2MetadataUrl: ''}`, ir.AuthKindOAuth2, byLowering, "",
		},
		{"flows absent", "", `{type: oauth2}`, ir.AuthKindOAuth2, byLoader, ""},
		{"flows null", "", `{type: oauth2, flows: null}`, ir.AuthKindOAuth2, byLoader, "/flows"},
		{"flows not an object", "", `{type: oauth2, flows: []}`, ir.AuthKindOAuth2, byLoader, "/flows"},
		// The null is read as the flow left out, so the scheme declares none.
		{"flows holding only a null flow", "", `{type: oauth2, flows: {implicit: null}}`, ir.AuthKindOAuth2, byLowering, ""},
		{
			"flows empty beside a 3.2 metadata url", "3.2.0",
			`{type: oauth2, flows: {}, oauth2MetadataUrl: 'https://meta'}`, ir.AuthKindOAuth2, byNobody, "",
		},
		{
			"flows empty beside a metadata url a 3.1 document writes", "3.1.0",
			`{type: oauth2, flows: {}, oauth2MetadataUrl: 'https://meta'}`, ir.AuthKindOAuth2, byNobody, "",
		},
		{
			"one declared flow", "",
			`{type: oauth2, flows: {implicit: {authorizationUrl: 'https://a', scopes: {}}}}`, ir.AuthKindOAuth2, byNobody, "",
		},
		{
			"one declared device flow", "3.2.0",
			`{type: oauth2, flows: {deviceAuthorization: {deviceAuthorizationUrl: 'https://d', tokenUrl: 'https://t', scopes: {}}}}`,
			ir.AuthKindOAuth2, byNobody, "",
		},
		// Pins the kind guard: a flows object on an apiKey scheme is a stray
		// field, kept beside it, not a flowless oauth2 scheme.
		{"an apiKey scheme carrying an empty flows object", "", `{type: apiKey, name: k, in: header, flows: {}}`, ir.AuthKindAPIKey, byNobody, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, svc, diags := serviceSpec(t, `openapi: `+cmp.Or(tc.version, "3.1.0")+`
info: {title: T, version: "1"}
security:
  - s: []
paths: {}
components:
  securitySchemes:
    s: `+tc.entry+`
`)
			s, interned := doc.Auth[ids.Auth("s")]
			require.True(t, interned, "the entry is interned, not refused: %+v", diags)
			assert.Equal(t, tc.kind, s.Kind)
			require.Len(t, svc.Auth, 1, "the requirement naming it survives")
			require.Len(t, svc.Auth[0].Schemes, 1)
			assert.Equal(t, ids.Auth("s"), svc.Auth[0].Schemes[0].Scheme, "and keeps a live AuthID")

			wantLoader := 0
			if tc.reporter == byLoader {
				wantLoader = 1
			}
			findings := loaderFlowsFindings(diags)
			assert.Len(t, findings, wantLoader, "the loader's report of a missing flows: %+v", diags)
			for _, f := range findings {
				assert.Equal(t, jsontext.Pointer("/components/securitySchemes/s"+tc.at), f.Provenance.Pointer,
					"the loader places its report where it is written, not by position alone (GitHub #803)")
			}
			if tc.reporter != byLowering {
				assert.Empty(t, messagesAt(diags, diag.OAuth2NoFlow), "no report from the lowering: %+v", diags)
				return
			}
			require.Len(t, messagesAt(diags, diag.OAuth2NoFlow), 1, "one report: %+v", diags)
			// At the declaration, not at the flows object inside it.
			msg := openapitest.DiagMessageAt(t, diags, diag.OAuth2NoFlow, ir.SeverityWarning,
				"/components/securitySchemes/s")
			assert.Contains(t, msg, "declares no flow")
		})
	}
}

// loaderFlowsFindings returns the loader's errors that an oauth2 scheme's flows
// is missing or no object. Each names the field only in its message, so that is
// what identifies the finding.
func loaderFlowsFindings(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Severity == ir.SeverityError && strings.HasPrefix(d.Code, diag.Validation) &&
			strings.Contains(d.Message, `Security Scheme Object field "flows"`) {
			out = append(out, d)
		}
	}
	return out
}

// TestLowerSecuritySchemes_AnUnkeepableFlowFieldIsReportedNotDropped is the
// flow-level twin of the scheme-level test above: a field the flow's kind does
// not define, holding a value JSON cannot represent, reaches the IR in no form,
// so it is reported as lost rather than announced as kept, and the slot it
// would have been misread into stays empty.
func TestLowerSecuritySchemes_AnUnkeepableFlowFieldIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s:
      type: oauth2
      flows:
        implicit: {authorizationUrl: "https://a.example/a", tokenUrl: .nan, scopes: {}}
`)
	s, ok := doc.Auth[ids.Auth("s")]
	require.True(t, ok)
	require.Len(t, s.Flows, 1)
	assert.Empty(t, s.Flows[0].TokenURL, "an implicit flow has no token endpoint to put it in")
	assert.Empty(t, s.Flows[0].Unmodeled, "nothing was kept, so nothing claims to have been")

	const at = "/components/securitySchemes/s/flows/implicit/tokenUrl"
	assert.Equal(t, []string{diag.UnpreservableConstruct, "openapi/validation/validation-type-mismatch"},
		codesAtPointer(diags, at),
		"the loss reported at the field once, beside the loader's typing warning, and not also announced as "+
			"kept: %+v", diags)
}

// TestLowerSecuritySchemes_AnAbsentFlowsIsReportedAtTheDeclaration pins where
// the loader's report of a missing `flows` is placed when the declaration is
// reached through aliases first (GitHub #803): once, at the target, with the
// position of its text, and never at an alias, which holds no flows either way.
// The lowering stands aside, so no no-flow warning appears anywhere.
func TestLowerSecuritySchemes_AnAbsentFlowsIsReportedAtTheDeclaration(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    alias: {$ref: '#/components/securitySchemes/target'}
    other: {$ref: '#/components/securitySchemes/target'}
    target: {type: oauth2}
`)
	for _, name := range []string{"alias", "other", "target"} {
		require.Contains(t, doc.Auth, ids.Auth(name), "%s interns", name)
	}

	const declared = "/components/securitySchemes/target"
	var required []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.Validation+"/validation-required-field" {
			required = append(required, d)
		}
	}
	require.Len(t, required, 1, "one finding for the one declaration: %+v", diags)
	assert.Equal(t, jsontext.Pointer(declared), required[0].Provenance.Pointer)
	assert.NotZero(t, required[0].Provenance.Position, "the finding still carries where the text is")
	assert.Empty(t, messagesAtPointer(diags, "/components/securitySchemes/alias"))
	assert.Empty(t, messagesAtPointer(diags, "/components/securitySchemes/other"))
	assert.Empty(t, messagesAt(diags, diag.OAuth2NoFlow), "the lowering does not repeat the loader: %+v", diags)
}

// oauthFlowSpec is a 3.2 document declaring one oauth2 scheme `s` whose only
// flow is written under keyword as body.
func oauthFlowSpec(keyword, body string) string {
	return `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s:
      type: oauth2
      flows:
        ` + keyword + `: ` + body + `
`
}

// onlyFlow returns the one flow of the oauth2 scheme `s` that spec compiled to.
func onlyFlow(t *testing.T, doc *ir.Document) ir.OAuthFlow {
	t.Helper()
	s, ok := doc.Auth[ids.Auth("s")]
	require.True(t, ok, "the scheme interns")
	require.Len(t, s.Flows, 1)
	return s.Flows[0]
}

// TestLowerSecuritySchemes_AFlowFieldItsKindDoesNotDefineIsKept pins that a
// URL written on a flow whose kind gives it no meaning is kept verbatim under
// that flow's Unmodeled rather than dropped or lowered into a slot the flow
// lacks (GitHub #865). Each row is one pair of flow keyword and undefined
// field; the rows are spelled out from the OAuth Flow Object, not derived from
// the lowering's own table, which would agree with itself.
func TestLowerSecuritySchemes_AFlowFieldItsKindDoesNotDefineIsKept(t *testing.T) {
	t.Parallel()
	const (
		authorize = "https://a.example/authorize"
		token     = "https://a.example/token"
		device    = "https://a.example/device"
		stray     = "https://stray.example/"
	)
	cases := []struct {
		keyword, kind, field, body string
		// wantAuthorize and wantToken are what the flow's own fields lower to.
		wantAuthorize, wantToken string
	}{
		{
			"implicit", "implicit", "deviceAuthorizationUrl",
			`{authorizationUrl: "` + authorize + `", deviceAuthorizationUrl: "` + stray + `", scopes: {}}`,
			authorize, "",
		},
		{
			"implicit", "implicit", "tokenUrl",
			`{authorizationUrl: "` + authorize + `", tokenUrl: "` + stray + `", scopes: {}}`,
			authorize, "",
		},
		{
			"authorizationCode", "authorization_code", "deviceAuthorizationUrl",
			`{authorizationUrl: "` + authorize + `", tokenUrl: "` + token + `", deviceAuthorizationUrl: "` + stray + `", scopes: {}}`,
			authorize, token,
		},
		{
			"password", "password", "authorizationUrl",
			`{tokenUrl: "` + token + `", authorizationUrl: "` + stray + `", scopes: {}}`,
			"", token,
		},
		{
			"password", "password", "deviceAuthorizationUrl",
			`{tokenUrl: "` + token + `", deviceAuthorizationUrl: "` + stray + `", scopes: {}}`,
			"", token,
		},
		{
			"clientCredentials", "client_credentials", "authorizationUrl",
			`{tokenUrl: "` + token + `", authorizationUrl: "` + stray + `", scopes: {}}`,
			"", token,
		},
		{
			"clientCredentials", "client_credentials", "deviceAuthorizationUrl",
			`{tokenUrl: "` + token + `", deviceAuthorizationUrl: "` + stray + `", scopes: {}}`,
			"", token,
		},
		{
			"deviceAuthorization", "device", "authorizationUrl",
			`{deviceAuthorizationUrl: "` + device + `", tokenUrl: "` + token + `", authorizationUrl: "` + stray + `", scopes: {}}`,
			device, token,
		},
	}
	for _, tc := range cases {
		t.Run(tc.keyword+" writing "+tc.field, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, oauthFlowSpec(tc.keyword, tc.body))
			openapitest.RequireNoErrorDiags(t, diags)
			fl := onlyFlow(t, doc)

			assert.Equal(t, tc.kind, fl.Kind)
			assert.Equal(t, tc.wantAuthorize, fl.AuthorizationURL, "only a field the kind defines fills this slot")
			assert.Equal(t, tc.wantToken, fl.TokenURL, "only a field the kind defines fills this slot")

			key := "openapi:" + tc.field
			pointer := "/components/securitySchemes/s/flows/" + tc.keyword + "/" + tc.field
			assert.Equal(t, []string{key}, unmodeledKeys(fl.Unmodeled), "the one stray field, and nothing else")
			kept := fl.Unmodeled[key]
			assert.Equal(t, `"`+stray+`"`, string(kept.Value))
			assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
			assert.Equal(t, jsontext.Pointer(pointer), kept.Provenance.Pointer)

			assert.Equal(t, "oauth flow "+tc.keyword+" does not define "+tc.field+"; kept verbatim under Unmodeled",
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, pointer))
			assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
				"one announcement for the one kept field: %+v", diags)
		})
	}
}

// TestLowerSecuritySchemes_AFlowWritingOnlyItsDefinedFieldsKeepsNothing is the
// control for the test above: each flow kind written with exactly the fields
// it defines, plus a refreshUrl every kind defines, lowers them all into their
// slots and keeps nothing, announced or not.
func TestLowerSecuritySchemes_AFlowWritingOnlyItsDefinedFieldsKeepsNothing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		keyword, kind, body      string
		wantAuthorize, wantToken string
	}{
		{
			"implicit", "implicit",
			`{authorizationUrl: "https://a.example/authorize", refreshUrl: "https://a.example/refresh", scopes: {}}`,
			"https://a.example/authorize", "",
		},
		{
			"authorizationCode", "authorization_code",
			`{authorizationUrl: "https://a.example/authorize", tokenUrl: "https://a.example/token", refreshUrl: "https://a.example/refresh", scopes: {}}`,
			"https://a.example/authorize", "https://a.example/token",
		},
		{
			"password", "password",
			`{tokenUrl: "https://a.example/token", refreshUrl: "https://a.example/refresh", scopes: {}}`,
			"", "https://a.example/token",
		},
		{
			"clientCredentials", "client_credentials",
			`{tokenUrl: "https://a.example/token", refreshUrl: "https://a.example/refresh", scopes: {}}`,
			"", "https://a.example/token",
		},
		{
			"deviceAuthorization", "device",
			`{deviceAuthorizationUrl: "https://a.example/device", tokenUrl: "https://a.example/token", refreshUrl: "https://a.example/refresh", scopes: {}}`,
			"https://a.example/device", "https://a.example/token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, oauthFlowSpec(tc.keyword, tc.body))
			openapitest.RequireNoErrorDiags(t, diags)
			fl := onlyFlow(t, doc)

			assert.Equal(t, tc.kind, fl.Kind)
			assert.Equal(t, tc.wantAuthorize, fl.AuthorizationURL)
			assert.Equal(t, tc.wantToken, fl.TokenURL)
			assert.Equal(t, "https://a.example/refresh", fl.RefreshURL)
			assert.Empty(t, fl.Unmodeled, "every field written is one the kind defines")
			assert.Empty(t, messagesAt(diags, diag.DegradedConstruct), "so nothing is announced: %+v", diags)
		})
	}
}

// TestLowerSecuritySchemes_AKeptFlowFieldIsPlacedAtTheDeclaration pins that a
// stray flow field is located where it is written when the scheme is reached
// through aliases declared before the target: every entry interns, the kept
// entry and the one announcement sit at the target, and nothing is placed at
// a position an alias does not hold.
func TestLowerSecuritySchemes_AKeptFlowFieldIsPlacedAtTheDeclaration(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    alias: {$ref: '#/components/securitySchemes/target'}
    other: {$ref: '#/components/securitySchemes/target'}
    target:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://a.example/token
          authorizationUrl: https://stray.example/
          scopes: {}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	const declared = "/components/securitySchemes/target/flows/clientCredentials/authorizationUrl"
	for _, name := range []string{"alias", "other", "target"} {
		s, ok := doc.Auth[ids.Auth(name)]
		require.True(t, ok, "%s interns a scheme of its own", name)
		require.Len(t, s.Flows, 1)
		assert.Equal(t, "https://a.example/token", s.Flows[0].TokenURL)
		assert.Empty(t, s.Flows[0].AuthorizationURL)
		assert.Equal(t, jsontext.Pointer(declared),
			s.Flows[0].Unmodeled["openapi:authorizationUrl"].Provenance.Pointer,
			"the kept field is located where it is written, for %s too", name)
	}

	assert.Equal(t, []string{declared}, sortedPointersAt(diags, diag.DegradedConstruct),
		"aliases of one declaration announce it once: %+v", diags)
	for _, d := range diags {
		for _, alias := range []string{"alias", "other"} {
			prefix := "/components/securitySchemes/" + alias
			assert.False(t, string(d.Provenance.Pointer) == prefix || strings.HasPrefix(string(d.Provenance.Pointer), prefix+"/"),
				"nothing is placed under %s, which holds only a $ref: %+v", alias, d)
		}
	}
}

// TestLowerSecuritySchemes_AMergedOrAliasedFlowFieldFillsOnlyItsKindsSlot pins
// the flow field spellings the raw mapping does not hold as written. A field
// the kind does not define never fills a slot, whether merged in through `<<`
// or written as an alias, and the alias, which the mapping does hold, is kept
// as its value. A merged field the kind defines still lowers. Whether a merged
// stray field is kept is GitHub #384's, as for every preservation site, so it
// is not asserted here.
func TestLowerSecuritySchemes_AMergedOrAliasedFlowFieldFillsOnlyItsKindsSlot(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, keyword, kind, body string
		wantAuth, wantTok         string
		// wantKept is the field expected under Unmodeled, or "" for none; merged
		// says the stray field arrives through `<<`, which leaves that to #384.
		wantKept string
		merged   bool
	}{
		{
			name: "a merge key supplying a field the kind does not define", keyword: "implicit", kind: "implicit",
			body:     `{<<: *base, authorizationUrl: "https://a.example/a", scopes: {}}`,
			wantAuth: "https://a.example/a", merged: true,
		},
		{
			name: "an alias value", keyword: "implicit", kind: "implicit",
			body:     `{authorizationUrl: "https://a.example/a", tokenUrl: *url, scopes: {}}`,
			wantKept: "tokenUrl", wantAuth: "https://a.example/a",
		},
		{
			name: "a merge key supplying a field the kind defines", keyword: "clientCredentials", kind: "client_credentials",
			body:    `{<<: *base, scopes: {}}`,
			wantTok: "https://a.example/t",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
x-base: &base {tokenUrl: "https://a.example/t"}
x-url: &url "https://a.example/t"
components:
  securitySchemes:
    s:
      type: oauth2
      flows:
        `+tc.keyword+`: `+tc.body+`
`)
			openapitest.RequireNoErrorDiags(t, diags)
			fl := onlyFlow(t, doc)

			assert.Equal(t, tc.kind, fl.Kind)
			assert.Equal(t, tc.wantAuth, fl.AuthorizationURL)
			assert.Equal(t, tc.wantTok, fl.TokenURL, "a field the kind does not define fills no slot")
			if tc.merged {
				return
			}
			if tc.wantKept == "" {
				assert.Empty(t, fl.Unmodeled)
				assert.Empty(t, messagesAt(diags, diag.DegradedConstruct), "%+v", diags)
				return
			}
			pointer := "/components/securitySchemes/s/flows/" + tc.keyword + "/" + tc.wantKept
			assert.Equal(t, []string{"openapi:" + tc.wantKept}, unmodeledKeys(fl.Unmodeled))
			kept := fl.Unmodeled["openapi:"+tc.wantKept]
			assert.Equal(t, `"https://a.example/t"`, string(kept.Value))
			assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
			assert.Equal(t, jsontext.Pointer(pointer), kept.Provenance.Pointer)
			assert.Equal(t, "oauth flow "+tc.keyword+" does not define "+tc.wantKept+"; kept verbatim under Unmodeled",
				openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, pointer))
		})
	}
}

// TestLowerSecuritySchemes_AFlowsScopesKeepSourceOrder pins that scopes lower
// in the order the document writes them, not sorted by name (GitHub #639):
// consent screens and generated documentation show them as the author did. One
// row is in no sorted order, ascending or descending; an empty description is
// kept as written, and a flow with no scopes lowers to nil.
func TestLowerSecuritySchemes_AFlowsScopesKeepSourceOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		scopes string
		want   []ir.OAuthScope
	}{
		{
			"descending", `{write: Modify data, read: Read data, admin: Full access}`,
			[]ir.OAuthScope{{Name: "write", Description: "Modify data"}, {Name: "read", Description: "Read data"}, {Name: "admin", Description: "Full access"}},
		},
		{
			"neither ascending nor descending", `{read: Read data, admin: Full access, write: Modify data}`,
			[]ir.OAuthScope{{Name: "read", Description: "Read data"}, {Name: "admin", Description: "Full access"}, {Name: "write", Description: "Modify data"}},
		},
		{"an empty description", `{read: ""}`, []ir.OAuthScope{{Name: "read"}}},
		{"none declared", `{}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, oauthFlowSpec("clientCredentials",
				`{tokenUrl: "https://a.example/token", scopes: `+tc.scopes+`}`))
			openapitest.RequireNoErrorDiags(t, diags)
			fl := onlyFlow(t, doc)

			assert.Empty(t, gocmp.Diff(tc.want, fl.Scopes), "scopes lower in source order")
		})
	}
}
