// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// schemeUses returns the scheme each member of an option names, in source order.
func schemeUses(option ir.AuthRequirement) []ir.AuthID {
	out := make([]ir.AuthID, 0, len(option.Schemes))
	for _, use := range option.Schemes {
		out = append(out, use.Scheme)
	}
	return out
}

// TestSecurityRequirement_AnUnresolvedMemberKeepsItsOption pins that an option
// lowers whole. A requirement is a conjunction, so leaving ghost out would
// demand key alone, and dropping the option would leave no requirement at all.
// The undeclared member comes first on purpose: source order is what the IR
// promises, and a lowering that moved it would pass with it last.
func TestSecurityRequirement_AnUnresolvedMemberKeepsItsOption(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghost: [read, write]
    key: []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	require.Len(t, svc.Auth, 1, "the option stays")
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("ghost"), ids.Auth("key")}, schemeUses(svc.Auth[0]),
		"both members, in source order")
	assert.Equal(t, []string{"read", "write"}, svc.Auth[0].Schemes[0].Scopes, "the scopes it lists stay on it")

	scheme, ok := doc.Auth[ids.UnresolvedAuth("ghost")]
	require.True(t, ok, "the name lowers to a scheme: %+v", doc.Auth)
	assert.Equal(t, ir.AuthKindUnresolved, scheme.Kind)
	assert.Equal(t, "ghost", scheme.Name.Source)

	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedSecurityScheme, ir.SeverityError),
		"reported as an error, once: %+v", diags)
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError),
		"a requirement is no $ref, and nothing it names is lost")
}

// TestSecurityRequirement_AMemberWhoseScopesDoNotReadStaysInItsOption pins that
// an option keeps every name it writes, whatever is beside the name. The library's
// model leaves such a member out (docs/parser-library.md#requirement-scopes), which
// would turn `- key:` into the empty option, no auth needed, and an AND into less
// than the source demands.
func TestSecurityRequirement_AMemberWhoseScopesDoNotReadStaysInItsOption(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "~", "null", "5", "read", "{a: b}", "[null]", "[read, {}]"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			doc, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - key: `+value+`
  - ghost: `+value+`
    other: [read]
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
    other: {type: oauth2, flows: {implicit: {authorizationUrl: 'https://a', scopes: {read: r}}}}
`)
			require.Len(t, svc.Auth, 2, "%+v", diags)
			assert.Equal(t, []ir.SchemeUse{{Scheme: ids.Auth("key")}}, svc.Auth[0].Schemes, "no option reads as no auth")
			assert.Equal(t, []ir.SchemeUse{
				{Scheme: ids.UnresolvedAuth("ghost")},
				{Scheme: ids.Auth("other"), Scopes: []string{"read"}},
			}, svc.Auth[1].Schemes, "both members of the AND, in source order")
			assert.Contains(t, doc.Auth, ids.UnresolvedAuth("ghost"))
			assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedSecurityScheme, ir.SeverityError), "%+v", diags)
		})
	}
}

// TestSecurityRequirement_EveryUnresolvedMemberOfAnOptionIsReported pins that a
// reader fixing the document sees every scheme to declare, not only the first
// the walk reached, read in the source order the IR promises.
func TestSecurityRequirement_EveryUnresolvedMemberOfAnOptionIsReported(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghostA: []
    ghostB: []
paths: {}
`)
	require.Len(t, svc.Auth, 1)
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("ghostA"), ids.UnresolvedAuth("ghostB")}, schemeUses(svc.Auth[0]))

	reported := messagesAt(diags, diag.UnresolvedSecurityScheme)
	require.Len(t, reported, 2, "both names are reported, not just the first: %+v", diags)
	assert.Equal(t, `security requirement references unresolved scheme "ghostA": `+
		`the requirement is kept, and the scheme has no mechanism`, reported[0])
	assert.Contains(t, reported[1], `"ghostB"`, "reported in the order the source names them")
	assert.Equal(t, []string{"/security/0", "/security/0"}, sortedPointersAt(diags, diag.UnresolvedSecurityScheme),
		"each names the option that declared it, which both members share")
}

