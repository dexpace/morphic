// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestDialects_ReadADocumentAtTheMinorItDeclares pins the minor a document a
// reference reaches is read at: the one it declares, else the source's, for a
// document declaring no version or one the table does not cover, and the
// rows of a minor shared by every document read at it.
func TestDialects_ReadADocumentAtTheMinorItDeclares(t *testing.T) {
	t.Parallel()
	d := dialects{spec: oas.Table(), version: "3.1.0", minor: "3.1"}
	version, minor, r30 := d.of("3.0.3")
	assert.Equal(t, []string{"3.0.3", "3.0"}, []string{version, minor})
	assert.True(t, r30.Defines("nullable"))
	for _, declared := range []string{"", "3.3.0", "2.0"} {
		version, minor, r := d.of(declared)
		assert.Equal(t, []string{"3.1.0", "3.1"}, []string{version, minor}, "%q", declared)
		assert.False(t, r.Defines("nullable"), "%q", declared)
	}
	// Identity, not equality: rows built twice would be equal, and the claim is
	// that a minor's rows are built once.
	if _, _, again := d.of("3.0.0"); again != r30 {
		t.Error("a second document declaring 3.0 built the minor's rows again")
	}
}
