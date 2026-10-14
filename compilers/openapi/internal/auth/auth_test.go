package auth_test

import (
	"cmp"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func TestAuth_SchemeKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		scheme string
		check  func(t *testing.T, s ir.AuthScheme)
	}{
		{
			name:   "apiKey",
			scheme: "{type: apiKey, in: header, name: X-Key}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindAPIKey, s.Kind)
				assert.Equal(t, "header", s.In)
				assert.Equal(t, "X-Key", s.KeyName)
			},
		},
		{
			// RFC 7235 §2.1 makes the scheme token case-insensitive, and a
			// document is free to spell it as the RFC's own prose does. Getting
			// this wrong degrades a first-class kind to AuthKindCustom, which an
			// SDK emitter renders as an opaque header rather than as basic auth.
			name:   "http basic, capitalised as the RFC spells it",
			scheme: "{type: http, scheme: Basic}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBasic, s.Kind)
				assert.Empty(t, s.Scheme, "a recognised token is not also carried as a custom one")
			},
		},
		{
			name:   "http bearer, upper case",
			scheme: "{type: http, scheme: BEARER, bearerFormat: JWT}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBearer, s.Kind)
				assert.Equal(t, "JWT", s.BearerFormat)
			},
		},
		{
			name:   "http basic",
			scheme: "{type: http, scheme: basic}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBasic, s.Kind)
			},
		},
		{
			name:   "http bearer",
			scheme: "{type: http, scheme: bearer, bearerFormat: JWT}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindHTTPBearer, s.Kind)
				assert.Equal(t, "JWT", s.BearerFormat)
			},
		},
		{
			name:   "oauth2 implicit",
			scheme: `{type: oauth2, flows: {implicit: {authorizationUrl: "https://a.example/auth", scopes: {read: r}}}}`,
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindOAuth2, s.Kind)
				require.Len(t, s.Flows, 1)
				assert.Equal(t, "implicit", s.Flows[0].Kind)
				assert.Equal(t, "https://a.example/auth", s.Flows[0].AuthorizationURL)
				assert.Equal(t, []ir.OAuthScope{{Name: "read", Description: "r"}}, s.Flows[0].Scopes)
			},
		},
		{
			name:   "openIdConnect",
			scheme: `{type: openIdConnect, openIdConnectUrl: "https://a.example/.well-known"}`,
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindOpenIDConnect, s.Kind)
				assert.Equal(t, "https://a.example/.well-known", s.OpenIDConnectURL)
			},
		},
		{
			name:   "mutualTLS",
			scheme: "{type: mutualTLS}",
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindMutualTLS, s.Kind)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: 3.1.0\n" +
				"info: {title: T, version: \"1\"}\n" +
				"paths: {}\n" +
				"components:\n" +
				"  securitySchemes:\n" +
				"    s: " + tc.scheme + "\n"
			doc, _, diags := serviceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			s, ok := doc.Auth[ids.Auth("s")]
			require.True(t, ok)
			tc.check(t, s)
		})
	}
}

func TestAuth_RequirementsOrOfAnds(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - {}
  - key: []
  - oauth: [read, write]
    key: []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
    oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://a.example/token
          scopes: {read: r, write: w}
`
	doc, svc, diags := serviceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	keyID := ids.Auth("key")
	scheme, ok := doc.Auth[keyID]
	require.True(t, ok)
	assert.Equal(t, ir.AuthKindAPIKey, scheme.Kind)
	assert.Equal(t, "header", scheme.In)
	assert.Equal(t, "X-Key", scheme.KeyName)

	oauth := doc.Auth[ids.Auth("oauth")]
	require.Len(t, oauth.Flows, 1)
	assert.Equal(t, "client_credentials", oauth.Flows[0].Kind)
	assert.Equal(t, "https://a.example/token", oauth.Flows[0].TokenURL)

	require.Len(t, svc.Auth, 3) // OR across options, source order
	assert.Empty(t, svc.Auth[0].Schemes, "empty requirement = no-auth is one acceptable choice")
	require.Len(t, svc.Auth[1].Schemes, 1)
	assert.Equal(t, keyID, svc.Auth[1].Schemes[0].Scheme)
	require.Len(t, svc.Auth[2].Schemes, 2, "one option, two schemes ANDed")
	assert.Equal(t, []string{"read", "write"}, svc.Auth[2].Schemes[0].Scopes)
}

// TestAuth_UnserializableExtensionStillWarns pins that a security scheme whose
// sole x-* extension fails to serialize still surfaces the warning:
// lowerSecurityScheme returns the diagnostic unconditionally, so a caller must
// not gate it behind the same "if len(ext) > 0" that guards the assignment —
// that guard is exactly false in the all-unserializable case, which used to
// drop the warning silently.
func TestAuth_UnserializableExtensionStillWarns(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s: {type: apiKey, in: header, name: X-Key, x-bad: {1: intkey}}
`
	doc, _, diags := serviceSpec(t, spec)
	assert.True(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning) > 0,
		"an entirely unserializable extension still warns even though the scheme's own Unmodeled ends up empty")
	scheme, ok := doc.Auth[ids.Auth("s")]
	require.True(t, ok)
	assert.Empty(t, scheme.Unmodeled, "the unserializable extension is dropped, not stored")
}

const authSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get: {operationId: x, responses: {"200": {description: ok}}}
components:
  securitySchemes:
    apiKey: {type: apiKey, in: header, name: X-Key}
    basicAuth: {type: http, scheme: basic, deprecated: true}
    bearerAuth: {type: http, scheme: bearer, bearerFormat: JWT}
    customHttp: {type: http, scheme: negotiate}
    oauth:
      type: oauth2
      x-note: n
      flows:
        authorizationCode:
          authorizationUrl: 'https://a'
          tokenUrl: 'https://t'
          refreshUrl: 'https://r'
          scopes: {read: read access}
        clientCredentials: {tokenUrl: 'https://t'}
        implicit: {authorizationUrl: 'https://a', scopes: {}}
        password: {tokenUrl: 'https://t'}
        deviceAuthorization: {deviceAuthorizationUrl: 'https://d', tokenUrl: 'https://t'}
    oidc: {type: openIdConnect, openIdConnectUrl: 'https://oidc'}
    mtls: {type: mutualTLS}
