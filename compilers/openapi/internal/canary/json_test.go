// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"bytes"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// jsonWith returns a JSON OpenAPI document whose info.description is the JSON
// string text, written as given between its quotes, with a path after it.
func jsonWith(text, path string) string {
	return `{"openapi":"3.1.0","info":{"title":"t","version":"1","description":"` + text +
		`"},"paths":{"` + path + `":{}}}`
}

// u spells a \uXXXX escape from its four hex digits, so that the halves of a
// surrogate pair are never adjacent where this file is written.
func u(hex string) string { return `\u` + hex }

// jsonAsYAML pins json-as-yaml: the YAML parser refuses a JSON text that writes
// a surrogate pair, a solidus escape, a key whose colon stands more than 1,024
// characters past its start, a raw DEL, U+FFFE, U+FFFF or a C1 control other
// than U+0085, or a tab before the root or on a line after the one it ends on,
// with a message that names no position of the escape, and reads a raw NEL as
// a space and a raw line or paragraph separator as the end of a line. It
// refuses the same escapes in an overlay.
func jsonAsYAML(t *testing.T) {
	const (
		escapeRefused  = "found invalid Unicode character escape code"
		solidusRefused = "found unknown escape character"
		controlRefused = "control characters are not allowed"
	)
	refused := []struct{ name, text, want string }{
		{"a surrogate pair", jsonWith("x"+u("d83d")+u("de00")+"y", "/"), escapeRefused},
		{"a surrogate pair in upper case", jsonWith(u("D83D")+u("DE00"), "/"), escapeRefused},
		{"a surrogate pair in a name", jsonWith("", "/"+u("d83d")+u("de00")), escapeRefused},
		{"half of a surrogate pair", jsonWith("x"+u("d83d")+"y", "/"), escapeRefused},
		{"a solidus escape", jsonWith(`a\/b`, "/"), solidusRefused},
		{"a solidus escape in a name", jsonWith("", `\/pets`), solidusRefused},
		{"a key of 1100 characters", jsonWith("", "/"+strings.Repeat("x", 1100)), "did not find expected ',' or '}'"},
		{"a key of 1023 characters", jsonWith("", "/"+strings.Repeat("x", 1022)), "did not find expected ',' or '}'"},
		{"a raw DEL", jsonWith("x\u007fy", "/"), controlRefused},
		{"a raw C1 control", jsonWith("x\u0080y", "/"), controlRefused},
		{"a raw U+FFFE", jsonWith("x\ufffey", "/"), controlRefused},
		{"a raw U+FFFF", jsonWith("x\uffffy", "/"), controlRefused},
		{"a tab before the root", "\t" + jsonWith("", "/"), "found character that cannot start any token"},
		{"a tab after the root", jsonWith("", "/") + "\n\t\n", "found character that cannot start any token"},
		{"a tab after a space before the root", " \t" + jsonWith("", "/"), "found character that cannot start any token"},
	}
	for _, tc := range refused {
		_, _, err := soa.Unmarshal(t.Context(), strings.NewReader(tc.text), soa.WithSkipValidation())
		require.Error(t, err, "%s is refused: %s", tc.name, revisit(t, result{}))
		assert.Contains(t, err.Error(), tc.want, "%s: %s", tc.name, revisit(t, result{}))
		assert.NotContains(t, err.Error(), "column", "%s: the message names no column: %s", tc.name, revisit(t, result{}))
	}

	edge, _, err := soa.Unmarshal(t.Context(), strings.NewReader(jsonWith("", "/"+strings.Repeat("x", 1021))), soa.WithSkipValidation())
	require.NoError(t, err, "a key of 1022 characters reads: %s", revisit(t, result{}))
	_, found := edge.Paths.Get("/" + strings.Repeat("x", 1021))
	assert.True(t, found, "and keeps the key whole: %s", revisit(t, result{}))

	control, _, err := soa.Unmarshal(t.Context(), strings.NewReader(jsonWith("caf"+u("00e9"), "/")), soa.WithSkipValidation())
	require.NoError(t, err, "an escape in the Basic Multilingual Plane reads: %s", revisit(t, result{}))
	assert.Equal(t, "café", control.Info.GetDescription(), revisit(t, result{}))

	nel, _, err := soa.Unmarshal(t.Context(), strings.NewReader(jsonWith("x\u0085y", "/")), soa.WithSkipValidation())
	require.NoError(t, err, revisit(t, result{}))
	assert.Equal(t, "x y", nel.Info.GetDescription(), "a raw NEL reads as a space: %s", revisit(t, result{}))

	for _, separator := range []string{"\u2028", "\u2029"} {
		var tree yaml.Node
		src := `{"a":"x` + separator + `y","b":1}`
		require.NoError(t, yaml.Unmarshal([]byte(src), &tree), revisit(t, result{}))
		key := tree.Content[0].Content[2]
		assert.Equal(t, 2, key.Line, "%U ends a line, so the key after it is on the second: %s", separator, revisit(t, result{}))
	}

	for name, text := range map[string]string{
		"a surrogate pair": overlayJSON("x" + u("d83d") + u("de00") + "y"),
		"a solidus escape": overlayJSON(`a\/b`),
	} {
		_, err := overlay.ParseReader(bytes.NewReader([]byte(text)))
		require.Error(t, err, "an overlay with %s is refused: %s", name, revisit(t, result{}))
	}
	_, err = overlay.ParseReader(bytes.NewReader([]byte(overlayJSON("plain"))))
	require.NoError(t, err, "and one with neither is read: %s", revisit(t, result{}))
}

// overlayJSON returns a JSON overlay document whose one action sets the
// description of the info object to text, written as given between quotes.
func overlayJSON(text string) string {
	return `{"overlay":"1.0.0","info":{"title":"o","version":"1"},"actions":` +
		`[{"target":"$.info","update":{"description":"` + text + `"}}]}`
}
