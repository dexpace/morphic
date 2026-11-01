// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth_test

import (
	"encoding/json/jsontext"
	"reflect"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestLowerSecuritySchemes_ANilEntryLowersAndReportsNothing pins the guard for a
// hand-built nil entry, which no parsed document produces: a malformed entry
// still arrives as an object. It carries no $ref that could have failed, so
// nothing is reported, and its name is held like any other entry's.
// TestLowerSecuritySchemes_ABrokenRefIsReportedByLoadOnce covers the shapes a
// document can write, through the compiler.
func TestLowerSecuritySchemes_ANilEntryLowersAndReportsNothing(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Components: &soa.Components{
		SecuritySchemes: sequencedmap.New(
			sequencedmap.NewElem("ghost", (*soa.ReferencedSecurityScheme)(nil))),
	}}

	got, diags := auth.LowerSecuritySchemes(lowering.Ctx{Doc: doc})

	assert.Contains(t, got.Schemes(), ids.UnresolvedAuth("ghost"))
	assert.Empty(t, diags, "a nil entry names no reference that could have failed")
}

// TestLowerSecuritySchemes_ABrokenRefIsReportedByLoadOnce pins how each
// unresolvable entry below is reported, through the compiler rather than a
// hand-built node: a document can write these shapes. None is named by a
// requirement, so nothing downstream reports them. Each still lowers to a scheme
// at its own pointer, which holds its name and no mechanism.
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
			assert.NotContains(t, doc.Auth, ids.Auth("ghost"), "it takes no ID a reference can spell")
			assert.Contains(t, doc.Auth, ids.Auth("key"), "its resolvable sibling keeps its own")
			scheme, ok := doc.Auth[ids.UnresolvedAuth("ghost")]
			require.True(t, ok, "the entry's name is held: %+v", doc.Auth)
			assert.Equal(t, ir.AuthKindUnresolved, scheme.Kind)
			assert.Equal(t, jsontext.Pointer("/components/securitySchemes/ghost"), scheme.Provenance.Pointer,
				"at the entry that wrote it")

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
	assert.Nil(t, got.Schemes(), "an empty Auth map would be a field the source never wrote")
	assert.Empty(t, diags)

	got, diags = auth.LowerSecuritySchemes(lowering.Ctx{Doc: &soa.OpenAPI{Components: &soa.Components{}}})
	assert.Nil(t, got.Schemes())
	assert.Empty(t, diags)
}

// TestLowerSecuritySchemes_AnEntryNamingNoMechanismLowersAsUnresolved pins the
// shapes that declare a scheme without saying what it is: an entry with no
// `type`, and an http entry with no `scheme` token. Both once interned a custom
// scheme whose mechanism was the empty string, which states that the API is
// authenticated by nothing in particular (GitHub #294). Each is reported at the
// entry and takes the kind that says the compiler holds no mechanism.
func TestLowerSecuritySchemes_AnEntryNamingNoMechanismLowersAsUnresolved(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		entry   string
		missing string
	}{
		{name: "no type at all", entry: `{}`, missing: "type"},
		{name: "no type, but fields an apiKey would use", entry: `{in: header, name: X-Key}`, missing: "type"},
		{name: "an explicitly empty type", entry: `{type: ""}`, missing: "type"},
		{name: "http with no scheme token", entry: `{type: http}`, missing: "scheme"},
		{name: "http with an empty scheme token", entry: `{type: http, scheme: ""}`, missing: "scheme"},
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
			assert.NotContains(t, doc.Auth, ids.Auth("ghost"), "it takes no ID a reference can spell")
			assert.Contains(t, doc.Auth, ids.Auth("key"), "its complete sibling keeps its own")
			scheme, ok := doc.Auth[ids.UnresolvedAuth("ghost")]
			require.True(t, ok, "the entry's name is held: %+v", doc.Auth)
			assert.Equal(t, ir.AuthKindUnresolved, scheme.Kind)
			assert.Equal(t, jsontext.Pointer("/components/securitySchemes/ghost"), scheme.Provenance.Pointer)

			d, ok := openapitest.FirstDiag(diags, diag.IncompleteSecurityScheme)
			require.True(t, ok, "the entry is reported: %+v", diags)
			assert.Equal(t, ir.SeverityError, d.Severity)
			assert.Equal(t, jsontext.Pointer("/components/securitySchemes/ghost"), d.Provenance.Pointer,
				"reported at the entry, not at the requirement that names it")
			assert.Equal(t, `security scheme "ghost" has no `+tc.missing+`, so it names no authentication `+
				`mechanism: it is kept without one, and every requirement naming it is kept`, d.Message,
				"the message states what became of the entry and of the requirements")
		})
	}
}

