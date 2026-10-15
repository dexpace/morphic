package load

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// versions is one release of each minor the compiler reads.
var versions = []string{"3.0.3", "3.1.1", "3.2.0"}

// libraryFindings returns what the library's own unmarshal and validation of
// src raise under rule, as "line:column message" lines.
func libraryFindings(t *testing.T, src, rule string) []string {
	t.Helper()
	_, errs := parsedModel(t, src)
	var out []string
	for _, e := range errs {
		if verr, ok := asValidationError(e); ok && verr.Rule == rule {
			out = append(out, fmt.Sprintf("%d:%d %s", verr.GetLineNumber(), verr.GetColumnNumber(), validationMessage(verr)))
		}
	}
	slices.Sort(out)
	return out
}

// loadedFindings returns what Load reports of src under rule, in the form
// libraryFindings uses. A finding about another document is placed at a $ref
// and carries its position in the message, so it is kept whole.
func loadedFindings(t *testing.T, src, rule string) []string {
	t.Helper()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	return diagsUnder(diags, rule)
}

// diagsUnder renders the diagnostics of diags under rule as libraryFindings
// renders the library's.
func diagsUnder(diags []ir.Diagnostic, rule string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == diag.Validation+"/"+rule {
			pos := d.Provenance.Position
			out = append(out, fmt.Sprintf("%d:%d %s", pos.Line, pos.Column, d.Message))
		}
	}
	slices.Sort(out)
	return out
}

// TestRequired_AnEmptyValueTheSpecificationAdmitsIsNoFinding pins each field
// every minor admits empty, which the library reads as missing when it is
// written so (GitHub #622). Each case first confirms the library raises the
// finding, so a library that stopped would not leave the case passing with
// nothing dropped.
func TestRequired_AnEmptyValueTheSpecificationAdmitsIsNoFinding(t *testing.T) {
	t.Parallel()
	const info = "info: {title: T, version: \"1\"}\n"
	const op = "paths:\n  /p:\n    post:\n      operationId: o\n"
	const ok = "      responses: {\"200\": {description: ok}}\n"
	scheme := func(s string) string {
		return info + "paths: {}\ncomponents:\n  securitySchemes:\n    s: " + s + "\n"
	}
	for _, tc := range []struct{ name, doc string }{
		{"info title", "info: {title: \"\", version: \"1\"}\npaths: {}\n"},
		{"info version", "info: {title: T, version: \"\"}\npaths: {}\n"},
		{"license name", "info: {title: T, version: \"1\", license: {name: \"\"}}\npaths: {}\n"},
		{"server variable default", info + "servers: [{url: \"https://h/{v}\", variables: {v: {default: \"\"}}}]\npaths: {}\n"},
		{"tag name", info + "tags: [{name: \"\"}]\npaths: {}\n"},
		{"document externalDocs url", info + "externalDocs: {url: \"\"}\npaths: {}\n"},
		{"tag externalDocs url", info + "tags: [{name: t, externalDocs: {url: \"\"}}]\npaths: {}\n"},
		{"operation externalDocs url", info + op + "      externalDocs: {url: \"\"}\n" + ok},
		{"query parameter name", info + op + "      parameters: [{name: \"\", in: query, schema: {type: string}}]\n" + ok},
		{"querystring parameter name", info + op + "      parameters: [{name: \"\", in: querystring, " +
			"content: {application/x-www-form-urlencoded: {schema: {type: object}}}}]\n" + ok},
		{"response description", info + op + "      responses: {\"200\": {description: \"\"}}\n"},
		{"request body content", info + op + "      requestBody: {content: {}}\n" + ok},
		{"query apiKey name", scheme("{type: apiKey, name: \"\", in: query}")},
		{"openIdConnectUrl", scheme("{type: openIdConnect, openIdConnectUrl: \"\"}")},
		{"implicit authorizationUrl", scheme("{type: oauth2, flows: {implicit: {authorizationUrl: \"\", scopes: {}}}}")},
		{"password tokenUrl", scheme("{type: oauth2, flows: {password: {tokenUrl: \"\", scopes: {}}}}")},
		{"clientCredentials tokenUrl", scheme("{type: oauth2, flows: {clientCredentials: {tokenUrl: \"\", scopes: {}}}}")},
		{"authorizationCode urls", scheme("{type: oauth2, flows: {authorizationCode: " +
			"{authorizationUrl: \"\", tokenUrl: \"\", scopes: {}}}}")},
		{"deviceAuthorization urls", scheme("{type: oauth2, flows: {deviceAuthorization: " +
			"{deviceAuthorizationUrl: \"\", tokenUrl: \"\", scopes: {}}}}")},
	} {
		for _, version := range versions {
			t.Run(tc.name+"/"+version, func(t *testing.T) {
				t.Parallel()
				src := "openapi: " + version + "\n" + tc.doc
				require.NotEmpty(t, libraryFindings(t, src, validation.RuleValidationRequiredField),
					"the library refuses the empty value")
				assert.Empty(t, loadedFindings(t, src, validation.RuleValidationRequiredField))
				assert.Empty(t, loadedFindings(t, src, validation.RuleValidationInvalidSyntax))
			})
		}
	}
}

