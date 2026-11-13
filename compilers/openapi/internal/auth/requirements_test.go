// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	soa "github.com/speakeasy-api/openapi/openapi"
	soacore "github.com/speakeasy-api/openapi/openapi/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
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

// TestSecurityRequirement_AScalarTheModelDoesNotDecodeIsAScope pins that a scope
// is the text a list writes, whatever tag it is written under. The model decodes
// no item under a tag outside YAML's JSON schema ruleset
// (docs/parser-library.md#undecoded-scalar), and would lose every scope beside it
// with nothing reported. A plain date, which the load phase tags a string, is a
// scope too.
func TestSecurityRequirement_AScalarTheModelDoesNotDecodeIsAScope(t *testing.T) {
	t.Parallel()
	for written, want := range map[string][]string{
		"[read, 2021-01-01]":    {"read", "2021-01-01"},
		"[!!binary aGk=, read]": {"aGk=", "read"},
		"[read, !local tagged]": {"read", "tagged"},
		"[*date, read]":         {"2021-01-01", "read"},
		"*dates":                {"2021-01-01"},
	} {
		t.Run(written, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-date: &date 2021-01-01
x-dates: &dates [2021-01-01]
security:
  - key: `+written+`
paths: {}
components:
  securitySchemes:
    key: {type: oauth2, flows: {implicit: {authorizationUrl: 'https://a', scopes: {read: r}}}}
`)
			assert.Empty(t, diags)
			assert.Equal(t, []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ids.Auth("key"), Scopes: want}}}}, svc.Auth)
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

// TestSecurityRequirement_AnOptionKeepsItsIndexPastANonObjectEntry pins that a
// report names the position the source writes. An entry that is no object lowers
// to no option, so what follows it lands one place earlier in the IR and stays
// where it is in the source.
func TestSecurityRequirement_AnOptionKeepsItsIndexPastANonObjectEntry(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - null
  - missing: []
paths: {}
`)
	require.Len(t, svc.Auth, 1, "the entry that is no object lowers to no option")
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("missing")}, schemeUses(svc.Auth[0]))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedSecurityScheme, "/security/1"),
		"reported where the source writes it: %+v", diags)
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
		ID: ids.UnresolvedAuth("missing"), Name: ir.NamingFor("missing"), Kind: ir.AuthKindUnresolved,
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

// uriSchemes declares the schemes the URI cases name: key with a mechanism,
// nomech naming none, and two entries whose names are themselves URIs naming
// another entry, one of them naming no mechanism.
const uriSchemes = `components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
    nomech: {type: http}
    "#/components/securitySchemes/named": {type: apiKey, in: header, name: X-Named}
    named: {type: apiKey, in: query, name: named}
    "#/components/securitySchemes/unnamed": {type: http}
    unnamed: {type: apiKey, in: query, name: unnamed}
`