// TestLowerSecuritySchemes_ARequirementNamingAnEntryWithNoMechanismKeepsIt pins
// the downstream half: the requirement stands, naming the scheme the entry
// lowered to rather than one minted over it, and is reported like any other that
// resolved to no mechanism.
func TestLowerSecuritySchemes_ARequirementNamingAnEntryWithNoMechanismKeepsIt(t *testing.T) {
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
	require.Len(t, svc.Auth, 1)
	assert.Equal(t, []ir.AuthID{ids.UnresolvedAuth("ghost")}, schemeUses(svc.Auth[0]))
	assert.Equal(t, []string{"/security/0"}, sortedPointersAt(diags, diag.UnresolvedSecurityScheme))
	assert.Equal(t, []string{"openapi:in", "openapi:name"},
		unmodeledKeys(doc.Auth[ids.UnresolvedAuth("ghost")].Unmodeled), "what the entry wrote is still there")
}

// TestLowerSecuritySchemes_AnEntryNamingNoMechanismKeepsWhatItWrote pins what
// #294 asked to survive. Each row writes what the entry's type would give a
// home, had it named a mechanism, and no mechanism field fills: those are what
// the kind denies, so they stay under Unmodeled. The http row is what separates
// keeping everything from keeping what the type leaves unread.
func TestLowerSecuritySchemes_AnEntryNamingNoMechanismKeepsWhatItWrote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
		want  []string
	}{
		{
			name:  "no type",
			entry: `{in: header, name: X-Key, flows: {implicit: {scopes: {}}}, x-note: n, bogus: b}`,
			want:  []string{"openapi:bogus", "openapi:flows", "openapi:in", "openapi:name", "openapi:x-note"},
		},
		{
			name:  "http with no scheme",
			entry: `{type: http, bearerFormat: JWT}`,
			want:  []string{"openapi:bearerFormat", "openapi:type"},
		},
		{name: "a type written empty", entry: `{type: ""}`, want: []string{"openapi:type"}},
		{name: "a type written null", entry: `{type: null, in: header}`, want: []string{"openapi:in", "openapi:type"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: `+tc.entry+`
`)
			scheme, ok := doc.Auth[ids.UnresolvedAuth("ghost")]
			require.True(t, ok, "%+v", doc.Auth)
			assert.Equal(t, tc.want, unmodeledKeys(scheme.Unmodeled))
			assert.Empty(t, scheme.BearerFormat+scheme.In+scheme.KeyName, "no mechanism field fills")
		})
	}
}

// TestLowerSecuritySchemes_AKeptTypeIsTheEntrysOwn pins what a kept type holds:
// the value written, where it is written, and for an alias the declaration's
// position, so `http` survives the kind that cannot say it.
func TestLowerSecuritySchemes_AKeptTypeIsTheEntrysOwn(t *testing.T) {
	t.Parallel()
	doc, _, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    alias: {$ref: '#/components/securitySchemes/ghost'}
    ghost: {type: http}
`)
	for _, name := range []string{"alias", "ghost"} {
		scheme, ok := doc.Auth[ids.UnresolvedAuth(name)]
		require.True(t, ok, "%+v", doc.Auth)
		kept := scheme.Unmodeled["openapi:type"]
		assert.JSONEq(t, `"http"`, string(kept.Value), name)
		assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason, name)
		assert.Equal(t, jsontext.Pointer("/components/securitySchemes/ghost/type"), kept.Provenance.Pointer, name)
	}
}

// TestLowerSecuritySchemes_AnEntryNamingNoMechanismKeepsItsDocs pins the text a
// caller writing the credential by hand needs, and that the entry's deprecation
// survives with it. Deprecation is a 3.2 field.
func TestLowerSecuritySchemes_AnEntryNamingNoMechanismKeepsItsDocs(t *testing.T) {
	t.Parallel()
	doc, _, _ := serviceSpec(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: {description: Issued by the upload endpoint., deprecated: true}
`)
	scheme, ok := doc.Auth[ids.UnresolvedAuth("ghost")]
	require.True(t, ok, "%+v", doc.Auth)
	assert.Equal(t, "Issued by the upload endpoint.", scheme.Docs.Description)
	assert.NotNil(t, scheme.Deprecation)
}

// TestLowerSecuritySchemes_AnAliasOfAnEntryNamingNoMechanismIsItsOwnScheme pins
// that aliases stay separate schemes here as they do for a scheme with a
// mechanism, each at its own entry, and that the text the target wrote reaches
// both. Unmodeled keeps pointing at the one place it is written.
func TestLowerSecuritySchemes_AnAliasOfAnEntryNamingNoMechanismIsItsOwnScheme(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    alias: {$ref: '#/components/securitySchemes/ghost'}
    ghost: {description: Issued by the upload endpoint., in: header}
`)
	assert.Empty(t, irverify.Verify(doc))
	alias, ok := doc.Auth[ids.UnresolvedAuth("alias")]
	require.True(t, ok, "%+v", doc.Auth)
	assert.Equal(t, jsontext.Pointer("/components/securitySchemes/alias"), alias.Provenance.Pointer)
	assert.Equal(t, "Issued by the upload endpoint.", alias.Docs.Description)
	assert.Equal(t, jsontext.Pointer("/components/securitySchemes/ghost/in"),
		alias.Unmodeled["openapi:in"].Provenance.Pointer)
	assert.Contains(t, doc.Auth, ids.UnresolvedAuth("ghost"))
	assert.Equal(t, []string{"/components/securitySchemes/alias", "/components/securitySchemes/ghost"},
		sortedPointersAt(diags, diag.IncompleteSecurityScheme), "each is reported at its own entry")
}

