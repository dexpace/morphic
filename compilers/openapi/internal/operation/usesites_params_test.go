// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// paramObjectKeysAfterSchema is param_schema_keys_apart with each object's own
// key written after its schema, the other order of the same document.
const paramObjectKeysAfterSchema = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters:
        - name: p
          in: query
          schema: {type: string, divisibleBy: FROM_SCHEMA}
          divisibleBy: FROM_PARAMETER_OBJECT
        - name: r
          in: query
          schema: {$ref: '#/components/schemas/S', format: FROM_SCHEMA_BESIDE_REF}
          format: FROM_PARAMETER_OBJECT
      responses:
        "200":
          description: ok
          headers:
            X-H: {schema: {type: string, divisibleBy: FROM_SCHEMA}, divisibleBy: FROM_HEADER_OBJECT}
components:
  schemas:
    S: {type: string}
`

// TestParameter_SchemaEntriesAreScoped holds what a parameter's schema writes to
// openapi:schema/, what an elected content entry's schema writes to
// openapi:content/<mt>/schema/, and the parameter's own keys to no scope, with
// the redaction either states filling the parameter and marking it.
func TestParameter_SchemaEntriesAreScoped(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, conformanceSpecsDir, "carried-keys"))
	assert.Equal(t, []string{"openapi/unpromoted-extension info /tags/1/x-stability"}, findings(diags))
	a := opTree(t, doc, "a")

	p := namedItem(t, at(a, "params"), "p")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:x-a":                `vendor_extension "FROM_PARAMETER" /paths/~1a/get/parameters/0/x-a`,
		"openapi:schema/x-a":         `vendor_extension "FROM_SCHEMA" /paths/~1a/get/parameters/0/schema/x-a`,
		"openapi:schema/x-sensitive": `vendor_extension true /paths/~1a/get/parameters/0/schema/x-sensitive`,
	}, entries(t, at(p, "unmodeled"))))
	assert.Equal(t, true, at(p, "secret"))
	assert.Equal(t, "extension-promotion", at(p, "provenance", "inferred"))

	const media = "/paths/~1a/get/parameters/1/content/application~1json"
	c := namedItem(t, at(a, "params"), "c")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:content/application~1json/x-a":         `vendor_extension "FROM_MEDIA" ` + media + `/x-a`,
		"openapi:content/application~1json/x-sensitive": `vendor_extension true ` + media + `/x-sensitive`,
		"openapi:content/application~1json/schema/x-a":  `vendor_extension "FROM_SCHEMA" ` + media + `/schema/x-a`,
	}, entries(t, at(c, "unmodeled"))))
	assert.Equal(t, true, at(c, "secret"))
	assert.Equal(t, "extension-promotion", at(c, "provenance", "inferred"))

	const header = "/paths/~1a/get/responses/200/headers/X-H"
	h := namedItem(t, at(a, "responses", "0", "headers"), "X-H")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:x-a": `vendor_extension "FROM_HEADER" ` + header + `/x-a`,
		"openapi:content/text~1plain/x-sensitive": `vendor_extension true ` + header +
			`/content/text~1plain/x-sensitive`,
	}, entries(t, at(h, "unmodeled"))))
	assert.Equal(t, true, at(h, "secret"))
	assert.Equal(t, "extension-promotion", at(h, "provenance", "inferred"))
}

// TestParameter_SchemaAndObjectSpellOneKey holds a parameter, its schema and a
// header each writing one key to a kept entry apiece, in both orders of their
// keys: no collision finding, and one document for both orders.
func TestParameter_SchemaAndObjectSpellOneKey(t *testing.T) {
	t.Parallel()
	want := map[string]map[string]string{
		"p": {
			"openapi:divisibleBy":        `out_of_scope "FROM_PARAMETER_OBJECT" /paths/~1a/get/parameters/0/divisibleBy`,
			"openapi:schema/divisibleBy": `out_of_scope "FROM_SCHEMA" /paths/~1a/get/parameters/0/schema/divisibleBy`,
		},
		"r": {
			"openapi:format":        `out_of_scope "FROM_PARAMETER_OBJECT" /paths/~1a/get/parameters/1/format`,
			"openapi:schema/format": `degraded_lowering "FROM_SCHEMA_BESIDE_REF" /paths/~1a/get/parameters/1/schema/format`,
		},
	}
	wantHeader := map[string]string{
		"openapi:divisibleBy": `out_of_scope "FROM_HEADER_OBJECT" ` +
			`/paths/~1a/get/responses/200/headers/X-H/divisibleBy`,
		"openapi:schema/divisibleBy": `out_of_scope "FROM_SCHEMA" ` +
			`/paths/~1a/get/responses/200/headers/X-H/schema/divisibleBy`,
	}
	docs := map[string]*ir.Document{}
	for name, src := range map[string]string{
		"object key first": specFrom(t, specsDir, "param_schema_keys_apart"),
		"schema first":     paramObjectKeysAfterSchema,
	} {
		doc, diags := parseFull(t, src)
		docs[name] = doc
		for _, f := range findings(diags) {
			assert.NotContains(t, f, "unknown-key-entry-taken", name)
		}
		assert.Len(t, findings(diags), 6, "%s: three table findings, two schema keywords, the $ref's key", name)

		a := opTree(t, doc, "a")
		for param, entry := range want {
			assert.Empty(t, cmp.Diff(entry, entries(t, at(namedItem(t, at(a, "params"), param), "unmodeled"))),
				"%s: %s", name, param)
		}
		h := namedItem(t, at(a, "responses", "0", "headers"), "X-H")
		assert.Empty(t, cmp.Diff(wantHeader, entries(t, at(h, "unmodeled"))), name)
	}
	assert.Empty(t, cmp.Diff(withoutSources(t, docs["object key first"]), withoutSources(t, docs["schema first"])),
		"the two orders give one document")
}

// TestParameter_XMLKeptOnce holds an xml object a parameter's schema keeps whole
// to that entry: the vendor extension inside it is not kept again, and the
// parameter's entries are exactly the one.
func TestParameter_XMLKeptOnce(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, specFrom(t, specsDir, "param_xml_kept_once"))
	q := namedItem(t, at(opTree(t, doc, ""), "params"), "q")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:schema/xml": `no_ir_home {"name":"s","x-tag":5} /paths/~1p/get/parameters/0/schema/xml`,
	}, entries(t, at(q, "unmodeled"))))

	doc, _ = parseFull(t, specFrom(t, specsDir, "param_branch_kept_once"))
	q = namedItem(t, at(opTree(t, doc, "g"), "params"), "q")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:schema/xml": `no_ir_home {"name":"s","x-tag":5} /paths/~1s~1{schema_id}/get/parameters/1/schema/xml`,
	}, entries(t, at(q, "unmodeled"))))
}

// TestParameter_BranchKeptOnce holds the keywords an allOf branch of a
// parameter's schema keeps to one entry each, beneath the schema's scope: the
// format no use of its string holds, and the readOnly the parameter has no
// field for, each reported where the keyword's keeper reports it.
func TestParameter_BranchKeptOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "param_branch_kept_once"))
	id := namedItem(t, at(opTree(t, doc, "g"), "params"), "schema_id")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:schema/format":   `degraded_lowering "uuid" /paths/~1s~1{schema_id}/get/parameters/0/schema/allOf/1/format`,
		"openapi:schema/readOnly": `no_ir_home true /paths/~1s~1{schema_id}/get/parameters/0/schema/allOf/1/readOnly`,
	}, entries(t, at(id, "unmodeled"))))
	assert.Equal(t, []string{
		"openapi/degraded-construct info /paths/~1s~1{schema_id}/get/parameters/0/schema/allOf/1",
		"openapi/degraded-construct info /paths/~1s~1{schema_id}/get/parameters/0/schema/allOf/1/readOnly",
		"openapi/degraded-construct info /paths/~1s~1{schema_id}/get/parameters/1/schema/xml",
	}, findings(diags))
}

// TestParameter_RedactionIsSingleSided holds redaction to one side: a schema
// stating it fills the parameter, a parameter's own x-sensitive: false withdraws
// nothing a schema states, and so the inline spelling and the $ref spelling of
// one fact agree. A property's own false likewise withdraws nothing its $ref
// target states.
func TestParameter_RedactionIsSingleSided(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "param_redaction_single_sided"))
	assert.Empty(t, diags)
	a := opTree(t, doc, "a")

	inl := namedItem(t, at(a, "params"), "inl")
	assert.Equal(t, true, at(inl, "secret"))
	assert.Equal(t, "extension-promotion", at(inl, "provenance", "inferred"))
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:x-sensitive":        `vendor_extension false /paths/~1a/get/parameters/0/x-sensitive`,
		"openapi:schema/x-sensitive": `vendor_extension true /paths/~1a/get/parameters/0/schema/x-sensitive`,
	}, entries(t, at(inl, "unmodeled"))))
	assert.Equal(t, true, at(namedItem(t, at(a, "params"), "ref"), "secret"))

	var found bool
	for _, td := range doc.Types {
		m, ok := td.(*ir.Model)
		if !ok {
			continue
		}
		for _, p := range m.Properties {
			if p.WireName == "s" {
				found = true
				assert.True(t, p.Secret, "the property's false withdraws nothing its $ref target states")
			}
		}
	}
	require.True(t, found, "the object parameter's property s lowers")
}

// TestGroups_ARepeatedTagNameTakesItsFirstDeclaration holds two tags of one
// name: the load's duplicate-key error stands and the document is still built,
// each tag keeping its own keys, and the group the name forms taking the first
// declaration's tier and keys.
func TestGroups_ARepeatedTagNameTakesItsFirstDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "tag_repeated_name"))
	assert.Equal(t, []string{"openapi/validation/validation-duplicate-key error /tags/1/name"}, findings(diags))
	d := tree(t, doc)
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:x-first":     `vendor_extension 1 /tags/0/x-first`,
		"openapi:x-stability": `vendor_extension "beta" /tags/0/x-stability`,
	}, entries(t, at(d, "tagDefs", "0", "unmodeled"))))
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:x-second":    `vendor_extension 2 /tags/1/x-second`,
		"openapi:x-stability": `vendor_extension "alpha" /tags/1/x-stability`,
	}, entries(t, at(d, "tagDefs", "1", "unmodeled"))))

	g := tree(t, doc.Services[0].Groups[0])
	assert.Equal(t, "g/tags/T", at(g, "id"))
	assert.Equal(t, "beta", at(g, "stability"))
	assert.Equal(t, "extension-promotion", at(g, "inferred"))
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:x-first":     `vendor_extension 1 /tags/0/x-first`,
		"openapi:x-stability": `vendor_extension "beta" /tags/0/x-stability`,
	}, entries(t, at(g, "unmodeled"))))
}

// TestParameter_AContentEntrysRedactionIsSingleSided holds a content
// parameter's own x-sensitive: false to withdrawing nothing its content entry
// states, as its schema's statement stands beside the same key.
func TestParameter_AContentEntrysRedactionIsSingleSided(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters:
        - {name: c, in: query, x-sensitive: false, content: {application/json: {x-sensitive: true, schema: {type: string}}}}
      responses: {"204": {description: ok}}
`)
	assert.Empty(t, diags)
	c := namedItem(t, at(opTree(t, doc, "a"), "params"), "c")
	assert.Equal(t, true, at(c, "secret"))
	assert.Equal(t, "extension-promotion", at(c, "provenance", "inferred"))
}

