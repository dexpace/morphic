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

// TestParameter_BranchKeptWholeOnce holds an allOf branch a parameter's schema
// keeps whole to that entry alone: the readOnly read out of the branch is
// inside it, so it is not kept a second time, and both findings that say the
// branch and the keyword are kept verbatim are still drawn.
func TestParameter_BranchKeptWholeOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "param_branch_kept_once"))
	id := namedItem(t, at(opTree(t, doc, "g"), "params"), "schema_id")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:schema/allOf/1": `degraded_lowering {"format":"uuid","readOnly":true,"type":"string"} ` +
			`/paths/~1s~1{schema_id}/get/parameters/0/schema/allOf/1`,
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
