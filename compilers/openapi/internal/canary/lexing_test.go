// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// lexed parses text as the library does and returns its first document, or
// the error.
func lexed(text string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// value returns the value node of key in the mapping a document node wraps.
func value(t *testing.T, doc *yaml.Node, key string) *yaml.Node {
	t.Helper()
	require.Equal(t, yaml.DocumentNode, doc.Kind)
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	require.Failf(t, "no such key", "%q", key)
	return nil
}

// yaml11Lexing pins yaml11-lexing: the lexer reads the YAML 1.1 of the library
// where YAML 1.2 differs, in the escapes it knows, the directive it accepts
// and the characters it ends a line at.
func yaml11Lexing(t *testing.T) {
	const nel, e0, bs = "\xc2\x85", "\xee\x80\x80", "\\"

	_, err := lexed("a: \"x" + bs + "/y\"\n")
	require.Error(t, err, "an escaped slash, which YAML 1.2 allows, is refused")
	assert.Contains(t, err.Error(), "found unknown escape character")

	doc, err := lexed("a: \"x" + bs + "ay\"\nb: 'x" + bs + "ay'\nc: x" + bs + "ay\n")
	require.NoError(t, err)
	assert.Equal(t, "x\x07y", value(t, doc, "a").Value, "a double-quoted escape of a is the bell")
	assert.Equal(t, "x"+bs+"ay", value(t, doc, "b").Value)
	assert.Equal(t, "x"+bs+"ay", value(t, doc, "c").Value)

	_, err = lexed("%YAML 1.2\n---\na: 1\n")
	require.Error(t, err, "a directive declaring 1.2 is refused")
	assert.Contains(t, err.Error(), "found incompatible YAML document")
	for name, text := range map[string]string{
		"1.1":                     "%YAML 1.1\n---\na: 1\n",
		"1.1 after a byte order":  "\xef\xbb\xbf%YAML 1.1\n---\na: 1\n",
		"1.1 padded to a width":   "%YAML 1.1 \n---\na: 1\n",
		"1.1 of a later document": "a: 0\n...\n%YAML 1.1\n---\na: 1\n",
	} {
		_, err := lexed(text)
		assert.NoError(t, err, name)
	}

	doc, err = lexed("a: \"p" + nel + "q\"\nb: 1\n")
	require.NoError(t, err)
	assert.Equal(t, "p q", value(t, doc, "a").Value, "a raw U+0085 in a double-quoted scalar reads as a space")
	assert.Equal(t, 3, value(t, doc, "b").Line, "and the next line's key is placed one line low")

	doc, err = lexed("a: \"x" + e0 + "y\"\nb: x" + e0 + "y\nc: |\n  x" + e0 + "y\n" + e0 + "k: 1 # c" + e0 + "b: 2\nz: 1\n")
	require.NoError(t, err)
	assert.Equal(t, "x"+e0+"y", value(t, doc, "a").Value, "a private-use character is an ordinary one")
	assert.Equal(t, "x"+e0+"y", value(t, doc, "b").Value)
	assert.Equal(t, "x"+e0+"y\n", value(t, doc, "c").Value)
	assert.Equal(t, "# c"+e0+"b: 2", value(t, doc, e0+"k").LineComment, "and a comment holding it is one comment")
	assert.Equal(t, 6, value(t, doc, "z").Line, "with every position after it exact")

	doc, err = lexed("a: ! x\nb: !local y\nc: !!binary aGk=\nd: 2021-01-01\ne: !e%EE%80%80 v\n")
	require.NoError(t, err)
	assert.Equal(t, "!!str", value(t, doc, "a").Tag, "a bare ! tags a string")
	assert.Zero(t, value(t, doc, "a").Style&yaml.TaggedStyle)
	assert.Equal(t, "!local", value(t, doc, "b").Tag)
	assert.NotZero(t, value(t, doc, "b").Style&yaml.TaggedStyle)
	assert.Equal(t, "!!binary", value(t, doc, "c").Tag)
	assert.NotZero(t, value(t, doc, "c").Style&yaml.TaggedStyle)
	assert.Equal(t, "!!timestamp", value(t, doc, "d").Tag, "a plain date is a timestamp without being tagged")
	assert.Zero(t, value(t, doc, "d").Style&yaml.TaggedStyle)
	assert.Equal(t, "!e"+e0, value(t, doc, "e").Tag, "a percent run in a tag decodes to its characters")
}
