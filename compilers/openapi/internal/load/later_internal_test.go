package load

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"os"
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
	"github.com/dexpace/morphic/ir"
)

// laterFixture writes every addition but the cookie style once, in a document
// declaring 3.0.3; laterFixtureSites says where.
const laterFixture = "../../../../testdata/conformance/openapi/later-version-constructs.yaml"

// laterFixtureSites is the key laterFixture writes each addition at.
var laterFixtureSites = map[addition]jsontext.Pointer{
	{openAPIObject, "jsonSchemaDialect", "", "3.1"}:        "/jsonSchemaDialect",
	{openAPIObject, "webhooks", "", "3.1"}:                 "/webhooks",
	{openAPIObject, "$self", "", "3.2"}:                    "/$self",
	{infoObject, "summary", "", "3.1"}:                     "/info/summary",
	{licenseObject, "identifier", "", "3.1"}:               "/info/license/identifier",
	{componentsObject, "pathItems", "", "3.1"}:             "/components/pathItems",
	{componentsObject, "mediaTypes", "", "3.2"}:            "/components/mediaTypes",
	{serverObject, "name", "", "3.2"}:                      "/servers/0/name",
	{tagObject, "summary", "", "3.2"}:                      "/tags/0/summary",
	{tagObject, "parent", "", "3.2"}:                       "/tags/0/parent",
	{tagObject, "kind", "", "3.2"}:                         "/tags/0/kind",
	{pathItemObject, "query", "", "3.2"}:                   "/paths/~1books/query",
	{pathItemObject, "additionalOperations", "", "3.2"}:    "/paths/~1books/additionalOperations",
	{parameterObject, "in", "querystring", "3.2"}:          "/paths/~1books/get/parameters/0/in",
	{mediaTypeObject, "$ref", "", "3.2"}:                   "/paths/~1books/post/requestBody/content/application~1json/$ref",
	{mediaTypeObject, "description", "", "3.2"}:            "/paths/~1books/get/responses/200/content/application~1jsonl/description",
	{mediaTypeObject, "itemSchema", "", "3.2"}:             "/paths/~1books/get/responses/200/content/application~1jsonl/itemSchema",
	{mediaTypeObject, "itemEncoding", "", "3.2"}:           "/paths/~1books/post/requestBody/content/multipart~1mixed/itemEncoding",
	{mediaTypeObject, "prefixEncoding", "", "3.2"}:         "/paths/~1books/post/requestBody/content/multipart~1related/prefixEncoding",
	{encodingObject, "encoding", "", "3.2"}:                "/paths/~1books/post/requestBody/content/multipart~1form-data/encoding/cover/encoding",
	{encodingObject, "prefixEncoding", "", "3.2"}:          "/paths/~1books/post/requestBody/content/multipart~1form-data/encoding/cover/prefixEncoding",
	{encodingObject, "itemEncoding", "", "3.2"}:            "/paths/~1books/post/requestBody/content/multipart~1form-data/encoding/cover/itemEncoding",
	{exampleObject, "dataValue", "", "3.2"}:                "/paths/~1books/get/responses/200/content/application~1jsonl/examples/one/dataValue",
	{exampleObject, "serializedValue", "", "3.2"}:          "/paths/~1books/get/responses/200/content/application~1jsonl/examples/one/serializedValue",
	{responseObject, "summary", "", "3.2"}:                 "/paths/~1books/get/responses/200/summary",
	{securitySchemeObject, "type", "mutualTLS", "3.1"}:     "/components/securitySchemes/clientCert/type",
	{securitySchemeObject, "oauth2MetadataUrl", "", "3.2"}: "/components/securitySchemes/device/oauth2MetadataUrl",
	{securitySchemeObject, "deprecated", "", "3.2"}:        "/components/securitySchemes/device/deprecated",
	{oauthFlowsObject, "deviceAuthorization", "", "3.2"}:   "/components/securitySchemes/device/flows/deviceAuthorization",
	{discriminatorObject, "defaultMapping", "", "3.2"}:     "/components/schemas/Item/discriminator/defaultMapping",
	{xmlObject, "nodeType", "", "3.2"}:                     "/components/schemas/Item/xml/nodeType",
	{referenceObject, "summary", "", "3.1"}:                "/paths/~1books/get/responses/404/summary",
	{referenceObject, "description", "", "3.1"}:            "/paths/~1books/post/requestBody/content/application~1json/description",
}