`

func TestAuth_AllSchemeKinds(t *testing.T) {
	t.Parallel()
	doc, _, _ := serviceSpec(t, authSpec)
	byKind := map[ir.AuthKind]ir.AuthScheme{}
	for _, s := range doc.Auth {
		byKind[s.Kind] = s
	}
	assert.Equal(t, "header", byKind[ir.AuthKindAPIKey].In)
	require.NotNil(t, byKind[ir.AuthKindHTTPBasic].Deprecation, "deprecated basic scheme")
	assert.Equal(t, "JWT", byKind[ir.AuthKindHTTPBearer].BearerFormat)
	assert.Equal(t, "openIdConnect", "openIdConnect")
	assert.Equal(t, "https://oidc", byKind[ir.AuthKindOpenIDConnect].OpenIDConnectURL)
	_, hasMTLS := byKind[ir.AuthKindMutualTLS]
	assert.True(t, hasMTLS)

	oauth := byKind[ir.AuthKindOAuth2]
	assert.NotEmpty(t, oauth.Unmodeled, "oauth x-* extension")
	kinds := openapitest.IndexBy(oauth.Flows, func(f ir.OAuthFlow) string { return f.Kind })
	assert.Len(t, oauth.Flows, 5)
	assert.Equal(t, "https://r", kinds["authorization_code"].RefreshURL)
	assert.NotEmpty(t, kinds["authorization_code"].Scopes)
	assert.Nil(t, kinds["implicit"].Scopes, "empty scope map is nil")
	assert.Equal(t, "https://d", kinds["device"].AuthorizationURL, "device auth url rides AuthorizationURL")

	// The negotiate HTTP scheme is custom with the token preserved.
	var sawCustomHTTP bool
	for _, s := range doc.Auth {
		if s.Kind == ir.AuthKindCustom && s.Scheme == "negotiate" {
			sawCustomHTTP = true
		}
	}
	assert.True(t, sawCustomHTTP, "unknown http scheme is custom")
}

func TestAuth_OAuthNoFlowsUnknownTypeAndGhostRef(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /x:
    get: {operationId: x, responses: {"200": {description: ok}}}
components:
  securitySchemes:
    oauthNoFlows: {type: oauth2}
    weird: {type: bananas}
    ghost: {$ref: '#/components/securitySchemes/Missing'}
`)
	doc, _, _ := serviceSpec(t, spec)
	var oauth, custom ir.AuthScheme
	for _, s := range doc.Auth {
		if s.Kind == ir.AuthKindOAuth2 {
			oauth = s
		}
		if s.Kind == ir.AuthKindCustom {
			custom = s
		}
	}
	assert.Equal(t, ir.AuthKindOAuth2, oauth.Kind)
	assert.Nil(t, oauth.Flows, "oauth2 with no flows lowers to nil flows")
	assert.Equal(t, "bananas", custom.Scheme, "unknown scheme type degrades to custom")
}

// TestLowerSecuritySchemes_NothingLoweredIsNilNotEmpty pins the guard that
// keeps an empty map out of the document. A components.securitySchemes block
// whose every entry fails to resolve declares no scheme the IR can carry, and
// an empty Auth map would be a field the source never wrote.
//
// The entry is a hand-built nil, which no parsed document produces — a
// malformed entry still arrives as an object. That makes this the nil-guard
// case rather than the reporting one, so nothing is reported: the entry carries
// no $ref to have failed. TestLowerSecuritySchemes_ABrokenRefIsReportedByLoadOnce
// covers the shapes a document can write, through the compiler.
func TestLowerSecuritySchemes_NothingLoweredIsNilNotEmpty(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Components: &soa.Components{
		SecuritySchemes: sequencedmap.New(
			sequencedmap.NewElem("ghost", (*soa.ReferencedSecurityScheme)(nil))),
	}}

	got, diags := auth.LowerSecuritySchemes(lowering.Ctx{Doc: doc})

	assert.Nil(t, got, "an unresolvable entry leaves no map behind")
	assert.Empty(t, diags, "a nil entry names no reference that could have failed")
}

// TestLowerSecuritySchemes_ABrokenRefIsReportedByLoadOnce pins how each
// unresolvable entry below is reported, through the compiler rather than a
// hand-built node: a document can write these shapes. None is named by a
// requirement, so nothing downstream reports them.
//
// A $ref resolving to nothing is reported once at the entry's own pointer: this
// package and the load phase both report it there, and the compiler keeps the
// load phase's, which carries the resolver's reason (GitHub #385). An entry not
// written as an object already draws the loader's type-mismatch alone.
func TestLowerSecuritySchemes_ABrokenRefIsReportedByLoadOnce(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
		// wantRef is the reference the load phase's report must quote, or ""
		// when nothing should land at the entry's own pointer.
		wantRef string
	}{
		{
			name:    "a $ref naming no target in this document",
			entry:   `{$ref: '#/components/securitySchemes/Missing'}`,
			wantRef: "#/components/securitySchemes/Missing",
		},
		{
			name:    "a $ref out to a document this compile refuses to read",
			entry:   `{$ref: 'other.yaml#/components/securitySchemes/X'}`,
			wantRef: "other.yaml#/components/securitySchemes/X",
		},
		{name: "an entry written as null", entry: `null`},
		{name: "an entry written as a scalar", entry: `42`},
		{name: "an entry written as a sequence", entry: `[a]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: `+tc.entry+`
    key: {type: apiKey, in: header, name: X-Key}
`)
			assert.NotContains(t, doc.Auth, ids.Auth("ghost"), "the entry interns no scheme")
			assert.Contains(t, doc.Auth, ids.Auth("key"), "its resolvable sibling still does")

			got := messagesAtPointer(diags, "/components/securitySchemes/ghost")
			if tc.wantRef == "" {
				assert.Equal(t, []string{"openapi/validation/validation-type-mismatch"},
					codesAtPointer(diags, "/components/securitySchemes/ghost"),
					"a non-$ref shape is left to the loader's own type-mismatch: %+v", diags)
				return
			}
			require.Len(t, got, 1, "the load phase reports the broken $ref exactly once: %+v", diags)
			assert.Contains(t, got[0], `unresolved $ref "`+tc.wantRef+`"`,
				"the load phase's own report, naming the reference as written")
		})
	}
}