// TestSecurityRequirement_A32KeyMayNameItsSchemeByURI is the reproduction of
// issue #1212. From 3.2 a key that is no component name may be the URI of the
// scheme, read as a reference against the document holding the requirement:
// one naming an entry of the source's securitySchemes is that entry, declared
// or not, exactly as if the key had been its name, and is reported where the
// name would be, quoting the key written, as a scope it repeats is reported
// under the key. A component name comes first, and any other key is the name it
// spells.
func TestSecurityRequirement_A32KeyMayNameItsSchemeByURI(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  string
		want ir.AuthID
	}{
		{"#/components/securitySchemes/key", ids.Auth("key")},
		{"spec.yaml#/components/securitySchemes/key", ids.Auth("key")},
		{"#/components/securitySchemes/k%65y", ids.Auth("key")},
		{"#/components/securitySchemes/named", ids.Auth("#/components/securitySchemes/named")},
		{"#/components/securitySchemes/unnamed", ids.UnresolvedAuth("#/components/securitySchemes/unnamed")},
		{"#/components/securitySchemes/nomech", ids.UnresolvedAuth("nomech")},
		{"#/components/securitySchemes/ghost", ids.UnresolvedAuth("ghost")},
		{"#/components/schemas/key", ids.UnresolvedAuth("#/components/schemas/key")},
		{"#/components/securitySchemes/key/x", ids.UnresolvedAuth("#/components/securitySchemes/key/x")},
		{"other.yaml#/components/securitySchemes/key", ids.UnresolvedAuth("other.yaml#/components/securitySchemes/key")},
		{"key#", ids.UnresolvedAuth("key#")},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			doc, svc, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
security: [{"`+tc.key+`": [read, read]}]
paths: {}
`+uriSchemes)
			require.Len(t, svc.Auth, 1)
			assert.Equal(t, []ir.SchemeUse{{Scheme: tc.want, Scopes: []string{"read"}}}, svc.Auth[0].Schemes)
			assert.Equal(t, []jsontext.Pointer{"/security/0" + ids.Ptr(tc.key, "1")},
				openapitest.PointersOf(diags, diag.RepeatedEntry), "a repeat is where the key writes it")
			assert.Contains(t, doc.Auth, tc.want)
			entries := []ir.AuthID{ids.Auth("key"), ids.UnresolvedAuth("nomech"),
				ids.Auth("#/components/securitySchemes/named"), ids.Auth("named"),
				ids.UnresolvedAuth("#/components/securitySchemes/unnamed"), ids.Auth("unnamed")}
			for id := range doc.Auth {
				if !slices.Contains(entries, id) {
					assert.Equal(t, tc.want, id, "the entries, and no scheme but the one the key names")
				}
			}

			reported := messagesAt(diags, diag.UnresolvedSecurityScheme)
			if !strings.HasPrefix(string(tc.want), "auth/unresolved/") {
				assert.Empty(t, reported)
				return
			}
			require.Len(t, reported, 1, "%+v", diags)
			assert.Contains(t, reported[0], fmt.Sprintf("unresolved scheme %q", tc.key), "the key as written")
		})
	}
}

// TestSecurityRequirement_A31KeyIsAName pins that 3.0 and 3.1 read a key as a
// name alone: neither defines the URI form, so a key spelling one names no
// entry and is reported, with the text a name draws.
func TestSecurityRequirement_A31KeyIsAName(t *testing.T) {
	t.Parallel()
	const uri = "#/components/securitySchemes/key"
	for _, version := range []string{"3.0.3", "3.1.1"} {
		_, svc, diags := serviceSpec(t, `openapi: `+version+`
info: {title: T, version: "1"}
security: [{"`+uri+`": []}]
paths: {}
`+uriSchemes)
		require.Len(t, svc.Auth, 1, version)
		assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth(uri)}, schemeUses(svc.Auth[0]), version)
		assert.Equal(t, []string{`security requirement references unresolved scheme "` + uri +
			`": the requirement is kept, and the scheme has no mechanism`},
			messagesAt(diags, diag.UnresolvedSecurityScheme), version)
	}
}

// TestSecurityRequirement_A32UnresolvedNameSaysWhatIsNotRead pins the clause a
// 3.2 document's error adds: a URI naming a scheme outside the source's
// securitySchemes is not followed, so the reader is not told only that the
// scheme has no mechanism.
func TestSecurityRequirement_A32UnresolvedNameSaysWhatIsNotRead(t *testing.T) {
	t.Parallel()
	_, _, diags := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
security: [{"#/x-schemes/extra": []}]
paths: {}
x-schemes:
  extra: {type: http, scheme: bearer}
`)
	assert.Equal(t, []string{`security requirement references unresolved scheme "#/x-schemes/extra": ` +
		`the requirement is kept, and the scheme has no mechanism; a URI naming a Security Scheme Object ` +
		`outside the source's components/securitySchemes is not read`},
		messagesAt(diags, diag.UnresolvedSecurityScheme))
}