// cookieStyle is the one addition laterFixture does not write: the bundled
// parser refuses it at every version (GitHub #389), and the corpus takes no
// document with an error. TestLaterConstructs_TheCookieStyle covers it.
var cookieStyle = addition{parameterObject, "style", "cookie", "3.2"}

// laterAt loads src and returns each openapi/later-version-construct it draws,
// keyed by pointer, with the message as the value.
func laterAt(t *testing.T, src string) map[jsontext.Pointer]string {
	t.Helper()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	got := map[jsontext.Pointer]string{}
	for _, d := range diags {
		if d.Code != diag.LaterVersionConstruct {
			continue
		}
		assert.Equal(t, ir.SeverityWarning, d.Severity)
		assert.NotContains(t, got, d.Provenance.Pointer, "one construct, one report")
		got[d.Provenance.Pointer] = d.Message
	}
	return got
}

// TestAdditions_EachIsReportedBelowItsMinorOnly holds every row of additions to
// the fixture writing it: at each declared minor, exactly the rows a later
// minor added are reported, each once at its own key, naming its object and
// the minor that added it. A row with no site in the fixture fails, so the
// table cannot grow past what is tested.
func TestAdditions_EachIsReportedBelowItsMinorOnly(t *testing.T) {
	t.Parallel()
	for _, a := range additions {
		if a != cookieStyle {
			assert.Contains(t, laterFixtureSites, a, "the fixture writes every addition")
		}
	}
	data, err := os.ReadFile(laterFixture)
	require.NoError(t, err)
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(string(data), "openapi: 3.0.3", "openapi: "+version, 1)
			got := laterAt(t, src)
			want := map[jsontext.Pointer]addition{}
			for a, at := range laterFixtureSites {
				if before(version, a.since) {
					want[at] = a
				}
			}
			assert.Equal(t, slices.Sorted(maps.Keys(want)), slices.Sorted(maps.Keys(got)))
			for at, a := range want {
				assert.Equal(t, laterDiag(ir.Provenance{}, a, version).Message, got[at])
			}
		})
	}
}

// TestLaterConstructs_TheCookieStyle covers the addition the fixture cannot
// write: a cookie style is reported below 3.2 beside the parser's own refusal
// of it, which stands at every version (GitHub #389).
func TestLaterConstructs_TheCookieStyle(t *testing.T) {
	t.Parallel()
	const at = "/paths/~1c/get/parameters/0/style"
	for _, version := range []string{"3.1.0", "3.2.0"} {
		got := laterAt(t, "openapi: "+version+`
info: {title: T, version: "1"}
paths:
  /c:
    get:
      parameters:
        - {name: s, in: cookie, style: cookie, schema: {type: string}}
      responses: {"200": {description: ok}}
`)
		if version == "3.2.0" {
			assert.Empty(t, got)
			continue
		}
		assert.Equal(t, map[jsontext.Pointer]string{at: laterDiag(ir.Provenance{}, cookieStyle, version).Message}, got)
	}
}

// TestLaterConstructs_AValueIsMatchedAsTheParserReadsIt pins a value addition to
// the one value it names: another value of the field is not reported, and the
// value is read through an alias or a `<<` merge as the parser reads it, so a
// spelling the document writes elsewhere still counts.
func TestLaterConstructs_AValueIsMatchedAsTheParserReadsIt(t *testing.T) {
	t.Parallel()
	got := laterAt(t, `openapi: 3.0.3
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {name: q, in: query, schema: {type: string}}
        - {name: w, in: &qs querystring, content: {text/plain: {schema: {type: string}}}}
      responses: {"200": {description: ok}}
  /b:
    get:
      parameters:
        - {name: v, in: *qs, content: {text/plain: {schema: {type: string}}}}
      responses: {"200": {description: ok}}
components:
  securitySchemes:
    k: {type: apiKey, name: k, in: header}
    m:
      <<: {type: mutualTLS}
`)
	assert.Equal(t, []jsontext.Pointer{
		"/components/securitySchemes/m/type",
		"/paths/~1a/get/parameters/1/in",
		"/paths/~1b/get/parameters/0/in",
	}, slices.Sorted(maps.Keys(got)))
}