// TestLowerSecuritySchemes_AnEntryTheLoadPhaseNeverReachedIsReportedHere pins
// what this package's report of a broken $ref is for. A resolver panic ends the
// load phase's walk at /components/responses/000, before it reaches the entry,
// so the load phase reports nothing there; without this report the scheme the
// document declares would be dropped with no diagnostic that sites it.
func TestLowerSecuritySchemes_AnEntryTheLoadPhaseNeverReachedIsReportedHere(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.0.3
info: {title: T, version: "1"}
paths: {}
components:
  responses:
    "000": {$ref: '#/B'}
  securitySchemes:
    ghost: {$ref: '#/components/securitySchemes/Missing'}
B: {$ref}
`)
	require.NotEmpty(t, messagesAtPointer(diags, "/components/responses/000"),
		"the fixture reaches the resolver panic it exists for: %+v", diags)

	assert.NotContains(t, doc.Auth, ids.Auth("ghost"))
	got := messagesAtPointer(diags, "/components/securitySchemes/ghost")
	require.Len(t, got, 1, "%+v", diags)
	assert.Equal(t, `security scheme "ghost" has a $ref that resolves to nothing: `+
		`"#/components/securitySchemes/Missing"`, got[0])
}

// TestLowerSecuritySchemes_NoComponentsAtAll pins the two earlier exits: a
// document with no components, and one whose components declare no schemes.
func TestLowerSecuritySchemes_NoComponentsAtAll(t *testing.T) {
	t.Parallel()
	got, diags := auth.LowerSecuritySchemes(lowering.Ctx{Doc: &soa.OpenAPI{}})
	assert.Nil(t, got)
	assert.Empty(t, diags)

	got, diags = auth.LowerSecuritySchemes(lowering.Ctx{Doc: &soa.OpenAPI{Components: &soa.Components{}}})
	assert.Nil(t, got)
	assert.Empty(t, diags)
}

// TestLowerSecuritySchemes_AnEntryNamingNoMechanismIsRefused pins the two
// shapes that declare a scheme without saying what it is: an entry with no
// `type`, and an http entry with no `scheme` token. Both used to intern a
// custom scheme whose mechanism was the empty string, which states that the API
// is authenticated by nothing in particular (GitHub #294).
//
// Each is reported at the entry's own components pointer and interned nowhere,
// exactly as an entry whose $ref resolves to nothing already is.
func TestLowerSecuritySchemes_AnEntryNamingNoMechanismIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
	}{
		{name: "no type at all", entry: `{}`},
		{name: "no type, but fields an apiKey would use", entry: `{in: header, name: X-Key}`},
		{name: "an explicitly empty type", entry: `{type: ""}`},
		{name: "http with no scheme token", entry: `{type: http}`},
		{name: "http with an empty scheme token", entry: `{type: http, scheme: ""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: `+tc.entry+`
    key: {type: apiKey, in: header, name: X-Key}
`)
			assert.NotContains(t, doc.Auth, ids.Auth("ghost"), "the entry interns no scheme")
			assert.Contains(t, doc.Auth, ids.Auth("key"), "its complete sibling still does")

			d, ok := firstDiagAt(diags, diag.IncompleteSecurityScheme)
			require.True(t, ok, "the refusal is reported: %+v", diags)
			assert.Equal(t, ir.SeverityError, d.Severity)
			assert.Equal(t, jsontext.Pointer("/components/securitySchemes/ghost"), d.Provenance.Pointer,
				"reported at the entry, not at the requirement that names it")
			assert.Contains(t, d.Message, `"ghost"`, "the report names the entry")
		})
	}
}

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
// the loader, wherever its REQUIRED-field rule finds `flows` missing, and this
// lowering otherwise. Never both, and neither for a scheme stating a flow or a
// metadata URL. Each row compiles through the real loader, so a library release
// that moves its rule reddens a row rather than doubling or dropping a report.
// Every interned entry keeps the requirement naming it, and the loader's report
// is placed at the declaration.
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
	}{
		{"flows written empty (the issue repro)", "", `{type: oauth2, flows: {}}`, ir.AuthKindOAuth2, byLowering},
		{"flows holding only an extension", "", `{type: oauth2, flows: {x-note: n}}`, ir.AuthKindOAuth2, byLowering},
		{
			"flows naming only a key no flow has", "",
			`{type: oauth2, flows: {application: {tokenUrl: 'https://t', scopes: {}}}}`, ir.AuthKindOAuth2, byLowering,
		},
		{
			"an empty metadata url names no server", "3.2.0",
			`{type: oauth2, flows: {}, oauth2MetadataUrl: ''}`, ir.AuthKindOAuth2, byLowering,
		},
		{"flows absent", "", `{type: oauth2}`, ir.AuthKindOAuth2, byLoader},
		{"flows null", "", `{type: oauth2, flows: null}`, ir.AuthKindOAuth2, byLoader},
		{"flows not an object", "", `{type: oauth2, flows: []}`, ir.AuthKindOAuth2, byLoader},
		// The library reads a flows object whose only flow fails to decode as
		// missing, and says so; a second report here would repeat it.
		{"flows holding a flow that does not decode", "", `{type: oauth2, flows: {implicit: null}}`, ir.AuthKindOAuth2, byLoader},
		{
			"flows empty beside a 3.2 metadata url", "3.2.0",
			`{type: oauth2, flows: {}, oauth2MetadataUrl: 'https://meta'}`, ir.AuthKindOAuth2, byNobody,
		},
		{
			"flows empty beside a metadata url a 3.1 document writes", "3.1.0",
			`{type: oauth2, flows: {}, oauth2MetadataUrl: 'https://meta'}`, ir.AuthKindOAuth2, byNobody,
		},
		{
			"one declared flow", "",
			`{type: oauth2, flows: {implicit: {authorizationUrl: 'https://a', scopes: {}}}}`, ir.AuthKindOAuth2, byNobody,
		},
		{
			"one declared device flow", "3.2.0",
			`{type: oauth2, flows: {deviceAuthorization: {deviceAuthorizationUrl: 'https://d', tokenUrl: 'https://t', scopes: {}}}}`,
			ir.AuthKindOAuth2, byNobody,
		},
		// Pins the kind guard: a flows object on an apiKey scheme is a stray
		// field, kept beside it, not a flowless oauth2 scheme.
		{"an apiKey scheme carrying an empty flows object", "", `{type: apiKey, name: k, in: header, flows: {}}`, ir.AuthKindAPIKey, byNobody},
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
				assert.Equal(t, jsontext.Pointer("/components/securitySchemes/s"), f.Provenance.Pointer,
					"the loader places its report at the declaration, not by position alone (GitHub #803)")
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

// loaderFlowsFindings returns the loader's reports that an oauth2 scheme's
// flows is missing. The library's REQUIRED-field rule names the field only in
// its message, so that is what identifies the finding.
func loaderFlowsFindings(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.Validation+"/"+validation.RuleValidationRequiredField &&
			strings.Contains(d.Message, "`securityScheme.flows`") {
			out = append(out, d)
		}
	}
	return out
}

// TestLowerSecuritySchemes_ARefusedEntryDropsTheRequirementNamingIt pins the
// downstream half of the refusal. Nothing is interned, so a requirement naming
// the entry resolves to no scheme and drops whole under the rule #41
// established, collapsing a sole-option list to nil rather than leaving the
// empty-option encoding that reads as "no auth is also fine".
func TestLowerSecuritySchemes_ARefusedEntryDropsTheRequirementNamingIt(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghost: []
paths: {}
components:
  securitySchemes:
    ghost: {in: header, name: X-Key}
`)
	assert.Empty(t, doc.Auth, "a document whose only scheme is refused carries no auth registry")
	assert.Nil(t, svc.Auth, "the sole option drops, collapsing the list rather than emptying it")
	assert.NotEmpty(t, messagesAt(diags, diag.UnresolvedRef),
		"the requirement that named it is reported too: %+v", diags)
}