// TestRequired_AFieldLeftOutIsStillRefused pins the other side of the empty
// values above: a field left out is missing at every version, whether the
// library finds it so while building the model or validating it.
func TestRequired_AFieldLeftOutIsStillRefused(t *testing.T) {
	t.Parallel()
	const info = "info: {title: T, version: \"1\"}\n"
	const op = "paths:\n  /p:\n    post:\n      operationId: o\n"
	for _, tc := range []struct{ name, doc string }{
		{"info title and version", "info: {license: {url: \"https://l\"}}\npaths: {}\n"},
		{"server url and variable default", info + "servers: [{description: d, variables: {v: {enum: [x]}}}]\npaths: {}\n"},
		{"tag name and externalDocs url", info + "tags: [{externalDocs: {description: d}}]\npaths: {}\n"},
		{"parameter name", info + op + "      parameters: [{in: query, schema: {type: string}}]\n" +
			"      responses: {\"200\": {description: ok}}\n"},
		{"request body content", info + op + "      requestBody: {description: d}\n" +
			"      responses: {\"200\": {description: ok}}\n"},
		{"apiKey name", info + "paths: {}\ncomponents: {securitySchemes: {s: {type: apiKey, in: header}}}\n"},
		{"flow urls", info + "paths: {}\ncomponents: {securitySchemes: {s: {type: oauth2, flows: " +
			"{authorizationCode: {scopes: {}}, deviceAuthorization: {scopes: {}}}}}}\n"},
	} {
		for _, version := range versions {
			t.Run(tc.name+"/"+version, func(t *testing.T) {
				t.Parallel()
				src := "openapi: " + version + "\n" + tc.doc
				want := libraryFindings(t, src, validation.RuleValidationRequiredField)
				require.NotEmpty(t, want)
				assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationRequiredField))
			})
		}
	}
}