// TestLaterConstructs_AnObjectIsJudgedWhereItIsWritten pins the reach of the
// walk: an object is visited where it is written, so a component two
// references reach is reported once, at the component, and a component nothing
// references is reported all the same. A key beside a $ref belongs to the
// Reference Object, not to the object it names, so it is not read as that
// object's field: 3.1 gives a Reference Object a summary.
func TestLaterConstructs_AnObjectIsJudgedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	got := laterAt(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: '#/components/responses/Ok', summary: As a sees it}
  /b:
    get:
      responses:
        "200": {$ref: '#/components/responses/Ok'}
components:
  responses:
    Ok: {description: ok, summary: Fine}
  parameters:
    Unused: {name: w, in: querystring, content: {text/plain: {schema: {type: string}}}}
`)
	assert.Equal(t, []jsontext.Pointer{
		"/components/parameters/Unused/in",
		"/components/responses/Ok/summary",
	}, slices.Sorted(maps.Keys(got)))
}

// referenceSiblingsSpec writes summary or description beside a $ref at every
// position a Reference Object stands, beside a Path Item's $ref, whose own
// fields those are, and beside a schema's, which GitHub #871 owns.
const referenceSiblingsSpec = `info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {$ref: '#/components/parameters/P', description: d}
      requestBody: {$ref: '#/components/requestBodies/B', description: d}
      responses:
        "200":
          description: ok
          headers:
            X: {$ref: '#/components/headers/H', description: d}
          links:
            l: {$ref: '#/components/links/L', description: d}
          content:
            application/json:
              schema: {$ref: '#/components/schemas/S', description: d}
              examples:
                e: {$ref: '#/components/examples/E', summary: s}
        "201": {$ref: '#/components/responses/R', summary: s}
      callbacks:
        c: {$ref: '#/components/callbacks/C', summary: s}
  /b: {$ref: '#/paths/~1a', summary: s}
components:
  schemas:
    S: {type: string}
  parameters:
    P: {name: p, in: query, schema: {type: string}}
  requestBodies:
    B: {content: {text/plain: {schema: {type: string}}}}
  headers:
    H: {schema: {type: string}}
  links:
    L: {operationId: g}
  examples:
    E: {value: 1}
  responses:
    R: {description: r}
  callbacks:
    C: {'{$request.body#/u}': {post: {responses: {"200": {description: ok}}}}}
  securitySchemes:
    K: {type: apiKey, name: k, in: header}
    Alias: {$ref: '#/components/securitySchemes/K', description: d}
`

// TestLaterConstructs_AReferenceObjectIsJudgedOnItsOwnKeys pins the Reference
// Object's summary and description, which 3.1 added: below 3.1 each is reported
// at its own key at every position a Reference Object stands, though the
// lowering folds it over the target's docs at every version (ir-design §12.2).
// A Path Item's $ref is not a Reference Object, and a schema's is not judged
// here (GitHub #871).
func TestLaterConstructs_AReferenceObjectIsJudgedOnItsOwnKeys(t *testing.T) {
	t.Parallel()
	got := laterAt(t, "openapi: 3.0.3\n"+referenceSiblingsSpec)
	assert.Equal(t, []jsontext.Pointer{
		"/components/securitySchemes/Alias/description",
		"/paths/~1a/get/callbacks/c/summary",
		"/paths/~1a/get/parameters/0/description",
		"/paths/~1a/get/requestBody/description",
		"/paths/~1a/get/responses/200/content/application~1json/examples/e/summary",
		"/paths/~1a/get/responses/200/headers/X/description",
		"/paths/~1a/get/responses/200/links/l/description",
		"/paths/~1a/get/responses/201/summary",
	}, slices.Sorted(maps.Keys(got)))
	for at, message := range got {
		assert.Contains(t, message, "of the Reference Object was added in OpenAPI 3.1", at)
	}
	assert.Empty(t, laterAt(t, "openapi: 3.1.0\n"+referenceSiblingsSpec))
}

// TestLaterConstructs_APathItemRefIsJudgedOnTheFieldsBesideIt pins a Path
// Item's $ref: the keys written beside it are the Path Item's own fields, so a
// later minor's is reported there, where it is written, whether or not
// anything lowers it (GitHub #577), and one every minor defines is not.
func TestLaterConstructs_APathItemRefIsJudgedOnTheFieldsBesideIt(t *testing.T) {
	t.Parallel()
	const spec = `info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses: {"200": {description: ok}}
  /b:
    $ref: '#/paths/~1a'
    summary: s
    query:
      responses: {"200": {description: ok}}
    additionalOperations:
      PURGE:
        responses: {"200": {description: ok}}
