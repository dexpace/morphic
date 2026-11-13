// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestTemplateFault_NamesWhatIsWrong pins the wording of the template check.
func TestTemplateFault_NamesWhatIsWrong(t *testing.T) {
	assert.Empty(t, oas.TemplateFault("https://{a}/{b}"))
	assert.Contains(t, oas.TemplateFault("{a}/{a}"), `"a"`)
	assert.Contains(t, oas.TemplateFault("{a"), "no }")
}

// TestFragmentAt_SplitsAsTheLoweringDoes holds the rule table's split of a
// path key to the table the lowering's is held to, so the expressions a path
// parameter must name are those of the template the key lowers to.
func TestFragmentAt_SplitsAsTheLoweringDoes(t *testing.T) {
	for key, at := range openapitest.PathKeyFragments() {
		if at < 0 {
			at = len(key)
		}
		assert.Equal(t, at, oas.FragmentAt(key), "fragmentAt(%q)", key)
	}
}