// TestRequired_EverySpellingOfTheEmptyStringIsAdmitted pins that the value,
// not its spelling, is admitted: each YAML spelling of the empty string, and
// one an alias or a merge key brings in, whose finding the library anchors at
// a node other than the value's own.
func TestRequired_EverySpellingOfTheEmptyStringIsAdmitted(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.0.3\ninfo: {title: T, version: \"1\"}\nx-empty: &e \"\"\nx-base: &b {description: \"\"}\n" +
		"paths:\n  /p:\n    get:\n      responses:\n        \"200\":\n"
	for _, response := range []string{
		"          description: ''\n",
		"          description: !!str \"\"\n",
		"          description: |-\n          headers: {}\n",
		"          description: *e\n",
		"          <<: *b\n",
	} {
		t.Run(strings.TrimSpace(response), func(t *testing.T) {
			t.Parallel()
			src := head + response
			require.NotEmpty(t, libraryFindings(t, src, validation.RuleValidationRequiredField))
			assert.Empty(t, loadedFindings(t, src, validation.RuleValidationRequiredField))
		})
	}
}

// TestRequired_AnEmptyValueNoMinorAdmitsIsStillRefused pins the empty values
// no minor admits, so the library's finding stands, worded as it words it: an
// enumerated field, whose values never include the empty string, a path
// parameter's name, which must match a template expression, and a name sent in
// a header or a cookie, or an HTTP scheme, each a token of the RFC the
// specification cites.
func TestRequired_AnEmptyValueNoMinorAdmitsIsStillRefused(t *testing.T) {
	t.Parallel()
	const info = "info: {title: T, version: \"1\"}\n"
	param := func(in string) string {
		return info + "paths:\n  /p:\n    get:\n      parameters: [{name: \"\", in: " + in +
			", schema: {type: string}}]\n      responses: {\"200\": {description: ok}}\n"
	}
	scheme := func(s string) string {
		return info + "paths: {}\ncomponents: {securitySchemes: {s: " + s + "}}\n"
	}
	for _, tc := range []struct{ name, doc string }{
		{"header parameter name", param("header")},
		{"cookie parameter name", param("cookie")},
		{"header apiKey name", scheme("{type: apiKey, name: \"\", in: header}")},
		{"cookie apiKey name", scheme("{type: apiKey, name: \"\", in: cookie}")},
		{"http scheme", scheme("{type: http, scheme: \"\"}")},
		{"path parameter name", info + "paths:\n  /p/{x}:\n    get:\n" +
			"      parameters: [{name: \"\", in: path, required: true, schema: {type: string}}]\n" +
			"      responses: {\"200\": {description: ok}}\n"},
		{"parameter in", info + "paths:\n  /p:\n    get:\n      parameters: [{name: q, in: \"\", schema: {type: string}}]\n" +
			"      responses: {\"200\": {description: ok}}\n"},
		{"security scheme type", info + "paths: {}\ncomponents: {securitySchemes: {s: {type: \"\"}}}\n"},
		{"apiKey in", info + "paths: {}\ncomponents: {securitySchemes: {s: {type: apiKey, name: n, in: \"\"}}}\n"},
	} {
		for _, version := range versions {
			t.Run(tc.name+"/"+version, func(t *testing.T) {
				t.Parallel()
				src := "openapi: " + version + "\n" + tc.doc
				want := libraryFindings(t, src, validation.RuleValidationRequiredField)
				require.NotEmpty(t, want)
				assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationRequiredField))
			})
		}
	}
}

// TestRequired_AnEmptyServerURLFollowsTheDeclaredVersion pins the one empty
// value a minor stopped admitting: a server URL, which 3.0 and 3.1 let be a
// relative reference and 3.2's server URL grammar requires a character of.
func TestRequired_AnEmptyServerURLFollowsTheDeclaredVersion(t *testing.T) {
	t.Parallel()
	const doc = "info: {title: T, version: \"1\"}\nservers: [{url: \"\"}]\npaths: {}\n"
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			src := "openapi: " + version + "\n" + doc
			library := libraryFindings(t, src, validation.RuleValidationRequiredField)
			require.Len(t, library, 1)
			var want []string
			if atLeast(version, nonEmptyServerURLSince) {
				want = library
			}
			assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationRequiredField))
		})
	}
}

// TestRequired_AValueAMergeKeySharesIsExcusedOnlyWhereEveryReadAdmitsIt pins
// the claims on a value: a merge key gives a path and a query parameter one
// name node, and the library anchors each one's finding there. The query
// parameter's is an artifact, the path parameter's is not, and the two cannot
// be told apart at the node, so both stand.
func TestRequired_AValueAMergeKeySharesIsExcusedOnlyWhereEveryReadAdmitsIt(t *testing.T) {
	t.Parallel()
	src := `openapi: 3.1.1
info: {title: T, version: "1"}
x-unnamed: &unnamed {name: "", schema: {type: string}}
paths:
  /p/{x}:
    get:
      parameters:
        - {<<: *unnamed, in: path, required: true}
        - {<<: *unnamed, in: query}
      responses: {"200": {description: ok}}
`
	want := libraryFindings(t, src, validation.RuleValidationRequiredField)
	require.Len(t, want, 2, "one finding per parameter, at the node the merge key shares")
	require.Equal(t, strings.Fields(want[0])[0], strings.Fields(want[1])[0])
	assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationRequiredField))
}