// TestLowerSecuritySchemes_FieldsTheTypeDoesNotDefineSurvive pins that no field
// a securityScheme entry declares is dropped: `in` on an oauth2 scheme or
// `flows` on an apiKey must reach Unmodeled, as "lossless by default" requires
// (GitHub #294).
//
// Every case declares every mechanism field, so each row states both
// halves of ir-design §12.1's one-home principle: what the type defines reaches
// its IR field and is not also kept raw, and what it does not define is kept
// raw and does not fill a field of another mechanism.
func TestLowerSecuritySchemes_FieldsTheTypeDoesNotDefineSurvive(t *testing.T) {
	t.Parallel()
	// everyField is written on each entry below; a row names the ones its type
	// defines and the rest must survive under Unmodeled.
	everyField := []string{"bearerFormat", "flows", "in", "name", "oauth2MetadataUrl", "openIdConnectUrl", "scheme"}
	cases := []struct {
		name    string
		typ     string
		defines []string
		check   func(t *testing.T, s ir.AuthScheme)
	}{
		{
			name: "apiKey", typ: "apiKey", defines: []string{"in", "name"},
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindAPIKey, s.Kind)
				assert.Equal(t, "header", s.In)
				assert.Equal(t, "X-Key", s.KeyName)
			},
		},
		{
			name: "http", typ: "http", defines: []string{"scheme", "bearerFormat"},
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindCustom, s.Kind)
				assert.Equal(t, "digest", s.Scheme, "the RFC 7235 token its own type defines")
				assert.Equal(t, "JWT", s.BearerFormat)
			},
		},
		{
			name: "oauth2", typ: "oauth2", defines: []string{"flows", "oauth2MetadataUrl"},
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindOAuth2, s.Kind)
				require.Len(t, s.Flows, 1)
				assert.Equal(t, "https://meta", s.OAuth2MetadataURL)
			},
		},
		{
			name: "openIdConnect", typ: "openIdConnect", defines: []string{"openIdConnectUrl"},
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindOpenIDConnect, s.Kind)
				assert.Equal(t, "https://oidc", s.OpenIDConnectURL)
			},
		},
		{
			// mutualTLS is the whole mechanism; nothing else on the entry is part
			// of it, so all seven fields are kept raw.
			name: "mutualTLS", typ: "mutualTLS", defines: nil,
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindMutualTLS, s.Kind)
			},
		},
		{
			// An unrecognised type degrades to a custom scheme carrying the token
			// itself, which takes the one IR field a declared `scheme` would have
			// reached — so that declaration is kept raw beside the six others.
			name: "an unrecognised type", typ: "bananas", defines: nil,
			check: func(t *testing.T, s ir.AuthScheme) {
				assert.Equal(t, ir.AuthKindCustom, s.Kind)
				assert.Equal(t, "bananas", s.Scheme, "the unrecognised token, not the declared scheme")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s:
      type: `+tc.typ+`
      in: header
      name: X-Key
      scheme: digest
      bearerFormat: JWT
      openIdConnectUrl: https://oidc
      oauth2MetadataUrl: https://meta
      flows:
        implicit: {authorizationUrl: 'https://a', scopes: {}}
`)
			s, ok := doc.Auth[ids.Auth("s")]
			require.True(t, ok)
			tc.check(t, s)

			var wantKept []string
			for _, f := range everyField {
				if !slices.Contains(tc.defines, f) {
					wantKept = append(wantKept, "openapi:"+f)
				}
			}
			assert.Equal(t, wantKept, unmodeledKeys(s.Unmodeled),
				"every field the type does not define is kept, and no field it does define is kept twice")
			for _, key := range wantKept {
				field := strings.TrimPrefix(key, "openapi:")
				assert.Equal(t, ir.ReasonDegradedLowering, s.Unmodeled[key].Reason,
					"%s was lowered to a weaker shape, not left unmodelled for want of a field", key)
				assert.Equal(t, jsontext.Pointer("/components/securitySchemes/s/"+field), s.Unmodeled[key].Provenance.Pointer,
					"the entry locates the field itself, not the scheme that carried it")
			}
		})
	}
}

// serviceSpec compiles src and returns the document, its single service, and
// every diagnostic. It drives the public entry point rather than this package
// directly: the requirements lowered here are reached through the service walk,
// which lives in the compiler above, and only an external test package may
// import it.
func serviceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

// firstDiagAt returns the first diagnostic carrying code, so a test can assert
// on its provenance pointer.
func firstDiagAt(diags []ir.Diagnostic, code string) (ir.Diagnostic, bool) {
	for _, d := range diags {
		if d.Code == code {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// messagesAt returns the message of every diagnostic carrying code, in the order
// they were reported, so a caller can pin which names were reported and not only
// how many.
func messagesAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Message)
		}
	}
	return out
}

// messagesAtPointer returns the message of every diagnostic whose provenance
// names pointer, in report order. It filters on the pointer alone, so a report
// arriving at the right place under the wrong code is still returned.
func messagesAtPointer(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, d.Message)
		}
	}
	return out
}

// codesAtPointer returns the code of every diagnostic sited at pointer,
// sorted, so a caller pins which stage reported there, and how often: the
// loader's findings carry the pointer of the node they are about as well.
func codesAtPointer(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, d.Code)
		}
	}
	slices.Sort(out)
	return out
}

// sortedPointersAt returns every provenance pointer carried by a diagnostic
// with code, sorted so a caller pins the set rather than the walk's order.
func sortedPointersAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, string(d.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}

// operationsByDeclaration indexes every operation in svc by its provenance
// pointer, which is where the operation is declared rather than where it mounts.
func operationsByDeclaration(svc ir.Service) map[string]ir.Operation {
	out := make(map[string]ir.Operation)
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			out[string(op.Provenance.Pointer)] = op
		}
	}
	return out
}

// TestLowerSecuritySchemes_KeptFieldsAndExtensionsShareOneMap pins that the two
// writers of a scheme's Unmodeled map do not overwrite each other. The x-*
// extensions used to be the only one and were assigned over the whole map, so a
// preserved field written before them would vanish without trace — the same
// silent drop this all exists to close, one layer up.
func TestLowerSecuritySchemes_KeptFieldsAndExtensionsShareOneMap(t *testing.T) {
	t.Parallel()
	doc, _, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s: {type: mutualTLS, bearerFormat: JWT, x-note: n}
`)
	s, ok := doc.Auth[ids.Auth("s")]
	require.True(t, ok)
	assert.Equal(t, []string{"openapi:bearerFormat", "openapi:x-note"}, unmodeledKeys(s.Unmodeled))
	assert.Equal(t, ir.ReasonVendorExtension, s.Unmodeled["openapi:x-note"].Reason)
	assert.Equal(t, ir.ReasonDegradedLowering, s.Unmodeled["openapi:bearerFormat"].Reason)
}

