// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestKeyFindings_AnAliasedValueIsPlacedAtItsKey holds every kind of finding
// the rule table places at a key to that key's own pointer and line where its
// value is an alias of a node anchored under another key: the anchor is
// another key's coordinate, and which of two keys holds it is the document's
// order, not the defect's place.
func TestKeyFindings_AnAliasedValueIsPlacedAtItsKey(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: v}\n"
	for name, tc := range map[string]struct {
		src, code, want string
	}{
		"a repeated key of an object": {
			head + "x-s: &s first\npaths:\n  /p:\n    get:\n      summary: a\n      summary: *s\n" +
				"      responses: {\"200\": {description: ok}}\n",
			"openapi/validation/validation-duplicate-key", "/paths/~1p/get/summary:8",
		},
		"a repeated key whose last copy is no alias": {
			head + "x-s: &s first\npaths:\n  /p:\n    get:\n      summary: *s\n      summary: a\n" +
				"      responses: {\"200\": {description: ok}}\n",
			"openapi/validation/validation-duplicate-key", "/paths/~1p/get/summary:8",
		},
		"a repeated key of a raw value": {
			head + "x-r: &r 1\nx-limits:\n  rate: 2\n  rate: *r\npaths: {}\n",
			"openapi/validation/validation-duplicate-key", "/x-limits/rate:6",
		},
		"a map key refused": {
			head + "x-sch: &s {type: string}\npaths: {}\ncomponents:\n  schemas:\n    \"bad key!\": *s\n",
			"openapi/validation/validation-invalid-syntax", "/components/schemas/bad key!:7",
		},
		"a reserved response header": {
			head + "x-h: &h {schema: {type: string}}\npaths:\n  /p:\n    get:\n      responses:\n" +
				"        \"200\":\n          description: ok\n          headers:\n            Content-Type: *h\n",
			"openapi/reserved-header-name", "/paths/~1p/get/responses/200/headers/Content-Type:11",
		},
		"a reserved header parameter": {
			head + "x-n: &n Content-Type\npaths:\n  /p:\n    get:\n      parameters:\n" +
				"        - in: header\n          name: *n\n          schema: {type: string}\n" +
				"      responses: {\"200\": {description: ok}}\n",
			"openapi/reserved-header-name", "/paths/~1p/get/parameters/0/name:9",
		},
	} {
		_, diags := parseVerified(t, tc.src)
		assert.Equal(t, []string{tc.want}, placesOf(diags, tc.code), name)
	}
}

// TestKeyFindings_AReservedHeaderIsPlacedAtItsKeyInEitherOrder compiles one
// document with the anchor written at the header and at an extension written
// before it, and requires one place for the finding: the header's key.
func TestKeyFindings_AReservedHeaderIsPlacedAtItsKeyInEitherOrder(t *testing.T) {
	t.Parallel()
	const header = "/paths/~1p/get/responses/200/headers/Content-Type"
	const operation = "  /p:\n    get:\n      responses:\n        \"200\":\n          description: ok\n" +
		"          headers:\n            Content-Type: %s\n"
	extensionFirst := "openapi: 3.1.0\ninfo: {title: t, version: v}\nx-h: &h {schema: {type: string}}\npaths:\n" +
		fmt.Sprintf(operation, "*h")
	headerFirst := "openapi: 3.1.0\ninfo: {title: t, version: v}\npaths:\n" +
		fmt.Sprintf(operation, "&h {schema: {type: string}}") + "x-h: *h\n"
	for name, src := range map[string]string{"extension first": extensionFirst, "header first": headerFirst} {
		_, diags := parseVerified(t, src)
		assert.Equal(t, header, firstPointer(diags, "openapi/reserved-header-name"), name)
	}
}

// placesOf returns "pointer:line" for each finding of code in diags.
func placesOf(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			out = append(out, fmt.Sprintf("%s:%d", diagnostic.Provenance.Pointer, diagnostic.Provenance.Position.Line))
		}
	}
	return out
}

// firstPointer returns the pointer of the first finding of code in diags, or
// "" where there is none.
func firstPointer(diags []ir.Diagnostic, code string) string {
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			return string(diagnostic.Provenance.Pointer)
		}
	}
	return ""
}