// TestLowerSecuritySchemes_AKeptFieldOfNoMechanismBlamesNoType pins the wording
// of a field an entry with no mechanism keeps: `http` does define bearerFormat,
// so the report other kinds draw, that the type does not define it, would be
// false here, and for an entry with no type it would name the empty string.
func TestLowerSecuritySchemes_AKeptFieldOfNoMechanismBlamesNoType(t *testing.T) {
	t.Parallel()
	_, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: {type: http, bearerFormat: JWT}
`)
	assert.Equal(t, []string{"security scheme bearerFormat belongs to no mechanism the entry names; " +
		"kept verbatim under Unmodeled"}, messagesAtPointer(diags, "/components/securitySchemes/ghost/bearerFormat"))
}

// TestLowerSecuritySchemes_ABrokenRefKeepsTheDocsBesideIt pins that a description
// a Reference Object writes survives the $ref it stands beside (GitHub #610).
func TestLowerSecuritySchemes_ABrokenRefKeepsTheDocsBesideIt(t *testing.T) {
	t.Parallel()
	doc, _, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: {$ref: '#/components/securitySchemes/Missing', description: Issued by the upload endpoint.}
`)
	scheme, ok := doc.Auth[ids.UnresolvedAuth("ghost")]
	require.True(t, ok, "%+v", doc.Auth)
	assert.Equal(t, "Issued by the upload endpoint.", scheme.Docs.Description)
}

// TestLowerSecuritySchemes_ABrokenRefIsKept pins that an entry whose $ref reaches
// nothing keeps that $ref, the one thing it says of where its mechanism is, with
// no second report beside the error it already drew. An entry that is no object
// has no $ref to keep.
func TestLowerSecuritySchemes_ABrokenRefIsKept(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: {$ref: './auth.yaml#/components/securitySchemes/Missing'}
    scalar: 5
`)
	ghost := doc.Auth[ids.UnresolvedAuth("ghost")].Unmodeled["openapi:$ref"]
	assert.JSONEq(t, `"./auth.yaml#/components/securitySchemes/Missing"`, string(ghost.Value))
	assert.Equal(t, ir.ReasonDegradedLowering, ghost.Reason)
	const at = "/components/securitySchemes/ghost/$ref"
	assert.Equal(t, jsontext.Pointer(at), ghost.Provenance.Pointer)
	assert.Empty(t, messagesAtPointer(diags, at))

	scalar, ok := doc.Auth[ids.UnresolvedAuth("scalar")]
	require.True(t, ok, "%+v", doc.Auth)
	assert.Empty(t, scalar.Unmodeled)
}

// TestLowerSecuritySchemes_ABrokenRefThatCannotBeKeptIsReported pins the third
// outcome of keeping a $ref: one JSON cannot hold reaches the IR in no form, which
// its own error says beside the one the reference draws.
func TestLowerSecuritySchemes_ABrokenRefThatCannotBeKeptIsReported(t *testing.T) {
	t.Parallel()
	doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: {$ref: .nan}
`)
	assert.Empty(t, doc.Auth[ids.UnresolvedAuth("ghost")].Unmodeled, "nothing was kept, so nothing claims to have been")

	d, ok := openapitest.FirstDiag(diags, diag.UnpreservableConstruct)
	require.True(t, ok, "the loss is reported: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/securitySchemes/ghost/$ref"), d.Provenance.Pointer)
}