// TestLowerSecuritySchemes_AKeptFieldIsAnnounced pins that a field kept under
// Unmodeled is also reported, so a reader learns to look for it rather than
// discovering the entry by reading the IR. The report names the field and sits
// at the field's own pointer, not at the scheme that carried it.
func TestLowerSecuritySchemes_AKeptFieldIsAnnounced(t *testing.T) {
	t.Parallel()
	_, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s: {type: mutualTLS, in: header}
`)
	assert.Equal(t, []string{diag.DegradedConstruct, "openapi/validation/validation-allowed-values"},
		codesAtPointer(diags, "/components/securitySchemes/s/in"),
		"the kept field is announced exactly once, beside the loader's finding that the type does not use it: %+v", diags)
	d, ok := firstDiagAt(diags, diag.DegradedConstruct)
	require.True(t, ok)
	assert.Contains(t, d.Message, "in")
	assert.Contains(t, d.Message, "mutualTLS", "and names the type that gave it no meaning")
	assert.Equal(t, ir.SeverityInfo, d.Severity, "the field survives, so this records rather than warns")
}

// TestLowerSecuritySchemes_ARefdEntryRecordsFieldsWhereTheyAreWritten pins
// which of an aliased scheme's two positions each part of it is placed at.
//
// A `$ref` entry is a one-key object, so `<entry>/bearerFormat` names a
// position no document holds (issue #107). The fields are read from the
// declaration, so the entries recording them name it. The scheme's identity
// does not follow: alias and target are two schemes a requirement can name
// separately, and irverify holds an AuthID to agree with its provenance path,
// so both stay on the entry.
//
// One report, not two: both entries name the declaration's position, so the
// compiler's identity dedup collapses the copies.
func TestLowerSecuritySchemes_ARefdEntryRecordsFieldsWhereTheyAreWritten(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    alias: {$ref: '#/components/securitySchemes/target'}
    target: {type: mutualTLS, bearerFormat: JWT, x-note: n}
`)
	const declared = "/components/securitySchemes/target"
	for _, name := range []string{"alias", "target"} {
		s, ok := doc.Auth[ids.Auth(name)]
		require.True(t, ok, "%s interns", name)
		assert.Equal(t, jsontext.Pointer("/components/securitySchemes/"+name), s.Provenance.Pointer,
			"the scheme is named where this document names it")
		assert.Equal(t, jsontext.Pointer(declared+"/bearerFormat"),
			s.Unmodeled["openapi:bearerFormat"].Provenance.Pointer,
			"the kept field is located where it is written")
		assert.Equal(t, jsontext.Pointer(declared+"/x-note"), s.Unmodeled["openapi:x-note"].Provenance.Pointer,
			"and so is the extension beside it")
	}
	assert.Empty(t, messagesAtPointer(diags, "/components/securitySchemes/alias"),
		"nothing is reported against a position the alias does not hold: %+v", diags)
	assert.Equal(t, []string{diag.DegradedConstruct, "openapi/validation/validation-allowed-values"},
		codesAtPointer(diags, declared+"/bearerFormat"),
		"two aliases of one declaration report it once: %+v", diags)
}

