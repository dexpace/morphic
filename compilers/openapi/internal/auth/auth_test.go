package auth_test

import (
	"slices"
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
func TestLowerSecuritySchemes_NothingLoweredIsNilNotEmpty(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Components: &soa.Components{
		SecuritySchemes: sequencedmap.New(
			sequencedmap.NewElem("ghost", (*soa.ReferencedSecurityScheme)(nil))),
	}}

	got, diags := auth.LowerSecuritySchemes(lowering.Ctx{Doc: doc})

	assert.Nil(t, got, "an unresolvable entry leaves no map behind")
	assert.Empty(t, diags)
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
