// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestEncodingRules_AnswerAsTheTableDoes pins the default contentType of a
// part whose schema writes no type at every minor the table covers, which 3.1
// added, and at versions it does not cover and a table with no Encoding
// Object, where no default is stated.
func TestEncodingRules_AnswerAsTheTableDoes(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]string{
		"3.0.3": "", "3.1.0": "application/octet-stream", "3.2.0": "application/octet-stream", "": "", "3.3.0": "",
	} {
		got, ok := oas.Table().EncodingRules(version).UntypedContentType()
		assert.Equal(t, want, got, version)
		assert.Equal(t, want != "", ok, version)
	}
	_, ok := oas.Spec{}.EncodingRules("3.1.0").UntypedContentType()
	assert.False(t, ok, "a table with no Encoding Object states no default")
}