// TestLowerSecuritySchemes_AnUnkeepableFieldIsReportedNotDropped pins the third
// outcome of keeping a field: one whose value JSON cannot hold reaches the IR in
// no form at all, so it is reported as the losslessness failure it is rather
// than announced as kept (GitHub #144). `.nan` is such a value, and a scheme
// declaring it is enough to reach this — the loader parses the entry, warns that
// the field is not used for the type, and hands it here intact.
func TestLowerSecuritySchemes_AnUnkeepableFieldIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s: {type: mutualTLS, name: .nan}
`)
	s, ok := doc.Auth[ids.Auth("s")]
	require.True(t, ok, "the scheme still lowers; only the one field could not be kept")
	assert.Empty(t, s.Unmodeled, "nothing was kept, so nothing claims to have been")

	d, ok := firstDiagAt(diags, diag.UnpreservableConstruct)
	require.True(t, ok, "the loss is reported: %+v", diags)
	assert.Equal(t, ir.SeverityError, d.Severity)
	require.Equal(t, []string{diag.UnpreservableConstruct, "openapi/validation/validation-allowed-values"},
		codesAtPointer(diags, "/components/securitySchemes/s/name"),
		"reported at the field exactly once, and not also announced as kept: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/securitySchemes/s/name"), d.Provenance.Pointer)
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
	assert.Equal(t, []string{diag.UnpreservableConstruct}, codesAtPointer(diags, at),
		"reported at the field once, and not also announced as kept: %+v", diags)
}

// unmodeledKeys returns u's keys sorted, so a caller pins the whole set rather
// than probing for the ones it happened to think of.
func unmodeledKeys(u ir.Unmodeled) []string {
	return slices.Sorted(maps.Keys(u))
}

// TestSecurityRequirement_OneUndeclaredMemberDropsTheWholeOption pins the
// refusal issue #14 exists for, corrected per issue #41. A requirement may name
// any string; only a name the document declares has an AuthID behind it, and
// writing one for a name it does not declare would put a reference into the IR
// that resolves to nothing. But a requirement is a conjunction — "ghost" and
// "key" must both be satisfied — so ghost failing to resolve makes the whole
// option unsatisfiable. Keeping the option with only ghost removed would leave
// behind a requirement for "key" alone, which is not what the source declared.
func TestSecurityRequirement_OneUndeclaredMemberDropsTheWholeOption(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghost: []
    key: []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	assert.Nil(t, svc.Auth,
		"the sole option named an AND of ghost+key; ghost failing to resolve drops it whole, key included")
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError),
		"the drop is reported exactly once: %+v", diags)
}

// TestSecurityRequirement_EveryUndeclaredMemberOfAnOptionIsReported pins what
// dropping an option whole must not cost: each name that failed is still named,
// so a reader fixing the document sees every scheme to declare, not only the
// first the walk tripped over.
//
// The compiled Auth cannot show this: stopping at the first bad member drops
// the same option and collapses the same list, so only the diagnostics differ.
// Both names are undeclared for that reason, and read in source order, which
// the IR promises.
func TestSecurityRequirement_EveryUndeclaredMemberOfAnOptionIsReported(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghostA: []
    ghostB: []
paths: {}
`)
	assert.Nil(t, svc.Auth, "neither name resolves, so the sole option drops and the list collapses")
	reported := messagesAt(diags, diag.UnresolvedRef)
	require.Len(t, reported, 2, "both undeclared names are reported, not just the first: %+v", diags)
	assert.Contains(t, reported[0], `"ghostA"`)
	assert.Contains(t, reported[1], `"ghostB"`, "reported in the order the source names them")
	assert.Equal(t, []string{"/security/0", "/security/0"}, sortedPointersAt(diags, diag.UnresolvedRef),
		"each names the option that declared it, which both members share")
}

// TestSecurityRequirement_ADeclaredSchemeIsNeverCalledUndeclared pins that the
// two reports about one broken scheme agree with each other. The document does
// declare "ghost" — its $ref is what fails — so the requirement naming it must
// not be told the scheme is undeclared, which contradicts the load phase's own
// report standing right beside it and sends a reader to add a declaration
// already there. The entry-level report is now the load phase's, naming the
// reference at the entry's own pointer (GitHub #385); the requirement-level one
// still says only that the name is unresolved, never that it was never
// declared.
func TestSecurityRequirement_ADeclaredSchemeIsNeverCalledUndeclared(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghost: []
paths: {}
components:
  securitySchemes:
    ghost: {$ref: '#/components/securitySchemes/Missing'}
`)
	assert.Nil(t, svc.Auth, "the sole option names a scheme that resolves to nothing")

	entry := messagesAtPointer(diags, "/components/securitySchemes/ghost")
	require.Len(t, entry, 1, "the entry whose $ref failed is reported: %+v", diags)
	assert.Contains(t, entry[0], `unresolved $ref "#/components/securitySchemes/Missing"`,
		"the load phase's own report, naming the reference that failed")

	req := messagesAtPointer(diags, "/security/0")
	require.Len(t, req, 1, "so is the requirement that names it: %+v", diags)
	assert.Contains(t, req[0], "unresolved", "which is true of a broken $ref and a typo alike")
	assert.NotContains(t, req[0], "undeclared",
		"the document declares this scheme; only its $ref is broken")
}

// TestSecurityRequirement_SoleOptionCollapsesListToNil reproduces issue #41
// directly: a document-level security list whose only option names an
// undeclared scheme must not leave behind AuthRequirement{Schemes: nil}, which
// ir-design §9 reads as "no auth is one acceptable choice" — the opposite of
// what a demanded-but-undeclared scheme means. Nor may it surface as [], which
// ir-design §9 reserves for a deliberate "explicitly public" declaration
// (Service.Auth's doc comment). The list collapses to nil instead: no usable
// default was declared, so operations fall back exactly as if security had
// never been written.
func TestSecurityRequirement_SoleOptionCollapsesListToNil(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - missing: []
paths: {}
`)
	assert.Nil(t, svc.Auth, "the only option is broken; the list is not left as [{}] or as []")
	assert.Empty(t, doc.Auth, "no scheme is declared at all")
	d, ok := firstDiagAt(diags, diag.UnresolvedRef)
	require.True(t, ok, "an unresolved-ref diagnostic: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/security/0"), d.Provenance.Pointer,
		"points at the requirement that named the missing scheme, not a nonexistent components entry")
}