// TestSecurityRequirement_AURIIsReadInTheDocumentHoldingIt pins the rows and the
// base a key is read with when the requirement is in another document a path
// mounts: that document's minor decides whether the key may be a URI, and a
// fragment alone names that document, not the source, so it is not followed.
// The source's file named whole is the source in either.
func TestSecurityRequirement_AURIIsReadInTheDocumentHoldingIt(t *testing.T) {
	t.Parallel()
	const keys = `[{"root.yaml#/components/securitySchemes/key": []}, {"#/components/securitySchemes/key": []}]`
	other := func(version string) string {
		return `openapi: ` + version + `
info: {title: O, version: "1"}
paths:
  /there:
    get:
      operationId: there
      security: ` + keys + `
      responses: {"200": {description: ok}}
components:
  securitySchemes:
    key: {type: http, scheme: basic}
`
	}
	ghostFragment := ids.UnresolvedAuth("#/components/securitySchemes/key")
	for _, tc := range []struct {
		source, other string
		want          []ir.AuthID
	}{
		{"3.1.1", "3.2.0", []ir.AuthID{ids.Auth("key"), ghostFragment}},
		{"3.2.0", "3.1.1", []ir.AuthID{ids.UnresolvedAuth("root.yaml#/components/securitySchemes/key"), ghostFragment}},
	} {
		doc, diags := compileBeside(t, `openapi: `+tc.source+`
info: {title: T, version: "1"}
paths:
  /there: {$ref: 'other.yaml#/paths/~1there'}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`, other(tc.other))
		op := openapitest.FindOp(t, doc, "there")
		require.Len(t, op.Auth, 2, "%s mounting %s: %+v", tc.source, tc.other, diags)
		assert.Equal(t, tc.want, []ir.AuthID{op.Auth[0].Schemes[0].Scheme, op.Auth[1].Schemes[0].Scheme},
			"%s mounting %s", tc.source, tc.other)
	}
}

// compileBeside compiles root as root.yaml beside other as other.yaml, read
// from memory with external references allowed.
func compileBeside(t *testing.T, root, other string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	memory, err := reader.NewMemory([]compilers.Source{{Path: "other.yaml", Data: []byte(other)}})
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "root.yaml", Data: []byte(root)}},
		compilers.Options{Reader: memory, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	require.Len(t, doc.Sources, 2, "the root and other.yaml: %+v", diags)
	return doc, diags
}

// TestSecurityRequirement_ANameAndItsURIAreOneSchemeInEitherOrder pins that the
// name a URI yields depends on the key and the document alone: one operation
// writing an undeclared name and another its URI reach one scheme, and the
// documents and their diagnostics are equal whichever is declared first, bar
// what the permutation moves by design: the source's hash, and the order of the
// operations it groups and of the diagnostics, compared as a multiset.
func TestSecurityRequirement_ANameAndItsURIAreOneSchemeInEitherOrder(t *testing.T) {
	t.Parallel()
	const (
		byName = `  /byName:
    get:
      operationId: byName
      security: [{ghost: []}]
      responses: {"200": {description: ok}}
`
		byURI = `  /byURI:
    get:
      operationId: byURI
      security: [{"#/components/securitySchemes/ghost": []}]
      responses: {"200": {description: ok}}
`
		head = "openapi: 3.2.0\ninfo: {title: T, version: \"1\"}\npaths:\n"
	)
	nameFirst, _, nameFirstDiags := serviceSpec(t, head+byName+byURI)
	uriFirst, _, uriFirstDiags := serviceSpec(t, head+byURI+byName)

	for order, doc := range map[string]*ir.Document{"name first": nameFirst, "URI first": uriFirst} {
		assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("ghost")}, slices.Collect(maps.Keys(doc.Auth)), order)
		for _, name := range []string{"byName", "byURI"} {
			op := openapitest.FindOp(t, doc, name)
			require.Len(t, op.Auth, 1, name)
			assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("ghost")}, schemeUses(op.Auth[0]), "%s: %s", order, name)
		}
	}
	assert.Empty(t, cmp.Diff(nameFirst, uriFirst, cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"),
		cmpopts.IgnoreFields(ir.Provenance{}, "Position"), cmpopts.IgnoreFields(ir.Document{}, "Diagnostics"),
		cmpopts.SortSlices(func(a, b ir.Operation) bool { return a.ID < b.ID })))
	assert.ElementsMatch(t, reported(nameFirstDiags), reported(uriFirstDiags))
}

// reported renders each diagnostic as its code, severity, pointer and message,
// so two orders of one document compare on what was reported and where.
func reported(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %v %s %s", d.Code, d.Severity, d.Provenance.Pointer, d.Message))
	}
	return out
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
	c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
	reg := auth.RegistryOf(c, nil)

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
	c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
	reg := auth.RegistryOf(c, nil)
	got, diags := reg.Requirements(c, []*soa.SecurityRequirement{nil}, "/security")

	assert.Empty(t, got, "a nil entry is no requirement, so it is dropped")
	assert.Empty(t, diags)
}

