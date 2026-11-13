// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRouteShape_LeavesOutEachExpressionsName pins routeShape to the reading
// pathKeyScan gives an expression: "{", a name holding no brace, then "}". A
// brace opening none stays as written, so two templates whose stray braces
// differ keep different shapes.
func TestRouteShape_LeavesOutEachExpressionsName(t *testing.T) {
	t.Parallel()
	for template, want := range map[string]string{
		"/a/{x}/b/{y}": "/a/{}/b/{}",
		"/a":           "/a",
		"":             "",
		"/a/{}":        "/a/{}",
		"/a/{x":        "/a/{x",
		"/a/{b{c}}":    "/a/{b{}}",
		"/a}/{x}":      "/a}/{}",
		"/{x}{y}":      "/{}{}",
		"/a?q={x}":     "/a?q={}",
		"{{{":          "{{{",
		"/{x#y}/{z?w}": "/{}/{}",
	} {
		assert.Equal(t, want, routeShape(template), "routeShape(%q)", template)
	}
}