`
	got := laterAt(t, "openapi: 3.1.0\n"+spec)
	assert.Equal(t, []jsontext.Pointer{
		"/paths/~1b/additionalOperations",
		"/paths/~1b/query",
	}, slices.Sorted(maps.Keys(got)))
	for at, message := range got {
		assert.Contains(t, message, "of the Path Item Object was added in OpenAPI 3.2", at)
	}
	assert.Empty(t, laterAt(t, "openapi: 3.2.0\n"+spec))
}

// bareReference is a $ref model with no core to read, and coredReference one
// whose core holds no mapping: shapes no library model takes, which ownRoot
// must still answer for rather than fault on.
type (
	bareReference  struct{}
	coredReference struct{ core any }
)

func (bareReference) GetRootNode() *yaml.Node  { return nil }
func (bareReference) IsReference() bool        { return true }
func (coredReference) GetRootNode() *yaml.Node { return nil }
func (coredReference) IsReference() bool       { return true }
func (r coredReference) GetCoreAny() any       { return r.core }

// TestOwnRoot_AModelWithNoReadableCoreHasNone pins ownRoot's two guards: a $ref
// whose own mapping cannot be read is judged on no keys, so nothing is reported
// for it, rather than on the target's.
func TestOwnRoot_AModelWithNoReadableCoreHasNone(t *testing.T) {
	t.Parallel()
	assert.Nil(t, ownRoot(bareReference{}))
	assert.Nil(t, ownRoot(coredReference{core: 42}))
}

// TestLaterConstructs_ARefIsNotJudgedOnceResolved pins the $ref guard where it
// has work to do. Load asks before references resolve, when a $ref holds no
// object; once resolved, a $ref's root node is its target's, and judging it
// would report the target's keys again, at every $ref reaching them.
func TestLaterConstructs_ARefIsNotJudgedOnceResolved(t *testing.T) {
	t.Parallel()
	loaded, _, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      responses:
        "200": {$ref: '#/components/responses/Ok'}
components:
  responses:
    Ok: {description: ok, summary: Fine}
`)}, Options{})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }
	got := laterConstructs(at, soa.Walk(t.Context(), loaded.Doc), loaded.Doc.OpenAPI)
	require.Len(t, got.diags, 1)
	assert.Equal(t, jsontext.Pointer("/components/responses/Ok/summary"), got.diags[0].Provenance.Pointer)
}

// TestLaterConstructs_AWalkThatPanicsIsReported pins the barrier around the
// walk, as parameterRepeats has one: a panic is an error at the model reached.
func TestLaterConstructs_AWalkThatPanicsIsReported(t *testing.T) {
	t.Parallel()
	panics := func(func(soa.WalkItem) bool) { panic("walk fault") }
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }
	got := laterConstructs(at, panics, "3.1.0")
	require.Len(t, got.diags, 1)
	assert.Equal(t, ir.SeverityError, got.diags[0].Severity)
	assert.Equal(t, diag.Validation, got.diags[0].Code)
	assert.Contains(t, got.diags[0].Message, "version check panicked: walk fault")
	assert.False(t, got.inSchema)
}

// TestAddedAfter_FollowsTheDeclaredMinor pins which rows a version is held to:
// every row for 3.0, the 3.2 rows for 3.1, and none for 3.2 or for a version
// the compiler does not read, so nothing is reported against a dialect nothing
// describes.
func TestAddedAfter_FollowsTheDeclaredMinor(t *testing.T) {
	t.Parallel()
	since := func(version string) []string {
		rows := addedAfter(version)
		out := make([]string, 0, len(rows))
		for _, a := range rows {
			out = append(out, a.since)
		}
		return slices.Compact(slices.Sorted(slices.Values(out)))
	}
	assert.Len(t, addedAfter("3.0.3"), len(additions))
	assert.Equal(t, []string{"3.1", "3.2"}, since("3.0.3"))
	assert.Equal(t, []string{"3.2"}, since("3.1.0"))
	assert.Empty(t, addedAfter("3.2.0"))
	assert.Empty(t, addedAfter("4.0.0"))
	assert.Empty(t, addedAfter(""))
}