// TestPromotion_AMisfitBehindANearerKeyIsReported holds each statement of a
// promoted key that is no value of its kind to one finding at its own key,
// whether it is the nearest statement or one behind it: a parameter's own key,
// its content entry's and their schemas', a header's and its schema's, and a
// path item's behind each of two operations, reported once.
func TestPromotion_AMisfitBehindANearerKeyIsReported(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    x-stability: 13
    get:
      operationId: a
      x-stability: 12
      parameters:
        - {name: m2, in: query, content: {application/json: {x-sensitive: 5, schema: {type: string, x-sensitive: 6}}}}
        - {name: m3, in: query, x-sensitive: 4, schema: {type: string, x-sensitive: 5}}
        - {name: m4, in: query, x-sensitive: true, schema: {type: string, x-sensitive: 7}}
      responses:
        "200":
          description: ok
          headers:
            H: {x-sensitive: 4, schema: {type: string, x-sensitive: 5}}
    post:
      operationId: b
      x-stability: beta
      responses: {"204": {description: ok}}
`)
	const op = "/paths/~1a/get/"
	assert.Equal(t, []string{
		"openapi/degraded-construct info " + op + "parameters/0/content/application~1json/schema/x-sensitive",
		"openapi/degraded-construct info " + op + "parameters/0/content/application~1json/x-sensitive",
		"openapi/degraded-construct info " + op + "parameters/1/schema/x-sensitive",
		"openapi/degraded-construct info " + op + "parameters/1/x-sensitive",
		"openapi/degraded-construct info " + op + "parameters/2/schema/x-sensitive",
		"openapi/degraded-construct info " + op + "responses/200/headers/H/schema/x-sensitive",
		"openapi/degraded-construct info " + op + "responses/200/headers/H/x-sensitive",
		"openapi/degraded-construct info " + op + "x-stability",
		"openapi/degraded-construct info /paths/~1a/x-stability",
	}, findings(diags))
}

// TestGroups_ADeprecationOnATagIsReported holds a deprecation a tag states to
// one info at each key stating it, whether the tag forms a group or not, as
// neither holds a deprecation; a tier key of a tag forming none keeps its own
// finding alone, and a tier on a group-forming tag fills the group.
func TestGroups_ADeprecationOnATagIsReported(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
tags:
  - {name: W, x-stability: deprecated, x-deprecated-reason: old}
  - {name: B, x-stability: beta, x-deprecated-reason: unused}
  - {name: Lone, x-fern-availability: deprecated, x-sunset: "2027-01-01"}
paths:
  /a:
    get: {operationId: a, tags: [W], responses: {"204": {description: ok}}}
  /b:
    get: {operationId: b, tags: [B], responses: {"204": {description: ok}}}
`)
	assert.Equal(t, []string{
		"openapi/unpromoted-extension info /tags/0/x-deprecated-reason",
		"openapi/unpromoted-extension info /tags/0/x-stability",
		"openapi/unpromoted-extension info /tags/2/x-fern-availability",
		"openapi/unpromoted-extension info /tags/2/x-sunset",
	}, findings(diags))
	assert.Equal(t, ir.StabilityBeta, doc.Services[0].Groups[1].Stability)
}

// TestHeader_FallbackAndCarriedReadApart holds a header's two readings of what
// its schema states to the case only each reads: a header's own x-sensitive:
// false withdraws nothing its schema's true states, which the single-sided
// reading takes; and a schema's misfit, behind no key of the header's own, is
// reported at the schema's key, which the nearest reading does.
func TestHeader_FallbackAndCarriedReadApart(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          headers:
            Lent: {x-sensitive: false, schema: {type: string, x-sensitive: true}}
            Carried: {schema: {type: string, x-sensitive: 5}}
`)
	headers := at(opTree(t, doc, "a"), "responses", "0", "headers")
	assert.Equal(t, true, at(namedItem(t, headers, "Lent"), "secret"))
	assert.Equal(t, []string{
		"openapi/degraded-construct info /paths/~1a/get/responses/200/headers/Carried/schema/x-sensitive",
	}, findings(diags))
}