// TestLowerSecuritySchemes_ARefTheModelDoesNotDecodeIsReported pins that no scheme
// of kind unresolved goes unreported, with no requirement naming it to draw an
// error instead. The model holds no reference for such a $ref
// (docs/parser-library.md#undecoded-scalar), so the load phase meets none and this
// report is the only one.
func TestLowerSecuritySchemes_ARefTheModelDoesNotDecodeIsReported(t *testing.T) {
	t.Parallel()
	for written, text := range map[string]string{
		"2021-01-01":    "2021-01-01",
		"!!binary aGk=": "aGk=",
		"!local tagged": "tagged",
		"*date":         "2021-01-01",
	} {
		t.Run(written, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-date: &date 2021-01-01
paths: {}
components:
  securitySchemes:
    ghost: {$ref: `+written+`}
`)
			scheme, ok := doc.Auth[ids.UnresolvedAuth("ghost")]
			require.True(t, ok, "%+v", doc.Auth)
			assert.Contains(t, scheme.Unmodeled, "openapi:$ref")

			got := messagesAtPointer(diags, "/components/securitySchemes/ghost")
			require.Len(t, got, 1, "%+v", diags)
			assert.Equal(t, `security scheme "ghost" has a $ref that resolves to nothing: "`+text+`"`, got[0])
			assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError), "%+v", diags)
		})
	}
}

// TestLowerSecuritySchemes_ARefHoldingNoTextDrawsOneReport pins the other side of
// reading the node: a $ref written as no scalar is the rule table's to report, and
// an empty one the load phase's, so this package adds neither a second one.
func TestLowerSecuritySchemes_ARefHoldingNoTextDrawsOneReport(t *testing.T) {
	t.Parallel()
	for written, want := range map[string]string{
		"{a: b}": "openapi/validation/validation-type-mismatch",
		"[a]":    "openapi/validation/validation-type-mismatch",
		`""`:     diag.UnresolvedRef,
	} {
		t.Run(written, func(t *testing.T) {
			t.Parallel()
			doc, _, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    ghost: {$ref: `+written+`}
`)
			require.Contains(t, doc.Auth, ids.UnresolvedAuth("ghost"))
			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, want, diags[0].Code)
		})
	}
}

// TestLowerSecuritySchemes_FieldsTheTypeDoesNotDefineSurvive pins that no field
// a securityScheme entry declares is dropped: `in` on an oauth2 scheme or
// `flows` on an apiKey must reach Unmodeled, as "lossless by default" requires
// (GitHub #294).
//
// Every case declares every mechanism field, so each row states both
// halves of docs/compilers/openapi.md#security-scheme-fields: what the type
// defines reaches its IR field and is not also kept raw, and what it does not
// define is kept raw and does not fill a field of another mechanism.
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
	assert.Equal(t, []string{diag.DegradedConstruct}, codesAtPointer(diags, "/components/securitySchemes/s/in"),
		"the kept field is announced exactly once, the one report of a field OpenAPI bars from no type: %+v", diags)
	d, ok := openapitest.FirstDiag(diags, diag.DegradedConstruct)
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
	assert.Equal(t, []string{diag.DegradedConstruct}, codesAtPointer(diags, declared+"/bearerFormat"),
		"two aliases of one declaration report it once: %+v", diags)
}

// TestLowerSecuritySchemes_AnUnkeepableFieldIsReportedNotDropped pins the third
// outcome of keeping a field: one whose value JSON cannot hold reaches the IR in
// no form at all, so it is reported as the losslessness failure it is rather
// than announced as kept (GitHub #144). `.nan` is such a value, and a scheme
// declaring it is enough to reach this: the loader parses the entry, warns that
// a number stands where a string belongs, and hands it here intact.
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

	d, ok := openapitest.FirstDiag(diags, diag.UnpreservableConstruct)
	require.True(t, ok, "the loss is reported: %+v", diags)
	assert.Equal(t, ir.SeverityError, d.Severity)
	require.Equal(t, []string{diag.UnpreservableConstruct, "openapi/validation/validation-type-mismatch"},
		codesAtPointer(diags, "/components/securitySchemes/s/name"),
		"the loss reported at the field exactly once, beside the loader's typing warning, and not also "+
			"announced as kept: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/securitySchemes/s/name"), d.Provenance.Pointer)
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

// TestAuth_UnserializableExtensionStillWarns pins that a security scheme whose
// sole x-* extension fails to serialize still surfaces the warning, although
// nothing reaches the scheme's Unmodeled: a warning gated on there being an
// extension to keep would be dropped in exactly this case.
func TestAuth_UnserializableExtensionStillWarns(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    s: {type: apiKey, in: header, name: X-Key, x-bad: {n: .nan}}
`
	doc, _, diags := serviceSpec(t, spec)
	assert.Positive(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"an entirely unserializable extension still warns even though the scheme's own Unmodeled ends up empty")
	scheme, ok := doc.Auth[ids.Auth("s")]
	require.True(t, ok)
	assert.Empty(t, scheme.Unmodeled, "the unserializable extension is dropped, not stored")
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
	assert.Equal(t, want, auth.MechanismFieldNames(),
		"mechanismFieldNames must list every per-type field, sorted")
}
