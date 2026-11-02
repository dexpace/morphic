// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// fixtureDates lists each date the fixtures below write.
var fixtureDates = []string{"2021-01-01", "2022-02-02", "2021-1-1", "2001-12-14T21:59:43.10Z", "2001-12-14 21:59:43.10"}

// quotedDates returns src with each date in fixtureDates quoted, the one
// spelling of the string a parser of every YAML version agrees on. Quoting
// moves what follows a date on its line, and no fixture draws a finding there,
// so each twin's positions hold.
func quotedDates(src string) string {
	for _, date := range fixtureDates {
		src = strings.ReplaceAll(src, date, `"`+date+`"`)
	}
	return src
}

// datedFixtureHead opens every fixture.
const datedFixtureHead = "openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\n"

// datedFields writes a plain date at fields the parser's model reads as a
// string, each a document of its own. The rows past "a security scheme's $ref"
// lowered as their twins before GitHub #1280 too: a value, an extension and a
// key are read off the node.
var datedFields = []struct{ name, src string }{
	{"the document's title and version", `openapi: 3.1.0
info:
  title: 2021-01-01
  version: 2021-01-01
  description: 2021-01-01
paths: {}
`},
	{"a tag's name and an operation's tags", datedFixtureHead + `tags:
  - name: 2021-01-01
paths:
  /b:
    get:
      tags:
        - 2021-01-01
      responses: {"200": {description: ok}}
`},
	{"an operation's ID, summary and description", datedFixtureHead + `paths:
  /b:
    get:
      operationId: 2021-01-01
      summary: 2021-01-01
      description: 2021-01-01
      responses: {"200": {description: ok}}
`},
	{"the other spellings of a timestamp", datedFixtureHead + `paths:
  /b:
    get:
      operationId: 2001-12-14T21:59:43.10Z
      summary: 2021-1-1
      description: 2001-12-14 21:59:43.10
      responses: {"200": {description: ok}}
`},
	{"a schema's title and required names", datedFixtureHead + `paths: {}
components:
  schemas:
    A:
      type: object
      title: 2021-01-01
      required:
        - 2021-01-01
        - b
      properties:
        b: {type: string}
        2021-01-01:
          type: string
`},
	{"a discriminator's property name", datedFixtureHead + `paths: {}
components:
  schemas:
    Pet:
      type: object
      required:
        - 2021-01-01
      properties:
        2021-01-01:
          type: string
      discriminator:
        propertyName: 2021-01-01
`},
	{"a response's description", datedFixtureHead + `paths:
  /b:
    get:
      responses:
        "200":
          description: 2021-01-01
`},
	{"a $ref at each reference object", datedFixtureHead + `paths:
  /a:
    $ref: 2021-01-01
  /b:
    get:
      parameters:
        - $ref: 2021-01-01
      requestBody:
        $ref: 2021-01-01
      responses:
        "200":
          $ref: 2021-01-01
        "201":
          description: ok
          headers:
            H:
              $ref: 2021-01-01
          links:
            L:
              $ref: 2021-01-01
      callbacks:
        c:
          $ref: 2021-01-01
`},
	{"a content entry's $ref", `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              $ref: 2021-01-01
            text/plain: {schema: {type: string}}
`},
	{"a path item's summary alone, beside a description and beside a $ref", datedFixtureHead + `paths:
  /alone:
    summary: 2021-01-01
    get: {responses: {"200": {description: ok}}}
  /beside:
    description: d
    summary: 2021-01-01
    get: {responses: {"200": {description: ok}}}
  /ref:
    $ref: '#/components/pathItems/O'
    summary: 2021-01-01
components:
  pathItems:
    O: {summary: REF, get: {responses: {"200": {description: ok}}}}
`},
	{"parameters named by two dates, mounted and not", datedFixtureHead + `paths:
  /op:
    parameters:
      - schema: {type: string}
        in: query
        name: 2021-01-01
      - schema: {type: string}
        in: query
        name: 2022-02-02
    get: {responses: {"200": {description: ok}}}
  /unmounted:
    parameters:
      - schema: {type: string}
        in: query
        name: 2021-01-01
      - schema: {type: string}
        in: query
        name: 2022-02-02
`},
	{"an http scheme's token", datedFixtureHead + `security:
  - h: []
paths: {}
components:
  securitySchemes:
    h:
      type: http
      scheme: 2021-01-01
`},
	{"a security scheme's $ref", datedFixtureHead + `paths: {}
components:
  securitySchemes:
    s:
      $ref: 2021-01-01
`},
	{"a value and an extension", datedFixtureHead + `x-when: 2021-01-01
paths: {}
components:
  schemas:
    D:
      type: string
      enum:
        - 2021-01-01
        - 2022-02-02
      default: 2021-01-01
`},
	{"a key", datedFixtureHead + `paths: {}
components:
  schemas:
    2021-01-01:
      type: object
      properties:
        2022-02-02: {type: string}
`},
}

