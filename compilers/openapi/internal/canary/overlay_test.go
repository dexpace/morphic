// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"bytes"
	"testing"

	"github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// overlayDeclaring parses an overlay document declaring version, with the
// library's own dialect field set to dialect when it is not empty.
func overlayDeclaring(t *testing.T, version, dialect string) *overlay.Overlay {
	t.Helper()
	src := "overlay: " + version + "\ninfo: {title: o, version: '1'}\nactions: [{target: $, remove: true}]\n"
	if dialect != "" {
		src += "x-speakeasy-jsonpath: " + dialect + "\n"
	}
	o, err := overlay.ParseReader(bytes.NewReader([]byte(src)))
	require.NoError(t, err)
	return o
}

// selected returns the scalar values selector selects in doc, read through
// o's path constructor, or the constructor's error.
func selected(t *testing.T, o *overlay.Overlay, selector, doc string) ([]string, error) {
	t.Helper()
	path, err := o.NewPath(selector, nil)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root))
	var values []string
	for _, n := range path.Query(&root) {
		values = append(values, n.Value)
	}
	return values, nil
}

// overlayJSONPathDialect pins overlay-jsonpath-dialect: the path constructor
// reads an Overlay 1.0 document's selector in a dialect of its own, in which
// an RFC 9535 filter does not parse, unless its dialect field names RFC 9535,
// and reads it as RFC 9535 once the field does, the property-name operator `~`
// included.
func overlayJSONPathDialect(t *testing.T) {
	const filter = `$.paths.*[?@.operationId=="put"]`
	const doc = "paths: {/a: {put: {operationId: put}}}\n"

	_, err := selected(t, overlayDeclaring(t, "1.0.0", ""), filter, doc)
	assert.Error(t, err, "an Overlay 1.0 selector is not read as RFC 9535: %s", revisit(t, result{}))

	_, err = selected(t, overlayDeclaring(t, "1.1.0", ""), filter, doc)
	assert.NoError(t, err, "an Overlay 1.1 selector is: %s", revisit(t, result{}))

	o := overlayDeclaring(t, "1.0.0", "legacy")
	o.JSONPathVersion = overlay.JSONPathRFC9535
	got, err := selected(t, o, filter+".operationId", doc)
	require.NoError(t, err, "with the field set, any selector is: %s", revisit(t, result{}))
	assert.Equal(t, []string{"put"}, got, revisit(t, result{}))

	_, err = selected(t, o, "$.paths.*~", doc)
	assert.NoError(t, err, "and ~ is read, though RFC 9535 has no such operator: %s", revisit(t, result{}))
}

// jsonpathRFC9535 pins jsonpath-rfc9535: the RFC 9535 parser the overlay
// library reads selectors with refuses a member name true, false or null
// after a dot and a \u escape, reads the escape \r as a line feed, accepts a
// control character unescaped in a string, and answers match() and search()
// true for a node lacking the value they test.
func jsonpathRFC9535(t *testing.T) {
	o := overlayDeclaring(t, "1.1.0", "")
	const doc = "{\"true\": T, \"null\": N, \"é\": E, \"a\\nb\": LF, \"a\\rb\": CR, \"a\\tb\": TAB}\n"

	for _, refused := range []string{"$.true", "$..null", `$['\` + `u00e9']`} {
		_, err := selected(t, o, refused, doc)
		assert.Error(t, err, "%s is refused: %s", refused, revisit(t, result{}))
	}

	got, err := selected(t, o, `$['a\rb']`, doc)
	require.NoError(t, err, revisit(t, result{}))
	assert.Equal(t, []string{"LF"}, got, "the escape \\r selects the member a line feed names: %s", revisit(t, result{}))

	got, err = selected(t, o, "$['a\tb']", doc)
	require.NoError(t, err, "a raw tab in a string is read: %s", revisit(t, result{}))
	assert.Equal(t, []string{"TAB"}, got, revisit(t, result{}))

	const list = "[{name: put}, {other: 1}]\n"
	for _, fn := range []string{"match", "search"} {
		got, err = selected(t, o, "$[?"+fn+"(@.name, 'nothing')].other", list)
		require.NoError(t, err, revisit(t, result{}))
		assert.Equal(t, []string{"1"}, got, "%s() selects the element lacking name: %s", fn, revisit(t, result{}))
	}
}
