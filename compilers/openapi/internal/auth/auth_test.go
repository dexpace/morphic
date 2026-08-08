package auth_test

import (
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
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
				assert.Equal(t, "r", s.Flows[0].Scopes["read"])
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
			requireNoErrorDiags(t, diags)
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
	requireNoErrorDiags(t, diags)

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
	assert.True(t, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning) > 0,
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
	kinds := indexBy(oauth.Flows, func(f ir.OAuthFlow) string { return f.Kind })
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
	spec := pathsSpec(`  /x:
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
// no $ref to have failed. TestLowerSecuritySchemes_OnlyABrokenRefIsSitedHere
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

// TestLowerSecuritySchemes_OnlyABrokenRefIsSitedHere pins which unresolvable
// entries this package reports and which it leaves alone, through the compiler
// rather than a hand-built node — the shapes below are what a document can
// actually write, and a hand-built one is not among them.
//
// Every case here drops the entry from the registry, and none is named by any
// security requirement, so nothing downstream would report it either. What
// separates them is whether anything else already places the fault. A $ref that
// resolves to nothing is reported by the load phase at no pointer at all
// (issue #235), leaving the entry unplaced — that is the gap this package
// fills. An entry written as something other than an object already draws the
// loader's type-mismatch, which names both the entry and what was wrong with
// it, so a second report would send the reader to the same place to learn less.
func TestLowerSecuritySchemes_OnlyABrokenRefIsSitedHere(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
		// wantRef is the reference the report must name, or "" when this
		// package is expected to report nothing at all.
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
				assert.Empty(t, got,
					"the loader already names this entry and its fault: %+v", diags)
				return
			}
			require.Len(t, got, 1, "the entry is placed exactly once: %+v", diags)
			assert.Contains(t, got[0], `"ghost"`, "the report names the entry")
			assert.Contains(t, got[0], tc.wantRef, "and the reference that failed")
		})
	}
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
			assert.Equal(t, "/components/securitySchemes/ghost", d.Provenance.Pointer,
				"reported at the entry, not at the requirement that names it")
			assert.Contains(t, d.Message, `"ghost"`, "the report names the entry")
		})
	}
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
// a securityScheme entry declares is dropped. Each mechanism's lowering reads
// only the fields its own type defines, so everything else the entry wrote —
// `in` on an oauth2 scheme, `flows` on an apiKey — reached no IR field and no
// Unmodeled entry either, which "lossless by default" forbids (GitHub #294).
//
// Every case declares all seven mechanism fields, so each row states both
// halves of §12.1's one-home rule at once: what the type defines reaches its IR
// field and is not also kept raw, and what it does not define is kept raw and
// does not silently fill a field of another mechanism.
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
				assert.Equal(t, "/components/securitySchemes/s/"+field, s.Unmodeled[key].Provenance.Pointer,
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

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}

// countDiagsAt counts the diagnostics matching code and sev exactly.
func countDiagsAt(diags []ir.Diagnostic, code string, sev ir.Severity) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			n++
		}
	}
	return n
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
		if d.Provenance.Pointer == pointer {
			out = append(out, d.Message)
		}
	}
	return out
}

// sortedPointersAt returns every provenance pointer carried by a diagnostic
// with code, sorted so a caller pins the set rather than the walk's order.
func sortedPointersAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d.Provenance.Pointer)
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
			out[op.Provenance.Pointer] = op
		}
	}
	return out
}

// indexBy builds a lookup keyed by key(item).
func indexBy[T any, K comparable](items []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		out[key(item)] = item
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
	got := messagesAtPointer(diags, "/components/securitySchemes/s/in")
	require.Len(t, got, 1, "the kept field is announced exactly once: %+v", diags)
	assert.Contains(t, got[0], "in")
	assert.Contains(t, got[0], "mutualTLS", "and names the type that gave it no meaning")
	d, ok := firstDiagAt(diags, diag.DegradedConstruct)
	require.True(t, ok)
	assert.Equal(t, ir.SeverityInfo, d.Severity, "the field survives, so this records rather than warns")
}

// TestLowerSecuritySchemes_ARefdEntryRecordsFieldsWhereTheyAreWritten pins
// which of an aliased scheme's two positions each part of it is placed at.
//
// A `$ref` entry is a one-key object, so `<entry>/bearerFormat` names a position
// no document holds — the fabricated-pointer failure issue #107 settled for
// every other referenced component. The fields are read from the declaration,
// so the entries recording them name the declaration.
//
// The scheme's own identity does not follow: an alias and its target are two
// named schemes a requirement can name separately, and irverify holds an AuthID
// to agreeing with its provenance path, so both stay on the entry.
//
// One report, not two: with both aliases naming the same position the compiler's
// identity dedup collapses them, which is what it is for — before this the two
// copies differed only in a pointer neither document position had.
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
		assert.Equal(t, "/components/securitySchemes/"+name, s.Provenance.Pointer,
			"the scheme is named where this document names it")
		assert.Equal(t, declared+"/bearerFormat",
			s.Unmodeled["openapi:bearerFormat"].Provenance.Pointer,
			"the kept field is located where it is written")
		assert.Equal(t, declared+"/x-note", s.Unmodeled["openapi:x-note"].Provenance.Pointer,
			"and so is the extension beside it")
	}
	assert.Empty(t, messagesAtPointer(diags, "/components/securitySchemes/alias"),
		"nothing is reported against a position the alias does not hold: %+v", diags)
	assert.Len(t, messagesAtPointer(diags, declared+"/bearerFormat"), 1,
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
	require.Len(t, messagesAtPointer(diags, "/components/securitySchemes/s/name"), 1,
		"reported at the field exactly once, and not also announced as kept: %+v", diags)
	assert.Equal(t, "/components/securitySchemes/s/name", d.Provenance.Pointer)
}

// unmodeledKeys returns u's keys sorted, so a caller pins the whole set rather
// than probing for the ones it happened to think of.
func unmodeledKeys(u ir.Unmodeled) []string {
	out := make([]string, 0, len(u))
	for k := range u {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// pathsSpec wraps a paths block in a minimal 3.1 document with no components.
func pathsSpec(paths string) string {
	return "openapi: 3.1.0\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths:\n" + paths
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
	assert.Equal(t, 1, countDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError),
		"the drop is reported exactly once: %+v", diags)
}

// TestSecurityRequirement_EveryUndeclaredMemberOfAnOptionIsReported pins the one
// thing dropping the option whole must not cost. The option is refused once, but
// each name that failed is still named — so a reader fixing the document sees
// every scheme it has to declare, not only the first one the walk tripped over.
//
// Nothing about the compiled Auth can see this: stopping at the first bad member
// drops exactly the same option and collapses exactly the same list, so only the
// diagnostics separate the two. Both names are undeclared here for that reason,
// and they are read in source order, which is the order the IR promises.
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
// not be told the scheme is undeclared, which contradicts the entry-level report
// standing right beside it and sends a reader to add a declaration already
// there. Both now say the name resolves to nothing, which is true whether the
// document wrote the entry or not.
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
	assert.Contains(t, entry[0], `"ghost"`, "naming the scheme, so it stands without its pointer")
	assert.Contains(t, entry[0], "resolves to nothing")

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
	assert.Equal(t, "/security/0", d.Provenance.Pointer,
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
	assert.Equal(t, 1, countDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError))
	d, ok := firstDiagAt(diags, diag.UnresolvedRef)
	require.True(t, ok, "an unresolved-ref diagnostic: %+v", diags)
	assert.Equal(t, "/security/1", d.Provenance.Pointer,
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
	assert.Equal(t, "/paths/~1x/get/security/0", d.Provenance.Pointer,
		"points at the operation's own requirement, not a nonexistent components entry")
}

// TestSecurityRequirement_EveryOperationCarrierDiagnosesAtItsDeclaration pins
// the base pointer for the carriers an operation-level security list sits on
// besides an inline path operation. All of them reach one LowerSecurityRequirements
// call, so a single wrong argument there misplaces every one of their
// diagnostics at once — and an inline path operation cannot show that, because
// it is mounted exactly where it is declared, leaving the two pointers equal. A
// $ref'd path item separates them: it is mounted under /paths and declared under
// /components, and only the declaration addresses a node the security list is
// written at. Its broken option sits at index 1 so the survivor is kept too.
//
// The pointers are compared as a sorted set: the claim is that each carrier
// reports at its own declaration, not that the walk visits them in some order.
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