// mergeCarriers is a document writing one security list at every position one
// can be written: the root, an inline operation, a callback's, an operation of
// a path item a path mounts by $ref, and a webhook's. LIST is the list.
const mergeCarriers = `openapi: 3.1.0
info: {title: T, version: "1"}
x-defs:
  s: &s 5
  e: &e {}
  bad: &bad {<<: 5}
security: LIST
paths:
  /x:
    get:
      operationId: inline
      security: LIST
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          '{$request.body#/url}':
            post:
              operationId: callback
              security: LIST
              responses: {"200": {description: ok}}
  /mounted:
    $ref: '#/components/pathItems/Mounted'
webhooks:
  hook:
    post:
      operationId: hook
      security: LIST
      responses: {"200": {description: ok}}
components:
  pathItems:
    Mounted:
      get:
        operationId: mounted
        security: LIST
        responses: {"200": {description: ok}}
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`

// carrierAuth returns the security each carrier of mergeCarriers lowered to,
// keyed by its operationId, the root's under "root".
func carrierAuth(t *testing.T, doc *ir.Document) map[string][]ir.AuthRequirement {
	t.Helper()
	out := map[string][]ir.AuthRequirement{"root": doc.Services[0].Auth}
	for _, name := range []string{"inline", "callback", "mounted", "hook"} {
		out[name] = openapitest.FindOp(t, doc, name).Auth
	}
	return out
}

// TestSecurityRequirement_AMergeNamingNoMappingIsNoOption is the reproduction of
// issue #1244. An entry whose `<<` names nothing to merge reads as {}, and read
// as the option meaning "no auth is one acceptable choice", it grants anonymous
// access the document never wrote. It names no scheme either, so it lowers to no
// option, at every carrier and in every spelling of the merge, while a merge of
// a mapping, an empty one included, is what it merges. The loader's error at
// the fault is the one report of a dropped entry: at the entry, or where an
// alias it merges is written.
func TestSecurityRequirement_AMergeNamingNoMappingIsNoOption(t *testing.T) {
	t.Parallel()
	key := ir.AuthRequirement{Schemes: []ir.SchemeUse{{Scheme: ids.Auth("key")}}}
	none := []ir.AuthRequirement{key}
	empty := []ir.AuthRequirement{key, {}}
	cases := []struct {
		entry string
		want  []ir.AuthRequirement
		// fault is where the loader reports the merge, for an entry dropped.
		fault string
	}{
		{`{<<: 5}`, none, "/security/1"}, {`{<<: null}`, none, "/security/1"},
		{`{<<: [5]}`, none, "/security/1"}, {`{<<: [{}, 5]}`, none, "/security/1"},
		{`{<<: {<<: 5}}`, none, "/security/1"}, {`{!!merge <<: 5}`, none, "/security/1"},
		{`{!!merge "<<": 5}`, none, "/security/1"}, {`{<<: *s}`, none, "/security/1"},
		{`{<<: [*e, *s]}`, none, "/security/1"}, {`*bad`, none, "/x-defs/bad"}, {`{<<: *bad}`, none, "/x-defs/bad"},
		{`{<<: {}}`, empty, ""}, {`{<<: &empty {}}`, empty, ""}, {`{<<: *e}`, empty, ""}, {`{<<: []}`, empty, ""},
		{`{<<: 5, key: []}`, []ir.AuthRequirement{key, key}, ""},
		// JSON has no merge key: the quoted key is a name, which reaches no
		// scheme, and the option stands.
		{`{"<<": 5}`, []ir.AuthRequirement{key, {Schemes: []ir.SchemeUse{{Scheme: ids.UnresolvedAuth("<<")}}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, strings.ReplaceAll(mergeCarriers, "LIST", "[{key: []}, "+tc.entry+"]"))
			for carrier, got := range carrierAuth(t, doc) {
				assert.Empty(t, cmp.Diff(tc.want, got, cmpopts.EquateEmpty()), carrier)
			}
			if tc.fault != "" {
				assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), tc.fault+": a `<<` merge key",
					"the merge naming nothing is reported: %+v", diags)
			}
		})
	}
}