// TestCompile_APlainDateCompilesAsItsQuotedTwin pins GitHub #1280. YAML 1.2,
// which OpenAPI recommends, has no timestamp and reads an unquoted date as a
// string, but the parser tags it a timestamp, from which its model decodes no
// string: each field read as left out, and a $ref as no reference, with no
// diagnostic. A document and its twin quoting each date must compile to one
// IR, their diagnostics included, whether a finding is right or not.
func TestCompile_APlainDateCompilesAsItsQuotedTwin(t *testing.T) {
	t.Parallel()
	for _, tc := range datedFields {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			twinSrc := quotedDates(tc.src)
			require.NotEqual(t, tc.src, twinSrc, "sanity: the fixture writes a date")

			plain, _ := parseFull(t, tc.src)
			twin, _ := parseFull(t, twinSrc)

			if diff := cmp.Diff(withoutSourceHash(twin), withoutSourceHash(plain)); diff != "" {
				t.Errorf("the plain dates compiled to other IR than their twin (-quoted +plain):\n%s", diff)
			}
		})
	}
}

// TestCompile_APlainMergeValueCompilesAsItsQuotedTwin holds the other scalar
// the parser tags as no string where YAML 1.2 reads one, a `<<` written as a
// value, which it tags a merge, to the same twin comparison. It takes each row
// of datedFields with its dates written `<<`, but a row writing a date as a
// key, where `<<` merges.
func TestCompile_APlainMergeValueCompilesAsItsQuotedTwin(t *testing.T) {
	t.Parallel()
	dateKey := regexp.MustCompile(`(?m)(^|[{,])\s*\d{4}-\d{1,2}-\d{1,2}:`)
	pairs := make([]string, 0, 2*len(fixtureDates))
	for _, date := range fixtureDates {
		pairs = append(pairs, date, "<<")
	}
	asMerges := strings.NewReplacer(pairs...)

	ran := 0
	for _, tc := range datedFields {
		if dateKey.MatchString(tc.src) {
			continue
		}
		ran++
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := asMerges.Replace(tc.src)
			require.Contains(t, src, "<<", "sanity: the fixture writes a <<")

			plain, _ := parseFull(t, src)
			twin, _ := parseFull(t, strings.ReplaceAll(src, "<<", `"<<"`))

			if diff := cmp.Diff(withoutSourceHash(twin), withoutSourceHash(plain)); diff != "" {
				t.Errorf("the plain << compiled to other IR than its twin (-quoted +plain):\n%s", diff)
			}
		})
	}
	require.NotZero(t, ran, "sanity: a row writes no date as a key")
}

// TestCompile_APlainDateInAnExternalDocumentCompilesAsItsTwin extends the twin
// comparison to a document an external reference names, read from a file and
// over HTTP, since each document the load phase reads is readied for the model
// apart from the source.
func TestCompile_APlainDateInAnExternalDocumentCompilesAsItsTwin(t *testing.T) {
	t.Parallel()
	const ext = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    summary: 2021-01-01
    get:
      operationId: 2021-01-01
      responses:
        "200":
          description: 2021-01-01
`
	t.Run("in a file", func(t *testing.T) {
		t.Parallel()
		if diff := cmp.Diff(compileBeside(t, quotedDates(ext)), compileBeside(t, ext)); diff != "" {
			t.Errorf("the plain dates compiled to other IR than their twin (-quoted +plain):\n%s", diff)
		}
	})
	t.Run("over HTTP", func(t *testing.T) {
		t.Parallel()
		plain, twin := compileServed(t, ext, quotedDates(ext))
		if diff := cmp.Diff(twin, plain); diff != "" {
			t.Errorf("the plain dates compiled to other IR than their twin (-quoted +plain):\n%s", diff)
		}
	})
}

// TestCompile_APlainDateAnOverlayWritesCompilesAsItsTwin extends the twin
// comparison to a value an overlay grafts onto the source, which the load phase
// readies for the model with the source's own nodes, after the overlay applies.
func TestCompile_APlainDateAnOverlayWritesCompilesAsItsTwin(t *testing.T) {
	t.Parallel()
	const patch = `overlay: 1.0.0
info: {title: Patch, version: "1"}
actions:
  - target: $.components.schemas.Pet
    update:
      title: 2021-01-01
`
	compile := func(patch string) *ir.Document {
		doc, _ := compileWith(t, openapi.Options{Overlay: &openapi.Overlay{Path: "patch.yaml", Data: []byte(patch)}})
		return withoutSourceHash(doc)
	}
	if diff := cmp.Diff(compile(quotedDates(patch)), compile(patch)); diff != "" {
		t.Errorf("the plain date compiled to other IR than its twin (-quoted +plain):\n%s", diff)
	}
}

// TestCompile_ADateTaggedOutrightKeepsItsTag holds the change of GitHub #1280 to
// plain dates: a scalar the source tags !!timestamp keeps its tag, so one whose
// text is no date is refused as that tag reads it, not kept as text, which is
// #245's to decide.
func TestCompile_ADateTaggedOutrightKeepsItsTag(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, datedFixtureHead+`paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        d:
          type: string
          default: !!timestamp soon
`)

	s, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, s.Properties, 1)
	assert.Nil(t, s.Properties[0].Default, "the default is dropped, not kept as its text")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
	assert.Contains(t, diags[0].Message, `timestamp literal "soon"`)
}