// TestRequired_AnEmptyValueAnOverlayWritesIsJudgedAlike pins that a value the
// caller's overlay writes empty is judged as one the source writes is, placed
// at the overlay that wrote it.
func TestRequired_AnEmptyValueAnOverlayWritesIsJudgedAlike(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.1.1
info: {title: T, version: "1"}
paths: {/p: {get: {responses: {"200": {description: ok}}}}}
`)}
	ov := &overlay.Options{Data: []byte(`overlay: 1.0.0
info: {title: O, version: "1"}
actions:
  - {target: $.info, update: {title: ""}}
  - {target: '$.paths["/p"].get.responses["200"]', update: {description: ""}}
  - {target: '$.paths["/p"].get', update: {parameters: [{name: "", in: path, required: true, schema: {type: string}}]}}
`)}
	_, diags, err := Load(t.Context(), 0, src, Options{Overlay: ov, OverlaySrcIndex: 1})
	require.NoError(t, err)
	got := diagsUnder(diags, validation.RuleValidationRequiredField)
	require.Len(t, got, 1, "the path parameter's empty name alone stands: %v", got)
	assert.Contains(t, got[0], "parameter.name")
	for _, d := range diags {
		if d.Code == diag.Validation+"/"+validation.RuleValidationRequiredField {
			assert.Equal(t, 1, d.Provenance.Source, "placed at the overlay")
		}
	}
}

// TestRequired_ARelaxedFieldFollowsTheDeclaredVersion pins the two fields a
// later minor stopped requiring, which the library requires at every version:
// an Operation's responses from 3.1 and a Response's description from 3.2. A
// missing one is still a finding below that minor, wherever the object is
// written, an operation of a construct a later minor added included: the
// declared version governs the objects it holds.
func TestRequired_ARelaxedFieldFollowsTheDeclaredVersion(t *testing.T) {
	t.Parallel()
	const info = "info: {title: T, version: \"1\"}\n"
	const ok = "responses: {\"200\": {description: ok}}"
	for _, tc := range []struct{ name, since, doc string }{
		{"operation", responsesOptionalSince, info + "paths: {/p: {get: {operationId: o}}}\n"},
		{"callback operation", responsesOptionalSince, info + "paths:\n  /p:\n    post:\n      " + ok +
			"\n      callbacks: {c: {\"{$request.body#/u}\": {post: {operationId: c}}}}\n"},
		{"additionalOperations operation", responsesOptionalSince, info +
			"paths: {/p: {additionalOperations: {PURGE: {operationId: x}}}}\n"},
		{"response", descriptionOptionalSince, info + "paths: {/p: {get: {responses: {\"200\": {headers: {}}}}}}\n"},
		{"component response", descriptionOptionalSince, info + "paths: {}\ncomponents: {responses: {R: {headers: {}}}}\n"},
		{"aliased response", descriptionOptionalSince, info +
			"paths: {/p: {get: {responses: {\"200\": &r {headers: {}}, \"201\": *r}}}}\n"},
	} {
		for _, version := range versions {
			t.Run(tc.name+"/"+version, func(t *testing.T) {
				t.Parallel()
				src := "openapi: " + version + "\n" + tc.doc
				library := libraryFindings(t, src, validation.RuleValidationRequiredField)
				require.NotEmpty(t, library, "the library requires the field")
				var want []string
				if !atLeast(version, tc.since) {
					want = library
				}
				assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationRequiredField))
			})
		}
	}
}

// TestRequired_AnOperationBesideAPathItemRefIsJudgedAlike pins the relaxation
// in the fields beside a path item $ref, which the compiler models, not the
// library's unmarshal of the source: their findings are the source's own, and
// reconciled with them.
func TestRequired_AnOperationBesideAPathItemRefIsJudgedAlike(t *testing.T) {
	t.Parallel()
	const doc = `info: {title: T, version: "1"}
paths:
  /a: {$ref: "#/paths/~1b", post: {operationId: s}}
  /b: {get: {responses: {"200": {description: ok}}}}
`
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			got := loadedFindings(t, "openapi: "+version+"\n"+doc, validation.RuleValidationRequiredField)
			if atLeast(version, responsesOptionalSince) {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1, "%v", got)
			assert.Contains(t, got[0], "operation.responses")
		})
	}
}

// TestRequired_AMappingReadAsAnotherKindKeepsItsFindings pins the claims
// across kinds: one mapping an alias reads as a Response missing its
// description, an artifact in 3.2, and as a Parameter missing its name and in,
// real findings anchored at the same node. None is dropped, since the artifact
// cannot be told from them there.
func TestRequired_AMappingReadAsAnotherKindKeepsItsFindings(t *testing.T) {
	t.Parallel()
	src := `openapi: 3.2.0
info: {title: T, version: "1"}
x-shared: &m {headers: {}}
paths:
  /p:
    get:
      parameters: [*m]
      responses: {"200": *m}
`
	want := libraryFindings(t, src, validation.RuleValidationRequiredField)
	require.Len(t, want, 3, "the parameter's name and in, and the response's description")
	assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationRequiredField))
}

// TestRequired_AnEmptyDefaultDrawsNoURLFinding pins the URL check's echo of an
// empty default: the check reads it as none and says so once per use, at the
// url, beside its findings of a variable no default or declaration backs,
// which stand. urlEchoes tells the two apart.
func TestRequired_AnEmptyDefaultDrawsNoURLFinding(t *testing.T) {
	t.Parallel()
	src := `openapi: 3.1.1
info: {title: T, version: "1"}
servers:
  - url: "https://h/{a}/{b}/{c}/{a}/{d}"
    variables:
      a: {default: ""}
      c: {description: none}
      d: {default: "", enum: [x, y]}
paths: {}
`
	library := libraryFindings(t, src, validation.RuleValidationInvalidSyntax)
	require.Len(t, library, 5, "a twice, b, c and d")
	got := loadedFindings(t, src, validation.RuleValidationInvalidSyntax)
	require.Len(t, got, 2, "%v", got)
	assert.Contains(t, got[0], "server variable `b` is not defined")
	assert.Contains(t, got[1], "server variable `c` has no default value")

	required := loadedFindings(t, src, validation.RuleValidationRequiredField)
	require.Len(t, required, 1, "c's missing default stands, the empty ones do not: %v", required)
	assert.Contains(t, required[0], "serverVariable.default")
	assert.Len(t, loadedFindings(t, src, validation.RuleValidationAllowedValues), 1,
		"d's empty default is not one of its enum")

	doc, _ := parsedModel(t, src)
	echoes, stand := urlEchoes(t.Context(), doc.Servers[0], documentOptions(doc))
	assert.Equal(t, []string{
		"server variable `a` has no default value", "server variable `d` has no default value",
	}, urlMessages(echoes))
	assert.Equal(t, []string{
		"server variable `b` is not defined", "server variable `c` has no default value",
	}, urlMessages(stand))
}

// urlMessages returns what each of found says, sorted.
func urlMessages(found map[urlFinding]bool) []string {
	out := make([]string, 0, len(found))
	for f := range found {
		out = append(out, f.message)
	}
	slices.Sort(out)
	return out
}

// TestRequired_AServerWritingNoEmptyDefaultDrawsNoEcho pins that the URL check
// runs a second time only for a server writing an empty default: what one
// writing none draws all stands, as the library draws it.
func TestRequired_AServerWritingNoEmptyDefaultDrawsNoEcho(t *testing.T) {
	t.Parallel()
	src := `openapi: 3.1.1
info: {title: T, version: "1"}
servers: [{url: "https://h/{a}/{b}", variables: {a: {default: x}}}]
paths: {}
`
	doc, _ := parsedModel(t, src)
	echoes, stand := urlEchoes(t.Context(), doc.Servers[0], documentOptions(doc))
	assert.Nil(t, echoes)
	assert.Len(t, stand, 1, "b is not defined")
	want := libraryFindings(t, src, validation.RuleValidationInvalidSyntax)
	require.Len(t, want, 1)
	assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationInvalidSyntax))
}

// TestRequired_AURLAMergeKeySharesKeepsWhatAnyServerRaisesThere pins the
// claims on the URL check's findings: a merge key gives two servers one url
// node, and the check words a missing default as it words an empty one, so
// the echo is excused only where every server reading the url draws it as one.
// The server missing its default comes first, as a claim the later server
// overwrote would excuse it.
func TestRequired_AURLAMergeKeySharesKeepsWhatAnyServerRaisesThere(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.1\ninfo: {title: T, version: \"1\"}\nx-s: &s {url: \"https://h/{v}\"}\nservers:\n"
	const empty = "  - {<<: *s, variables: {v: {default: \"\"}}}\n"
	for _, tc := range []struct {
		name, servers string
		stands        bool
	}{
		{"a missing default", "  - {<<: *s, variables: {v: {description: none}}}\n" + empty, true},
		{"every default empty", empty + empty, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := head + tc.servers + "paths: {}\n"
			library := libraryFindings(t, src, validation.RuleValidationInvalidSyntax)
			require.Len(t, library, 2, "one per server, at the url the merge key shares")
			require.Equal(t, strings.Fields(library[0])[0], strings.Fields(library[1])[0])
			var want []string
			if tc.stands {
				want = library
			}
			assert.Equal(t, want, loadedFindings(t, src, validation.RuleValidationInvalidSyntax))
		})
	}
}

// TestRequired_ValidatingAgainChangesNothingTheServerHolds pins that the URL
// check runs again over copies: the server and its variables keep the
// defaults and the validity the library gave them.
func TestRequired_ValidatingAgainChangesNothingTheServerHolds(t *testing.T) {
	t.Parallel()
	doc, _ := parsedModel(t, `openapi: 3.1.1
info: {title: T, version: "1"}
servers: [{url: "https://h/{a}/{b}", variables: {a: {default: ""}, b: {default: x}}}]
paths: {}
`)
	s := doc.Servers[0]
	a, _ := s.Variables.Get("a")
	b, _ := s.Variables.Get("b")
	valid := []bool{s.Valid, a.Valid, b.Valid}

	echoes, _ := urlEchoes(t.Context(), s, documentOptions(doc))
	require.NotEmpty(t, echoes)
	assert.Empty(t, a.Default)
	assert.Equal(t, "x", b.Default)
	assert.Equal(t, valid, []bool{s.Valid, a.Valid, b.Valid})
}

// parsedModel builds src's model as Load does, with what the library's own
// unmarshal and validation of it found.
func parsedModel(t *testing.T, src string) (*soa.OpenAPI, []error) {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	releaseAnchors(&root)
	doc, errs, err := unmarshal(t.Context(), []byte(src), &root)
	require.NoError(t, err)
	return doc, errs
}

// TestRequired_AnObjectAReferenceReachesIsJudgedAlike pins the two places a
// referenced object's findings are drawn besides the source's validation: its
// resolution, where a missing field is found, and its own validation, where an
// empty one is. Each object is held to the source's declared version, whether
// the source holds it outside its model or another document does, even one
// declaring another version: which governs there is GitHub #839's.
func TestRequired_AnObjectAReferenceReachesIsJudgedAlike(t *testing.T) {
	t.Parallel()
	const objects = "  Empty: {description: \"\"}\n  Absent: {headers: {}}\n"
	const responses = "      responses:\n        \"200\": {$ref: '%[1]s/Empty'}\n        \"201\": {$ref: '%[1]s/Absent'}\n"
	paths := "info: {title: T, version: \"1\"}\npaths:\n  /p:\n    get:\n"
	for _, version := range versions {
		t.Run("in the source/"+version, func(t *testing.T) {
			t.Parallel()
			src := "openapi: " + version + "\n" + paths + fmt.Sprintf(responses, "#/x-responses") +
				"x-responses:\n" + objects
			assertRelaxedOnly(t, version, loadedFindings(t, src, validation.RuleValidationRequiredField))
		})
		t.Run("in another document/"+version, func(t *testing.T) {
			t.Parallel()
			other := "openapi: 3.1.1\ninfo: {title: O, version: \"1\"}\npaths: {}\nx-responses:\n" + objects
			dir := externalDir(t, map[string]string{"other.yaml": other})
			root := "openapi: " + version + "\n" + paths + fmt.Sprintf(responses, "./other.yaml#/x-responses")
			_, diags := loadExternal(t, dir, root, Options{})
			assertRelaxedOnly(t, version, diagsUnder(diags, validation.RuleValidationRequiredField))
		})
	}
}

// assertRelaxedOnly requires got to hold exactly the Absent response's missing
// description where version requires it, and nothing where it does not.
func assertRelaxedOnly(t *testing.T, version string, got []string) {
	t.Helper()
	if atLeast(version, descriptionOptionalSince) {
		assert.Empty(t, got)
		return
	}
	require.Len(t, got, 1, "%v", got)
	assert.Contains(t, got[0], "response.description")
}

// TestFindRequiredArtifacts_AWalkThatPanicsExcusesNothing pins the barrier
// around the walk: a panic is an error at the model reached, and none of what
// was claimed before it is excused, since a model after it may hold a finding
// at a node an artifact was claimed at.
func TestFindRequiredArtifacts_AWalkThatPanicsExcusesNothing(t *testing.T) {
	t.Parallel()
	doc, found := parsedModel(t, "openapi: 3.2.0\ninfo: {title: \"\", version: \"1\"}\npaths: {}\n")
	walk := func(yield func(soa.WalkItem) bool) {
		for item := range soa.Walk(t.Context(), doc) {
			if !yield(item) {
				return
			}
		}
		panic("walk fault")
	}
	got := findRequiredArtifacts(t.Context(), found, walk, doc.OpenAPI, documentOptions(doc))
	assert.Empty(t, got.at, "the empty title was claimed before the panic")

	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }
	diags := got.reportedAt("/paths/~1p").faults(at)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.Validation, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/paths/~1p"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "required-field check panicked: walk fault")
}

// TestFindRequiredArtifacts_NoFindingOfTheRuleIsNoWalk pins that the walk is
// paid for only where the library raised a finding of the rule: a document
// drawing none, the common case, is not walked again.
func TestFindRequiredArtifacts_NoFindingOfTheRuleIsNoWalk(t *testing.T) {
	t.Parallel()
	doc, found := parsedModel(t, `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {/p: {get: {responses: {"200": {description: ok}}}}}
components: {securitySchemes: {s: {type: apiKey, name: n, in: sideways}}}
`)
	require.NotEmpty(t, found, "a finding of another rule")
	walked := false
	walk := func(func(soa.WalkItem) bool) { walked = true }
	got := findRequiredArtifacts(t.Context(), found, walk, doc.OpenAPI, documentOptions(doc))
	assert.False(t, walked)
	assert.Empty(t, got.at)
	assert.Empty(t, got.faults(func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }))
}

// TestRequired_TheRelaxedFieldIsTheOnlyOneTheLibraryRequires pins what relaxed
// rests on: of an Operation and a Response written empty, the library finds
// one field missing, anchored at the mapping. A release requiring another of
// either object reddens this before relaxed excuses its finding too.
func TestRequired_TheRelaxedFieldIsTheOnlyOneTheLibraryRequires(t *testing.T) {
	t.Parallel()
	got := libraryFindings(t, `openapi: 3.0.3
info: {title: T, version: "1"}
paths:
  /p:
    get: {}
    put:
      responses:
        "200": {}
`, validation.RuleValidationRequiredField)
	assert.Equal(t, []string{
		"5:10 `operation.responses` is required",
		"8:16 `response.description` is required",
	}, got)
}