// TestSecurityRequirement_PartialListDropOnlyRemovesTheBrokenOption pins that a
// broken option is dropped in isolation: surviving options keep their source
// order, and a genuinely declared empty option ("no auth is also fine") is
// untouched by the drop of an unrelated, broken option.
//
// The broken option sits at index 1 on purpose. A diagnostic that named the
// list rather than the option inside it — or that spelled every option's index
// as 0 — would send a reader to a requirement that is perfectly valid, so the
// index is load-bearing and only a non-zero one can show that it is carried.
func TestSecurityRequirement_PartialListDropOnlyRemovesTheBrokenOption(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - key: []
  - missing: []
  - {}
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	require.Len(t, svc.Auth, 2, "only the middle, broken option drops")
	require.Len(t, svc.Auth[0].Schemes, 1)
	assert.Equal(t, ids.Auth("key"), svc.Auth[0].Schemes[0].Scheme, "the first option survives in place")
	assert.Empty(t, svc.Auth[1].Schemes, "the trailing empty option still means no-auth-is-fine")
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError))
	d, ok := firstDiagAt(diags, diag.UnresolvedRef)
	require.True(t, ok, "an unresolved-ref diagnostic: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/security/1"), d.Provenance.Pointer,
		"the pointer names the broken option's own index, not the list or a constant 0")
}

// TestSecurityRequirement_OperationLevelSoleOptionCollapsesToNil is the
// operation-level counterpart of TestSecurityRequirement_SoleOptionCollapsesListToNil:
// an operation's own security override degrades the same way the service
// default does — collapsing to nil, so the operation falls back to inheriting
// the service default rather than reading as explicitly public — and its
// diagnostic points at the operation's own declaration site rather than the
// document root or a nonexistent components entry.
func TestSecurityRequirement_OperationLevelSoleOptionCollapsesToNil(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: x
      security: [{missing: []}]
      responses: {"200": {description: ok}}
`)
	require.NotEmpty(t, svc.Groups, "the operation was lowered into a group")
	require.NotEmpty(t, svc.Groups[0].Operations, "the operation was lowered")
	op := svc.Groups[0].Operations[0]
	assert.Nil(t, op.Auth, "the operation's sole option is broken; it now inherits the service default")
	d, ok := firstDiagAt(diags, diag.UnresolvedRef)
	require.True(t, ok, "an unresolved-ref diagnostic: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/paths/~1x/get/security/0"), d.Provenance.Pointer,
		"points at the operation's own requirement, not a nonexistent components entry")
}

// TestSecurityRequirement_EveryOperationCarrierDiagnosesAtItsDeclaration pins
// the base pointer for the carriers an operation-level security list sits on
// besides an inline path operation. All reach one LowerSecurityRequirements
// call, so one wrong argument misplaces every one of their diagnostics. An
// inline path operation cannot show that: it is mounted where it is declared,
// so the two pointers are equal. A $ref'd path item separates them, mounted
// under /paths and declared under /components, where the list is written. Its
// broken option sits at index 1 so the survivor is kept too.
//
// The pointers are compared as a sorted set, since carrier order is not the
// claim.
func TestSecurityRequirement_EveryOperationCarrierDiagnosesAtItsDeclaration(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /cb:
    post:
      operationId: cbOp
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          '{$request.body#/url}':
            post:
              operationId: callbackOp
              security: [{missing: []}]
              responses: {"200": {description: ok}}
  /reffed:
    $ref: '#/components/pathItems/shared'
webhooks:
  hook:
    post:
      operationId: hookOp
      security: [{missing: []}]
      responses: {"200": {description: ok}}
components:
  pathItems:
    shared:
      get:
        operationId: sharedOp
        security: [{key: []}, {missing: []}]
        responses: {"200": {description: ok}}
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	want := []string{
		"/components/pathItems/shared/get/security/1",
		"/paths/~1cb/post/callbacks/onEvent/{$request.body#~1url}/post/security/0",
		"/webhooks/hook/post/security/0",
	}
	assert.Equal(t, want, sortedPointersAt(diags, diag.UnresolvedRef),
		"each carrier reports at its own declaration site")

	// Each lookup is required to hit before it is asserted on: a missing key
	// yields the zero Operation, whose Auth is nil, so an absent carrier would
	// satisfy the nil assertions below without ever having been compiled.
	ops := operationsByDeclaration(svc)
	require.Len(t, ops, len(want)+1, "one operation per carrier, plus the callback's own parent")
	hook, ok := ops["/webhooks/hook/post"]
	require.True(t, ok, "the webhook operation was lowered")
	assert.Nil(t, hook.Auth, "the webhook's sole option is broken, so it inherits")
	callback, ok := ops["/paths/~1cb/post/callbacks/onEvent/{$request.body#~1url}/post"]
	require.True(t, ok, "the callback operation was lowered")
	assert.Nil(t, callback.Auth, "the callback operation's sole option is broken, so it inherits")
	shared, ok := ops["/components/pathItems/shared/get"]
	require.True(t, ok, "the $ref'd path item's operation was lowered")
	require.Len(t, shared.Auth, 1, "it keeps its one good option")
	require.Len(t, shared.Auth[0].Schemes, 1)
	assert.Equal(t, ids.Auth("key"), shared.Auth[0].Schemes[0].Scheme)
}

// TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne pins the difference
// between a document that says nothing about security and one that says
// explicitly that none is required.
//
// ir.Service.Auth and ir.Operation.Auth carry no omitempty, so the IR spells the
// two apart — null for "inherit whatever encloses this", [] for "no auth is an
// acceptable option" (ir-design §9). Collapsing them makes an explicitly public
// operation read as one that never mentioned security, which is the opposite
// claim.
func TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx

	absent, absentDiags := auth.LowerSecurityRequirements(c, nil, "")
	assert.Nil(t, absent, "no security key at all inherits the enclosing default")
	assert.Empty(t, absentDiags)

	empty, emptyDiags := auth.LowerSecurityRequirements(c, []*soa.SecurityRequirement{}, "")
	assert.NotNil(t, empty, "an empty list is a declaration, not an absence")
	assert.Empty(t, empty, "and it declares no options")
	assert.Empty(t, emptyDiags)
}

// TestSecurityRequirement_ANonObjectEntryIsNotTheEmptyOption pins that a
// security-list entry not written as an object is dropped whole rather than
// lowered to AuthRequirement{}, which ir-design §9 reserves for "no auth is one
// acceptable choice" and which, beside a real requirement, would make auth
// optional (GitHub #284).
//
// The `{}` an author writes on purpose is a row too: refusing every memberless
// option would pass the malformed rows while deleting that encoding.
//
// Each malformed row pins that the loader's report survives, since the drop is
// otherwise silent. The aliased rows catch a drop that tests the entry's node
// kind, which would lose the `{}` reached through an alias.
func TestSecurityRequirement_ANonObjectEntryIsNotTheEmptyOption(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
		// wantOptions is how many of the two declared options survive.
		wantOptions int
	}{
		{name: "null", entry: `null`, wantOptions: 1},
		{name: "YAML's other spelling of null", entry: `~`, wantOptions: 1},
		{name: "a string", entry: `"key"`, wantOptions: 1},
		{name: "a number", entry: `42`, wantOptions: 1},
		{name: "a sequence", entry: `[key]`, wantOptions: 1},
		{name: "an alias to a scalar", entry: `*scalar`, wantOptions: 1},
		{name: "an option the document really wrote as {}", entry: `{}`, wantOptions: 2},
		{name: "an option reached through an alias to {}", entry: `*empty`, wantOptions: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-defs:
  empty: &empty {}
  scalar: &scalar just a string
security:
  - key: []
  - `+tc.entry+`
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
			require.Len(t, svc.Auth, tc.wantOptions, "surviving options: %+v", svc.Auth)
			require.Len(t, svc.Auth[0].Schemes, 1, "the real requirement is untouched")
			assert.Equal(t, ids.Auth("key"), svc.Auth[0].Schemes[0].Scheme)

			if tc.wantOptions == 2 {
				assert.Empty(t, svc.Auth[1].Schemes, "the empty option the document wrote is kept")
				assert.Empty(t, loaderErrors(diags), "a document with nothing wrong with it: %+v", diags)
				return
			}
			assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), "openapi.security.1",
				"the entry is dropped here without a report, so the loader's must place it: %+v", diags)
		})
	}
}

// TestSecurityRequirement_OperationLevelNonObjectEntryIsNotTheEmptyOption is the
// operation-level counterpart of TestSecurityRequirement_ANonObjectEntryIsNotTheEmptyOption.
// Both levels reach one lowering, but only a test at each proves it: a
// document-level fixture alone passes just as well on a fix wired into the
// service walk and never reached by an operation's own override.
func TestSecurityRequirement_OperationLevelNonObjectEntryIsNotTheEmptyOption(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		entry       string
		wantOptions int
	}{
		{name: "null", entry: `null`, wantOptions: 1},
		{name: "an option the operation really wrote as {}", entry: `{}`, wantOptions: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: x
      security:
        - key: []
        - `+tc.entry+`
      responses: {"200": {description: ok}}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
			op := onlyOperation(t, svc)
			require.Len(t, op.Auth, tc.wantOptions, "surviving options: %+v", op.Auth)
			require.Len(t, op.Auth[0].Schemes, 1, "the real requirement is untouched")
			assert.Equal(t, ids.Auth("key"), op.Auth[0].Schemes[0].Scheme)

			if tc.wantOptions == 2 {
				assert.Empty(t, op.Auth[1].Schemes, "the empty option the operation wrote is kept")
				return
			}
			assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), "operation.security.1",
				"the entry is dropped here without a report, so the loader's must place it: %+v", diags)
		})
	}
}

// TestSecurityRequirement_ASoleNonObjectEntryCollapsesTheListToNil pins the
// solo spelling at both levels. It is the worse half of #284: `security: [null]`
// lowered to [{}], which ir-design §9 reads as an explicit "no auth is one
// acceptable choice" — a document that says nothing usable about security
// instead declaring the API public. Dropping the entry leaves an originally
// non-empty list empty, which the enclosing collapse turns into nil, so the
// carrier inherits exactly as if security had never been written.
func TestSecurityRequirement_ASoleNonObjectEntryCollapsesTheListToNil(t *testing.T) {
	t.Parallel()
	_, svc, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security: [null]
paths:
  /x:
    get:
      operationId: x
      security: [null]
      responses: {"200": {description: ok}}
`)
	assert.Nil(t, svc.Auth, "the document's sole option is not an option; the list is not [{}] or []")
	assert.Nil(t, onlyOperation(t, svc).Auth, "nor is the operation's own override")
}

