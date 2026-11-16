// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness/openapicases"
)

// TestSuiteGroups_ReadsTheVendoredSuite reads the embedded suite: each group
// named once, a group of 2020-12 among them and one of an earlier release not.
func TestSuiteGroups_ReadsTheVendoredSuite(t *testing.T) {
	groups, err := openapicases.SuiteGroups()
	require.NoError(t, err)
	named := map[string]bool{}
	for _, g := range groups {
		assert.False(t, named[g.Name], "%s is named twice", g.Name)
		named[g.Name] = true
		assert.NotEmpty(t, g.Tests, g.Name)
	}
	assert.True(t, named["items.json/a schema given for prefixItems"])
	assert.False(t, named["items.json/an array of schemas for items"], "a group of 2019-09 and before")
}