// TestSecurityRequirement_ADeclaredSchemeIsNeverCalledUndeclared pins that the
// two reports about one broken scheme agree. The document does declare "ghost",
// and only its $ref fails, so telling the requirement the scheme is undeclared
// would contradict the load phase's report beside it and send a reader to add a
// declaration that is already there.
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
	require.Len(t, svc.Auth, 1, "the requirement stands, though the scheme it names resolves to nothing")
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("ghost")}, schemeUses(svc.Auth[0]))

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

// TestSecurityRequirement_ASoleUnresolvedOptionIsNeitherEmptyNorAbsent pins the
// two readings a demanded credential must not take at the document level: the
// empty option and [] each say no auth is needed (issue #41), and an absent list
// says none was declared, which every operation would inherit as no requirement.
func TestSecurityRequirement_ASoleUnresolvedOptionIsNeitherEmptyNorAbsent(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - missing: []
paths: {}
`)
	require.Len(t, svc.Auth, 1, "the list stands")
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("missing")}, schemeUses(svc.Auth[0]))
	assert.Len(t, doc.Auth, 1, "the name is the one scheme the document holds")

	d, ok := openapitest.FirstDiag(diags, diag.UnresolvedSecurityScheme)
	require.True(t, ok, "reported: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/security/0"), d.Provenance.Pointer,
		"points at the requirement that named the missing scheme, not a nonexistent components entry")
}

// TestSecurityRequirement_AnUnresolvedOptionKeepsItsPlaceInTheList pins that
// every option stays, in priority order, and that a declared empty option keeps
// its meaning beside one naming nothing. An option at index 1 shows the
// diagnostic carries its own index rather than the list's or a constant 0.
func TestSecurityRequirement_AnUnresolvedOptionKeepsItsPlaceInTheList(t *testing.T) {
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
	require.Len(t, svc.Auth, 3, "no option drops")
	assert.Equal(t, []ir.AuthID{ids.Auth("key")}, schemeUses(svc.Auth[0]))
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("missing")}, schemeUses(svc.Auth[1]), "in its own place")
	assert.Empty(t, svc.Auth[2].Schemes, "the trailing empty option still means no-auth-is-fine")
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedSecurityScheme, ir.SeverityError))
	d, ok := openapitest.FirstDiag(diags, diag.UnresolvedSecurityScheme)
	require.True(t, ok, "reported: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/security/1"), d.Provenance.Pointer,
		"the pointer names the option's own index, not the list or a constant 0")
}

// TestSecurityRequirement_AnOperationsOwnUnresolvedListDoesNotInherit is the
// reproduction of issue #633: an operation stating its own requirement must not
// read as inheriting the service's, whose credentials an SDK would then send.
func TestSecurityRequirement_AnOperationsOwnUnresolvedListDoesNotInherit(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security: [{key: []}]
paths:
  /x:
    get:
      operationId: x
      security: [{missing: []}]
      responses: {"200": {description: ok}}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	op := onlyOperation(t, svc)
	require.Len(t, op.Auth, 1, "the operation keeps its own requirement")
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("missing")}, schemeUses(op.Auth[0]))
	d, ok := openapitest.FirstDiag(diags, diag.UnresolvedSecurityScheme)
	require.True(t, ok, "reported: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/paths/~1x/get/security/0"), d.Provenance.Pointer,
		"points at the operation's own requirement, not a nonexistent components entry")
}

// TestSecurityRequirement_ANameIsOneScheme pins that a name recurring across
// requirements lowers to one scheme, whichever of them comes first, and that a
// scheme nothing declares records no pointer: no position writes it, and a
// pointer under components would name something the document does not hold.
func TestSecurityRequirement_ANameIsOneScheme(t *testing.T) {
	t.Parallel()
	doc, svc, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security: [{missing: []}]
paths:
  /x:
    get:
      operationId: x
      security: [{missing: [read]}]
      responses: {"200": {description: ok}}
`)
	assert.Equal(t, svc.Auth[0].Schemes[0].Scheme, onlyOperation(t, svc).Auth[0].Schemes[0].Scheme)
	assert.Empty(t, cmp.Diff(map[ir.AuthID]ir.AuthScheme{ids.UnresolvedAuth("missing"): {
		ID: ids.UnresolvedAuth("missing"), Name: compile.NamingFor("missing"), Kind: ir.AuthKindUnresolved,
	}}, doc.Auth))
}