// TestSecurityRequirement_AJSONDocumentWritesNoMerge pins #1244's spelling in a
// JSON document, at the root and on an operation: JSON has no merge key, so
// `"<<"` is a name like any other, which reaches no scheme and keeps its
// option, reported, rather than reading as the empty one.
func TestSecurityRequirement_AJSONDocumentWritesNoMerge(t *testing.T) {
	t.Parallel()
	const list = `[{"<<": 5}, {"<<": {}}]`
	doc, svc, diags := serviceSpec(t, `{"openapi": "3.1.0", "info": {"title": "T", "version": "1"},
  "security": `+list+`,
  "paths": {"/x": {"get": {"operationId": "x", "security": `+list+`,
    "responses": {"200": {"description": "ok"}}}}}}`)
	want := []ir.AuthRequirement{
		{Schemes: []ir.SchemeUse{{Scheme: ids.UnresolvedAuth("<<")}}},
		{Schemes: []ir.SchemeUse{{Scheme: ids.UnresolvedAuth("<<")}}},
	}
	assert.Empty(t, cmp.Diff(want, svc.Auth), "the document")
	assert.Empty(t, cmp.Diff(want, onlyOperation(t, svc).Auth), "the operation")
	assert.Contains(t, doc.Auth, ids.UnresolvedAuth("<<"))
	assert.Equal(t, []string{"/paths/~1x/get/security/0", "/paths/~1x/get/security/1", "/security/0", "/security/1"},
		sortedPointersAt(diags, diag.UnresolvedSecurityScheme))
}

// TestSecurityRequirement_ASoleMergeNamingNoMappingCollapsesTheListToNil pins
// the worse half of #1244 at every carrier: read as [{}], `security: [{<<: 5}]`
// is public to anyone who accepts the error. Its one entry names no option, so
// the list says nothing about auth and collapses to nil, where the loader
// reports the merge at the entry.
func TestSecurityRequirement_ASoleMergeNamingNoMappingCollapsesTheListToNil(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, strings.ReplaceAll(mergeCarriers, "LIST", "[{<<: 5}]"))
	for carrier, got := range carrierAuth(t, doc) {
		assert.Nil(t, got, carrier)
	}
	reported := strings.Join(loaderErrors(diags), "\n")
	for _, at := range []string{"/security/0: ", "/paths/~1x/get/security/0: ", "/webhooks/hook/post/security/0: ",
		"/components/pathItems/Mounted/get/security/0: ",
		"/paths/~1x/get/callbacks/onEvent/{$request.body#~1url}/post/security/0: "} {
		assert.Contains(t, reported, at, "the loader places the merge at the entry")
	}
}

// TestSecurityRequirement_AMergeChainIsReadToItsEnd pins that an entry merging
// through a chain longer than nodeview.MergeDepthLimit is read as the parser
// reads it: empty mappings all the way down write {}, and a `<<` naming no
// mapping at the end names no option.
func TestSecurityRequirement_AMergeChainIsReadToItsEnd(t *testing.T) {
	t.Parallel()
	chain := func(leaf string) string {
		for range nodeview.MergeDepthLimit + 6 {
			leaf = "{<<: " + leaf + "}"
		}
		return leaf
	}
	key := ir.AuthRequirement{Schemes: []ir.SchemeUse{{Scheme: ids.Auth("key")}}}
	for leaf, want := range map[string][]ir.AuthRequirement{
		"{}": {key, {}}, "{<<: 5}": {key},
	} {
		_, svc, _ := serviceSpec(t, strings.ReplaceAll(mergeCarriers, "LIST", "[{key: []}, "+chain(leaf)+"]"))
		assert.Empty(t, cmp.Diff(want, svc.Auth, cmpopts.EquateEmpty()), leaf)
	}
}

// TestRegistryRequirements_AnEntryWhoseRootIsNoMappingIsNoOption covers the
// guard a parsed document cannot reach: a requirement whose root node is no
// mapping. The marshaller records a root only for a mapping, so only a
// hand-built requirement holds one, and it is no option rather than {}.
func TestRegistryRequirements_AnEntryWhoseRootIsNoMappingIsNoOption(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
	reg := auth.RegistryOf(c, nil)
	req := soa.NewSecurityRequirement()
	core := soacore.NewSecurityRequirement()
	core.SetRootNode(&yaml.Node{Kind: yaml.ScalarNode, Value: "5"})
	req.SetCore(core)

	got, diags := reg.Requirements(c, []*soa.SecurityRequirement{req}, "")
	assert.Nil(t, got, "no option, so the list collapses")
	assert.Empty(t, diags)
}

