// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.True(t, r30.Schema.Defines("nullable"))
	for _, declared := range []string{"", "3.3.0", "2.0"} {
		version, minor, r := d.of(declared)
		assert.Equal(t, []string{"3.1.0", "3.1"}, []string{version, minor}, "%q", declared)
		assert.False(t, r.Schema.Defines("nullable"), "%q", declared)
	}
	// Identity, not equality: rows built twice would be equal, and the claim is
	// that a minor's rows are built once.
	if _, _, again := d.of("3.0.0"); again != r30 {
		t.Error("a second document declaring 3.0 built the minor's rows again")
	}
}

// TestLoad_TheSourceSharesItsRowsWithADocumentOfItsMinor pins that the load
// phase reads the source's Schema Object rows off the one table it judged the
// source against, as it reads each other document's: a document declaring the
// source's minor holds the same rows, by identity, and one of another minor
// rows of its own.
func TestLoad_TheSourceSharesItsRowsWithADocumentOfItsMinor(t *testing.T) {
	t.Parallel()
	declaring := func(version string) string {
		return "openapi: " + version + "\ninfo: {title: E, version: \"1\"}\npaths: {}\n" +
			"components: {schemas: {S: {type: string}}}\n"
	}
	dir := externalDir(t, map[string]string{"same.yaml": declaring("3.1.1"), "other.yaml": declaring("3.0.3")})
	doc, _ := loadExternal(t, dir, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n"+
		"  schemas:\n    A: {$ref: 'same.yaml#/components/schemas/S'}\n    B: {$ref: 'other.yaml#/components/schemas/S'}\n",
		Options{})
	rows := map[string]Rows{}
	for _, o := range doc.Others.List() {
		rows[o.Label] = o.Rows
	}
	require.Len(t, rows, 2, "both documents are read")
	if rows["same.yaml"] != doc.Rows {
		t.Error("the source's rows were built apart from those of a document of its minor")
	}
	assert.True(t, rows["other.yaml"].Schema.Defines("nullable"))
	assert.False(t, doc.Rows.Schema.Defines("nullable"))
}
