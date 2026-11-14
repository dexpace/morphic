// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed pins the format that
// asks for nothing.
func TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed(t *testing.T) {
	assert.True(t, oas.ValidFormat(oas.FormatNone, "anything at all"))
	assert.False(t, oas.ValidFormat(oas.ComponentName, "a b"))
	assert.Equal(t, "1 entry", oas.Plural(1))
	assert.Equal(t, "2 entries", oas.Plural(2))
}

// TestValidFormat_APlainNameURINamesAFragmentAloneByThePlainNameGrammar pins
// the identifier drafts 6 and 7 write: any URI reference, but one naming a
// fragment alone that is no JSON pointer names a plain name, a letter, then
// letters, digits, '-', '_', ':' and '.'.
func TestValidFormat_APlainNameURINamesAFragmentAloneByThePlainNameGrammar(t *testing.T) {
	for s, want := range map[string]bool{
		"#a": true, "#a:b": true, "#Z9-_.:": true, "#_a": false, "#1a": false, "#a b": false, "#é": false,
		"#": true, "#/definitions/a": true, "other.json#_a": true, "https://example.com/s": true, "a%zz": false,
	} {
		assert.Equal(t, want, oas.ValidFormat(oas.PlainNameURI, s), "%q", s)
	}
}