// TestSecurityRequirement_ARepeatedScopeIsKeptOnce pins issue #738's fold of
// SchemeUse.Scopes: the scopes a scheme must hold are a set, so a repeat is
// kept at its first occurrence and reported where it is written, naming the
// first, under the name as the pointer escapes it. Each scheme's list folds on
// its own, at the document and on an operation alike, and scopes compare as
// written.
func TestSecurityRequirement_ARepeatedScopeIsKeptOnce(t *testing.T) {
	t.Parallel()
	const list = `[{o: [read, read, write, Read, read]}, {o: [read], "a/b": [read, read]}]`
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security: `+list+`
paths:
  /x:
    get:
      operationId: x
      security: `+list+`
      responses: {"200": {description: ok}}
components:
  securitySchemes:
    o: {type: oauth2, flows: {clientCredentials: {tokenUrl: 'https://x/t', scopes: {read: r, write: w, Read: R}}}}
    a/b: {type: oauth2, flows: {clientCredentials: {tokenUrl: 'https://x/t', scopes: {read: r}}}}
`)
	want := []ir.AuthRequirement{
		{Schemes: []ir.SchemeUse{{Scheme: ids.Auth("o"), Scopes: []string{"read", "write", "Read"}}}},
		{Schemes: []ir.SchemeUse{{Scheme: ids.Auth("o"), Scopes: []string{"read"}}, {Scheme: ids.Auth("a/b"), Scopes: []string{"read"}}}},
	}
	assert.Empty(t, cmp.Diff(want, svc.Auth), "the document")
	assert.Empty(t, cmp.Diff(want, onlyOperation(t, svc).Auth), "the operation")

	var got []string
	for _, d := range diags {
		if d.Code == diag.RepeatedEntry {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			got = append(got, string(d.Provenance.Pointer)+": "+d.Message)
		}
	}
	slices.Sort(got)
	var wantDiags []string
	for _, base := range []string{"", "/paths/~1x/get"} {
		wantDiags = append(wantDiags,
			base+`/security/0/o/1: scope "read" of scheme "o" repeats the entry at index 0; it is kept once`,
			base+`/security/0/o/4: scope "read" of scheme "o" repeats the entry at index 0; it is kept once`,
			base+`/security/1/a~1b/1: scope "read" of scheme "a/b" repeats the entry at index 0; it is kept once`)
	}
	slices.Sort(wantDiags)
	assert.Equal(t, wantDiags, got)
}

// TestSecurityRequirement_AnUnresolvedNameNamesADeclaredNearMiss is the
// reproduction of issue #694: a name matched exactly and reaching no scheme is
// often a declared one in another case or with a typo, and the error names it.
// Resolution stays exact, so the requirement names the unresolved scheme all the
// same. An entry that reaches no mechanism is no hint, a name with no near miss
// draws the plain text, and in a 3.2 document the hint comes before the URI
// clause.
func TestSecurityRequirement_AnUnresolvedNameNamesADeclaredNearMiss(t *testing.T) {
	t.Parallel()
	const plain = "the requirement is kept, and the scheme has no mechanism"
	cases := []struct {
		version, key, want string
	}{
		{"3.1.1", "ApiKey", `"ApiKey": ` + plain + `; a scheme named "apiKey" is declared`},
		{"3.1.1", "apiKye", `"apiKye": ` + plain + `; a scheme named "apiKey" is declared`},
		{"3.1.1", "basicAuth", `"basicAuth": ` + plain},
		{"3.1.1", "oauth", `"oauth": ` + plain},
		{"3.2.0", "#/components/securitySchemes/api_key", `"#/components/securitySchemes/api_key": ` + plain +
			`; a scheme named "apiKey" is declared; a URI naming a Security Scheme Object outside the source's ` +
			`components/securitySchemes is not read`},
	}
	for _, tc := range cases {
		t.Run(tc.version+" "+tc.key, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := serviceSpec(t, `openapi: `+tc.version+`
info: {title: T, version: "1"}
security: [{"`+tc.key+`": []}]
paths: {}
components:
  securitySchemes:
    apiKey: {type: apiKey, in: header, name: X-Key}
    basic_auth: {type: http}
`)
			require.Len(t, svc.Auth, 1)
			assert.Equal(t, []string{"security requirement references unresolved scheme " + tc.want},
				messagesAt(diags, diag.UnresolvedSecurityScheme))
		})
	}
}