// onlyOperation returns svc's single operation, failing if it has any other
// number — an assertion the callers need before reading Auth, because a missing
// operation yields the zero value, whose Auth is nil and would satisfy a nil
// assertion without anything having been compiled.
func onlyOperation(t *testing.T, svc ir.Service) ir.Operation {
	t.Helper()
	var ops []ir.Operation
	for _, g := range svc.Groups {
		ops = append(ops, g.Operations...)
	}
	require.Len(t, ops, 1, "the fixture declares exactly one operation")
	return ops[0]
}

// loaderErrors returns the message of every error the load phase's validation
// produced, in report order. It matches on the code prefix every such finding
// carries rather than on one rule, so a test asserts that the entry was placed
// without pinning which library rule placed it.
func loaderErrors(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == ir.SeverityError && strings.HasPrefix(d.Code, diag.Validation+"/") {
			out = append(out, d.Message)
		}
	}
	return out
}

// TestLowerSecuritySchemes_RefSiteDocsOverrideTheDeclaration pins the auth half
// of GitHub #610: a securitySchemes entry written as a Reference Object keeps
// the description written beside its $ref, and the declaration it names keeps
// its own — the fold is per entry, never a mutation of the target.
func TestLowerSecuritySchemes_RefSiteDocsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    BaseKey:
      description: declared
      type: apiKey
      in: header
      name: X-Key
    ApiKey:
      $ref: '#/components/securitySchemes/BaseKey'
      description: the use site
`)
	require.Len(t, doc.Auth, 2)
	byName := map[string]string{}
	for _, scheme := range doc.Auth {
		byName[scheme.Name.Source] = scheme.Docs.Description
	}
	assert.Equal(t, "the use site", byName["ApiKey"])
	assert.Equal(t, "declared", byName["BaseKey"],
		"the aliasing entry's siblings do not reach the declaration")
	require.Empty(t, messagesAt(diags, diag.UnresolvedRef))
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
		if d.Code == diag.Validation+"/"+validation.RuleValidationRequiredField {
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
