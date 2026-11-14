// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// TestParseCoreInteger_ReadsTheCoreSchemasThreeForms holds the one reading of
// the core schema's integer to its three forms, and every near miss to none.
func TestParseCoreInteger_ReadsTheCoreSchemasThreeForms(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]nodeview.CoreInteger{
		"0644": {Digits: "0644", Base: 10}, "-007": {Digits: "007", Base: 10, Negative: true},
		"+12": {Digits: "12", Base: 10}, "089": {Digits: "089", Base: 10},
		"0o17": {Digits: "17", Base: 8}, "0x1fA": {Digits: "1fA", Base: 16},
	} {
		got, ok := nodeview.ParseCoreInteger(text)
		assert.True(t, ok, "%q", text)
		assert.Equal(t, want, got, "%q", text)
	}
	for _, text := range []string{
		"", "-", "+", "0o", "0x", "0o8", "0x1G", "0b11", "0X1F", "+0x1F", "-0o7", "1_000", "+-5", "1.5", "1e3", " 1",
	} {
		_, ok := nodeview.ParseCoreInteger(text)
		assert.False(t, ok, "%q", text)
	}
}

// TestCoreInteger_SignIsTheValues holds the sign to the value written, a
// negative zero being zero.
func TestCoreInteger_SignIsTheValues(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]int{"-0": 0, "000": 0, "0x0": 0, "-7": -1, "0o1": 1, "+3": 1} {
		c, ok := nodeview.ParseCoreInteger(text)
		assert.True(t, ok, "%q", text)
		assert.Equal(t, want, c.Sign(), "%q", text)
	}
}