// TestModelMinor_IsTheLatestMinorRead pins the library's model to the newest
// minor this compiler reads. ownAdditionalOperations runs the checks the library
// skips below that minor, so a library that moves it changes what is skipped,
// and must be read before this compiler follows.
func TestModelMinor_IsTheLatestMinorRead(t *testing.T) {
	t.Parallel()
	assert.Equal(t, supportedMinors[len(supportedMinors)-1], modelMinor())
}

// additionalOpsSpec writes, under additionalOperations, an operation whose path
// parameter is not required, which only the library's validation of the
// operation reports, and a key naming a method with a field of its own. A
// path item written as a $ref holds the same operations, checked where its
// target is written.
const additionalOpsSpec = `info: {title: T, version: "1"}
paths:
  /x/{id}:
    additionalOperations:
      PURGE:
        parameters:
          - {name: id, in: path, schema: {type: string}}
        responses: {"200": {description: ok}}
      GET:
        responses: {"200": {description: ok}}
  /y/{id}:
    $ref: '#/components/pathItems/Y'
components:
  pathItems:
    Y:
      additionalOperations:
        PURGE:
          parameters:
            - {name: id, in: path, schema: {type: string}}
          responses: {"200": {description: ok}}
`

// unreadOpsSpec writes an operation the parser cannot read whole, as it lacks
// its responses, so the library raises no refusal of the field below 3.2 and
// checks nothing inside it either.
const unreadOpsSpec = `info: {title: T, version: "1"}
paths:
  /x:
    additionalOperations:
      PURGE: {operationId: purge}
      GET:
        responses: {"200": {description: ok}}
`

// validationAt loads src and returns the library findings it reports, as
// "code line:column" lines, with what laterConstructs adds left out.
func validationAt(t *testing.T, src string) []string {
	t.Helper()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{})
	require.NoError(t, err)
	var out []string
	for _, d := range diags {
		if strings.HasPrefix(d.Code, diag.Validation) {
			out = append(out, fmt.Sprintf("%s %d:%d", d.Code, d.Provenance.Position.Line, d.Provenance.Position.Column))
		}
	}
	return out
}

// TestOwnAdditionalOperations_A31DocumentIsCheckedAs32Is pins what the compiler
// takes over below 3.2: the library's refusal of the field is gone, and what
// the library checks inside it at 3.2 is checked all the same — each finding
// once, at 3.2 as well, where the library checks it itself, and whether or not
// the library read the field whole enough to refuse it.
func TestOwnAdditionalOperations_A31DocumentIsCheckedAs32Is(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spec string
		want       int
	}{
		{"read whole", additionalOpsSpec, 3},
		{"not read whole", unreadOpsSpec, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			at32 := validationAt(t, "openapi: 3.2.0\n"+tc.spec)
			require.Len(t, at32, tc.want, "got %v", at32)
			assert.Equal(t, at32, validationAt(t, "openapi: 3.1.0\n"+tc.spec))
			assert.Equal(t, at32, validationAt(t, "openapi: 3.0.3\n"+tc.spec))
		})
	}
}

// TestOwnAdditionalOperations_OnlyTheFieldsRefusalIsDropped pins that the
// version rule is taken over for the one field: the library's report of a
// version it cannot read stays.
func TestOwnAdditionalOperations_OnlyTheFieldsRefusalIsDropped(t *testing.T) {
	t.Parallel()
	got := validationAt(t, "openapi: \"3.1\"\n"+additionalOpsSpec)
	assert.Contains(t, got, diag.Validation+"/"+validation.RuleValidationSupportedVersion+" 1:10")
}

// TestOwnAdditionalOperations_AWalkThatPanicsIsReported pins the barrier around
// the walk: the panic joins the findings as an error rather than escaping.
func TestOwnAdditionalOperations_AWalkThatPanicsIsReported(t *testing.T) {
	t.Parallel()
	doc, valErrs := parseSpec(t, minimal31)
	panics := func(func(soa.WalkItem) bool) { panic("walk fault") }
	got := ownAdditionalOperations(t.Context(), doc, panics, valErrs)
	require.Len(t, got, len(valErrs)+1)
	assert.True(t, slices.ContainsFunc(got, func(err error) bool {
		return strings.Contains(err.Error(), "additionalOperations check panicked: walk fault")
	}))
}