// TestSecurityRequirement_ANameIsMatchedExactly pins that a resolved name is
// spelled as the document spells it and that the match does not fold case or
// separators: guessing api_token for APIToken would send a credential the
// document did not name.
func TestSecurityRequirement_ANameIsMatchedExactly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
		want ir.AuthID
	}{
		{name: "another spelling of a declared name", key: `APIToken`, want: "auth/unresolved/components/securitySchemes/APIToken"},
		{name: "the empty name", key: `""`, want: "auth/unresolved/components/securitySchemes/"},
		{name: "a name a pointer must escape", key: `"a/b~c"`, want: "auth/unresolved/components/securitySchemes/a~1b~0c"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, svc, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - `+tc.key+`: []
paths: {}
components:
  securitySchemes:
    api_token: {type: apiKey, in: header, name: X-Key}
`)
			require.Len(t, svc.Auth, 1)
			assert.Equal(t, []ir.AuthID{tc.want}, schemeUses(svc.Auth[0]))
			assert.Contains(t, doc.Auth, tc.want)
		})
	}
}

// TestSecurityRequirement_A32URIIsReadAsAName pins a limitation: 3.2 lets a key
// be a scheme's URI, which is not read yet (#1212), so a document that fully
// describes the scheme lowers as unresolved and still fails its run, and the
// finding says so. A warning here would be a request an SDK refuses and no
// failed run to show for it.
func TestSecurityRequirement_A32URIIsReadAsAName(t *testing.T) {
	t.Parallel()
	const uri = "#/components/securitySchemes/key"
	_, svc, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
security:
  - "`+uri+`": []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	require.Len(t, svc.Auth, 1)
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth(uri)}, schemeUses(svc.Auth[0]))
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedSecurityScheme, ir.SeverityError))
}

// TestSecurityRequirement_EveryOperationCarrierDiagnosesAtItsDeclaration pins
// the base pointer and the lowering for the carriers an operation-level list
// sits on besides an inline path operation. All reach one lowering, so one wrong
// argument misplaces every diagnostic. A $ref'd path item separates the two
// pointers: mounted under /paths, declared under /components, where the list is
// written. The pointers compare as a sorted set, since carrier order is no claim.
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
	assert.Equal(t, want, sortedPointersAt(diags, diag.UnresolvedSecurityScheme),
		"each carrier reports at its own declaration site")

	// Each lookup is required to hit before it is asserted on: a missing key
	// yields the zero Operation, whose Auth is nil.
	ops := operationsByDeclaration(svc)
	require.Len(t, ops, len(want)+1, "one operation per carrier, plus the callback's own parent")
	hook, ok := ops["/webhooks/hook/post"]
	require.True(t, ok, "the webhook operation was lowered")
	require.Len(t, hook.Auth, 1, "the webhook keeps its own requirement")
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("missing")}, schemeUses(hook.Auth[0]))
	callback, ok := ops["/paths/~1cb/post/callbacks/onEvent/{$request.body#~1url}/post"]
	require.True(t, ok, "the callback operation was lowered")
	require.Len(t, callback.Auth, 1, "the callback operation keeps its own requirement")
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("missing")}, schemeUses(callback.Auth[0]))
	shared, ok := ops["/components/pathItems/shared/get"]
	require.True(t, ok, "the $ref'd path item's operation was lowered")
	require.Len(t, shared.Auth, 2, "it keeps both options, in order")
	assert.Equal(t, []ir.AuthID{ids.Auth("key")}, schemeUses(shared.Auth[0]))
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("missing")}, schemeUses(shared.Auth[1]))
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

// TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne pins the difference
// between a document that says nothing about security and one that says
// explicitly that none is required.
//
// ir.Service.Auth and ir.Operation.Auth are omitzero, so the IR spells the two
// apart — an absent key for "inherit whatever encloses this", [] for
// "explicitly public" (docs/ir/auth.md). Collapsing them makes an explicitly
// public operation read as one that never mentioned security, which is the
// opposite claim.
func TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx
	reg, _ := auth.LowerSecuritySchemes(lowering.Ctx{Doc: &soa.OpenAPI{}})

	absent, absentDiags := reg.Requirements(c, nil, "")
	assert.Nil(t, absent, "no security key at all inherits the enclosing default")
	assert.Empty(t, absentDiags)

	empty, emptyDiags := reg.Requirements(c, []*soa.SecurityRequirement{}, "")
	assert.NotNil(t, empty, "an empty list is a declaration, not an absence")
	assert.Empty(t, empty, "and it declares no options")
	assert.Empty(t, emptyDiags)
}

// TestSecurityRequirement_ANonObjectEntryIsNotTheEmptyOption pins that a
// security-list entry not written as an object is dropped whole rather than
// lowered to AuthRequirement{}, which docs/ir/auth.md reserves for "no auth is
// one acceptable choice" and which, beside a real requirement, would make auth
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
			assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), "/security/1",
				"the entry is dropped here without a report, so the loader's must place it, or name where an "+
					"alias reads it: %+v", diags)
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
			assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), "/paths/~1x/get/security/1: ",
				"the entry is dropped here without a report, so the loader's must place it: %+v", diags)
		})
	}
}

// TestSecurityRequirement_ASoleNonObjectEntryCollapsesTheListToNil pins the
// solo spelling at both levels. It is the worse half of #284: `security: [null]`
// lowered to [{}], which docs/ir/auth.md reads as an explicit "no auth is one
// acceptable choice". The entry names nothing to keep, so the list collapses to
// nil, the one spelling that says nothing about auth, beside the loader's error.
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

// TestSecurityRequirement_ASecurityThatIsNoListReadsAsLeftOut pins the limit
// Requirements states: the library reads such a value as nil, so the carrier
// inherits, and the rule table's error is what keeps a run from reading that as
// the document's meaning.
func TestSecurityRequirement_ASecurityThatIsNoListReadsAsLeftOut(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"{key: []}", "{}", "5", "key"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: x
      security: `+value+`
      responses: {"200": {description: ok}}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
			assert.Nil(t, onlyOperation(t, svc).Auth)
			assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), "/paths/~1x/get/security: ", "%+v", diags)
		})
	}
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

// loaderErrors returns every error the load phase's validation produced, in
// report order, as its pointer and message. It matches on the code prefix every
// such finding carries rather than on one rule, so a test asserts where the
// entry was placed without pinning which rule placed it.
func loaderErrors(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == ir.SeverityError && strings.HasPrefix(d.Code, diag.Validation+"/") {
			out = append(out, string(d.Provenance.Pointer)+": "+d.Message)
		}
	}
	return out
}

// TestRegistryRequirements_DropsANilEntrySilently covers the half of the
// object guard a parsed document cannot reach: a hand-built list holding a nil
// entry. It is dropped like any other entry that is no object, rather than
// surviving as the empty option that reads as "no auth is one acceptable
// choice", and silently: an entry that is no object is the loader's to report,
// and a nil one is in no document at all.
func TestRegistryRequirements_DropsANilEntrySilently(t *testing.T) {
	t.Parallel()
	reg, _ := auth.LowerSecuritySchemes(lowering.Ctx{Doc: &soa.OpenAPI{}})
	got, diags := reg.Requirements(lowering.Ctx{}, []*soa.SecurityRequirement{nil}, "/security")

	assert.Empty(t, got, "a nil entry is no requirement, so it is dropped")
	assert.Empty(t, diags)
}
