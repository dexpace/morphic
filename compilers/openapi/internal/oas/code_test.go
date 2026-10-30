// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestTemplateFault_NamesWhatIsWrong pins the wording of the template check.
func TestTemplateFault_NamesWhatIsWrong(t *testing.T) {
	assert.Empty(t, oas.TemplateFault("https://{a}/{b}"))
	assert.Contains(t, oas.TemplateFault("{a}/{a}"), `"a"`)
	assert.Contains(t, oas.TemplateFault("{a"), "no }")
}
