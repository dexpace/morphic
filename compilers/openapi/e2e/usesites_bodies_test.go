// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBodies_ThreeFindingsOfOneDocument compiles the document of a content key
// that is no media type, a form body with no parts and a body on GET through a
// verified run, and requires the three findings and no other.
func TestBodies_ThreeFindingsOfOneDocument(t *testing.T) {
	t.Parallel()
	doc, diags := compileVerified(t, "bodies.yaml", testdataSpec(t, "media_key_and_form_bodies"))
	assert.Equal(t, []string{
		"openapi/body-without-semantics info /paths/~1a/get/requestBody",
		"openapi/partless-form-body warning /paths/~1mp/post/requestBody/content/multipart~1form-data/schema",
		"openapi/validation/validation-invalid-syntax warning /paths/~1a/get/requestBody/content/application json",
	}, findings(diags))

	content := at(opTree(t, doc, "a"), "request", "contents", "0")
	assert.Equal(t, "application json", at(content, "mediaType"), "the entry lowers as written")
}

// TestBodies_AMethodThatForbidsOne compiles a TRACE with a body through a
// verified run: one warning, under its own code.
func TestBodies_AMethodThatForbidsOne(t *testing.T) {
	t.Parallel()
	_, diags := compileVerified(t, "trace.yaml", testdataSpec(t, "body_on_trace"))
	got := findings(diags)
	assert.Contains(t, got, "openapi/body-forbidden-by-method warning /paths/~1a/trace/requestBody")
	assert.NotContains(t, got, "openapi/body-without-semantics info /paths/~1a/trace/requestBody")
}

// TestContentParameters_SerializationKeywordsAreReported compiles the spec of a
// content parameter writing style, explode and allowReserved through a verified
// run: three warnings, and the document still verifies with the content
// parameter carrying no invented style.
func TestContentParameters_SerializationKeywordsAreReported(t *testing.T) {
	t.Parallel()
	doc, diags := compileVerified(t, "content-params.yaml", testdataSpec(t, "param_content_serialization"))
	assert.Equal(t, []string{
		"openapi/inapplicable-location-keyword warning /paths/~1a/get/parameters/0/allowReserved",
		"openapi/inapplicable-location-keyword warning /paths/~1a/get/parameters/0/explode",
		"openapi/inapplicable-location-keyword warning /paths/~1a/get/parameters/0/style",
	}, findings(diags))
	bindings := at(opTree(t, doc, "a"), "bindings", "http", "0", "paramBindings")
	binding, _ := namedItem(t, bindings, "c").(map[string]any)
	assert.NotContains(t, binding, "style")
}

// TestExamples_AnEmptyKeyChangesTheDocument compiles the two spellings of one
// example, an entry keyed by the empty string and the singular example, through
// verified runs: they differ beyond their sources, and the first draws the one
// info.
func TestExamples_AnEmptyKeyChangesTheDocument(t *testing.T) {
	t.Parallel()
	named, namedDiags := compileVerified(t, "named.yaml", testdataSpec(t, "example_key_empty"))
	single, singleDiags := compileVerified(t, "single.yaml", testdataSpec(t, "example_key_single"))
	assert.Equal(t, []string{"openapi/degraded-construct info /paths/~1a/get/responses/200/content/application~1json/examples/"},
		findings(namedDiags))
	assert.Empty(t, singleDiags)
	assert.NotEqual(t, withoutVersion(t, named), withoutVersion(t, single))
}
