// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestSecurityRules_AnswerAsTheTableDoes pins which minors read a requirement's
// key as a scheme's URI where it is no component name: 3.2, which added the
// form, and none the table does not cover, nor a table with no Security
// Requirement Object.
func TestSecurityRules_AnswerAsTheTableDoes(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]bool{
		"3.0.3": false, "3.1.1": false, "3.2.0": true, "": false, "3.3.0": false,
	} {
		assert.Equal(t, want, oas.Table().SecurityRules(version).NamesByURI(), version)
	}
	assert.False(t, oas.Spec{}.SecurityRules("3.2.0").NamesByURI(), "a table with no Security Requirement Object")
}
